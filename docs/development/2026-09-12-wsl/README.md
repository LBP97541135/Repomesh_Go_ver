# WSL 开发环境验收

日期：2026-09-12。环境已准备；Codex 桌面 Agent／终端切换由用户手动完成。B00、B01 保持 VERIFIED，B02 未启动。

主开发副本为 `/home/xubohan/projects/Repomesh_Go_ver`。Windows 打开路径为 `\\wsl$\Ubuntu-22.04\home\xubohan\projects\Repomesh_Go_ver`。D 盘原工程保留，不做持续双向同步。本次复制以用户已有提交 `ad9a49b2fe3b5fe743a65c838ded6cb022e79363` 为基线，保留当时的验证脚本修改及本地资料；没有提交或推送本轮修改。

| 项目 | 结果 |
| --- | --- |
| 默认发行版 | Ubuntu-22.04，WSL2，用户 xubohan |
| Linux 工具 | Go 1.26.4、Node 22.22.1、npm 10.9.4、PowerShell 7.6.6、PostgreSQL 17.11、bubblewrap 0.6.1 |
| Codex | Linux CLI 0.153.4；实际沙箱命令通过 |
| pstack | app-server 实测项目内 54 个技能、项目外 0 个、加载错误 0 |
| 文件复制 | 初始清单 10,661 个文件、210,755,913 字节；哈希和逻辑暂存内容核对通过 |
| Git | 保留 main／HEAD；独立 AgentTeams 保持原版本及干净状态；恢复索引规定的可执行位 |
| Git 认证 | 目标仓库复用 Windows Git Credential Manager；远程读取成功，不复制凭据文件 |
| B01 | Windows、Linux 均完成 Go／前端检查和真实 PostgreSQL 验证，无数据库测试跳过 |
| Linux 打包 | linux/amd64；10 个产物哈希、3 个版本、Windows 浏览器页面及探针通过 |
| Vite | Windows 浏览器自动显示入口文件修改，恢复源码后页面恢复；入口变更触发自动整页刷新，未声称组件状态保留 |
| 失败清理 | 不存在的 PostgreSQL 路径被明确拒绝，结果 NOT_VERIFIED 且 cleanupPassed=true |
| 服务清理 | 本次 PostgreSQL、Web、Vite 和专属测试浏览器均已关闭 |

开发工具使用 Linux 原生路径。Go、Node 和 Codex 的固定版本位于 `/opt/repomesh-dev-tools`，通过 `/usr/local/bin` 调用；原 `/usr/bin/node` 保留。PowerShell、PostgreSQL 来自各自官方软件源。PostgreSQL 不创建常驻 main 集群，验证脚本创建并清理专属实例。补齐 bubblewrap、ripgrep，并从 GitHub 官方地址更新了原有过期的软件源签名密钥。

Windows 的 `.wslconfig` 保持 NAT，仅增加 `localhostForwarding=true`，并保留 `.wslconfig.repomesh-20260912.bak`。重启 Ubuntu 后，Windows 浏览器可访问 WSL 的 `127.0.0.1` 服务。[微软的 localhost 转发说明](https://learn.microsoft.com/en-us/windows/wsl/networking#accessing-linux-networking-apps-from-windows-localhost)

自动审批曾因超时中断一次配置检查，重试后继续。另一次审批拒绝 `0.0.0.0` 监听方案，理由是扩大网络暴露；该方案没有执行。最终通过的方案保持回环监听，没有新增防火墙放行规则。

复制保留历史实验及本地文档，排除清单中的 node_modules、Python 缓存及产品旧构建输出。历史符号链接保持原目标，可能仍指向旧实验环境；不表示旧实验可直接运行。原 Windows worktree 元数据单独保存在 `/home/xubohan/projects/repomesh-windows-worktree-metadata-20260912`，不注册为 Linux 活动工作树。当前 AgentTeams 的脚本可执行位按其独立 Git 索引恢复。

继续开发前，由用户完成保留的第 2 项：在 Codex 设置中将 Agent 和终端切到 WSL，重启 Codex，再打开上述 WSL 工程目录。在新的 WSL 任务中确认 `pwd`、`command -v go node npm pwsh`，然后读取当前 HANDOFF 和 IMPLEMENTATION-PLAN。桌面切换和切换后的模型调用尚未验收；单独安装的 Linux CLI 尚未单独登录。Docker、真实模型调用和 AgentTeams 运行接入不属于本轮环境验收。

## 可复验入口

在 WSL 工程根目录运行：

```bash
pwsh -NoProfile -File scripts/verify-batch.ps1 -Batch B01 -PostgresBin /usr/lib/postgresql/17/bin
```

脚本每次创建新的证据目录，保留原有数据库测试、错误诊断和清理门槛。生产打包继续使用 `scripts/build.ps1`，重复打包须使用新版本标签。

## 证据

- [复制清单](copy-manifest.json)、[最终文件差异与状态核查](final-verification.json)
- [Windows B01](windows-b01/checks.json)、[Linux B01](linux-b01/checks.json)、[Linux 失败清理](linux-cleanup-negative/checks.json)
- [工具、沙箱和 Git 验证](platform-verification.json)、[项目技能发现](codex-verification.json)
- [发布包与服务清理](runtime.json)、[Windows 页面验证](windows-browser-package.json)
- [Vite 文件变化](windows-browser-hmr.json)、[源码恢复](windows-browser-restored.json)
- [执行计划](PLAN.md)、[审查及修订](review.md)、[决策记录](decisions.tsv)
- [最终 localhost 配置和 Windows 访问](network-verification.json)

Sequence Work into Verifiable Units 用于分开验收复制、工具、数据库和浏览器；Prove It Works 用于要求真实数据库与 Windows 浏览器证据。原始失败保留在过程记录中，未改写成成功。
