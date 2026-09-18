# 桌面 WSL 登录故障最终验收

2026-09-12，已在用户原来报错的桌面任务完成真实模型响应，结果为 WSL_AUTH_OK。

## 原因与修复

此前验证的是新启动的独立 app-server。桌面实际通过 proxy 连接登录前就已启动的常驻进程 PID 552，SSH 重连没有更换该进程。独立设备登录更新磁盘凭据后，旧进程仍报 since logged out or signed in to another account。原任务中的最小模型请求在重启前实际失败，证明仅验证新进程不足以覆盖桌面任务。

仅对用户 xubohan 在 Ubuntu-22.04 中的该后台进程发送 SIGTERM，确认退出后用相同二进制、相同参数启动，保留 features.code_mode_host=true。新 PID 为 4136。未终止 WSL、未影响其他远端主机、未退出 Windows 账号、未再次复制凭据。

内置 daemon restart 拒绝管理这个由桌面直接启动的 unmanaged 服务，因此使用上述限定进程的正常停止和原命令启动。没有为解决此问题安装新的 daemon 或修改主机启动方式。

## 同一任务的前后对照

- 主机：remote-ssh-discovered:repomesh-wsl。
- 原任务：01a0955a-370c-7a03-9f99-2dbf0117d1c1。
- 修复前最小请求：turn 01a0955c-cb2b-7071-a384-7390e350da4b，失败并返回账号已退出或切换的错误。
- 修复后最小请求：turn 01a0955e-310b-73a3-87b5-6b99ce4c5004，状态 completed，11:26:18Z 返回 WSL_AUTH_OK。
- 已直接核对远端任务 rollout 中的 assistant 响应。没有借助新建任务规避原任务，也未启动 B02 开发。

[进程重启记录](desktop-daemon-restart.json)与[桌面响应结果](desktop-auth-verification.json)保存了本轮验收。此前账户 API 验收只证明新进程认证可用，本记录补齐桌面真实请求。

用户现在可以在该任务中发送“继续按最初的接手要求执行”。无需再登录，也无需再次创建任务。
