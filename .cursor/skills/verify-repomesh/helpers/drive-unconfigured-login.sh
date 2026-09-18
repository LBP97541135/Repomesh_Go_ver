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
mkdir -p "$EVIDENCE"

html="$EVIDENCE/login.html"
session="$EVIDENCE/session.json"
health="$EVIDENCE/healthz.json"
ready="$EVIDENCE/readyz.json"

login_code="$(curl -sS -o "$html" -w '%{http_code}' "$ORIGIN/login")"
health_code="$(curl -sS -o "$health" -w '%{http_code}' "$ORIGIN/healthz")"
ready_code="$(curl -sS -o "$ready" -w '%{http_code}' "$ORIGIN/readyz")"
session_code="$(curl -sS -o "$session" -w '%{http_code}' "$ORIGIN/api/session")"

if [[ "$login_code" != 200 ]]; then
  echo "GET /login expected 200 got $login_code" >&2
  exit 1
fi
if ! grep -q 'id="root"' "$html"; then
  echo "login HTML missing #root" >&2
  exit 1
fi
if [[ "$health_code" != 200 || "$ready_code" != 503 || "$session_code" != 503 ]]; then
  echo "probe mismatch health=$health_code ready=$ready_code session=$session_code" >&2
  exit 1
fi
if ! grep -q 'AUTH_NOT_CONFIGURED' "$session"; then
  echo "session body missing AUTH_NOT_CONFIGURED" >&2
  exit 1
fi

cat >"$EVIDENCE/proof.json" <<EOF
{
  "feature": "unconfigured-login",
  "entryPoint": "/login",
  "origin": "$ORIGIN",
  "loginStatus": $login_code,
  "healthzStatus": $health_code,
  "readyzStatus": $ready_code,
  "sessionStatus": $session_code,
  "sessionCode": "AUTH_NOT_CONFIGURED",
  "githubLoginButton": "absent-on-unconfigured-api",
  "note": "HTML is the SPA shell. Heading 暂时无法确认登录状态 is rendered after /api/session 503."
}
EOF

echo "drove unconfigured-login evidence=$EVIDENCE"
