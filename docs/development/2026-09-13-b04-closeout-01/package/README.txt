RepoMesh 0.4.0-b04-integrated-20260913-r2 (linux/amd64) - authentication, project management and model-source save with database tools

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
