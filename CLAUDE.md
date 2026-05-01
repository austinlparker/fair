# fair

Last reviewed: 2026-04-30 (images-mirror lives in content repo for prod)

A Go CLI that publishes markdown posts to an ATProto PDS as `standard.site`
lexicon records, then renders them as plain HTML for static deploy.

**Source of truth for architecture:** `docs/design-plans/2026-04-29-blog-v2026.md`
(historical, captures the original design intent).
**Operator/user surface:** `README.md`. This file captures only the
non-obvious rules and the things that bite when forgotten.

**Status:** Feature-complete for single-author personal use. The deployment
is whatever the operator wires up — see `docs/runbook.md` for the
recommended first-deploy path.

## Stack

- Go 1.26+, single static binary (`go build -o fair ./cmd/fair`)
- Module path: `aparker.io/fair`
- CLI: `spf13/cobra`
- Markdown: `github.com/yuin/goldmark` (extensions are HASH-SAFE — see
  "Hash determinism" — but they DO change rendered HTML for future builds)
- Frontmatter: `gopkg.in/yaml.v3`
- Config: `github.com/BurntSushi/toml`
- ATProto OAuth: `github.com/haileyok/atproto-oauth-golang@v0.0.3` (pinned —
  upstream pre-1.0; mind the version when bumping)
- License: MIT (see `LICENSE`)

## Domain

- Lexicons: `site.standard.publication`, `site.standard.document`,
  with `site.standard.content.markdown` content variant.
- Records on the PDS are canonical. Markdown files in the configured
  `blog_posts` directory are working copies; `fair publish` is idempotent
  (content-hash skip; `--force` overrides).
- Each blog has its own authoring repo + operator notes — those live
  with the content, not here. See the operator's `CLAUDE.md` in their
  blog-posts directory for the deploy-side specifics.

## Subcommands

`fair --profile <name> <verb>` — see README for full usage. One-line reminder:
`auth login|logout|status`, `init publication`, `publish <path>`,
`unpublish <rkey>`, `build`, `ls`, `doctor`, `config show`,
`emit-client-metadata <out>`.

## Profile mechanism (mandatory, no default)

- Every invocation needs `--profile <name>` or `FAIR_PROFILE`. There is no default.
- Profiles are filesystem-discovered: `config.<name>.toml` in `--config-dir`
  (default `~/.config/fair`). Unknown profile → error with Levenshtein-≤2
  "did you mean" suggestion.
- Profile-scoped state, dist, images-mirror, and keychain entries prevent any
  cross-contamination between sandbox and prod. Keep that isolation when adding
  new state.

## Auto-token-refresh (every authenticated subcommand)

`cmd/fair/profile.go:newAuthedClient` is the single entry point for any
subcommand that needs authenticated XRPC. It loads the persisted session,
the persistent ECDSA client signing key, computes `client_id` (loopback for
sandbox, metadata-URL for prod), and calls `Client.RefreshIfNeeded` — no-op
when fresh, silent token rotation otherwise. Failures surface as
"please re-login" errors that name the exact command to run.

**Do not call `atproto.NewClient` directly from a subcommand** — go through
`newAuthedClient` so refresh stays automatic. `cmd/fair/auth.go` is the only
caller of `atproto.Login` (it *creates* the session rather than using one).

## Hash determinism (do not break casually)

`internal/markdown/Hash` drives `fair publish` idempotency. Any change to
its inputs invalidates every stored record's hash, forcing full republish.

**Hash is over the markdown SOURCE, not goldmark's rendered HTML.**
Specifically: `Hash(NormalizedBody, Canonical(Frontmatter), CoverSourceHash)`,
sections separated by `\x00`. This is the load-bearing correction —
adding/removing goldmark extensions or rendering options does NOT
change stored hashes. It only changes future rendered HTML.

