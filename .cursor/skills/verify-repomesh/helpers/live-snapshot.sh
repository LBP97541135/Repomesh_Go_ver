#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: $0 <output.json>" >&2
  exit 1
fi

OUT="$1"
URL_FILE="${REPOMESH_DATABASE_URL_FILE:-$HOME/.config/repomesh/database-url}"
if [[ ! -f "$URL_FILE" ]]; then
  echo "missing database url file" >&2
  exit 1
fi
DBURL="$(cat "$URL_FILE")"
FIXTURE="${REPOMESH_INSTALLED_PRIVATE_REPO_ID:-1367444901}"

mkdir -p "$(dirname "$OUT")"

psql "$DBURL" --no-psqlrc -v ON_ERROR_STOP=1 -qAt -c "SELECT json_build_object(
  'captured_at', (now() AT TIME ZONE 'utc'),
  'account', (
    SELECT json_build_object('github_id', github_id, 'display_name', display_name)
    FROM repomesh_access.accounts
    LIMIT 1
  ),
  'attempt_counts', (
    SELECT coalesce(json_agg(json_build_object('purpose', purpose, 'state', state, 'n', n) ORDER BY purpose, state), '[]'::json)
    FROM (
      SELECT purpose, state, count(*)::int AS n
      FROM repomesh_access.attempts
      GROUP BY purpose, state
    ) s
  ),
  'latest_attempts', (
    SELECT coalesce(json_agg(json_build_object(
      'id', id,
      'purpose', purpose,
      'state', state,
      'expected_github_id', expected_github_id,
      'observed_at', observed_at,
      'created_at', created_at
    ) ORDER BY created_at DESC), '[]'::json)
    FROM (
      SELECT id, purpose, state, expected_github_id, observed_at, created_at
      FROM repomesh_access.attempts
      ORDER BY created_at DESC
      LIMIT 8
    ) a
  ),
  'sessions', (
    SELECT coalesce(json_agg(json_build_object(
      'generation', generation,
      'revoked', revoked,
      'created_at', created_at,
      'last_active_at', last_active_at
    ) ORDER BY last_active_at DESC), '[]'::json)
    FROM (
      SELECT generation, revoked, created_at, last_active_at
      FROM repomesh_access.sessions
      ORDER BY last_active_at DESC
      LIMIT 8
    ) s
  ),
  'connection', (
    SELECT json_build_object(
      'status', status,
      'access_epoch', access_epoch,
      'revision', revision,
      'refresh_state', refresh_state,
      'observed_at', observed_at,
      'access_expires_at', access_expires_at,
      'refresh_expires_at', refresh_expires_at,
      'credential_committed_at', credential_committed_at
    )
    FROM repomesh_access.connections
    LIMIT 1
  ),
  'latest_discovery', (
    SELECT json_build_object(
      'complete', b.complete,
      'had_success', b.had_success,
      'delivered', b.delivered,
      'access_epoch', b.access_epoch,
      'observed_at', b.observed_at,
      'item_count', (SELECT count(*) FROM repomesh_access.discovered_repositories r WHERE r.batch = b.id),
      'fixture_present', EXISTS (
        SELECT 1 FROM repomesh_access.discovered_repositories r
        WHERE r.batch = b.id AND r.github_id = ${FIXTURE}::bigint
      )
    )
    FROM repomesh_access.discovery_batches b
    ORDER BY b.observed_at DESC NULLS LAST
    LIMIT 1
  )
);" > "$OUT"

python3 - "$OUT" <<'PY'
import json, sys
path = sys.argv[1]
with open(path, encoding="utf-8") as f:
    data = json.loads(f.read())
with open(path, "w", encoding="utf-8") as f:
    json.dump(data, f, indent=2, ensure_ascii=False)
    f.write("\n")
print(path)
PY
