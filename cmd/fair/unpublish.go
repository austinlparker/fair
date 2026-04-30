package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"aparker.io/fair/internal/atproto"
	"aparker.io/fair/internal/ops"
	"github.com/spf13/cobra"
)

// newUnpublishCmd builds `fair unpublish <rkey>`. Deletes the record
// and updates state.json. Local source is untouched.
func newUnpublishCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unpublish <rkey>",
		Short: "Delete a site.standard.document record from the PDS",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			profile, cfg, err := resolveProfileAndLoad(cmd)
			if err != nil {
				return err
			}
			caBundle, _ := cmd.Flags().GetString("ca-bundle")
			rootCAs, err := atproto.LoadExtraRoots(caBundle)
			if err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()

			client, err := newAuthedClient(ctx, cmd, cfg, profile, rootCAs)
			if err != nil {
				return err
			}

			rkey := args[0]
			if err := ops.Unpublish(ctx, client, rkey, cfg.StateFile); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "deleted site.standard.document/%s\n", rkey)
			return nil
		},
	}
	cmd.Flags().String("ca-bundle", "", "PEM file containing extra trusted CAs (sandbox: mkcert root)")
	return cmd
}
