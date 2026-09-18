# 2026-09-14 Cloud VM B02.6 验证清单

本文件是这一轮真实验收的唯一进度表。每完成、阻塞或改判一项，先改这一页，再继续下一项。不要改写 `docs/development/2026-09-12-b026-*` 的旧证据。不要把本机观察写成施工计划 `VERIFIED`。

独立复核完成前，B02 保持 `IN_PROGRESS`。

## 退出条件

同时满足才停止本轮驱动。

- 下表「本轮」列里，账号 A 可做的 LIVE 项都是 `PASS`、`BLOCKED`、`DEFERRED_BY_USER` 或 `WAITING`。
- 每个 `PASS` 都有本目录证据，且证据与 `helpers/live-snapshot.sh` 的最近快照一致。
- `LIVE-09` 等到真实刷新窗口，不改库里的过期时间。
- `LIVE-10` 在 `LIVE-09` 之后做。
- 本轮不把 B02、B03、B04 标成整批 `VERIFIED`。

## 状态词

只使用这些值。`PASS` 只用于本 origin、本 App、本轮独立证据。旧 origin 的 PASS 不继承。

| 值 | 含义 |
| --- | --- |
| `PASS` | 本轮按手册判据成立，证据在本目录 |
| `FAIL` | 本轮做了，判据不成立 |
| `BLOCKED` | 缺条件，无法做完 |
| `NOT_RUN` | 还没做 |
| `DEFERRED_BY_USER` | 用户要求不做，不计 PASS |
| `OBSERVED_NOT_CLOSED` | 本轮看见了，尚未按手册收口或未独立复核 |
| `WAITING` | 在等真实时钟，禁止用改库代替 |

## 固定条件

| 项 | 值 |
| --- | --- |
| HTTPS origin | `https://127.0.0.1:18443/` |
| Web listen | `http://127.0.0.1:18080`。不要用地址栏打开它做 cookie 证明 |
| 环境默认 Web | `:8080`。不要动 |
| App | `RepoMesh-Cloud-Verify-1ef0`，ID `4940006`，安装 `161608372` |
| 安装内测试仓 | 稳定 ID `1367444901` |
| 账号 A | login `catbobyman`，展示名 `catmem`，GitHub ID `137759882` |
| 源码头 | 已快进合入 `main`（`e889844`）。发现超时修复、本目录 LIVE 证据，以及忽略密钥/Cookie 的 `.gitignore` 都在其中。独立复核前不要把 B02 标成 `VERIFIED` |
| 状态目录 | `/tmp/repomesh-verify-20260914T115738-48932` |
| 证据禁写 | 私仓名、cookie、OAuth code、PEM、数据库密码 |

## 下一项

本轮账号 A 可做的 LIVE 项已收口（PASS / BLOCKED / DEFERRED_BY_USER）。不要把 B02 标成整批 `VERIFIED`。独立复核仍未做。

## B02.6 本轮 LIVE

