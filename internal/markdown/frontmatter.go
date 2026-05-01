package markdown

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Frontmatter is the typed view of a post's YAML frontmatter. Fields that
// the publish flow cares about (title, date, etc.) are surfaced explicitly.
// Unknown YAML keys are silently ignored — callers should treat the source
// markdown file as the source of truth for any extra metadata.
//
// All string fields are stored as written. Date normalization to RFC3339
// happens later in the publish flow, not here.
type Frontmatter struct {
	ID          string   `yaml:"id,omitempty"`
	Title       string   `yaml:"title"`
	Date        string   `yaml:"date"`
	Description string   `yaml:"description,omitempty"`
	Cover       string   `yaml:"cover,omitempty"`
	Tags        []string `yaml:"tags,omitempty"`
	Slug        string   `yaml:"slug,omitempty"`
	Updated     string   `yaml:"updated,omitempty"`
	BskyPost    string   `yaml:"bsky_post,omitempty"` // at-uri of an app.bsky.feed.post record; published as bskyPostRef StrongRef
}

// Split separates the optional YAML frontmatter (delimited by "---" lines)
// from the body of a markdown source. Returns ("", body, nil) when no
// frontmatter is present.
//
// The returned frontmatter is the YAML text between the two fence lines
// (no fences, ending with one trailing newline if non-empty). The body is
// the source after the closing fence with up to one leading blank line
// trimmed (matching gray-matter convention).
func Split(src string) (frontmatter, body string, err error) {
	const fence = "---"
	if !strings.HasPrefix(src, fence) {
		return "", src, nil
	}

	// Walk the source line-by-line. Line 0 is the opening fence; we look
	// for a subsequent line that is exactly "---" (CR-tolerant) to mark
	// the close.
	lines := strings.Split(src, "\n")
	openLine := strings.TrimSuffix(lines[0], "\r")
	if openLine != fence {
		// First line starts with "---" but has trailing junk (e.g. "---x");
		// treat the source as bodyless-frontmatter mismatch — accept as body.
		return "", src, nil
	}

	closing := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSuffix(lines[i], "\r") == fence {
			closing = i
			break
		}
	}
	if closing < 0 {
		return "", "", fmt.Errorf("unterminated frontmatter (missing closing %q)", fence)
	}

	frontmatter = strings.Join(lines[1:closing], "\n")
	if frontmatter != "" {
		frontmatter += "\n"
	}

	// Body is everything after the closing fence line. strings.Split with
	// "\n" leaves the trailing slice intact; rejoin and trim one blank line.
	body = strings.Join(lines[closing+1:], "\n")
	body = strings.TrimPrefix(body, "\n")
	return frontmatter, body, nil
}

// Parse splits source markdown into typed frontmatter + body and validates
// that required fields (title, date) are present. Use Parse when the
// publish flow needs typed access; use Split for raw splitting.
func Parse(src string) (Frontmatter, string, error) {
	fmRaw, body, err := Split(src)
	if err != nil {
		return Frontmatter{}, "", err
	}

	var fm Frontmatter
	if fmRaw != "" {
		if err := yaml.Unmarshal([]byte(fmRaw), &fm); err != nil {
			return Frontmatter{}, "", fmt.Errorf("parse frontmatter: %w", err)
		}
	}

	if fm.Title == "" {
		return Frontmatter{}, "", fmt.Errorf("frontmatter missing required field: title")
	}
	if fm.Date == "" {
		return Frontmatter{}, "", fmt.Errorf("frontmatter missing required field: date")
	}

	return fm, body, nil
}
