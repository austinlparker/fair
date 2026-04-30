package markdown

import (
	"fmt"
	"slices"
	"strings"
)

// Canonical returns deterministic bytes representing the hash-relevant
// portion of a Frontmatter. It excludes fields that should not affect
// idempotency (id, updated) and normalizes lists (tags: lowercased, sorted,
// deduplicated). Used as one input to Hash().
//
// Format is a stable, line-per-key textual encoding rather than YAML so
// that "the canonical bytes" don't depend on a YAML library's whitespace
// or quoting choices. Hashing reads bytes verbatim — formatting changes
// here would invalidate stored hashes, so the format is intentionally
// rigid.
func Canonical(fm Frontmatter) []byte {
	var b strings.Builder

	// Keys appear alphabetically; missing/empty fields are omitted so the
	// canonical output of `{Title: "X"}` doesn't carry empty defaults.
	if fm.Cover != "" {
		fmt.Fprintf(&b, "cover: %s\n", fm.Cover)
	}
	if fm.Date != "" {
		fmt.Fprintf(&b, "date: %s\n", fm.Date)
	}
	if fm.Description != "" {
		fmt.Fprintf(&b, "description: %s\n", fm.Description)
	}
	if fm.Slug != "" {
		fmt.Fprintf(&b, "slug: %s\n", fm.Slug)
	}
	if tags := normalizeTags(fm.Tags); len(tags) > 0 {
		fmt.Fprintf(&b, "tags: [%s]\n", strings.Join(tags, ", "))
	}
	if fm.Title != "" {
		fmt.Fprintf(&b, "title: %s\n", fm.Title)
	}
	// id, updated intentionally omitted — they must not affect the hash.

	return []byte(b.String())
}

// normalizeTags returns tags with whitespace trimmed, lowercased, deduped,
// and sorted. Empty tags after trimming are dropped.
func normalizeTags(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, t := range in {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	slices.Sort(out)
	return out
}
