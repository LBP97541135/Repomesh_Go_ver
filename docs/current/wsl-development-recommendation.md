# RepoMesh 使用 WSL2 开发的建议

日期：2026-09-12。状态：环境与专用 SSH 已验证，Codex 桌面接入已确认，远端独立登录及账户接口复验已通过。本次开发任务又直接核实 Linux 目录、工具路径和桌面项目绑定，用户已恢复开发，见 [B02 接手记录](../development/2026-09-12-b02-preflight/README.md)。

本文保留建议提出时的快照。后续已完成源码复制、工具安装和 Windows／Linux 验收，实际结果见 [WSL 环境验收](../development/2026-09-12-wsl/README.md)。下文“未迁移”“待验证”等表述属于实施前记录；不覆盖上述最新验收。Docker 与产品的平台支持范围未因此改变。

## 当前 Codex 接入方案

用户已选择仅 RepoMesh 使用 WSL，其他项目继续使用 Windows。本机 Ubuntu 通过专用 SSH 主机 `repomesh-wsl` 接入 Codex，工程目录为 `/home/xubohan/projects/Repomesh_Go_ver`。无需将全局 Agent／终端改为 WSL。SSH 与远端 Codex 已通过验证；桌面项目已接入，登录刷新故障已通过独立设备登录恢复，见 [SSH 接入记录](../development/2026-09-12-wsl-ssh/README.md)。此决定替代此前会话中的全局切换建议，以下环境迁移建议保留为历史依据。

## 建议与适用条件

建议将 WSL2 作为 RepoMesh 的主要开发环境，源码和开发工具放在 Linux 文件系统中，Windows 保留编辑器界面和浏览器。当前 B00 和 B01 已完成验证，并按用户要求暂停施工。进度和未提交工作已记录在[暂停记录](../development/2026-09-12-batch-01/PAUSE.md)，接下来先准备 Codex 与 WSL 环境，再决定主工作目录切换。

这个建议基于项目已采用的容器管理、受限主机执行和受控 Python 分析方向。开发时需要验证进程停止、目录权限、挂载和资源限制，使用 Linux 环境更接近这些能力的预期运行条件。具体生产部署平台仍需明确，本文不将 Linux 单平台支持写成已采用决定。依据见 [ADR-0013](../adr/0013-web-coordinator-host-executor-processes.md)、[ADR-0020](../adr/0020-python-repository-analysis-plugin.md)和[工程开发说明](development-scaffold.md)。

当前三种选择的比较如下。

| 选择 | 适合的工作 | 对 RepoMesh 的建议 |
| --- | --- | --- |
| Windows 源码与 Windows 工具 | 当前前端、Go 业务代码和 Windows 原生兼容检查 | 可继续完成当前批次，保留为需要时的 Windows 验证环境。 |
| Windows 源码，经 WSL 的 `/mnt/d` 访问 | 临时运行 Linux 命令、短期兼容检查 | 适合过渡，不作为长期主开发布局。 |
| WSL Linux 文件系统中的源码与工具 | Linux 构建、数据库验证、后续容器和执行集成 | 推荐作为主要开发布局，切换前完成本文所列验证。 |

