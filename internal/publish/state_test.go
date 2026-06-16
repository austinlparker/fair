package publish_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"aparker.io/fair/internal/publish"
)

func TestState_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")

	want := &publish.State{
		Entries: map[string]publish.StateEntry{
			"wide-events": {
				ID:               "01HXTEST",
				LastHash:         "abc123",
				LastCoverHash:    "cover456",
				LastPublishedAt:  time.Date(2025, 4, 17, 0, 0, 0, 0, time.UTC),
			},
		},
	}

	if err := want.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := publish.LoadState(path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	entry, ok := got.Entries["wide-events"]
	if !ok {
		t.Fatal("entry not found after round-trip")
	}
	if entry.ID != "01HXTEST" {
		t.Errorf("ID: %q", entry.ID)
	}
	if entry.LastHash != "abc123" {
		t.Errorf("LastHash: %q", entry.LastHash)
	}
	if entry.LastCoverHash != "cover456" {
		t.Errorf("LastCoverHash: %q", entry.LastCoverHash)
	}
}

func TestLoadState_MissingReturnsEmpty(t *testing.T) {
	got, err := publish.LoadState(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("missing state should not error: %v", err)
	}
	if got == nil {
		t.Fatal("got nil State")
	}
	if got.Entries == nil {
		t.Error("Entries should be non-nil map")
	}
}

func TestState_Save_Atomic_NoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	s := &publish.State{
		Entries: map[string]publish.StateEntry{
			"foo": {ID: "01HX"},
		},
	}
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}

	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() == filepath.Base(path) {
			continue
		}
		t.Errorf("temp file leaked: %s", e.Name())
	}
}

func TestState_FilePerms(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("perms model differs on windows")
	}
	path := filepath.Join(t.TempDir(), "state.json")
	(&publish.State{Entries: map[string]publish.StateEntry{}}).Save(path)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("got %o, want 0600", info.Mode().Perm())
	}
}

func TestLoadState_CorruptJSONReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	os.WriteFile(path, []byte("{not json"), 0o600)
	_, err := publish.LoadState(path)
	if err == nil {
		t.Fatal("expected parse error")
	}
}
