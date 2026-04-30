package publish_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aparker.io/fair/internal/publish"
)

func writePNG(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRewriteImages_RelativeImageCopiedAndRewritten(t *testing.T) {
	sourceDir := t.TempDir()
	mirrorDir := t.TempDir()
	writePNG(t, filepath.Join(sourceDir, "images/foo.png"), "fake-png")

	body := "Look: ![alt](images/foo.png) end\n"
	got, err := publish.RewriteImages(body, "wide-events", sourceDir, mirrorDir)
	if err != nil {
		t.Fatalf("RewriteImages: %v", err)
	}
	if !strings.Contains(got, "/images/wide-events/foo.png") {
		t.Errorf("body not rewritten:\n%s", got)
	}
	if strings.Contains(got, "images/foo.png") && !strings.Contains(got, "/images/wide-events/foo.png") {
		t.Errorf("original path not replaced:\n%s", got)
	}

	mirrored := filepath.Join(mirrorDir, "wide-events", "foo.png")
	if _, err := os.Stat(mirrored); err != nil {
		t.Errorf("expected mirror at %s: %v", mirrored, err)
	}
}

func TestRewriteImages_AbsoluteURLsUntouched(t *testing.T) {
	body := "External: ![alt](https://example.com/foo.png)\n"
	got, err := publish.RewriteImages(body, "slug", t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "https://example.com/foo.png") {
		t.Errorf("absolute URL changed:\n%s", got)
	}
}

func TestRewriteImages_MissingSourceFile_Errors(t *testing.T) {
	body := "![alt](missing/nope.png)\n"
	_, err := publish.RewriteImages(body, "slug", t.TempDir(), t.TempDir())
	if err == nil {
		t.Fatal("expected error for missing image")
	}
	if !strings.Contains(err.Error(), "missing/nope.png") {
		t.Errorf("error %q should mention missing path", err.Error())
	}
}

func TestRewriteImages_MultipleImages_AllCopied(t *testing.T) {
	sourceDir := t.TempDir()
	mirrorDir := t.TempDir()
	writePNG(t, filepath.Join(sourceDir, "images/a.png"), "a")
	writePNG(t, filepath.Join(sourceDir, "images/b.png"), "b")

	body := "![](images/a.png) and ![](images/b.png)\n"
	got, err := publish.RewriteImages(body, "post", sourceDir, mirrorDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/images/post/a.png", "/images/post/b.png"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in:\n%s", want, got)
		}
	}
	for _, want := range []string{"a.png", "b.png"} {
		if _, err := os.Stat(filepath.Join(mirrorDir, "post", want)); err != nil {
			t.Errorf("missing mirror file %s: %v", want, err)
		}
	}
}

func TestRewriteImages_DuplicateReference_OneCopy(t *testing.T) {
	sourceDir := t.TempDir()
	mirrorDir := t.TempDir()
	writePNG(t, filepath.Join(sourceDir, "images/foo.png"), "x")

	body := "![](images/foo.png) and again ![](images/foo.png)\n"
	got, err := publish.RewriteImages(body, "post", sourceDir, mirrorDir)
	if err != nil {
		t.Fatal(err)
	}
	count := strings.Count(got, "/images/post/foo.png")
	if count != 2 {
		t.Errorf("expected 2 rewrites, got %d:\n%s", count, got)
	}
	// Only one mirror file
	entries, _ := os.ReadDir(filepath.Join(mirrorDir, "post"))
	if len(entries) != 1 {
		t.Errorf("expected 1 mirrored file, got %d", len(entries))
	}
}

func TestRewriteImages_Idempotent(t *testing.T) {
	sourceDir := t.TempDir()
	mirrorDir := t.TempDir()
	writePNG(t, filepath.Join(sourceDir, "images/foo.png"), "content")

	body := "![](images/foo.png)\n"
	first, err := publish.RewriteImages(body, "post", sourceDir, mirrorDir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := publish.RewriteImages(body, "post", sourceDir, mirrorDir)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if first != second {
		t.Errorf("non-idempotent:\nfirst:  %s\nsecond: %s", first, second)
	}
}
