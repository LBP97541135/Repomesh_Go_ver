# B02.6 LIVE-04 同账号重连证据复核 01

复核时间：2026-09-12。范围限于 LIVE-04 的前后只读快照、浏览器事件、README 和此前 LIVE-03 的 Cookie 元数据文件。未读取秘密正文，未操作浏览器、服务或数据库。本记录不评价其他 LIVE 场景，也不改变 B02.6 或 B02 的整体状态。

## 结论

LIVE-04 标为 PASS 有证据支持。

重连前，`before-same-account-reconnect.txt` 显示 actor `16198e14-7249-4cc2-a15e-57cd8b568874` 的 revision 为 `4a517ea9-7379-4c6e-a188-a0100beac60f`、access_epoch 为 1，并有一个 generation 1、`revoked=false` 的浏览器会话。

`same-account-reconnect-events.jsonl` 随后记录 `/api/auth/github/reconnect` 返回 201，callback 返回 303 到本地结果页，重连 attempt `3ef28404-a5c1-42b6-9041-6344caee1156` 的 purpose 为 `reconnect` 且 state 为 `confirmed`。重连后两次 session 均为 200，并返回相同本地 user UUID 和 connected 状态。

`after-same-account-reconnect.txt` 显示同一 actor 的 revision 已改为 `a8a723f7-7396-4709-bdf2-cd1cbe28a977`，access_epoch 从 1 变为 2。该快照还显示 generation 1 已 `revoked=true`，generation 2 已建立且 `revoked=false`。这满足同账号、连接 revision 更新和旧浏览器会话撤销的 LIVE-04 判据。

事件明确记录了一个新的 `reconnect` attempt，且前后快照在该 attempt 周围变化。因此 access_epoch 的增加归属为真实同账号重连，不能作为 LIVE-09 自然刷新证据。README 对这一点的说明准确。

## 已补足的 Cookie 记录

`first-login-cookie-metadata.json` 已以不含值的形式保存 Windows Chrome Cookie 存储元数据：`__Host-repomesh-session`、`repomesh.bohanxu.me`、Path=/、Secure、HttpOnly、SameSite=Lax。复核 02 中提出的 P2 Cookie 存储属性复查建议已经补足，且它从一开始就不阻断 LIVE-03 或 LIVE-04。

## 范围外状态

此复核不评价 LIVE-05 至 LIVE-10。它们仍需各自的真实证据，B02.6 与 B02 不能据此标为 VERIFIED。
