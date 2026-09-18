# 自然刷新观察器与会话保持复核 02

复核时间：2026-09-12。范围限于更新后的 `observer.py`、`snapshot.sql`、`session-keepalive-02.mjs` 和启动安排。脚本尚未运行；本记录不是自然刷新或浏览器验收通过证据。未操作真实数据库、浏览器或服务。

## 已补足的观察边界

`snapshot.sql` 现在把当前 actor 所有会话 binding 的 attempts 也纳入快照。观察器的 `NEW_ATTEMPT_DETECTED` 比较因此能发现同一 binding 上新增的匿名 login pending attempt，而不只看到已写入 actor 或 original_actor 的 attempt。该查询仍在 read-only transaction 内，且不输出 token、Cookie 值或其他秘密。

`prepare` 现在拒绝 `refresh_due_at` 已到或已过的连接，避免把在刷新窗口内建立的连接作为自然刷新基线。它仍要求 confirmed reconnect、当前 revision、connected/idle、有效 refresh token、未来期限和唯一活跃会话。这个封基线条件符合自然刷新归因要求。

会话保持脚本只在 baseline.json 已存在、其中有唯一未撤销且未过期的预期用户会话时启动；它从该会话真实 `expires_at` 得到停止时间，使用固定 target 的同源 `GET /api/session`，每 10 分钟一次，不记录 Cookie 值。主代理随后用只读快照确认 `last_active_at` 前进，这能验证保持请求实际生效。

## 启动前需修正的 P1

`session-keepalive-02.mjs` 当前会把 `ORIGIN_SKIPPED`、`NETWORK_FAILURE`、500 或 503 等非 200 结果写入日志后继续循环。它只在 401 或错误 user ID 时退出。这样脚本可能没有维持会话，却持续运行到绝对 session deadline；观察器最终会以原会话无效失败，而不会产生错误 PASS，但启动监控不会尽早报告保持失败。

在启动自然观察前，脚本应把首个和每个后续请求都要求为 HTTP 200，且 user ID 必须等于 baseline 的预期用户。错误 origin、网络失败、非 200、缺少 user 或 user 不匹配都应记录安全错误码并退出非零。`githubConnection.status` 可继续只作记录，因为连接观察超过 60 秒时 API 允许显示 unknown。

完成这项修正后，依次建立新 baseline、启动 keeper、确认一次 `last_active_at` 前进，再启动 observer。真实刷新、刷新后私有样本、移动视口、注销和历史往返仍须等待实际输出，不能由本次脚本审阅判为通过。
