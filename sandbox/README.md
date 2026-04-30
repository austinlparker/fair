# Local sandbox

End-to-end testing of `fair` without touching your real PDS or the
public ATProto network. Runs an isolated `bluesky-social/pds` Docker
container behind a Caddy TLS proxy on a real public hostname
(`pds.localtest.me`, which always resolves to 127.0.0.1).

## Why the TLS dance?

The PDS image hardcodes `https://` for its OAuth issuer URL and rejects
loopback hosts (`localhost`, `127.0.0.1`). To work around this without
ngrok, we use:

- `pds.localtest.me` — public DNS that always resolves to 127.0.0.1.
  Non-loopback per the PDS validator, but lives entirely on your laptop.
- `mkcert` — generates an OS-trusted local CA and a cert for
  `pds.localtest.me`.
- Caddy — terminates TLS in front of the PDS using the mkcert cert.

The PDS thinks it lives at `https://pds.localtest.me:8443`. The Go CLI
talks to it via real HTTPS. No federation, no public traffic.

## What's here

| File | Purpose |
|---|---|
| `compose.yaml` | PDS + Caddy services |
| `caddy/Caddyfile` | Caddy reverse-proxy config |
| `caddy/certs/` | (generated) mkcert-issued cert + key |
| `.env.example` | Template for required PDS secrets |
| `seed.sh` | First-time bootstrap: cert + docker up + test account + client metadata |
| `reset.sh` | Wipe everything: docker volume, local state, session file |
| `public/` | (generated) `oauth-client-metadata.json` for the OAuth flow |

## Prerequisites

- Docker (Orbstack, Docker Desktop, or native engine — anything with `docker compose`)
- Go ≥ 1.26 (for building `fair`)
- `mkcert` (`brew install mkcert`)
- macOS or Linux. Windows requires WSL2.

**One-time:** run `sudo mkcert -install` to add the local CA to the
system trust store. Without this, browsers (which the OAuth flow opens
during login) will warn about the cert. The Go CLI trusts the CA
explicitly regardless.

The PDS image bundles `goat`, the official admin CLI; no separate install.

## Workflow

```bash
# First time, or after a reset:
sandbox/seed.sh

# Iterate locally — replace these with the real subcommands as Phase 3 lands:
fair --profile sandbox emit-client-metadata --out sandbox/public/
# fair --profile sandbox sandbox serve         (TODO Phase 3f)
# fair --profile sandbox auth login            (TODO Phase 3f)
# fair --profile sandbox publish post.md       (TODO Phase 4)

# When you want a true reset:
sandbox/reset.sh
```

## Expected ports

| Endpoint | Where |
|---|---|
| PDS XRPC | https://pds.localtest.me:8443/xrpc/... |
| Health   | https://pds.localtest.me:8443/xrpc/_health |
| OAuth client metadata | https://pds.localtest.me:8443/.well-known/oauth-client-metadata.json (served by `fair sandbox serve` — TODO Phase 3f) |
| Loopback callback | http://127.0.0.1:&lt;random&gt;/callback (per `fair auth login` invocation) |

## Profile config

You'll need `~/.config/fair/config.sandbox.toml`. The DID is filled in
after `seed.sh` creates the test account:

```toml
pds_url          = "https://pds.localtest.me:8443"
did              = "did:plc:..."           # printed by seed.sh
domain           = "https://pds.localtest.me:8443"
publication_name = "Sandbox Blog"
blog_posts       = "/Users/austinparker/code/blog-posts"
state_file       = ".fair/state.sandbox.json"
images_mirror    = "images-mirror.sandbox/"
dist_dir         = "dist.sandbox/"
```

(No `cloudflare_project` — sandbox cannot deploy.)

## Known limitations

- Federation is intentionally off. Records on this PDS aren't visible
  outside the laptop, but they're still real ATProto records — the same
  putRecord/listRecords/getRecord calls that hit prod will work here.
- The public PLC directory (plc.directory) is still consulted for DID
  generation. If you need true offline mode, run a local PLC mirror and
  override `PDS_DID_PLC_URL` in compose.yaml.
- Lexicon validation is "optimistic" by default — unknown NSIDs (like
  `site.standard.*`) are accepted as opaque CBOR. Production behavior
  matches as long as records are well-formed.
