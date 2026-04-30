# fair

Last reviewed: 2026-04-30

A Go CLI that publishes markdown posts to an ATProto PDS as `standard.site`
lexicon records, then renders them as plain HTML for static deploy to
`aparker.io`. Replaces the Next.js + WhiteWind stack at `~/code/pds-blog`.

**Source of truth for architecture:** `docs/design-plans/2026-04-29-blog-v2026.md`.
**Operator/user surface:** `README.md`. This file captures only the
non-obvious rules and the things that bite when forgotten.

**Status:** Phases 1–7 of the design plan are complete; the codebase is
feature-complete for single-author personal use. The actual prod cutover
(domain DNS flip, first push to Tangled) is the operator's to drive.

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

- Site domain is **`aparker.io`** (not austinparker.me — common autocomplete trap).
- Lexicons: `site.standard.publication`, `site.standard.document`,
  with `site.standard.content.markdown` content variant.
- Records are canonical. Markdown files in `~/code/blog-posts` are working
  copies; `fair publish` is idempotent (content-hash skip; `--force` overrides).

## Subcommands

`fair --profile <name> <verb>` — see README for full usage. One-line reminder:
`auth login|logout|status`, `init publication`, `publish <path>`,
`unpublish <rkey>`, `build`, `ls`, `doctor`, `migrate cleanup-whtwnd`,
`config show`, `emit-client-metadata <out>`.

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

`internal/build/render.go` runs three text passes on goldmark's output, in
order. They live outside the parser specifically so hash stays stable:

1. `resolveRelativeURLs` — joins `publication.url` with any `/...` href/src
   so rendered HTML works on any origin.
2. `transformBlueskyEmbeds` — rewrites `<blockquote data-bluesky-uri>` into
   `<blockquote>...<footer>Discuss on Bluesky</footer></blockquote>` (no JS).
3. `wrapStandaloneImagesAsFigures` — promotes `<p><img></p>` blocks to
   `<figure><img><figcaption>alt</figcaption></figure>`. Inline images
   are left alone.

Each pass is a regex on rendered HTML, not a goldmark parser/renderer
extension. Adding new transforms here is safe; modifying the goldmark parser
config to do the same thing is not.

## Domain-portable records

Inline image URLs in PDS record bodies are **relative** (`/images/<slug>/foo.png`).
The build flow joins them with `publication.url` to produce absolute URLs in
rendered HTML. This is a deliberate design choice — keep it relative or
readers like Leaflet/pckt break.

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

## OAuth scopes (changing this list invalidates sessions)

`internal/atproto.BlogScopes` is the single source of truth:
`atproto repo:site.standard.publication repo:site.standard.document repo:com.whtwnd.blog.entry blob`.
Adding/removing collections here means existing sessions are missing the
new scope and need re-login. The `repo:com.whtwnd.blog.entry` grant is for
the one-time `migrate cleanup-whtwnd` flow — drop it once no PDS holds
legacy whitewind records.

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

## Boundaries

- Safe to edit: `cmd/`, `internal/`, `sandbox/`, `docs/`.
- `internal/markdown/testdata/` posts are golden inputs — changing them
  invalidates hash tests; do it deliberately.
- `~/code/blog-posts/` is the authoring repo, separate from this one.

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
