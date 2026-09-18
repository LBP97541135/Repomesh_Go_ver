#!/usr/bin/env bash
# Idempotent Cloud Agent bootstrap for RepoMesh.
# Prepares durable, source-derived state after checkout: PostgreSQL 17,
# Go modules, and the built frontend. Safe to run repeatedly. Per-boot
# service startup lives in start.sh, not here.
set -euo pipefail
umask 077

if [ "$(uname -s)" != Linux ] || [ "$(id -u)" -eq 0 ]; then
  echo "Run this script as a regular Linux user (WSL is supported)." >&2
  exit 1
fi

for tool in go node npm openssl; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "Missing $tool; prepare Go >= 1.26, Node.js >= 22.12, npm and OpenSSL first." >&2
    exit 1
  fi
done
if ! go env GOVERSION | awk -F '[go.]+' '{ exit !($2 > 1 || ($2 == 1 && $3 >= 26)) }'; then
  echo "Go >= 1.26 is required." >&2
  exit 1
fi
node -e 'const [major, minor] = process.versions.node.split(".").map(Number); if (major < 22 || (major === 22 && minor < 12)) { console.error("Node.js >= 22.12 is required."); process.exit(1); }'
go version
node --version
if ! command -v pwsh >/dev/null 2>&1; then
  echo "PowerShell 7 (pwsh) is required for packaging and batch verification; source startup remains available."
fi
if [ "$(go env CGO_ENABLED)" != 1 ] || ! command -v "$(go env CC)" >/dev/null 2>&1; then
  echo "Race checks require CGO_ENABLED=1 and a C compiler."
fi

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

PG_MAJOR=17
PG_BIN="/usr/lib/postgresql/${PG_MAJOR}/bin"
PG_ROOT="${REPOMESH_DEV_PG_ROOT:-${HOME}/repomesh-pg}"
PG_PORT="${REPOMESH_DEV_PG_PORT:-5432}"
PG_DATA="${PG_ROOT}/data"
if [[ "$PG_ROOT" != /* || "$PG_ROOT" == *"'"* || "$PG_ROOT" == *$'\n'* ]] ||
   [[ ! "$PG_PORT" =~ ^[0-9]{1,5}$ ]] || (( 10#$PG_PORT < 1 || 10#$PG_PORT > 65535 )); then
  echo "Use an absolute REPOMESH_DEV_PG_ROOT without quotes/newlines and a port from 1 to 65535." >&2
  exit 1
fi
PG_PORT=$((10#$PG_PORT))

echo "==> Installing PostgreSQL ${PG_MAJOR} (idempotent)"
if [ ! -x "${PG_BIN}/postgres" ]; then
  sudo install -d /usr/share/postgresql-common/pgdg
  sudo curl -fsSL https://www.postgresql.org/media/keys/ACCC4CF8.asc \
    -o /usr/share/postgresql-common/pgdg/apt.postgresql.org.asc
  . /etc/os-release
  echo "deb [signed-by=/usr/share/postgresql-common/pgdg/apt.postgresql.org.asc] https://apt.postgresql.org/pub/repos/apt ${VERSION_CODENAME}-pgdg main" \
    | sudo tee /etc/apt/sources.list.d/pgdg.list >/dev/null
  sudo apt-get update -qq
  sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq \
    "postgresql-${PG_MAJOR}" "postgresql-client-${PG_MAJOR}"
else
  echo "    PostgreSQL ${PG_MAJOR} already present"
fi
"${PG_BIN}/postgres" --version

echo "==> Initializing loopback dev cluster (idempotent)"
mkdir -p "${PG_ROOT}"
chmod 700 "${PG_ROOT}"
if [ ! -s "${PG_DATA}/PG_VERSION" ]; then
  if [ ! -s "${PG_ROOT}/password" ]; then
    openssl rand -hex 24 > "${PG_ROOT}/password"
    chmod 600 "${PG_ROOT}/password"
  fi
  # Loopback TCP requires a password (scram) so credential-enforcement tests
  # pass; the local unix socket stays trust for maintenance commands.
  "${PG_BIN}/initdb" -D "${PG_DATA}" -U ubuntu --pwfile="${PG_ROOT}/password" \
    --auth-host=scram-sha-256 --auth-local=trust \
    --encoding=UTF8 --locale=C.UTF-8 >/dev/null
  {
    echo "listen_addresses = '127.0.0.1'"
    echo "port = ${PG_PORT}"
    echo "unix_socket_directories = '${PG_ROOT}'"
    echo "password_encryption = 'scram-sha-256'"
  } >> "${PG_DATA}/postgresql.conf"
  echo "    Cluster initialized at ${PG_DATA}"
else
  if [ "$(cat "${PG_DATA}/PG_VERSION")" != "$PG_MAJOR" ] || [ ! -s "${PG_ROOT}/password" ]; then
    echo "Existing cluster version or password file does not match; preserve it and check the configuration." >&2
    exit 1
  fi
  if [ "$("${PG_BIN}/postgres" -D "${PG_DATA}" -C port)" != "$PG_PORT" ]; then
    echo "Existing cluster port differs from REPOMESH_DEV_PG_PORT; use its original port or a new data directory." >&2
    exit 1
  fi
  echo "    Cluster already initialized at ${PG_DATA}"
fi

# Record the loopback dev connection string for tests and db commands.
DEV_PW="$(cat "${PG_ROOT}/password")"
printf 'postgres://ubuntu:%s@127.0.0.1:%s/repomesh_dev?sslmode=disable\n' "${DEV_PW}" "${PG_PORT}" \
  > "${PG_ROOT}/connection-url.txt"
chmod 600 "${PG_ROOT}/connection-url.txt"

echo "==> Downloading Go modules"
go mod download

echo "==> Installing frontend dependencies and building assets"
npm --prefix web ci
npm --prefix web run build

echo "==> install.sh complete; run bash .cursor/start.sh to start the database and apply migrations."
