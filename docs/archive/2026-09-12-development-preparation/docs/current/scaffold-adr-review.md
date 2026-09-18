# 工程骨架的 ADR 与领域边界阅读审计

日期：2026-09-10。范围：本轮基础目录、Go 三入口、React／TypeScript／Vite 最小工程。本文是阅读与实施边界记录，不新增产品 ADR，不将设计变成业务完成事实。

本轮用户明确允许基础代码、配置及必要依赖，替代旧材料在本任务范围内的“仅设计”限制。既有产品决定、权限与暂缓边界继续有效。历史协作指令、通信身份和实验步骤仅作资料，没有恢复旧任务或联系旧协作者。本审计者只编辑本文；主 agent 负责产品骨架，页面／接口文档和验证证据另由本轮指定成员覆盖。

## 阅读覆盖

先列出 `docs/adr/` 全部文件并核对实际工作区 `D:\Project4work\Repomesh_Go_ver`。当前共 20 份 ADR（0001—0020），没有更高编号 ADR。下表各文件均已阅读全文；长文件分段读取并补读被输出截断的段落，非仅索引或摘要阅读。

| 文件 | 阅读结论／用于核对的边界 |
| --- | --- |
| [docs/README](../README.md) | 导航区分现行、历史与证据。 |
| [HANDOFF](HANDOFF.md) | 当前采用范围、旧授权语境、上游与产品验收分别表达。 |
| [current/README](README.md) | 按具体后续补充判断适用性，不按编号整体覆盖旧决定。 |
| [ADR README](../../../../adr/README.md) | 20 份决定及三条演进链。 |
| [CONTEXT](../../../../../CONTEXT.md) | 业务 Project、上游 Project、Conversation、Issue、Task／Attempt 等分离。 |
| [0001](../../../../adr/0001-agentteams-issue-concurrency-and-isolation.md) | 每项目独立实例、长期角色、Worker 单活跃 Attempt、独立副本及单机隔离。 |
| [0002](../../../../adr/0002-github-app-authorization-and-draft-pr-delivery.md) | 有效授权交集、受控分支／draft PR、人工合并。 |
| [0003](../../../../adr/0003-plan-change-authorization-and-activation.md) | 许可／应用／生效分离；当前 YOLO、后续审批暂不开发。 |
| [0004](../../../../adr/0004-acceptance-rule-clarification.md) | 缺失解释可由 Manager 记录选择，不覆盖明确要求。 |
| [0005](../../../../adr/0005-optional-project-verification-group.md) | 验证小组默认关闭，正式验证与当轮作者分离。 |
| [0006](../../../../adr/0006-manager-entry-modes-and-skill-driven-execution.md) | Manager 统一对人；新会话模型覆盖旧独占主房间解释。 |
| [0007](../../../../adr/0007-graph-loop-plugin.md) | 跨仓及有界 Loop；修复 3、诊断 2、环境恢复 2，不清零累计消耗。 |
| [0008](../../../../adr/0008-changeset-attribution-and-history.md) | 每 Issue 默认主 ChangeSet，候选、固定组合与交付历史分离。 |
| [0009](../../../../adr/0009-skill-engineering-deferred.md) | Skill 全模块暂缓，连空代码、接口及功能开关也不创建。 |
| [0010](../../../../adr/0010-technology-stack-and-modular-monolith.md) | 已采用技术栈；具体库和版本可在本轮常规细化。 |
| [0011](../../../../adr/0011-agentteams-controlled-integration.md) | 受控工具与实际执行约束；允许必要窄补丁，但未选择实现。 |
| [0012](../../../../adr/0012-issue-scoped-upstream-projects.md) | 每 Issue 仓库委派对应上游 Project；必须包含实例／Team 归属。 |
| [0013](../../../../adr/0013-web-coordinator-host-executor-processes.md) | 三类 Go 进程、同工程和配套发布；内部协议未冻结。 |
| [0014](../../../../adr/0014-in-process-graph-plugin.md) | Graph 在后台进程内，仓内 DAG 复用上游、计算与派工分离。 |
| [0015](../../../../adr/0015-round-scoped-upstream-dags.md) | 获准轮次有限 DAG；原生 Loop 存在但首期不据此自主推进。 |
| [0016](../../../../adr/0016-transactional-background-work.md) | 业务事实／来源／幂等／待办／事件同事务，外部调用在事务后。 |
| [0017](../../../../adr/0017-atomic-attempt-resource-reservation.md) | Attempt／Worker／容量原子预留，环境准备后启动前再核验。 |
| [0018](../../../../adr/0018-provision-instance-after-first-draft.md) | 现行触发为首次会话消息／页面创建提交后准备，项目本身不启动。 |
| [0019](../../../../adr/0019-conversation-issue-separation.md) | 一会话可关联多个独立 Issue，不自动确认反向多对多。 |
| [0020](../../../../adr/0020-python-repository-analysis-plugin.md) | 可选受控 Python 建项前仓库分析，Go 保有业务归属与控制。 |
| [技术选型](../../../../current/technology-selection.md) | React／TS／Vite、Go、PostgreSQL、持久队列、REST＋SSE、对象存储。 |
| [架构 v1](../../../../current/architecture-design-v1.md) | 全文 1—13 节；已采用方向与建议的模块、具体协议分别处理。 |
| [Graph／Loop](../../../../current/graph-loop-design.md) | 全文 1—7 节；两个锁定提交及证据级别、跨仓与循环职责。 |
| [Skill](../../../../current/skill-engineering-design.md) | 全文含静态机制表；成员级分发不证明 Issue 版本隔离。 |
| [团队执行](../../../../current/team-execution-policy.md) | A1—A5；Worker 默认 1 是并发上限，故障先核查旧执行。 |
| [验证节点](../../../../current/verification-node-design.md) | 全文含必检、五类结果、证据附件与复核；完成不等于通过。 |
| [联调环境](../../../../current/integration-environment-design.md) | C1—C6；实际固定组合、配置及数据与 mock 范围。 |
| [draft PR](../../../../current/draft-pr-review-design.md) | E1—E5；首个初审候选可开 draft，人转正式与合并。 |
| [ChangeSet 旧入口](changeset-design-discussion.md) | 兼容历史链接，不恢复旧待确认清单。 |
| [ChangeSet 设计](../../../../current/changeset-design.md) | CS1—CS14；本轮修复、最终组合、部分合并与恢复归属。 |
| [ChangeSet 结构](../../../../current/changeset-structure.md) | 全文含 YAML 示例；逻辑分组不冻结表或 Schema。 |
| [项目配置](../../../../current/project-configuration-design.md) | J1—J3；账号／App／项目／本次范围共同核验，部分可用。 |

