package build

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"aparker.io/fair/internal/images"
)

// imgTagRe captures whole <img ...> tags. Group 1 is the inner attributes.
var imgTagRe = regexp.MustCompile(`(?s)<img\b([^>]*)>`)

// srcAttrRe captures the value of src= within an attribute string.
var srcAttrRe = regexp.MustCompile(`\bsrc="([^"]+)"`)

// EnrichResponsiveImages walks every <img> in html, looks for sibling
// resized variants under mirrorDir, and rewrites the tag with srcset/
// sizes/width/height attributes when variants are present. Tags whose
// src is not under publicationURL+/images/, or whose source can't be
// decoded, are left untouched.
//
// The expected pipeline order is:
//  1. RenderMarkdown produces HTML with `<img src="<absolute>/images/<slug>/<base>.<ext>">`.
//  2. This pass enriches those tags using on-disk mirror state.
//
// mirrorDir empty disables enrichment (tests, sandbox-without-mirror).
func EnrichResponsiveImages(html, mirrorDir, publicationURL string) string {
	if mirrorDir == "" || publicationURL == "" {
		return html
	}
	prefix := strings.TrimRight(publicationURL, "/") + "/images/"
	return imgTagRe.ReplaceAllStringFunc(html, func(tag string) string {
		attrs := imgTagRe.FindStringSubmatch(tag)[1]
		srcMatch := srcAttrRe.FindStringSubmatch(attrs)
		if srcMatch == nil {
			return tag
		}
		src := srcMatch[1]
		if !strings.HasPrefix(src, prefix) {
			return tag
		}
		rel := strings.TrimPrefix(src, prefix) // "<slug>/<base>.<ext>"
		srcPath := filepath.Join(mirrorDir, rel)

		srcW, srcH, variants, err := images.FindVariants(srcPath)
		if err != nil || (len(variants) == 0 && srcW == 0) {
			return tag
		}

		// Build srcset entries: each variant + the original at its
		// natural width. Sort by width ascending so the smallest is
		// listed first (cosmetic — browser doesn't care).
		type entry struct {
			url string
			w   int
		}
		entries := make([]entry, 0, len(variants)+1)
		for _, v := range variants {
			variantURL := strings.TrimSuffix(src, filepath.Ext(rel))
			variantURL = fmt.Sprintf("%s-%d%s", variantURL, v.Width, filepath.Ext(rel))
			entries = append(entries, entry{url: variantURL, w: v.Width})
		}
		entries = append(entries, entry{url: src, w: srcW})
		sort.Slice(entries, func(i, j int) bool { return entries[i].w < entries[j].w })

		var srcset strings.Builder
		for i, e := range entries {
			if i > 0 {
				srcset.WriteString(", ")
			}
			fmt.Fprintf(&srcset, "%s %dw", e.url, e.w)
		}

		// Strip any pre-existing srcset/sizes/width/height to avoid
		// duplicates if this pass runs twice.
		cleaned := stripAttrs(attrs, "srcset", "sizes", "width", "height")
		return fmt.Sprintf(
			`<img%s srcset="%s" sizes="100vw" width="%d" height="%d">`,
			cleaned, srcset.String(), srcW, srcH,
		)
	})
}

// stripAttrs returns attrs with the listed attribute=value pairs
// removed. Preserves leading whitespace and quoting style.
func stripAttrs(attrs string, names ...string) string {
	for _, name := range names {
		re := regexp.MustCompile(`\s+` + regexp.QuoteMeta(name) + `="[^"]*"`)
		attrs = re.ReplaceAllString(attrs, "")
	}
	return attrs
}

// EnhanceImages adds loading="lazy" decoding="async" to every <img> tag
// in html that doesn't already have those attributes. Free perf wins:
// browser-driven lazy loading defers off-screen image fetches, async
// decoding moves the decode off the main thread.
//
// Idempotent — re-running over already-enhanced HTML is a no-op for
// tags that already carry both attributes.
func EnhanceImages(html string) string {
	return imgTagRe.ReplaceAllStringFunc(html, func(tag string) string {
		attrs := imgTagRe.FindStringSubmatch(tag)[1]
		if !strings.Contains(attrs, "loading=") {
			attrs += ` loading="lazy"`
		}
		if !strings.Contains(attrs, "decoding=") {
			attrs += ` decoding="async"`
		}
		return "<img" + attrs + ">"
	})
}
