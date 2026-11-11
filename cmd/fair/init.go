package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newInitCmd builds `fair init` and its subcommands.
func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "First-run setup helpers",
	}
	cmd.AddCommand(newInitPublicationCmd())
	return cmd
}

// newInitPublicationCmd builds `fair init publication`, which creates
// the singleton site.standard.publication record at rkey "self".
//
// Idempotent: re-running updates the existing record (putRecord
// replaces). Safe to use to update url/name after a domain switch.
func newInitPublicationCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "publication",
		Short: "Create or update the site.standard.publication record at rkey=self",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := newAuthed(cmd)
			if err != nil {
				return err
			}
			defer a.stop()

			record := map[string]any{
				"$type": "site.standard.publication",
				"url":   a.cfg.Domain,
				"name":  a.cfg.PublicationName,
			}
			ref, err := a.client.PutRecord(a.ctx, "site.standard.publication", "self", record)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "publication record at %s\n", ref.URI)
			return nil
		},
	}
}
