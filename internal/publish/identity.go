// Package publish implements the markdown -> standard.site PDS record
// pipeline. The orchestrator (Publish in publish.go) drives the 13-step
// flow documented in docs/design-plans/2026-04-29-blog-v2026.md §3.
package publish

import (
	"crypto/rand"
	"fmt"
	"os"
	"strings"
	"time"

	"aparker.io/fair/internal/markdown"
	"github.com/oklog/ulid/v2"
)

// EnsureID returns the post's stable ULID identity, writing it back into
// the source markdown frontmatter if missing. This is the only place the
// CLI mutates files in blog-posts/ and the only thing it writes there.
//
// The ID is load-bearing for rename detection: identity follows the ID,
// not the file path. Once written, the file is "owned" by the publish
// pipeline; subsequent runs use the same ID even if the file moves.
func EnsureID(path string) (string, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	fm, body, err := markdown.Parse(string(src))
	if err != nil {
		return "", fmt.Errorf("parse %s: %w", path, err)
	}
	if fm.ID != "" {
		return fm.ID, nil
	}

	id, err := generateULID()
	if err != nil {
		return "", err
	}
	updated, err := writeBackID(string(src), body, id)
	if err != nil {
		return "", err
	}
	// Atomic write: temp file + rename in same dir.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(updated), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("rename %s -> %s: %w", tmp, path, err)
	}
	return id, nil
}

// generateULID produces a Crockford-base32 ULID using the current
// monotonic time + random entropy.
func generateULID() (string, error) {
	t := ulid.Timestamp(time.Now())
	id, err := ulid.New(t, rand.Reader)
	if err != nil {
		return "", fmt.Errorf("generate ULID: %w", err)
	}
	return id.String(), nil
}

// writeBackID inserts an `id:` line into the YAML frontmatter of src.
// The body is the previously-parsed body slice so we can reconstruct
// the file deterministically.
//
// The frontmatter is reconstructed by splitting the original src around
// the closing fence and inserting "id: <ULID>" as the first frontmatter
// line. We don't re-marshal via yaml.v3 because that would lose comment
// preservation and quote-style choices the user made.
func writeBackID(src, body, id string) (string, error) {
	const fence = "---\n"
	rest, ok := strings.CutPrefix(src, fence)
	if !ok {
		// Some files might use \r\n; try that too.
		rest, ok = strings.CutPrefix(src, "---\r\n")
		if !ok {
			return "", fmt.Errorf("source does not start with --- frontmatter fence")
		}
	}
	closingIdx := strings.Index(rest, "\n---")
	if closingIdx < 0 {
		return "", fmt.Errorf("missing closing frontmatter fence")
	}
	fmYAML := rest[:closingIdx]
	tail := rest[closingIdx:] // starts with "\n---"

	// Insert id as the first key. Trailing newline ensured.
	newYAML := "id: " + id + "\n" + fmYAML
	if !strings.HasSuffix(newYAML, "\n") {
		newYAML += "\n"
	}
	_ = body // body unused — we keep the original tail so closing fence
	// and any blank line between fence and body stay byte-identical.
	return fence + newYAML + tail + "\n", nil
}
