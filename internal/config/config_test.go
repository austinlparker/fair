package config_test

import (
	"path/filepath"
	"testing"

	"aparker.io/fair/internal/config"
)

func TestLoad_ValidTOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.prod.toml")
	contents := `pds_url           = "https://bsky.social"
did               = "did:plc:abc123"
domain            = "https://aparker.io"
publication_name  = "Austin Parker"
blog_posts        = "/Users/austinparker/code/blog-posts"
state_file        = ".fair/state.prod.json"
images_mirror     = "images-mirror.prod/"
dist_dir          = "dist/"
cloudflare_project = "aparker-blog"
`
	if err := writeFile(path, contents); err != nil {
		t.Fatal(err)
	}

	got, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if got.PDSURL != "https://bsky.social" {
		t.Errorf("PDSURL: got %q, want %q", got.PDSURL, "https://bsky.social")
	}
	if got.DID != "did:plc:abc123" {
		t.Errorf("DID: got %q, want %q", got.DID, "did:plc:abc123")
	}
	if got.Domain != "https://aparker.io" {
		t.Errorf("Domain: got %q, want %q", got.Domain, "https://aparker.io")
	}
	if got.PublicationName != "Austin Parker" {
		t.Errorf("PublicationName: got %q, want %q", got.PublicationName, "Austin Parker")
	}
	if got.BlogPosts != "/Users/austinparker/code/blog-posts" {
		t.Errorf("BlogPosts: got %q", got.BlogPosts)
	}
	if got.StateFile != ".fair/state.prod.json" {
		t.Errorf("StateFile: got %q", got.StateFile)
	}
	if got.ImagesMirror != "images-mirror.prod/" {
		t.Errorf("ImagesMirror: got %q", got.ImagesMirror)
	}
	if got.DistDir != "dist/" {
		t.Errorf("DistDir: got %q", got.DistDir)
	}
	if got.CloudflareProject != "aparker-blog" {
		t.Errorf("CloudflareProject: got %q", got.CloudflareProject)
	}
}

func TestLoad_MissingRequiredField(t *testing.T) {
	cases := []struct {
		name    string
		omit    string
		wantSub string
	}{
		{"pds_url", "pds_url", "pds_url"},
		{"did", "did", "did"},
		{"domain", "domain", "domain"},
		{"publication_name", "publication_name", "publication_name"},
		{"blog_posts", "blog_posts", "blog_posts"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			full := map[string]string{
				"pds_url":          `pds_url = "https://bsky.social"`,
				"did":              `did = "did:plc:abc"`,
				"domain":           `domain = "https://aparker.io"`,
				"publication_name": `publication_name = "Test"`,
				"blog_posts":       `blog_posts = "/tmp/posts"`,
			}
			delete(full, tc.omit)

			path := filepath.Join(t.TempDir(), "c.toml")
			contents := ""
			for _, line := range full {
				contents += line + "\n"
			}
			if err := writeFile(path, contents); err != nil {
				t.Fatal(err)
			}

			_, err := config.Load(path)
			if err == nil {
				t.Fatalf("expected error for missing %s, got nil", tc.omit)
			}
			if !contains(err.Error(), tc.wantSub) {
				t.Errorf("error %q should mention %q", err.Error(), tc.wantSub)
			}
		})
	}
}
