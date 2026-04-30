package build_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aparker.io/fair/internal/build"
)

func TestAtomicPromote_FreshTarget(t *testing.T) {
	tmpDir := t.TempDir()
	tmpOut := filepath.Join(tmpDir, "dist.tmp")
	target := filepath.Join(tmpDir, "dist")

	if err := os.MkdirAll(tmpOut, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpOut, "index.html"), []byte("<h1>v1</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := build.AtomicPromote(tmpOut, target); err != nil {
		t.Fatalf("AtomicPromote: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(target, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "<h1>v1</h1>" {
		t.Errorf("got %q", string(got))
	}
}

func TestAtomicPromote_ReplacesExisting(t *testing.T) {
	tmpDir := t.TempDir()
	tmpOut := filepath.Join(tmpDir, "dist.tmp")
	target := filepath.Join(tmpDir, "dist")

	// Pretend a previous build already exists with v0
	os.MkdirAll(target, 0o755)
	os.WriteFile(filepath.Join(target, "index.html"), []byte("<h1>v0</h1>"), 0o644)

	// New build in tmpOut
	os.MkdirAll(tmpOut, 0o755)
	os.WriteFile(filepath.Join(tmpOut, "index.html"), []byte("<h1>v1</h1>"), 0o644)

	if err := build.AtomicPromote(tmpOut, target); err != nil {
		t.Fatalf("AtomicPromote: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(target, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "<h1>v1</h1>" {
		t.Errorf("target not replaced: got %q", string(got))
	}
}

func TestAtomicPromote_RemovesOldDirAfterPromote(t *testing.T) {
	tmpDir := t.TempDir()
	tmpOut := filepath.Join(tmpDir, "dist.tmp")
	target := filepath.Join(tmpDir, "dist")

	os.MkdirAll(target, 0o755)
	os.WriteFile(filepath.Join(target, "index.html"), []byte("v0"), 0o644)
	os.MkdirAll(tmpOut, 0o755)
	os.WriteFile(filepath.Join(tmpOut, "index.html"), []byte("v1"), 0o644)

	if err := build.AtomicPromote(tmpOut, target); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		// Only the target should remain. The tmp dir was renamed; old
		// dir was renamed-aside and removed.
		if !strings.HasSuffix(e.Name(), "dist") || strings.HasPrefix(e.Name(), "dist.old") {
			t.Errorf("leftover directory: %s", e.Name())
		}
	}
}

func TestAtomicPromote_TmpOutMustExist(t *testing.T) {
	tmpDir := t.TempDir()
	err := build.AtomicPromote(filepath.Join(tmpDir, "missing"), filepath.Join(tmpDir, "dist"))
	if err == nil {
		t.Fatal("expected error for missing tmp dir")
	}
}

func TestSweepStaleDistOldDirs(t *testing.T) {
	tmpDir := t.TempDir()
	for _, name := range []string{"dist.old.123", "dist.old.456", "unrelated"} {
		os.MkdirAll(filepath.Join(tmpDir, name), 0o755)
	}

	if err := build.SweepStaleDistOld(tmpDir); err != nil {
		t.Fatal(err)
	}

	entries, _ := os.ReadDir(tmpDir)
	got := []string{}
	for _, e := range entries {
		got = append(got, e.Name())
	}
	want := []string{"unrelated"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Errorf("got %v, want %v", got, want)
	}
}
