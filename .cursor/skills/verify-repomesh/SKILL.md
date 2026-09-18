---
name: verify-repomesh
description: Drive the RepoMesh web UI (login, workspace, projects, model settings) the way a user does. Use when proving a page, HTTP contract, or B02 live GitHub acceptance against a real process, not when only running go test.
---

# Verify RepoMesh

Primary surface is the Go-served React UI. Users open pages in a browser. The same origin exposes `/api/*`. Companion processes are `repomesh-coordinator` (required for live GitHub) and `repomesh-host-executor` (unimplemented, exit 1). Vite `5173` is frontend-only and has no API proxy. Do not use it for auth proof.

This skill is for the next agent. Read the feature map before driving. Drive every listed entry point for the feature you claim, or report the unmet precondition.

The default drive origin is local HTTP, for example `http://127.0.0.1:18080`. Do not treat `https://repomesh.example.com` or any public hostname as the skill default. Product live GitHub auth still sets `__Host-` cookies with `Secure`. Local HTTP never proves those cookies were accepted. Mark that path `verified-unreachable` until the operator supplies a matching HTTPS origin in `auth.json`.

操作员逐步填写说明见 [OPERATOR-GUIDE.md](OPERATOR-GUIDE.md)。

## Operator config

Copy the example, then edit the operator file. Helpers read it. Do not commit `config.yaml`.

```bash
cp .cursor/skills/verify-repomesh/config.example.yaml .cursor/skills/verify-repomesh/config.yaml
```

```bash
python3 .cursor/skills/verify-repomesh/helpers/load-config.py --check
```

The yaml holds only what the operator must supply. Secret bodies stay in files on the host. Fields:

- `listen.host` / `listen.port` and optional `origin` — local loopback HTTP only
- `run_scope` — `unconfigured-only`, `account-a-live`, or `restore-leftovers`
- `auth_config_path` — absolute path to `auth.json`; optional for `unconfigured-only`
- `reuse_existing_postgres_and_wrap_root`
- `github_account_a_login`
- `installed_private_repo_id` and optional `out_of_install_repo_id`
- `confirmations.holder_will_click_github_ui` and the restore leftover flags
- `database.url_env_var` or `database.connection_url_file` — not the password

`unconfigured-only` can use the committed example. Live scopes fail until `config.yaml` has those fields. The loader rejects a public hostname as the skill origin.

Override the file with `REPOMESH_VERIFY_CONFIG` if needed.

## Operator packet for B02 live

B02 live is GitHub App login, reconnect, discovery, and natural refresh. It is not a model-provider test. Do not ask for a model API key.

Fill `config.yaml` first. The operator must place secrets on the Linux host that runs Web and coordinator. The operator must not paste secret bodies into chat, evidence JSON, or this repo.

Live scope still needs these host files. Paths go in `auth.json` or `config.yaml`. Bodies do not.

| File | Role | Constraints |
| --- | --- | --- |
| `auth.json` | Non-secret App IDs, HTTPS origin, callback, secret paths | Copy from `configs/auth.example.json`. Unknown fields rejected. Product rejects a non-HTTPS `origin`. |
| client secret file | GitHub App OAuth client secret | Regular file, `0600`, not a symlink, ≤64 KiB. |
| App RSA PEM | GitHub App private key | Same file rules. PKCS#1 or PKCS#8, ≥2048 bits. |
| wrap root | Envelope-encryption root | Raw 32 random bytes, not hex text. Do not regenerate a same-id root over an existing database. |
| TLS cert and key, or a reverse proxy | Browser-trusted HTTPS | `__Host-` cookies require Secure. HTTP origins cannot prove live login. |

`callbackUrl` in `auth.json` must equal `<https-origin>/api/auth/github/callback` character for character. That HTTPS origin is a product requirement, not the skill's default drive URL. Launch still listens on the local `listen.host:listen.port` from yaml.

**Do not provide.**

- Model vendor API keys, billing keys, or B04/B05 test permits.
- Account B, collaborator invites, or a second-account plan. `LIVE-05` and `LIVE-08-USER-READ` are `DEFERRED_BY_USER`.
- Docker socket, AgentTeams credentials, or host-executor setup.
- Cookie values, OAuth `code`/`state`, PEM text, wrap-root bytes, database passwords, or raw HAR.

**Optional restore packet.** Only if `run_scope` is `restore-leftovers`. Confirm in GitHub UI, then set the restore booleans in `config.yaml`.

- App visibility is private again.
- Installation `161284386` is gone.
- Collaborator and pending invite on the A-owned fixture repo are gone.
- B OAuth grant for the App is revoked.
- A installation `161172403` still selects only repository `1367444901` with Metadata read, Contents write, Pull requests write.
- Account A cannot see repository `1368000734`.

Historical origin `https://repomesh.bohanxu.me:8443` and old PIDs are record-time facts. Do not reuse them. Do not put that hostname in `config.yaml` `origin`.

## Launch

Work from the repository root on Linux. Auth secret files are Linux-only.

