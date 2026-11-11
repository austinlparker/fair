package atproto

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/xrpc"
	oauth "github.com/haileyok/atproto-oauth-golang"
	"github.com/ipfs/go-cid"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/multiformats/go-multihash"
)

// Client is the authenticated XRPC wrapper used by the publish flow.
// It exposes repo CRUD methods modeled on com.atproto.repo.* over one
// of two auth modes:
//
//   - OAuth (NewClient): binds a persisted Session to a *haileyok*
//     XrpcClient and handles DPoP nonce rotation transparently. The
//     session must already exist on disk (`fair auth login` has run).
//   - App password (NewAppPasswordClient): a fresh legacy bearer
//     session per process, nothing persisted. For headless runs (CI)
//     where the OAuth browser hop and rotating refresh tokens don't fit.
//
// Concurrency: Client is not goroutine-safe. The publish flow is single-
// threaded so this is acceptable; if that changes, add a Mutex around
// session mutations and persistence.
type Client struct {
	did string
	// do issues one authenticated XRPC request; its shape matches both
	// underlying libraries' Do methods minus their auth arguments.
	do func(ctx context.Context, kind, encoding, method string, params map[string]any, body, out any) error

	// OAuth mode only; zero in app-password mode.
	session Session
	store   TokenStore
	profile string
	dpopKey jwk.Key
}

// NewClient loads the session for profile and prepares the XRPC layer.
// rootCAs may be nil (production); pass mkcert's CA pool for sandbox.
func NewClient(profile string, store TokenStore, rootCAs *x509.CertPool) (*Client, error) {
	session, err := store.Load(profile)
	if err != nil {
		return nil, fmt.Errorf("load session for %s: %w", profile, err)
	}

	dpopKey, err := jwk.ParseKey(session.DPoPPrivateJWK)
	if err != nil {
		return nil, fmt.Errorf("parse stored DPoP JWK: %w", err)
	}

	c := &Client{
		did:     session.DID,
		session: session,
		store:   store,
		profile: profile,
		dpopKey: dpopKey,
	}
	x := &oauth.XrpcClient{Client: NewHTTPClient(rootCAs)}
	// Persist the new nonce on every PDS rotation so the next CLI
	// invocation starts with the correct value.
	x.OnDpopPdsNonceChanged = func(_, nonce string) {
		c.session.DPoPPDSNonce = nonce
		_ = c.store.Save(profile, c.session)
	}
	c.do = func(ctx context.Context, kind, encoding, method string, params map[string]any, body, out any) error {
		// Built per request: refresh and nonce rotation mutate c.session.
		args := &oauth.XrpcAuthedRequestArgs{
			Did:            c.session.DID,
			PdsUrl:         c.session.PDSURL,
			Issuer:         c.session.AuthServerIssuer,
			AccessToken:    c.session.AccessToken,
			DpopPdsNonce:   c.session.DPoPPDSNonce,
			DpopPrivateJwk: c.dpopKey,
		}
		return x.Do(ctx, args, kind, encoding, method, params, body, out)
	}
	return c, nil
}

// NewAppPasswordClient signs in with com.atproto.server.createSession
// and returns a Client using the resulting bearer token. identifier is
// a handle or DID. Nothing is persisted and the token is never
// refreshed: it outlives any single CLI run by a wide margin.
func NewAppPasswordClient(ctx context.Context, pdsURL, identifier, password string, rootCAs *x509.CertPool) (*Client, error) {
	x := &xrpc.Client{
		Client: NewHTTPClient(rootCAs),
		Host:   strings.TrimRight(pdsURL, "/"),
	}
	var out struct {
		AccessJwt string `json:"accessJwt"`
		DID       string `json:"did"`
	}
	in := map[string]any{"identifier": identifier, "password": password}
	if err := x.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.server.createSession", nil, in, &out); err != nil {
		return nil, fmt.Errorf("createSession: %w", err)
	}
	if out.AccessJwt == "" || out.DID == "" {
		return nil, fmt.Errorf("createSession: response missing accessJwt or did")
	}
	x.Auth = &xrpc.AuthInfo{AccessJwt: out.AccessJwt, Did: out.DID}
	return &Client{did: out.DID, do: x.Do}, nil
}

// DID returns the repo the client is authenticated as.
func (c *Client) DID() string {
	return c.did
}

