package publish_test

import (
	"strings"
	"testing"

	"aparker.io/fair/internal/publish"
)

func TestExtractDescription_FirstParagraph(t *testing.T) {
	body := "This is the first paragraph.\n\nSecond paragraph not used.\n"
	got := publish.ExtractDescription(body)
	if got != "This is the first paragraph." {
		t.Errorf("got %q", got)
	}
}

func TestExtractDescription_SkipsHeadingThenFindsParagraph(t *testing.T) {
	body := "# Heading\n\nThe actual first paragraph here.\n"
	got := publish.ExtractDescription(body)
	if got != "The actual first paragraph here." {
		t.Errorf("got %q", got)
	}
}

func TestExtractDescription_SkipsImageThenFindsParagraph(t *testing.T) {
	body := "![cover](images/foo.png)\n\nSecond block — first paragraph for description.\n"
	got := publish.ExtractDescription(body)
	if got != "Second block — first paragraph for description." {
		t.Errorf("got %q", got)
	}
}

func TestExtractDescription_NoParagraph_ReturnsEmpty(t *testing.T) {
	body := "# Heading only\n\n## Another heading\n"
	got := publish.ExtractDescription(body)
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestExtractDescription_GivesUpAfterFirstFiveBlocks(t *testing.T) {
	// 5+ non-paragraph blocks, then a paragraph — description should be empty
	body := "# h1\n\n## h2\n\n### h3\n\n#### h4\n\n##### h5\n\nFinally text.\n"
	got := publish.ExtractDescription(body)
	if got != "" {
		t.Errorf("got %q, want empty (gave up after 5 blocks)", got)
	}
}

func TestExtractDescription_TruncatedAtGraphemeBoundary(t *testing.T) {
	long := strings.Repeat("a", 4000) // exceeds 3000-grapheme cap
	body := long + "\n"
	got := publish.ExtractDescription(body)
	// uniseg cluster count, not byte count
	if uniLen := publish.GraphemeCount(got); uniLen > 3000 {
		t.Errorf("description not truncated: %d graphemes", uniLen)
	}
}

func TestExtractTextContent_StripsCodeFences(t *testing.T) {
	body := "Hello.\n\n```go\nfunc main() {}\n```\n\nWorld.\n"
	got := publish.ExtractTextContent(body)
	if strings.Contains(got, "func main") {
		t.Errorf("code fence text leaked: %q", got)
	}
	if !strings.Contains(got, "Hello") || !strings.Contains(got, "World") {
		t.Errorf("paragraphs missing: %q", got)
	}
}

func TestExtractTextContent_KeepsAltText(t *testing.T) {
	body := "Look: ![alt text](foo.png)\n"
	got := publish.ExtractTextContent(body)
	if !strings.Contains(got, "alt text") {
		t.Errorf("alt text dropped: %q", got)
	}
}

func TestExtractTextContent_NormalizesWhitespace(t *testing.T) {
	body := "Line 1\n\n\n\nLine 2.\n"
	got := publish.ExtractTextContent(body)
	// No multiple consecutive newlines
	if strings.Contains(got, "\n\n\n") {
		t.Errorf("excessive whitespace: %q", got)
	}
}

func TestExtractTextContent_TruncatedAt30000Graphemes(t *testing.T) {
	long := strings.Repeat("hello world. ", 5000) // ~65000 chars
	got := publish.ExtractTextContent(long)
	if c := publish.GraphemeCount(got); c > 30000 {
		t.Errorf("not truncated: %d graphemes", c)
	}
}
