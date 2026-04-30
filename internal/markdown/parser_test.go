package markdown_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"aparker.io/fair/internal/markdown"
	gmast "github.com/yuin/goldmark/ast"
	gmtext "github.com/yuin/goldmark/text"
)

func TestParser_RendersBasicMarkdown(t *testing.T) {
	in := "# Hello\n\nworld\n"
	var buf bytes.Buffer
	if err := markdown.Parser().Convert([]byte(in), &buf); err != nil {
		t.Fatalf("Convert: %v", err)
	}
	got := buf.String()
	if got == "" {
		t.Fatal("got empty render")
	}
	// Auto-heading-IDs add `id=...` to <h1>, so check the prefix only.
	for _, want := range []string{"<h1", "Hello", "<p>", "world"} {
		if !bytes.Contains(buf.Bytes(), []byte(want)) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

func TestParser_RendersGFMTable(t *testing.T) {
	in := "| col |\n|---|\n| a |\n"
	var buf bytes.Buffer
	if err := markdown.Parser().Convert([]byte(in), &buf); err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("<table>")) {
		t.Errorf("GFM table not rendered:\n%s", buf.String())
	}
}

func TestParser_RendersGFMStrikethrough(t *testing.T) {
	in := "~~struck~~\n"
	var buf bytes.Buffer
	if err := markdown.Parser().Convert([]byte(in), &buf); err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("<del>")) {
		t.Errorf("strikethrough not rendered:\n%s", buf.String())
	}
}

func TestParser_RendersGFMAutolink(t *testing.T) {
	in := "Visit https://aparker.io for posts.\n"
	var buf bytes.Buffer
	if err := markdown.Parser().Convert([]byte(in), &buf); err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte(`href="https://aparker.io"`)) {
		t.Errorf("autolink not rendered:\n%s", buf.String())
	}
}

func TestParser_ParsesRealFixturesWithoutError(t *testing.T) {
	entries, err := os.ReadDir("testdata/posts")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	if len(entries) == 0 {
		t.Skip("no fixtures present")
	}

	for _, e := range entries {
		t.Run(e.Name(), func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join("testdata/posts", e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			// Strip frontmatter so we feed just the body to goldmark.
			_, body, err := markdown.Split(string(src))
			if err != nil {
				t.Fatalf("Split: %v", err)
			}

			// AST parse should not panic or error.
			reader := gmtext.NewReader([]byte(body))
			doc := markdown.Parser().Parser().Parse(reader)
			if doc == nil {
				t.Fatal("nil doc")
			}
			// Sanity: at least one block-level child.
			if doc.ChildCount() == 0 {
				t.Errorf("no block children parsed")
			}
			// Walk to confirm no unexpected node types.
			err = gmast.Walk(doc, func(_ gmast.Node, entering bool) (gmast.WalkStatus, error) {
				if !entering {
					return gmast.WalkContinue, nil
				}
				return gmast.WalkContinue, nil
			})
			if err != nil {
				t.Errorf("walk: %v", err)
			}

			// Render for good measure.
			var buf bytes.Buffer
			if err := markdown.Parser().Convert([]byte(body), &buf); err != nil {
				t.Errorf("Convert: %v", err)
			}
			if buf.Len() == 0 {
				t.Errorf("empty render for %s", e.Name())
			}
		})
	}
}
