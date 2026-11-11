package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runCLI constructs the root command and executes it with the given args
// and env, returning combined stdout+stderr and exit error. It does not
// actually call os.Exit.
func runCLI(t *testing.T, args []string, env map[string]string) (string, error) {
	t.Helper()
	for k, v := range env {
		t.Setenv(k, v)
	}
	root := newRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs(args)
	err := root.Execute()
	return buf.String(), err
}

// writeProfileConfig creates ~/.config/fair/config.<profile>.toml fixtures
// and returns the directory containing them.
func writeProfileConfig(t *testing.T, profiles map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range profiles {
		path := filepath.Join(dir, "config."+name+".toml")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const minimalProd = `pds_url = "https://prod.example/pds"
did = "did:plc:prod"
domain = "https://aparker.io"
publication_name = "Prod"
blog_posts = "/tmp/posts"
state_file = ".fair/state.prod.json"
images_mirror = "images-mirror.prod/"
dist_dir = "dist/"
`

const minimalSandbox = `pds_url = "http://localhost:2583"
did = "did:plc:sandbox"
domain = "http://localhost:8000"
publication_name = "Sandbox"
blog_posts = "/tmp/posts"
state_file = ".fair/state.sandbox.json"
images_mirror = "images-mirror.sandbox/"
dist_dir = "dist.sandbox/"
`

func TestConfigShow_PicksProfileFromFlag(t *testing.T) {
	dir := writeProfileConfig(t, map[string]string{
		"prod":    minimalProd,
		"sandbox": minimalSandbox,
	})

	out, err := runCLI(t, []string{"--profile", "prod", "--config-dir", dir, "config", "show"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "https://prod.example/pds") {
		t.Errorf("expected prod PDS URL in output, got:\n%s", out)
	}
}

func TestConfigShow_PicksProfileFromEnv(t *testing.T) {
	dir := writeProfileConfig(t, map[string]string{
		"prod":    minimalProd,
		"sandbox": minimalSandbox,
	})

	out, err := runCLI(t,
		[]string{"--config-dir", dir, "config", "show"},
		map[string]string{"FAIR_PROFILE": "sandbox"},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "http://localhost:2583") {
		t.Errorf("expected sandbox PDS URL in output, got:\n%s", out)
	}
}

func TestConfigShow_NoProfile_Errors(t *testing.T) {
	dir := writeProfileConfig(t, map[string]string{
		"prod": minimalProd,
	})
	// explicit clear of env so a developer's shell doesn't pollute the test
	t.Setenv("FAIR_PROFILE", "")

	out, err := runCLI(t, []string{"--config-dir", dir, "config", "show"}, nil)
	if err == nil {
		t.Fatalf("expected error, got success with output:\n%s", out)
	}
	if !strings.Contains(err.Error(), "profile") {
		t.Errorf("error %q should mention profile", err.Error())
	}
}

func TestConfigShow_TypoSuggestsCloseProfile(t *testing.T) {
	dir := writeProfileConfig(t, map[string]string{
		"prod":    minimalProd,
		"sandbox": minimalSandbox,
	})

	_, err := runCLI(t, []string{"--profile", "sandbo", "--config-dir", dir, "config", "show"}, nil)
	if err == nil {
		t.Fatal("expected typo error")
	}
	if !strings.Contains(err.Error(), "sandbox") {
		t.Errorf("error %q should suggest %q", err.Error(), "sandbox")
	}
	if !strings.Contains(err.Error(), "did you mean") {
		t.Errorf("error %q should say 'did you mean'", err.Error())
	}
}

func TestEmitClientMetadata_WritesUnderProfileDomain(t *testing.T) {
	cfgDir := writeProfileConfig(t, map[string]string{"prod": minimalProd})
	outDir := t.TempDir()

	out, err := runCLI(t,
		[]string{"--profile", "prod", "--config-dir", cfgDir, "emit-client-metadata", "--out", outDir},
		nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v\noutput: %s", err, out)
	}

	wantPath := filepath.Join(outDir, ".well-known", "oauth-client-metadata.json")
	data, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("expected metadata at %s: %v", wantPath, err)
	}
	if !strings.Contains(string(data), `"client_id": "https://aparker.io/.well-known/oauth-client-metadata.json"`) {
		t.Errorf("client_id not derived from prod domain:\n%s", string(data))
	}
}

func TestEmitClientMetadata_RequiresOutFlag(t *testing.T) {
	cfgDir := writeProfileConfig(t, map[string]string{"prod": minimalProd})
	_, err := runCLI(t,
		[]string{"--profile", "prod", "--config-dir", cfgDir, "emit-client-metadata"},
		nil,
	)
	if err == nil {
		t.Fatal("expected error: --out is required")
	}
	if !strings.Contains(err.Error(), "out") {
		t.Errorf("error %q should mention --out", err.Error())
	}
}

// End-to-end through the CLI with FAIR_APP_PASSWORD: createSession,
// then publish --all against a fake PDS using the bearer token.
func TestPublishAll_AppPassword(t *testing.T) {
	var puts, badAuth int
	pds := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/xrpc/com.atproto.server.createSession":
			var in struct{ Identifier, Password string }
			_ = json.NewDecoder(r.Body).Decode(&in)
			if in.Identifier != "did:plc:ci" || in.Password != "app-pass" {
				w.WriteHeader(http.StatusUnauthorized)
				fmt.Fprint(w, `{"error":"AuthenticationRequired","message":"bad credentials"}`)
				return
			}
			fmt.Fprint(w, `{"accessJwt":"tok123","refreshJwt":"r","handle":"ci.test","did":"did:plc:ci"}`)
		case "/xrpc/com.atproto.repo.putRecord":
			if r.Header.Get("Authorization") != "Bearer tok123" {
				badAuth++
			}
			puts++
			fmt.Fprint(w, `{"uri":"at://did:plc:ci/site.standard.document/x","cid":"bafy"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer pds.Close()

	content := t.TempDir()
	for _, slug := range []string{"one", "two"} {
		dir := filepath.Join(content, slug)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		src := "---\ntitle: " + slug + "\ndate: \"2025-04-17\"\n---\n\nbody\n"
		if err := os.WriteFile(filepath.Join(dir, "index.md"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dir := writeProfileConfig(t, map[string]string{"ci": fmt.Sprintf(`pds_url = %q
did = "did:plc:ci"
domain = "https://example.com"
publication_name = "CI"
blog_posts = %q
state_file = %q
images_mirror = %q
`, pds.URL, content, filepath.Join(content, ".fair", "state.json"), filepath.Join(content, "images-mirror"))})

	args := []string{"--config-dir", dir, "--profile", "ci", "publish", "--all"}
	out, err := runCLI(t, args, map[string]string{"FAIR_APP_PASSWORD": "app-pass"})
	if err != nil {
		t.Fatalf("publish --all: %v\n%s", err, out)
	}
	if puts != 2 || badAuth != 0 {
		t.Errorf("puts=%d badAuth=%d, want 2 and 0\n%s", puts, badAuth, out)
	}

	out, err = runCLI(t, args, nil)
	if err != nil {
		t.Fatalf("second run: %v\n%s", err, out)
	}
	if puts != 2 || strings.Count(out, "skipped") != 2 {
		t.Errorf("second run should skip both posts: puts=%d\n%s", puts, out)
	}

	if _, err := runCLI(t, args, map[string]string{"FAIR_APP_PASSWORD": "wrong"}); err == nil {
		t.Error("expected failure with a bad app password")
	}
	if _, err := runCLI(t, append(args, "one/index.md"), nil); err == nil {
		t.Error("expected --all plus a path to be rejected")
	}
}
