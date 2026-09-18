# WSL Codex 登录恢复

2026-09-12，用户完成桌面 SSH 接入后报告刷新令牌已撤销。现已完成独立登录及账户接口复验，B02 仍保持交接暂停状态。

## 实测

- Windows 桌面的额度接口当前成功。
- 经 repomesh-wsl 启动的新 app-server，account/read 成功，但 account/rateLimits/read 返回 -32603，错误包含 revoked，与用户截图一致。
- codex login status 仍显示已登录，因此该命令不能证明刷新凭据有效。
- 先前环境配置采用官方 headless fallback，将 Windows 登录缓存复制到 WSL。两端之后发生过刷新，当前凭据不同。缓存复用可能与失效有关，尚未证明服务端撤销的具体触发原因。

## 恢复

用户已完成 WSL 的 codex login --device-auth 授权，命令退出码为 0，返回 Successfully logged in。后续为 WSL 保留独立登录会话，继续使用相同 ChatGPT 账号；不再复制 Windows 的活动登录缓存。

一次性设备码与所有登录令牌不写入仓库。账号确认已由用户本人完成。

经 SSH 新启动 app-server 后，account/read 与此前失败的 account/rateLimits/read 均返回成功，未再出现 revoked 错误。Windows 桌面的额度接口也成功。随后用户重试发现桌面旧进程仍报账号已切换。仅 SSH 重连不会替换常驻进程；限定重启该进程后，原任务的最小模型响应已通过。验证没有调用工具或开始 B02。

此前 verification.json 保留为配置当时的历史结果，不覆盖此次登录故障。当前登录恢复状态为已完成真实账户接口复验。后续已重启桌面实际连接的旧后台进程，原报错任务的模型响应验证通过。见 [桌面最终验收](DESKTOP-AUTH-VERIFIED.md)。机器记录见 [auth-recovery-status.json](auth-recovery-status.json)。

依据：[官方登录与设备授权说明](https://learn.chatgpt.com/docs/auth#preferred-device-code-authentication-beta)。

独立复核：WSL 任务在 2026-09-12T11:13:19.279Z 记录了与截图一致的令牌撤销错误，Windows UI 同时展示该错误。未发现 refresh_token_reused 证据，因此不能把复制缓存导致的并发刷新冲突写成已证实根因。
