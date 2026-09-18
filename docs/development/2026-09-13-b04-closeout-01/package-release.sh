#!/usr/bin/env bash
set -euo pipefail

# Replicates scripts/build.ps1 because this environment has no pwsh.
# Same layout, same refusal to overwrite an existing dist/repomesh-$Version.

Version=${1:?usage: package-release.sh <version>}
if [[ ! "$Version" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]]; then
  echo "invalid version" >&2
  exit 2
fi

repo=$(cd "$(dirname "$0")/../../.." && pwd)
releasePath="$repo/dist/repomesh-$Version"
if [[ -e "$releasePath" ]]; then
  echo "Release directory already exists: $releasePath. Choose a new version." >&2
  exit 1
fi

cd "$repo"
npm --prefix web ci
npm --prefix web run build
mkdir -p "$releasePath/bin" "$releasePath/web" "$releasePath/configs"

targetOS=$(go env GOOS)
targetArch=$(go env GOARCH)
for name in repomesh-web repomesh-coordinator repomesh-host-executor; do
  go build -buildvcs=false -trimpath -ldflags "-X repomesh.local/repomesh/internal/buildinfo.Version=$Version" -o "$releasePath/bin/$name" "./cmd/$name"
done
cp -a web/dist "$releasePath/web/dist"
cp configs/repomesh.env.example "$releasePath/configs/repomesh.env.example"
cp configs/auth.example.json "$releasePath/configs/auth.example.json"
cp web/package-lock.json "$releasePath/web/package-lock.json"
cp README.md "$releasePath/SOURCE-README.md"
cat >"$releasePath/README.txt" <<EOF
RepoMesh $Version ($targetOS/$targetArch) - authentication, project management and model-source save with database tools

Run from this bundle directory:
  ./bin/repomesh-web --assets ./web/dist
  ./bin/repomesh-coordinator --version
  ./bin/repomesh-host-executor --version

Set REPOMESH_DATABASE_URL before explicit database operations:
  ./bin/repomesh-web db check
  ./bin/repomesh-web db migrate --timeout 30s
Database commands do not require web assets or start HTTP.
Web without authentication configuration does not connect to a database.
Configured Web and coordinator require current migrations; they never migrate automatically.

Import deployment execution sources with a database deployment identity:
  ./bin/repomesh-web sources import --file PATH
  ./bin/repomesh-web sources result --import-id UUID
Do not import from the browser.

For Linux authentication, copy configs/auth.example.json to your deployment directory.
Configure your GitHub App, exact HTTPS origin/callback and protected secret files.
Set REPOMESH_AUTH_CONFIG and REPOMESH_DATABASE_URL for both processes.
Run ./bin/repomesh-web --assets ./web/dist and ./bin/repomesh-coordinator.
Do not place secret material in this release directory.
Use direct TLS files or a controlled HTTPS reverse proxy; never weaken Secure cookies.

Web: http://127.0.0.1:8080 ; /healthz = process only ; /readyz = 503.
Coordinator exits 1 without auth configuration; configured it performs auth maintenance.
Host executor exits 1: not implemented.
Project management supports pending-configuration projects, listing, project metadata edits,
explicit repository additions, pinned configuration references and original-operation recovery.
Model settings persist owner provider snapshots and original save receipts. Keys leave the page
immediately and are not stored in the browser. This bundle does not send model HTTP requests.
Issue management, runtime execution, AgentTeams, Docker and Python integration remain unimplemented.
All three binaries and web assets belong to this one release bundle.
Local authentication, project management and model-source checks do not prove real GitHub,
real model calls or full business acceptance.
EOF

python3 - "$releasePath" "$Version" "$targetOS" "$targetArch" <<'PY'
import hashlib, json, pathlib, sys
release, version, target_os, target_arch = sys.argv[1:]
root = pathlib.Path(release)
artifacts = {}
for path in sorted(p for p in root.rglob("*") if p.is_file()):
    relative = path.relative_to(root).as_posix()
    artifacts[relative] = hashlib.sha256(path.read_bytes()).hexdigest()
payload = {
    "version": version,
    "stage": "model-sources-local",
    "businessReady": False,
    "target": f"{target_os}/{target_arch}",
    "artifacts": artifacts,
}
(root / "release.json").write_text(json.dumps(payload, indent=2) + "\n", encoding="utf-8")
print(f"Release bundle: {root}")
PY
