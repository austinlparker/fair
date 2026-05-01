package publish

import (
	"context"
	"fmt"
	"strings"
	"time"

	"aparker.io/fair/internal/atproto"
)

// AnnounceInput is the data the announce flow needs to create an
// app.bsky.feed.post. Pulled out of the main publish flow so it can be
// unit-tested without touching the full document pipeline.
type AnnounceInput struct {
	Title       string             // post title — used as the embed card title
	Description string             // post description — used as the embed card description
	URL         string             // canonical absolute URL of the article on the rendered site
	CoverBlob   *atproto.BlobRef   // optional; used as the embed card thumbnail
}

// maxBskyPostText is the Bluesky post graphemes limit. We measure in
// runes here, which is a slight under-count for posts containing emoji
// composed of multiple codepoints, but conservative — undershooting
// this constraint is fine, overshooting causes the PDS to reject.
const maxBskyPostText = 300

// Announce creates an app.bsky.feed.post record on the user's repo
// announcing a published article, and returns the new post's at-uri
// and CID. The result becomes the StrongRef stored as bskyPostRef on
// the document so readers can pull replies as comments.
//
// The announce post body is "<title>\n\n<URL>" with a link card embed
// pointing at the article URL. The embed reuses CoverBlob as its thumb
// when one is supplied.
func Announce(ctx context.Context, client *atproto.Client, in AnnounceInput) (uri, cid string, err error) {
	text := buildAnnounceText(in.Title, in.URL)

	external := map[string]any{
		"uri":         in.URL,
		"title":       in.Title,
		"description": in.Description,
	}
	if in.CoverBlob != nil {
		external["thumb"] = in.CoverBlob
	}

	record := map[string]any{
		"$type":     "app.bsky.feed.post",
		"text":      text,
		"createdAt": time.Now().UTC().Format(time.RFC3339),
		"langs":     []string{"en"},
		"embed": map[string]any{
			"$type":    "app.bsky.embed.external",
			"external": external,
		},
	}

	ref, err := client.CreateRecord(ctx, "app.bsky.feed.post", record)
	if err != nil {
		return "", "", fmt.Errorf("create app.bsky.feed.post: %w", err)
	}
	return ref.URI, ref.CID, nil
}

// buildAnnounceText composes the post body. Format:
//
//	<title>
//	<URL>
//
// If the title alone (plus newline plus URL) exceeds 300 runes we
// truncate the title and add an ellipsis. URL is preserved verbatim
// since shortening it would break the embed card resolution.
func buildAnnounceText(title, articleURL string) string {
	title = strings.TrimSpace(title)
	urlPart := "\n\n" + articleURL
	maxTitle := maxBskyPostText - len([]rune(urlPart))
	if r := []rune(title); len(r) > maxTitle {
		title = string(r[:maxTitle-1]) + "…"
	}
	return title + urlPart
}
