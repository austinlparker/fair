package publish

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"aparker.io/fair/internal/images"
)

// RewriteImages finds inline images in the markdown body, copies their
// source files to <mirrorDir>/<slug>/<basename>, and replaces the
// references in the body with /images/<slug>/<basename>.
//
// Already-absolute URLs (http:// or https://) and other non-relative
// paths are left untouched. The returned body is byte-stable across
// repeated calls with the same inputs (idempotent).
//
// Image refs are detected by simple regex-style scanning of `![alt](path)`
// — the markdown AST would catch them more cleanly but goldmark doesn't
// emit position offsets for in-place rewriting. The pattern is well-
// defined enough that the regex matches what goldmark sees.
func RewriteImages(body, slug, sourceDir, mirrorDir string) (string, error) {
	// Walk the body looking for ![alt](url) patterns. We do this byte-
	// by-byte rather than with regexp.ReplaceAllStringFunc because we
	// want to error on unfindable images (regex would silently keep them).
	var out strings.Builder
	out.Grow(len(body))

	i := 0
	for i < len(body) {
		// Look for an image start: "![" preceded by neither "\\" nor "!".
		startMarker := strings.Index(body[i:], "![")
		if startMarker < 0 {
			out.WriteString(body[i:])
			break
		}
		startMarker += i

		// Find the matching ']' for the alt text.
		altEnd := strings.IndexByte(body[startMarker+2:], ']')
		if altEnd < 0 || startMarker+2+altEnd+1 >= len(body) || body[startMarker+2+altEnd+1] != '(' {
			// Not an image expression — emit literally and continue past "!".
			out.WriteString(body[i : startMarker+1])
			i = startMarker + 1
			continue
		}
		altText := body[startMarker+2 : startMarker+2+altEnd]

		// Find the matching ')' for the URL.
		urlStart := startMarker + 2 + altEnd + 2 // past "]("
		urlEnd := strings.IndexByte(body[urlStart:], ')')
		if urlEnd < 0 {
			out.WriteString(body[i:startMarker])
			out.WriteString(body[startMarker:])
			i = len(body)
			break
		}
		rawURL := body[urlStart : urlStart+urlEnd]

		// Emit text up to the start of this image.
		out.WriteString(body[i:startMarker])

		// Decide whether to rewrite.
		if isAbsoluteURL(rawURL) {
			// Untouched.
			out.WriteString(body[startMarker : urlStart+urlEnd+1])
		} else {
			rewritten, err := copyAndRewrite(rawURL, altText, slug, sourceDir, mirrorDir)
			if err != nil {
				return "", err
			}
			out.WriteString(rewritten)
		}
		i = urlStart + urlEnd + 1
	}
	return out.String(), nil
}

// isAbsoluteURL returns true if u parses as an absolute URL with a scheme.
// Relative paths and bare anchors return false.
func isAbsoluteURL(u string) bool {
	parsed, err := url.Parse(u)
	if err != nil {
		return false
	}
	return parsed.IsAbs()
}

// copyAndRewrite resolves a relative image ref against sourceDir, copies
// it to mirrorDir/slug/<basename>, and returns the rewritten markdown
// "![alt](/images/slug/basename)" snippet.
func copyAndRewrite(rawURL, altText, slug, sourceDir, mirrorDir string) (string, error) {
	src := filepath.Join(sourceDir, rawURL)
	if _, err := os.Stat(src); err != nil {
		return "", fmt.Errorf("image %q not found at %s: %w", rawURL, src, err)
	}
	base := filepath.Base(rawURL)
	destDir := filepath.Join(mirrorDir, slug)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", fmt.Errorf("ensure mirror dir %s: %w", destDir, err)
	}
	dest := filepath.Join(destDir, base)
	if err := copyFileIdempotent(src, dest); err != nil {
		return "", err
	}
	// Generate resized variants alongside the source so the build flow
	// can emit srcset/sizes. Unsupported formats (gif, svg, etc.) come
	// back as (nil, nil) — image becomes a plain non-responsive <img>.
	if _, err := images.GenerateVariants(dest); err != nil {
		return "", fmt.Errorf("generate variants for %s: %w", dest, err)
	}
	return fmt.Sprintf("![%s](/images/%s/%s)", altText, slug, base), nil
}

// copyFileIdempotent copies src to dest. If dest already exists with
// identical contents, it's a no-op (preserves mtime so dist re-deploys
// don't think every image changed). Otherwise the copy overwrites.
func copyFileIdempotent(src, dest string) error {
	srcData, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}
	if existing, err := os.ReadFile(dest); err == nil && bytesEqual(existing, srcData) {
		return nil // identical — skip
	}
	if err := os.WriteFile(dest, srcData, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", dest, err)
	}
	return nil
}

// bytesEqual is a thin wrapper to keep the io.ReadAll-based shape if we
// ever switch to streaming for large files.
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// readAll is kept around as a placeholder for streaming if needed later.
var _ = io.ReadAll
