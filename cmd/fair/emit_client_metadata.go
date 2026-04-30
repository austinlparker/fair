package main

import (
	"fmt"

	"aparker.io/fair/internal/atproto"
	"github.com/spf13/cobra"
)

// newEmitClientMetadataCmd builds `fair emit-client-metadata --out <dir>`,
// which writes the OAuth client metadata document referenced by the
// auth flow's `client_id`. The document is part of the cold-start build
// — it must be deployed under the active profile's domain before any
// `auth login` against that domain can succeed.
//
// The flow is local-only: no PDS reads, no auth required, just config
// + filesystem writes. Native loopback clients use auth_method=none, so
// no client signing key needs to land in the metadata.
func newEmitClientMetadataCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "emit-client-metadata",
		Short: "Write OAuth client metadata to <out>/.well-known/oauth-client-metadata.json",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out, _ := cmd.Flags().GetString("out")
			if out == "" {
				return fmt.Errorf("--out is required")
			}
			_, cfg, err := resolveProfileAndLoad(cmd)
			if err != nil {
				return err
			}
			if err := atproto.EmitClientMetadata(out, cfg.Domain); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "wrote %s/.well-known/oauth-client-metadata.json\n", out)
			return nil
		},
	}
	cmd.Flags().String("out", "", "directory to write into (creates <out>/.well-known/...)")
	return cmd
}
