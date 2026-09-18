# 开发暂停记录

用户于 2026-09-12 要求停止施工、对齐进度，并先核对 Codex 与 WSL 设置。

- B00 VERIFIED。工具版本、六项工程检查、实际 Web 探针与三个入口已验证。
- B01 VERIFIED。pgx 连接、迁移历史与显式 db check/migrate、12 组真实 PostgreSQL 测试、二进制失败诊断、PostgreSQL 进程重启、全部工程检查已通过。
- 代码及脚本独立复核通过。复核提出的失败原因断言、实际 HTML 比对、文件哈希清单、打包证据均已补全。详情见 [review.md](review.md)。
- 配套包位于 dist/repomesh-0.1.0-b01-20260912，三个版本及 10 个产物哈希匹配。它仍标明业务未就绪。
- B02 及其后续批次未开始。OAuth、项目表与 API、模型、Issue、运行接入均未实现。C05、C06、P9 的状态未改变。

本轮代码已经写入主工作区。父仓库基底为 4516805a276d65eb79490a12783654f3d7b1c677，但大量新增和修改尚未提交，只有该提交无法恢复本轮工作。未执行整体暂存、提交、推送或迁移目录。历史既有改动保留。精确的 9 个实现文件和来源工作区见 [integration-source.json](integration-source.json)，新增脚本与属性哈希见 [structural-checks.json](structural-checks.json)。

临时实现工作区保留在 C:/Users/18092/AppData/Local/Temp/repomesh-b01-postgres-20260912，分支 codex/b01-postgres-foundation，实现代理已停止写入。数据库验证实例已全部停止并清理；首个失败启动遗留的目录也经 pg_ctl 确认未运行后清理。bin/dev-postgres17 保留已下载的本地测试二进制，不随产品发布。

恢复时先查看 [IMPLEMENTATION-PLAN.md](../../plan/IMPLEMENTATION-PLAN.md)。若采用 WSL，先按 [WSL 建议](../../current/wsl-development-recommendation.md)保存未提交和忽略文件清单、处理外部 worktree 元数据及独立 AgentTeams 状态，然后复制到 Linux 文件系统并重新验收。当前 scripts/verify-batch.ps1 仍面向 Windows 原生 PostgreSQL，尚未适配 Linux。

本次暂停不创建 wip 提交，因为工作区包含用户之前的大量未提交资料，而本次指令要求记录进度；磁盘文件、摘要及决策轨迹已经持久保存。决策日志见 [decisions.tsv](decisions.tsv)，验收总览见 [README.md](README.md)。

## 暂停后的 Codex 与 WSL 只读核对

目标发行版 Ubuntu-22.04 已存在并使用 WSL2，普通用户为 xubohan。当前默认发行版却是 docker-desktop。建议在 Windows 中执行 wsl --set-default Ubuntu-22.04，再在 Codex Settings 中把 Agent 设为 WSL并重启应用。内置终端是另一项独立设置，也选择 WSL；它只对新终端生效。依据为[官方 Windows 应用说明](https://learn.chatgpt.com/docs/windows/windows-app#windows-subsystem-for-linux-wsl)和[终端设置](https://learn.chatgpt.com/docs/windows/windows-app#customize-for-your-dev-setup)。

目标 /home/xubohan/projects/Repomesh_Go_ver 尚未创建。原项目 .codex/config.toml 的 marketplace source 仍指向 D:/Project4work/Repomesh_Go_ver，复制并验收新目录后才将目标副本改成 Linux 绝对路径。WSL 默认环境未找到 bwrap，官方建议安装 bubblewrap。command -v codex 当前命中 /mnt/c/Users/18092/AppData/Roaming/npm/codex，这不能证明已有 Linux 原生 CLI。桌面应用的 Agent 设置与独立 CLI 安装分开核对。

本次仅记录建议，没有修改默认发行版、Codex 全局设置、项目插件路径，没有安装 WSL 工具或复制源码。