微软建议在使用 Linux 命令行工具时，将项目文件存放在 WSL 文件系统中，减少跨文件系统访问开销。[微软文件系统说明](https://learn.microsoft.com/en-us/windows/wsl/filesystems)

Docker 对 WSL2 的建议同样是将挂载进 Linux 容器的源码放在 Linux 文件系统中，这关系到挂载性能和文件变更通知。上述收益来自官方指导，本项目尚未做迁移前后的性能对比，不能承诺具体加速倍数。[Docker WSL2 最佳实践](https://docs.docker.com/desktop/features/wsl/best-practices/)

## 已检查的本机情况

以下为本次会话在 2026-09-12 的检查快照。版本由实际命令读取；“未找到”仅指该次默认非交互环境的命令搜索结果，不证明整个磁盘上没有安装。

| 项目 | 检查结果 | 后续处理 |
| --- | --- | --- |
| WSL 发行版 | 已有 `Ubuntu-22.04`，使用 WSL2 | 已能通过 `wsl.exe -d Ubuntu-22.04` 执行命令；最终工具组合仍需验收。 |
| Linux 内核 | `5.15.167.4-microsoft-standard-WSL2` | 这是内核版本，不是 WSL 应用版本；容器准备阶段另查 WSL 与 Docker 的兼容要求。 |
| Linux 用户 | `xubohan`，UID 为 1000 | 可使用普通用户目录作为开发目录。 |
| 当前源码访问 | `/mnt/d/Project4work/Repomesh_Go_ver` | 访问成功，但文件仍位于 Windows 的 D 盘。 |
| Go | `command -v go` 未找到 | 按根 `go.mod` 安装 Go 1.26.0 或满足工程要求的版本。 |
| Node.js / npm | Node.js `18.20.8`，npm `10.8.2` | Node.js 低于当前前端要求的 `22.12.0`，需升级并重新安装前端依赖。 |
| Git / Python | Git `2.34.1`，Python `3.10.12` | 只确认命令存在及版本；Python 分析依赖尚未验证。 |
| PowerShell | `command -v pwsh` 未找到 | 当前打包和批次检查使用 PowerShell，需准备合适的 Linux 版本。 |
| Docker | 找到 Docker Desktop 提供的命令入口 | 未执行引擎连接、容器启动或挂载验证，不能据此宣布集成可用。 |

工程版本要求来自 [go.mod](../../go.mod)、[前端 package.json](../../web/package.json)和[根 README](../../README.md)。Ubuntu 与目标 PowerShell 版本的支持组合应在安装时核对，不能假设所有 PowerShell 7 版本都适用于现有发行版。[PowerShell Ubuntu 安装说明](https://learn.microsoft.com/en-us/powershell/scripting/install/install-ubuntu)

本次命令启动还出现了 WSL 的 localhost 代理转发提示。文件访问和版本检查成功不代表软件源、模型接口或其他出站网络已经可用，准备阶段应分别验证实际需要的连接。

## 源码基线与当前迁移成本

本次读取时，父仓库 HEAD 为 `4516805a276d65eb79490a12783654f3d7b1c677`，工作区有大量已修改、未跟踪和删除的文件。以下结论包括未提交工作，不能仅检出该提交就复现全部状态。

旧骨架说明与进行中的开发存在时间差。本次已看到 `internal/database/`、`go.sum`、`pgx` 依赖和 Web 的 `db check` / `db migrate` 入口；[施工计划](../plan/IMPLEMENTATION-PLAN.md)读取时将数据库基础批次 B01 标为 `IN_PROGRESS`。这是本建议最初读取时的快照。随后 B00 和 B01 已完成验证并暂停，最新结果见[批次验收](../development/2026-09-12-batch-01/README.md)。迁移实施时重新读取计划和实际源码，不能继续按“只有标准库、没有数据库代码”准备环境。

已确认需要处理的迁移事项如下。

| 位置 | 本次源码观察 | 建议处理 |
| --- | --- | --- |
| [配套打包脚本](../../scripts/build.ps1) | 使用 `go env GOOS` 和 `go env GOARCH` 判断目标，按目标选择 `.exe` 后缀。 | 优先保留并在 Linux PowerShell 中验证，不必仅因迁移就重写一份 Bash 打包流程。 |
| [批次验证脚本](../../scripts/verify-batch.ps1) | PostgreSQL 工具写死为 `initdb.exe`、`pg_ctl.exe`、`postgres.exe`、`psql.exe`。 | 适配 Linux 工具名和发现方式，保留同一套验收语义。 |
| 同一批次验证脚本 | 假设 `npm-cli.js` 位于 Node 可执行文件旁的固定目录；临时目录检查包含 Windows 路径处理。 | 核对 Linux 安装布局和清理边界，验证正常结束及失败后的资源回收。安装 `pwsh` 本身不足以证明脚本可运行。 |
| [项目插件配置模板](../../.codex/config.example.toml) | 原 Windows 本机 marketplace 指向 `D:/Project4work/Repomesh_Go_ver`；现仅跟踪占位模板。 | 复制模板为 Git 忽略的 `.codex/config.toml`，填写插件加载进程可访问的新绝对路径，并重新验证技能发现。规则见[插件说明](../../.agents/plugins/README.md)。 |
| [AgentTeams 来源记录](../../third_party/agentteams-source.json) | 上游是独立 Git 仓库，被父仓库忽略；记录的版本为 `517caff9280242a00a4d4c06365352b9e41659c6`。 | 单独核对上游实际 HEAD 和本地修改；迁移保留原状态，重建时使用确定版本。只克隆父仓库不会带回上游。 |

因此，建议的迁移范围包含开发工具、验证入口和项目路径配置。业务代码继续共用，只有真实存在的操作系统差异才集中适配。

## 建议的目录与工作方式

建议目标目录示例为：

```text
/home/xubohan/projects/Repomesh_Go_ver
```

这是建议路径，本次没有创建该目录。它位于 Ubuntu 自身的 Linux 文件系统中。实际 Windows 磁盘空间是否足够，还应结合 WSL 虚拟磁盘所在卷检查；不能把 Linux 中显示的虚拟磁盘容量直接当作 Windows 主机可用空间。

| 工作 | 建议运行位置 |
| --- | --- |
| 编辑器界面、浏览器 | Windows；编辑器连接 WSL 工作目录，构建和调试进程在 WSL 执行。 |
| Git、Go、Node/npm、Python、PowerShell 检查 | WSL，使用该环境安装的工具和缓存。 |
| 当前数据库集成验证 | WSL 的独立测试 PostgreSQL，使用本次验证专属实例和数据。 |
| 后续容器集成 | 先验证 Docker Desktop 对目标发行版的 WSL 集成，再按已采用的执行边界接入。 |
| Windows 原生版本验收 | 独立 Windows 工作副本或对应 CI 环境，按产品支持范围安排。 |

编辑器、开发代理和终端的实际执行位置都应检查。只把窗口打开到新目录，不代表它们已使用 Linux 工具。开发者能够访问 Docker 也不改变产品边界：Web 和 Agent 仍不持有 Docker socket，宿主操作经过受限主机执行进程。

不要在 Windows 与 Linux 之间共用 `node_modules`、Python 虚拟环境和平台相关构建产物。目标副本按锁文件重新安装依赖，历史证据继续保留原运行条件。

## 分步迁移方案

### 1. 收口当前工作并保存迁移清单

待当前开发批次到达可交接状态后，约定切换时点。记录父仓库和独立上游的 HEAD、分支、暂存区、未暂存修改、未跟踪文件，以及需要保留的忽略文件。活动进程和其他编辑者停止写入后，再制作一致的开发副本。

清单应覆盖本地文档、历史验证材料、必要配置和工作区元数据。秘密配置单独处理，不把值写进迁移报告或 Git。不能用一次 `git clone` 代替含未提交工作的完整保存，也不需要为了迁移而把所有现有修改一起提交。

### 2. 在 WSL 建立开发副本

先确认目标路径不存在，再以复制方式准备目标目录，保留原 Windows 目录用于核对和回退。迁移前检查 `.git` 是普通目录还是指向其他位置的文件，并核对已有 worktree；外部 Git 元数据引用需要单独处理。

对照清单核对文件内容、分支、暂存状态、文件模式和符号链接。AgentTeams 单独核对，保留 `third_party/go.mod` 与 `validation/go.mod` 的遍历边界。依赖缓存与旧构建产物在新环境重建，历史证据不改写。

### 3. 准备工具与配置

安装符合工程要求的 Linux 工具，确认实际执行路径；配置所需的软件源、Git 认证和出站网络。更新目标副本的项目绝对路径，验证插件发现与开发代理执行位置。

先保留现有 PowerShell 打包入口，再按上表适配批次验证脚本。数据库基础检查可以先使用独立 PostgreSQL；Docker 挂载和 AgentTeams 运行验证留到相应集成范围，不作为“文件已复制”的默认附带操作。

### 4. 完成验收后切换

在目标副本保存新的命令、版本、退出码和检查结果。达到下一节的切换条件后，更新实际使用说明和交接记录，并把 WSL 副本设为日常工作的唯一主要副本。

原 Windows 目录作为保留副本，不长期双向自动同步。若继续承担 Windows 验证，使用明确的 Git 提交或补丁同步源码，依赖各自安装。

## 切换条件与验收范围

以下是后续实施时的检查要求，本次没有执行。

首先在 WSL 项目根目录执行基础工程检查，各命令成功后再继续下一项：

```bash
go version
node --version
npm --version

npm --prefix web ci
npm --prefix web run typecheck
npm --prefix web run build

go build ./...
go test -count=1 ./...
go vet ./...
```

工程命令通过后，仍需完成下面的行为验证。

| 验收项 | 通过条件 |
| --- | --- |
| 工作区一致性 | 所需源码、未提交修改、上游状态和历史材料均能按迁移清单核对；环境适配改动单独列明。 |
| 数据库集成 | 使用独立测试实例设置 `REPOMESH_TEST_DATABASE_URL`，执行真实迁移、并发、回滚及历史不匹配测试。未配置时 `TestPostgres*` 会跳过，普通 `go test` 成功不足以证明通过。 |
| Web 与三个入口 | 从 WSL 启动构建产物，检查页面、版本、探针和退出行为；从 Windows 浏览器验证访问。预期结果以切换时的实现为准，当前 `/readyz` 的 503 不应被写成迁移故障或业务就绪。 |
| Vite 开发 | 实际编辑文件，确认浏览器更新，并确认运行的是目标副本的源码。 |
| 配套打包 | 使用新的未占用版本标签运行 `pwsh -NoProfile -File scripts/build.ps1 -Version <新版本标签>`，检查 Linux 目标清单、三个配套二进制和前端资源，并启动产物验证。版本占位符需替换后执行。 |
| 验证入口 | 适配后的批次脚本在 Linux 正常运行；失败能够留下证据并清理本次创建的资源，不能削弱已有真实数据库检查来换取通过。 |
| 日常工具 | 编辑器、开发代理、Git 和所需网络访问能够在 WSL 工作目录持续使用；固定插件可被正确发现。 |

本轮切换验收不自动启动真实模型调用、旧 AgentTeams 实验或共享容器清理。后续 Docker 与主机执行集成须另有真实环境证据。WSL 中通过也不替代目标 Linux 服务器和 Windows 原生版本各自需要的发布验收。

## 回退与后续记录

若工具、脚本或日常工作流尚未通过，继续使用原 Windows 目录，保留 WSL 副本中的日志和适配成果。如果 WSL 副本已经产生新业务修改，先保存并逐项合回原副本，再恢复在 Windows 的日常开发，避免丢失新工作。

实际采用本建议后，再更新根 README 的默认开发步骤、相关交接中的开发路径，以及项目插件的路径配置。记录最终发行版、工具版本、Docker 或 PostgreSQL 运行方式和真实检查结果。生产平台支持范围若发生变化，应另行记录相应架构决定；本文始终区分环境建议、源码观察和运行验收。
