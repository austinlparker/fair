// Package images generates resized variants of source images so the
// build flow can emit srcset/sizes attributes for responsive images.
//
// Without CSS, srcset/sizes alone doesn't make images visually scale
// to the viewport — the build flow pairs this with one inline `<style>`
// rule (`img{max-width:100%;height:auto}`) to actually constrain
// rendered size. Variants exist so that a phone fetches a 480-pixel
// image instead of a 2000-pixel one.
package images

import (
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/image/draw" //nolint:goimports
	// Force decoder registration for jpeg/png on image.Decode below.
	_ "image/jpeg"
	_ "image/png"
)

// VariantWidths is the set of pixel widths variants are generated at.
// Variants are only generated when the source is wider than the target
// (no upscaling). A copy of the original is always retained as the
// canonical fallback.
var VariantWidths = []int{480, 960, 1440, 1920}

// Variant is one resized image written under the same directory as
// the source.
type Variant struct {
	Width  int    // pixel width of this variant
	Path   string // absolute path on disk
	Height int    // pixel height (proportional to source)
}

// Decoded carries the source image plus its dimensions, returned by
// Decode. Build code uses Width/Height to populate <img width height>
// for layout stability.
type Decoded struct {
	Img    image.Image
	Width  int
	Height int
	Format string // "jpeg" or "png"
}

// Decode opens path and decodes it as JPEG or PNG. Other formats return
// an error so callers can skip them gracefully.
func Decode(path string) (*Decoded, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, format, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	if format != "jpeg" && format != "png" {
		return nil, fmt.Errorf("unsupported image format %q for %s", format, path)
	}
	b := img.Bounds()
	return &Decoded{Img: img, Width: b.Dx(), Height: b.Dy(), Format: format}, nil
}

// GenerateVariants resizes srcPath into the standard set of widths and
// writes each as <basename-without-ext>-<width>.<ext> next to the
// source. Widths >= source width are skipped (no upscaling). Variants
// already on disk with a matching mtime are skipped (idempotent).
//
// Returns the variants that exist on disk after this call (including
// pre-existing ones), each tagged with its actual width and height.
//
// Unsupported or undecodable images (gif, webp, svg, corrupt, fake test
// fixtures, etc.) return nil, nil — caller treats the image as non-
// responsive and emits a plain <img>. Decode errors are intentionally
// swallowed here because the publish flow should not fail just because
// one image happens to be in a format we can't resize; the rendered
// site still works, just without srcset for that one image.
func GenerateVariants(srcPath string) ([]Variant, error) {
	dec, err := Decode(srcPath)
	if err != nil {
		return nil, nil
	}

	dir := filepath.Dir(srcPath)
	base := filepath.Base(srcPath)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)

	var out []Variant
	for _, w := range VariantWidths {
		if w >= dec.Width {
			continue
		}
		variantPath := filepath.Join(dir, fmt.Sprintf("%s-%d%s", stem, w, ext))
		h := dec.Height * w / dec.Width

		// Skip if already on disk and newer than the source.
		if upToDate(srcPath, variantPath) {
			out = append(out, Variant{Width: w, Path: variantPath, Height: h})
			continue
		}

		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		draw.CatmullRom.Scale(dst, dst.Bounds(), dec.Img, dec.Img.Bounds(), draw.Over, nil)

		if err := writeImage(variantPath, dst, dec.Format); err != nil {
			return nil, err
		}
		out = append(out, Variant{Width: w, Path: variantPath, Height: h})
	}
	return out, nil
}

// upToDate reports whether dst exists and is at least as new as src.
func upToDate(src, dst string) bool {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return false
	}
	dstInfo, err := os.Stat(dst)
	if err != nil {
		return false
	}
	return !dstInfo.ModTime().Before(srcInfo.ModTime())
}

// writeImage encodes img to path in the given format. Caller controls
// path/ext alignment — this function trusts what it's given.
func writeImage(path string, img image.Image, format string) error {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp) }()

	switch format {
	case "jpeg":
		if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 85}); err != nil {
			f.Close()
			return err
		}
	case "png":
		enc := &png.Encoder{CompressionLevel: png.BestCompression}
		if err := enc.Encode(f, img); err != nil {
			f.Close()
			return err
		}
	default:
		f.Close()
		return fmt.Errorf("writeImage: unsupported format %q", format)
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// FindVariants returns variants present on disk for srcPath, derived
// from the canonical naming convention. Used by the build flow to
// build srcset attributes without re-running the (slow) decode/resize
// path. Returns the source's intrinsic dimensions for width/height
// attributes.
func FindVariants(srcPath string) (sourceWidth, sourceHeight int, variants []Variant, err error) {
	dec, err := Decode(srcPath)
	if err != nil {
		return 0, 0, nil, err
	}
	dir := filepath.Dir(srcPath)
	base := filepath.Base(srcPath)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)

	for _, w := range VariantWidths {
		if w >= dec.Width {
			continue
		}
		variantPath := filepath.Join(dir, fmt.Sprintf("%s-%d%s", stem, w, ext))
		if _, err := os.Stat(variantPath); err != nil {
			continue
		}
		h := dec.Height * w / dec.Width
		variants = append(variants, Variant{Width: w, Path: variantPath, Height: h})
	}
	return dec.Width, dec.Height, variants, nil
}
