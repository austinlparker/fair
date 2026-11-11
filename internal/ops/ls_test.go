package ops_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"aparker.io/fair/internal/atproto"
	"aparker.io/fair/internal/ops"
)

// pagedLister serves records two per page to exercise cursor handling.
type pagedLister struct{ rkeys []string }

func (p pagedLister) ListRecords(_ context.Context, _, cursor string, _ int) (*atproto.ListRecordsOutput, error) {
	start := 0
	if cursor != "" {
		fmt.Sscanf(cursor, "%d", &start)
	}
	end := min(start+2, len(p.rkeys))
	out := &atproto.ListRecordsOutput{}
	for _, rkey := range p.rkeys[start:end] {
		v, _ := json.Marshal(map[string]string{"title": "T " + rkey, "path": "/" + rkey})
		out.Records = append(out.Records, atproto.ListRecordsItem{
			URI:   "at://did:plc:test/site.standard.document/" + rkey,
			Value: v,
		})
	}
	if end < len(p.rkeys) {
		out.Cursor = fmt.Sprint(end)
	}
	return out, nil
}

func writePost(t *testing.T, root, slug, rkeyLine string) {
	t.Helper()
	dir := filepath.Join(root, slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := "---\n" + rkeyLine + "title: " + slug + "\ndate: \"2025-04-17\"\n---\n\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, "index.md"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLs_MatchesByFrontmatterRkeyAcrossPages(t *testing.T) {
	root := t.TempDir()
	writePost(t, root, "a", "rkey: rk1\n")
	writePost(t, root, "b", "rkey: rk3\n")  // on the second page
	writePost(t, root, "b2", "rkey: rk3\n") // duplicate claim
	writePost(t, root, "c", "rkey: gone\n") // rkey with no record
	writePost(t, root, "d", "")             // never published

	rows, err := ops.Ls(context.Background(), pagedLister{rkeys: []string{"rk1", "rk2", "rk3"}}, root)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, r := range rows {
		got[r.Path] = r.Status
	}
	want := map[string]string{
		"a/index.md":  ops.StatusSynced,
		"b/index.md":  ops.StatusSynced,
		"b2/index.md": ops.StatusConflict,
		"c/index.md":  ops.StatusLocalOnly,
		"d/index.md":  ops.StatusLocalOnly,
		"/rk2":        ops.StatusPDSOnly,
	}
	if len(got) != len(want) {
		t.Fatalf("rows: got %v, want %v", got, want)
	}
	for path, status := range want {
		if got[path] != status {
			t.Errorf("%s: got %q, want %q", path, got[path], status)
		}
	}
	if rows[0].Status != ops.StatusSynced || rows[len(rows)-1].Status != ops.StatusLocalOnly {
		t.Errorf("rows not ordered by status: %v", rows)
	}
}