- Body normalized: LF endings, BOM stripped, exactly one trailing newline.
- Frontmatter canonicalized via `Canonical()`: sorted keys, no comments,
  `id`/`updated`/`updatedAt` excluded. Encoded as **rigid line-per-key bytes**,
  not YAML, so `yaml.v3` upgrades don't shift the hash.
- Tags lowercased, trimmed, deduped, sorted.
- Cover hashed by **source-file SHA-256** (not blob CID) so we can short-circuit
  before any upload.

## Markdown rendering (goldmark)

`internal/markdown/Parser()` returns a singleton goldmark configured with:

- GFM (tables, strikethrough, task lists, autolinks)
- Footnote (`[^1]` syntax → `<sup>` + footnote list)
- DefinitionList (`Term\n: Definition` → `<dl>`)
- `WithAutoHeadingID` (stable anchor IDs)
- `WithUnsafe` (HTML passthrough — required for hand-authored
  `<blockquote data-bluesky-uri>` markers the build flow rewrites)

Extension changes are hash-safe (see above) but will alter rendered HTML on
the next `fair build`. Consumers like Leaflet/pckt re-render from source
with their own goldmark config and may produce different HTML than us — that
is expected.

## HTML post-processing (kept out of goldmark deliberately)

`internal/build/render.go` runs three text passes on goldmark's output
inside `RenderMarkdown`, in order. They live outside the parser
specifically so hash stays stable:

1. `resolveRelativeURLs` — joins `publication.url` with any `/...` href/src
   so rendered HTML works on any origin.
2. `transformBlueskyEmbeds` — rewrites `<blockquote data-bluesky-uri>` into
   `<blockquote>...<footer>Discuss on Bluesky</footer></blockquote>` (no JS).
3. `wrapStandaloneImagesAsFigures` — promotes `<p><img></p>` blocks to
   `<figure><img><figcaption>alt</figcaption></figure>`. Inline images
   are left alone.

A fourth pass — `EnrichResponsiveImages` in `internal/build/srcset.go`
— runs in `Build` after `RenderMarkdown` returns (not inside it,
because it needs `ImagesMirror` which the markdown package shouldn't
know about). Adds `srcset`/`sizes`/`width`/`height` to `<img>` tags
whose source has resized variants on disk.

Each pass is a regex on rendered HTML, not a goldmark parser/renderer
extension. Adding new transforms here is safe; modifying the goldmark parser
config to do the same thing is not.

## Domain-portable records

Inline image URLs in PDS record bodies are **relative** (`/images/<slug>/foo.png`).
The build flow joins them with `publication.url` to produce absolute URLs in
rendered HTML. This is a deliberate design choice — keep it relative or
readers like Leaflet/pckt break.

## Inline images: filesystem-staged, not PDS blobs

Inline image bytes are **not** uploaded as PDS blobs (only cover images are).
`fair publish` rewrites `![alt](./foo.png)` to a relative `/images/<slug>/<basename>`
URL in the record body, and copies the source file into the directory
configured as `images_mirror`. Alongside the source, it also generates
resized variants (`<basename>-480.<ext>`, `-960`, `-1440`, `-1920`)
when the source is wider than each target — see
`internal/images/variants.go`. Variants for unsupported formats (gif,
svg, etc.) are skipped silently; that image just gets a plain `<img>`.

`fair build` reads from the mirror and copies into `dist/images/`. A
build-time HTML pass (`internal/build/srcset.go::EnrichResponsiveImages`)
walks `<img>` tags whose src points into `<publicationURL>/images/...`
and rewrites them with `srcset`/`sizes`/`width`/`height` attributes
when variants are present on disk. The pass is a no-op when
`ImagesMirror` is empty or when an image has no variants.

The browser needs two CSS rules for the rendered layout to behave —
they live in one inline `<style>` block in every template `<head>`:

```css
body{max-width:720px;margin:0 auto;padding:1rem}
img{max-width:100%;height:auto}
```

The first caps the reading column to ~720px on desktop and centers it,
which gives a typographically reasonable line length and naturally
constrains images (since they live inside the body). The second makes
images shrink to fit the column on narrow viewports.

