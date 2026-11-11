// Command fair is the standard.site publishing CLI.
//
// Subcommands are wired in their own files; this file owns only the root.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "fair:", err)
		os.Exit(1)
	}
}

// newRootCmd returns the top-level cobra command with persistent flags
// shared by all subcommands. Subcommands attach via newConfigCmd, etc.,
// rather than init() blocks so tests can build a fresh tree per run.
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "fair",
		Short:         "Publish standard.site lexicon records and render plain HTML",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().String("profile", "", "named profile (or set FAIR_PROFILE)")
	root.PersistentFlags().String("config-dir", defaultConfigDir(), "directory containing config.<profile>.toml files")
	root.PersistentFlags().String("ca-bundle", "", "PEM file containing extra trusted CAs (sandbox: mkcert root)")

	root.AddCommand(newConfigCmd())
	root.AddCommand(newEmitClientMetadataCmd())
	root.AddCommand(newAuthCmd())
	root.AddCommand(newInitCmd())
	root.AddCommand(newPublishCmd())
	root.AddCommand(newBuildCmd())
	root.AddCommand(newLsCmd())
	root.AddCommand(newUnpublishCmd())
	root.AddCommand(newDoctorCmd())
	return root
}
