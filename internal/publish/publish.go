package publish

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"aparker.io/fair/internal/atproto"
	"aparker.io/fair/internal/markdown"
)

// Options control the publish flow's behavior.
type Options struct {
	// DryRun: parse, hash, and report what would happen, but never
	// upload blobs, putRecord, announce, or update state.json. The local
	// side effects still happen: id/rkey are written back to the source
	// frontmatter (the rkey must be stable across runs) and inline
	// images are staged into the mirror.
	DryRun bool

	// Force: bypass the content-hash short-circuit and re-put the
	// record even when nothing changed. Advances updatedAt like any
	// other re-put of an already-published post.
	Force bool

	// Announce: when true and the post has no existing bsky_post
	// frontmatter, create an app.bsky.feed.post on the user's repo
	// linking to the article and store its at-uri+cid as bskyPostRef
	// on the document record. The new at-uri also gets written back
	// to the source frontmatter as bsky_post: so subsequent runs are
	// idempotent (no duplicate announce posts).
	Announce bool

	// Logger receives operator-facing progress strings. nil silences output.
	Logger func(format string, args ...any)
}

// Config bundles the per-call publish-flow inputs.
type Config struct {
	// BlogPosts: root directory containing post directories (used as
	// the resolution base for inline image references).
	BlogPosts string

	// StateFile: path to state.<profile>.json.
	StateFile string

	// ImagesMirror: staging directory for inline images. RewriteImages
	// copies into <ImagesMirror>/<slug>/<basename>.
	ImagesMirror string

	// PublicationURI: the publication record's AT-URI, e.g.
	// at://did:plc:.../site.standard.publication/self.
	PublicationURI string

	// PublicationURL: the rendered site's absolute URL (no trailing
	// slash), e.g. https://aparker.io. Used to construct article URLs
	// for the --announce embed card.
	PublicationURL string

	// MaxCoverSize: byte cap on cover images. The site.standard.document
	// spec recommends 1MB; we enforce it at the client because PDS may
	// accept larger blobs but external consumers truncate.
	MaxCoverSize int64
}

// Result describes the outcome of one Publish call.
type Result struct {
	Rkey         string
	DID          string
	URI          string // at://did/collection/rkey from putRecord
	CID          string
	Skipped      bool   // true when content hash was unchanged & not --force
	Reason       string // why skipped, if applicable
	BlobUploaded bool
	UpdatedAt    bool // true when updatedAt was advanced
}

// PDS is the slice of the authenticated client the publish flow uses.
// *atproto.Client satisfies it; tests substitute a fake.
type PDS interface {
	DID() string
	PutRecord(ctx context.Context, collection, rkey string, record any) (*atproto.RecordRef, error)
	CreateRecord(ctx context.Context, collection string, record any) (*atproto.RecordRef, error)
	GetRecord(ctx context.Context, collection, rkey string) (*atproto.GetRecordOutput, error)
	UploadBlob(ctx context.Context, mimeType string, data []byte) (*atproto.BlobRef, error)
}

const documentCollection = "site.standard.document"

// coverMimeTypes are the blob types the OAuth scope (atproto.BlogScopes)
// lets us upload. Anything else is rejected by the PDS with an opaque
// scope error, so we check up front.
var coverMimeTypes = []string{"image/png", "image/jpeg", "image/webp"}

