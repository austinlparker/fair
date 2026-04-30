package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"aparker.io/fair/internal/atproto"
	"github.com/spf13/cobra"
)

// newInitCmd builds `fair init` and its subcommands. Currently only
// `init publication` is wired; `init local` (config scaffolder) is a
// future addition once we have something to scaffold beyond the TOML.
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
// replaces). Safe to use to update url/name/description after a domain
// switch.
func newInitPublicationCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "publication",
		Short: "Create or update the site.standard.publication record at rkey=self",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
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

			record := map[string]any{
				"$type": "site.standard.publication",
				"url":   cfg.Domain,
				"name":  cfg.PublicationName,
			}
			ref, err := client.PutRecord(ctx, "site.standard.publication", "self", record)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "publication record at %s\n", ref.URI)
			return nil
		},
	}
	cmd.Flags().String("ca-bundle", "", "PEM file containing extra trusted CAs (sandbox: mkcert root)")
	return cmd
}
