package build

import (
	"crypto/tls"
	"crypto/x509"
)

// httpsClientConfig builds a *tls.Config trusting the given pool.
// Split into its own file so atomic.go and build.go don't both
// duplicate the TLS plumbing.
func httpsClientConfig(pool *x509.CertPool) *tls.Config {
	return &tls.Config{RootCAs: pool}
}
