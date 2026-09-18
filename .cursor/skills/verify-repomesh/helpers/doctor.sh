#!/usr/bin/env bash
set -euo pipefail

HELPERS="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [[ -z "${REPOMESH_VERIFY_ORIGIN:-}" ]]; then
  eval "$(python3 "$HELPERS/load-config.py" --export)"
fi

STATE="${REPOMESH_VERIFY_STATE:-}"
if [[ -n "${REPOMESH_VERIFY_ORIGIN:-}" ]]; then
  ORIGIN="$REPOMESH_VERIFY_ORIGIN"
elif [[ -n "$STATE" && -f "$STATE/origin" ]]; then
  ORIGIN="$(cat "$STATE/origin")"
else
  echo "set REPOMESH_VERIFY_ORIGIN or REPOMESH_VERIFY_STATE/origin" >&2
  exit 1
fi

case "$ORIGIN" in
  http://127.0.0.1:*|http://localhost:*|http://[::1]:*)
    ;;
  *)
    echo "doctor refuses non-local origin $ORIGIN; skill drive origin must be loopback HTTP" >&2
    exit 1
    ;;
esac

MODE="${REPOMESH_VERIFY_MODE:-}"
if [[ -z "$MODE" && -n "$STATE" && -f "$STATE/mode" ]]; then
  MODE="$(cat "$STATE/mode")"
fi
MODE="${MODE:-unconfigured}"

if [[ -n "$STATE" && -f "$STATE/web.pid" ]]; then
  PID="$(cat "$STATE/web.pid")"
  if ! kill -0 "$PID" 2>/dev/null; then
    echo "web pid $PID is not running" >&2
    exit 1
  fi
fi

if [[ "$MODE" == live && -n "$STATE" && -f "$STATE/coordinator.pid" ]]; then
  CPID="$(cat "$STATE/coordinator.pid")"
  if ! kill -0 "$CPID" 2>/dev/null; then
    echo "coordinator pid $CPID is not running" >&2
    exit 1
  fi
fi

health="$(mktemp)"
ready="$(mktemp)"
session="$(mktemp)"
trap 'rm -f "$health" "$ready" "$session"' EXIT

health_code="$(curl -sS -o "$health" -w '%{http_code}' "$ORIGIN/healthz")"
ready_code="$(curl -sS -o "$ready" -w '%{http_code}' "$ORIGIN/readyz")"
session_code="$(curl -sS -o "$session" -w '%{http_code}' "$ORIGIN/api/session")"

if [[ "$health_code" != 200 ]]; then
  echo "healthz expected 200 got $health_code" >&2
  exit 1
fi
if ! grep -q '"businessReady":false' "$health" && ! grep -q '"businessReady": false' "$health"; then
  echo "healthz businessReady is not false" >&2
  cat "$health" >&2
  exit 1
fi
if [[ "$ready_code" != 503 ]]; then
  echo "readyz expected 503 got $ready_code" >&2
  exit 1
fi

session_body="$(tr -d '\n' <"$session")"
if [[ "$MODE" == unconfigured ]]; then
  if [[ "$session_code" != 503 ]]; then
    echo "unconfigured session expected 503 got $session_code" >&2
    exit 1
  fi
  if [[ "$session_body" != *AUTH_NOT_CONFIGURED* ]]; then
    echo "unconfigured session missing AUTH_NOT_CONFIGURED" >&2
    exit 1
  fi
elif [[ "$MODE" == live ]]; then
  if [[ "$session_code" != 401 ]]; then
    echo "live anonymous session expected 401 got $session_code" >&2
    exit 1
  fi
  if [[ "$session_body" != *AUTHENTICATION_REQUIRED* ]]; then
    echo "live session missing AUTHENTICATION_REQUIRED" >&2
    exit 1
  fi
  echo "doctor note: local HTTP 401 does not prove __Host- cookie acceptance" >&2
else
  echo "unknown mode $MODE" >&2
  exit 1
fi

echo "doctor ok mode=$MODE origin=$ORIGIN healthz=200 readyz=503 session=$session_code"
