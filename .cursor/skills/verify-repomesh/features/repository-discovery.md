# Repository discovery

Repository discovery is the signed-in home page. The user sees the current account, GitHub connection status, and repositories whose user-read qualification has been verified. App work capability is a separate pill.

## Sub-features

- `home-account` shows `session.user.displayName` and connection status.
- `home-list` lists verified-readable repositories with App capability pills.
- `home-search` filters the current discovery result.
- `home-pages` walks `下一页` and `上一页` while `coverage` stays honest.
- `home-refresh` clicks `重新发现`.

## How to get to it (user POV)

- After a confirmed login, open `/` or choose `稍后处理，返回工作区`.
- Choose brand `RepoMesh` from an inner page.

## Driving it with Playwright and curl

Preconditions:

- Live session cookie is present. `/api/session` is 200.
- Doctor is live mode on the process you started.
- On the default local HTTP origin this feature is `verified-unreachable`. Prerequisite: HTTPS origin in `auth.json` plus a browser that accepted `__Host-repomesh-session`. Source: `web/src/RepositoryHome.tsx`, `internal/web/auth.go`.

- **Unconfigured gate.** `helpers/drive-local-gates.sh` opens `/` on `http://127.0.0.1:<port>`. Page is the SPA shell. `GET /api/repositories` is 503 `AUTH_NOT_CONFIGURED`. That is the local reachable proof. It is not a signed-in list.
- **Open home.** Heading `仓库与账号连接`. Pill `已登录` is visible. Connection label is `GitHub 已连接` only while `githubConnection.status` is `connected`. Older than 60 seconds it becomes `GitHub 连接待确认`.
- **List.** `GET /api/repositories` returns items and `coverage`. Handler budget is 45s; the browser allows 60s. Same query shares one in-flight GET; workspace remount must not abort it. End page is `partial` unless the product later changes that rule. Successful batches reuse for 10 minutes; `refresh=1` or `重新发现` starts a new batch.
- **First read.** 503 `RESULT_UNCONFIRMED` until the coordinator worker fills the batch. Home retries that, `ABORTED`, and `NETWORK_ERROR` up to three times. Do not open a second empty batch for the same query.
- **Pills.** Allowed App work is `App 能力已核实`. Denied is `App 工作授权不足`. Unknown is `App 能力待确认`. Subline `读取资格已核实`.
- **Search.** Fill `#repository-search` and submit `搜索`. Names in the list match the query or the empty copy appears.
- **Pages.** LIVE-08 walks `limit=5` (or UI `下一页`) until unique ids match the default-page count. Terminal `coverage=partial` / `APP_INSTALLATION_SCOPE` is honest. Missing out-of-install private is not `denied`.
- **Reconnect entry.** Button `重新连接 GitHub` goes to `/login` in reconnect mode.

## Gotchas

- Unconfigured Web never reaches this page. `main.tsx` keeps `AuthEntry` until `authenticated`.
- Names appear only after user-read verification. Unconfirmed selections hide names. Stale user-read (`observed_at` > 60s) re-checks; failure omits the row. That 60s cache is not the 10-minute discovery batch TTL and not the 60s connection observation window.
- Connection `unknown` after 60s shows `GitHub 连接待确认` with notice `目前无法确认 GitHub 连接状态…`. Click `检查当前登录状态`. Do not treat it as an empty repository list.
- Do not record repository names of private fixtures in shared evidence. Stable IDs are enough (`repo_00000000001367444901` for GitHub id `1367444901`).
- computerUse paraphrases pills. The allowed fixture copy is `App 能力已核实`.
- `RepositoryHome` does not abort the shared list fetch on unmount. `RepositoryPicker` may pass `signal`. Do not "fix" a hang by aborting the in-flight GET.
- After 30 minutes idle on a **configured** HTTPS origin, the UI is `开始使用 RepoMesh` with button `使用 GitHub 登录`. That is anonymous configured, not unconfigured. Unconfigured stays `暂时无法确认登录状态` with no GitHub button.
- Do not attach full repository-list screenshots to git or PRs. They show private names. Record stable ids, coverage, and pill copy instead.
