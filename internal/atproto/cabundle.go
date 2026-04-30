package atproto

import (
	"crypto/x509"
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// LoadExtraRoots reads PEM CA bundles from each path in paths and
// returns them as an x509.CertPool. Empty paths are ignored. Used to
// trust mkcert's local CA when running against the sandbox.
//
// Returns nil pool if no path was supplied; callers can pass that
// straight into newHTTPClient to fall back to system roots.
func LoadExtraRoots(paths ...string) (*x509.CertPool, error) {
	var anyAdded bool
	pool := x509.NewCertPool()
	for _, p := range paths {
		if p == "" {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil, fmt.Errorf("CA bundle not found: %s", p)
			}
			return nil, fmt.Errorf("read CA bundle %s: %w", p, err)
		}
		if !pool.AppendCertsFromPEM(data) {
			return nil, fmt.Errorf("no PEM certs found in %s", p)
		}
		anyAdded = true
	}
	if !anyAdded {
		return nil, nil
	}
	return pool, nil
}
