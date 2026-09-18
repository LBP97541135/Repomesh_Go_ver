#!/usr/bin/env bash
set -euo pipefail

bundle=${1:?usage: run-release-validation.sh bundle evidence-directory}
evidence=${2:?usage: run-release-validation.sh bundle evidence-directory}
repo=$(cd "$(dirname "$0")/../../.." && pwd)
bundle=$(cd "$bundle" && pwd)
evidence=$(cd "$evidence" && pwd)
postgres_bin=/usr/lib/postgresql/17/bin
version=$(python3 - "$bundle/release.json" <<'PY'
import json, sys
print(json.load(open(sys.argv[1], encoding="utf-8-sig"))["version"])
PY
)
runtime=$(mktemp -d /tmp/repomesh-b03-integration-release.XXXXXX)
data="$runtime/data"
password_file="$runtime/password"
password=$(openssl rand -hex 32)
printf '%s' "$password" >"$password_file"
chmod 0600 "$password_file"
port=$(python3 - <<'PY'
import socket
s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()
PY
)
postgres_started=false
web_pid=

cleanup() {
  status=$?
  set +e
  if [[ -n "$web_pid" ]] && kill -0 "$web_pid" 2>/dev/null; then kill -TERM "$web_pid"; wait "$web_pid"; fi
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
  printf 'runtime directory removal: completed\n' >>"$evidence/cleanup.txt"
  exit "$status"
}
trap cleanup EXIT

python3 - "$bundle" "$evidence/artifact-check.json" <<'PY'
import hashlib, json, pathlib, sys
root=pathlib.Path(sys.argv[1]); release=json.loads((root/'release.json').read_text(encoding='utf-8-sig'))
actual={}
for path in sorted(p for p in root.rglob('*') if p.is_file() and p.name!='release.json'):
    actual[path.relative_to(root).as_posix()]=hashlib.sha256(path.read_bytes()).hexdigest()
result={'version':release['version'],'stage':release['stage'],'businessReady':release['businessReady'],'target':release['target'],'manifestArtifactCount':len(release['artifacts']),'actualArtifactCount':len(actual),'setMatches':set(release['artifacts'])==set(actual),'hashesMatch':release['artifacts']==actual,'releaseJsonSha256':hashlib.sha256((root/'release.json').read_bytes()).hexdigest()}
pathlib.Path(sys.argv[2]).write_text(json.dumps(result,indent=2)+'\n')
if not result['setMatches'] or not result['hashesMatch'] or result['businessReady'] is not False: raise SystemExit(1)
PY
for name in repomesh-web repomesh-coordinator repomesh-host-executor; do
  "$bundle/bin/$name" --version >"$evidence/$name-version.txt" 2>&1
  grep -Fx "$name $version" "$evidence/$name-version.txt" >/dev/null
done

"$postgres_bin/initdb" -D "$data" -U repomesh_test --pwfile="$password_file" --auth-host=scram-sha-256 --auth-local=scram-sha-256 --encoding=UTF8 --locale=C >"$evidence/initdb.txt" 2>&1
python3 - "$password_file" <<'PY'
import os, sys
os.unlink(sys.argv[1])
PY
printf "listen_addresses='127.0.0.1'\nport=%s\nunix_socket_directories=''\nfsync=on\n" "$port" >>"$data/postgresql.conf"
"$postgres_bin/pg_ctl" -D "$data" -l "$runtime/postgres.log" -w start >"$evidence/postgres-start.txt" 2>&1
postgres_started=true
database_url="postgres://repomesh_test:${password}@127.0.0.1:${port}/postgres?sslmode=disable"
run_db() {
  name=$1; expected=$2; shift 2
  set +e
  REPOMESH_DATABASE_URL="$database_url" "$bundle/bin/repomesh-web" "$@" >"$evidence/$name.txt" 2>&1
  exit_code=$?
  set -e
  printf '%s\n' "$exit_code" >"$evidence/$name.exit"
  [[ "$exit_code" -eq "$expected" ]]
}
run_db db-check-empty 1 db check
grep -F 'schema status=missing current=0 target=4 pending=4' "$evidence/db-check-empty.txt" >/dev/null
run_db db-migrate 0 db migrate --timeout 30s
grep -F 'schema status=current current=4 target=4 pending=0' "$evidence/db-migrate.txt" >/dev/null
run_db db-migrate-repeat 0 db migrate --timeout 30s
run_db db-check-final 0 db check
grep -F 'schema status=current current=4 target=4 pending=0' "$evidence/db-check-final.txt" >/dev/null

