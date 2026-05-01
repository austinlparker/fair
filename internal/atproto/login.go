package atproto

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"time"

	oauth "github.com/haileyok/atproto-oauth-golang"
	"github.com/haileyok/atproto-oauth-golang/helpers"
	"github.com/lestrrat-go/jwx/v2/jwk"
)

// LoopbackRedirectPath is the path the loopback HTTP server registers
// for the OAuth callback. It's also embedded in the loopback client_id
// query string so the PDS's auto-generated client metadata accepts our
// dynamic-port redirect_uri.
const LoopbackRedirectPath = "/callback"

// BlogScopes is the space-separated OAuth scope string the publish flow
// needs from the auth server. ATProto's scope grammar requires explicit
// per-collection grants; "atproto" alone is identity-only and won't let
// us putRecord against site.standard.* collections.
//
// Composition:
//   - atproto — base scope (always required)
//   - repo:site.standard.publication — read/write the publication singleton
//   - repo:site.standard.document — read/write/delete posts
//   - repo:app.bsky.feed.post — create the optional announce post (--announce)
//     for the bsky_post / bskyPostRef discussion thread on each document
//   - blob — upload cover images
const BlogScopes = "atproto repo:site.standard.publication repo:site.standard.document repo:app.bsky.feed.post blob"

// LoopbackClientID builds an ATProto-compliant loopback client_id of the
// form http://localhost?scope=...&redirect_uri=http%3A%2F%2F127.0.0.1%2F<path>.
// The PDS auto-generates client metadata from this URL — no metadata
// document is fetched, bypassing SSRF protections that block fetches
// to any hostname resolving to a loopback IP.
func LoopbackClientID(redirectPath string) string {
	if redirectPath == "" {
		redirectPath = LoopbackRedirectPath
	}
	q := url.Values{}
	q.Set("scope", BlogScopes)
	// http://127.0.0.1<path> — port left blank because RFC 8252 allows
	// the actual loopback port to be picked at runtime.
	q.Set("redirect_uri", "http://127.0.0.1"+redirectPath)
	return "http://localhost?" + q.Encode()
}

// LoginConfig is the input to Login. ClientKey is the persistent ECDSA
// JWK loaded by LoadOrCreateClientKey. RootCAs is optional — set it to
// the mkcert CA pool when running against the sandbox so the Go HTTP
// client trusts the sandbox cert without requiring sudo mkcert -install.
type LoginConfig struct {
	// PDSURL: the user's PDS URL (e.g., "https://pds.localtest.me",
	// "https://bsky.social"). Must be HTTPS and have no port (the
	// haileyok library rejects URLs with explicit ports).
	PDSURL string

	// Handle: optional login_hint sent in the PAR request. Empty means
	// the user types it on the auth page.
	Handle string

	// ClientID: the public URL of this CLI's OAuth client metadata
	// document, e.g. "https://aparker.io/.well-known/oauth-client-metadata.json".
	ClientID string

	// ClientKey: the ECDSA P-256 JWK used to sign client_assertion JWTs.
	ClientKey jwk.Key

	// RootCAs: optional extra CA pool. If non-nil, it's used as the
	// trust anchor; the system pool is NOT consulted. Use for sandbox
	// (mkcert root). Leave nil for production.
	RootCAs *x509.CertPool
}

// ErrStateMismatch indicates the state value returned in the loopback
// callback did not match what was sent in the PAR request. Treat as a
// CSRF / replay attempt.
var ErrStateMismatch = errors.New("OAuth state mismatch")