```bash
cp .cursor/skills/verify-repomesh/config.example.yaml .cursor/skills/verify-repomesh/config.yaml
# edit run_scope and live fields only when the operator asked for them
RUN_ID="$(date -u +%Y%m%dT%H%M%S)-$$"
export REPOMESH_VERIFY_RUN="$RUN_ID"
export REPOMESH_VERIFY_STATE="/tmp/repomesh-verify-$RUN_ID"
export REPOMESH_VERIFY_EVIDENCE="$PWD/.cursor/skills/verify-repomesh/evidence/$RUN_ID"
python3 .cursor/skills/verify-repomesh/helpers/load-config.py --check
bash .cursor/skills/verify-repomesh/helpers/launch.sh
```

Ready when the helper prints `ready origin=http://127.0.0.1:18080` (or the yaml port) and `GET /healthz` is 200. Unconfigured stderr has `web listening` with `authentication_configured=false`.

`run_scope: unconfigured-only` starts Web with `--auth-config=`. That is the default local proof.

`account-a-live` and `restore-leftovers` start Web and coordinator on the same local listen address, using `auth_config_path` and the database URL from yaml. Doctor can then see `/api/session` 401 `AUTHENTICATION_REQUIRED`. Browser login remains `verified-unreachable` until the holder opens the HTTPS origin from `auth.json`. Local HTTP is still the launch/doctor address the skill uses.

Refuse to drive a shared instance you did not start. Default environment Web on `:8080` may already exist. This skill uses `:18080` unless yaml names another free loopback port. If `:18080` already holds a live account-A run (Web + TLS proxy), do not launch a second live process on that port and do not run `cleanup.sh` against that state directory. Unconfigured proof then uses a throwaway yaml (`REPOMESH_VERIFY_CONFIG`) on another loopback port such as `18082`.

Teardown uses the cleanup helper. It kills only the PIDs in the state directory of **this** drive. Do not `pkill` by name. Do not tear down a LIVE-09 wait instance.

## HTTPS cookie proof

Local HTTP is launch/doctor only. Cookie acceptance is a different origin.

Proven Cloud VM path (account A, loopback TLS proxy):

1. Skill Web listens `http://127.0.0.1:18080`. Coordinator shares the same `auth.json` and database.
2. A reverse proxy terminates TLS on `https://127.0.0.1:18443` and forwards to `18080`. After it decodes upstream chunked bodies it must set `Content-Length` and close. Stripping `Transfer-Encoding` without a length leaves Chrome waiting until timeout. The proxy script stays outside git.
3. `auth.json` `origin` is exactly `https://127.0.0.1:18443` and `callbackUrl` is that origin plus `/api/auth/github/callback`.
4. The holder browser opens **only** `https://127.0.0.1:18443/`. Never type `:18080` or env `:8080` in the address bar for cookie proof.
5. Self-signed certs show Chrome **Not secure**. That blocks LIVE-01 (trusted public CA). It is not by itself a cookie failure. Proceeding can still set `__Host-` cookies.
6. Record cookie **flags** only (`Secure`, `HttpOnly`, `SameSite=Lax`, `Path=/`). Never write cookie values, OAuth `code`, or HAR.
7. Session idle is 30 minutes (`last_active_at`). Keep the tab alive with `GET /api/session` on the HTTPS origin. Do not commit Chrome cookie-decrypt scripts.
8. Do not click `退出登录` until LIVE-09 natural refresh has a snapshot. Coordinator claims refresh at `access_expires_at` minus 30 seconds. Do not edit expiry in the database.

Helper for anonymous HTTPS probes (`curl -k`, no Cookie header):

```bash
bash .cursor/skills/verify-repomesh/helpers/probe-https-origin.sh https://127.0.0.1:18443
```

computerUse and similar GUI agents invent Chinese copy. Trust screenshots. The allowed App pill is `App 能力已核实`, not paraphrases.

## Doctor

```bash
bash .cursor/skills/verify-repomesh/helpers/doctor.sh
```

Loads yaml when `REPOMESH_VERIFY_ORIGIN` is unset, otherwise uses that origin or `$REPOMESH_VERIFY_STATE/origin`. The origin must be loopback HTTP. Pass when the process is listening, `/healthz` is 200 with `businessReady=false`, `/readyz` is 503, and session matches the expected mode.

- Unconfigured. `/api/session` is 503 `AUTH_NOT_CONFIGURED`. State file `mode=unconfigured`.
- Live. `/api/session` is 401 `AUTHENTICATION_REQUIRED`. Coordinator must still be running if you started it. That 401 on HTTP is not cookie proof.

## Drive

Prefer Playwright role clicks, matching `docs/development/2026-09-12-batch-02/browser-check.mjs`. Stable handles are Chinese accessible names, not coordinates.

