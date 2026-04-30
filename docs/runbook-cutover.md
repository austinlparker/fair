# Cutover runbook: pds-blog (Next.js + WhiteWind) → blog-v2026 (Go + standard.site)

Single-author production migration. Total wall-clock: ~30 min if nothing
goes sideways. Safer to do this on a slow afternoon than on a Friday.

## Pre-flight

- [ ] All Phase 1–6 tests passing: `go test ./...` green
- [ ] Sandbox verified end-to-end at least once: `fair --profile sandbox auth login` → `init publication` → `publish` → `build` → eyeball
- [ ] `~/code/blog-posts` clean and committed (this CLI mutates frontmatter on first publish to write back ULIDs)
- [ ] You're prepared to deploy a near-empty `dist/` to aparker.io once before first auth login (cold-start contract; the OAuth client metadata document needs to live at the prod domain before the AS can fetch it)

## 1. Set up the prod profile

Create `~/.config/fair/config.prod.toml`:

```toml
pds_url           = "https://bsky.social"   # or your actual PDS endpoint
did               = "did:plc:..."           # your real DID (resolve via bsky)
domain            = "https://aparker.io"
publication_name  = "Austin Parker"
description       = "..."                   # optional tagline
blog_posts        = "/Users/austinparker/code/blog-posts"
state_file        = ".fair/state.prod.json"
images_mirror     = "images-mirror.prod/"
dist_dir          = "dist/"
cloudflare_project = "aparker-blog"         # if you use fair deploy later
loopback_client   = false                   # prod fetches the metadata doc
social_links      = [
  "https://bsky.app/profile/<your-handle>",
  # add github, mastodon, etc.
]
# loopback_client deliberately omitted (defaults to false). Prod uses
# the published metadata document, not the localhost client_id form.
```

Resolve your DID with:

```bash
curl -s "https://bsky.social/xrpc/com.atproto.identity.resolveHandle?handle=<your-handle>" | jq .did
```

## 2. Cold-start build + first deploy

Build with no auth, no records — emits the empty site + the OAuth client
metadata that `auth login` will need to fetch.

```bash
./fair --profile prod build
ls dist/
# Should include: .well-known/oauth-client-metadata.json
```

Deploy:

```bash
wrangler pages deploy dist/ --project-name=aparker-blog
# Or: configure git-integrated deploys and push
```

Verify:

```bash
curl -fsS https://aparker.io/.well-known/oauth-client-metadata.json | jq .client_id
# Should echo: https://aparker.io/.well-known/oauth-client-metadata.json
```

## 3. Auth login (browser hop)

```bash
./fair --profile prod auth login --handle <your-handle>
```

The browser opens to your PDS's OAuth consent page; review the requested
scopes (atproto + repo:site.standard.* + blob), approve, redirect lands
on the loopback. `auth status` should now show your DID.

## 4. Create the publication record

```bash
./fair --profile prod init publication
# > publication record at at://did:plc:.../site.standard.publication/self
```

## 5. Publish all 11 posts

```bash
for d in ~/code/blog-posts/*/; do
  ./fair --profile prod publish "$d/index.md"
done
```

Each first publish writes a ULID back to the markdown frontmatter — commit
those changes back to ~/code/blog-posts after this loop. Re-running is
idempotent (content-hash skip).

Watch for:
- **Cover too large**: rejected client-side at 1MB. Convert/downscale and
  retry the affected post.
- **Slug grammar errors**: directory names must match `[a-z0-9-]+`. Rename
  directories (or set explicit `slug:` in frontmatter) before publishing.
- **Reserved slugs**: `self`, `index`, `feed`, `sitemap`, `.well-known` are
  rejected.

## 6. Final build + deploy

```bash
./fair --profile prod build
wrangler pages deploy dist/ --project-name=aparker-blog
```

## 7. Verify

```bash
./fair --profile prod ls
# Expect: 11 'synced' rows
./fair --profile prod doctor
# Expect: all 5 checks ✓
curl -fsS https://aparker.io/feed.xml | head -10
curl -fsS https://aparker.io/.well-known/site.standard.publication
```

Open https://aparker.io/ in a browser. Click through one or two posts.
Eyeball OG meta tags via View Source.

Cross-render in an external standard.site reader (e.g., Leaflet) to
confirm interop:
- Navigate to your DID in their UI
- Verify your posts appear with correct metadata

## 8. (Optional) DNS / project bindings

Whatever name you use for the Cloudflare Pages project, make sure the
custom domain `aparker.io` is bound to it and DNS points there.

## 9. (After ~1 week of stable operation) Cleanup

When you've confirmed nothing depends on the legacy WhiteWind records:

```bash
./fair --profile prod migrate cleanup-whtwnd --yes
```

Deletes every `com.whtwnd.blog.entry` record in your repo. Idempotent
on missing.

## 10. Archive the old repo

```bash
cd ~/code/pds-blog
git tag archived-2026-04-30
# Then archive the GitHub repo via the web UI (Settings → Archive).
```

Don't delete it — keep it around in case you ever want to look back at
the old render.

## Rollback (if something goes wrong)

The standard.site records and the WhiteWind records coexist on the PDS
until step 9 is run. To roll back:

1. Re-deploy the old Next.js site to aparker.io (it still reads
   `com.whtwnd.blog.entry` records).
2. Optionally `./fair --profile prod ls`, then `./fair unpublish <rkey>`
   each `site.standard.document` record to remove the new lexicon.
3. Don't run `migrate cleanup-whtwnd` until you're sure.

The blog-posts repo got ULIDs written back; those are harmless but you
can manually remove the `id:` lines if you want a clean slate.
