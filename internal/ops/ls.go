// Package ops implements the operational livability commands the
// publish/build flow needs to be pleasant: ls (PDS vs local diff),
// doctor (health check), pull (PDS -> markdown recovery), unpublish
// (delete a record), rename (atomic re-rkey).
package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"aparker.io/fair/internal/atproto"
	"aparker.io/fair/internal/markdown"
	"aparker.io/fair/internal/publish"
)

// LsRow is one row of the `fair ls` output. Status reflects whether
// each post is on PDS only, local only, in both, or in conflict.
type LsRow struct {
	Status string // "synced" | "pds-only" | "local-only" | "conflict"
	Rkey   string // rkey on PDS (empty for local-only)
	ID     string // ULID from frontmatter or PDS record (empty when missing)
	Title  string // best-known title
	Path   string // local file path (empty for pds-only) or PDS path
	Detail string // free-form note (e.g., conflict description)
}

// Ls compares the local blog-posts/ tree against the PDS
// site.standard.document collection. Source of truth for identity is
// the ULID id in frontmatter (local) and the matching state.json entry
// (PDS-side rkey lookup).
//
// Returned rows are sorted: synced first, then conflicts, pds-only,
// local-only.
func Ls(ctx context.Context, client *atproto.Client, blogPosts, stateFile string) ([]LsRow, error) {
	state, err := publish.LoadState(stateFile)
	if err != nil {
		return nil, err
	}

	// Load PDS records.
	listing, err := client.ListRecords(ctx, "site.standard.document", "", 100)
	if err != nil {
		return nil, fmt.Errorf("listRecords: %w", err)
	}
	pdsByRkey := map[string]pdsDoc{}
	for _, item := range listing.Records {
		var v struct {
			Title string `json:"title"`
			Path  string `json:"path"`
		}
		_ = json.Unmarshal(item.Value, &v)
		rkey := rkeyFromAtURI(item.URI)
		entry, _ := state.Entries[rkey]
		pdsByRkey[rkey] = pdsDoc{
			URI:   item.URI,
			Title: v.Title,
			Path:  v.Path,
			ID:    entry.ID,
		}
	}

	// Walk blog-posts/.
	localByID := map[string]localPost{}
	localUnclaimed := []localPost{} // no id in frontmatter yet
	if blogPosts != "" {
		err := filepath.WalkDir(blogPosts, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || filepath.Base(p) != "index.md" {
				return nil
			}
			src, readErr := os.ReadFile(p)
			if readErr != nil {
				return nil
			}
			fm, _, parseErr := markdown.Parse(string(src))
			if parseErr != nil {
				// Not a valid post; skip
				return nil
			}
			lp := localPost{Path: p, Title: fm.Title, ID: fm.ID}
			if fm.ID == "" {
				localUnclaimed = append(localUnclaimed, lp)
			} else {
				localByID[fm.ID] = lp
			}
			return nil
		})
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("walk blog-posts: %w", err)
		}
	}

	rows := []LsRow{}

	// Cross-reference by ID via state.json: state.Entries[rkey].ID
	// gives us the PDS record's ID; localByID gives us the local file's ID.
	for rkey, doc := range pdsByRkey {
		if doc.ID == "" {
			// PDS has the record but we don't have the ID locally
			// (state.json missing or older than this record). Show as
			// pds-only with a hint; doctor / pull would reconcile.
			rows = append(rows, LsRow{
				Status: "pds-only",
				Rkey:   rkey,
				Title:  doc.Title,
				Path:   doc.Path,
				Detail: "no local state entry — run `fair doctor` to rebuild state",
			})
			continue
		}
		local, hasLocal := localByID[doc.ID]
		if hasLocal {
			rel, _ := filepath.Rel(blogPosts, local.Path)
			rows = append(rows, LsRow{
				Status: "synced",
				Rkey:   rkey,
				ID:     doc.ID,
				Title:  doc.Title,
				Path:   rel,
			})
			delete(localByID, doc.ID)
		} else {
			rows = append(rows, LsRow{
				Status: "pds-only",
				Rkey:   rkey,
				ID:     doc.ID,
				Title:  doc.Title,
				Path:   doc.Path,
				Detail: "local source missing — `fair pull` to recover or `fair unpublish` to delete",
			})
		}
	}

	// Anything left in localByID is local-only (id was assigned but
	// the record isn't on PDS, e.g., never published or deleted).
	for id, lp := range localByID {
		rel, _ := filepath.Rel(blogPosts, lp.Path)
		rows = append(rows, LsRow{
			Status: "local-only",
			ID:     id,
			Title:  lp.Title,
			Path:   rel,
			Detail: "id present but no PDS record — run `fair publish`",
		})
	}
	for _, lp := range localUnclaimed {
		rel, _ := filepath.Rel(blogPosts, lp.Path)
		rows = append(rows, LsRow{
			Status: "local-only",
			Title:  lp.Title,
			Path:   rel,
			Detail: "no id assigned — first `fair publish` will create one",
		})
	}

	sort.SliceStable(rows, func(i, j int) bool {
		return statusRank(rows[i].Status) < statusRank(rows[j].Status)
	})

	return rows, nil
}

func statusRank(s string) int {
	switch s {
	case "synced":
		return 0
	case "conflict":
		return 1
	case "pds-only":
		return 2
	case "local-only":
		return 3
	}
	return 99
}

type pdsDoc struct {
	URI   string
	Title string
	Path  string
	ID    string
}

type localPost struct {
	Path  string
	Title string
	ID    string
}

// rkeyFromAtURI returns the rkey portion of an at:// URI like
// at://did:.../site.standard.document/<rkey>.
func rkeyFromAtURI(uri string) string {
	for i := len(uri) - 1; i >= 0; i-- {
		if uri[i] == '/' {
			return uri[i+1:]
		}
	}
	return uri
}
