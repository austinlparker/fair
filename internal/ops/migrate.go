package ops

import (
	"context"
	"fmt"

	"aparker.io/fair/internal/atproto"
)

// CleanupWhtwnd deletes all com.whtwnd.blog.entry records in the
// authenticated user's repo. Used after the cutover to standard.site
// is verified — leaves the PDS clean of the prior lexicon.
//
// Safe to run multiple times; deleteRecord is idempotent on missing.
// Returns the count of records that were deleted.
func CleanupWhtwnd(ctx context.Context, client *atproto.Client) (int, error) {
	listing, err := client.ListRecords(ctx, "com.whtwnd.blog.entry", "", 100)
	if err != nil {
		return 0, fmt.Errorf("listRecords: %w", err)
	}

	count := 0
	for _, item := range listing.Records {
		rkey := rkeyFromAtURI(item.URI)
		if err := client.DeleteRecord(ctx, "com.whtwnd.blog.entry", rkey); err != nil {
			return count, fmt.Errorf("delete %s: %w", rkey, err)
		}
		count++
	}
	return count, nil
}