// --- repo CRUD --------------------------------------------------------------

// PutRecordInput models com.atproto.repo.putRecord input.
type PutRecordInput struct {
	Repo       string `json:"repo"`
	Collection string `json:"collection"`
	Rkey       string `json:"rkey"`
	Record     any    `json:"record"`
	Validate   *bool  `json:"validate,omitempty"`
}

// RecordRef is the URI+CID returned by put/get/list operations.
type RecordRef struct {
	URI string `json:"uri"`
	CID string `json:"cid"`
}

// PutRecord uploads a record to the repo. record is marshaled to JSON
// directly (use map[string]any or a struct with json tags).
func (c *Client) PutRecord(ctx context.Context, collection, rkey string, record any) (*RecordRef, error) {
	in := PutRecordInput{
		Repo:       c.did,
		Collection: collection,
		Rkey:       rkey,
		Record:     record,
	}
	var out RecordRef
	err := c.do(ctx,
		xrpc.Procedure, "application/json",
		"com.atproto.repo.putRecord", nil, in, &out,
	)
	if err != nil {
		return nil, fmt.Errorf("putRecord: %w", err)
	}
	return &out, nil
}

// CreateRecordInput models com.atproto.repo.createRecord input. Like
// putRecord but the rkey is server-assigned (a TID). Used for records
// where the operator doesn't have a stable identifier — most notably
// app.bsky.feed.post when announcing a blog post.
type CreateRecordInput struct {
	Repo       string `json:"repo"`
	Collection string `json:"collection"`
	Rkey       string `json:"rkey,omitempty"`
	Record     any    `json:"record"`
	Validate   *bool  `json:"validate,omitempty"`
}

// CreateRecord creates a new record with a server-assigned rkey.
// Returns the assigned URI+CID.
func (c *Client) CreateRecord(ctx context.Context, collection string, record any) (*RecordRef, error) {
	in := CreateRecordInput{
		Repo:       c.did,
		Collection: collection,
		Record:     record,
	}
	var out RecordRef
	err := c.do(ctx,
		xrpc.Procedure, "application/json",
		"com.atproto.repo.createRecord", nil, in, &out,
	)
	if err != nil {
		return nil, fmt.Errorf("createRecord: %w", err)
	}
	return &out, nil
}

// GetRecordOutput is the shape of com.atproto.repo.getRecord output.
type GetRecordOutput struct {
	URI   string          `json:"uri"`
	CID   string          `json:"cid"`
	Value json.RawMessage `json:"value"`
}

// GetRecord fetches a single record by collection+rkey.
func (c *Client) GetRecord(ctx context.Context, collection, rkey string) (*GetRecordOutput, error) {
	params := map[string]any{
		"repo":       c.did,
		"collection": collection,
		"rkey":       rkey,
	}
	var out GetRecordOutput
	err := c.do(ctx,
		xrpc.Query, "",
		"com.atproto.repo.getRecord", params, nil, &out,
	)
	if err != nil {
		return nil, fmt.Errorf("getRecord: %w", err)
	}
	return &out, nil
}

// ListRecordsOutput is the shape of com.atproto.repo.listRecords output.
type ListRecordsOutput struct {
	Records []ListRecordsItem `json:"records"`
	Cursor  string            `json:"cursor,omitempty"`
}

// ListRecordsItem is a single record entry in a list response.
type ListRecordsItem struct {
	URI   string          `json:"uri"`
	CID   string          `json:"cid"`
	Value json.RawMessage `json:"value"`
}

// ListRecords pages through a collection. Pass cursor="" on first call;
// subsequent calls pass the cursor from the prior response.
//
// limit is clamped to [1, 100] by the PDS; passing 0 lets the server pick.
func (c *Client) ListRecords(ctx context.Context, collection, cursor string, limit int) (*ListRecordsOutput, error) {
	params := map[string]any{
		"repo":       c.did,
		"collection": collection,
	}
	if cursor != "" {
		params["cursor"] = cursor
	}
	if limit > 0 {
		params["limit"] = limit
	}
	var out ListRecordsOutput
	err := c.do(ctx,
		xrpc.Query, "",
		"com.atproto.repo.listRecords", params, nil, &out,
	)
	if err != nil {
		return nil, fmt.Errorf("listRecords: %w", err)
	}
	return &out, nil
}

