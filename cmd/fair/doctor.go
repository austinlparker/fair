package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"aparker.io/fair/internal/ops"
	"github.com/spf13/cobra"
)

// newDoctorCmd builds `fair doctor`. Health-check report against the
// active profile. Exits non-zero when any check fails.
func newDoctorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run health checks against the active profile",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			profile, cfg, err := resolveProfileAndLoad(cmd)
			if err != nil {
				return err
			}
			rootCAs, err := loadRootCAs(cmd)
			if err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()

			checks := ops.Doctor(ctx, ops.DoctorOptions{
				Profile:   profile,
				Cfg:       cfg,
				Store:     sessionStore(cmd),
				RootCAs:   rootCAs,
				StateFile: cfg.StateFile,
			})

			out := cmd.OutOrStdout()
			anyFailed := false
			for _, c := range checks {
				glyph := "✓"
				if !c.OK {
					glyph = "✗"
					anyFailed = true
				}
				fmt.Fprintf(out, "%s  %-30s %s\n", glyph, c.Name, c.Detail)
			}
			if anyFailed {
				return fmt.Errorf("one or more checks failed")
			}
			return nil
		},
	}
	return cmd
}
