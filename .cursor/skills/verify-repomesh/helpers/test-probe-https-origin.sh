#!/usr/bin/env bash
# URL-guard checks for probe-https-origin.sh. Does not start RepoMesh.
set -euo pipefail

HELPERS="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROBE="$HELPERS/probe-https-origin.sh"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
export REPOMESH_VERIFY_EVIDENCE="$TMP/evidence"
mkdir -p "$REPOMESH_VERIFY_EVIDENCE"

fail=0
if bash "$PROBE" "http://127.0.0.1:18443" >/dev/null 2>"$TMP/http.err"; then
  echo "http origin should fail" >&2
  fail=1
fi
if ! grep -q 'loopback HTTPS' "$TMP/http.err"; then
  echo "http rejection message missing" >&2
  fail=1
fi
if bash "$PROBE" "https://github.com" >/dev/null 2>"$TMP/pub.err"; then
  echo "public https origin should fail" >&2
  fail=1
fi
if ! grep -q 'loopback HTTPS' "$TMP/pub.err"; then
  echo "public rejection message missing" >&2
  fail=1
fi
if [[ "$fail" -ne 0 ]]; then
  exit 1
fi
echo "probe-https-origin URL guards passed"
