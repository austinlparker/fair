package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"aparker.io/fair/internal/atproto"
	"aparker.io/fair/internal/publish"
	"github.com/spf13/cobra"
)

// resolveSourcePath finds a markdown file by trying the user-supplied
// path as-given first, then as a path relative to blog-posts.
//
// Cases handled:
//   - Absolute path: used as-is.
//   - Relative path that exists in CWD: used.
//   - Relative path that doesn't exist in CWD but does under blogPosts: used.
//   - Relative path with redundant blog-posts prefix: stripped and tried
//     under blogPosts (e.g., user types "blog-posts/foo/index.md" from a
//     different cwd).
func resolveSourcePath(arg, blogPosts string) (string, error) {
	abs, err := filepath.Abs(arg)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(abs); err == nil {
		return abs, nil
	}
	if blogPosts == "" {
		return "", fmt.Errorf("file not found: %s (and no blog_posts configured to search)", arg)
	}
	// Try arg verbatim under blogPosts.
	candidate := filepath.Join(blogPosts, arg)
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}
	// Strip a leading "blog-posts/" if present (user copy-pasted from
	// a different cwd) and retry.
	if stripped := strings.TrimPrefix(arg, filepath.Base(blogPosts)+string(filepath.Separator)); stripped != arg {
		candidate = filepath.Join(blogPosts, stripped)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("file not found: %q (tried %s and %s/%s)", arg, abs, blogPosts, arg)
}

// newPublishCmd builds `fair publish <path/to/post.md>`. Drives the
// 13-step orchestrator in internal/publish.
func newPublishCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "publish <path-to-post.md>",
		Short: "Publish a markdown post as a site.standard.document record",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			profile, cfg, err := resolveProfileAndLoad(cmd)
			if err != nil {
				return err
			}

			dryRun, _ := cmd.Flags().GetBool("dry-run")
			force, _ := cmd.Flags().GetBool("force")
			caBundle, _ := cmd.Flags().GetString("ca-bundle")

			rootCAs, err := atproto.LoadExtraRoots(caBundle)
			if err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()

			client, err := newAuthedClient(ctx, cmd, cfg, profile, rootCAs)
			if err != nil {
				return err
			}

			sourcePath, err := resolveSourcePath(args[0], cfg.BlogPosts)
			if err != nil {
				return err
			}

			pubURI := fmt.Sprintf("at://%s/site.standard.publication/self", cfg.DID)

			result, err := publish.Publish(ctx, client, sourcePath, publish.Config{
				BlogPosts:      cfg.BlogPosts,
				StateFile:      cfg.StateFile,
				ImagesMirror:   cfg.ImagesMirror,
				PublicationURI: pubURI,
				MaxCoverSize:   1 << 20, // 1 MiB
			}, publish.Options{
				DryRun: dryRun,
				Force:  force,
				Logger: func(format string, a ...any) {
					fmt.Fprintf(cmd.OutOrStderr(), "  "+format+"\n", a...)
				},
			})
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			if result.Skipped {
				fmt.Fprintf(out, "skipped %s (%s)\n", result.Rkey, result.Reason)
				return nil
			}
			if dryRun {
				fmt.Fprintf(out, "dry-run: %s would be put at site.standard.document/%s\n", result.Rkey, result.Rkey)
				return nil
			}
			fmt.Fprintf(out, "published %s\n", result.URI)
			if result.BlobUploaded {
				fmt.Fprintf(out, "  cover blob uploaded\n")
			}
			if result.UpdatedAt {
				fmt.Fprintf(out, "  updatedAt advanced\n")
			}
			return nil
		},
	}
	cmd.Flags().Bool("dry-run", false, "report what would happen without uploading or putting")
	cmd.Flags().Bool("force", false, "re-put even when content hash is unchanged")
	cmd.Flags().String("ca-bundle", "", "PEM file containing extra trusted CAs (sandbox: mkcert root)")
	return cmd
}
