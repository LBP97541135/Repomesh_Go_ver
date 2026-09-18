#!/usr/bin/env bash
set -euo pipefail

HELPERS="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HELPERS/../../../.." && pwd)"
cd "$ROOT"

eval "$(python3 "$HELPERS/load-config.py" --export)"

STATE="${REPOMESH_VERIFY_STATE:?set REPOMESH_VERIFY_STATE}"
EVIDENCE="${REPOMESH_VERIFY_EVIDENCE:?set REPOMESH_VERIFY_EVIDENCE}"

if [[ "${REPOMESH_VERIFY_RUN_SCOPE}" == "unconfigured-only" ]]; then
  exec bash "$HELPERS/launch-unconfigured.sh"
fi

echo "live launch listens on local ${REPOMESH_VERIFY_ADDR} (${REPOMESH_VERIFY_ORIGIN})." >&2
echo "product auth.json must still use an HTTPS origin. Local HTTP cannot prove __Host- cookies." >&2

if [[ -z "${REPOMESH_AUTH_CONFIG:-}" ]]; then
  echo "REPOMESH_AUTH_CONFIG is empty after config load" >&2
  exit 1
fi
if [[ -z "${REPOMESH_DATABASE_URL:-}" ]]; then
  echo "REPOMESH_DATABASE_URL is empty after config load" >&2
  exit 1
fi

mkdir -p "$STATE" "$EVIDENCE"
if [[ -e "$STATE/web.pid" ]] && kill -0 "$(cat "$STATE/web.pid")" 2>/dev/null; then
  echo "already running pid=$(cat "$STATE/web.pid")" >&2
  exit 1
fi

ASSETS="${REPOMESH_WEB_ASSETS:-$ROOT/web/dist}"
if [[ ! -f "$ASSETS/index.html" ]]; then
  npm --prefix "$ROOT/web" ci
  npm --prefix "$ROOT/web" run build
fi

go run ./cmd/repomesh-web db check
go run ./cmd/repomesh-web db migrate --timeout 30s

BIN="$STATE/repomesh-web"
go build -o "$BIN" ./cmd/repomesh-web
COORD_BIN="$STATE/repomesh-coordinator"
go build -o "$COORD_BIN" ./cmd/repomesh-coordinator

: >"$STATE/web.stderr.log"
: >"$STATE/coordinator.stderr.log"

"$BIN" --addr "${REPOMESH_VERIFY_ADDR}" --assets "$ASSETS" --auth-config "${REPOMESH_AUTH_CONFIG}" \
  >"$STATE/web.stdout.log" 2>"$STATE/web.stderr.log" &
echo $! >"$STATE/web.pid"

"$COORD_BIN" >"$STATE/coordinator.stdout.log" 2>"$STATE/coordinator.stderr.log" &
echo $! >"$STATE/coordinator.pid"

ORIGIN="${REPOMESH_VERIFY_ORIGIN}"
for _ in $(seq 1 150); do
  if curl -sf "$ORIGIN/healthz" >/dev/null; then
    printf '%s\n' "$ORIGIN" >"$STATE/origin"
    printf 'live\n' >"$STATE/mode"
    printf '%s\n' "${REPOMESH_VERIFY_ADDR}" >"$STATE/addr"
    echo "ready origin=$ORIGIN mode=live"
    echo "verified-unreachable: browser __Host- cookie acceptance still requires the HTTPS origin inside auth.json" >&2
    exit 0
  fi
  if ! kill -0 "$(cat "$STATE/web.pid")" 2>/dev/null; then
    echo "web exited before ready" >&2
    cat "$STATE/web.stderr.log" >&2 || true
    exit 1
  fi
  sleep 0.2
done

echo "web did not become ready" >&2
exit 1