The "no CSS" claim in the README is that we don't ship a stylesheet,
framework, or design system — not that we emit zero style attributes.
Body width can't be constrained without CSS for the "narrow on mobile,
narrow on desktop, centered everywhere" case (HTML only supports fixed
`width="N"` on tables, which overflows mobile). The picture-with-media-
queries pattern *can* size images alone without CSS but doesn't help
text, costs more publish-side complexity, and doesn't center the result
— so we keep the two-rule block instead.

The operator decides where `images_mirror` lives. Two common shapes:
- **Inside the content repo** (e.g., `<blog-posts>/images-mirror/`) — the
  mirror travels with the markdown, deploy CI reads it from a git clone,
  no PDS blob fetch needed.
- **Outside the content repo** (e.g., `~/.config/fair/images-mirror.<profile>/`)
  — fine for sandbox/test profiles where the bytes are throwaway between
  resets.

Why not blobs on the PDS: would need a sidecar collection in our own NSID
to record path↔CID mapping (`site.standard.document` is external — not
ours to extend), plus a hash-format bump to fold inline image source
hashes into `Hash()`. Punted to a future phase if/when remote-publish
becomes a real flow. Until then, the operator's chosen mirror location
is the durability story for inline image bytes.

## Build flow (cold-start contract)

`internal/build.Build` is the orchestrator. Key invariant: it works with no
auth, no records, even before any publication exists. It always emits
`.well-known/oauth-client-metadata.json` first (auth needs it on virgin
deploys); on missing publication it emits a minimal cold-start site
(well-known + empty feed/sitemap + stub index/404), atomic-promotes,
returns. Otherwise it paginates `listRecords` (unauthenticated), renders
per-post pages plus index, feed.xml, rss.xml, sitemap.xml, robots.txt,
JSON Feed, `index.json`, tag pages, archive, and copies `ImagesMirror`
into `dist.tmp/images/` before atomic-promoting `dist.tmp` → `dist`.

Templates live in `internal/build/templates/`, embedded via `embed.FS`.
HTML templates use `html/template` (auto-escaping); XML/JSON templates use
`text/template` so XML declarations and JSON braces survive intact.

## OAuth scopes (changing this list can invalidate sessions)

`internal/atproto.BlogScopes` is the single source of truth:
`atproto repo:site.standard.publication repo:site.standard.document repo:app.bsky.feed.post blob`.
Adding a new collection here means existing sessions are missing the new
scope and need re-login (subset removals are fine — existing tokens
gracefully retain access to scopes they were issued under). When adding
a one-time migration scope, drop it from this list once the migration is
complete.

`repo:app.bsky.feed.post` is for `fair publish --announce` — the optional
flow that creates a Bluesky announce post per published article and pins
it as `bskyPostRef`. Drop this scope only if you remove the --announce
feature entirely; otherwise leave it.

## Cobra wiring pattern

Subcommands use `newXxxCmd()` constructors registered via `root.AddCommand`
in `newRootCmd()`. **Do NOT** use `init()` blocks — `cmd/fair/main_test.go`
relies on building a fresh tree per test (`runCLI` calls `newRootCmd()` each
invocation). New subcommands follow the same pattern.

## Sandbox: pds.localtest.me + mkcert + Caddy

The `bluesky-social/pds` image hardcodes `https://` for OAuth issuer URLs and
rejects loopback hostnames. Workaround: `pds.localtest.me` (public DNS that
always resolves to 127.0.0.1, non-loopback per the PDS validator), mkcert
(OS-trusted local CA — run `sudo mkcert -install` once for browser trust;
the Go CLI trusts the CA explicitly regardless), and Caddy (terminates TLS
in front of the PDS). `PDS_SERVICE_HANDLE_DOMAINS=".pds.localtest.me"`
enables handle creation; the seed handle is `aparker.pds.localtest.me`
because `test`/`admin`/etc. are on the PDS's reserved-handle list.

