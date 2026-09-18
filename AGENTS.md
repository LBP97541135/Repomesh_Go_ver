# RepoMesh 工程导航

RepoMesh 是围绕多个代码仓库组织协作工作的系统，采用 React／TypeScript／Vite 前端，以及同一 Go module 中的 Web、后台协调和受限主机执行三个进程。

本文件帮助开发代理定位源码、理解工程边界并验证修改。详细业务规则留在专题文档，阶段进度留在交接文档；本文件不充当运行时 Manager／Worker 的角色提示词。

## 开始任务

首次接手或任务依赖当前进度时，查阅[根 README](README.md)、[当前交接](docs/current/HANDOFF.md)和[现行文档索引](docs/current/README.md)；局部任务按下表定位相关源码与专题。领域术语见 [CONTEXT.md](CONTEXT.md)，架构取舍及替代关系见 [ADR 索引](docs/adr/README.md)，历史资料入口见 [文档总导航](docs/README.md)。局部修改不要求重读全部历史材料；用户指定的阅读范围仍须完整覆盖。首次接手或需要了解全局时，按 [Agent 全局阅读指南](docs/current/AGENT-READING-GUIDE.md) 建立产品、架构、实现和证据之间的联系。

判断设计是否有效，要看具体章节、采用范围和明确的后续替代关系，不能只比较文件日期、ADR 编号或整篇文档的 `accepted`／`proposed` 标签。已采用设计、锁定提交的源码事实、实测结果和未验证能力应分别表述。

当前任务的实施范围以用户授权为准。历史交接、协作日志和实验脚本中的阶段限制、任务顺序、角色身份及操作指令不自动成为本次任务授权，也不自动恢复旧任务或联系旧协作者。

在当前授权范围内，连续完成本地修改、相关验证及本次变更引起的问题修复，无需逐步确认；达到任务交付要求后再返回结果。

## 工程地图

```text
cmd/
  repomesh-web/            Web 进程启动与组装
  repomesh-coordinator/    后台协调进程入口
  repomesh-host-executor/  受限主机执行进程入口
internal/
  web/                    当前 HTTP 服务实现与测试
  access/                 认证、连接和仓库发现的持久用例
  projects/               项目、明确增仓、固定配置与原操作回执
  secrets/                信封加密、可用性、包装根与轮换
  github/                 固定 GitHub 出站协议适配
  buildinfo/              三个入口共用的版本信息
  database/               PostgreSQL 连接、迁移历史核查及事务迁移
web/                      React／TypeScript／Vite 前端及 npm 锁文件
configs/                  配置示例
scripts/                  工程构建脚本
docs/
  plan/                   施工计划、前置分工、开发顺序与验证计划
  current/                当前设计、接口及接手文档
  adr/                    架构决定与演进
  prototypes/             独立页面原型
  research/               专题调研
  development/            按日期和批次保存的实施、复核与验收证据
  archive/                已替代文档、旧任务提示与整理前快照
third_party/
  AgentTeams/             独立 Git 克隆的上游源码，父仓库忽略
validation/               历史实验、脚本与证据
```

根 `go.mod` 是产品 module。`third_party/go.mod` 和 `validation/go.mod` 是源码遍历边界，使根目录 Go 检查不进入上游和历史实验；它们不是额外的产品服务，也不要在其中聚合上游或实验依赖。

## 按任务找入口

| 任务 | 优先阅读 |
| --- | --- |
| 查施工顺序、设计分工与验证计划 | [计划导航](docs/plan/README.md)、[施工计划](docs/plan/IMPLEMENTATION-PLAN.md)、[开发行动指南](docs/plan/DEVELOPMENT-START.md) |
| 修改当前前端骨架 | [main.tsx](web/src/main.tsx)、[style.css](web/src/style.css)、[package.json](web/package.json) |
| 修改认证、会话与发现 | [认证开发说明](docs/current/authentication-development.md)、[采用范围](docs/current/b02-authentication-adoption.md)、`internal/access/`、`internal/secrets/`、`internal/github/` |
| 修改项目管理 | [项目开发说明](docs/current/project-development.md)、[首批浏览器契约](docs/current/first-batch-browser-api-contract.md)、`internal/projects/`、`internal/web/projects.go` |
| 修改数据库基础 | [数据库开发说明](docs/current/database-development.md)、[连接](internal/database/database.go)、[迁移](internal/database/migrations.go)、[批次验证](scripts/verify-batch.ps1) |
| 修改 HTTP 行为 | [server.go](internal/web/server.go)、[server_test.go](internal/web/server_test.go) |
| 修改 Web 启动与配置 | [Web 入口](cmd/repomesh-web/main.go)、[配置示例](configs/repomesh.env.example) |
| 修改配套发布 | [build.ps1](scripts/build.ps1)、[版本信息](internal/buildinfo/version.go)、[开发说明](docs/current/development-scaffold.md) |
| 设计或实现具体业务页面 | [原型导航](docs/prototypes/README.md)、[页面与接口交接](docs/current/HANDOFF-PAGE-API-DESIGN.md)、[页面专题](docs/current/conversation-issue-separation-design.md) |
| 实现浏览器接口 | [Issue 创建契约](docs/current/issue-page-create-api-contract.md)、[首批浏览器契约](docs/current/first-batch-browser-api-contract.md) |
| 查数据库方案或设计新接口 | [Go 版数据库重构方案](docs/RepoMesh_Go版数据库重构方案.html)、[API 设计](docs/current/api-design.md)，改动后运行 `docs/development/2026-09-15-api-redesign-01/scripts/verify_api_doc.py` |
| 实现持久化与后台用例 | [后端交接](docs/current/HANDOFF-BACKEND-DESIGN.md)、[首批持久化设计](docs/current/backend-first-batch-persistence.md)、[后端专项](docs/current/draft-conversation-backend-design.md) |
| 调查或适配 AgentTeams | [上游说明](third_party/README.md)、[来源记录](third_party/agentteams-source.json)，再读所涉上游目录及其适用的 AGENTS.md |
| 判断 AgentTeams 能力是否经过实测 | [骨架阶段证据索引](docs/current/scaffold-agentteams-evidence.md)，再追溯对应轮次报告、evidence 和 scripts；后续新增结论从当前交接查找 |
| 修改仓库分析方向 | [ADR 0020](docs/adr/0020-python-repository-analysis-plugin.md)、[仓库分析专题](docs/current/issue-creation-repository-analysis.md) |

