# RepoMesh 专用 WSL SSH 开发环境

用户已批准仅本项目使用 WSL，其余 Windows 项目继续使用本地 Codex。B02 保持暂停。

| 批次 | 工作 | 验证 | 状态 |
| --- | --- | --- | --- |
| S1 | 专用 loopback SSH、密钥、host key、按主机启动 WSL | SSH 登录、配置隔离、冷启动 | VERIFIED |
| S2 | 远端 Codex 登录与工具 | SSH login shell、app-server、项目插件 | VERIFIED |
| S3 | 桌面 Connections 主机与项目 | 应用实际连接 Linux 项目、Windows 设置保留 | VERIFIED：用户完成接入，项目列表已确认 |
| S4 | 更新交接和建议 | 文档链接、路径、实际状态 | VERIFIED |

采用 pstack figure-it-out 流程。操作是已确定机制的环境配置，不增加产品模块，跳过类型架构设计。根代理负责写配置与应用操作；wsl_ssh_review 已独立复核隔离、启动、PATH 与验收边界。

只向 Windows SSH config 追加专用 Host 块，备份原文件。Linux sshd 只监听 loopback；认证文件只保存在用户目录，不进入仓库或日志。通过主机专属 ProxyCommand 启动发行版，避免全局启动任务。
