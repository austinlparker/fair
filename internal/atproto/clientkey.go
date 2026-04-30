package atproto

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/haileyok/atproto-oauth-golang/helpers"
	"github.com/lestrrat-go/jwx/v2/jwk"
)

// LoadOrCreateClientKey returns the persistent ECDSA P-256 client signing
// key at path, generating and saving one if the file is missing. The key
// is used for OAuth client_assertion JWTs (the haileyok library always
// signs assertions, even when the AS allows token_endpoint_auth_method=none).
//
// File perms are 0600. Directory created with MkdirAll if missing.
//
// The returned key is the private JWK; pass it through PublicJWKS to get
// the JWKS document to embed in the OAuth client metadata.
func LoadOrCreateClientKey(path string) (jwk.Key, error) {
	if data, err := os.ReadFile(path); err == nil {
		key, parseErr := jwk.ParseKey(data)
		if parseErr != nil {
			return nil, fmt.Errorf("parse client key %s: %w", path, parseErr)
		}
		return key, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("read client key %s: %w", path, err)
	}

	// Generate fresh key. Pass nil to helpers.GenerateKey to use a
	// timestamp-based kid; we don't have a profile prefix to distinguish
	// keys here because the file path itself is profile-scoped.
	key, err := helpers.GenerateKey(nil)
	if err != nil {
		return nil, fmt.Errorf("generate client key: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("ensure key dir: %w", err)
	}

	data, err := json.Marshal(key)
	if err != nil {
		return nil, fmt.Errorf("marshal client key: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return nil, fmt.Errorf("write client key: %w", err)
	}
	return key, nil
}

// PublicJWKS returns a JWKS-shaped map containing the public half of key.
// Suitable to embed as the "jwks" field of an OAuth client metadata JSON
// document. Verifies the private "d" field is not present in the result.
func PublicJWKS(key jwk.Key) (map[string]any, error) {
	pub, err := key.PublicKey()
	if err != nil {
		return nil, fmt.Errorf("derive public key: %w", err)
	}
	b, err := json.Marshal(pub)
	if err != nil {
		return nil, fmt.Errorf("marshal public key: %w", err)
	}
	var pubMap map[string]any
	if err := json.Unmarshal(b, &pubMap); err != nil {
		return nil, fmt.Errorf("unmarshal public key: %w", err)
	}
	if _, leaked := pubMap["d"]; leaked {
		return nil, fmt.Errorf("private 'd' field present in public JWK; refusing to leak")
	}
	return map[string]any{
		"keys": []any{pubMap},
	}, nil
}
