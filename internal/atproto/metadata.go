package atproto

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ClientMetadata is the public OAuth client metadata document the PDS
// fetches at the URL given as `client_id`. The shape follows the ATProto
// OAuth profile (RFC 7591 + RFC 9449 DPoP) for a native loopback CLI:
// no client secret, application_type=native, token_endpoint_auth_method=none,
// loopback redirect URI.
//
// The @atproto/oauth-provider validator enforces native + none in
// ClientManager.validateClientMetadata; using private_key_jwt with native
// is rejected with `invalid_client_metadata`.
type ClientMetadata struct {
	ClientID                string   `json:"client_id"`
	ClientName              string   `json:"client_name"`
	ApplicationType         string   `json:"application_type"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	RedirectURIs            []string `json:"redirect_uris"`
	Scope                   string   `json:"scope"`
	DPoPBoundAccessTokens   bool     `json:"dpop_bound_access_tokens"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
}

// EmitClientMetadata writes the OAuth client metadata JSON to
// <out>/.well-known/oauth-client-metadata.json. The client_id is derived
// from the domain so the document is self-referential when fetched.
//
// `domain` should be an absolute URL like "https://aparker.io" (trailing
// slash optional). Empty domain returns an error — silently emitting
// half-configured metadata would be a footgun later.
//
// out is created with MkdirAll if missing.
func EmitClientMetadata(out string, domain string) error {
	domain = strings.TrimRight(domain, "/")
	if domain == "" {
		return fmt.Errorf("empty domain: cannot derive client_id")
	}

	meta := ClientMetadata{
		ClientID:                domain + "/.well-known/oauth-client-metadata.json",
		ClientName:              "fair",
		ApplicationType:         "native",
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		ResponseTypes:           []string{"code"},
		RedirectURIs:            []string{"http://127.0.0.1/callback"},
		Scope:                   BlogScopes,
		DPoPBoundAccessTokens:   true,
		TokenEndpointAuthMethod: "none",
	}

	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	// Trailing newline — friendlier in editors and curl output.
	data = append(data, '\n')

	dir := filepath.Join(out, ".well-known")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	path := filepath.Join(dir, "oauth-client-metadata.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