// Publish runs the publish flow on a single markdown file. See
// docs/design-plans/2026-04-29-blog-v2026.md §3 (publish flow) for the
// step ordering rationale.
func Publish(ctx context.Context, client PDS, sourcePath string, cfg Config, opts Options) (Result, error) {
	log := opts.Logger
	if log == nil {
		log = func(string, ...any) {}
	}

	// 1. Identity (writes id back to source if missing).
	id, err := EnsureID(sourcePath)
	if err != nil {
		return Result{}, fmt.Errorf("ensure id: %w", err)
	}

	// 1b. Record key (writes a TID rkey back to source if missing). The
	//     lexicon requires key:tid; persisting it in frontmatter keeps
	//     re-publishes idempotent across machines.
	rkey, err := EnsureRkey(sourcePath)
	if err != nil {
		return Result{}, fmt.Errorf("ensure rkey: %w", err)
	}

	// 2. Reread the (possibly mutated) source.
	src, err := os.ReadFile(sourcePath)
	if err != nil {
		return Result{}, fmt.Errorf("read %s: %w", sourcePath, err)
	}
	fm, body, err := markdown.Parse(string(src))
	if err != nil {
		return Result{}, fmt.Errorf("parse: %w", err)
	}

	// 3. Slug + grammar.
	slug := fm.Slug
	if slug == "" {
		slug, err = SlugFromPath(sourcePath)
		if err != nil {
			return Result{}, err
		}
	}
	if err := ValidateSlug(slug); err != nil {
		return Result{}, err
	}

	// 4. State. Renames are no longer special-cased: the TID rkey lives
	//    in frontmatter, so renaming a post's directory just changes the
	//    slug-derived `path` field on the same record — a normal update,
	//    not a delete-and-recreate.
	state, err := LoadState(cfg.StateFile)
	if err != nil {
		return Result{}, err
	}

	// 5. Body normalization + image rewriting.
	body = markdown.Normalize(body)
	sourceDir := filepath.Dir(sourcePath)
	rewrittenBody, err := RewriteImages(body, slug, sourceDir, cfg.ImagesMirror)
	if err != nil {
		return Result{}, err
	}

	// 6. Description fallback + textContent.
	description := fm.Description
	if description == "" {
		description = ExtractDescription(rewrittenBody)
	}
	textContent := ExtractTextContent(rewrittenBody)

	// 7. Cover bytes + size check + source hash (pre-upload, so no
	//    wasted upload on a no-op publish).
	var coverHash string
	var coverData []byte
	var coverMime string
	if fm.Cover != "" {
		coverPath := filepath.Join(sourceDir, fm.Cover)
		coverData, err = os.ReadFile(coverPath)
		if err != nil {
			return Result{}, fmt.Errorf("read cover %s: %w", coverPath, err)
		}
		if cfg.MaxCoverSize > 0 && int64(len(coverData)) > cfg.MaxCoverSize {
			return Result{}, fmt.Errorf("cover %s is %d bytes; max is %d. Convert/downscale and try again", coverPath, len(coverData), cfg.MaxCoverSize)
		}
		sum := sha256.Sum256(coverData)
		coverHash = hex.EncodeToString(sum[:])
		coverMime, _, _ = mime.ParseMediaType(mime.TypeByExtension(strings.ToLower(filepath.Ext(coverPath))))
		if !slices.Contains(coverMimeTypes, coverMime) {
			return Result{}, fmt.Errorf("cover %s: unsupported type (want png, jpeg, or webp)", coverPath)
		}
	}

	// 8. Compute hash + short-circuit.
	hash := markdown.Hash(markdown.HashInput{
		NormalizedBody:  rewrittenBody,
		Frontmatter:     fm,
		CoverSourceHash: coverHash,
	})
	prevEntry, hadEntry := state.Entries[rkey]
	unchanged := hadEntry && prevEntry.LastHash == hash
	onPDS := hadEntry
	skipReason := "hash unchanged"
	if !hadEntry {
		// No local state for this post: a fresh checkout, a CI runner,
		// or a genuinely new post. The state file is only a cache — the
		// PDS is canonical — so ask it. If the record there already
		// matches the source, adopt it instead of re-putting. A lookup
		// error (normally RecordNotFound) just means "publish it".
		if existing, err := client.GetRecord(ctx, documentCollection, rkey); err == nil {
			onPDS = true
			unchanged = matchesPublished(existing.Value, publishedView{
				Site:        cfg.PublicationURI,
				Title:       fm.Title,
				Path:        "/" + slug,
				Date:        fm.Date,
				Description: description,
				Tags:        markdown.NormalizeTags(fm.Tags),
				Text:        rewrittenBody,
				CoverData:   coverData,
				BskyPost:    fm.BskyPost,
			})
			skipReason = "matches PDS record"
		}
	}
	// --announce on an unchanged post that's never been announced
	// must NOT short-circuit: the announce flow needs to run, the
	// document needs to be re-put with the new bskyPostRef, and the
	// frontmatter needs to gain bsky_post:.
	needsAnnounce := opts.Announce && fm.BskyPost == "" && !opts.DryRun
	if unchanged && !opts.Force && !needsAnnounce {
		log("skip %s (%s)", slug, skipReason)
		if !hadEntry && !opts.DryRun {
			state.Entries[rkey] = StateEntry{ID: id, LastHash: hash, LastPublishedAt: time.Now().UTC()}
			if err := state.Save(cfg.StateFile); err != nil {
				return Result{}, fmt.Errorf("save state: %w", err)
			}
		}
		return Result{
			Rkey:    rkey,
			DID:     client.DID(),
			Skipped: true,
			Reason:  skipReason,
		}, nil
	}

	if opts.DryRun {
		log("would publish %s (force=%v, cover=%v, images-mirrored)", slug, opts.Force, fm.Cover != "")
		return Result{Rkey: rkey, DID: client.DID()}, nil
	}

	// 9. Upload the cover. This runs on every real publish, not only
	//    when the cover bytes changed: putRecord replaces the whole
	//    record, so the blob ref has to be present each time or the
	//    cover silently disappears on a body-only edit. Re-uploading
	//    identical bytes is idempotent on the PDS (same CID back).
	var coverBlob *atproto.BlobRef
	if coverData != nil {
		log("uploading cover (%d bytes, %s)", len(coverData), coverMime)
		coverBlob, err = client.UploadBlob(ctx, coverMime, coverData)
		if err != nil {
			return Result{}, fmt.Errorf("upload cover: %w", err)
		}
	}

	// 10. Build record.
	publishedAt, err := normalizeRFC3339(fm.Date)
	if err != nil {
		return Result{}, fmt.Errorf("normalize publishedAt: %w", err)
	}
	record := map[string]any{
		"$type":       documentCollection,
		"site":        cfg.PublicationURI,
		"title":       fm.Title,
		"publishedAt": publishedAt,
		"path":        "/" + slug,
		"content": map[string]any{
			"$type":   "site.standard.content.markdown",
			"text":    rewrittenBody,
			"version": "1.0",
		},
		"textContent": textContent,
	}
	if description != "" {
		record["description"] = description
	}
	if coverBlob != nil {
		record["coverImage"] = coverBlob
	}
	if len(fm.Tags) > 0 {
		record["tags"] = markdown.NormalizeTags(fm.Tags)
	}
	if fm.BskyPost != "" {
		// Resolve the at-uri to a CID and emit a strongRef per the
		// standard.site lexicon. Own-repo lookups use the authed
		// client (trusts our PDS, important for sandbox); external
		// lookups hit the public Bluesky AppView.
		cid, err := resolveBskyPostCID(ctx, fm.BskyPost, client)
		if err != nil {
			return Result{}, fmt.Errorf("bsky_post: %w", err)
		}
		record["bskyPostRef"] = map[string]any{
			"uri": fm.BskyPost,
			"cid": cid,
		}
	} else if opts.Announce && !opts.DryRun {
		// No prior announce post; create one and pin it as the comment
		// root. The new at-uri gets written back to frontmatter so
		// re-runs short-circuit on this branch.
		articleURL := strings.TrimRight(cfg.PublicationURL, "/") + "/" + slug + "/"
		announceDesc := description
		if announceDesc == "" {
			announceDesc = fm.Title
		}
		log("announce app.bsky.feed.post → %s", articleURL)
		announceURI, announceCID, err := Announce(ctx, client, AnnounceInput{
			Title:       fm.Title,
			Description: announceDesc,
			URL:         articleURL,
			CoverBlob:   coverBlob,
		})
		if err != nil {
			return Result{}, fmt.Errorf("announce: %w", err)
		}
		record["bskyPostRef"] = map[string]any{
			"uri": announceURI,
			"cid": announceCID,
		}
		// Persist the new at-uri to the source markdown so subsequent
		// publishes reuse it (idempotency) and so it appears in the
		// authored frontmatter for everyone to see. Also update fm and
		// recompute hash so the state save below stores the
		// post-writeback hash; otherwise a no-flag re-publish would
		// see "frontmatter changed" and unnecessarily re-put.
		if err := writeBackBskyPost(sourcePath, announceURI); err != nil {
			return Result{}, fmt.Errorf("write back bsky_post: %w", err)
		}
		fm.BskyPost = announceURI
		hash = markdown.Hash(markdown.HashInput{
			NormalizedBody:  rewrittenBody,
			Frontmatter:     fm,
			CoverSourceHash: coverHash,
		})
	}
	// Any re-put of a post that's already on the PDS is an update. (This
	// also keeps updatedAt from vanishing on --force: the record is
	// rebuilt from scratch, so omitting the field would delete it.)
	updatedAtAdvanced := false
	if onPDS {
		record["updatedAt"] = time.Now().UTC().Format(time.RFC3339)
		updatedAtAdvanced = true
	}

	// 11. PutRecord.
	log("putRecord %s/%s", documentCollection, rkey)
	ref, err := client.PutRecord(ctx, documentCollection, rkey, record)
	if err != nil {
		return Result{}, err
	}

	// 12. Persist state. (Image mirror copies happened in step 5;
	//     orphan files on a put-success/state-save fail are harmless.)
	state.Entries[rkey] = StateEntry{
		ID:              id,
		LastHash:        hash,
		LastPublishedAt: time.Now().UTC(),
	}
	if err := state.Save(cfg.StateFile); err != nil {
		return Result{}, fmt.Errorf("published OK but local state save failed: %w (the next publish of this post will re-put it)", err)
	}

	return Result{
		Rkey:         rkey,
		DID:          client.DID(),
		URI:          ref.URI,
		CID:          ref.CID,
		BlobUploaded: coverBlob != nil,
		UpdatedAt:    updatedAtAdvanced,
	}, nil
}

