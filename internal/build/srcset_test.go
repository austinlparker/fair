package build_test

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aparker.io/fair/internal/build"
	"aparker.io/fair/internal/images"
)

func makePNG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 200, A: 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode: %v", err)
	}
}

func TestEnrichResponsiveImages_AddsSrcsetWhenVariantsExist(t *testing.T) {
	mirror := t.TempDir()
	slugDir := filepath.Join(mirror, "post-a")
	if err := os.MkdirAll(slugDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	src := filepath.Join(slugDir, "fig.png")
	makePNG(t, src, 1600, 900)
	if _, err := images.GenerateVariants(src); err != nil {
		t.Fatalf("GenerateVariants: %v", err)
	}

	html := `<p>before <img src="https://example.com/images/post-a/fig.png" alt="caption"> after</p>`
	got := build.EnrichResponsiveImages(html, mirror, "https://example.com")

	for _, want := range []string{
		`srcset="`,
		`sizes="100vw"`,
		`width="1600"`,
		`height="900"`,
		`fig-480.png 480w`,
		`fig-960.png 960w`,
		`fig-1440.png 1440w`,
		`https://example.com/images/post-a/fig.png 1600w`,
		`alt="caption"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestEnrichResponsiveImages_LeavesImageAloneWhenNoVariants(t *testing.T) {
	mirror := t.TempDir()
	html := `<img src="https://example.com/images/missing/x.png" alt="x">`
	got := build.EnrichResponsiveImages(html, mirror, "https://example.com")
	if got != html {
		t.Errorf("expected unchanged, got:\n%s", got)
	}
}

func TestEnrichResponsiveImages_NoOpWhenMirrorEmpty(t *testing.T) {
	html := `<img src="https://example.com/images/x/y.png" alt="x">`
	got := build.EnrichResponsiveImages(html, "", "https://example.com")
	if got != html {
		t.Errorf("expected unchanged when mirror empty, got:\n%s", got)
	}
}

func TestEnrichResponsiveImages_LeavesAbsoluteForeignSrcAlone(t *testing.T) {
	mirror := t.TempDir()
	html := `<img src="https://other.example.com/foo.png">`
	got := build.EnrichResponsiveImages(html, mirror, "https://example.com")
	if got != html {
		t.Errorf("expected foreign-host image unchanged, got:\n%s", got)
	}
}
