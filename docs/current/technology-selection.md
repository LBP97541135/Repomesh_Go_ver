# RepoMesh 技术选型（已确认）

基础工程阶段补充（2026-09-10）：本轮用户已授权基础代码、配置和必要依赖，现有最小 Go／React 工程见[开发说明](development-scaffold.md)。下文选型与原确认记录保留；其中“安装、编码未获授权”仅描述历史任务，不覆盖本轮新授权。数据库、React Flow 和业务集成尚未实现。

原选型确认：2026-09-08；导航修订：2026-09-09。整组技术方案已收口，正式决定及取舍见 [ADR-0010](../adr/0010-technology-stack-and-modular-monolith.md)。当前页面／接口和后端分别接续设计，下一步统一见 [HANDOFF](HANDOFF.md)，本专题不指定接手角色。尚未开始生产实现或运行验收。

## 采用方案

| 层次 | 已确认方案 |
| --- | --- |
| 前端 | React＋TypeScript＋Vite。 |
| 图展示 | React Flow；具体布局算法后续细化。 |
| 自有后端 | Go。 |
| 业务状态存储 | PostgreSQL。 |
| 后台任务 | PostgreSQL 持久队列，负责可靠触发和重试。 |
| 前后端通信 | REST 查询／操作＋SSE 推送。 |
| 证据和产物 | 对象存储，数据库保存索引。 |
| 后端组织 | 模块化单体，一个 Go 后端工程按业务领域划分模块；后台任务可独立运行。 |
| 协作与执行基础 | 沿用每项目独立 AgentTeams 实例、首期单机容器和独立 Attempt 等既定设计。 |

Go 的采用限定于 RepoMesh 自有后端，不要求改写 AgentTeams 或其他执行工具。数据库队列、Graph／Loop 插件和 RepoMesh 执行控制各自承担既定职责；队列完成、任务结束和业务验收分别表达。

## 接续架构设计

后续用户选择在 Issue 创建时调用 Python 仓库分析插件，已按 [ADR-0020](../adr/0020-python-repository-analysis-plugin.md)局部扩展 Go 技术范围：业务、权限、作业和记录仍由 Go 管理，解析与候选分析运行于受控 Python 包。首期规则／关键词路径不依赖向量数据库；历史决策分析和向量存储尚未纳入实施，详见[专题](issue-creation-repository-analysis.md)。

以下保留选型之后的架构进展；具体模块拆分尚未形成新决定。下一阶段已转入页面与接口设计，按场景需要继续细化未决架构问题，见[接手 prompt](../archive/2026-09-12-development-preparation/docs/current/NEXT-SESSION-PROMPT.md)。

后续已确认的架构方向分别见 [ADR-0011](../adr/0011-agentteams-controlled-integration.md) 的受控接入、[ADR-0012](../adr/0012-issue-scoped-upstream-projects.md) 的 Issue 上游映射，以及 [ADR-0013](../adr/0013-web-coordinator-host-executor-processes.md) 的 Web／后台协调／受限主机执行三类进程。同一 Go 工程、配套发布的安排已采用，领域模块和具体协议继续细化。

Graph 插件在后台协调进程内按独立模块运行、随后端发布的方式已采用，见 [ADR-0014](../adr/0014-in-process-graph-plugin.md)。本次不选定动态插件机制或第三方框架，具体接口与上游任务映射仍需细化。

后续还确认了 [ADR-0015](../adr/0015-round-scoped-upstream-dags.md) 的按获准轮次下发有限 DAG、[ADR-0016](../adr/0016-transactional-background-work.md) 的业务与后台待办同事务保存，以及 [ADR-0017](../adr/0017-atomic-attempt-resource-reservation.md) 的 Attempt／Worker／容量统一预留及启动前再核验。具体协议、字段和恢复算法仍未冻结。

版本、HTTP 框架、数据访问工具、UI 组件库、图布局算法、队列库和对象存储产品留待实现细化。本次收口不将未明确点名的库或产品视为已采用，也不重新打开上述技术方向选择。性能、容量和运行恢复仍需实现后验证。

## 确认记录

用户先回复“就用Go吧”，随后回复“OK其他选型我也认，你记录。选型做完了。我觉得现在要讨论的是架构设计。”本专题和 ADR-0010 据此记录全部选型；安装、编码、部署、远端操作及自动新建任务仍未获授权。
