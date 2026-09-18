#!/usr/bin/env bash
set -euo pipefail

mode=${1:?usage: run-browser-validation.sh normal|empty evidence-directory}
evidence=${2:?usage: run-browser-validation.sh normal|empty evidence-directory}
case "$mode" in normal|empty) ;; *) echo "mode must be normal or empty" >&2; exit 2 ;; esac
if [[ -e "$evidence" ]]; then echo "evidence directory already exists" >&2; exit 2; fi

repo=$(cd "$(dirname "$0")/../../.." && pwd)
script="$repo/docs/development/2026-09-12-b03-integration-01/browser-acceptance.mjs"
postgres_bin=/usr/lib/postgresql/17/bin
export LD_LIBRARY_PATH="/tmp/repomesh-b02-browser/libraries/usr/lib/x86_64-linux-gnu${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
runtime=$(mktemp -d "/tmp/repomesh-b03-integration-browser-${mode}.XXXXXX")
data="$runtime/data"
manifest_dir="$runtime/manifest"
password_file="$runtime/password"
mkdir -m 0700 "$evidence" "$manifest_dir"
password=$(openssl rand -hex 32)
printf '%s' "$password" >"$password_file"
chmod 0600 "$password_file"
port=$(python3 - <<'PY'
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
)
go_pid=
postgres_started=false

cleanup() {
  status=$?
  set +e
  if [[ -n "$go_pid" ]] && kill -0 "$go_pid" 2>/dev/null; then
    kill -TERM "$go_pid" 2>/dev/null
    wait "$go_pid" 2>/dev/null
  fi
  if [[ "$postgres_started" == true ]]; then
    while IFS= read -r database; do
      if [[ "$database" =~ ^repomesh_b02_it_[0-9a-f]{24}$ ]]; then
        PGPASSWORD="$password" "$postgres_bin/psql" -h 127.0.0.1 -p "$port" -U repomesh_test -d postgres -v ON_ERROR_STOP=1 -c "DROP DATABASE \"$database\" WITH (FORCE)" >>"$evidence/cleanup.txt" 2>&1
      fi
    done < <(PGPASSWORD="$password" "$postgres_bin/psql" -h 127.0.0.1 -p "$port" -U repomesh_test -d postgres -Atqc "SELECT datname FROM pg_database WHERE datname LIKE 'repomesh_b02_it_%'")
    PGPASSWORD="$password" "$postgres_bin/psql" -h 127.0.0.1 -p "$port" -U repomesh_test -d postgres -Atqc "SELECT count(*) FROM pg_database WHERE datname LIKE 'repomesh_b02_it_%'" 2>/dev/null | sed 's/^/remaining matching test databases before PostgreSQL stop: /' >>"$evidence/cleanup.txt"
    "$postgres_bin/pg_ctl" -D "$data" -m fast -w stop >>"$evidence/cleanup.txt" 2>&1
    printf 'PostgreSQL stop: completed (fast)\n' >>"$evidence/cleanup.txt"
  fi
  python3 - "$runtime" <<'PY'
import shutil, sys
shutil.rmtree(sys.argv[1], ignore_errors=True)
PY
  printf 'runtime directory removal: completed\nChrome profile: Playwright ephemeral context removed with browser process\n' >>"$evidence/cleanup.txt"
  exit "$status"
}
trap cleanup EXIT

"$postgres_bin/initdb" -D "$data" -U repomesh_test --pwfile="$password_file" --auth-host=scram-sha-256 --auth-local=scram-sha-256 --encoding=UTF8 --locale=C >"$evidence/initdb.txt" 2>&1
python3 - "$password_file" <<'PY'
import os, sys
os.unlink(sys.argv[1])
PY
printf "listen_addresses='127.0.0.1'\nport=%s\nunix_socket_directories=''\nfsync=on\n" "$port" >>"$data/postgresql.conf"
"$postgres_bin/pg_ctl" -D "$data" -l "$runtime/postgres.log" -w start >"$evidence/postgres-start.txt" 2>&1
postgres_started=true
database_url="postgres://repomesh_test:${password}@127.0.0.1:${port}/postgres?sslmode=disable"
empty=0
[[ "$mode" == empty ]] && empty=1
REPOMESH_TEST_DATABASE_URL="$database_url" REPOMESH_B03_BROWSER_TEST=1 REPOMESH_B03_BROWSER_ASSETS="$repo/web/dist" REPOMESH_B03_BROWSER_MANIFEST_DIR="$manifest_dir" REPOMESH_B03_BROWSER_DURATION=20m REPOMESH_B03_BROWSER_EMPTY_CATALOG="$empty" \
  go test -count=1 -run '^TestProjectBrowserServer$' -v ./internal/web >"$evidence/go-test.txt" 2>&1 &
go_pid=$!
for _ in $(seq 1 600); do
  [[ -f "$manifest_dir/manifest.json" ]] && break
  if ! kill -0 "$go_pid" 2>/dev/null; then wait "$go_pid"; fi
  sleep 0.1
done
if [[ ! -f "$manifest_dir/manifest.json" ]]; then echo "manifest was not written" >&2; exit 1; fi
install -m 0600 "$manifest_dir/manifest.json" "$evidence/manifest.json"
set +e
node "$script" "$manifest_dir/manifest.json" >"$evidence/browser-result.json" 2>"$evidence/browser-stderr.txt"
browser_exit=$?
set -e
printf '%s\n' "$browser_exit" >"$evidence/browser.exit"
if [[ "$browser_exit" -ne 0 ]]; then
  origin=$(python3 - "$manifest_dir/manifest.json" <<'PY'
import json, sys
print(json.load(open(sys.argv[1], encoding="utf-8"))["origin"])
PY
  )
  curl --fail --insecure --silent --show-error -X POST -H "Origin: $origin" -H 'Content-Type: application/json' --data '{"action":"shutdown"}' "$origin/__test/control" >>"$evidence/failure-shutdown.txt" 2>&1 || true
fi
set +e
wait "$go_pid"
go_exit=$?
set -e
go_pid=
printf '%s\n' "$go_exit" >"$evidence/go-test.exit"
python3 - "$evidence/browser-result.json" "$evidence/network-whitelist.json" <<'PY'
import collections, json, sys
source, target = sys.argv[1:]
data = json.load(open(source, encoding="utf-8"))
allowed = {
    (200, None), (201, None),
    (401, "AUTHENTICATION_REQUIRED"),
    (403, "PROJECT_UPDATE_NOT_ALLOWED"),
    (404, "PROJECT_CREATION_NOT_FOUND"),
    (404, "PROJECT_UPDATE_NOT_FOUND"),
    (404, "RESOURCE_NOT_FOUND"),
    (503, "RESULT_UNCONFIRMED"),
    (503, None),
}
unexpected = [item for item in data.get("network", []) if (item.get("status"), item.get("code")) not in allowed]
counts = collections.Counter(f"{item.get('status')}:{item.get('code')}" for item in data.get("network", []))
result = {"passed": data.get("passed") is True and not unexpected, "allowed": sorted(f"{status}:{code}" for status, code in allowed), "counts": dict(sorted(counts.items())), "unexpected": unexpected}
with open(target, "w", encoding="utf-8") as output: json.dump(result, output, ensure_ascii=False, indent=2); output.write("\n")
if not result["passed"]: raise SystemExit(1)
PY
printf 'mode=%s\nport=%s\nbrowser_exit=%s\ngo_test_exit=%s\n' "$mode" "$port" "$browser_exit" "$go_exit" >"$evidence/run-summary.txt"
if [[ "$browser_exit" -ne 0 || "$go_exit" -ne 0 ]]; then exit 1; fi
