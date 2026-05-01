package images_test

import (
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"aparker.io/fair/internal/images"
)

// makeTestPNG writes a solid-color PNG of width×height and returns the path.
func makeTestPNG(t *testing.T, dir, name string, width, height int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 255), G: uint8(y % 255), B: 128, A: 255})
		}
	}
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode %s: %v", path, err)
	}
	return path
}

// makeTestJPEG writes a solid-color JPEG of width×height and returns the path.
func makeTestJPEG(t *testing.T, dir, name string, width, height int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatalf("encode %s: %v", path, err)
	}
	return path
}

func TestGenerateVariants_PNG_GeneratesSmallerWidthsOnly(t *testing.T) {
	dir := t.TempDir()
	src := makeTestPNG(t, dir, "wide.png", 1600, 1200)

	got, err := images.GenerateVariants(src)
	if err != nil {
		t.Fatalf("GenerateVariants: %v", err)
	}

	// Source is 1600 wide; expect variants at 480, 960, 1440 (skip 1920).
	wantWidths := []int{480, 960, 1440}
	if len(got) != len(wantWidths) {
		t.Fatalf("variant count: got %d, want %d (%v)", len(got), len(wantWidths), got)
	}
	for i, v := range got {
		if v.Width != wantWidths[i] {
			t.Errorf("variant %d width: got %d, want %d", i, v.Width, wantWidths[i])
		}
		// Height should be proportional: 1200/1600 = 0.75
		wantH := wantWidths[i] * 1200 / 1600
		if v.Height != wantH {
			t.Errorf("variant %d height: got %d, want %d", i, v.Height, wantH)
		}
		if _, err := os.Stat(v.Path); err != nil {
			t.Errorf("variant %d path %s: %v", i, v.Path, err)
		}
	}
}

func TestGenerateVariants_JPEG(t *testing.T) {
	dir := t.TempDir()
	src := makeTestJPEG(t, dir, "photo.jpg", 1000, 750)

	got, err := images.GenerateVariants(src)
	if err != nil {
		t.Fatalf("GenerateVariants: %v", err)
	}
	// 1000 wide → variants at 480, 960 only.
	if len(got) != 2 {
		t.Fatalf("variant count: got %d, want 2 (%v)", len(got), got)
	}
	if got[0].Width != 480 || got[1].Width != 960 {
		t.Errorf("widths: got %v, want [480 960]", []int{got[0].Width, got[1].Width})
	}
	// Variant filenames carry the width and original extension.
	wantPaths := []string{
		filepath.Join(dir, "photo-480.jpg"),
		filepath.Join(dir, "photo-960.jpg"),
	}
	for i, v := range got {
		if v.Path != wantPaths[i] {
			t.Errorf("variant %d path: got %s, want %s", i, v.Path, wantPaths[i])
		}
	}
}

func TestGenerateVariants_NoUpscale(t *testing.T) {
	dir := t.TempDir()
	src := makeTestPNG(t, dir, "small.png", 300, 200)

	got, err := images.GenerateVariants(src)
	if err != nil {
		t.Fatalf("GenerateVariants: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("source smaller than smallest variant should produce nothing, got %v", got)
	}
}

func TestGenerateVariants_Idempotent(t *testing.T) {
	dir := t.TempDir()
	src := makeTestPNG(t, dir, "img.png", 1000, 800)

	first, err := images.GenerateVariants(src)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	if len(first) == 0 {
		t.Fatal("expected variants on first run")
	}
	mtime0 := mustModTime(t, first[0].Path)

	// Second call should not rewrite (mtime unchanged).
	second, err := images.GenerateVariants(src)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if len(second) != len(first) {
		t.Errorf("variant count changed across runs: %d → %d", len(first), len(second))
	}
	mtime1 := mustModTime(t, first[0].Path)
	if !mtime0.Equal(mtime1) {
		t.Errorf("variant mtime changed on idempotent re-run: %v → %v", mtime0, mtime1)
	}
}

func TestFindVariants_AfterGenerate(t *testing.T) {
	dir := t.TempDir()
	src := makeTestPNG(t, dir, "doc.png", 1500, 1000)
	if _, err := images.GenerateVariants(src); err != nil {
		t.Fatalf("GenerateVariants: %v", err)
	}

	w, h, vs, err := images.FindVariants(src)
	if err != nil {
		t.Fatalf("FindVariants: %v", err)
	}
	if w != 1500 || h != 1000 {
		t.Errorf("source dims: got %dx%d, want 1500x1000", w, h)
	}
	// 1500-wide source: variants generated at 480, 960, 1440 (1920 skipped).
	if len(vs) != 3 {
		t.Errorf("variant count: got %d, want 3 (480, 960, 1440)", len(vs))
	}
}

func mustModTime(t *testing.T, path string) (mtime interface{ Equal(any) bool }) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return modTime{info.ModTime().UnixNano()}
}

type modTime struct{ ns int64 }

func (m modTime) Equal(other any) bool {
	o, ok := other.(modTime)
	return ok && m.ns == o.ns
}
