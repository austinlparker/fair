package atproto_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"aparker.io/fair/internal/atproto"
)

func TestClientKey_GeneratesNewWhenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client-key.json")
	key, err := atproto.LoadOrCreateClientKey(path)
	if err != nil {
		t.Fatalf("LoadOrCreateClientKey: %v", err)
	}
	if key == nil {
		t.Fatal("nil key")
	}
	if key.KeyID() == "" {
		t.Error("key has no kid")
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected key file at %s: %v", path, err)
	}
}

func TestClientKey_ReusesExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client-key.json")
	first, err := atproto.LoadOrCreateClientKey(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := atproto.LoadOrCreateClientKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if first.KeyID() != second.KeyID() {
		t.Errorf("kid drifted on reload: %s -> %s", first.KeyID(), second.KeyID())
	}
}

func TestClientKey_FileIs0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("perms model differs on windows")
	}
	path := filepath.Join(t.TempDir(), "client-key.json")
	if _, err := atproto.LoadOrCreateClientKey(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("got perms %o, want 0600", info.Mode().Perm())
	}
}

func TestClientKey_PublicJWKSContainsKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client-key.json")
	key, err := atproto.LoadOrCreateClientKey(path)
	if err != nil {
		t.Fatal(err)
	}
	jwks, err := atproto.PublicJWKS(key)
	if err != nil {
		t.Fatalf("PublicJWKS: %v", err)
	}
	keys, ok := jwks["keys"].([]any)
	if !ok || len(keys) != 1 {
		t.Fatalf("expected 1 key in JWKS, got %v", jwks["keys"])
	}
	pub, _ := keys[0].(map[string]any)
	if pub["kid"] != key.KeyID() {
		t.Errorf("public kid mismatch: %v vs %s", pub["kid"], key.KeyID())
	}
	if pub["d"] != nil {
		t.Error("private key 'd' field leaked into public JWKS")
	}
}
