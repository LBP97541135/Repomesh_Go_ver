#!/usr/bin/env bash
set -euo pipefail
HELPERS="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
python3 "$HELPERS/load-config.py" "$@"
