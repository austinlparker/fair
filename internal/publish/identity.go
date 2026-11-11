// Package publish implements the markdown -> standard.site PDS record
// pipeline. The orchestrator (Publish in publish.go) drives the flow
// documented in docs/design-plans/2026-04-29-blog-v2026.md §3.
package publish

import (
	"crypto/rand"
	"fmt"
	"os"
	"strings"
	"time"

	"aparker.io/fair/internal/markdown"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/oklog/ulid/v2"
)

// EnsureID returns the post's stable ULID identity, writing it back into
// the source markdown frontmatter if missing.
func EnsureID(path string) (string, error) {
	return ensureFrontmatterKey(path, "id",
		func(fm markdown.Frontmatter) string { return fm.ID },
		generateULID)
}

// EnsureRkey returns the post's TID record key, writing it back into the
// source frontmatter if missing. The site.standard.document lexicon
// declares key:tid, so the rkey must be a TID — we generate one per post
// and persist it in frontmatter so re-publishes (including from another
// machine with no local state) target the same record instead of
// creating a duplicate.
//
// This is distinct from the slug, which drives the URL via the record
// `path` field. The rkey is the PDS record key only.
func EnsureRkey(path string) (string, error) {
	return ensureFrontmatterKey(path, "rkey",
		func(fm markdown.Frontmatter) string { return fm.Rkey },
		func() (string, error) { return syntax.NewTIDNow(0).String(), nil })
}

// ensureFrontmatterKey returns the existing value of a frontmatter key,
// or generates one and writes it back as the first frontmatter line.
func ensureFrontmatterKey(path, key string, get func(markdown.Frontmatter) string, generate func() (string, error)) (string, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	fm, _, err := markdown.Parse(string(src))
	if err != nil {
		return "", fmt.Errorf("parse %s: %w", path, err)
	}
	if v := get(fm); v != "" {
		return v, nil
	}
	value, err := generate()
	if err != nil {
		return "", err
	}
	updated, err := insertFrontmatterLine(string(src), key+": "+value, true)
	if err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	if err := writeFileAtomic(path, updated); err != nil {
		return "", err
	}
	return value, nil
}

// generateULID produces a Crockford-base32 ULID from the current time
// plus random entropy.
func generateULID() (string, error) {
	id, err := ulid.New(ulid.Timestamp(time.Now()), rand.Reader)
	if err != nil {
		return "", fmt.Errorf("generate ULID: %w", err)
	}
	return id.String(), nil
}

// writeBackBskyPost appends a `bsky_post: <at-uri>` line to the YAML
// frontmatter of the file at path. Used after a successful --announce
// so future runs reuse the announce post instead of creating another.
func writeBackBskyPost(path, atURI string) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	updated, err := insertFrontmatterLine(string(src), `bsky_post: "`+atURI+`"`, false)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return writeFileAtomic(path, updated)
}

// insertFrontmatterLine adds one `key: value` line to the frontmatter
// of src — as the first key when first is true, otherwise the last —
// and leaves every other byte of the file untouched. We don't re-marshal
// through yaml.v3 because that would drop the author's comments and
// quoting choices.
func insertFrontmatterLine(src, line string, first bool) (string, error) {
	rest, ok := strings.CutPrefix(src, "---\n")
	opening := "---\n"
	if !ok {
		rest, ok = strings.CutPrefix(src, "---\r\n")
		opening = "---\r\n"
		if !ok {
			return "", fmt.Errorf("source does not start with --- frontmatter fence")
		}
	}
	// The closing fence is the first line that is exactly "---". An
	// empty frontmatter block puts it at the very start of rest.
	var fmYAML, tail string
	if strings.HasPrefix(rest, "---") {
		tail = rest
	} else {
		idx := strings.Index(rest, "\n---")
		if idx < 0 {
			return "", fmt.Errorf("missing closing frontmatter fence")
		}
		fmYAML, tail = rest[:idx+1], rest[idx+1:]
	}
	if first {
		return opening + line + "\n" + fmYAML + tail, nil
	}
	return opening + fmYAML + line + "\n" + tail, nil
}

// writeFileAtomic replaces path via a temp file + rename in the same
// directory, so a crash mid-write can't leave a truncated post.
func writeFileAtomic(path, content string) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s -> %s: %w", tmp, path, err)
	}
	return nil
}