共 37 份本职责材料。AgentTeams 两轮验证原始 evidence／scripts 及页面／后端专项的覆盖由相应审计报告记账；本文不冒称已独立复现这些实验。

## 决定演进与证据级别

1. ADR-0001 的独占事项主房间和旧正式 Issue 启动时点，经 0018／0019 及明确后续补充演进为会话与 Issue 分离、首次会话消息或页面建项提交后异步准备。项目先保存、稳定归属、长期角色、独立 Attempt 保留。
2. ADR-0007 的 Graph／Loop 插件经 0014／0015 补充收窄为后台进程内跨仓协调及 Loop 策略；仓内当轮 DAG 的算法复用上游。不会因此建立独立 Graph 服务或完整 Go DAG 引擎。
3. ADR-0020 局部扩展 0010／0013：三类 Go 进程外允许可选 Python 分析子进程，业务与控制仍属于 Go；不恢复 Skill、不采用通用插件市场或微服务体系。
4. `accepted` 是设计采用。`eeaab64391ccaec9118e84977f538aefd40720d6` 与 `517caff9280242a00a4d4c06365352b9e41659c6` 是不同时间的锁定源码事实；09-09 实测与 09-10 增量有各自限定范围。组件 fake 传输、真实 HTTP／存储、真实 runtime／模型、RepoMesh 业务验收不能互相替代。源码未变只支持限定复用旧证据，不证明新版全套通过。

## 最小工程组织建议及依赖方向

