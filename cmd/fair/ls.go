package main

import (
	"fmt"
	"text/tabwriter"

	"aparker.io/fair/internal/ops"
	"github.com/spf13/cobra"
)

// newLsCmd builds `fair ls`. Diffs PDS records against the local
// authoring directory and prints a tabular status report.
func newLsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "Show PDS records vs local markdown drift status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := newAuthed(cmd)
			if err != nil {
				return err
			}
			defer a.stop()

			rows, err := ops.Ls(a.ctx, a.client, a.cfg.BlogPosts)
			if err != nil {
				return err
			}

			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "STATUS\tRKEY\tTITLE\tPATH\tNOTE")
			for _, r := range rows {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
					r.Status, r.Rkey, truncate(r.Title, 50), r.Path, r.Detail)
			}
			return tw.Flush()
		},
	}
}

// truncate shortens s to at most max runes, marking the cut with "...".
func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-3]) + "..."
}
