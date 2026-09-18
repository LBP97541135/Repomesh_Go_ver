# B026 刷新观察启动独立复核 01

复核时间：2026-09-12（只读；未重跑实验，未操作浏览器、服务或 live 数据库）。

## 结论

本轮自然刷新尚未通过，且当前证据不支持“观察器正在持续等待”这一启动状态。Windows keeper 的启动和首个保活请求有足够证据；Linux observer 则在首个正常样本后已写出 `FAILED / INVALID_TIMESTAMP`。因此，刷新窗口 `2026-09-13T00:58:45.991534Z` 仍须等待，但必须在可持续采样的观察器重新建立后，才有机会产出本轮 LIVE09 的有效证据。

这是 P1 级别的本轮验收证据缺口：它阻断 LIVE09 和依赖它的 B02.6 完成判定；它不是已证明的产品认证源码缺陷，也不否定 r3 的真实登录、重连和 Cookie 回归结果。

## 已核对的启动前提

- `baseline.json` 记录了 r3 的真实重连 `cbfce23e-631b-4d97-a66a-97b25e0d60bb`：同一 actor、连接 epoch 5、revision `fb6d2db3-5069-483f-9c79-5efb16b58921`、generation 5 未撤销、连接 `connected/idle` 且持有 refresh token。其 `refresh_due_at` 为 `2026-09-13T00:58:45.991534Z`，晚于 17:00Z 的基线采样；基线本身合格，且 epoch 5 是显式 reconnect，不能算自然刷新。
- `keeper-startup-proof.json` 显示 keeper Windows PID 52940 将 generation 5 的 `last_active_at` 从 `17:00:20.609977Z` 推进至 `17:00:40.749846Z`，同时无新 attempt、连接未变。此证据满足 keeper 的首个固定目标 `/api/session` 保活前提。
- `processes.json` 登记 observer Linux PID 292515、keeper Windows PID 52940，状态为 `STARTED_NOT_COMPLETED`。进程登记本身不足以替代观察结果，须与结果文件一致解释。
- `observations.jsonl` 只有一条白名单样本（`2026-09-12T17:01:09.449187Z`）。它仍是 epoch 5 / revision `fb6d…` / `connected/idle`，generation 5 未撤销，且没有相对基线新增 attempt；这只说明首个采样没有看到异常或刷新。

## 当前观察结果

`outcome.json` 已存在，状态为 `FAILED`，错误码为 `INVALID_TIMESTAMP`。它的 `after` 与唯一观察样本相同，表明失败发生在后续采样取得可记录的变化之前。`confirmed-refresh.json`、`browser-result.json` 和浏览器事件文件均不存在。

根据 `observer.py` 的结果约定，只有 observer 分类为自然刷新成功才会写 `confirmed-refresh.json` 并运行 `post-refresh.mjs`；因此现有资料既不构成自然刷新 PASS，也不构成刷新后浏览器行为 PASS。README 中“observer active / wait”应在恢复实际连续采样后再作为当前状态陈述；不能以 PID 或首条样本覆盖既有 `FAILED` outcome。

`INVALID_TIMESTAMP` 的具体来源尚未由现有白名单输出定位；本复核不修改或重跑脚本。恢复工作应保留这次失败证据，并在新的、隔离的观察轮次中先证明连续采样，再等待同一轮的自然刷新窗口。

## 范围与阶段状态交叉核对

- `b026-live-03/README.md`、cookie-fix README 与 `release-r3-verification.json` 相互吻合：r3 版本和 SHA 为 `0.2.0-b026-cookie-20260912-r3` / `54363f7…`; 真实登录与重连均有 Cookie、session 200 和旧 generation 撤销的独立复核。这些事实保持 PASS。
- `docs/current/HANDOFF.md` 的最新段和两个 README 都将自然刷新列为未完成，将第二账号相关的 LIVE05（另一账号不可读）和 LIVE08（实际用户读权撤销）列为 BLOCKED。它们仍然成立；本轮不把安装权限恢复或 API 分页结论扩大为这两项通过。
- B02 仍为 `IN_PROGRESS`，B03 仍为 `TODO`，B02.6 不能标记为通过。即使后续重新建立观察并取得 LIVE09，自然刷新之外的两项 second-account 阻塞也仍需保留。

## 后续验收门槛

在不干扰此基线的前提下，下一可验收观察轮应同时保存：有效基线、持续的窗口前样本、到期后 epoch 与 revision 的单次自然推进、无人工 attempt 的比较、以及 observer 成功后触发的浏览器结果。仅有 keeper 心跳、进程 PID、基线或到期等待均不满足该门槛。
