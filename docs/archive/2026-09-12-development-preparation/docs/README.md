# RepoMesh 文档导航

更新：2026-09-10，新增基础工程阶段。现已建立最小 Go 三入口及 React／TypeScript／Vite 骨架；业务仍处于设计阶段，尚无业务或 AgentTeams 集成验收。既有本地内存原型保留为设计参考。

| 需要了解什么 | 阅读入口 |
| --- | --- |
| 当前已采用什么、还缺什么 | [当前交接](current/HANDOFF.md) |
| 如何安装、构建、启动工程骨架 | [根 README](../../../../README.md) · [开发说明](../../../current/development-scaffold.md) · [验收记录](../../../current/scaffold-verification.md) |
| 按主题寻找设计、契约或历史材料 | [现行文档索引](current/README.md) |
| 查看后来制作的三个页面设计原型 | [页面原型目录](prototypes/README.md)：草稿与房间、项目优先、会话与独立 Issue。 |
| 决策如何演进、哪些旧规则被替代 | [ADR 索引与替代关系](../../../adr/README.md) |
| Graph／Loop 怎么复用 AgentTeams 原生 DAG | [Graph／Loop 现行专题](../../../current/graph-loop-design.md) |
| 创建 Issue 时如何分析关联仓库 | [仓库分析按钮与 Python 插件](../../../current/issue-creation-repository-analysis.md) |
| 本次文档评审发现及维护结果 | [2026-09-09 文档评审](current/document-review-2026-09-09.md) |
| 项目、会话、Issue、Team 等术语 | [领域语言](../../../../CONTEXT.md) |
| 接续页面／接口或后端设计 | [页面／接口 handoff](current/HANDOFF-PAGE-API-DESIGN.md) · [后端 handoff](current/HANDOFF-BACKEND-DESIGN.md)：缺口、优先级、交付结果与开工条件。 |
| 核对授权及设计协作原文 | [持续设计授权](../../../current/design-delegation.md) · [通信约定与日志入口](current/design-communication.md) |
| 查询旧提案、原型和整理前快照 | [历史归档](../../README.md) |

当前入口模型由 [ADR-0019](../../../adr/0019-conversation-issue-separation.md) 定义会话与独立 Issue 的关系；实例准备时点按 [ADR-0018 的后续补充](../../../adr/0018-provision-instance-after-first-draft.md)；双入口创建及具体 REST／Issue SSE 按[创建契约](../../../current/issue-page-create-api-contract.md)。技术与架构已确认到 ADR-0020 所列范围，新增建项前 Python 仓库分析；其他模块和运行适配仍在细化。

文档中的“已采用／accepted”表示设计决定。Skill 工程整体暂缓，产品当前使用 YOLO；后续审批设计已记录。完整状态由交接页统一维护，导航不再复制各阶段进度清单。
