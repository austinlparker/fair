package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"aparker.io/fair/internal/atproto"
	"aparker.io/fair/internal/build"
	"github.com/spf13/cobra"
)

// newBuildCmd builds `fair build`. Read-only — no auth required.
// Reads PDS records, renders to <DistDir>.tmp/, atomically promotes
// to <DistDir>/.
func newBuildCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Render the active profile's PDS records into a static HTML site",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, cfg, err := resolveProfileAndLoad(cmd)
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

			result, err := build.Build(ctx, build.Config{
				PDSURL:          cfg.PDSURL,
				DID:             cfg.DID,
				PublicationURL:  cfg.Domain,
				PublicationName: cfg.PublicationName,
				Description:     cfg.Description,
				SocialLinks:     cfg.SocialLinks,
				ImagesMirror:    cfg.ImagesMirror,
				DistDir:         cfg.DistDir,
				CAPool:          rootCAs,
			})
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			if result.ColdStart {
				fmt.Fprintf(out, "cold-start build: %s (no publication record yet)\n", result.OutDir)
				return nil
			}
			fmt.Fprintf(out, "built %d posts in %s\n", result.PostCount, result.OutDir)
			return nil
		},
	}
	cmd.Flags().String("ca-bundle", "", "PEM file containing extra trusted CAs (sandbox: mkcert root)")
	return cmd
}
