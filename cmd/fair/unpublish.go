package main

import (
	"fmt"

	"aparker.io/fair/internal/ops"
	"github.com/spf13/cobra"
)

// newUnpublishCmd builds `fair unpublish <rkey>`. Deletes the record
// and updates state.json. Local source is untouched.
func newUnpublishCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unpublish <rkey>",
		Short: "Delete a site.standard.document record from the PDS",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newAuthed(cmd)
			if err != nil {
				return err
			}
			defer a.stop()

			rkey := args[0]
			if err := ops.Unpublish(a.ctx, a.client, rkey, a.cfg.StateFile); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "deleted site.standard.document/%s\n", rkey)
			return nil
		},
	}
}
