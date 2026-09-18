#!/usr/bin/env bash
set -euo pipefail

STATE="${REPOMESH_VERIFY_STATE:?set REPOMESH_VERIFY_STATE}"

stop_pid_file() {
  local file="$1"
  [[ -f "$file" ]] || return 0
  local pid
  pid="$(cat "$file")"
  if kill -0 "$pid" 2>/dev/null; then
    kill "$pid"
    for _ in $(seq 1 30); do
      if ! kill -0 "$pid" 2>/dev/null; then
        break
      fi
      sleep 0.1
    done
    if kill -0 "$pid" 2>/dev/null; then
      kill -9 "$pid" || true
    fi
  fi
}

stop_pid_file "$STATE/coordinator.pid"
stop_pid_file "$STATE/web.pid"

rm -rf "$STATE"
echo "cleaned state; evidence left in ${REPOMESH_VERIFY_EVIDENCE:-unset}"
