# r3 真实 callback Cookie 回归独立复核 01

复核时间：2026-09-12。范围限于 r3 切换记录、r3 前后只读数据库快照和真实 login/reconnect 浏览器事件。未读取秘密正文，未操作数据库、浏览器或服务。

## 结论

r3 已在真实 GitHub 登录和同账号重连中关闭此前的 callback Cookie 丢失症状。

`switch.json` 记录 Web 与 coordinator 已切换到 `0.2.0-b026-cookie-20260912-r3`，迁移仍为 current=3、target=3、pending=0，切换未触碰 PostgreSQL 或浏览器。r3 前快照的同一 actor 为 epoch 3，generation 3 是唯一未撤销会话。

真实 login attempt `3fe25d1c-de6f-4996-98e5-cd3304038582` 的 callback 返回 303 到无查询参数的本地结果页，并设置 `__Host-repomesh-session`，其白名单属性为 Secure、HttpOnly、SameSite=Lax 和 Path=/。后续两次 `/api/session` 为 200，attempt 为 confirmed。`after-login.txt` 显示同一 actor 的 epoch 变为 4，generation 3 已撤销，generation 4 未撤销。

随后真实 reconnect attempt `cbfce23e-631b-4d97-a66a-97b25e0d60bb` 同样返回 callback 303 与 Set-Cookie，后续两次 `/api/session` 为 200，attempt 的 purpose 为 reconnect 且 state 为 confirmed。`after-reconnect.txt` 显示同一 actor 的 epoch 变为 5，generation 4 已撤销，generation 5 未撤销，连接为 connected/idle。该重连同时确认旧浏览器会话撤销与新会话有效。

这些真实证据与 r2 的故障形成直接对照：r2 attempt 3069fa41 数据库已 confirmed 并创建 generation 3，但 callback 没有 Set-Cookie，浏览器 session 为 401。r3 的 login 与 reconnect 都同时得到 confirmed attempt、会话 Cookie 和 session 200，符合修复针对的竞争闭环。

本复核只关闭 Cookie 竞争的真实登录与重连回归缺口。自然刷新、第二账号相关 blocked 场景、移动视口、注销和 B02.6 整体状态仍需各自证据，不能据此标为 VERIFIED。
