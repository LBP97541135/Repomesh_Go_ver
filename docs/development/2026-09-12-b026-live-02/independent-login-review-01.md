# B02.6 LIVE-01 至 LIVE-03 证据复核 01

复核时间：2026-09-12。范围限于 `docs/development/2026-09-12-b026-live-02/` 的 README、LIVE-01/LIVE-02 证据，以及 LIVE-03 的过期与首次确认登录证据。未读取秘密正文，未操作浏览器、服务或数据库。此记录不把 B02.6 或 B02 标为 VERIFIED。

## 结论

LIVE-01 和 LIVE-02 的现有证据足以支持 README 中的 PASS。LIVE-03 有一条真实成功回调和已确认会话的证据，但现有记录没有证明该会话属于预期 GitHub 账号。因此 LIVE-03 只能保持 IN_PROGRESS，不能填 PASS。

## 已确认的证据

### LIVE-01

`real-cancel-events.jsonl` 记录固定 HTTPS origin 的 `/healthz` 为 200、`/readyz` 为 503、未登录 `/api/session` 为 401。README 记录 Windows Chrome 在 1037x705 视口正常显示登录入口，且没有绕过 TLS。该结果符合 LIVE-01 的入口、同源响应和未登录条件。

P2：事件文件本身只记录响应，不记录证书信任状态或视口。README 的操作记录补足这两点。若后续需要让单一机器可复查，可保存一张已裁剪的浏览器截图或浏览器安全状态说明，但这不影响本次 LIVE-01 结论。

### LIVE-02

`real-cancel-events.jsonl` 显示浏览器进入真实 `github.com/login/oauth/authorize`，随后 callback 返回 303 到无查询串的本地结果页。该 attempt API 返回 `cancelled/USER_CANCELLED`，两次 `/api/session` 都是 401。`after-real-cancel.txt` 在同一事件后记录该 attempt 为 `cancelled/USER_CANCELLED`，且连接与会话均为零行。README 对应记录为在真实 GitHub 授权页点击 Cancel。

这些记录共同支持 LIVE-02 的取消状态、无新会话和无连接条件。未发现 P0 或 P1 缺口。

## LIVE-03 暂定证据和缺口

首次同意尝试 9042a6c6 在 08:38 到期。`first-consent-expired-events.jsonl` 记录 callback 303 回到结果页，随后 `/api/session` 为 401，attempt 为 `expired/ATTEMPT_EXPIRED`。`first-consent-expired.txt` 同时显示没有连接或会话。这是有效的失败证据，不能计作 LIVE-03 成功。

随后，`first-confirmed-login-events.jsonl` 记录新 attempt 0a385caf 的 callback 在 2026-09-12T15:44:40Z 返回 303，目标是无 code/state 的 `/auth/result/0a385caf-1869-411f-a01c-d14556f086d0`。回调响应的白名单 Cookie 字段为 `__Host-repomesh-session`、Secure、HttpOnly、SameSite=Lax、Path=/。随后两次 `/api/session` 为 200，均返回同一内部 `user.id` 和 `connection.status=connected`；attempt 为 `confirmed`。`first-confirmed-login.txt` 的只读快照也显示该 attempt confirmed、一条 connected/idle 连接及一条未撤销会话。

P1：没有预期 GitHub 账号的可比较身份声明。事件中 `/api/session.user` 只有内部 UUID，没有 `displayName`、GitHub login、GitHub 数字 ID 或其他已脱敏的预期账号标识。数据库 `actor` 也是内部 UUID，不能替代 GitHub 账号身份。手册要求“session 为预期账号”，所以在获得安全的预期身份断言前，不能把这条 confirmed attempt 升为 LIVE-03 PASS。建议由当前浏览器检查以允许字段记录会话的 `user.displayName`，或记录能与预期账号比对的已脱敏身份字段及比较结果；不得记录 token、Cookie 值、callback query 或授权 URL。

P1：LIVE-03 表格与证据不同步。README 仍称 08:44 的新尝试“尚未记录成功”，但两个 `first-confirmed-login*` 文件已记录 08:44:40 的 confirmed attempt、200 session、连接和会话。更新 README 时应描述该成功为“身份待核对的暂定成功”，状态继续为 IN_PROGRESS，避免把已出现的证据写成未发生。

P2：Cookie 字段证据表明 callback 设置了所需属性，后续 session 200 证明浏览器已携带有效会话。现有白名单记录没有单独列出 Domain 属性或浏览器 Cookie 存储快照。对 `__Host-` 名称，若需要逐项审计，可在不保存 Cookie 值的前提下补一条属性存在/不存在的断言；这不是当前 LIVE-03 的主要阻塞项。

## 状态

- LIVE-01：PASS，有限的 P2 复查便利性建议。
- LIVE-02：PASS，无 P0/P1 证据缺口。
- LIVE-03：IN_PROGRESS。08:44 证据支持真实授权后的确认登录和有效会话，但预期账号身份尚未得到可复查证明。
- B02.6：仍未完成。LIVE-04 至 LIVE-10 不在本次复核范围，也没有被推定完成。
