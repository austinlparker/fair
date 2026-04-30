package config_test

import (
	"path/filepath"
	"strings"
	"testing"

	"aparker.io/fair/internal/config"
)

func TestResolveProfile_FlagWinsOverEnv(t *testing.T) {
	got, err := config.ResolveProfile("prod", "sandbox", []string{"prod", "sandbox"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "prod" {
		t.Errorf("got %q, want prod", got)
	}
}

func TestResolveProfile_EnvUsedWhenFlagEmpty(t *testing.T) {
	got, err := config.ResolveProfile("", "sandbox", []string{"prod", "sandbox"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "sandbox" {
		t.Errorf("got %q, want sandbox", got)
	}
}

func TestResolveProfile_NoFlagNoEnv_Errors(t *testing.T) {
	_, err := config.ResolveProfile("", "", []string{"prod", "sandbox"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "FAIR_PROFILE") && !strings.Contains(err.Error(), "--profile") {
		t.Errorf("error %q should mention --profile or FAIR_PROFILE", err.Error())
	}
}

func TestResolveProfile_UnknownProfile_SuggestsCloseMatch(t *testing.T) {
	_, err := config.ResolveProfile("sandbo", "", []string{"prod", "sandbox"})
	if err == nil {
		t.Fatal("expected error for typo")
	}
	if !strings.Contains(err.Error(), "sandbox") {
		t.Errorf("error %q should suggest %q", err.Error(), "sandbox")
	}
	if !strings.Contains(err.Error(), "did you mean") {
		t.Errorf("error %q should say 'did you mean'", err.Error())
	}
}

func TestResolveProfile_UnknownProfile_NoCloseMatch(t *testing.T) {
	_, err := config.ResolveProfile("xyzzy", "", []string{"prod", "sandbox"})
	if err == nil {
		t.Fatal("expected error for unknown profile")
	}
	// far-from-anything should still surface the unknown name and the known list,
	// but not lie about a match
	if strings.Contains(err.Error(), "did you mean") {
		t.Errorf("error %q must not suggest a match for far-from-anything name", err.Error())
	}
	if !strings.Contains(err.Error(), "xyzzy") {
		t.Errorf("error %q should mention the unknown profile name", err.Error())
	}
}

func TestResolveProfile_KnownProfileFromEnv(t *testing.T) {
	got, err := config.ResolveProfile("", "prod", []string{"prod", "sandbox"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "prod" {
		t.Errorf("got %q, want prod", got)
	}
}

func TestResolveProfile_EmptyKnownList_Errors(t *testing.T) {
	_, err := config.ResolveProfile("prod", "", nil)
	if err == nil {
		t.Fatal("expected error when no profiles configured")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "no profiles") {
		t.Errorf("error %q should explain no profiles configured", err.Error())
	}
}

func TestDiscoverProfiles_FindsConfigFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"config.prod.toml", "config.sandbox.toml"} {
		if err := writeFile(filepath.Join(dir, name), "pds_url = \"\""); err != nil {
			t.Fatal(err)
		}
	}
	// noise files that should be ignored
	for _, name := range []string{"profiles.toml", "config.toml", "session.json", "config.bad.txt"} {
		if err := writeFile(filepath.Join(dir, name), ""); err != nil {
			t.Fatal(err)
		}
	}

	got, err := config.DiscoverProfiles(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"prod", "sandbox"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	gotSet := map[string]bool{}
	for _, n := range got {
		gotSet[n] = true
	}
	for _, n := range want {
		if !gotSet[n] {
			t.Errorf("missing profile %q in %v", n, got)
		}
	}
}

func TestDiscoverProfiles_EmptyDir(t *testing.T) {
	got, err := config.DiscoverProfiles(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

func TestDiscoverProfiles_MissingDir(t *testing.T) {
	got, err := config.DiscoverProfiles(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("missing dir should not error (returns empty list): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}
