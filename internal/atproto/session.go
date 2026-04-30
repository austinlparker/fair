// Package atproto holds the OAuth + XRPC plumbing the fair CLI uses to
// talk to a PDS. Session and TokenStore live here; the higher-level
// Client lives in client.go.
package atproto

import "time"

// Session is the per-profile authentication state persisted across CLI
// invocations. Every field is required to be present for the client to
// successfully sign and refresh requests; missing any of them means the
// user must `fair auth login` again.
//
// Field choices follow what haileyok/atproto-oauth-golang produces:
//   - DPoPPrivateJWK is the raw JSON-encoded JWK (not a parsed struct)
//     so we can persist whatever shape the library writes without
//     re-marshaling.
//   - DPoPPDSNonce and DPoPAuthServerNonce rotate independently; the
//     library exposes a callback when the PDS nonce changes.
type Session struct {
	DID                 string    `json:"did"`
	PDSURL              string    `json:"pds_url"`
	AuthServerIssuer    string    `json:"auth_server_issuer"`
	AccessToken         string    `json:"access_token"`
	RefreshToken        string    `json:"refresh_token"`
	ExpiresAt           time.Time `json:"expires_at"`
	DPoPPrivateJWK      []byte    `json:"dpop_private_jwk"`
	DPoPPDSNonce        string    `json:"dpop_pds_nonce"`
	DPoPAuthServerNonce string    `json:"dpop_auth_server_nonce"`
}

// IsExpired reports whether the access token is at-or-past its expiry
// at the given clock time. Callers should refresh before issuing the
// next request when this returns true.
func (s Session) IsExpired(now time.Time) bool {
	return !now.Before(s.ExpiresAt)
}