// Login runs the full ATProto OAuth loopback flow: starts the loopback
// HTTP listener, makes a PAR request, opens the user's browser to the
// authorization URL, waits for the redirect, exchanges the code for
// tokens, and returns a populated Session.
//
// The browser hop happens via `open`/`xdg-open`; if that fails the
// authorization URL is printed to stderr so the user can open it
// manually. Login blocks until the callback arrives or ctx is canceled.
//
// The returned Session has DPoPPDSNonce empty — that nonce is issued by
// the PDS on the first authenticated XRPC request, not at login time.
func Login(ctx context.Context, cfg LoginConfig) (Session, error) {
	if cfg.PDSURL == "" {
		return Session{}, fmt.Errorf("Login: PDSURL is required")
	}
	if cfg.ClientID == "" {
		return Session{}, fmt.Errorf("Login: ClientID is required")
	}
	if cfg.ClientKey == nil {
		return Session{}, fmt.Errorf("Login: ClientKey is required")
	}

	// 1. Loopback first — we need its random port to construct the
	//    OAuth client's redirect_uri before the PAR request.
	loopback, err := StartLoopback()
	if err != nil {
		return Session{}, fmt.Errorf("start loopback: %w", err)
	}
	defer loopback.Close()

	// 2. HTTP client. If RootCAs is set, trust only those (sandbox);
	//    otherwise rely on system trust (production).
	httpClient := newHTTPClient(cfg.RootCAs)

	// 3. Build the haileyok OAuth client.
	oauthClient, err := oauth.NewClient(oauth.ClientArgs{
		Http:        httpClient,
		ClientJwk:   cfg.ClientKey,
		ClientId:    cfg.ClientID,
		RedirectUri: loopback.RedirectURI(),
	})
	if err != nil {
		return Session{}, fmt.Errorf("oauth.NewClient: %w", err)
	}

	// 4. Resolve PDS -> auth server.
	authServerURL, err := oauthClient.ResolvePdsAuthServer(ctx, cfg.PDSURL)
	if err != nil {
		return Session{}, fmt.Errorf("resolve PDS auth server: %w", err)
	}

	// 5. Auth server metadata. The library validates strict ATProto
	//    conformance; failures here mean the AS isn't speaking the
	//    profile correctly (e.g., issuer carries a port).
	meta, err := oauthClient.FetchAuthServerMetadata(ctx, authServerURL)
	if err != nil {
		return Session{}, fmt.Errorf("fetch auth server metadata: %w", err)
	}

	// 6. Per-session DPoP key. Distinct from the persistent client
	//    signing key — DPoP keys may be rotated freely.
	dpopKey, err := helpers.GenerateKey(nil)
	if err != nil {
		return Session{}, fmt.Errorf("generate dpop key: %w", err)
	}

	// 7. PAR. Library handles PKCE + state generation internally and
	//    returns them on the response so we can verify in step 11.
	//    Scope must match what was declared in the client metadata
	//    (loopback) or the published metadata document (prod).
	parResp, err := oauthClient.SendParAuthRequest(
		ctx,
		authServerURL,
		meta,
		cfg.Handle, // login_hint, may be empty
		BlogScopes, // scope
		dpopKey,
	)
	if err != nil {
		return Session{}, fmt.Errorf("PAR request: %w", err)
	}

	// 8. Build the authorization URL the user opens in their browser.
	authURL, err := buildAuthorizationURL(meta.AuthorizationEndpoint, cfg.ClientID, parResp.RequestUri)
	if err != nil {
		return Session{}, fmt.Errorf("build auth URL: %w", err)
	}

	// 9. Browser hop.
	fmt.Fprintf(os.Stderr, "Opening browser for authorization...\n")
	fmt.Fprintf(os.Stderr, "If it doesn't open, paste this URL:\n  %s\n\n", authURL)
	if err := openBrowser(authURL); err != nil {
		// Not fatal — the user can copy-paste from stderr above.
		fmt.Fprintf(os.Stderr, "(could not auto-open browser: %v)\n", err)
	}

	// 10. Wait for the redirect.
	callback, err := loopback.Wait(ctx)
	if err != nil {
		return Session{}, fmt.Errorf("await callback: %w", err)
	}

	// 11. Verify state to prevent CSRF / replay.
	if callback.State != parResp.State {
		return Session{}, ErrStateMismatch
	}
	if callback.Error != "" {
		msg := callback.Error
		if callback.ErrorDescription != "" {
			msg += ": " + callback.ErrorDescription
		}
		return Session{}, fmt.Errorf("authorization denied: %s", msg)
	}
	if callback.Code == "" {
		return Session{}, fmt.Errorf("callback returned empty code")
	}

	// 12. Code -> tokens.
	tokens, err := oauthClient.InitialTokenRequest(
		ctx,
		callback.Code,
		callback.Iss,
		parResp.PkceVerifier,
		parResp.DpopAuthserverNonce,
		dpopKey,
	)
	if err != nil {
		return Session{}, fmt.Errorf("token exchange: %w", err)
	}

	// 13. Persist DPoP key as JSON bytes so we can round-trip it across
	//     CLI invocations later.
	dpopBytes, err := json.Marshal(dpopKey)
	if err != nil {
		return Session{}, fmt.Errorf("marshal dpop key: %w", err)
	}

	return Session{
		DID:                 tokens.Sub,
		PDSURL:              cfg.PDSURL,
		AuthServerIssuer:    callback.Iss,
		AccessToken:         tokens.AccessToken,
		RefreshToken:        tokens.RefreshToken,
		ExpiresAt:           time.Now().Add(time.Duration(tokens.ExpiresIn) * time.Second),
		DPoPPrivateJWK:      dpopBytes,
		DPoPPDSNonce:        "", // set on first authenticated XRPC call
		DPoPAuthServerNonce: tokens.DpopAuthserverNonce,
	}, nil
}

// newHTTPClient builds an http.Client. When extraRoots is non-nil, the
// client trusts ONLY that pool (suitable for sandbox + mkcert). When
// nil, the client uses the system trust store.
func newHTTPClient(extraRoots *x509.CertPool) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if extraRoots != nil {
		transport.TLSClientConfig = &tls.Config{
			RootCAs: extraRoots,
		}
	}
	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}
}

// buildAuthorizationURL appends client_id and request_uri to the
// authorization endpoint. Per RFC 9126, when using PAR the auth server
// only needs (client_id, request_uri) — all other params live in the
// pushed request.
func buildAuthorizationURL(endpoint, clientID, requestURI string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("client_id", clientID)
	q.Set("request_uri", requestURI)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// openBrowser launches the OS default browser pointed at rawURL.
// macOS: `open`. Linux: `xdg-open`. Windows: not supported (fair sandbox
// doesn't run on Windows; in production deploy from macOS/Linux).
func openBrowser(rawURL string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", rawURL)
	case "linux":
		cmd = exec.Command("xdg-open", rawURL)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
	return cmd.Start()
}
