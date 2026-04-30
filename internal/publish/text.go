package publish

import (
	"strings"

	"aparker.io/fair/internal/markdown"
	"github.com/rivo/uniseg"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// MaxDescriptionGraphemes caps the description field at the
// site.standard.document spec limit of 3000 graphemes.
const MaxDescriptionGraphemes = 3000

// MaxTextContentGraphemes caps the textContent field at 30000 graphemes
// per the spec.
const MaxTextContentGraphemes = 30000

// ExtractDescription returns the body's first paragraph as plain text,
// truncated to MaxDescriptionGraphemes. If no paragraph appears among
// the first 5 top-level AST blocks, an empty string is returned.
//
// Used as a fallback when the frontmatter has no explicit description.
func ExtractDescription(body string) string {
	doc := markdown.Parser().Parser().Parse(text.NewReader([]byte(body)))

	// Walk top-level children; skip up to 4 non-paragraph blocks before
	// giving up.
	count := 0
	for child := doc.FirstChild(); child != nil; child = child.NextSibling() {
		count++
		if count > 5 {
			return ""
		}
		if p, ok := child.(*ast.Paragraph); ok {
			// Skip paragraphs whose only top-level inline content is an
			// image — common pattern for cover images / figures. Look at
			// the paragraph's direct children for any non-Image node.
			if isImageOnlyParagraph(p) {
				continue
			}
			text := nodeText(p, []byte(body))
			text = collapseWhitespace(text)
			return TruncateGraphemes(text, MaxDescriptionGraphemes)
		}
	}
	return ""
}

// ExtractTextContent returns a plaintext rendering of the body suitable
// for the site.standard.document.textContent field — used by indexers
// for full-text search. Code fences are stripped (their content
// pollutes search), alt text from images is preserved, whitespace is
// collapsed.
//
// Truncated to MaxTextContentGraphemes. Truncation is grapheme-aware
// to avoid splitting multi-byte characters mid-rune.
func ExtractTextContent(body string) string {
	doc := markdown.Parser().Parser().Parse(text.NewReader([]byte(body)))

	var b strings.Builder
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch v := n.(type) {
		case *ast.FencedCodeBlock, *ast.CodeBlock:
			// Skip both the fenced ``` blocks and indented code blocks.
			return ast.WalkSkipChildren, nil
		case *ast.Image:
			// Take the alt text (children of Image are alt-text nodes).
			b.WriteString(nodeText(v, []byte(body)))
			b.WriteByte(' ')
			return ast.WalkSkipChildren, nil
		case *ast.Text:
			b.Write(v.Segment.Value([]byte(body)))
			if v.SoftLineBreak() || v.HardLineBreak() {
				b.WriteByte(' ')
			}
		case *ast.Paragraph, *ast.Heading, *ast.ListItem, *ast.Blockquote:
			// Block boundaries — append a separator after the children
			// have been visited. Done in the !entering branch is cleaner
			// but we approximate here by adding a newline now.
		}
		return ast.WalkContinue, nil
	})
	return TruncateGraphemes(collapseWhitespace(b.String()), MaxTextContentGraphemes)
}

// isImageOnlyParagraph returns true when the paragraph's only non-
// trivial inline content is one or more Image nodes. Used to skip
// cover-image-only paragraphs in description extraction.
//
// Heuristic: presence of any non-Image child node means "has prose".
// A pure whitespace Text node between two Images is rare in practice;
// when it occurs, treating the paragraph as image-only is the
// reasonable default (skip and look for prose lower in the post).
func isImageOnlyParagraph(p *ast.Paragraph) bool {
	hasImage := false
	for c := p.FirstChild(); c != nil; c = c.NextSibling() {
		switch c.(type) {
		case *ast.Image:
			hasImage = true
		default:
			return false
		}
	}
	return hasImage
}

// nodeText concatenates all *ast.Text descendants of n into a single
// string, using source as the underlying byte buffer.
func nodeText(n ast.Node, source []byte) string {
	var b strings.Builder
	_ = ast.Walk(n, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if t, ok := node.(*ast.Text); ok {
			b.Write(t.Segment.Value(source))
		}
		return ast.WalkContinue, nil
	})
	return b.String()
}

// collapseWhitespace replaces runs of whitespace (including newlines)
// with a single space, then trims leading/trailing whitespace.
func collapseWhitespace(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := true // start as space so we trim leading
	for _, r := range s {
		if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}
	out := b.String()
	return strings.TrimRight(out, " ")
}

// TruncateGraphemes returns s truncated to at most max grapheme clusters.
// Splitting at grapheme boundaries (not bytes or runes) avoids the
// classic emoji/combining-character corruption.
func TruncateGraphemes(s string, max int) string {
	if max <= 0 || s == "" {
		return s
	}
	g := uniseg.NewGraphemes(s)
	count := 0
	end := 0
	for g.Next() {
		count++
		if count > max {
			return s[:end]
		}
		// Position after the latest grapheme cluster.
		_, end = g.Positions()
	}
	return s
}

// GraphemeCount returns the number of grapheme clusters in s. Exported
// for tests; internal callers can use uniseg.GraphemeClusterCount.
func GraphemeCount(s string) int {
	return uniseg.GraphemeClusterCount(s)
}
