#!/usr/bin/env bash
# Wipe the sandbox: nukes the docker volume, local state, generated
# client metadata, and any keychain/session entries scoped to the
# sandbox profile. Use when you want a true clean-slate test.
#
# Idempotent. Will not touch the prod profile.

set -euo pipefail

cd "$(dirname "$0")"

echo "stopping PDS and removing volume..."
docker compose -f compose.yaml --env-file .env down -v 2>/dev/null || true

echo "removing generated client metadata..."
rm -rf public/.well-known

echo "removing local sandbox state..."
rm -rf ../.fair/state.sandbox.json
rm -rf ../images-mirror.sandbox/
rm -rf ../dist.sandbox/

# Best-effort sandbox session cleanup. If the binary is built and the
# session-file fallback was used, this will remove it. Keychain entries
# for the sandbox profile (if Phase 3b lands) should also be cleared
# here when the keychain backend is added.
SESSION_FILE="${HOME}/.config/fair/session.sandbox.json"
if [[ -f "$SESSION_FILE" ]]; then
  echo "removing sandbox session file at $SESSION_FILE..."
  rm -f "$SESSION_FILE"
fi

echo "sandbox reset. Run sandbox/seed.sh to bring it back up."
