package publish_test

import (
	"strings"
	"testing"

	"aparker.io/fair/internal/publish"
)

func TestValidateSlug_Acceptable(t *testing.T) {
	cases := []string{
		"hello",
		"wide-events-and-personal-software",
		"2025-04-17-my-post",
		"a",
		"abc123",
		"123-numeric-prefix",
	}
	for _, s := range cases {
		t.Run(s, func(t *testing.T) {
			if err := publish.ValidateSlug(s); err != nil {
				t.Errorf("got %v, want nil", err)
			}
		})
	}
}

func TestValidateSlug_RejectsBadGrammar(t *testing.T) {
	cases := []struct {
		slug string
		why  string
	}{
		{"", "empty"},
		{"Hello", "uppercase"},
		{"hello world", "space"},
		{"hello/path", "slash"},
		{"hello.md", "dot"},
		{"-leading-dash", "leading dash"},
		{"trailing-dash-", "trailing dash"},
		{"double--dash", "double dash"},
		{"emoji-🚀", "non-ascii"},
		{"under_score", "underscore"},
	}
	for _, tc := range cases {
		t.Run(tc.why, func(t *testing.T) {
			err := publish.ValidateSlug(tc.slug)
			if err == nil {
				t.Errorf("expected error for %q (%s)", tc.slug, tc.why)
			}
		})
	}
}

func TestValidateSlug_RejectsReservedNames(t *testing.T) {
	for _, name := range []string{"self", "index", "feed", "sitemap", ".well-known"} {
		t.Run(name, func(t *testing.T) {
			err := publish.ValidateSlug(name)
			if err == nil {
				t.Fatalf("expected error for reserved name %q", name)
			}
			if !strings.Contains(strings.ToLower(err.Error()), "reserved") {
				t.Errorf("error %q should mention 'reserved'", err.Error())
			}
		})
	}
}

func TestSlugFromPath_DerivesFromDir(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"/repo/blog-posts/wide-events/index.md", "wide-events"},
		{"/repo/blog-posts/small-software/index.md", "small-software"},
		{"./hello-world/index.md", "hello-world"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			got, err := publish.SlugFromPath(tc.path)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSlugFromPath_RejectsTopLevelMarkdown(t *testing.T) {
	// blog-posts/foo.md (no directory) — slug ambiguous, force user to put it in a dir
	_, err := publish.SlugFromPath("/repo/blog-posts/foo.md")
	if err == nil {
		t.Fatal("expected error for top-level markdown")
	}
}
