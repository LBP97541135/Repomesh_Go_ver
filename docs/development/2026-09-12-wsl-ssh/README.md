# RepoMesh 专用 WSL SSH 环境

2026-09-12 开发接手补充。本次任务已实际运行于 WSL2 Linux 的 `/home/xubohan/projects/Repomesh_Go_ver`，桌面项目列表再次确认 `repomesh-wsl` 绑定及同一路径。用户已恢复开发；B02 前置核对完成，认证采用范围待确认，用户确认 GitHub App 尚未配置，业务实现尚未开始。最新状态和证据见 [B02 接手记录](../2026-09-12-b02-preflight/README.md)。下文的暂停和“本轮没有恢复 B02”描述保留 SSH 准备阶段的范围，不是当前暂停指令。

当前补充：已通过 Codex 项目列表确认桌面接入，主机为 remote-ssh-discovered:repomesh-wsl，路径为 /home/xubohan/projects/Repomesh_Go_ver。随后出现刷新令牌撤销错误；故障时 Windows 账户接口正常而 WSL 失败。用户现已完成 WSL 独立设备登录，SSH 账户信息与额度接口复验均通过，Windows 端仍正常。见 [登录恢复记录](AUTH-RECOVERY.md)。随后已更换桌面实际连接的旧后台进程，原报错任务返回 WSL_AUTH_OK，见 [桌面最终验收](DESKTOP-AUTH-VERIFIED.md)。以下 S1/S2 验证保留为初始环境结果。

更新：2026-09-12。用户已批准仅 RepoMesh 使用 WSL，其余 Windows 项目保留本地 Codex。B02 仍暂停。

SSH 与远端 Codex 在准备阶段已配置并验证；后续已确认桌面连接和项目添加完成。最新账户恢复结果以登录恢复记录为准。桌面控制工具初始化和重置后均返回 `trusted Node process exited unexpectedly`，没有实际点击应用设置，也没有更改全局 Agent／终端设置。

| 批次 | 结果 | 证据 |
| --- | --- | --- |
| S1 专用 SSH | VERIFIED | loopback 监听、严格 host key 校验、专用密钥、Ubuntu 冷启动 2.2 秒、原有 8 个唯一 Host 的 ssh -G 输出完全相同 |
| S2 远端 Codex | VERIFIED | SSH app-server 握手、账户读取、已认证额度接口、6 个可用模型、项目内 54 个 pstack 技能／项目外 0 个 |
| S3 桌面接入 | VERIFIED | 用户完成接入，Codex 项目列表已返回正确的远端主机及 Linux 工程目录；认证另见恢复记录 |
| S4 文档交接 | VERIFIED | 根说明、当前交接、施工表和 WSL 建议已同步本方案 |

## 已配置的入口

- SSH 名称：`repomesh-wsl`；Linux 用户：`xubohan`。
- 工程目录：`/home/xubohan/projects/Repomesh_Go_ver`。
- Windows SSH 配置：`C:/Users/18092/.ssh/config`，只追加本项目 Host 块。原文件备份位置见 [setup.json](setup.json)。
- 专用密钥与主机记录：用户 `.ssh` 目录下的 `id_ed25519_repomesh_wsl` 和 `known_hosts_repomesh_wsl`，不进入仓库。
- Linux 服务配置：`/etc/ssh/sshd_config.d/00-repomesh-wsl.conf`；只监听 `127.0.0.1:22222`，禁止 root、密码和键盘交互登录。
- 启动入口：`/usr/local/bin/repomesh-ssh-proxy`。主机专属 ProxyCommand 调用 WSL，等待 SSH 就绪后转发原始流，无需开机任务或固定 WSL IP。
- 远端 Codex：`/usr/local/bin/codex`，版本 `0.153.4`。最初按官方 headless 方法复用本机登录缓存；后续刷新失效后已改为 WSL 独立设备登录，并通过认证接口复验。未调用模型生成、未消耗重置额度。

Windows PowerShell 可直接执行：

```powershell
ssh repomesh-wsl
```

## 应用接入操作记录（已完成）

1. 打开 Codex **设置 → Connections／连接 → SSH hosts**，选择或添加 `repomesh-wsl` 并启用连接。SSH 别名已经写入配置，无需再填私钥内容。
2. 添加项目时选择这个 SSH 主机，目录填写 `/home/xubohan/projects/Repomesh_Go_ver`。
3. 保留 Windows 本地 Agent 设置；无需将全局 Agent 和内置终端改为 WSL。在新主机的项目任务中检查 `pwd`、`uname -s` 和 `command -v codex`，确认实际使用 Linux 工程。

仅打开 Windows D 盘或 WSL UNC 目录不能替代 SSH 主机绑定。本轮没有创建新任务，也没有恢复 B02。

此前 Windows 浏览器访问 WSL Web／Vite 已通过，见 [WSL 环境验收](../2026-09-12-wsl/README.md)。SSH Agent 对 Windows 浏览器的自动控制仍需在远端任务中单独配置和验收，不能从 SSH 连通推断其可用。

## 验证与复核

机器结果见 [verification.json](verification.json)。只读代理 wsl_ssh_review 独立检查了实际 listener、sshd 有效配置、私钥 ACL、Linux 公钥权限、主机指纹和 BatchMode 登录，S1/S2 无发现。检查未读取私钥或登录凭据。未把 app-server 成功当作桌面连接成功。

本轮只修改开发环境和文档，未改业务代码，未重复运行已通过的 B01 产品检查。原 Windows 工程保留；本轮文档单独同步到 WSL 副本。

依据：[Codex SSH 连接说明](https://learn.chatgpt.com/docs/remote-connections#connect-to-an-ssh-host)、[Codex 登录缓存与 headless 登录](https://learn.chatgpt.com/docs/auth)。
