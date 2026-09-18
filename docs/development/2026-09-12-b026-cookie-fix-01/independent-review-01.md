# Cookie callback 竞争修复独立复核 01

复核时间：2026-09-12。范围是 `internal/access/callback.go`、`internal/access/callback_concurrency_test.go` 和本目录 pre-fix/post-fix 记录。复核未读取秘密正文，未操作真实验收数据库、浏览器或服务。测试只使用 `REPOMESH_TEST_DATABASE_URL` 所在 PostgreSQL maintenance database 创建并清理随机命名的 `repomesh_b02_it_*` 数据库。

## 结论

修复关闭了已记录的 callback Cookie 丢失竞争。`CompleteCallback` 现在建立最多 15 秒的服务 deadline，并在发布 `identity_pending` 的同一 SQL 更新中把 `next_run_at` 设为该 deadline。worker 的领取条件仍要求 `next_run_at <= now()`，因此它不能在活跃 callback 的同步身份核验期间抢先确认。

同步确认没有返回 Cookie 时，代码只在同一 attempt、同一 identity generation、同一 attempt generation 仍为 `identity_pending` 时将 `next_run_at` 释放为现在。callback deadline 到达或请求中断使该更新无法执行时，原保留截止时间已经到达，worker 可恢复。worker 继续用 `confirmIdentity(..., false)`，所以恢复不会重签发 Cookie。

这与真实故障闭环一致。`b026-refresh-01/baseline-reconnect-cookie-failure.txt` 与对应 events 记录 reconnect `3069fa41-fdf5-44e1-9096-d0476fa643e7` 已 confirmed、连接 epoch 已到 3、旧 generation 已撤销而新 generation 已建立，但 callback 没有 Set-Cookie，浏览器 session 为 401。`pre-fix.txt` 只保存本地 fake-provider 并发回归的失败输出，重现 worker 先确认和 callback Cookie 为空；它不含真实 attempt 证据。`post-fix.txt` 显示同一回归在修复后通过。两份 fix 记录都不是新的 GitHub 外部验收。

## 并发和恢复验证

`TestPostgresCallbackOwnsIdentityConfirmationWhileLive` 让第一次 identity 查询停在 callback 内，然后调用 worker。它分别覆盖 login 和 reconnect，并断言 worker 没有工作、attempt 保持 `identity_pending`、会话数和撤销数没有变化、callback 解除阻塞后是唯一返回 Cookie 的确认者。

`TestPostgresCallbackDeadlineReleasesIdentityRecovery` 使用 500ms callback context，使第一次 identity 查询等待到 deadline。随后 worker 成功确认 attempt；callback 没有 Cookie。这保留了崩溃、超时或断连后的恢复语义。

现有 `TestPostgresReadOnlyIdentityRecoveryAndCookieLoss` 继续验证后台恢复不重复 OAuth code exchange，也不通过 callback 重签发丢失 Cookie。

我运行了以下检查，均通过：

```bash
REPOMESH_TEST_DATABASE_URL='host=/home/xubohan/.local/state/repomesh-b026 port=55432 user=xubohan dbname=postgres sslmode=disable' \
  go test ./internal/access -run '^(TestPostgresCallbackOwnsIdentityConfirmationWhileLive|TestPostgresCallbackDeadlineReleasesIdentityRecovery|TestPostgresReadOnlyIdentityRecoveryAndCookieLoss|TestPostgresConcurrentStartAndCallback|TestPostgresReconnectMismatchAndLateLogout|TestPostgresCallbackCannotWinAfterLogout|TestPostgresDelayedAnonymousLoginCannotReplaceReconnectedActor|TestPostgresReconnectCannotReplaceNewerOtherBrowserConnection)$' -count=1 -v

REPOMESH_TEST_DATABASE_URL='host=/home/xubohan/.local/state/repomesh-b026 port=55432 user=xubohan dbname=postgres sslmode=disable' \
  go test -race ./internal/access -run '^(TestPostgresCallbackOwnsIdentityConfirmationWhileLive|TestPostgresCallbackDeadlineReleasesIdentityRecovery|TestPostgresReadOnlyIdentityRecoveryAndCookieLoss)$' -count=1 -v
```

两条命令都退出 0。第一条用了约 3.1 秒，第二条用了约 2.5 秒。

## 保护条件

修复没有改变 pending 到 exchanging 的唯一交换领取、state hash 校验或 attempt generation 检查。条件释放也受 attempt state 与两代 generation 限制。现有测试继续覆盖 callback 重放、取消后的终态、错误账号重连、登出后的迟到 callback、跨浏览器较新连接 revision 和迟到匿名登录不得覆盖新连接。

## 源码复核与外部验收边界

未发现本次修复的可复现源码缺陷。空 Cookie 时的 release update 同时受 attempt state、identity generation 和 attempt generation 约束。拒绝、过期、stale 或被较新 attempt 替代时，该条件不会重新安排旧 attempt；当前没有显示它会错误改变授权、会话或连接状态。以后若要区分 identity 上游失败、账户禁用和 stale no-op，可增加内部完成原因以改善诊断，但这只是非缺陷的维护建议。

修复后仍需要用新产物执行一次真实 GitHub 同账号重连，确认 callback 的 Set-Cookie、session 200、confirmed attempt、新 session live 和旧 session revoked。该事项是部署后的真实验收门槛，不能作为阻止创建、启动或测试本次修复产物的源码问题。
