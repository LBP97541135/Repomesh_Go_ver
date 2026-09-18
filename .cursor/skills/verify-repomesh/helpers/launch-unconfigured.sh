#!/usr/bin/env bash
set -euo pipefail

HELPERS="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HELPERS/../../../.." && pwd)"
cd "$ROOT"

eval "$(python3 "$HELPERS/load-config.py" --export)"

if [[ "${REPOMESH_VERIFY_RUN_SCOPE}" != "unconfigured-only" ]]; then
  echo "launch-unconfigured.sh is for run_scope=unconfigured-only; got ${REPOMESH_VERIFY_RUN_SCOPE}" >&2
  echo "use helpers/launch.sh for live scopes, which still cannot prove __Host- cookies on HTTP" >&2
  exit 1
fi

STATE="${REPOMESH_VERIFY_STATE:?set REPOMESH_VERIFY_STATE}"
EVIDENCE="${REPOMESH_VERIFY_EVIDENCE:?set REPOMESH_VERIFY_EVIDENCE}"
ADDR="${REPOMESH_VERIFY_ADDR}"
ASSETS="${REPOMESH_WEB_ASSETS:-$ROOT/web/dist}"

mkdir -p "$STATE" "$EVIDENCE"

if [[ -e "$STATE/web.pid" ]] && kill -0 "$(cat "$STATE/web.pid")" 2>/dev/null; then
  echo "already running pid=$(cat "$STATE/web.pid")" >&2
  exit 1
fi

if [[ ! -f "$ASSETS/index.html" ]]; then
  npm --prefix "$ROOT/web" ci
  npm --prefix "$ROOT/web" run build
fi

BIN="$STATE/repomesh-web"
go build -o "$BIN" ./cmd/repomesh-web

LOG="$STATE/web.stderr.log"
: >"$LOG"

"$BIN" --addr "$ADDR" --assets "$ASSETS" --auth-config= \
  >"$STATE/web.stdout.log" 2>"$LOG" &
echo $! >"$STATE/web.pid"

ORIGIN="${REPOMESH_VERIFY_ORIGIN}"
for _ in $(seq 1 150); do
  if curl -sf "$ORIGIN/healthz" >/dev/null; then
    printf '%s\n' "$ORIGIN" >"$STATE/origin"
    printf 'unconfigured\n' >"$STATE/mode"
    printf '%s\n' "$ADDR" >"$STATE/addr"
    echo "ready origin=$ORIGIN"
    exit 0
  fi
  if ! kill -0 "$(cat "$STATE/web.pid")" 2>/dev/null; then
    echo "web exited before ready" >&2
    cat "$LOG" >&2 || true
    exit 1
  fi
  sleep 0.2
done

echo "web did not become ready" >&2
exit 1