| Control | Handle |
| --- | --- |
| Unconfigured login heading | `暂时无法确认登录状态` |
| Unconfigured retry | button `检查当前登录状态` |
| Live login | button `使用 GitHub 登录` |
| Live reconnect | workspace `重新连接 GitHub`; login page `重新连接同一账号` |
| Query prior attempt | button `查询原授权尝试` |
| Query login result | button `查询本次授权结果` |
| Query reconnect result | button `查询本次连接结果` |
| Return to workspace | button `稍后处理，返回工作区` |
| Sign out | button `退出登录` or `退出当前账号` |
| Connection connected | `GitHub 已连接` |
| Connection stale/unknown | `GitHub 连接待确认` (observation older than 60s is not a list failure) |
| App capability allowed | pill `App 能力已核实` |
| App capability denied | pill `App 工作授权不足` |
| App capability unknown | pill `App 能力待确认` |
| Projects | button `项目` and heading `项目` |
| New project | button `新建项目` |
| Save project | button `确认保存项目` |
| Model settings | muted header `模型设置`; page heading `模型连接` |
| Save provider | button `保存供应商` |

Unconfigured recipe lives in `features/unconfigured-login.md`. Helper:

```bash
bash .cursor/skills/verify-repomesh/helpers/drive-unconfigured-login.sh
```

Signed-in routes on the same local unconfigured process stay on the auth shell. Record that with:

```bash
bash .cursor/skills/verify-repomesh/helpers/drive-local-gates.sh
```

Live GitHub recipe lives in `features/live-github-auth.md`. Copy `docs/development/2026-09-12-b02-external-preparation/live-acceptance-template.md` into a new `docs/development/<date>-b026-<topic>/` directory. Do not overwrite old LIVE evidence.

The current Cloud VM account-A run is [CHECKLIST.md](../../../docs/development/2026-09-14-b026-cloud-live-01/CHECKLIST.md). After every LIVE item, update that checklist, write a snapshot with `helpers/live-snapshot.sh`, and append `docs/development/2026-09-14-b026-cloud-live-01/decisions.tsv`. A new origin or App gets a new dated directory. Do not inherit PASS from `https://repomesh.bohanxu.me:8443`. Do not mark B02 `VERIFIED` from a skill drive.

Local batch script is not a user-path proof:

```bash
pwsh -NoProfile -File scripts/verify-batch.ps1 -Batch B02 -PostgresBin /usr/lib/postgresql/17/bin
```

That emits `LOCAL_VERIFIED` only. Fixture browsers in `browser-check.mjs` isolate the external system with `page.route`. Live GitHub must hit `github.com` and `api.github.com` for real.

## Evidence

Store proof under `$REPOMESH_VERIFY_EVIDENCE`. Cleanup must not delete that directory.

Required for a user-path claim:

- HTTP status and error `code` for `/healthz`, `/readyz`, `/api/session`, and the feature's write or query.
- Screenshot or ARIA-equivalent of the page after the user action, with the RepoMesh heading visible.
- For mutations, a second read of the same resource or a read-only database snapshot. No secrets in the snapshot.
- Feature id and entry point recorded in `proof.json`.

Forbidden in evidence: Cookie values, `authorizationUrl`, OAuth `code`/`state`, CSRF tokens, PEM, wrap-root bytes, API keys, database URLs with passwords, raw HAR, private repository names, full discovery screenshots that show those names.

`/readyz` 503 and `businessReady=false` are expected. They are not failures.

## Cleanup

```bash
bash .cursor/skills/verify-repomesh/helpers/cleanup.sh
```

Kills only the PIDs in `$REPOMESH_VERIFY_STATE/web.pid` and `coordinator.pid`. Removes the state directory. Leaves `$REPOMESH_VERIFY_EVIDENCE` in place.

## Helpers

All helpers are in `.cursor/skills/verify-repomesh/helpers/`. Invoke them from the repository root after the Launch environment variables are set. They load `config.yaml` (or the example) through `load-config.py`.

- `load-config.py` / `load-config.sh` parse yaml, reject public drive origins, and fail if required fields for `run_scope` are missing. `--check`, `--export`, `--json`.
- `test-load-config.py` checks the loader against the example and missing live fields.
- `launch.sh` dispatches on `run_scope`.
- `launch-unconfigured.sh` builds `web/dist` if `index.html` is missing, starts Web with `--auth-config=`, writes origin and pid.
- `doctor.sh` is read-only and refuses a non-local origin.
- `drive-unconfigured-login.sh` exercises `/login` on the unconfigured process and writes `proof.json`.
- `drive-local-gates.sh` opens `/`, `/projects`, `/projects/new`, and `/settings/models` on that process and records signed-in features as `verified-unreachable` when session is `AUTH_NOT_CONFIGURED`. Unconfigured `/api/model-providers` is 404 `not_implemented` because those routes are not registered without a model service.
- `cleanup.sh` tears down PIDs from the state directory of this drive only.
- `live-snapshot.sh` writes a read-only JSON snapshot of account, attempts, sessions, connection epoch/revision/expiry, and latest discovery. No tokens, cookies, or PEM. Needs `REPOMESH_DATABASE_URL_FILE` or `$HOME/.config/repomesh/database-url`.
- `probe-https-origin.sh` curls a loopback HTTPS origin with `curl -k`, no Cookie header, and writes `https-probes/proof.json`. Rejects HTTP and public hosts.
- `test-probe-https-origin.sh` checks those URL guards without starting RepoMesh.

## Feature map

`.cursor/skills/verify-repomesh/features/README.md`
