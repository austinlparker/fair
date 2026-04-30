package atproto_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"aparker.io/fair/internal/atproto"
)

func sampleSession() atproto.Session {
	return atproto.Session{
		DID:                 "did:plc:abc",
		PDSURL:              "https://bsky.social",
		AuthServerIssuer:    "https://bsky.social",
		AccessToken:         "AT-token",
		RefreshToken:        "RT-token",
		ExpiresAt:           time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC),
		DPoPPrivateJWK:      []byte(`{"kty":"EC","crv":"P-256","x":"abc","y":"def","d":"ghi"}`),
		DPoPPDSNonce:        "pds-nonce-1",
		DPoPAuthServerNonce: "as-nonce-1",
	}
}

func TestFileStore_RoundTripPreservesAllFields(t *testing.T) {
	store := atproto.NewFileStore(t.TempDir())
	want := sampleSession()

	if err := store.Save("prod", want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.Load("prod")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round-trip mismatch:\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestFileStore_LoadMissingReturnsErrNotFound(t *testing.T) {
	store := atproto.NewFileStore(t.TempDir())
	_, err := store.Load("nonexistent")
	if !errors.Is(err, atproto.ErrSessionNotFound) {
		t.Errorf("got %v, want ErrSessionNotFound", err)
	}
}

func TestFileStore_DeleteRemovesSession(t *testing.T) {
	store := atproto.NewFileStore(t.TempDir())
	if err := store.Save("prod", sampleSession()); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete("prod"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err := store.Load("prod")
	if !errors.Is(err, atproto.ErrSessionNotFound) {
		t.Errorf("after delete, expected ErrSessionNotFound, got %v", err)
	}
}

func TestFileStore_DeleteMissingIsNoOp(t *testing.T) {
	store := atproto.NewFileStore(t.TempDir())
	// No save first — delete on missing must not error
	if err := store.Delete("ghost"); err != nil {
		t.Errorf("delete on missing: %v", err)
	}
}

func TestFileStore_FilePermsAre0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("perms model differs on windows")
	}
	dir := t.TempDir()
	store := atproto.NewFileStore(dir)
	if err := store.Save("prod", sampleSession()); err != nil {
		t.Fatal(err)
	}

	matches, err := filepath.Glob(filepath.Join(dir, "session.prod*"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("no session file found: %v %v", err, matches)
	}
	info, err := os.Stat(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("got perms %o, want 0600", info.Mode().Perm())
	}
}

func TestFileStore_SaveIsAtomic_NoPartialWrite(t *testing.T) {
	// Save the same profile twice; if Save crashed in the middle we'd
	// expect a partial file. We can't crash mid-write in a unit test,
	// but we *can* assert that Save's intermediate temp file does not
	// linger after success.
	dir := t.TempDir()
	store := atproto.NewFileStore(dir)
	if err := store.Save("prod", sampleSession()); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() == "session.prod.json" {
			continue
		}
		t.Errorf("unexpected leftover file after Save: %q", e.Name())
	}
}

func TestFileStore_ProfilesIsolated(t *testing.T) {
	store := atproto.NewFileStore(t.TempDir())
	prodSess := sampleSession()
	prodSess.DID = "did:plc:prod"

	sandSess := sampleSession()
	sandSess.DID = "did:plc:sandbox"

	if err := store.Save("prod", prodSess); err != nil {
		t.Fatal(err)
	}
	if err := store.Save("sandbox", sandSess); err != nil {
		t.Fatal(err)
	}

	got, err := store.Load("prod")
	if err != nil || got.DID != "did:plc:prod" {
		t.Errorf("prod load: got %v, %v", got, err)
	}
	got, err = store.Load("sandbox")
	if err != nil || got.DID != "did:plc:sandbox" {
		t.Errorf("sandbox load: got %v, %v", got, err)
	}
}

func TestSession_IsExpired(t *testing.T) {
	now := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name      string
		expiresAt time.Time
		want      bool
	}{
		{"future, not expired", now.Add(time.Hour), false},
		{"past, expired", now.Add(-time.Hour), true},
		{"at exactly now (treat as expired)", now, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := atproto.Session{ExpiresAt: tc.expiresAt}
			if got := s.IsExpired(now); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
