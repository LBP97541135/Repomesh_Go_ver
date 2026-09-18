#!/usr/bin/env bash
# Start only this development cluster. Web runs in its own configured terminal.
set -euo pipefail
umask 077

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"
PG_MAJOR=17
PG_BIN="/usr/lib/postgresql/${PG_MAJOR}/bin"
PG_ROOT="${REPOMESH_DEV_PG_ROOT:-${HOME}/repomesh-pg}"
PG_DATA="${PG_ROOT}/data"

if [ "$(uname -s)" != Linux ] || [ "$(id -u)" -eq 0 ]; then
  echo "Run this script as a regular Linux user (WSL is supported)." >&2
  exit 1
fi
if [ ! -s "${PG_DATA}/PG_VERSION" ] || [ ! -s "${PG_ROOT}/connection-url.txt" ]; then
  echo "Development cluster or connection file missing; run bash .cursor/install.sh first." >&2
  exit 1
fi
if [ "$(cat "${PG_DATA}/PG_VERSION")" != "$PG_MAJOR" ]; then
  echo "Existing PostgreSQL version does not match; do not reinitialize this directory." >&2
  exit 1
fi
PG_PORT="$("${PG_BIN}/postgres" -D "${PG_DATA}" -C port)"
if [ -n "${REPOMESH_DEV_PG_PORT:-}" ] && [ "$REPOMESH_DEV_PG_PORT" != "$PG_PORT" ]; then
  echo "REPOMESH_DEV_PG_PORT differs from the initialized cluster port." >&2
  exit 1
fi

echo "==> Starting PostgreSQL ${PG_MAJOR}"
if "${PG_BIN}/pg_ctl" -D "${PG_DATA}" status >/dev/null 2>&1; then
  echo "    Already running"
else
  "${PG_BIN}/pg_ctl" -D "${PG_DATA}" -l "${PG_ROOT}/server.log" -w -t 30 start
fi

# Use the cluster's private socket, not a potentially unrelated TCP listener.
"${PG_BIN}/pg_isready" -h "${PG_ROOT}" -p "$PG_PORT" -U ubuntu
exists="$("${PG_BIN}/psql" -X -v ON_ERROR_STOP=1 -h "${PG_ROOT}" -p "$PG_PORT" -U ubuntu -d postgres -tAc \
  "SELECT 1 FROM pg_database WHERE datname='repomesh_dev'")"
if [ "$exists" != 1 ]; then
  "${PG_BIN}/createdb" -h "${PG_ROOT}" -p "$PG_PORT" -U ubuntu -O ubuntu repomesh_dev
fi

# Apply migrations only to the development URL created by install.sh.
# This export affects this script, not later agent terminals.
REPOMESH_DATABASE_URL="$(cat "${PG_ROOT}/connection-url.txt")"
export REPOMESH_DATABASE_URL
go run ./cmd/repomesh-web db migrate --timeout 30s
go run ./cmd/repomesh-web db check

echo "==> Development database ready on 127.0.0.1:${PG_PORT}."
echo "    In each agent terminal, load the development connection before db commands or tests:"
printf '      export REPOMESH_DATABASE_URL="$(cat %q)"\n' "${PG_ROOT}/connection-url.txt"
printf '      export REPOMESH_TEST_DATABASE_URL="$REPOMESH_DATABASE_URL"\n'
echo "    The web terminal starts separately. Configure HTTPS/auth and start coordinator only when needed."