使用仓库根单个 `go.mod`，三个 `cmd` 启动入口共享必要的内部运行辅助；前端独立 package 与锁文件放在 `web/`。仅创建有实际内容的目录，不把架构建议中的每个业务模块预建成空包。

| 进程 | 最终职责 | 本轮骨架可以承诺什么 |
| --- | --- | --- |
| Web | 页面请求、访问核验、持久输入、查询和 SSE。 | HTTP 生命周期、最小存活信息／静态入口；业务访问、写入与 SSE 尚未实现。 |
| 后台协调 | 持久任务、计划落实、资源协调、观察和恢复。 | 可构建且状态诚实的启动入口；没有队列消费、调度或 AgentTeams 接入。 |
| 受限主机执行 | 已登记环境操作及未来 Python 分析的生命周期。 | 可构建且状态诚实的启动入口；没有主机命令 API、Docker 操作或权限隔离验收。 |

当前依赖是 `cmd → 实际需要的 internal 运行代码 → Go 标准库`；浏览器只面向 Web。未来按用例出现再组织 `HTTP／后台入口 → 应用用例 → 领域规则` 与受控 Adapter，避免领域代码依赖浏览器、Controller DTO 或主机执行命令。业务跨模块事务由用例组织，不能因代码目录拆分而拆散创建原子范围。三类进程共同使用同一个 Go module 和发布版本，但各自启动、停止；发布包配套包含三个二进制、前端静态产物、配置样例与说明。配套发布不是高可用或已实现的内部通信。

Graph 未来由后台提供上游观察后调用，返回业务计算结果；不持有外部凭据、不派 Worker。本轮无需 Graph 空接口。Python 未来由 Go 固定和核权输入、保存独立分析作业和结果，主机执行职责管理受控启动／限额／停止核查／回收，通过待细化的版本化 JSON 交换材料；分析失败允许手动选仓，不触发 AgentTeams 准备。本轮不创建 Python 包、协议或执行实现。

## 保留未决事项

- 数据库表、队列库、领取／代次／恢复算法、容量口径、跨进程调用协议和凭据分配仍需场景设计。
- Manager MCP 名称和业务方向已采用；完整 Schema、可信来源及逻辑操作承接协议未编制。创建 REST／Issue SSE 已有设计基线，不能概括为所有 API 均未定。
- 多 Issue 会话的目标、房间／session、消息并发及恢复、内部仓库事项粒度尚未定；不造领域实体或冻结枚举填补空白。
- 上游受控派工、存储及工具旁路、完整换图、停机及旧写能力核查等仍待正式适配；最小工程构建不解决这些能力缺口。
- Skill 全模块、审批能力、直接编辑图继续暂缓；Issue、计划、调度、权限、GitHub、AgentTeams 与 Python 集成本轮均不实现。

后续独立复核应检查实际目录和启动语义是否符合以上边界，重点检查是否以“ready／success”掩盖未实现能力，以及是否误将 `validation/` 历史实验作为产品依赖或完成证据。

## 非主要实现者独立复核

本审计者没有编写或修改产品实现、配置、锁文件、构建脚本及其测试；实施由主 agent 完成。本次读取了三个 `cmd` 入口、`internal/buildinfo`、`internal/web` 与测试、前端源文件及配置、锁文件根依赖、配置示例、忽略规则、打包脚本、根 README 和开发说明，并独立执行本地验证。未启动外部服务或旧实验。

### 发现、修复与关闭

发现 1 项生命周期缺陷：原 Web 在另一个 goroutine 调用 `Shutdown`，但 `Serve` 在监听器关闭后立即返回，`Run/main` 随即退出，不能保证在途 HTTP 请求收尾。主 agent 修复为取消后同步等待 `Shutdown` 完成，再核对 `Serve` 结果；超时显式关闭并返回错误。新增 `TestShutdownWaitsForInflightRequest` 使用真实本地 HTTP 请求，核查取消期间进程控制函数不能提前返回。

本审计者重新读取修复代码并执行 `go test ./...` 和 `go vet ./...`，均通过；该缺陷已关闭。复核未要求新增业务抽象或接口。

