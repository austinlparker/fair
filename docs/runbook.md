# First-deploy runbook

How to take a freshly-built `fair` binary, your PDS account, and a
domain, and end up with a live `standard.site`-rendered blog. Total
wall-clock: ~30 min if nothing goes sideways.

## Pre-flight

- [ ] All tests passing: `go test ./...` green
- [ ] Sandbox verified end-to-end at least once: `fair --profile sandbox auth login` → `init publication` → `publish` → `build` → eyeball
- [ ] Your authoring directory (e.g., `~/code/blog-posts`) is clean and committed somewhere — `fair publish` mutates frontmatter on first publish to write back ULIDs, plus copies inline images into the configured `images_mirror`
- [ ] You're prepared to deploy a near-empty `dist/` to your domain once before first auth login (cold-start contract; the OAuth client metadata document needs to live at the prod domain before the auth server can fetch it)

## 1. Set up the prod profile

Create `~/.config/fair/config.prod.toml`:

```toml
pds_url           = "https://bsky.social"   # or your actual PDS endpoint
did               = "did:plc:..."           # your real DID (resolve via DNS or your PDS)
domain            = "https://example.com"
publication_name  = "Your Name"
description       = "..."                   # optional tagline
blog_posts        = "/path/to/blog-posts"
state_file        = ".fair/state.prod.json"
# Mirror lives in the content repo so the deploy CI can read images
# from a git clone (no PDS blob fetch for inline images).
images_mirror     = "/path/to/blog-posts/images-mirror"
dist_dir          = "dist/"
cloudflare_project = "your-pages-project"   # if you use a fair deploy wrapper later
loopback_client   = false                   # prod fetches the metadata doc
social_links      = [
  "https://bsky.app/profile/<your-handle>",
  # add github, mastodon, etc.
]
```

Resolve your DID via the PDS-independent paths (in preference order):

```bash
# 1. DNS TXT — canonical, no infrastructure dependency
dig +short TXT _atproto.<your-handle> | tr -d '"' | sed 's/^did=//'

# 2. HTTPS well-known — works when your handle is a domain you control
curl -fsS https://<your-handle>/.well-known/atproto-did

# 3. PDS XRPC fallback — trust your PDS to resolve. Use *your* PDS, not
#    bsky.social, unless your handle actually lives on bsky.social.
curl -fsS "https://<your-pds>/xrpc/com.atproto.identity.resolveHandle?handle=<your-handle>" | jq -r .did
```

For a domain handle you own, (1) or (2) is the right answer; both work
even if every PDS in the network is down.

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
wrangler pages deploy dist/ --project-name=<your-pages-project>
# Or: configure git-integrated deploys and push
```

Verify:

```bash
curl -fsS https://example.com/.well-known/oauth-client-metadata.json | jq .client_id
# Should echo: https://example.com/.well-known/oauth-client-metadata.json
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

## 5. Publish your posts

```bash
for d in /path/to/blog-posts/*/; do
  ./fair --profile prod publish "$d/index.md"
done
```

Each first publish writes a ULID back to the markdown frontmatter AND
copies inline image bytes into `<blog-posts>/images-mirror/<slug>/`.
Commit both back to the blog-posts repo after this loop:

```bash
cd /path/to/blog-posts
git add . && git status   # eyeball what changed
git commit -m "publish: initial standard.site posts"
git push
```

The `git push` gives inline image bytes durability outside your
laptop. Re-running `fair publish` is idempotent (content-hash skip).

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
wrangler pages deploy dist/ --project-name=<your-pages-project>
```

Build runs entirely locally — `fair build` reads public records from
the PDS (no auth needed) and writes `dist/`. The deploy step is
`wrangler` from your laptop. Two reasons we don't ship CI for this:
publishing already requires the laptop (OAuth session lives there),
and the build is fast enough that `build && deploy` is one shell
command. If you eventually want hands-off rebuilds (e.g., to pick up
out-of-band PDS edits), see the dedicated cron-Worker / Jetstream
options in the design plan.

## 7. Verify

```bash
./fair --profile prod ls
# Expect: every post 'synced'
./fair --profile prod doctor
# Expect: all checks ✓
curl -fsS https://example.com/feed.xml | head -10
curl -fsS https://example.com/.well-known/site.standard.publication
```

Open `https://example.com/` in a browser. Click through one or two posts.
Eyeball OG meta tags via View Source.

Cross-render in an external standard.site reader (e.g., Leaflet) to
confirm interop:
- Navigate to your DID in their UI
- Verify your posts appear with correct metadata

## 8. (Optional) DNS / project bindings

Make sure your custom domain is bound to the Pages project and DNS
points there.

## Rollback (if something goes wrong)

The standard.site records on the PDS are entirely independent of your
deployed site. To roll back the deployment, redeploy the previous
`dist/` (Cloudflare Pages keeps deployment history). To roll back
records, `./fair --profile prod ls`, then `./fair unpublish <rkey>` for
each `site.standard.document` record you want to remove.

If frontmatter ULIDs got written back during a failed run, they're
harmless — re-running `fair publish` will reuse them as rkeys.
