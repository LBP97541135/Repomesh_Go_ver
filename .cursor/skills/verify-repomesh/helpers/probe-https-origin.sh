#!/usr/bin/env bash
# Probe a loopback HTTPS origin without sending or recording cookies.
# This is LIVE-01 / cookie-path shape only. It cannot prove a trusted public CA.
set -euo pipefail

HELPERS="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [[ -z "${REPOMESH_VERIFY_EVIDENCE:-}" ]]; then
  eval "$(python3 "$HELPERS/load-config.py" --export)"
fi

EVIDENCE="${REPOMESH_VERIFY_EVIDENCE:?set REPOMESH_VERIFY_EVIDENCE}"
ORIGIN="${1:-${REPOMESH_VERIFY_HTTPS_ORIGIN:-}}"
if [[ -z "$ORIGIN" ]]; then
  echo "usage: $0 https://127.0.0.1:<tls-port>" >&2
  echo "or set REPOMESH_VERIFY_HTTPS_ORIGIN" >&2
  exit 1
fi
ORIGIN="${ORIGIN%/}"

case "$ORIGIN" in
  https://127.0.0.1:*|https://localhost:*|https://[::1]:*)
    ;;
  *)
    echo "probe-https-origin refuses $ORIGIN; must be loopback HTTPS" >&2
    exit 1
    ;;
esac

OUT="$EVIDENCE/https-probes"
mkdir -p "$OUT"

probe() {
  local name="$1"
  local path="$2"
  local file="$OUT/${name}.json"
  local code
  # -k: self-signed loopback certs. Do not send a Cookie header. Do not save response headers
  # (Set-Cookie values must never land in evidence).
  code="$(curl -k -sS --max-time 15 -o "$file" -w '%{http_code}' "$ORIGIN$path")"
  printf '%s' "$code"
}

health_code="$(probe healthz /healthz)"
ready_code="$(probe readyz /readyz)"
session_code="$(probe session /api/session)"

fail=0
if [[ "$health_code" != 200 ]]; then
  echo "https healthz expected 200 got $health_code" >&2
  fail=1
fi
if ! grep -q '"businessReady":false' "$OUT/healthz.json" && ! grep -q '"businessReady": false' "$OUT/healthz.json"; then
  echo "https healthz businessReady is not false" >&2
  fail=1
fi
if [[ "$ready_code" != 503 ]]; then
  echo "https readyz expected 503 got $ready_code" >&2
  fail=1
fi

session_body="$(tr -d '\n' <"$OUT/session.json")"
session_note=""
if [[ "$session_code" == 401 && "$session_body" == *AUTHENTICATION_REQUIRED* ]]; then
  session_note="anonymous-live"
elif [[ "$session_code" == 503 && "$session_body" == *AUTH_NOT_CONFIGURED* ]]; then
  session_note="unconfigured"
elif [[ "$session_code" == 200 ]]; then
  echo "https /api/session was 200 with no Cookie header; unexpected for this probe" >&2
  fail=1
else
  echo "https /api/session expected 401 AUTHENTICATION_REQUIRED or 503 AUTH_NOT_CONFIGURED, got $session_code" >&2
  fail=1
fi

if [[ "$fail" -ne 0 ]]; then
  exit 1
fi

cat >"$OUT/proof.json" <<EOF
{
  "feature": "https-origin-probes",
  "origin": "$ORIGIN",
  "insecureTlsSkipVerify": true,
  "sentCookieHeader": false,
  "healthzStatus": $health_code,
  "readyzStatus": $ready_code,
  "sessionStatus": $session_code,
  "sessionShape": "$session_note",
  "note": "Self-signed loopback HTTPS can return these codes. Chrome Not secure is LIVE-01 trusted-CA, not this probe. Cookie acceptance still needs the holder browser on this exact origin."
}
EOF

echo "probed https origin=$ORIGIN session=$session_note evidence=$OUT"
