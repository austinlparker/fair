#!/usr/bin/env bash
# Bootstrap the local sandbox PDS for fair.
#
# What it does:
#   1. Pre-flight: docker, openssl, curl, mkcert, fair binary
#   2. Verify mkcert root CA is trusted by the system
#   3. Generate sandbox/.env if missing (random secrets)
#   4. Generate TLS cert for pds.localtest.me if missing
#   5. docker compose up -d (PDS + Caddy)
#   6. Poll https://pds.localtest.me/xrpc/_health until healthy
#   7. Create test account via `goat` (bundled in the PDS image)
#   8. Emit OAuth client metadata into sandbox/public/.well-known/
#
# Idempotent. Safe to re-run; only generates secrets/certs/account on
# first run.

set -euo pipefail

cd "$(dirname "$0")"

# --- Pre-flight -------------------------------------------------------------

require() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing prerequisite: $1" >&2
    if [[ -n "${2:-}" ]]; then
      echo "  install: $2" >&2
    fi
    exit 1
  fi
}

require docker
require openssl
require curl
require mkcert  "brew install mkcert"

case "$(uname -s)" in
  Darwin|Linux) ;;
  *) echo "sandbox supports macOS and Linux only" >&2; exit 1 ;;
esac

# Verify mkcert root CA is in the system trust store. Without this,
# `fair auth login` will fail because the browser won't trust the
# pds.localtest.me cert when the PDS redirects to its auth page.
CAROOT="$(mkcert -CAROOT)"
if [[ ! -f "$CAROOT/rootCA.pem" ]]; then
  echo "mkcert has not generated a CA yet" >&2
  echo "run: sudo mkcert -install" >&2
  exit 1
fi

# Check whether the CA is trusted by browsers. The Go CLI can trust
# mkcert's CA explicitly without sudo, but the OAuth flow opens a
# browser, and unverified-cert warnings there are a UX papercut.
# Warn but don't fail.
case "$(uname -s)" in
  Darwin)
    if ! security find-certificate -c "mkcert" /Library/Keychains/System.keychain >/dev/null 2>&1; then
      echo "WARN: mkcert root CA not in system trust store." >&2
      echo "      Run once for browser trust: sudo mkcert -install" >&2
      echo "      The Go CLI will still work (it trusts the CA explicitly)." >&2
      echo
    fi
    ;;
esac

# Resolve the fair binary; build if missing.
FAIR_BIN="${FAIR_BIN:-$(realpath ../fair 2>/dev/null || echo "")}"
if [[ -z "$FAIR_BIN" || ! -x "$FAIR_BIN" ]]; then
  echo "building fair binary..."
  (cd .. && go build -o fair ./cmd/fair)
  FAIR_BIN="$(realpath ../fair)"
fi

# --- Generate .env if missing -----------------------------------------------

if [[ ! -f .env ]]; then
  echo "generating sandbox/.env with random secrets..."
  cat > .env <<EOF
PDS_JWT_SECRET=$(openssl rand -hex 32)
PDS_ADMIN_PASSWORD=$(openssl rand -hex 16)
PDS_PLC_ROTATION_KEY_K256_PRIVATE_KEY_HEX=$(openssl rand -hex 32)
EOF
  chmod 600 .env
fi

# Pull admin password into our shell for the goat invocation later.
# shellcheck disable=SC1091
set -a; source .env; set +a

# --- Generate TLS cert if missing -------------------------------------------

mkdir -p caddy/certs
if [[ ! -f caddy/certs/pds.localtest.me.pem ]]; then
  echo "generating TLS cert for pds.localtest.me..."
  (cd caddy && mkcert -cert-file certs/pds.localtest.me.pem -key-file certs/pds.localtest.me-key.pem pds.localtest.me)
fi

# --- Start PDS + Caddy ------------------------------------------------------

echo "starting PDS + Caddy..."
docker compose -f compose.yaml --env-file .env up -d

echo -n "waiting for PDS to be healthy "
HEALTHY=""
# Trust mkcert's CA explicitly so curl works whether or not the user
# has run `sudo mkcert -install`. The Go CLI does the same.
ROOT_CA="$CAROOT/rootCA.pem"
for _ in $(seq 1 60); do
  if curl -sfo /dev/null --cacert "$ROOT_CA" https://pds.localtest.me/xrpc/_health; then
    HEALTHY="yes"
    echo " ready"
    break
  fi
  echo -n "."
  sleep 1
done

if [[ -z "$HEALTHY" ]]; then
  echo >&2
  echo "PDS did not become healthy at https://pds.localtest.me" >&2
  echo "Check logs: docker compose -f sandbox/compose.yaml --env-file sandbox/.env logs" >&2
  exit 1
fi

# --- Create test account (idempotent) ---------------------------------------

# `test`, `admin`, etc. are on the PDS's reserved-handle list.
HANDLE="${SANDBOX_HANDLE:-aparker.pds.localtest.me}"
PDS_HANDLE_DOMAIN_NO_DOT=".pds.localtest.me"
EMAIL="${SANDBOX_EMAIL:-aparker@aparker.io}"
PASSWORD="${SANDBOX_PASSWORD:-sandbox}"

if docker exec fair-sandbox-pds goat pds account list 2>/dev/null | grep -q "$HANDLE"; then
  echo "account $HANDLE exists, skipping create"
else
  echo "creating test account $HANDLE..."
  docker exec fair-sandbox-pds goat pds admin account create \
    --admin-password "$PDS_ADMIN_PASSWORD" \
    --handle "$HANDLE" \
    --email "$EMAIL" \
    --password "$PASSWORD"
  echo
  echo "Test account ready:"
  echo "  handle:   $HANDLE"
  echo "  password: $PASSWORD"
fi

# --- Emit OAuth client metadata --------------------------------------------

mkdir -p public
"$FAIR_BIN" --profile sandbox emit-client-metadata --out public

# --- Done -------------------------------------------------------------------

echo
echo "sandbox ready. Endpoints:"
echo "  PDS XRPC:        https://pds.localtest.me"
echo "  Health:          https://pds.localtest.me/xrpc/_health"
echo "  Client metadata: file://$(pwd)/public/.well-known/oauth-client-metadata.json"
echo
echo "Next: \`fair --profile sandbox auth login\` (Phase 3f, in progress)."
