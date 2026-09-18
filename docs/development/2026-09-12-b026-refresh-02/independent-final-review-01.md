# B02.6 自然刷新及后刷新浏览器独立复核 01

复核时间：2026-09-13。复核者未执行真实登录、重连、浏览器、服务或数据库操作；只读取本目录已生成证据、既有私仓样本记录和 keeper 白名单日志，并检查 observer Linux PID 是否已退出。

## 判定

**PASS（本目录的 LIVE-09 与 LIVE-10）**。没有发现开放 P0、P1 或 P2。该结论允许进入第二账号验收的 `GATE`，但不把第二账号的 LIVE-05、LIVE-08-USER-READ、RESTORE 或整批 B02/B02.6 标为通过；它们仍是待执行的真实验收。

## LIVE-09：自然刷新

- `baseline.json` 与 refresh-01 的基线逐字相同（SHA-256 `3163816971d13ac908cf44c148dc0e303951b95fc1304b89a2a5cdaa38c989ea`）。启动前连续性记录确认未登录、未重连、无新增 attempt，且原有效 session generation 仍为 5。
- `observations.jsonl` 有 520 条只读样本。到期前最后一条在 `2026-09-13T00:58:45.025160Z`，比 due 早 966 ms，仍为基线 epoch/revision；到期后第一条在 `00:58:55.060813Z`。全部样本保持同一 actor、同一未撤销 generation 5 session 和相同的七项 attempt 集合；epoch 序列仅为 5、6，没有提前变化或第二次推进。
- due 为 `00:58:45.991534Z`。`confirmed-refresh.json`/`outcome.json` 的新 credential 提交为 `00:58:47.880334Z`，即 due 后 1.889 秒；确认样本距提交 7.180 秒。epoch 恰从 5 增至 6，revision 改变，credential 提交时间、access 到期和 refresh 到期均前进，refresh token 仍可用，状态回到 `connected/idle`。
- `outcome.json` 为 `REFRESH_CONFIRMED`，并与 `confirmed-refresh.json` 的最终连接快照一致。该因果链没有以新 attempt、登录、重连、时间修改或 token 重放替代自然刷新。

观察器在到期前两分钟采用十秒采样；本次实际在正常启动窗口内完成，未触发此前记录的“连接观察超过 60 秒会导致浏览器工具假阴性”风险。该风险保留为工具设计边界，而非本次 P1 缺陷或通过依据。

## LIVE-10：刷新后原会话浏览器验收

- `browser-result.json` 为 `PASS`，`browserReturnCode` 为 0。其刷新前提再次核对 epoch +1、revision/提交时间/两个期限前进、提交晚于 due、相同 generation 5 和无新增 attempt，且为 settled。
- 23 条按时间排序的 `browser-events.jsonl` 先完成一次真实新发现的桌面检查及一次移动检查；每次保留首个 `503/RESULT_UNCONFIRMED` 后的实际 `200` 重读，而非把暂未确认误作成功。固定样本是已在 live-02 记录为真实私仓的同一稳定样本；刷新后的用户参与和 App 能力观察均为 allowed，且观察时间晚于新 credential 提交。
- 移动视口为 390x844，文档与 body 宽度均为 375，没有水平溢出。历史准备经 `/login` 再回工作区，结果明确确认期间没有授权 POST。
- 实际注销返回 204。注销后、history back 和 history forward 三个匿名阶段均记录 session 401 与样本不再可见；因此覆盖当前会话失效、敏感仓库显示清理和历史往返，而非仅检查一次导航。

## 证据裁剪与进程收束

- 对 `baseline.json`、全部 observations、`confirmed-refresh.json`、`outcome.json`、`browser-result.json` 与 browser events 的结构和值模式复查，没有 Cookie、CSRF、authorization URL、token、OAuth code/state、密码、PEM 或原始 HAR 值。连接记录仅保留布尔 `has_refresh_token` 与状态字段；browser events 只含时间、事件、受控路径、HTTP 状态及断言结果。
- 当前复核时 Linux observer PID 293161 已不存在。`processes.json` 的 `STARTED_NOT_COMPLETED` 是启动时静态记录，不能代表最终状态；实际 `outcome.json` 已落盘且浏览器返回 0。
- keeper 的白名单日志最后两项为 `00:50:44.486Z` 的正常 200 和注销后的 `01:00:44.552Z` 的 401；操作记录确认 Windows keeper PID 52940 在 `01:01Z` 已不存在。未复制 keeper 的非白名单字段。该 401 与浏览器实际注销后的预期收束相符，不是会话保持失败。

复核的核心输入哈希：`observations.jsonl` `ca982e8a3e7b46c0d588bbf203c2a73216a634b48729aabcba8e08a2f67d9055`，`confirmed-refresh.json` `edacb0202e34561c1c88703f16e19f73416ef06eb68c6bfedf1fb34810f0cb2c`，`outcome.json` `08968da37a8be70c594c0e445d67441f8f27719f8e4d92eff318597d0e8f69a7`，`browser-events.jsonl` `0112b3a76483cb648f248af34e49b1da8b402180476b547b6605db6b0b4b6368`，`browser-result.json` `7cf9c8c2dd947cc5de33fb2d1af128754e3af20c181376821cb68b9a0270a9b5`。
