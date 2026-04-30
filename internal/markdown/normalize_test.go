package markdown_test

import (
	"strings"
	"testing"

	"aparker.io/fair/internal/markdown"
)

func TestNormalize_StripsBOM(t *testing.T) {
	bom := "\ufeff"
	got := markdown.Normalize(bom + "hello\n")
	if strings.HasPrefix(got, bom) {
		t.Errorf("BOM not stripped: %q", got)
	}
	if got != "hello\n" {
		t.Errorf("got %q, want %q", got, "hello\n")
	}
}

func TestNormalize_ConvertsCRLFToLF(t *testing.T) {
	got := markdown.Normalize("a\r\nb\r\nc\r\n")
	want := "a\nb\nc\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNormalize_ConvertsBareCRToLF(t *testing.T) {
	// Old-Mac line endings, rare but real
	got := markdown.Normalize("a\rb\rc\r")
	want := "a\nb\nc\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNormalize_EnsuresTrailingNewline(t *testing.T) {
	got := markdown.Normalize("hello")
	if got != "hello\n" {
		t.Errorf("got %q, want trailing newline", got)
	}
}

func TestNormalize_NoDoubleTrailingNewline(t *testing.T) {
	got := markdown.Normalize("hello\n\n\n")
	// Trailing newlines collapsed to a single one.
	if got != "hello\n" {
		t.Errorf("got %q, want %q", got, "hello\n")
	}
}

func TestNormalize_PreservesInternalBlankLines(t *testing.T) {
	got := markdown.Normalize("a\n\nb\n")
	if got != "a\n\nb\n" {
		t.Errorf("got %q, blank line was eaten", got)
	}
}

func TestNormalize_Idempotent(t *testing.T) {
	cases := []string{
		"",
		"\n",
		"hello\n",
		"\ufeffhello\r\n",
		"a\r\n\r\nb\r\n",
	}
	for _, c := range cases {
		once := markdown.Normalize(c)
		twice := markdown.Normalize(once)
		if once != twice {
			t.Errorf("not idempotent: input %q -> %q -> %q", c, once, twice)
		}
	}
}

func TestNormalize_EmptyString(t *testing.T) {
	// Empty in, empty out — don't synthesize a newline from nothing
	got := markdown.Normalize("")
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}
