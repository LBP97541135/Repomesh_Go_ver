# B02.6 真实验收记录（Cloud VM 2026-09-14）

依据 [真实验收手册](../../current/b02-github-live-acceptance.md) 与 [本轮清单](CHECKLIST.md)。结果只填 PASS、FAIL、BLOCKED、NOT_RUN。清单里的 `OBSERVED_NOT_CLOSED`、`DEFERRED_BY_USER`、`WAITING` 在本表对应 BLOCKED、NOT_RUN 或暂不填 PASS。

## 运行条件

| 项目 | 实际值 |
| --- | --- |
| 开始与结束时间，含时区 | 开始 2026-09-14 11:57 UTC。LIVE-09/10 收口 2026-09-15 01:35 UTC |
| 操作者与独立复核者 | Cloud Agent 驱动本机 Chrome。独立复核未做 |
| 发布版本、release.json 哈希及源码清单 | 开发构建 `version=dev`。LIVE 证据目录随 `main` 上的发现修复 |
| 固定 HTTPS origin、callback | `https://127.0.0.1:18443`；`https://127.0.0.1:18443/api/auth/github/callback` |
| App ID、Client ID、安装权限及范围 | App `4940006`。安装 `161608372` 只选仓库 `1367444901`。Metadata read、Contents write、Pull requests write（LIVE-07 未能降权） |
| 专用数据库代号、schema current/target/pending | 库名 `repomesh_live`。连接串不入库 |
| 测试账号及仓库代号 | 账号 A GitHub ID `137759882`。安装内仓 `1367444901`。私仓名称不入库 |
| 浏览器版本、桌面与移动视口 | VM Chrome。桌面已观察。窄窗口 LIVE-10 注销后 501×844（请求 390×844，Chrome 最小宽度）再恢复 1820×1100 |
| 两个进程 UID、PID、启动时间与停止安排 | Web 与 coordinator 的 PID 以状态目录为准。环境 `:8080` 不纳入 |
| 用户令牌过期已启用、初始 expires_at、refresh_due_at | 基线 `access_expires_at` `2026-09-15T01:26:40Z`；`refresh_due_at` `2026-09-15T01:26:10Z`。刷新后 `2026-09-15T09:27:21Z`。未改库到期时间 |
| 替身、时间修改和请求重放 | 未使用 |

## 必需场景

| ID | 实际操作和成功判据 | 初始状态 | 实际观察、时间与证据 |
| --- | --- | --- | --- |
| LIVE-01 | 浏览器信任固定 HTTPS 证书，同源登录入口可用，未登录 session 401；healthz 200、readyz 503 | BLOCKED | 2026-09-14。匿名 `GET /api/session` 401。healthz 200。readyz 503。Chrome 地址栏 Not secure。持有人无法提供公有 CA。[CHECKLIST.md](CHECKLIST.md) |
| LIVE-02 | 在真实 GitHub 授权页取消，原 attempt cancelled/USER_CANCELLED，无新会话 | BLOCKED | GitHub 对已授权账号自动同意，没有 Cancel 页。不在当前已登录工作区伪造取消 |
| LIVE-03 | 真实授权返回固定 callback，303 到结果页，原 attempt confirmed，session 为预期账号；Cookie 标志正确，URL 无 code/state 残留 | PASS | 16:20 UTC login `0c8eb712-b6c7-4716-b6ce-63bcec1b10c8` confirmed。代理 callback → `/auth/result/{id}`。账号 A `137759882` / catmem 与 session user.id `122f497c-ee8a-44f7-ae43-3b1994192481` 一致。Cookie `__Host-` Secure HttpOnly Lax Path=/。见 evidence/live-03-login-identity.json、live-03-cookie-flags.json、live-03-proxy.txt |
| LIVE-04 | 同账号重连原尝试 confirmed，user.id 不变，连接 revision 更新，旧本浏览器会话撤销 | PASS | 17:26 UTC attempt e8271f6b-71f0-4653-ad10-f0fb6dfa54fa reconnect confirmed，GitHub ID 137759882。epoch 5→6。generation 3 撤销、4 有效。代理 POST reconnect 201、callback 303。页面「本次连接已确认」。见 snapshots/after-live-04-attempt2.json、evidence/live-04-attempt2-proxy.txt、evidence/live-04-reconnect-confirmed.webp。第一次 401 失败见 attempt1 证据，不改写 |
| LIVE-05 | 错误账号重连 ACCOUNT_MISMATCH，原账号和连接不被替换，跨账号仓库结果不泄露 | NOT_RUN | DEFERRED_BY_USER。不开账号 B |
| LIVE-06 | 已安装专用私仓且用户有读权，userParticipation.status allowed；安装未暂停且三项权限完整，appCapability.status allowed，观察时间可核对 | PASS | 17:57 UTC `GET /api/repositories?limit=50` 200。fixture `repo_00000000001367444901` 两项均为 allowed。15 条中仅 1 条 app allowed。UI「App 能力已核实」。见 evidence/live-06-fixture.json、snapshots/after-live-06.json |
| LIVE-07 | 专用 App 权限或安装不足的可读仓库样本，实际 denied 与 reasonCode 对应；恢复配置后新观察 allowed。不能用私仓缺席代替 denied | BLOCKED | 17:48 UTC GitHub App 设置页要求 sudo/2FA。未改权限。见 evidence/live-07-blocked.md |
| LIVE-08 | 私仓发现、分页和刷新真实往返，coverage.status partial；安装外私仓缺席不误报完整覆盖，用户失去读权且原观察超过 60 秒后重查，不继续披露旧名称 | PASS | 17:57 UTC 同源 limit=5 三页 15 项无重复，末页 partial / APP_INSTALLATION_SCOPE。YAML 未填安装外仓 ID，没有点名缺席样本。USER-READ 见延期行。见 evidence/live-08-pagination.jsonl |
| LIVE-09 | 保持原连接直到自然刷新窗口；前后只读快照证明 epoch 增一、revision 与期限前进、connected/idle，期间无登录或重连 | PASS | 01:28 UTC `after-live-09.json` epoch 6→7，revision 更换，connected/idle，期限前进，login/reconnect 计数未增。等待期会话已在 18:40 logout 204。见 evidence/live-09.md、snapshots/pre-live-09.json、after-live-09.json |
| LIVE-10 | 刷新后同一有效会话的新发现成功，注销后 session 401、清除仓库显示；桌面和移动浏览器真实往返已观察 | PASS | 等待会话未能活过刷新窗口。刷新快照之后新登录仅用于 `重新发现`（epoch 8、15 条、fixture）。01:32:58 logout 204，session 401，桌面与窄窗口登录壳，列表消失。见 evidence/live-10-logout.md、snapshots/after-live-10.json |

## 结果与限制

- B02.6 本轮账号 A 可做项已按清单收口。B02 整体仍为 IN_PROGRESS，独立复核未完成。
- LIVE-01、LIVE-02、LIVE-07 因持有人条件 BLOCKED，不记 PASS。
- 记录每次失败，修复后在新证据中注明替代关系，不改写旧失败。
- 原始 callback URL、token、Cookie、csrfToken、authorizationUrl、请求正文和数据库秘密不入此记录。
- 独立复核未完成前不得把 B02 标成 VERIFIED。
