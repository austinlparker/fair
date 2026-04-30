package atproto

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// CallbackResult is the parsed query string from /callback. Either
// Code+Iss are set (success) or Error is set (PDS rejected the
// authorization). State is always echoed back so the caller can verify
// it matches what was sent in the PAR request.
type CallbackResult struct {
	Code             string
	State            string
	Iss              string
	Error            string
	ErrorDescription string
}

// LoopbackServer is the embedded HTTP server that catches the OAuth
// authorization-code redirect during `fair auth login`. The CLI binds
// it on a random local port, opens the browser to the auth URL with
// this redirect_uri, and Wait()s for the user to complete the consent
// flow in their browser.
type LoopbackServer struct {
	listener net.Listener
	server   *http.Server
	port     int

	once   sync.Once
	result chan CallbackResult
	closed chan struct{}
}

// StartLoopback binds 127.0.0.1:0, registers the /callback handler, and
// starts serving in the background. The returned server is ready to
// receive a redirect immediately. Caller must invoke Close() when done.
func StartLoopback() (*LoopbackServer, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("bind loopback: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	s := &LoopbackServer{
		listener: ln,
		port:     port,
		result:   make(chan CallbackResult, 1),
		closed:   make(chan struct{}),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", s.handleCallback)
	s.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		_ = s.server.Serve(ln)
	}()
	return s, nil
}

// RedirectURI returns the loopback URI for use in the OAuth client
// metadata's `redirect_uri` and the PAR request's redirect_uri. The
// port is random per invocation; the URI is only valid for the life
// of this LoopbackServer.
func (s *LoopbackServer) RedirectURI() string {
	return fmt.Sprintf("http://127.0.0.1:%d/callback", s.port)
}

// Wait blocks until the browser hits /callback or ctx is canceled.
// Calling Wait after Close returns an error.
func (s *LoopbackServer) Wait(ctx context.Context) (CallbackResult, error) {
	select {
	case r := <-s.result:
		return r, nil
	case <-s.closed:
		return CallbackResult{}, errors.New("loopback server closed before callback received")
	case <-ctx.Done():
		return CallbackResult{}, ctx.Err()
	}
}

// Close shuts down the listener and any in-flight serve goroutine. It is
// safe to call multiple times; only the first invocation does work.
func (s *LoopbackServer) Close() error {
	var err error
	s.once.Do(func() {
		close(s.closed)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		err = s.server.Shutdown(ctx)
	})
	return err
}

// handleCallback parses the redirect query string and emits a
// CallbackResult on the result channel. If the channel is full
// (more than one /callback hit, somehow), subsequent hits are dropped
// — the OAuth flow is single-shot.
func (s *LoopbackServer) handleCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	res := CallbackResult{
		Code:             q.Get("code"),
		State:            q.Get("state"),
		Iss:              q.Get("iss"),
		Error:            q.Get("error"),
		ErrorDescription: q.Get("error_description"),
	}

	// Show the user a confirmation page; they may need to switch back
	// to the terminal to see what happens next.
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if res.Error != "" {
		fmt.Fprintf(w,
			`<!doctype html><title>Sign-in failed</title>
<h1>Sign-in failed</h1>
<p>%s%s</p>
<p>You can close this tab and check the terminal.</p>`,
			res.Error,
			func() string {
				if res.ErrorDescription != "" {
					return ": " + res.ErrorDescription
				}
				return ""
			}(),
		)
	} else {
		fmt.Fprint(w, `<!doctype html><title>Signed in</title>
<h1>Signed in.</h1>
<p>You can close this tab and return to the terminal.</p>`)
	}

	select {
	case s.result <- res:
	default:
		// Already received a callback — drop subsequent ones.
	}
}
