# Publishing from GitHub Actions

The repository root is a composite action that publishes every post in
a content repository, commits what `fair publish` writes back, and
builds the static site. Deploying the result is left to your workflow,
so fair never sees your hosting credentials. The example below deploys
to Cloudflare Workers static assets; any static host works.

## What you need

**In the content repository**

- A fair config checked in, e.g. `fair.toml`. It is the same format as
  `~/.config/fair/config.<profile>.toml`, with paths relative to the
  repository root:

  ```toml
  pds_url          = "https://your-pds.example"   # from your DID doc, not necessarily bsky.social
  did              = "did:plc:..."
  domain           = "https://example.com"
  publication_name = "Your Blog"
  blog_posts       = "."
  state_file       = ".fair/state.json"
  images_mirror    = "images-mirror"
  dist_dir         = "dist"
  ```

  `images_mirror` must be inside the repository: the action commits
  the images `publish` stages there, and `build` reads them back.
  `state_file` is only a local cache — a runner starts without one, and
  `publish` then compares each post against its record on the PDS — so
  it can stay gitignored, along with `dist/`.

- A `wrangler.toml` (for the deploy step below) whose `[assets]`
  directory is the config's `dist_dir`.

**Secrets**

| Secret | Used by | Notes |
|---|---|---|
| `FAIR_APP_PASSWORD` | fair | An app password for the account in `did`, created on its PDS. |
| `CLOUDFLARE_API_TOKEN` | deploy step | The *Edit Cloudflare Workers* token template is sufficient. |
| `CLOUDFLARE_ACCOUNT_ID` | deploy step | |

The publication record must already exist (`fair init publication`,
once, from a machine with an OAuth session).

## Workflow

```yaml
name: publish
on:
  push:
    branches: [main]
  workflow_dispatch:

# One run at a time: two runs racing would both try to push write-backs.
concurrency:
  group: fair-publish
  cancel-in-progress: false

permissions:
  contents: write   # the action commits rkeys and staged images

jobs:
  publish:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: austinlparker/fair@main
        with:
          config: fair.toml
          app-password: ${{ secrets.FAIR_APP_PASSWORD }}

      - uses: cloudflare/wrangler-action@v3
        with:
          apiToken: ${{ secrets.CLOUDFLARE_API_TOKEN }}
          accountId: ${{ secrets.CLOUDFLARE_ACCOUNT_ID }}
          command: deploy
```

Commits pushed with the default `GITHUB_TOKEN` do not start new
workflow runs, so the write-back commits don't loop.

## What a run does

1. Builds `fair` from the action's own source.
2. `fair publish --all --dry-run` — assigns an `rkey`/`id` to any new
   post and stages inline images into the mirror. Committed and pushed
   **before** anything reaches the PDS, so a post can never be published
   under a key that isn't in git.
3. `fair publish --all` — a post whose record on the PDS already
   matches its source is skipped (one `getRecord` per post).
4. Commits any remaining write-backs (`bsky_post` frontmatter when
   `announce` is on). This step runs even if step 3 failed partway.
5. `fair build` into `dist_dir`.

## Why an app password

fair's interactive login is ATProto OAuth as a public client. That
doesn't fit a headless job: there is no non-interactive grant, refresh
tokens are single-use (every run would have to write a new secret back
to GitHub), and the spec caps public-client sessions at about two
weeks. `FAIR_APP_PASSWORD` switches any authenticated subcommand to a
one-shot `createSession` login instead; nothing is persisted.

The trade-off is scope: an app password can write to the whole
repository, not just the `site.standard.*` collections the OAuth
session is limited to. Use a dedicated app password for CI so it can be
revoked on its own.

## Inputs

| Input | Default | |
|---|---|---|
| `config` | — | Path to the fair config in the calling repo. |
| `app-password` | — | App password secret. |
| `working-directory` | `.` | Where fair runs; config-relative paths resolve here. |
| `announce` | `false` | Create a Bluesky announce post for posts without one. |
| `build` | `true` | Run `fair build` after publishing. |
| `commit` | `true` | Commit and push write-backs. |