// DeleteRecord removes a record by collection+rkey. Idempotent on a
// well-behaved PDS — deleting a missing record returns success.
func (c *Client) DeleteRecord(ctx context.Context, collection, rkey string) error {
	in := map[string]any{
		"repo":       c.did,
		"collection": collection,
		"rkey":       rkey,
	}
	err := c.do(ctx,
		xrpc.Procedure, "application/json",
		"com.atproto.repo.deleteRecord", nil, in, nil,
	)
	if err != nil {
		return fmt.Errorf("deleteRecord: %w", err)
	}
	return nil
}

// --- blob -------------------------------------------------------------------

// BlobRef is the typed blob the publish flow embeds in record fields
// (e.g., site.standard.document.coverImage). The shape matches ATProto's
// canonical blob CBOR encoding when serialized to JSON.
type BlobRef struct {
	Type     string  `json:"$type"`
	Ref      BlobCID `json:"ref"`
	MimeType string  `json:"mimeType"`
	Size     int64   `json:"size"`
}

// BlobCID is the inner CID link.
type BlobCID struct {
	Link string `json:"$link"`
}

// BlobCIDFor returns the CID a PDS assigns to a blob with these bytes
// (CIDv1, raw codec, sha2-256), so local bytes can be compared against
// a published BlobRef without uploading them.
func BlobCIDFor(data []byte) string {
	sum, err := multihash.Sum(data, multihash.SHA2_256, -1)
	if err != nil {
		panic(err) // SHA2_256 at default length cannot fail
	}
	return cid.NewCidV1(cid.Raw, sum).String()
}

// uploadBlobOutput is the response shape from com.atproto.repo.uploadBlob.
type uploadBlobOutput struct {
	Blob BlobRef `json:"blob"`
}

// UploadBlob uploads a blob and returns the typed BlobRef suitable for
// embedding in record fields.
func (c *Client) UploadBlob(ctx context.Context, mimeType string, data []byte) (*BlobRef, error) {
	if mimeType == "" {
		return nil, fmt.Errorf("UploadBlob: mimeType required")
	}
	body := bytes.NewReader(data)
	var out uploadBlobOutput
	err := c.do(ctx,
		xrpc.Procedure, mimeType,
		"com.atproto.repo.uploadBlob", nil, body, &out,
	)
	if err != nil {
		return nil, fmt.Errorf("uploadBlob: %w", err)
	}
	return &out.Blob, nil
}

// --- token refresh ----------------------------------------------------------

// RefreshIfNeeded calls the auth server's refresh endpoint if the
// session's access token is at-or-past expiry, then persists the new
// session. Caller should invoke this at the top of any publish-flow
// run that may use the Client across the expiry boundary.
//
// clientID and clientKey must match the ones used at login (loopback
// or domain-derived, plus the persistent ECDSA JWK). RefreshIfNeeded
// is a no-op if the session isn't expired.
func (c *Client) RefreshIfNeeded(ctx context.Context, clientID string, clientKey jwk.Key, rootCAs *x509.CertPool) error {
	// App-password clients hold a fresh token and have nothing to refresh.
	if c.store == nil || !c.session.IsExpired(time.Now()) {
		return nil
	}
	oauthClient, err := oauth.NewClient(oauth.ClientArgs{
		Http:      NewHTTPClient(rootCAs),
		ClientJwk: clientKey,
		ClientId:  clientID,
		// RedirectUri is required by NewClient but not used during refresh.
		RedirectUri: "http://127.0.0.1/callback",
	})
	if err != nil {
		return fmt.Errorf("oauth.NewClient for refresh: %w", err)
	}

	resp, err := oauthClient.RefreshTokenRequest(
		ctx,
		c.session.RefreshToken,
		c.session.AuthServerIssuer,
		c.session.DPoPAuthServerNonce,
		c.dpopKey,
	)
	if err != nil {
		return fmt.Errorf("refresh token: %w", err)
	}

	c.session.AccessToken = resp.AccessToken
	c.session.RefreshToken = resp.RefreshToken
	c.session.ExpiresAt = time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second)
	if resp.DpopAuthserverNonce != "" {
		c.session.DPoPAuthServerNonce = resp.DpopAuthserverNonce
	}
	return c.store.Save(c.profile, c.session)
}
