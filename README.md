# fair

A Go CLI that publishes markdown posts to an [ATProto](https://atproto.com)
PDS as [`standard.site`](https://standard.site) lexicon records, then
renders them as plain HTML — no CSS, no JavaScript — for static deploy.

The PDS is the source of truth for what the site shows. Markdown files
on disk are working copies. Re-running `fair publish` is idempotent
(short-circuits when the content hash hasn't changed). External
standard.site readers (Leaflet, pckt, indexers) see the same records
your site does.

## Features

- **Single static binary.** `go build` and you're done.
- **PDS canonical.** Site renders from `site.standard.document` records,
  not from your local markdown.
- **OAuth (loopback + DPoP).** Native ATProto OAuth flow against your
  PDS. No app passwords.
- **Idempotent publish.** SHA-256 over canonical inputs (markdown body +
  sorted/deduped tags + cover-source-hash); `--dry-run` and `--force`
  available.
- **Pure HTML output.** Semantic markup, native widgets (`<details>`,
  `<progress>`, `<meter>`, `<figure>`, etc.), browser default styling.
  The only CSS shipped is a two-rule inline block — one constrains the
  reading column to ~720px on desktop, one keeps images from overflowing
  it. No stylesheet, no framework, no design system; everything else
  uses browser defaults.
- **Responsive images.** `fair publish` generates resized variants
  (480/960/1440/1920) alongside each inline image; rendered HTML emits
  `srcset` + `sizes` so phones fetch a small variant and big screens
  fetch a sharp one.
- **Native widgets:**
  [demo article](https://aparker.io/html-feature-demo/) shows
  what's possible with no CSS or JS.
- **Discovery & sharing:** OpenGraph, Twitter cards, canonical links,
  `rel=me` social verification, favicon from `publication.icon` blob.
- **Alternate formats:** Atom + RSS 2.0 + JSON Feed + `index.json` +
  `sitemap.xml` + `robots.txt`, all generated.
- **Tag pages, archive page, prev/next navigation** between posts.
- **Year-grouped home page** with native `<details>` disclosures; current
  year expanded by default.
- **Optional home-page intro:** drop `_index.md` in your blog-posts
  directory and it renders above the year disclosures.
- **Local sandbox** for end-to-end testing without touching prod
  (Caddy + mkcert + `pds.localtest.me` + a real PDS Docker container).
- **Operational tools:** `ls` (PDS-vs-local diff), `doctor` (health
  checks), `unpublish`.

## Status

Alpha but feature-complete for single-author personal use. See
[`docs/design-plans/2026-04-29-blog-v2026.md`](docs/design-plans/2026-04-29-blog-v2026.md)
for the validated architecture (historical) and
[`docs/runbook.md`](docs/runbook.md) for the first-deploy walkthrough.

## Quick start

```bash
# Build
go build -o fair ./cmd/fair

# Configure your profile
mkdir -p ~/.config/fair
cat > ~/.config/fair/config.prod.toml <<'EOF'
pds_url           = "https://bsky.social"
did               = "did:plc:..."          # your DID
domain            = "https://aparker.io"
publication_name  = "Your Blog"
description       = "Tagline"
blog_posts        = "/path/to/blog-posts"
state_file        = ".fair/state.prod.json"
# For prod, mirror lives inside the content repo so deploy CI can read
# inline image bytes from a git clone (no PDS blob fetch for inline images).
images_mirror     = "/path/to/blog-posts/images-mirror"
dist_dir          = "dist/"
social_links      = ["https://bsky.app/profile/handle.bsky.social"]
EOF

# Cold-start build + deploy (publishes the OAuth client metadata
# document the auth flow needs to fetch)
./fair --profile prod build
wrangler pages deploy dist/

# Auth + create publication record
./fair --profile prod auth login --handle handle.bsky.social
./fair --profile prod init publication

# Publish a post
./fair --profile prod publish blog-posts/my-post/index.md

# Persist the frontmatter id-write + any new images in images-mirror/
( cd /path/to/blog-posts && git add . && git commit -m 'publish: my-post' && git push )

# Render & deploy
./fair --profile prod build
wrangler pages deploy dist/
```

## Subcommand reference

| Command | What it does |
|---|---|
| `auth login` | OAuth loopback flow against the active profile's PDS |
| `auth status` | Show the persisted session |
| `auth logout` | Clear the session |
| `init publication` | Create/update the singleton `site.standard.publication/self` record |
| `emit-client-metadata --out <dir>` | Write OAuth client metadata JSON |
| `publish <path>` | Publish a markdown post (`--dry-run`, `--force` available) |
| `build` | Render all PDS records to `dist/` (no auth required) |
| `ls` | Diff PDS records against local markdown |
| `doctor` | Five-check health report |
| `unpublish <rkey>` | Delete a `site.standard.document` record |
| `config show` | Print loaded config for the active profile |

Profile is **mandatory** — pass `--profile <name>` or set
`FAIR_PROFILE`. There is no default. Configs live at
`~/.config/fair/config.<profile>.toml`.

## Sandbox

The repo includes a fully-functional local sandbox PDS for end-to-end
testing without touching production:

```bash
sandbox/seed.sh        # one-time bootstrap (Docker compose + mkcert + test account)
./fair --profile sandbox auth login --ca-bundle "$(mkcert -CAROOT)/rootCA.pem" \
  --handle aparker.pds.localtest.me
# … publish, build, eyeball at http://localhost:8000/ via dist-preview …
sandbox/reset.sh       # full teardown
```

See [`sandbox/README.md`](sandbox/README.md) for why we use
`pds.localtest.me` + Caddy + mkcert (the upstream PDS image rejects
loopback hostnames in OAuth issuer URLs, so we work around it with a
public-DNS-but-resolves-to-127.0.0.1 hostname).

## Building from source

Requirements:

- Go 1.26+
- Docker (for the sandbox)
- `mkcert` (for the sandbox; `brew install mkcert`)
- macOS or Linux (Windows works for prod commands but not sandbox)

```bash
git clone https://tangled.sh/aparker.io/fair
cd fair
go build -o fair ./cmd/fair
go test ./...
```

## Project layout

```
cmd/fair/             # CLI entrypoints (one file per subcommand)
internal/atproto/     # OAuth + XRPC client + session storage
internal/build/       # PDS records → dist/ HTML rendering
internal/config/      # TOML config + profile resolution
internal/markdown/    # Normalize + frontmatter + hash + goldmark parser
internal/ops/         # ls, doctor, unpublish
internal/publish/     # 13-step publish orchestrator + building blocks
sandbox/              # Local PDS Docker compose + Caddy + scripts
docs/design-plans/    # Validated architecture
docs/runbook.md          # First-deploy runbook
```

## License

MIT — see [`LICENSE`](LICENSE).

## Acknowledgments

- [standard.site](https://standard.site) — the lexicon being implemented
- [haileyok/atproto-oauth-golang](https://github.com/haileyok/atproto-oauth-golang) — the OAuth client library
- [bluesky-social/indigo](https://github.com/bluesky-social/indigo) — XRPC plumbing
- [yuin/goldmark](https://github.com/yuin/goldmark) — markdown rendering
