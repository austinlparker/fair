package markdown_test

import (
	"testing"

	"aparker.io/fair/internal/markdown"
)

func TestHash_DeterministicForEqualInputs(t *testing.T) {
	in := markdown.HashInput{
		NormalizedBody:  "Hello world.\n",
		Frontmatter:     markdown.Frontmatter{Title: "X", Date: "2025-04-17"},
		CoverSourceHash: "",
	}
	a := markdown.Hash(in)
	b := markdown.Hash(in)
	if a != b {
		t.Errorf("hash not deterministic: %s vs %s", a, b)
	}
	if len(a) != 64 {
		t.Errorf("hash length: got %d, want 64 hex chars", len(a))
	}
}

func TestHash_BodyChange_ChangesHash(t *testing.T) {
	base := markdown.HashInput{
		NormalizedBody: "v1\n",
		Frontmatter:    markdown.Frontmatter{Title: "X", Date: "2025-04-17"},
	}
	other := base
	other.NormalizedBody = "v2\n"
	if markdown.Hash(base) == markdown.Hash(other) {
		t.Error("body change did not affect hash")
	}
}

func TestHash_TitleChange_ChangesHash(t *testing.T) {
	a := markdown.Hash(markdown.HashInput{
		NormalizedBody: "x\n",
		Frontmatter:    markdown.Frontmatter{Title: "A", Date: "2025-04-17"},
	})
	b := markdown.Hash(markdown.HashInput{
		NormalizedBody: "x\n",
		Frontmatter:    markdown.Frontmatter{Title: "B", Date: "2025-04-17"},
	})
	if a == b {
		t.Error("title change did not affect hash")
	}
}

func TestHash_CoverHash_AffectsHash(t *testing.T) {
	base := markdown.HashInput{
		NormalizedBody: "x\n",
		Frontmatter:    markdown.Frontmatter{Title: "X", Date: "2025-04-17"},
	}
	a := markdown.Hash(base)
	withCover := base
	withCover.CoverSourceHash = "abc123"
	b := markdown.Hash(withCover)
	if a == b {
		t.Error("cover hash did not affect output")
	}
}

func TestHash_IgnoresIDAndUpdated(t *testing.T) {
	a := markdown.Hash(markdown.HashInput{
		NormalizedBody: "x\n",
		Frontmatter:    markdown.Frontmatter{Title: "X", Date: "2025-04-17"},
	})
	b := markdown.Hash(markdown.HashInput{
		NormalizedBody: "x\n",
		Frontmatter: markdown.Frontmatter{
			Title:   "X",
			Date:    "2025-04-17",
			ID:      "01HXTEST",
			Updated: "2025-05-01",
		},
	})
	if a != b {
		t.Errorf("id/updated affected hash: %s vs %s", a, b)
	}
}

func TestHash_TagOrderingIgnored(t *testing.T) {
	a := markdown.Hash(markdown.HashInput{
		NormalizedBody: "x\n",
		Frontmatter: markdown.Frontmatter{
			Title: "X",
			Date:  "2025-04-17",
			Tags:  []string{"a", "B", "c"},
		},
	})
	b := markdown.Hash(markdown.HashInput{
		NormalizedBody: "x\n",
		Frontmatter: markdown.Frontmatter{
			Title: "X",
			Date:  "2025-04-17",
			Tags:  []string{"C", "b", "A"},
		},
	})
	if a != b {
		t.Errorf("tag ordering/case affected hash: %s vs %s", a, b)
	}
}
