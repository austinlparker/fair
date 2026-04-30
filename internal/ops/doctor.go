package ops

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"strings"
	"time"

	"aparker.io/fair/internal/atproto"
	"aparker.io/fair/internal/config"
	"aparker.io/fair/internal/publish"
)

// Check is one row of the doctor output. OK reports whether the check
// passed; Detail explains what was checked or what failed.
type Check struct {
	Name   string
	OK     bool
	Detail string
}

// DoctorOptions configure what Doctor inspects. Pass the same store +
// rootCAs the rest of the CLI uses so the auth check exercises the
// real session.
type DoctorOptions struct {
	Profile   string
	Cfg       *config.Config
	Store     atproto.TokenStore
	RootCAs   *x509.CertPool
	StateFile string
}

// Doctor runs a battery of health checks against the active profile.
// Returns the list of checks performed; callers print + decide overall
// exit status.
func Doctor(ctx context.Context, opts DoctorOptions) []Check {
	checks := []Check{}

	// 1. Config sanity. Already loaded so this is a tautology, but
	//    surfacing as a step gives operators a clear progress feed.
	checks = append(checks, Check{
		Name:   "config loaded",
		OK:     true,
		Detail: fmt.Sprintf("profile=%s pds=%s domain=%s", opts.Profile, opts.Cfg.PDSURL, opts.Cfg.Domain),
	})

	hc := &http.Client{Timeout: 10 * time.Second}
	if opts.RootCAs != nil {
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.TLSClientConfig = &tls.Config{RootCAs: opts.RootCAs}
		hc.Transport = t
	}

	// 2. PDS reachable.
	pdsHealth := strings.TrimRight(opts.Cfg.PDSURL, "/") + "/xrpc/_health"
	checks = append(checks, httpCheck(ctx, hc, "PDS reachable", pdsHealth))

	// 3. Publication record present.
	pubURL := fmt.Sprintf("%s/xrpc/com.atproto.repo.getRecord?repo=%s&collection=site.standard.publication&rkey=self",
		strings.TrimRight(opts.Cfg.PDSURL, "/"), opts.Cfg.DID)
	checks = append(checks, httpCheck(ctx, hc, "publication record exists", pubURL))

	// 4. Auth session present and not expired.
	session, err := opts.Store.Load(opts.Profile)
	switch {
	case err != nil:
		checks = append(checks, Check{Name: "auth session", OK: false, Detail: "no session — run `fair auth login`"})
	case session.IsExpired(time.Now()):
		checks = append(checks, Check{Name: "auth session", OK: false, Detail: "session expired — re-run `fair auth login`"})
	default:
		checks = append(checks, Check{Name: "auth session", OK: true, Detail: fmt.Sprintf("did=%s expires=%s", session.DID, session.ExpiresAt.Format(time.RFC3339))})
	}

	// 5. State.json consistency.
	state, err := publish.LoadState(opts.StateFile)
	if err != nil {
		checks = append(checks, Check{Name: "local state", OK: false, Detail: err.Error()})
	} else {
		checks = append(checks, Check{Name: "local state", OK: true, Detail: fmt.Sprintf("%d entries", len(state.Entries))})
	}

	return checks
}

// httpCheck performs a HEAD/GET against url and reports the result as
// a Check.
func httpCheck(ctx context.Context, hc *http.Client, name, url string) Check {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return Check{Name: name, OK: false, Detail: err.Error()}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return Check{Name: name, OK: false, Detail: err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return Check{Name: name, OK: true, Detail: fmt.Sprintf("HTTP %d", resp.StatusCode)}
	}
	return Check{Name: name, OK: false, Detail: fmt.Sprintf("HTTP %d", resp.StatusCode)}
}
