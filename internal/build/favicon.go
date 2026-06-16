package build

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Blob downloads are capped so a compromised or malicious PDS can't
// stream gigabytes for a "tiny" image and exhaust disk/memory on the
// build host. Real favicons are <100KB; covers are spec-capped at ~1MB
// at publish time, with headroom here for larger blobs already on the PDS.
const (
	maxFaviconBytes = 5 << 20  // 5 MiB
	maxCoverBytes   = 10 << 20 // 10 MiB
)

// fetchBlobToFile downloads the blob named by ref via
// com.atproto.sync.getBlob?did=<did>&cid=<cid> and writes it to dest,
// capping the body at maxBytes. Parent directories are created as needed.
func fetchBlobToFile(ctx context.Context, hc *http.Client, pdsURL, did string, ref *blobRef, dest string, maxBytes int64) error {
	if ref == nil || ref.Ref.Link == "" {
		return errors.New("blob ref empty")
	}
	q := url.Values{}
	q.Set("did", did)
	q.Set("cid", ref.Ref.Link)
	u := strings.TrimRight(pdsURL, "/") + "/xrpc/com.atproto.sync.getBlob?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("getBlob status %d", resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, io.LimitReader(resp.Body, maxBytes)); err != nil {
		return err
	}
	return nil
}

// fetchAndSaveFavicon pulls the publication's icon blob from the PDS
// and writes it to <tmpOut>/favicon.<ext>. Returns the public path
// ("/favicon.png") suitable for <link rel="icon"> hrefs.
//
// The icon is the publication record's `icon` typed-blob field.
// Failures here are non-fatal — the build flow logs and continues
// without a favicon.
func fetchAndSaveFavicon(ctx context.Context, hc *http.Client, cfg Config, icon *blobRef, tmpOut string) (string, error) {
	if icon == nil || icon.Ref.Link == "" {
		return "", errors.New("icon ref empty")
	}
	ext := extensionForMime(icon.MimeType)
	if ext == "" {
		ext = ".png" // safe default
	}
	if err := fetchBlobToFile(ctx, hc, cfg.PDSURL, cfg.DID, icon, filepath.Join(tmpOut, "favicon"+ext), maxFaviconBytes); err != nil {
		return "", err
	}
	return "/favicon" + ext, nil
}

// fetchAndSaveCover pulls a document's coverImage blob from the PDS and
// writes it to <tmpOut>/covers/<slug>.<ext>. Returns the public path
// ("/covers/<slug>.png") used for og:image, twitter:image, and the
// post-header <img>. Covers live under /covers/ rather than /images/ to
// stay clear of the inline-image mirror copied into /images/.
//
// Failures here are non-fatal — the build flow logs and renders the
// post without a cover.
func fetchAndSaveCover(ctx context.Context, hc *http.Client, cfg Config, cover *blobRef, slug, tmpOut string) (string, error) {
	if cover == nil || cover.Ref.Link == "" {
		return "", errors.New("cover ref empty")
	}
	ext := extensionForMime(cover.MimeType)
	if ext == "" {
		ext = ".png" // safe default
	}
	if err := fetchBlobToFile(ctx, hc, cfg.PDSURL, cfg.DID, cover, filepath.Join(tmpOut, "covers", slug+ext), maxCoverBytes); err != nil {
		return "", err
	}
	return "/covers/" + slug + ext, nil
}

// extensionForMime maps an image MIME type to a conventional file
// extension. Handles the common cases; falls back to "" for callers
// to substitute a default.
func extensionForMime(mt string) string {
	exts, _ := mime.ExtensionsByType(mt)
	for _, e := range exts {
		// Prefer canonical short forms (ExtensionsByType returns several).
		switch e {
		case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".ico":
			return e
		}
	}
	return ""
}
