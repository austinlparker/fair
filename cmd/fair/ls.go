package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"text/tabwriter"

	"aparker.io/fair/internal/atproto"
	"aparker.io/fair/internal/ops"
	"github.com/spf13/cobra"
)

// newLsCmd builds `fair ls`. Diffs PDS records against local
// blog-posts/ and prints a tabular status report.
func newLsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "Show PDS records vs local markdown drift status",
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

			rows, err := ops.Ls(ctx, client, cfg.BlogPosts, cfg.StateFile)
			if err != nil {
				return err
			}

			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "STATUS\tRKEY\tID\tTITLE\tNOTE")
			for _, r := range rows {
				note := r.Detail
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
					r.Status, r.Rkey, truncate(r.ID, 12), truncate(r.Title, 50), note)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().String("ca-bundle", "", "PEM file containing extra trusted CAs (sandbox: mkcert root)")
	return cmd
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max < 4 {
		return s[:max]
	}
	return s[:max-3] + "..."
}
