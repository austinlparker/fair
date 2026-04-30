package main

import (
	"context"
	"crypto/x509"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"aparker.io/fair/internal/atproto"
	"aparker.io/fair/internal/config"
	"github.com/spf13/cobra"
)

// defaultConfigDir returns ~/.config/fair when HOME is set, falling back
// to "." when it isn't (unusual; only happens in pathological test envs).
// The returned value is used as the default for --config-dir; subcommands
// can read it back from the cobra flag.
func defaultConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "."
	}
	return filepath.Join(home, ".config", "fair")
}

// resolveProfileAndLoad reads --profile and --config-dir from the command,
// falls back to FAIR_PROFILE for the profile, validates the chosen profile
// against the discovered allowlist, and loads the matching config file.
//
// All commands that touch config use this helper so error wording, env
// fallback, and the typo-suggestion behavior stay consistent.
func resolveProfileAndLoad(cmd *cobra.Command) (string, *config.Config, error) {
	flagProfile, _ := cmd.Flags().GetString("profile")
	configDir, _ := cmd.Flags().GetString("config-dir")
	envProfile := os.Getenv("FAIR_PROFILE")

	known, err := config.DiscoverProfiles(configDir)
	if err != nil {
		return "", nil, fmt.Errorf("discover profiles in %s: %w", configDir, err)
	}

	profile, err := config.ResolveProfile(flagProfile, envProfile, known)
	if err != nil {
		return "", nil, err
	}

	cfgPath := filepath.Join(configDir, "config."+profile+".toml")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return "", nil, err
	}
	return profile, cfg, nil
}

// newAuthedClient builds an atproto.Client from the persisted session
// for profile and refreshes the access token if it's expired. Used by
// every subcommand that needs to make authenticated XRPC calls.
//
// The refresh dance:
//   - load client signing key (used for client_assertion JWTs in PAR/refresh)
//   - compute client_id (loopback form for sandbox, metadata-URL for prod)
//   - call Client.RefreshIfNeeded which is a no-op when the session
//     hasn't expired
//
// Failures here surface as clear "please re-login" errors so the user
// knows what to do.
func newAuthedClient(ctx context.Context, cmd *cobra.Command, cfg *config.Config, profile string, rootCAs *x509.CertPool) (*atproto.Client, error) {
	configDir, _ := cmd.Flags().GetString("config-dir")
	store := atproto.NewFileStore(filepath.Join(configDir, "sessions"))
	client, err := atproto.NewClient(profile, store, rootCAs)
	if err != nil {
		return nil, fmt.Errorf("%w (run `fair --profile %s auth login`)", err, profile)
	}

	keyPath := filepath.Join(configDir, "client-key."+profile+".json")
	clientKey, err := atproto.LoadOrCreateClientKey(keyPath)
	if err != nil {
		return nil, err
	}

	var clientID string
	if cfg.LoopbackClient {
		clientID = atproto.LoopbackClientID(atproto.LoopbackRedirectPath)
	} else {
		clientID = strings.TrimRight(cfg.Domain, "/") + "/.well-known/oauth-client-metadata.json"
	}

	if err := client.RefreshIfNeeded(ctx, clientID, clientKey, rootCAs); err != nil {
		return nil, fmt.Errorf("refresh session: %w (try `fair --profile %s auth login`)", err, profile)
	}
	return client, nil
}