| 顺序 | ID | 本轮 | 下一步 |
| --- | --- | --- | --- |
| 1 | LIVE-01 | `BLOCKED` | 匿名 session 401、healthz 200、readyz 503 已看到。证书是自签，Chrome 显示 Not secure。持有人无法为 `127.0.0.1` 提供浏览器信任的公有 CA。 |
| 2 | LIVE-03 | `PASS` | 16:20 UTC login `0c8eb712-b6c7-4716-b6ce-63bcec1b10c8` confirmed。callback 落到 `/auth/result/{id}`。账号 A GitHub ID `137759882`、显示名 catmem 与 session `user.id` `122f497c-ee8a-44f7-ae43-3b1994192481` 一致。Chrome cookie `__Host-repomesh-session` 与 `__Host-repomesh-binding`：Secure、HttpOnly、SameSite=Lax、Path=/。见 evidence/live-03-* 与 snapshots/after-live-06.json。 |
| 3 | LIVE-06 | `PASS` | `GET /api/repositories?limit=50` 200。15 条。fixture `repo_00000000001367444901` `userParticipation=allowed`、`appCapability=allowed`（17:57:45Z）。覆盖 `partial` / `APP_INSTALLATION_SCOPE`。安装内仓 UI 为「App 能力已核实」。见 evidence/live-06-fixture.json、snapshots/after-live-06.json。 |
| 4 | LIVE-04 | `PASS` | 第二次 17:26 UTC。`POST /api/auth/github/reconnect` 201。attempt `e8271f6b-71f0-4653-ad10-f0fb6dfa54fa` purpose=reconnect confirmed，expected GitHub ID `137759882`。epoch 5→6，revision 更换。本浏览器 generation 3 撤销、generation 4 有效。页面「本次连接已确认」。第一次 401 失败保留。 |
| 5 | LIVE-08 | `PASS` | 同源 `limit=5` 游标三页共 15 项、无重复，末页 `coverage=partial` / `APP_INSTALLATION_SCOPE`，fixture 在第 3 页且 `appCapability=allowed`。工作区「部分发现」，默认 50 项时下一页不可用。YAML 未填安装外仓 ID，没有点名缺席样本；未把覆盖写成 complete。USER-READ 仍延期。见 evidence/live-08-pagination.jsonl。 |
| 6 | LIVE-07 | `BLOCKED` | 17:48 UTC 打开专用 App 设置页，GitHub sudo/2FA Confirm access。持有人无法在此完成二次验证。未改权限。不能用安装缺席行代替 `APP_PERMISSION_MISSING`。见 evidence/live-07-blocked.md。 |
| 7 | LIVE-02 | `BLOCKED` | GitHub 对已授权账号自动同意，没有 Cancel 页。不在当前已登录工作区伪造取消。 |
| 8 | LIVE-09 | `PASS` | 01:28 UTC `snapshots/after-live-09.json`：epoch 6→7，revision `c909b3e9…`→`bb7d66a9-152c-42cb-8016-08e112fbe1cf`，connected/idle，`access_expires_at` 前进到 `2026-09-15T09:27:21Z`，`credential_committed_at` `2026-09-15T01:27:21Z`。login/reconnect 计数仍为 5 与 1。未改库到期时间。等待期浏览器会话已在 18:40 POST logout 204 撤销，token 仍自然刷新。见 evidence/live-09.md。 |
| 9 | LIVE-10 | `PASS` | 刷新证明之后新登录 `66522553-…` 仅用于 `重新发现`（发现 epoch 8、15 条、fixture 在）。01:32:58 POST `/api/auth/logout` 204。随后 HTTPS `GET /api/session` 401 `AUTHENTICATION_REQUIRED`。generation 6 撤销。桌面与窄窗口均为「开始使用 RepoMesh」/「使用 GitHub 登录」，列表消失。`/projects` 同壳。见 evidence/live-10-logout.md、snapshots/after-live-10.json。 |
| — | LIVE-05 | `DEFERRED_BY_USER` | 不开账号 B。 |
| — | LIVE-08-USER-READ | `DEFERRED_BY_USER` | 不开账号 B。 |

## 计划口径（旧 origin，不继承）

施工计划与 HANDOFF 仍是 2026-09-13 的旧 App / `https://repomesh.bohanxu.me:8443`。本轮新 App 不能沿用那些 PASS。

| 范围 | 计划状态 | 本轮要不要做 |
| --- | --- | --- |
| B01 | `VERIFIED` | 不做 |
| B02 本地 B02.1–B02.5 | `LOCAL_VERIFIED` | 不做 |
| B02 整批 | `IN_PROGRESS`，外部 `PAUSED_BY_USER` | 本清单收口后仍要独立复核才能改计划 |
| B03 | `INTEGRATED_LOCAL_VERIFIED` | 不做。矩阵已齐，非整批 VERIFIED |
| B04 | `INTEGRATED_LOCAL_VERIFIED` | 不做。授权范围不发送模型请求 |
| 旧 LIVE-05 | 历史 `FAIL` | 延期 |
| 旧环境 6 步恢复 | `RESTORE_IN_PROGRESS` | 仅当 `run_scope` 为 `restore-leftovers` |

旧恢复 6 步仍未证明清理完。本轮默认不管它们。

## 维护规则

1. 做完一项立刻改本页「本轮」列，并在 [live-acceptance.md](live-acceptance.md) 写下观察。
2. 跑 `bash .cursor/skills/verify-repomesh/helpers/live-snapshot.sh docs/development/2026-09-14-b026-cloud-live-01/snapshots/<id>.json`。
3. 向 [decisions.tsv](decisions.tsv) 追加一行。
4. 再开始下一项。
5. 新 origin 或新 App 开新的 `docs/development/<日期>-b026-<主题>/`，不要覆盖本目录。
