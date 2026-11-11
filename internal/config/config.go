// Package config loads per-profile fair CLI configuration from TOML files
// and resolves which profile a given invocation should use.
package config

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// Config is the per-profile CLI configuration loaded from TOML.
type Config struct {
	PDSURL            string `toml:"pds_url"`
	DID               string `toml:"did"`
	Domain            string `toml:"domain"`
	PublicationName   string `toml:"publication_name"`
	BlogPosts         string `toml:"blog_posts"`
	StateFile         string `toml:"state_file"`
	ImagesMirror      string `toml:"images_mirror"`
	DistDir           string `toml:"dist_dir"`
	CloudflareProject string `toml:"cloudflare_project"`

	// LoopbackClient switches the OAuth flow to use the special
	// `http://localhost`-style client_id documented in ATProto's
	// oauth-types loopback handling. When true, no client metadata
	// document is fetched by the auth server — required for sandbox
	// because the PDS's SSRF protection blocks fetches to any
	// hostname that resolves to a loopback address.
	LoopbackClient bool `toml:"loopback_client"`

	// SocialLinks: URLs declared as <link rel="me"> in every page,
	// supporting IndieAuth-style social verification. Typical entries
	// are bsky.app and github profile URLs.
	SocialLinks []string `toml:"social_links"`

	// Description: optional publication-wide tagline rendered on the
	// index page and in OpenGraph meta. Falls back empty.
	Description string `toml:"description"`
}

// Load reads a TOML config file from path, decodes it, and validates that
// all required fields are populated. It returns an error if the file cannot
// be read, the TOML cannot be parsed, or any required field is empty.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	var c Config
	if err := toml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	if err := c.validate(); err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	return &c, nil
}

// validate returns an error naming the first missing required field:
// connection details (PDSURL, DID), site identity (Domain,
// PublicationName), and the source directory (BlogPosts). The remaining
// paths (state_file, images_mirror, dist_dir) have no defaults; the
// subcommands that need them check at point of use.
func (c *Config) validate() error {
	required := []struct {
		name  string
		value string
	}{
		{"pds_url", c.PDSURL},
		{"did", c.DID},
		{"domain", c.Domain},
		{"publication_name", c.PublicationName},
		{"blog_posts", c.BlogPosts},
	}
	for _, f := range required {
		if f.value == "" {
			return fmt.Errorf("required field %s is missing or empty", f.name)
		}
	}
	return nil
}
