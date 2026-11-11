package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

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
			clientKey, err := loadClientKey(cmd, profile)
			if err != nil {
				return err
			}
			rootCAs, err := loadRootCAs(cmd)
			if err != nil {
				return err
			}

			// SIGINT-aware context so Ctrl-C during the browser hop
			// closes the loopback and exits cleanly.
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()

			session, err := atproto.Login(ctx, atproto.LoginConfig{
				PDSURL:    cfg.PDSURL,
				Handle:    handle,
				ClientID:  oauthClientID(cfg),
				ClientKey: clientKey,
				RootCAs:   rootCAs,
			})
			if err != nil {
				return err
			}

			if err := sessionStore(cmd).Save(profile, session); err != nil {
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
			if err := sessionStore(cmd).Delete(profile); err != nil {
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
			session, err := sessionStore(cmd).Load(profile)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "profile:    %s\n", profile)
			fmt.Fprintf(out, "did:        %s\n", session.DID)
			fmt.Fprintf(out, "pds:        %s\n", session.PDSURL)
			fmt.Fprintf(out, "issuer:     %s\n", session.AuthServerIssuer)
			fmt.Fprintf(out, "expires:    %s (access token; refreshed automatically)\n", session.ExpiresAt.Format("2006-01-02 15:04:05 MST"))
			return nil
		},
	}
}
