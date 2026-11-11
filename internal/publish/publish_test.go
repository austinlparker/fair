package publish_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aparker.io/fair/internal/atproto"
	"aparker.io/fair/internal/publish"
)

// fakePDS records what the publish flow sends.
type fakePDS struct {
	docs    map[string]json.RawMessage // site.standard.document records by rkey
	puts    []map[string]any
	uploads int
	creates int
}

func (f *fakePDS) DID() string { return "did:plc:test" }

func (f *fakePDS) PutRecord(_ context.Context, collection, rkey string, record any) (*atproto.RecordRef, error) {
	// Round-trip through JSON so assertions see what the PDS would.
	b, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	f.puts = append(f.puts, m)
	if f.docs == nil {
		f.docs = map[string]json.RawMessage{}
	}
	f.docs[rkey] = b
	return &atproto.RecordRef{URI: "at://did:plc:test/" + collection + "/" + rkey, CID: "bafyrecord"}, nil
}

func (f *fakePDS) CreateRecord(_ context.Context, collection string, _ any) (*atproto.RecordRef, error) {
	f.creates++
	return &atproto.RecordRef{URI: "at://did:plc:test/" + collection + "/3announce0000", CID: "bafyannounce"}, nil
}

func (f *fakePDS) GetRecord(_ context.Context, collection, rkey string) (*atproto.GetRecordOutput, error) {
	if collection != "site.standard.document" {
		return &atproto.GetRecordOutput{CID: "bafyannounce"}, nil
	}
	doc, ok := f.docs[rkey]
	if !ok {
		return nil, errors.New("RecordNotFound")
	}
	return &atproto.GetRecordOutput{CID: "bafyrecord", Value: doc}, nil
}

func (f *fakePDS) UploadBlob(_ context.Context, mimeType string, data []byte) (*atproto.BlobRef, error) {
	f.uploads++
	return &atproto.BlobRef{Type: "blob", Ref: atproto.BlobCID{Link: atproto.BlobCIDFor(data)}, MimeType: mimeType, Size: int64(len(data))}, nil
}

// newPost lays out <tmp>/posts/hello/index.md (+ cover.png) and returns
// the source path and a publish.Config pointing at temp state/mirror.
func newPost(t *testing.T, frontmatterExtra string) (string, publish.Config) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "posts", "hello")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "index.md")
	content := "---\ntitle: Hello\ndate: \"2025-04-17\"\n" + frontmatterExtra + "---\n\nFirst paragraph.\n"
	if err := os.WriteFile(src, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cover.png"), []byte("not-really-a-png"), 0o644); err != nil {
		t.Fatal(err)
	}
	return src, publish.Config{
		BlogPosts:      filepath.Join(root, "posts"),
		StateFile:      filepath.Join(root, "state.json"),
		ImagesMirror:   filepath.Join(root, "mirror"),
		PublicationURI: "at://did:plc:test/site.standard.publication/self",
		PublicationURL: "https://example.com",
		MaxCoverSize:   1 << 20,
	}
}

func appendBody(t *testing.T, path, extra string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(extra); err != nil {
		t.Fatal(err)
	}
}

func TestPublish_FirstPublishThenSkip(t *testing.T) {
	src, cfg := newPost(t, "")
	pds := &fakePDS{}

	res, err := publish.Publish(context.Background(), pds, src, cfg, publish.Options{})
	if err != nil {
		t.Fatalf("first publish: %v", err)
	}
	if res.Skipped || len(pds.puts) != 1 {
		t.Fatalf("first publish: skipped=%v puts=%d", res.Skipped, len(pds.puts))
	}
	rec := pds.puts[0]
	if rec["path"] != "/hello" || rec["title"] != "Hello" || rec["publishedAt"] != "2025-04-17T00:00:00Z" {
		t.Errorf("unexpected record: %v", rec)
	}
	if _, ok := rec["updatedAt"]; ok {
		t.Errorf("first publish should not set updatedAt")
	}

	res, err = publish.Publish(context.Background(), pds, src, cfg, publish.Options{})
	if err != nil {
		t.Fatalf("second publish: %v", err)
	}
	if !res.Skipped || len(pds.puts) != 1 {
		t.Errorf("second publish should skip: skipped=%v puts=%d", res.Skipped, len(pds.puts))
	}
}

