package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

// newPublishCmd builds `fair publish <post.md>...` / `fair publish --all`.
// Drives the orchestrator in internal/publish once per post.
func newPublishCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "publish <path-to-post.md>... | --all",
		Short: "Publish markdown posts as site.standard.document records",
		Args: func(cmd *cobra.Command, args []string) error {
			all, _ := cmd.Flags().GetBool("all")
			if all == (len(args) > 0) {
				return fmt.Errorf("pass one or more post paths, or --all (not both)")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newAuthed(cmd)
			if err != nil {
				return err
			}
			defer a.stop()
			cfg := a.cfg

			// Checked before any network write: discovering a missing
			// state path only after a putRecord succeeded would leave
			// the post published but untracked.
			if cfg.StateFile == "" {
				return fmt.Errorf("state_file is not set in the %s profile config", a.profile)
			}

			dryRun, _ := cmd.Flags().GetBool("dry-run")
			force, _ := cmd.Flags().GetBool("force")
			announce, _ := cmd.Flags().GetBool("announce")

			var sources []string
			if all, _ := cmd.Flags().GetBool("all"); all {
				sources, err = publish.FindPosts(cfg.BlogPosts)
				if err != nil {
					return fmt.Errorf("find posts under %s: %w", cfg.BlogPosts, err)
				}
			} else {
				for _, arg := range args {
					p, err := resolveSourcePath(arg, cfg.BlogPosts)
					if err != nil {
						return err
					}
					sources = append(sources, p)
				}
			}

			pubCfg := publish.Config{
				BlogPosts:      cfg.BlogPosts,
				StateFile:      cfg.StateFile,
				ImagesMirror:   cfg.ImagesMirror,
				PublicationURI: fmt.Sprintf("at://%s/site.standard.publication/self", cfg.DID),
				PublicationURL: cfg.Domain,
				MaxCoverSize:   1 << 20, // 1 MiB
			}
			opts := publish.Options{
				DryRun:   dryRun,
				Force:    force,
				Announce: announce,
				Logger: func(format string, a ...any) {
					fmt.Fprintf(cmd.OutOrStderr(), "  "+format+"\n", a...)
				},
			}

			// Fail fast: posts already published in this run are recorded
			// in state, so a re-run after fixing the bad post skips them.
			out := cmd.OutOrStdout()
			for _, src := range sources {
				result, err := publish.Publish(a.ctx, a.client, src, pubCfg, opts)
				if err != nil {
					return fmt.Errorf("%s: %w", src, err)
				}
				switch {
				case result.Skipped:
					fmt.Fprintf(out, "skipped %s (%s)\n", result.Rkey, result.Reason)
				case dryRun:
					fmt.Fprintf(out, "dry-run: %s would be put at site.standard.document/%s\n", src, result.Rkey)
				default:
					fmt.Fprintf(out, "published %s\n", result.URI)
					if result.BlobUploaded {
						fmt.Fprintf(out, "  cover blob uploaded\n")
					}
					if result.UpdatedAt {
						fmt.Fprintf(out, "  updatedAt advanced\n")
					}
				}
			}
			return nil
		},
	}
	cmd.Flags().Bool("all", false, "publish every index.md under blog_posts (unchanged posts are skipped)")
	cmd.Flags().Bool("dry-run", false, "report what would happen without uploading or putting")
	cmd.Flags().Bool("force", false, "re-put even when content hash is unchanged")
	cmd.Flags().Bool("announce", false, "create an app.bsky.feed.post on first publish and pin it as bskyPostRef (the comment-thread root)")
	return cmd
}