// publishedView is the author-controlled content of a document, in the
// form the publish flow would write it. textContent and updatedAt are
// derived, so they're not part of the comparison.
type publishedView struct {
	Site, Title, Path string
	Date              string // frontmatter form; normalized before comparing
	Description       string
	Tags              []string
	Text              string
	CoverData         []byte // nil when the post has no cover
	BskyPost          string
}

// matchesPublished reports whether the record value fetched from the
// PDS already carries exactly the content in want.
func matchesPublished(value json.RawMessage, want publishedView) bool {
	var got struct {
		Site        string   `json:"site"`
		Title       string   `json:"title"`
		Path        string   `json:"path"`
		PublishedAt string   `json:"publishedAt"`
		Description string   `json:"description"`
		Tags        []string `json:"tags"`
		Content     struct {
			Text string `json:"text"`
		} `json:"content"`
		CoverImage *struct {
			Ref struct {
				Link string `json:"$link"`
			} `json:"ref"`
		} `json:"coverImage"`
		BskyPostRef *struct {
			URI string `json:"uri"`
		} `json:"bskyPostRef"`
	}
	if err := json.Unmarshal(value, &got); err != nil {
		return false
	}
	publishedAt, err := normalizeRFC3339(want.Date)
	if err != nil {
		return false
	}
	var gotCover, wantCover, gotBsky string
	if got.CoverImage != nil {
		gotCover = got.CoverImage.Ref.Link
	}
	if want.CoverData != nil {
		wantCover = atproto.BlobCIDFor(want.CoverData)
	}
	if got.BskyPostRef != nil {
		gotBsky = got.BskyPostRef.URI
	}
	return got.Site == want.Site &&
		got.Title == want.Title &&
		got.Path == want.Path &&
		got.PublishedAt == publishedAt &&
		got.Description == want.Description &&
		slices.Equal(got.Tags, want.Tags) &&
		got.Content.Text == want.Text &&
		gotCover == wantCover &&
		gotBsky == want.BskyPost
}

// normalizeRFC3339 accepts gray-matter-style date strings ("2025-04-17",
// "2025-04-17T12:00:00Z", and a few common variations) and returns an
// RFC3339 datetime suitable for the site.standard.document.publishedAt
// field. Bare dates default to midnight UTC.
func normalizeRFC3339(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", errors.New("empty date")
	}
	// Try a few accepted formats in order.
	for _, layout := range []string{time.RFC3339, time.RFC3339Nano, "2006-01-02", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC().Format(time.RFC3339), nil
		}
	}
	return "", fmt.Errorf("unrecognized date format %q (want RFC3339 or YYYY-MM-DD)", s)
}
