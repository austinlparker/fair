package atproto_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aparker.io/fair/internal/atproto"
)

func TestEmitClientMetadata_WritesAtWellKnownPath(t *testing.T) {
	dir := t.TempDir()
	if err := atproto.EmitClientMetadata(dir, "https://aparker.io"); err != nil {
		t.Fatalf("EmitClientMetadata: %v", err)
	}
	path := filepath.Join(dir, ".well-known", "oauth-client-metadata.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file at %s: %v", path, err)
	}
}

func TestEmitClientMetadata_ProducesValidShape(t *testing.T) {
	dir := t.TempDir()
	if err := atproto.EmitClientMetadata(dir, "https://aparker.io"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".well-known", "oauth-client-metadata.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("metadata is not valid JSON: %v\n%s", err, string(data))
	}

	wantString := map[string]string{
		"client_id":                  "https://aparker.io/.well-known/oauth-client-metadata.json",
		"application_type":           "native",
		"token_endpoint_auth_method": "none",
	}
	if !strings.Contains(got["scope"].(string), "atproto") {
		t.Errorf("scope missing 'atproto': %q", got["scope"])
	}
	if !strings.Contains(got["scope"].(string), "repo:site.standard.document") {
		t.Errorf("scope missing 'repo:site.standard.document': %q", got["scope"])
	}
	for k, want := range wantString {
		if got[k] != want {
			t.Errorf("%s: got %v, want %q", k, got[k], want)
		}
	}

	// dpop_bound_access_tokens is a bool
	if b, ok := got["dpop_bound_access_tokens"].(bool); !ok || !b {
		t.Errorf("dpop_bound_access_tokens: got %v, want true", got["dpop_bound_access_tokens"])
	}

	// grant_types must include both authorization_code and refresh_token
	for _, want := range []string{"authorization_code", "refresh_token"} {
		if !containsAny(got["grant_types"], want) {
			t.Errorf("grant_types missing %q: got %v", want, got["grant_types"])
		}
	}

	// response_types ⊇ ["code"]
	if !containsAny(got["response_types"], "code") {
		t.Errorf("response_types missing 'code': got %v", got["response_types"])
	}

	// redirect_uris must include the loopback callback
	if !containsAny(got["redirect_uris"], "http://127.0.0.1/callback") {
		t.Errorf("redirect_uris missing loopback callback: got %v", got["redirect_uris"])
	}
}

func TestEmitClientMetadata_ClientIDFollowsDomain(t *testing.T) {
	cases := []struct {
		domain string
		want   string
	}{
		{"https://aparker.io", "https://aparker.io/.well-known/oauth-client-metadata.json"},
		{"https://aparker.io/", "https://aparker.io/.well-known/oauth-client-metadata.json"},
		{"http://localhost:8000", "http://localhost:8000/.well-known/oauth-client-metadata.json"},
	}
	for _, tc := range cases {
		t.Run(tc.domain, func(t *testing.T) {
			dir := t.TempDir()
			if err := atproto.EmitClientMetadata(dir, tc.domain); err != nil {
				t.Fatal(err)
			}
			data, _ := os.ReadFile(filepath.Join(dir, ".well-known", "oauth-client-metadata.json"))
			var got map[string]any
			_ = json.Unmarshal(data, &got)
			if got["client_id"] != tc.want {
				t.Errorf("got %q, want %q", got["client_id"], tc.want)
			}
		})
	}
}

func TestEmitClientMetadata_RejectsEmptyDomain(t *testing.T) {
	dir := t.TempDir()
	err := atproto.EmitClientMetadata(dir, "")
	if err == nil {
		t.Fatal("expected error for empty domain")
	}
}

func TestEmitClientMetadata_CreatesDirectoryIfMissing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "deep", "nested", "out")
	if err := atproto.EmitClientMetadata(dir, "https://aparker.io"); err != nil {
		t.Fatalf("should mkdir-p: %v", err)
	}
	path := filepath.Join(dir, ".well-known", "oauth-client-metadata.json")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file not at %s: %v", path, err)
	}
}

// containsAny returns true if v is a JSON-decoded array containing want.
func containsAny(v any, want string) bool {
	arr, ok := v.([]any)
	if !ok {
		return false
	}
	for _, item := range arr {
		if s, ok := item.(string); ok && s == want {
			return true
		}
	}
	return false
}
