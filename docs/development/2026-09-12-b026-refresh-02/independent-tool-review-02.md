# B026 后刷新历史准备独立工具复核 02

复核时间：2026-09-12。只读静态审查；未运行 `post-refresh.mjs`，未操作浏览器、服务或 live 数据库。

## 结论

refresh-02 对 `post-refresh.mjs` 的补充范围恰当。它在注销前建立可验证的 `/login` → `/` 真实浏览器历史，避免旧脚本从 reconnect 结果页开始、受 401 导航影响而将“没有可前进历史”误判为刷新后失败。补充不发起授权；若历史准备期间出现到本系统 login 或 reconnect 接口的 POST，脚本会以明确错误失败。

这只是未来刷新确认后的浏览器检查工具修复，不构成已执行的浏览器证据或 LIVE09 PASS。

## 差异与安全边界

- 新增 `prepareHistory(page)`：监听同源 `POST /api/auth/github/login` 与 `POST /api/auth/github/reconnect`；任一出现即记录事件并以 `AUTHORIZATION_POST_DURING_HISTORY_SETUP` 失败。
- 它点击工作区的“重新连接 GitHub”，等待站内 `/login`；随后点击“稍后处理，返回工作区”，等待站内 `/` 和已登录状态。完成后才执行原有注销、匿名 session 与历史后退/前进检查。
- 成功结果新增 `historyPreparedWithoutAuthorizationPost: true`，使该前提成为显式通过条件。

前端源码与此假设一致：`RepositoryHome.tsx` 的“重新连接 GitHub”仅调用 `navigate('/login')`；`AuthEntry.tsx` 的“稍后处理，返回工作区”仅通过 `auth.refresh()` 读取当前 session 后 `window.location.assign('/')`。实际创建 login/reconnect 授权尝试的 POST 仅绑定在 AuthEntry 的“使用 GitHub 登录”或“重新连接同一账号”按钮，历史准备没有点击它们。

新请求监听是防御性检查。若将来页面回归而上述导航意外改成授权 POST，脚本会失败而不会产生虚假的 PASS；在当前源码中静态路径不触发该副作用。原有 refresh 快照校验、固定 actor/fixture、无新 attempt 校验、注销 204、401 与历史匿名检查均未放宽。