最终包核对时另发现 README 的直接启动命令仍指向修复前保留的 `0.1.0-scaffold` 包；主 agent 先将 README 改为 `0.1.0-scaffold-final`，再构建新包。本审计者核对根 README、包内 `SOURCE-README.md` 字节一致，启动路径与最终清单一致，该文档一致性项已关闭。Graph 专题的“当前无 Go 产品实现”也已改为骨架已存在、Graph／业务未实现，保留历史实验原义。

### 独立执行结果

| 检查 | 结果及范围 |
| --- | --- |
| `go test ./...`、`go vet ./...` | 修复前后均分别检查；最终测试包含在途请求收尾回归，通过。命令级 `GOFLAGS=-buildvcs=false` 避免当前工作区所有者导致的 Git 元数据查询失败，未改全局 Git 设置。 |
| `go list ./...` | 只列三个 `cmd`、`internal/buildinfo`、`internal/web` 共 5 个产品包，不包含历史实验。 |
| `npm --prefix web run typecheck` | 独立通过；未在主 agent 安装／打包期间并行改动锁文件或 `node_modules`。 |
| `0.1.0-scaffold` 初次包清单 | 所有 `release.json` 记录的产物 SHA-256 匹配；三个二进制 `--version` 均为该版本且退出 0。此包早于生命周期修复，保留为构建历史，不作为最终修复交付包。 |
| `0.1.0-scaffold.1` 修复包清单 | 10 份产物 SHA-256 均匹配；三个二进制均输出 `0.1.0-scaffold.1` 并退出 0。随后发现根 README 直接启动路径仍指旧包，已要求同步。 |
| `0.1.0-scaffold-final` 最终包清单 | 独立核对 10 份产物 SHA-256 均匹配；三个二进制均输出 `0.1.0-scaffold-final` 并退出 0。根 README、包内源 README 和启动说明都对应最终包，未覆盖中间产物。 |
| 协调／主机执行默认启动 | 分别输出明确的 `not implemented`，退出码 1，无监听或业务执行。 |
| Web 缺少静态目录 | 报缺少前端 `index.html`，退出码 1，没有伪造可用状态。 |
| 初次包实际本地 Web | 使用仓库根命令和独立 loopback 端口 18879 启动；首页、实际 JS 与 CSS 均 200；`healthz` 为 200 且 `businessReady=false`；`readyz` 为 503；`/api`、`/api/v1/issues` 及其 POST 为 404；未知页和 `/assets/` 目录为 404。完成后 Ctrl+C 退出 0。 |
| 工程与发布边界 | 无业务空包、Graph 服务、Python／Skill 实现或数据库 Schema；发布脚本只复制明确产品文件，不复制 `validation/`，也不启动服务。 |
| 忽略规则差异 | 本轮只增加 TS 构建缓存、本地浏览器工具目录及 `output/playwright/` 忽略；原有 `*.log`、`dist/` 等规则已存在，本轮没有扩展为全局忽略历史证据。 |
| 最终文档一致性及本地链接 | 复核 HANDOFF、两个导航、根 README、开发说明、验收记录及本轮审计记录共 9 份文档的相对文件链接，排除代码围栏后缺失 0；技术、架构、Graph 及两份角色接手说明均区分新骨架授权与历史设计。检查未访问外部服务，也不声称远端链接通过。 |

初次后台启动验证命令曾被自动审批以 `blocked by policy` 拒绝，未产生执行结果。随后改用工具托管的前台本地 Web 会话并通过 Ctrl+C 停止，完成上表实际 HTTP 核查；没有为此申请额外权限或绕过外部服务限制。

最终修复包及 HANDOFF／导航一致性已核对完成，独立复核发现均已关闭，未发现本轮范围内尚未关闭的实现问题。最终交付只采用 `0.1.0-scaffold-final`，其余包保留为本轮构建历史。以上只证明最小骨架的构建、路由与生命周期；不证明业务、运行隔离、恢复或 AgentTeams 集成验收。浏览器桌面／移动走查和旧材料散列保留检查由主 agent 记录，本审计者没有将它们冒称为独立重复执行。
