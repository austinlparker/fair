package main

import (
	"context"
	"crypto/x509"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"aparker.io/fair/internal/atproto"
	"aparker.io/fair/internal/config"
	"github.com/lestrrat-go/jwx/v2/jwk"
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

// sessionStore returns the profile-keyed token store under --config-dir.
func sessionStore(cmd *cobra.Command) *atproto.FileStore {
	configDir, _ := cmd.Flags().GetString("config-dir")
	return atproto.NewFileStore(filepath.Join(configDir, "sessions"))
}

// loadClientKey returns the profile's persistent client signing key,
// creating it on first use.
func loadClientKey(cmd *cobra.Command, profile string) (jwk.Key, error) {
	configDir, _ := cmd.Flags().GetString("config-dir")
	return atproto.LoadOrCreateClientKey(filepath.Join(configDir, "client-key."+profile+".json"))
}

// loadRootCAs reads the --ca-bundle persistent flag. nil means "use
// system trust".
func loadRootCAs(cmd *cobra.Command) (*x509.CertPool, error) {
	caBundle, _ := cmd.Flags().GetString("ca-bundle")
	return atproto.LoadExtraRoots(caBundle)
}

// oauthClientID is the client_id for the profile. Loopback mode
// (sandbox) uses the http://localhost form so the PDS auto-generates
// client metadata without fetching a document; otherwise it's the URL
// of the metadata document the build flow deploys under the profile's
// domain.
func oauthClientID(cfg *config.Config) string {
	if cfg.LoopbackClient {
		return atproto.LoopbackClientID(atproto.LoopbackRedirectPath)
	}
	return strings.TrimRight(cfg.Domain, "/") + "/.well-known/oauth-client-metadata.json"
}

// authed is what an authenticated subcommand needs to do its work.
type authed struct {
	ctx     context.Context
	stop    context.CancelFunc
	profile string
	cfg     *config.Config
	client  *atproto.Client
}

// appPasswordEnv, when set, switches authenticated subcommands from
// the stored OAuth session to a one-shot app-password session. It
// exists for headless runs (CI). Environment only, by design: a flag
// would put the secret in shell history and process listings.
const appPasswordEnv = "FAIR_APP_PASSWORD"

// newAuthed resolves the profile, builds a SIGINT-aware context, and
// returns an authenticated client. Every subcommand that makes
// authenticated XRPC calls goes through here. Callers must defer
// a.stop().
//
// Default mode is the stored OAuth session, refreshed if needed;
// failures surface as "please re-login" errors naming the exact
// command to run. With FAIR_APP_PASSWORD set, it signs in as the
// profile's DID with that app password instead and touches no stored
// session.
func newAuthed(cmd *cobra.Command) (*authed, error) {
	profile, cfg, err := resolveProfileAndLoad(cmd)
	if err != nil {
		return nil, err
	}
	rootCAs, err := loadRootCAs(cmd)
	if err != nil {
		return nil, err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)

	if password := os.Getenv(appPasswordEnv); password != "" {
		client, err := atproto.NewAppPasswordClient(ctx, cfg.PDSURL, cfg.DID, password, rootCAs)
		if err != nil {
			stop()
			return nil, fmt.Errorf("app-password login (%s): %w", appPasswordEnv, err)
		}
		return &authed{ctx: ctx, stop: stop, profile: profile, cfg: cfg, client: client}, nil
	}

	client, err := atproto.NewClient(profile, sessionStore(cmd), rootCAs)
	if err != nil {
		stop()
		return nil, fmt.Errorf("%w (run `fair --profile %s auth login`)", err, profile)
	}
	clientKey, err := loadClientKey(cmd, profile)
	if err != nil {
		stop()
		return nil, err
	}
	if err := client.RefreshIfNeeded(ctx, oauthClientID(cfg), clientKey, rootCAs); err != nil {
		stop()
		return nil, fmt.Errorf("refresh session: %w (try `fair --profile %s auth login`)", err, profile)
	}
	return &authed{ctx: ctx, stop: stop, profile: profile, cfg: cfg, client: client}, nil
}
