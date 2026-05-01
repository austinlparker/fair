package publish

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"aparker.io/fair/internal/atproto"
)

// publicBskyAppView is the unauthenticated AppView the publish flow
// queries to resolve a Bluesky post at-uri to its current CID. We use
// it for one specific job: building bskyPostRef StrongRefs at publish
// time. The endpoint accepts unauthenticated reads of public records.
const publicBskyAppView = "https://public.api.bsky.app"

// bskyAppViewClient is the HTTP client used for resolveBskyPostCID.
// Short timeout — this is one request per published post that uses
// bsky_post; we don't need to be patient.
var bskyAppViewClient = &http.Client{Timeout: 10 * time.Second}

// resolveBskyPostCID parses an at-uri of the form
//   at://<did-or-handle>/app.bsky.feed.post/<rkey>
// and fetches the matching record's CID. When the at-uri references
// the publishing user's own repo, we use the authenticated client's
// GetRecord (which already trusts the user's PDS — important for
// sandbox/self-hosted setups where the public AppView can't see the
// post). Otherwise we hit the public Bluesky AppView, which works for
// any federated bsky post.
//
// site.standard.document.bskyPostRef is typed as a strongRef per the
// canonical lexicon at
// https://tangled.sh/standard.site/lexicons/blob/main/src/lexicons/site.standard.document.ts
func resolveBskyPostCID(ctx context.Context, atURI string, client *atproto.Client) (cid string, err error) {
	repo, collection, rkey, err := parseATURI(atURI)
	if err != nil {
		return "", fmt.Errorf("bsky_post %q: %w", atURI, err)
	}
	if collection != "app.bsky.feed.post" {
		return "", fmt.Errorf("bsky_post %q: expected app.bsky.feed.post collection, got %s", atURI, collection)
	}

	// Own-repo lookup uses the authenticated client (which trusts the
	// session's PDS — covers sandbox/mkcert).
	if client != nil && repo == client.Session().DID {
		out, err := client.GetRecord(ctx, collection, rkey)
		if err != nil {
			return "", fmt.Errorf("getRecord on own PDS for %s: %w", atURI, err)
		}
		if out.CID == "" {
			return "", fmt.Errorf("own PDS returned no cid for %s", atURI)
		}
		return out.CID, nil
	}

	// External-repo lookup goes to the public Bluesky AppView. Default
	// HTTP client is fine here — public.api.bsky.app uses a normal
	// publicly-trusted certificate.
	q := url.Values{}
	q.Set("repo", repo)
	q.Set("collection", collection)
	q.Set("rkey", rkey)
	endpoint := publicBskyAppView + "/xrpc/com.atproto.repo.getRecord?" + q.Encode()
	body, err := tryGetRecord(ctx, endpoint)
	if err != nil {
		return "", fmt.Errorf("public AppView for %s: %w", atURI, err)
	}
	if body.CID == "" {
		return "", fmt.Errorf("public AppView returned no cid for %s", atURI)
	}
	return body.CID, nil
}

// tryGetRecord performs one getRecord lookup and decodes the URI+CID
// envelope. Caller is responsible for trying alternative endpoints on
// failure.
func tryGetRecord(ctx context.Context, endpoint string) (struct {
	URI string `json:"uri"`
	CID string `json:"cid"`
}, error) {
	var body struct {
		URI string `json:"uri"`
		CID string `json:"cid"`
	}
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return body, err
	}
	resp, err := bskyAppViewClient.Do(req)
	if err != nil {
		return body, fmt.Errorf("fetch %s: %w", endpoint, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return body, fmt.Errorf("getRecord returned %d for %s", resp.StatusCode, endpoint)
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return body, fmt.Errorf("decode getRecord response: %w", err)
	}
	return body, nil
}

// parseATURI splits an at-uri into its three components. Expected shape:
//   at://<repo>/<collection>/<rkey>
// where repo is a DID or handle, collection is an NSID, rkey is an
// arbitrary record key. Returns (repo, collection, rkey, nil) on
// success.
func parseATURI(atURI string) (repo, collection, rkey string, err error) {
	const prefix = "at://"
	if !strings.HasPrefix(atURI, prefix) {
		return "", "", "", fmt.Errorf("not an at-uri")
	}
	parts := strings.SplitN(atURI[len(prefix):], "/", 3)
	if len(parts) != 3 {
		return "", "", "", fmt.Errorf("expected at://<repo>/<collection>/<rkey>")
	}
	if parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", "", fmt.Errorf("expected at://<repo>/<collection>/<rkey>")
	}
	return parts[0], parts[1], parts[2], nil
}
