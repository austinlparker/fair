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

// newMigrateCmd builds `fair migrate` and its cleanup-whtwnd
// subcommand. Lives separately from `init` because migrate is
// inherently destructive (deletes records).
func newMigrateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "One-time migration helpers (legacy lexicon cleanup, etc.)",
	}
	cmd.AddCommand(newMigrateCleanupWhtwndCmd())
	return cmd
}

func newMigrateCleanupWhtwndCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cleanup-whtwnd",
		Short: "Delete all com.whtwnd.blog.entry records (post-cutover cleanup)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			confirm, _ := cmd.Flags().GetBool("yes")
			if !confirm {
				fmt.Fprintln(cmd.OutOrStderr(), "destructive: deletes every com.whtwnd.blog.entry record in your repo.")
				fmt.Fprintln(cmd.OutOrStderr(), "re-run with --yes to proceed.")
				return fmt.Errorf("confirmation required")
			}

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

			count, err := ops.CleanupWhtwnd(ctx, client)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "deleted %d com.whtwnd.blog.entry record(s)\n", count)
			return nil
		},
	}
	cmd.Flags().Bool("yes", false, "confirm deletion")
	cmd.Flags().String("ca-bundle", "", "PEM file containing extra trusted CAs (sandbox: mkcert root)")
	return cmd
}
