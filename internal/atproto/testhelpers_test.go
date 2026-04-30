package atproto_test

import (
	"path/filepath"
	"testing"

	"aparker.io/fair/internal/atproto"
)

// testJWKS returns a minimal JWKS map suitable for EmitClientMetadata
// in tests. Each call generates a fresh client key in t.TempDir() so
// tests don't share state.
func testJWKS(t *testing.T) map[string]any {
	t.Helper()
	keyPath := filepath.Join(t.TempDir(), "test-client-key.json")
	key, err := atproto.LoadOrCreateClientKey(keyPath)
	if err != nil {
		t.Fatalf("LoadOrCreateClientKey: %v", err)
	}
	jwks, err := atproto.PublicJWKS(key)
	if err != nil {
		t.Fatalf("PublicJWKS: %v", err)
	}
	return jwks
}
