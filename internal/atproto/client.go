package atproto

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/xrpc"
	oauth "github.com/haileyok/atproto-oauth-golang"
	"github.com/lestrrat-go/jwx/v2/jwk"
)

// Client is the authenticated XRPC wrapper used by the publish flow.
// It binds a Session to a *haileyok* XrpcClient, handles DPoP nonce
// rotation transparently (persisting via the TokenStore), and exposes
// repo CRUD methods modeled on com.atproto.repo.*.
//
// Construction: NewClient(profile, store, rootCAs). The session must
// already exist on disk (i.e., `fair auth login` has run for profile).
//
// Concurrency: Client is not goroutine-safe. The publish flow is single-
// threaded so this is acceptable; if that changes, add a Mutex around
// session mutations and persistence.
type Client struct {
	session Session
	store   TokenStore
	profile string
	dpopKey jwk.Key
	xrpc    *oauth.XrpcClient
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
		session: session,
		store:   store,
		profile: profile,
		dpopKey: dpopKey,
		xrpc: &oauth.XrpcClient{
			Client: newHTTPClient(rootCAs),
		},
	}
	// Persist the new nonce on every PDS rotation so the next CLI
	// invocation starts with the correct value.
	c.xrpc.OnDpopPdsNonceChanged = func(_, nonce string) {
		c.session.DPoPPDSNonce = nonce
		_ = c.store.Save(profile, c.session)
	}
	return c, nil
}

// Session returns a copy of the current session state.
func (c *Client) Session() Session {
	return c.session
}

// authedArgs assembles the per-request auth bundle the haileyok library
// expects.
func (c *Client) authedArgs() *oauth.XrpcAuthedRequestArgs {
	return &oauth.XrpcAuthedRequestArgs{
		Did:            c.session.DID,
		PdsUrl:         c.session.PDSURL,
		Issuer:         c.session.AuthServerIssuer,
		AccessToken:    c.session.AccessToken,
		DpopPdsNonce:   c.session.DPoPPDSNonce,
		DpopPrivateJwk: c.dpopKey,
	}
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
		Repo:       c.session.DID,
		Collection: collection,
		Rkey:       rkey,
		Record:     record,
	}
	var out RecordRef
	err := c.xrpc.Do(ctx, c.authedArgs(),
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
		Repo:       c.session.DID,
		Collection: collection,
		Record:     record,
	}
	var out RecordRef
	err := c.xrpc.Do(ctx, c.authedArgs(),
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
		"repo":       c.session.DID,
		"collection": collection,
		"rkey":       rkey,
	}
	var out GetRecordOutput
	err := c.xrpc.Do(ctx, c.authedArgs(),
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
		"repo":       c.session.DID,
		"collection": collection,
	}
	if cursor != "" {
		params["cursor"] = cursor
	}
	if limit > 0 {
		params["limit"] = limit
	}
	var out ListRecordsOutput
	err := c.xrpc.Do(ctx, c.authedArgs(),
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
		"repo":       c.session.DID,
		"collection": collection,
		"rkey":       rkey,
	}
	err := c.xrpc.Do(ctx, c.authedArgs(),
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
	err := c.xrpc.Do(ctx, c.authedArgs(),
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
	if !c.session.IsExpired(time.Now()) {
		return nil
	}
	oauthClient, err := oauth.NewClient(oauth.ClientArgs{
		Http:      newHTTPClient(rootCAs),
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
