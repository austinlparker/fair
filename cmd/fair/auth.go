package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"aparker.io/fair/internal/atproto"
	"github.com/spf13/cobra"
)

// newAuthCmd builds `fair auth ...`: login, logout, status. Each
// subcommand is profile-aware via the inherited persistent flags.
func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage OAuth sessions for the active profile",
	}
	cmd.AddCommand(newAuthLoginCmd())
	cmd.AddCommand(newAuthLogoutCmd())
	cmd.AddCommand(newAuthStatusCmd())
	return cmd
}

func newAuthLoginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Run the OAuth loopback flow against the profile's PDS",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			profile, cfg, err := resolveProfileAndLoad(cmd)
			if err != nil {
				return err
			}

			handle, _ := cmd.Flags().GetString("handle")
			caBundle, _ := cmd.Flags().GetString("ca-bundle")

			configDir, _ := cmd.Flags().GetString("config-dir")
			keyPath := filepath.Join(configDir, "client-key."+profile+".json")
			clientKey, err := atproto.LoadOrCreateClientKey(keyPath)
			if err != nil {
				return err
			}

			rootCAs, err := atproto.LoadExtraRoots(caBundle)
			if err != nil {
				return err
			}

			// Loopback mode (sandbox): use the http://localhost client_id
			// form so the PDS auto-generates client metadata without
			// trying to fetch a document. Prod uses the URL of the
			// metadata document itself (deployed under the profile's
			// domain by `fair emit-client-metadata` + the build flow).
			var clientID string
			if cfg.LoopbackClient {
				clientID = atproto.LoopbackClientID(atproto.LoopbackRedirectPath)
			} else {
				clientID = strings.TrimRight(cfg.Domain, "/") + "/.well-known/oauth-client-metadata.json"
			}

			// SIGINT-aware context so Ctrl-C during the browser hop
			// closes the loopback and exits cleanly.
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()

			session, err := atproto.Login(ctx, atproto.LoginConfig{
				PDSURL:    cfg.PDSURL,
				Handle:    handle,
				ClientID:  clientID,
				ClientKey: clientKey,
				RootCAs:   rootCAs,
			})
			if err != nil {
				return err
			}

			store := atproto.NewFileStore(filepath.Join(configDir, "sessions"))
			if err := store.Save(profile, session); err != nil {
				return fmt.Errorf("save session: %w", err)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "logged in: %s\n", session.DID)
			fmt.Fprintf(out, "  PDS: %s\n", session.PDSURL)
			fmt.Fprintf(out, "  expires at: %s\n", session.ExpiresAt.Format("2006-01-02 15:04:05 MST"))
			return nil
		},
	}
	cmd.Flags().String("handle", "", "optional login_hint for the OAuth authorize page")
	cmd.Flags().String("ca-bundle", "", "PEM file containing extra trusted CAs (sandbox: mkcert root)")
	return cmd
}

func newAuthLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Forget the OAuth session for the active profile",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			profile, _, err := resolveProfileAndLoad(cmd)
			if err != nil {
				return err
			}
			configDir, _ := cmd.Flags().GetString("config-dir")
			store := atproto.NewFileStore(filepath.Join(configDir, "sessions"))
			if err := store.Delete(profile); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "logged out of profile %q\n", profile)
			return nil
		},
	}
}

func newAuthStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the current OAuth session for the active profile",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			profile, _, err := resolveProfileAndLoad(cmd)
			if err != nil {
				return err
			}
			configDir, _ := cmd.Flags().GetString("config-dir")
			store := atproto.NewFileStore(filepath.Join(configDir, "sessions"))
			session, err := store.Load(profile)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "profile:    %s\n", profile)
			fmt.Fprintf(out, "did:        %s\n", session.DID)
			fmt.Fprintf(out, "pds:        %s\n", session.PDSURL)
			fmt.Fprintf(out, "issuer:     %s\n", session.AuthServerIssuer)
			fmt.Fprintf(out, "expires:    %s\n", session.ExpiresAt.Format("2006-01-02 15:04:05 MST"))
			return nil
		},
	}
}