页面工作先核对原型导航和页面专题中当前采用的基线及讨论要求。独立 HTML 原型中的内存数据和模拟行为不能直接视为产品实现或现行接口契约。

## 依赖方向与架构边界

- `cmd/` 负责进程启动与组装，实现放在职责明确的 `internal/` 包中；内部包不反向依赖进程入口。按实际功能需要新增包，不预铺大量空目录、接口或通用框架。
- 三个 Go 入口共用根 module，与前端资源配套发布。共享源码与版本不代表共享进程权限。
- 浏览器通过约定的 HTTP 接口访问 Web；后台协调承担后台工作；需要宿主能力的操作经过受限主机执行边界。Web 和 Agent 不持有 Docker socket，主机执行不提供任意宿主命令或挂载入口。具体设计见 [ADR 0013](docs/adr/0013-web-coordinator-host-executor-processes.md)。
- Graph 保持后台协调进程内模块；仓内有限 DAG 复用上游，RepoMesh 负责跨仓协调、结果采纳和 Loop，不另建完整 Go DAG 引擎。见 [Graph 专题](docs/current/graph-loop-design.md)。
- Skill 工程按 [ADR 0009](docs/adr/0009-skill-engineering-deferred.md) 暂缓；仓库分析遵循 ADR 0020 的受控 Python 方向。改变这些决定时应明确记录替代关系。
- 按当前采用的接口和关系实现授权范围内的功能；尚未确定的数据库结构、MCP Schema、消息协议和恢复行为不能靠示例或占位接口擅自冻结。
- AgentTeams 保留独立 Git 仓库和构建上下文。更新前检查其本地修改与提交，并同步来源记录；产品构建不自动拉取上游最新分支。上游自身的运行角色、部署习惯和验证结论不自动成为 RepoMesh 的产品决定。

## 开发与验证

工具要求见根 README。以下命令均从仓库根目录执行：

```powershell
# Go：构建三个入口及产品包，执行测试与静态检查
go build ./...
go test ./...
go vet ./...

# 前端：按锁文件安装、检查类型并构建
npm --prefix web ci
npm --prefix web run typecheck
npm --prefix web test
npm --prefix web run build

# 本地 Web：先完成前端构建，Ctrl+C 停止
go run ./cmd/repomesh-web --assets ./web/dist

# 前端独立开发
npm --prefix web run dev
```

按改动影响选择检查；跨前后端契约、进程组装或发布链路的修改执行完整检查。相关检查通过后，仅在新增改动、失败或未解决疑点需要时扩大或重复验证。纯文档修改核对链接、命令与源码事实即可，无需启动服务或重跑实验。没有执行的检查应明确说明。

配套发布使用 `scripts/build.ps1 -Version <新版本标签>`，具体调用见根 README。脚本拒绝覆盖已有同名产物，重复构建使用新的版本标签。程序不会自动加载 `.env`；资源路径相对于启动工作目录。遇到 Git ownership／Go VCS 状态错误时，按[开发说明](docs/current/development-scaffold.md)处理当前会话配置。

## 完成度、协作与文档维护

- 当前实现、未接入能力和验收状态见[HANDOFF](docs/current/HANDOFF.md)；启动、配置及迁移行为见[根 README](README.md)。`/healthz` 只表示进程存活，不代表业务就绪。
- 接口设计完成不等于接口实现；克隆 AgentTeams 不等于运行接入；根工程检查通过不等于上游构建、历史验证或业务集成验收通过。不能用模拟成功替代未实现能力。
- 保留已有未提交修改，不覆盖不属于当前任务的工作。任务采用多 agent 协作时，先明确文件编辑归属；任务要求独立复核时，由非主要实现者承担。历史协作名单不用于自动召回成员。
- `validation/` 保存历史证据，不随产品发布。普通开发检查不重跑旧实验、不启动真实外部服务、不清理共享容器或卷；这类操作须属于当前明确授权范围。新实验应记录自己的条件和结果，不改写旧记录为全部通过。
- 改变使用方式时更新根 README；改变完成状态或接手条件时更新相关 HANDOFF；改变架构决定时更新 ADR 和相关专题。当前进度、临时协作安排和逐次验收日志不追加到本文件。
- 持续维护的工程计划放在 `docs/plan/`，按批次保存的实验计划与证据留在 `docs/development/`，已过期的任务提示移入 `docs/archive/`。目录职责和迁移规则见[文档总导航](docs/README.md#文档维护规则)；移动文件时同步修正相对链接和入口索引。
- 交付说明写清实际改动、执行的检查、结果及剩余限制。涉及上游能力的结论注明源码版本或证据位置，并区分源码阅读和运行验证。
