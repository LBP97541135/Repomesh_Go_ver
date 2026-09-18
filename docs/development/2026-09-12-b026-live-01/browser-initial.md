# 初始浏览器检查

专用 WSLg 有头 Chromium 正在 Windows 桌面显示；版本 153.0.8010.12，视口 1050×737。新 profile 为 `/tmp/repomesh-b02-live/profile`，0700；CDP 仅监听 127.0.0.1:9229。

通过 DNS 解析规则仅将 repomesh.bohanxu.me 指向 127.0.0.1，正常验证 HTTPS 证书。未接入既有用户 profile，未禁用证书校验。

主代理实际执行独立控制器 inspect，得到：

- 页面：https://repomesh.bohanxu.me:8443/login。
- 同源文档响应：HTTP 200。
- 公开按钮：“使用 GitHub 登录”。
- 当前该 origin 的 Cookie 数量：0。

结合 startup.md 中实际 healthz 200、readyz 503 和匿名 session 401，LIVE-01 通过。此记录不代表任何 GitHub 授权通过。

控制器位于 `/tmp/repomesh-b02-live/control.mjs`。不保存浏览器 profile、原始网络导出、Cookie 值、OAuth 查询串或输入框内容到证据。