Don't try to "simplify" sandbox to bare localhost — it's been tried and the
PDS rejects the resulting OAuth issuer URL.

## Local preview

`.claude/launch.json` defines two run configs: `sandbox-pds` (docker compose
up) and `dist-preview` (python `http.server` on port 8000 serving
`dist.sandbox/`). Use the latter to browse the rendered site locally
without a webserver setup.

## Test layout

- Unit tests live next to the code (`*_test.go` in the same package).
- Integration tests against the sandbox PDS are gated behind `//go:build sandbox`.
  They require `sandbox/seed.sh` to have run first.
- Cobra tests build a fresh root via `newRootCmd()` per invocation; preserve that.

## Home-page customization (`_index.md`, `_outro.md`)

Two optional files at the root of the authoring directory drive
home-page-only customization. Neither gets published to the PDS — both
are build-time chrome. Standard.site readers (Leaflet, pckt) see only
the records on the PDS, not these files.

**`_index.md`** — body markdown rendered as the intro section between
`<header>` and the year disclosures. Optional YAML frontmatter at the
top drives further home-page-only behaviors. All fields optional:

```yaml
---
description: "Override Publication.Description for the home page only"
subtitle: "Extra <p> between <h1> and the publication description"
pinned:
  - small-software
  - keep-the-internet-weird
hide_years: false
---

[intro markdown body…]
```

- `description` — overrides `publication.description` in the home
  page's `<meta name="description">`, OG tag, twitter card. Useful when
  the publication-record description is too short or is being kept
  generic for standard.site readers.
- `subtitle` — extra `<p>` rendered between the `<h1>` title and the
  publication description.
- `pinned: [slug, ...]` — renders a "Featured" `<section>` between the
  intro and the year disclosures, listing those posts in the order
  given. Slugs that don't match any published post log a warning and
  are skipped.
- `hide_years: true` — suppresses the year-group disclosures (e.g.,
  for a fully-curated home page that only shows pinned posts +
  outro).

The body of `_index.md` (everything after the closing `---`) is the
intro markdown. Goes through the same goldmark pipeline as posts,
including the `EnrichResponsiveImages` pass — though the intro doesn't
get image variants the way post bodies do, since intro images aren't
pre-staged into the mirror.

**`_outro.md`** — plain markdown (no frontmatter), rendered below the
year groups and above the home-page footer. Useful for "subscribe / how
this site works / contact" content that doesn't fit at the top of the
page. Missing file is not an error.

`internal/build.loadHomeContent` parses `_index.md` (frontmatter +
body), `loadOutro` reads `_outro.md`. Both are no-ops when the file
doesn't exist. See `internal/build/build.go` for the wiring.

## Boundaries

- Safe to edit: `cmd/`, `internal/`, `sandbox/`, `docs/`.
- `internal/markdown/testdata/` posts are golden inputs — changing them
  invalidates hash tests; do it deliberately.
- The authoring directory (configured `blog_posts`) is the operator's repo,
  separate from this one. Within it, `_index.md` and `_outro.md` (if
  present) are read at build time as home-page chrome (intro +
  customization frontmatter; outro); every other `*.md` under a post
  directory is content.

## Gotchas

- `FAIR_PROFILE=""` (empty string) is treated as "unset" by `ResolveProfile`,
  so test setup that does `t.Setenv("FAIR_PROFILE", "")` to clear inherited
  env works as intended.
- `FileStore.Save` is atomic via temp-file + rename in the same directory.
  Don't change the temp location without preserving same-fs invariant.
- `EmitClientMetadata` derives `client_id` from the profile's `domain` field;
  half-configured (empty domain) returns an error rather than silently writing
  a bad doc.
- `publish.EnsureID` writes the ULID `id` back into the source markdown
  frontmatter. The source IS mutated in `--dry-run` for that one field —
  rkeys must be stable across runs even when nothing else gets uploaded.
- Windows is unsupported for sandbox; prod-side commands are fine. Use WSL2.
