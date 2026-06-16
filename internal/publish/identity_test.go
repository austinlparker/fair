package publish_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aparker.io/fair/internal/publish"
)

func TestEnsureID_GeneratesWhenMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "post.md")
	if err := os.WriteFile(path, []byte(`---
title: "Hello"
date: "2025-04-17"
---

body
`), 0o644); err != nil {
		t.Fatal(err)
	}

	id, err := publish.EnsureID(path)
	if err != nil {
		t.Fatalf("EnsureID: %v", err)
	}
	if id == "" {
		t.Fatal("got empty id")
	}
	// ULID is 26 chars, Crockford base32
	if len(id) != 26 {
		t.Errorf("id length: got %d, want 26", len(id))
	}

	// File now contains the id
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "id: "+id) {
		t.Errorf("frontmatter missing 'id: %s' after write-back:\n%s", id, string(got))
	}
}

func TestEnsureID_PreservesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "post.md")
	original := `---
id: 01HXEXISTING000000000000000
title: Hello
date: "2025-04-17"
---

body
`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	id, err := publish.EnsureID(path)
	if err != nil {
		t.Fatal(err)
	}
	if id != "01HXEXISTING000000000000000" {
		t.Errorf("id changed: got %q, want existing", id)
	}

	// File is byte-identical (no rewrite when id was present)
	got, _ := os.ReadFile(path)
	if string(got) != original {
		t.Errorf("file rewritten unnecessarily:\nwant:\n%s\ngot:\n%s", original, string(got))
	}
}

func TestEnsureID_WriteBackPreservesBody(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "post.md")
	body := "body line 1\nbody line 2\n\nfinal paragraph\n"
	if err := os.WriteFile(path, []byte(`---
title: "Hello"
date: "2025-04-17"
---

`+body), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := publish.EnsureID(path); err != nil {
		t.Fatal(err)
	}

	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), body) {
		t.Errorf("body lost during write-back:\n%s", string(got))
	}
}

func TestEnsureRkey_GeneratesValidTIDWhenMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "post.md")
	if err := os.WriteFile(path, []byte(`---
title: "Hello"
date: "2025-04-17"
---

body
`), 0o644); err != nil {
		t.Fatal(err)
	}

	rkey, err := publish.EnsureRkey(path)
	if err != nil {
		t.Fatalf("EnsureRkey: %v", err)
	}
	// TID is exactly 13 chars (the lexicon's key:tid requirement).
	if len(rkey) != 13 {
		t.Errorf("rkey length: got %d (%q), want 13", len(rkey), rkey)
	}

	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "rkey: "+rkey) {
		t.Errorf("frontmatter missing 'rkey: %s' after write-back:\n%s", rkey, string(got))
	}
}

func TestEnsureRkey_PreservesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "post.md")
	original := `---
rkey: 3lbexisting00
title: Hello
date: "2025-04-17"
---

body
`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	rkey, err := publish.EnsureRkey(path)
	if err != nil {
		t.Fatal(err)
	}
	if rkey != "3lbexisting00" {
		t.Errorf("rkey changed: got %q, want existing", rkey)
	}
	got, _ := os.ReadFile(path)
	if string(got) != original {
		t.Errorf("file rewritten unnecessarily:\nwant:\n%s\ngot:\n%s", original, string(got))
	}
}

func TestEnsureID_RejectsMissingTitle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "post.md")
	os.WriteFile(path, []byte("---\ndate: \"2025-04-17\"\n---\n\nbody\n"), 0o644)

	_, err := publish.EnsureID(path)
	if err == nil {
		t.Fatal("expected error for missing title")
	}
}
