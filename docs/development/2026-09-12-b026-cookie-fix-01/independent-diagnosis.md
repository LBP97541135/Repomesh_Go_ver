# B02.6 callback Cookie 丢失竞争诊断

复核时间：2026-09-12。此为只读诊断。依据是 `b026-refresh-01` 的真实失败证据、`internal/access/callback.go`、`internal/access/worker.go`、现有 PostgreSQL 测试和认证开发说明。未读取秘密正文，未操作浏览器、服务或数据库。

## 结论

这是同步 callback 与后台身份恢复工作者的竞争，不是预期的“callback 已失败或进程已中断后，由后台恢复且不补发 Cookie”的路径。

真实 attempt `3069fa41-fdf5-44e1-9096-d0476fa643e7` 已 confirmed，数据库显示新连接、access epoch 3、generation 3 会话已创建，并且 generation 2 已撤销。与此同时，`baseline-reconnect-cookie-failure-events.jsonl` 记录 callback 303 没有 Set-Cookie，随后两次 `/api/session` 为 401，读取该 attempt 也为 401。这个组合说明提交成功但活跃浏览器没有得到新会话 Cookie。它阻断这个真实重连的成功判定。

## 根因

`CompleteCallback` 在交换成功后将 attempt 改为 `identity_pending` 并保存 token 引用，然后直接调用 `confirmIdentity(..., true)`。这条更新没有改变 `next_run_at`，其 schema 默认值为 `now()`。

同时，`RunOne` 会立即领取任何 `identity_pending` 且 `next_run_at <= now()` 的 attempt，并调用 `confirmIdentity(..., false)`。两方都通过最终事务的 state 和 generation 检查，因此只有一方会提交。若 worker 先提交，数据库状态正确，但它传入 `false`，不会把新建会话的 Cookie 放进 callback 响应。随后仍在运行的 callback 看到 attempt 已不再是 `identity_pending`，返回空 Cookie；HTTP 层照常发 303，浏览器随后为 401。

现有 `TestPostgresReadOnlyIdentityRecoveryAndCookieLoss` 有意验证后台恢复不补发 Cookie。认证开发说明也规定确认回执不能补发丢失的 Cookie。该规则适用于 callback 已无法完成、超时或崩溃后的恢复，不能用来接受一个仍活跃的 callback 因 worker 抢先提交而丢失 Cookie。

## 最小修复方向

复用已有的 `attempts.next_run_at`，无需加密 Cookie、Cookie 重签发机制或新数据库 schema。

在 callback 成功把 attempt 发布为 `identity_pending` 的同一条更新中，将 `next_run_at` 设为该活跃 callback 的有效截止时间。服务层必须明确给这个截止时间设上限，即使调用方传入没有 deadline 的 context 也不能留下无限保留。Web 入口已有 15 秒认证请求 deadline，可把实际 deadline 与一个服务常量上限取更早者。worker 在该时间前不得领取该 attempt。

callback 自己继续同步调用 `confirmIdentity(..., true)`。若同步身份核验返回失败而 attempt 仍为相同 generation 的 `identity_pending`，callback 应条件式把 `next_run_at` 释放为当前时间，让 worker 立即接手恢复。当前 `confirmIdentity` 将上游 identity 错误折叠为一个空结果和 nil error；实现时需要增加仅供 callback 使用的完成结果，区分“identity 调用失败，仍可恢复”与“已确认、拒绝、超时或被较新尝试取代”。不能把所有空 Cookie 结果都释放为可恢复工作。若 callback 的 context 到期、进程崩溃或连接断开而无法释放，预留会在明确的截止时间到达后自然过期，worker 仍可恢复。worker 保持 `confirmIdentity(..., false)`，因此真正的异步恢复仍不会获得 Cookie。

释放和保留更新必须以当前 `identity_pending`、binding、attempt ID 与 generation 为条件。这样取消、过期、后到 callback、新 attempt 以及连接 revision 改变都不能被过期 callback 或 worker 覆盖。

## 必须保留的保护

- callback 只能从 pending 领取并交换一次，保留现有 state hash、attempt generation 和 unique exchange 保护。
- 取消、过期和 stale callback 继续只走现有终态或 unknown 路径，不能因为保留时间得到新的 Cookie。
- 同账号重连继续比较 expected connection revision；账号不匹配仍必须写 `ACCOUNT_MISMATCH`，不能创建会话。
- `NEWER_ATTEMPT`、绑定 identity generation 和会话撤销规则保持原样。
- callback 回放和后台恢复仍不能重签发或泄露已丢失 Cookie。

## 建议验证合同

增加可控的身份查询阻塞测试，使 callback 已发布 `identity_pending` 但尚未结束时运行 `RunOne`。worker 必须不能领取，callback 必须成为唯一提交者并返回 Cookie。

再覆盖同步身份查询失败、callback deadline 到期和模拟 callback 中断三种情况。它们应在保留期结束或条件释放后由 worker 确认连接，但不返回 Cookie；现有 read-only recovery and cookie-loss 测试应保留并改为显式覆盖这一语义。

保留并运行取消、超时、重放、并发 callback、同账号重连、错误账号 `ACCOUNT_MISMATCH` 与 `NEWER_ATTEMPT` 用例。真实回归应在修复后的新产物中从正常同账号重连开始，确认 callback 的 Set-Cookie、session 200、confirmed attempt、旧 session revoked 和新 session live 同时成立。
