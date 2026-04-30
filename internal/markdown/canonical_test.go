package markdown_test

import (
	"bytes"
	"strings"
	"testing"

	"aparker.io/fair/internal/markdown"
)

func TestCanonical_DeterministicForEqualInputs(t *testing.T) {
	fm1 := markdown.Frontmatter{
		Title: "Hello",
		Date:  "2025-04-17",
		Tags:  []string{"observability", "personal"},
	}
	fm2 := fm1
	a := markdown.Canonical(fm1)
	b := markdown.Canonical(fm2)
	if !bytes.Equal(a, b) {
		t.Errorf("not deterministic:\na = %q\nb = %q", a, b)
	}
}

func TestCanonical_TagsSortedLowercasedDeduped(t *testing.T) {
	fm := markdown.Frontmatter{
		Title: "X",
		Date:  "2025-04-17",
		Tags:  []string{"Personal", "observability", "PERSONAL", "Foo"},
	}
	got := string(markdown.Canonical(fm))
	if !strings.Contains(got, "[foo, observability, personal]") {
		t.Errorf("tags not normalized; got:\n%s", got)
	}
}

func TestCanonical_ExcludesIDUpdated(t *testing.T) {
	with := markdown.Frontmatter{
		ID:      "01HXTEST",
		Title:   "Hello",
		Date:    "2025-04-17",
		Updated: "2025-05-01",
	}
	without := markdown.Frontmatter{
		Title: "Hello",
		Date:  "2025-04-17",
	}
	a := markdown.Canonical(with)
	b := markdown.Canonical(without)
	if !bytes.Equal(a, b) {
		t.Errorf("id/updated affected canonical bytes:\nwith    = %q\nwithout = %q", a, b)
	}
}

func TestCanonical_DifferentTitleProducesDifferentBytes(t *testing.T) {
	a := markdown.Canonical(markdown.Frontmatter{Title: "A", Date: "2025-04-17"})
	b := markdown.Canonical(markdown.Frontmatter{Title: "B", Date: "2025-04-17"})
	if bytes.Equal(a, b) {
		t.Error("different titles produced equal bytes")
	}
}

func TestCanonical_FieldsAppearInStableOrder(t *testing.T) {
	fm := markdown.Frontmatter{
		Title:       "Hello",
		Date:        "2025-04-17",
		Description: "An essay",
		Cover:       "./cover.png",
		Tags:        []string{"a"},
		Slug:        "hello",
	}
	got := string(markdown.Canonical(fm))
	// Expect alphabetical key order: cover, date, description, slug, tags, title
	wantOrder := []string{"cover:", "date:", "description:", "slug:", "tags:", "title:"}
	pos := 0
	for _, key := range wantOrder {
		idx := strings.Index(got[pos:], key)
		if idx < 0 {
			t.Fatalf("key %q not found in canonical output (or out of order):\n%s", key, got)
		}
		pos += idx + len(key)
	}
}
