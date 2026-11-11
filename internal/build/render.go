package build

import (
	"bytes"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"aparker.io/fair/internal/markdown"
)

// RenderMarkdown turns a markdown body (as stored in the PDS record's
// content.text field) into the HTML chunk embedded in post.html.
//
// Inline image URLs in the body are stored relative to the publication
// (e.g., "/images/<slug>/foo.png"); we rewrite them to absolute URLs
// against publicationURL so the rendered HTML works the same on disk
// and on the live origin.
//
// Bluesky blockquotes (<blockquote data-bluesky-uri="...">) are
// detected and rewritten into a no-JS-friendly form: a quote block
// followed by a <footer> with a "Discuss on Bluesky" link. Pure text
// transformation; this function performs zero network I/O.
func RenderMarkdown(body, publicationURL string) (string, error) {
	body = markdown.Normalize(body)

	// Goldmark converts to HTML; we then post-process the output to
	// resolve relative image URLs. Doing it on the markdown source
	// would also work but the HTML pass is simpler.
	var buf bytes.Buffer
	if err := markdown.Parser().Convert([]byte(body), &buf); err != nil {
		return "", fmt.Errorf("render markdown: %w", err)
	}
	html := buf.String()

	html, err := resolveRelativeURLs(html, publicationURL)
	if err != nil {
		return "", err
	}
	html = transformBlueskyEmbeds(html)
	html = wrapStandaloneImagesAsFigures(html)
	return html, nil
}

// resolveRelativeURLs joins publicationURL with any href/src that
// starts with "/" (relative to site root). Already-absolute URLs are
// untouched. Anchors (#frag) and other relative forms are also left
// alone for now — the convention is that internal links use absolute
// site-relative paths starting with "/".
func resolveRelativeURLs(html, publicationURL string) (string, error) {
	if publicationURL == "" {
		return "", fmt.Errorf("resolveRelativeURLs: publicationURL is empty")
	}
	pub, err := url.Parse(strings.TrimRight(publicationURL, "/"))
	if err != nil {
		return "", err
	}
	return siteRelativeAttrRe.ReplaceAllString(html, `${1}="`+strings.ReplaceAll(pub.String(), "$", "$$")+`${2}"`), nil
}

// siteRelativeAttrRe matches src="/..." and href="/...". The second
// character must not be another slash: "//host/path" is a
// protocol-relative URL to a different origin, not a site path.
var siteRelativeAttrRe = regexp.MustCompile(`\b(src|href)="(/(?:[^/"][^"]*)?)"`)

// blueskyBlockquoteRe matches blockquotes carrying our convention
// data-bluesky-uri="at://..." marker. Everything inside the quote is
// captured verbatim.
var blueskyBlockquoteRe = regexp.MustCompile(`(?s)<blockquote data-bluesky-uri="(at://[^"]+)"[^>]*>(.*?)</blockquote>`)

// transformBlueskyEmbeds rewrites <blockquote data-bluesky-uri="...">
// blocks into a plain blockquote followed by a "Discuss on Bluesky"
// link, so the rendered output works without JavaScript.
//
// Bluesky URIs are at-uris (at://did:plc:.../app.bsky.feed.post/rkey).
// The link target is the canonical bsky.app web URL derived from the
// at-uri components.
func transformBlueskyEmbeds(html string) string {
	return blueskyBlockquoteRe.ReplaceAllStringFunc(html, func(match string) string {
		parts := blueskyBlockquoteRe.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		atURI := parts[1]
		inner := parts[2]
		webURL, err := bskyWebURL(atURI)
		if err != nil {
			// On parse failure, leave the at-uri as the link target.
			webURL = atURI
		}
		return fmt.Sprintf(`<blockquote>%s<footer>— <a href="%s">Discuss on Bluesky</a></footer></blockquote>`,
			inner, webURL)
	})
}

// standaloneImageParaRe matches a paragraph that contains exactly one
// <img> tag and nothing else (whitespace tolerated). Goldmark wraps
// standalone images in <p>; we promote those to <figure> + <figcaption>
// so the rendered output gets semantic figure markup without changing
// the goldmark parser config (which would invalidate stored hashes).
var standaloneImageParaRe = regexp.MustCompile(`(?s)<p>\s*(<img\b[^>]*>)\s*</p>`)

// imgAltRe extracts the alt attribute (single- or double-quoted) from
// an <img ...> tag. Match group 2 is the alt text.
var imgAltRe = regexp.MustCompile(`alt=("([^"]*)"|'([^']*)')`)

// wrapStandaloneImagesAsFigures rewrites <p><img></p> blocks into
// <figure><img><figcaption>alt</figcaption></figure>. Inline images
// (e.g., "see <img> here") are left alone — only paragraphs whose only
// non-whitespace content is one <img> get promoted.
//
// If the alt attribute is empty or absent, no <figcaption> is emitted
// (an empty caption looks worse than none).
func wrapStandaloneImagesAsFigures(html string) string {
	return standaloneImageParaRe.ReplaceAllStringFunc(html, func(match string) string {
		groups := standaloneImageParaRe.FindStringSubmatch(match)
		if len(groups) < 2 {
			return match
		}
		imgTag := groups[1]
		alt := extractAlt(imgTag)
		if alt == "" {
			return "<figure>" + imgTag + "</figure>"
		}
		return "<figure>" + imgTag + "<figcaption>" + alt + "</figcaption></figure>"
	})
}

// extractAlt returns the alt attribute text from an <img> tag, or ""
// when absent. Handles both ' and " quoting.
func extractAlt(imgTag string) string {
	m := imgAltRe.FindStringSubmatch(imgTag)
	if len(m) == 0 {
		return ""
	}
	if m[2] != "" {
		return m[2]
	}
	return m[3]
}

// bskyWebURL converts at://did:plc:abc/app.bsky.feed.post/xyz into
// https://bsky.app/profile/did:plc:abc/post/xyz.
func bskyWebURL(atURI string) (string, error) {
	const prefix = "at://"
	if !strings.HasPrefix(atURI, prefix) {
		return "", fmt.Errorf("not an at-uri: %s", atURI)
	}
	parts := strings.Split(atURI[len(prefix):], "/")
	if len(parts) != 3 {
		return "", fmt.Errorf("malformed at-uri %s", atURI)
	}
	return fmt.Sprintf("https://bsky.app/profile/%s/post/%s", parts[0], parts[2]), nil
}
