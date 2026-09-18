# Windows 可见登录窗口恢复

用户报告看不到此前 WSLg 页面。原 CDP 能返回 GitHub 登录路径，但这不能证明页面在用户桌面可见，因此改为 Windows 原生 Chrome 的独立验收 profile。

- Windows Chrome 版本：153.0.8010.36。
- 主进程：PID 56812，Windows 窗口句柄 5639914。系统检查窗口存在、可见且未最小化；自动取得前台焦点未成功，不能据此声称用户已看到。
- 专用 profile：`C:\Users\18092\AppData\Local\RepoMesh\b026-browser`。未复制用户原 Chrome profile 或 Cookie。
- Windows CDP：`http://127.0.0.1:9230`，与原 WSLg 的 9229 区分。Windows Node 位于 `C:\Program Files\nodejs\node.exe`，可导入 `file://wsl.localhost/Ubuntu-22.04/tmp/repomesh-b02-browser/node_modules/playwright/index.mjs` 后连接。
- 新窗口随后显示 Google 账号页面。保留该页，另开 GitHub 标签，并确认其标题为 `Sign in to GitHub · GitHub`，脱敏路径为 `https://github.com/login`。
- GitHub 标签的固定 targetId 为 `48CE03E9D300725922ABC78DF4BF463C`。后续控制必须按该 targetId 选择，不能假设 page 0，也不能操作 Google 登录页。

此次只打开页面，没有读取账号表单、密码或 Cookie 值，没有发起 RepoMesh OAuth。原 WSLg profile、浏览器和白名单日志保留；旧 WSLg 监控不覆盖 Windows 新窗口。正式继续取消与授权前，应为 Windows target 接通白名单监控，另存新日志。

## 08:19 PDT 接续检查

用户报告已登录后，原 Windows PID 56812 和 9230 监听均不存在。退出原因未确定。使用原专用 profile 重新启动 Windows Chrome，未复制其他 profile 或 Cookie。新主进程为 54296，GitHub 标签 targetId 为 `D8FAE2B91987B49165CC42BE596F4000`，后续应使用此 target 替代上文旧值。

实际浏览器访问 GitHub 首页后仍显示一个准确匹配的 `Sign in` 链接，随后已打开 `/login`。因此只能确认该专用窗口仍需登录，不能推断用户在其他窗口的登录状态。本次未发起 RepoMesh OAuth。Linux 对固定 HTTPS origin 的探测返回 200，证书验证为 0；这不替代 Windows 浏览器场景。Windows 白名单监控仍待接通。

## 08:21 PDT 登录及代理检查

用户再次完成登录后，同一专用 target 位于 GitHub 首页，准确匹配的 `Sign in` 链接为 0。此时数据库只读快照见 `before-windows-oauth.txt`，仍为零连接和零会话，旧尝试已经 superseded 或 expired。

Windows 浏览器访问 RepoMesh 时实际显示 `ERR_CONNECTION_CLOSED`。同机 Windows curl 返回 HTTPS 200、证书校验 0。Windows 系统代理开启，现有绕过列表未包含该域名。只为专用 Chrome 增加 `--proxy-bypass-list=repomesh.bohanxu.me`，未修改系统代理。原窗口通过 CloseMainWindow 正常关闭，使用原 profile 重开，保留登录配置。CDP Browser.close 请求未确认，不能将其记为成功关闭。

新主 PID 为 7948，固定 targetId 为 `6532F79B259735F8E05EE0187C932F46`，替代上面的旧值。白名单监控 01 曾启动并记录 ready，但未捕获真实 OAuth；原文件保留。监控 02 使用该固定 target，Windows Node PID 为 53236，准备输出 `/tmp/repomesh-b02-live/windows-monitor-02.jsonl`，是否就绪以实际 ready 事件为准。

## 08:26 PDT 实际入口与取消通过

单独指定绕过列表后仍显示 ERR_CONNECTION_CLOSED。[Chromium 文档](https://new.chromium.org/developers/design-documents/network-settings/)明确该参数须与 `--proxy-server` 同用。读取系统代理的受控本机地址后，只为此专用 Chrome 同时指定 `--proxy-server=http://127.0.0.1:10808` 和 `--proxy-bypass-list=repomesh.bohanxu.me`。系统代理及其绕过列表未改。通过 CloseMainWindow 关闭上一个专用窗口，再使用相同 profile 启动。

当前 Windows Chrome 主 PID 为 29320，targetId 为 `CBEA86CD7C41D085994D442CEB5B60A4`，CDP 仍为 127.0.0.1:9230。Windows 白名单监控 03 的 Node PID 为 46132，08:26:01 PDT 已记录 ready；输出 `/tmp/repomesh-b02-live/windows-monitor-03.jsonl` 已校正为 0600，父目录 0700。之前监控 01/02 及其日志保留；不得用旧 target 控制当前窗口。

实际页面标题为 RepoMesh · 工程骨架，视口 1037×705，显示“使用 GitHub 登录”。同源请求 session 401、healthz 200、readyz 503。未设置忽略 TLS 选项。点击登录后进入真正 GitHub 授权页，证明原 profile 的账号登录可继续使用。

08:26:23 PDT 点击 GitHub 的 Cancel 后，callback 303 到原尝试结果页。页面显示“你已取消 GitHub 授权”，URL 无 query/hash。API 为 cancelled/USER_CANCELLED 且 observedAt 以 Z 表示；只读数据库无连接、无会话。现场事件快照见 `real-cancel-events.jsonl`，数据库前后见 `before-windows-oauth.txt` 与 `after-real-cancel.txt`。随后从页面返回登录入口并发起下一次正式授权，账号持有人同意仍待执行。

返回登录入口后，页面实际保留原尝试并显示“查询原授权尝试”“使用原尝试重新发起授权”“明确开始一次新的登录”。最初控制脚本等待普通登录按钮超时，未创建第二次尝试；检查实际按钮后，于 08:28:09 PDT 点击“明确开始一次新的登录”，得到新 attempt `9042a6c6-633e-4621-8f41-bb270b72982e`。GitHub 真实页面已显示 Authorize 与 Cancel，尚未代理点击同意。当前窗口句柄 7079336，临时置顶后已恢复普通层级；等待账号持有人操作。原取消失败恢复观察与脚本超时均保留，不当作授权成功。
