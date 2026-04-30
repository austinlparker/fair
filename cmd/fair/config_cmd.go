package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newConfigCmd builds `fair config` and its subcommands. Currently only
// `config show` is wired; future additions (e.g., `config set`) hang here.
func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect or modify CLI configuration",
	}
	cmd.AddCommand(newConfigShowCmd())
	return cmd
}

// newConfigShowCmd builds `fair config show`, which resolves the active
// profile and prints the loaded config in TOML-like form. Useful for
// answering "which PDS would `fair publish` actually talk to?"
func newConfigShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print the loaded configuration for the active profile",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			profile, cfg, err := resolveProfileAndLoad(cmd)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "# profile: %s\n", profile)
			fmt.Fprintf(out, "pds_url            = %q\n", cfg.PDSURL)
			fmt.Fprintf(out, "did                = %q\n", cfg.DID)
			fmt.Fprintf(out, "domain             = %q\n", cfg.Domain)
			fmt.Fprintf(out, "publication_name   = %q\n", cfg.PublicationName)
			fmt.Fprintf(out, "blog_posts         = %q\n", cfg.BlogPosts)
			fmt.Fprintf(out, "state_file         = %q\n", cfg.StateFile)
			fmt.Fprintf(out, "images_mirror      = %q\n", cfg.ImagesMirror)
			fmt.Fprintf(out, "dist_dir           = %q\n", cfg.DistDir)
			if cfg.CloudflareProject != "" {
				fmt.Fprintf(out, "cloudflare_project = %q\n", cfg.CloudflareProject)
			}
			return nil
		},
	}
}