set +e
REPOMESH_TEST_DATABASE_URL="$database_url" REPOMESH_B03_TEST_RELEASE_DIR="$bundle" go test -count=1 -run '^TestPostgresProjectProcessRestart$' -v -timeout 2m ./internal/web >"$evidence/process-restart.txt" 2>&1
restart_exit=$?
set -e
printf '%s\n' "$restart_exit" >"$evidence/process-restart.exit"
[[ "$restart_exit" -eq 0 ]]
grep -F -- '--- PASS: TestPostgresProjectProcessRestart' "$evidence/process-restart.txt" >/dev/null

web_port=$(python3 - <<'PY'
import socket
s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()
PY
)
env -u REPOMESH_AUTH_CONFIG -u REPOMESH_DATABASE_URL "$bundle/bin/repomesh-web" --addr "127.0.0.1:$web_port" --assets "$bundle/web/dist" >"$evidence/unconfigured-web.txt" 2>&1 &
web_pid=$!
for _ in $(seq 1 200); do curl --silent --fail "http://127.0.0.1:$web_port/healthz" >"$evidence/healthz.json" && break; kill -0 "$web_pid"; sleep 0.05; done
python3 - "$evidence/healthz.json" <<'PY'
import json, sys
d=json.load(open(sys.argv[1])); assert d['businessReady'] is False
PY
printf 'path\tstatus\n' >"$evidence/http-status.tsv"
for path in /healthz /readyz /api/projects /projects /project-creations/00000000-0000-4000-8000-000000000001; do
  code=$(curl --silent --output /dev/null --write-out '%{http_code}' "http://127.0.0.1:$web_port$path")
  printf '%s\t%s\n' "$path" "$code" >>"$evidence/http-status.tsv"
done
grep -Fx $'/healthz\t200' "$evidence/http-status.tsv" >/dev/null
grep -Fx $'/readyz\t503' "$evidence/http-status.tsv" >/dev/null
grep -Fx $'/api/projects\t503' "$evidence/http-status.tsv" >/dev/null
grep -Fx $'/projects\t200' "$evidence/http-status.tsv" >/dev/null
grep -Fx $'/project-creations/00000000-0000-4000-8000-000000000001\t200' "$evidence/http-status.tsv" >/dev/null
kill -TERM "$web_pid"
set +e; wait "$web_pid"; web_exit=$?; set -e
web_pid=
printf '%s\n' "$web_exit" >"$evidence/unconfigured-web.exit"
[[ "$web_exit" -eq 0 ]]

set +e
env -u REPOMESH_AUTH_CONFIG -u REPOMESH_DATABASE_URL "$bundle/bin/repomesh-coordinator" >"$evidence/coordinator.txt" 2>&1
coordinator_exit=$?
env -u REPOMESH_AUTH_CONFIG -u REPOMESH_DATABASE_URL "$bundle/bin/repomesh-host-executor" >"$evidence/host-executor.txt" 2>&1
host_exit=$?
set -e
printf '%s\n' "$coordinator_exit" >"$evidence/coordinator.exit"
printf '%s\n' "$host_exit" >"$evidence/host-executor.exit"
[[ "$coordinator_exit" -eq 1 && "$host_exit" -eq 1 ]]
grep -F 'authentication is not configured' "$evidence/coordinator.txt" >/dev/null
grep -F 'not implemented' "$evidence/host-executor.txt" >/dev/null

python3 - "$evidence" "$version" <<'PY'
import json, pathlib, sys
root=pathlib.Path(sys.argv[1]); version=sys.argv[2]
summary={'version':version,'businessReady':False,'artifactCheck':'passed','versions':'three matched','migration':{'empty':[0,4,4],'current':[4,4,0],'repeat':[4,4,0]},'processRestart':{'passed':1,'skipped':0},'http':{'healthz':200,'readyz':503,'apiProjects':503,'projects':200,'projectCreation':200},'coordinatorExit':1,'hostExecutorExit':1}
(root/'summary.json').write_text(json.dumps(summary,indent=2)+'\n')
PY