// A body-only edit re-puts the whole record; the cover blob ref has to
// ride along or the post loses its cover on the PDS.
func TestPublish_BodyEditKeepsCover(t *testing.T) {
	src, cfg := newPost(t, "cover: cover.png\n")
	pds := &fakePDS{}
	ctx := context.Background()

	if _, err := publish.Publish(ctx, pds, src, cfg, publish.Options{}); err != nil {
		t.Fatal(err)
	}
	appendBody(t, src, "\nSecond paragraph.\n")
	res, err := publish.Publish(ctx, pds, src, cfg, publish.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(pds.puts) != 2 {
		t.Fatalf("puts: got %d, want 2", len(pds.puts))
	}
	if _, ok := pds.puts[1]["coverImage"]; !ok {
		t.Errorf("coverImage dropped on body-only edit: %v", pds.puts[1])
	}
	if !res.UpdatedAt || pds.puts[1]["updatedAt"] == nil {
		t.Errorf("edit should advance updatedAt")
	}
}

func TestPublish_ForceAdvancesUpdatedAt(t *testing.T) {
	src, cfg := newPost(t, "")
	pds := &fakePDS{}
	ctx := context.Background()

	if _, err := publish.Publish(ctx, pds, src, cfg, publish.Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := publish.Publish(ctx, pds, src, cfg, publish.Options{Force: true}); err != nil {
		t.Fatal(err)
	}
	if len(pds.puts) != 2 || pds.puts[1]["updatedAt"] == nil {
		t.Errorf("--force re-put should carry updatedAt: %v", pds.puts)
	}
}

func TestPublish_DryRunTouchesNothingRemote(t *testing.T) {
	src, cfg := newPost(t, "cover: cover.png\n")
	pds := &fakePDS{}

	if _, err := publish.Publish(context.Background(), pds, src, cfg, publish.Options{DryRun: true, Announce: true}); err != nil {
		t.Fatal(err)
	}
	if len(pds.puts)+pds.uploads+pds.creates != 0 {
		t.Errorf("dry-run made remote calls: puts=%d uploads=%d creates=%d", len(pds.puts), pds.uploads, pds.creates)
	}
	if _, err := os.Stat(cfg.StateFile); err == nil {
		t.Errorf("dry-run wrote state")
	}
	got, _ := os.ReadFile(src)
	if !strings.Contains(string(got), "rkey: ") {
		t.Errorf("dry-run should still assign an rkey:\n%s", got)
	}
}

func TestPublish_AnnounceIsIdempotent(t *testing.T) {
	// The body mentions "bsky_post:" — the write-back must key off the
	// parsed frontmatter, not a substring match on the whole file.
	src, cfg := newPost(t, "")
	appendBody(t, src, "\nSet `bsky_post:` in frontmatter to pin a thread.\n")
	pds := &fakePDS{}
	ctx := context.Background()

	if _, err := publish.Publish(ctx, pds, src, cfg, publish.Options{Announce: true}); err != nil {
		t.Fatal(err)
	}
	res, err := publish.Publish(ctx, pds, src, cfg, publish.Options{Announce: true})
	if err != nil {
		t.Fatal(err)
	}
	if pds.creates != 1 {
		t.Errorf("announce posts created: got %d, want 1", pds.creates)
	}
	if !res.Skipped {
		t.Errorf("second --announce run should skip")
	}
	got, _ := os.ReadFile(src)
	if !strings.HasSuffix(string(got), "pin a thread.\n") || strings.HasSuffix(string(got), "\n\n") {
		t.Errorf("write-backs disturbed the end of the file:\n%q", got)
	}
}

// With no local state (fresh checkout, CI runner) the PDS decides
// whether a post is unchanged.
func TestPublish_NoStateAdoptsMatchingPDSRecord(t *testing.T) {
	src, cfg := newPost(t, "cover: cover.png\ntags: [Go, atproto]\n")
	pds := &fakePDS{}
	ctx := context.Background()

	if _, err := publish.Publish(ctx, pds, src, cfg, publish.Options{}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(cfg.StateFile); err != nil {
		t.Fatal(err)
	}

	res, err := publish.Publish(ctx, pds, src, cfg, publish.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Skipped || len(pds.puts) != 1 || pds.uploads != 1 {
		t.Fatalf("unchanged post was re-put without state: skipped=%v puts=%d uploads=%d", res.Skipped, len(pds.puts), pds.uploads)
	}
	if _, err := os.Stat(cfg.StateFile); err != nil {
		t.Errorf("adopting should rebuild the state entry: %v", err)
	}

	// Changed content with no state is an update, not a first publish.
	if err := os.Remove(cfg.StateFile); err != nil {
		t.Fatal(err)
	}
	appendBody(t, src, "\nEdited elsewhere.\n")
	if _, err := publish.Publish(ctx, pds, src, cfg, publish.Options{}); err != nil {
		t.Fatal(err)
	}
	if len(pds.puts) != 2 || pds.puts[1]["updatedAt"] == nil {
		t.Errorf("edit without state should re-put with updatedAt: puts=%d", len(pds.puts))
	}

	// A changed cover alone is also detected (by blob CID).
	if err := os.Remove(cfg.StateFile); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(src), "cover.png"), []byte("different-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := publish.Publish(ctx, pds, src, cfg, publish.Options{}); err != nil {
		t.Fatal(err)
	}
	if len(pds.puts) != 3 {
		t.Errorf("cover change without state should re-put: puts=%d", len(pds.puts))
	}
}
