#!/usr/bin/env bash
set -euo pipefail

HELPERS="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [[ -z "${REPOMESH_VERIFY_ORIGIN:-}" || -z "${REPOMESH_VERIFY_STATE:-}" ]]; then
  eval "$(python3 "$HELPERS/load-config.py" --export)"
fi

STATE="${REPOMESH_VERIFY_STATE:?set REPOMESH_VERIFY_STATE}"
EVIDENCE="${REPOMESH_VERIFY_EVIDENCE:?set REPOMESH_VERIFY_EVIDENCE}"
if [[ -f "$STATE/origin" ]]; then
  ORIGIN="$(cat "$STATE/origin")"
else
  ORIGIN="${REPOMESH_VERIFY_ORIGIN:?set REPOMESH_VERIFY_ORIGIN}"
fi
mkdir -p "$EVIDENCE/gates"

probe() {
  local name="$1"
  local path="$2"
  local file="$EVIDENCE/gates/${name}.body"
  local code
  code="$(curl -sS -o "$file" -w '%{http_code}' "$ORIGIN$path")"
  printf '%s' "$code"
}

home_code="$(probe home /)"
projects_code="$(probe projects /projects)"
new_project_code="$(probe projects-new /projects/new)"
models_code="$(probe models /settings/models)"
login_code="$(probe login /login)"
session_code="$(probe session /api/session)"
repos_api="$(probe repositories-api /api/repositories)"
projects_api="$(probe projects-api /api/projects)"
models_api="$(probe models-api /api/model-providers)"

fail=0
for code in "$home_code" "$projects_code" "$new_project_code" "$models_code" "$login_code"; do
  if [[ "$code" != 200 ]]; then
    echo "expected SPA 200, got a page status $code" >&2
    fail=1
  fi
done
for pair in "session:$session_code" "repositories-api:$repos_api" "projects-api:$projects_api"; do
  name="${pair%%:*}"
  code="${pair#*:}"
  if [[ "$code" != 503 ]]; then
    echo "$name API expected 503 AUTH_NOT_CONFIGURED got $code" >&2
    fail=1
  fi
done
if ! grep -q 'AUTH_NOT_CONFIGURED' "$EVIDENCE/gates/session.body"; then
  echo "session missing AUTH_NOT_CONFIGURED" >&2
  fail=1
fi
if ! grep -q 'AUTH_NOT_CONFIGURED' "$EVIDENCE/gates/repositories-api.body"; then
  echo "repositories API missing AUTH_NOT_CONFIGURED" >&2
  fail=1
fi
if ! grep -q 'AUTH_NOT_CONFIGURED' "$EVIDENCE/gates/projects-api.body"; then
  echo "projects API missing AUTH_NOT_CONFIGURED" >&2
  fail=1
fi
if [[ "$models_api" != 404 ]]; then
  echo "model-providers API expected 404 not_implemented on unconfigured Web got $models_api" >&2
  fail=1
fi
if ! grep -q 'not_implemented' "$EVIDENCE/gates/models-api.body"; then
  echo "model-providers API missing not_implemented" >&2
  fail=1
fi
if [[ "$fail" -ne 0 ]]; then
  exit 1
fi

cat >"$EVIDENCE/gates/proof.json" <<EOF
{
  "feature": "local-unconfigured-gates",
  "origin": "$ORIGIN",
  "pages": {
    "/": $home_code,
    "/login": $login_code,
    "/projects": $projects_code,
    "/projects/new": $new_project_code,
    "/settings/models": $models_code
  },
  "apis": {
    "/api/session": $session_code,
    "/api/repositories": $repos_api,
    "/api/projects": $projects_api,
    "/api/model-providers": $models_api
  },
  "sessionCode": "AUTH_NOT_CONFIGURED",
  "modelProvidersUnconfigured": "404 not_implemented because registerModels no-ops when Models.Service is nil",
  "signedInFeatures": "verified-unreachable",
  "prerequisite": "Authenticated session cookie. Product sets __Host-repomesh-session with Secure=true, so a local HTTP origin cannot accept a live GitHub session. account-a-live also needs auth.json HTTPS origin, PostgreSQL, wrap root, coordinator, and holder GitHub clicks."
}
EOF

echo "drove local unconfigured gates evidence=$EVIDENCE/gates"
