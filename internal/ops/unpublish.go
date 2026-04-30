package ops

import (
	"context"
	"fmt"

	"aparker.io/fair/internal/atproto"
	"aparker.io/fair/internal/publish"
)

// Unpublish deletes the site.standard.document record at rkey from the
// PDS and removes the corresponding entry from state.json. The local
// markdown file is NOT touched — by convention the source is a working
// copy and the user decides what to do with it.
//
// Idempotent: deleting a missing record is silently OK.
func Unpublish(ctx context.Context, client *atproto.Client, rkey, stateFile string) error {
	if rkey == "" {
		return fmt.Errorf("unpublish: rkey required")
	}
	if err := client.DeleteRecord(ctx, "site.standard.document", rkey); err != nil {
		return fmt.Errorf("delete %s: %w", rkey, err)
	}
	state, err := publish.LoadState(stateFile)
	if err != nil {
		return err
	}
	if _, ok := state.Entries[rkey]; ok {
		delete(state.Entries, rkey)
		if err := state.Save(stateFile); err != nil {
			return fmt.Errorf("update state: %w", err)
		}
	}
	return nil
}
