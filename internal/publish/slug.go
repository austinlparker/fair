package publish

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
)

// ReservedSlugs are names that would collide with paths the build flow
// emits at the site root (a post at /tags would be overwritten by the
// tag index, and so on). ValidateSlug rejects them.
var ReservedSlugs = []string{
	"self", "index", "feed", "sitemap", ".well-known",
	"tags", "archive", "images", "covers",
}

// ValidateSlug returns nil if s matches the publish-flow slug grammar:
// non-empty, lowercase ASCII letters/digits/hyphens, no double-hyphens,
// no leading/trailing hyphen, not a reserved name. The slug becomes the
// record's `path` field and the post's directory in the built site;
// internal/build/safety.go enforces the same grammar on the read side.
func ValidateSlug(s string) error {
	if s == "" {
		return errors.New("slug is empty")
	}
	if slices.Contains(ReservedSlugs, s) {
		return fmt.Errorf("slug %q is reserved", s)
	}
	if strings.HasPrefix(s, "-") {
		return fmt.Errorf("slug %q has leading hyphen", s)
	}
	if strings.HasSuffix(s, "-") {
		return fmt.Errorf("slug %q has trailing hyphen", s)
	}
	if strings.Contains(s, "--") {
		return fmt.Errorf("slug %q has double hyphen", s)
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-':
		default:
			return fmt.Errorf("slug %q has invalid character %q (only [a-z0-9-] allowed)", s, r)
		}
	}
	return nil
}

// SlugFromPath derives the slug from a markdown file path: it's the name
// of the file's parent directory. blog-posts/wide-events/index.md
// produces "wide-events". Top-level markdown files (no parent dir under
// the content root) are rejected — we want a directory-per-post layout
// so images can live next to the index.md.
//
// The returned slug is NOT validated against grammar; pass it through
// ValidateSlug separately.
func SlugFromPath(path string) (string, error) {
	dir := filepath.Dir(path)
	base := filepath.Base(dir)
	if base == "" || base == "." || base == "/" {
		return "", fmt.Errorf("cannot derive slug from path %q (no parent directory)", path)
	}
	// Reject when the parent looks like a generic root (heuristic: "blog-posts",
	// "posts", "content"). User should put each post in its own directory.
	for _, generic := range []string{"blog-posts", "posts", "content"} {
		if base == generic {
			return "", fmt.Errorf("cannot derive slug from %q: file is at content root, expected blog-posts/<slug>/index.md", path)
		}
	}
	return base, nil
}

// FindPosts returns every post source (a file named index.md) under
// root, sorted. Hidden directories (.git, .fair, ...) are skipped.
func FindPosts(root string) ([]string, error) {
	var posts []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == "index.md" {
			posts = append(posts, p)
		}
		return nil
	})
	return posts, err // WalkDir visits in lexical order
}
