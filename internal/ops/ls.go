// Package ops implements the operational commands around the
// publish/build flow: ls (PDS vs local diff), doctor (health check),
// and unpublish (delete a record).
package ops

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"aparker.io/fair/internal/atproto"
	"aparker.io/fair/internal/markdown"
	"aparker.io/fair/internal/publish"
)

// Ls row statuses, in display order.
const (
	StatusSynced    = "synced"     // record on PDS and a local post claim the same rkey
	StatusConflict  = "conflict"   // more than one local post claims the same rkey
	StatusPDSOnly   = "pds-only"   // record on PDS with no local source
	StatusLocalOnly = "local-only" // local post with no record on PDS
)

// LsRow is one row of the `fair ls` output.
type LsRow struct {
	Status string
	Rkey   string // empty for a local post that has never been published
	Title  string
	Path   string // local path relative to blog_posts; the record's path field for pds-only
	Detail string
}

// RecordLister is the slice of the authenticated client Ls uses.
type RecordLister interface {
	ListRecords(ctx context.Context, collection, cursor string, limit int) (*atproto.ListRecordsOutput, error)
}

// Ls compares the local authoring tree against the PDS
// site.standard.document collection. Posts are matched by the TID rkey
// in their frontmatter, so the result doesn't depend on local state
// and is the same on any machine with a checkout of the content.
//
// "synced" means the record exists on both sides, not that the content
// is identical — `fair publish --dry-run` answers that per post.
func Ls(ctx context.Context, client RecordLister, blogPosts string) ([]LsRow, error) {
	type pdsDoc struct{ Title, Path string }
	pds := map[string]pdsDoc{}
	for cursor := ""; ; {
		page, err := client.ListRecords(ctx, "site.standard.document", cursor, 100)
		if err != nil {
			return nil, fmt.Errorf("listRecords: %w", err)
		}
		for _, item := range page.Records {
			var v pdsDoc
			_ = json.Unmarshal(item.Value, &v) // best effort: a malformed record still gets a row
			pds[item.URI[strings.LastIndexByte(item.URI, '/')+1:]] = v
		}
		if page.Cursor == "" || len(page.Records) == 0 {
			break
		}
		cursor = page.Cursor
	}

	var rows []LsRow
	claimed := map[string]string{} // rkey -> relative path of the first local post claiming it
	if blogPosts != "" {
		posts, err := publish.FindPosts(blogPosts)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("walk %s: %w", blogPosts, err)
		}
		for _, p := range posts {
			src, err := os.ReadFile(p)
			if err != nil {
				return nil, err
			}
			fm, _, err := markdown.Parse(string(src))
			if err != nil {
				continue // not a publishable post (e.g. a draft without title/date)
			}
			rel, _ := filepath.Rel(blogPosts, p)
			row := LsRow{Rkey: fm.Rkey, Title: fm.Title, Path: rel}
			switch first, dup := claimed[fm.Rkey]; {
			case fm.Rkey == "":
				row.Status, row.Detail = StatusLocalOnly, "never published"
			case dup:
				row.Status, row.Detail = StatusConflict, "same rkey as "+first+" — publishing either overwrites the other"
			default:
				claimed[fm.Rkey] = rel
				if _, onPDS := pds[fm.Rkey]; onPDS {
					row.Status = StatusSynced
				} else {
					row.Status, row.Detail = StatusLocalOnly, "has an rkey but no PDS record (unpublished, or never published)"
				}
			}
			rows = append(rows, row)
		}
	}
	for rkey, doc := range pds {
		if _, ok := claimed[rkey]; !ok {
			rows = append(rows, LsRow{
				Status: StatusPDSOnly,
				Rkey:   rkey,
				Title:  doc.Title,
				Path:   doc.Path,
				Detail: "no local source — `fair unpublish " + rkey + "` to delete",
			})
		}
	}

	rank := map[string]int{StatusSynced: 0, StatusConflict: 1, StatusPDSOnly: 2, StatusLocalOnly: 3}
	slices.SortFunc(rows, func(a, b LsRow) int {
		return cmp.Or(
			cmp.Compare(rank[a.Status], rank[b.Status]),
			cmp.Compare(a.Path, b.Path),
			cmp.Compare(a.Rkey, b.Rkey),
		)
	})
	return rows, nil
}
