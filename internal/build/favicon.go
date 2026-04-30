package build

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// fetchAndSaveFavicon pulls the publication's icon blob from the PDS
// and writes it to <tmpOut>/favicon.<ext>. Returns the public path
// ("/favicon.png") suitable for <link rel="icon"> hrefs.
//
// The icon is the publication record's `icon` typed-blob field.
// Resolution: com.atproto.sync.getBlob?did=<did>&cid=<cid>.
//
// Failures here are non-fatal — the build flow logs and continues
// without a favicon.
func fetchAndSaveFavicon(ctx context.Context, hc *http.Client, cfg Config, icon *blobRef, tmpOut string) (string, error) {
	if icon == nil || icon.Ref.Link == "" {
		return "", errors.New("icon ref empty")
	}
	u := fmt.Sprintf("%s/xrpc/com.atproto.sync.getBlob?did=%s&cid=%s",
		strings.TrimRight(cfg.PDSURL, "/"), cfg.DID, icon.Ref.Link)
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return "", err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("getBlob status %d", resp.StatusCode)
	}

	ext := extensionForMime(icon.MimeType)
	if ext == "" {
		ext = ".png" // safe default
	}
	dest := filepath.Join(tmpOut, "favicon"+ext)
	out, err := os.Create(dest)
	if err != nil {
		return "", err
	}
	defer out.Close()
	if _, err := io.Copy(out, resp.Body); err != nil {
		return "", err
	}
	return "/favicon" + ext, nil
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
