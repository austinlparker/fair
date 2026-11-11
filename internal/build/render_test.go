package build_test

import (
	"strings"
	"testing"

	"aparker.io/fair/internal/build"
)

func TestRenderMarkdown_BasicHTML(t *testing.T) {
	got, err := build.RenderMarkdown("# Hello\n\nWorld.\n", "https://aparker.io")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<h1", "Hello", "<p>", "World"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestRenderMarkdown_ResolvesRelativeImageURL(t *testing.T) {
	body := "![alt](/images/post/foo.png)\n"
	got, err := build.RenderMarkdown(body, "https://aparker.io")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `src="https://aparker.io/images/post/foo.png"`) {
		t.Errorf("relative URL not resolved:\n%s", got)
	}
}

func TestRenderMarkdown_LeavesAbsoluteURLAlone(t *testing.T) {
	body := "![alt](https://example.com/foo.png)\n"
	got, err := build.RenderMarkdown(body, "https://aparker.io")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "https://example.com/foo.png") {
		t.Errorf("absolute URL changed:\n%s", got)
	}
	if strings.Contains(got, "aparker.io/foo.png") {
		t.Errorf("absolute URL prefixed with publication:\n%s", got)
	}
}

func TestRenderMarkdown_BlueskyEmbedToFooterLink(t *testing.T) {
	body := `<blockquote data-bluesky-uri="at://did:plc:abc/app.bsky.feed.post/xyz">a quote</blockquote>` + "\n"
	got, err := build.RenderMarkdown(body, "https://aparker.io")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "data-bluesky-uri") {
		t.Errorf("data-bluesky-uri should be transformed away:\n%s", got)
	}
	if !strings.Contains(got, "https://bsky.app/profile/did:plc:abc/post/xyz") {
		t.Errorf("bsky web URL missing:\n%s", got)
	}
	if !strings.Contains(got, "Discuss on Bluesky") {
		t.Errorf("footer link missing:\n%s", got)
	}
}

func TestRenderMarkdown_StandaloneImageBecomesFigure(t *testing.T) {
	body := "![cover image](/images/post/foo.png)\n"
	got, err := build.RenderMarkdown(body, "https://aparker.io")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "<figure>") {
		t.Errorf("standalone image not wrapped in figure:\n%s", got)
	}
	if !strings.Contains(got, "<figcaption>cover image</figcaption>") {
		t.Errorf("alt text not used as figcaption:\n%s", got)
	}
}

func TestRenderMarkdown_StandaloneImageWithoutAlt_NoFigcaption(t *testing.T) {
	body := "![](/images/post/foo.png)\n"
	got, err := build.RenderMarkdown(body, "https://aparker.io")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "<figure>") {
		t.Errorf("expected figure wrap, got:\n%s", got)
	}
	if strings.Contains(got, "<figcaption>") {
		t.Errorf("empty alt should not produce figcaption:\n%s", got)
	}
}

func TestRenderMarkdown_InlineImageNotWrappedInFigure(t *testing.T) {
	// Image embedded in a sentence — not a standalone paragraph.
	body := "See ![inline](/images/post/foo.png) here.\n"
	got, err := build.RenderMarkdown(body, "https://aparker.io")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "<figure>") {
		t.Errorf("inline image should NOT be wrapped:\n%s", got)
	}
}

func TestRenderMarkdown_DetailsSummaryPassesThrough(t *testing.T) {
	body := "<details><summary>Click to expand</summary>\n\nHidden content here.\n\n</details>\n"
	got, err := build.RenderMarkdown(body, "https://aparker.io")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<details>", "<summary>Click to expand</summary>", "Hidden content"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestRenderMarkdown_PerformsZeroNetworkIO(t *testing.T) {
	// Sanity assertion via the bsky transform doing pure URL parsing.
	// If we ever wire goldmark extensions that fetch (oembed, etc.),
	// this comment + a CI net-block flag should catch it.
	body := `<blockquote data-bluesky-uri="at://did:plc:abc/app.bsky.feed.post/xyz">x</blockquote>`
	if _, err := build.RenderMarkdown(body, "https://aparker.io"); err != nil {
		t.Fatal(err)
	}
}

func TestRenderMarkdown_LeavesProtocolRelativeURLAlone(t *testing.T) {
	got, err := build.RenderMarkdown(`<a href="//cdn.example.org/x.js">x</a> and [home](/)`+"\n", "https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `href="//cdn.example.org/x.js"`) {
		t.Errorf("protocol-relative URL was rewritten: %s", got)
	}
	if !strings.Contains(got, `href="https://example.com/"`) {
		t.Errorf("site-root link not resolved: %s", got)
	}
}
