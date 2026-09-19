# RepoMesh ADR 索引与决策演进

更新：2026-09-15。ADR 保存“为什么作出决定”及确认来源；详细规则见[现行专题](../current/README.md)，当前工作状态见 [HANDOFF](../current/HANDOFF.md)。下列决定均为设计，不代表产品实现或上游运行验收。

## 阅读状态

2026-09-12 的[设计审查修订](../current/design-readiness-revisions.md)补充 Key 恢复、Issue 配置归属和页面契约，并澄清 Skill 证据适用范围。新协议和绑定时点仍为候选，没有替代本索引的已采用架构决定。真实运行的未决协议另见[接入门槛](../current/execution-integration-gates.md)，不能把架构方向已采用等同于运行接口已闭合。

`accepted` 只确认 ADR 列明的范围。后继决定可以局部替代旧条款，旧 ADR 的其余部分仍然有效；不能因为编号较新就覆盖所有旧约束。原 `date` 是原决定日期，`updated` 是文档修订日期。Skill 工程的 `implementation: deferred` 与设计方向已同意并不冲突。

## 决策目录

| ADR | 原决定日期 | 主题 | 当前适用范围与后续关系 |
| --- | --- | --- | --- |
| [0001](0001-agentteams-issue-concurrency-and-isolation.md) | 2026-09-07 | 团队、Issue 并行与隔离 | 项目实例、长期团队和 Attempt 隔离有效；统一 Manager 入口见 0006，会话关系见 0019，启动触发见 0018 后续补充。 |
| [0002](0002-github-app-authorization-and-draft-pr-delivery.md) | 2026-09-07 | GitHub App 授权与 draft PR | 有效权限交集、受控交付和人工合并；实例准备不再以首条正式 Issue 为唯一触发。 |
| [0003](0003-plan-change-authorization-and-activation.md) | 2026-09-07 | 计划许可、应用与生效 | 当前 YOLO，后续审批规则保留；上游映射见 0012，获准轮次推进见 0015。 |
| [0004](0004-acceptance-rule-clarification.md) | 2026-09-07 | 缺失业务规则的解释 | 按自动化档位决定，解释与验收通过分开。 |
| [0005](0005-optional-project-verification-group.md) | 2026-09-07 | 可选项目验证小组 | 验证小组可选，独立验证与权限边界始终保留。 |
| [0006](0006-manager-entry-modes-and-skill-driven-execution.md) | 2026-09-08 | Manager、模式与 Skill 分工 | Manager 统一对人，当前 YOLO；Skill 工程按 0009 延后，旧一项一主会话关系按 0019 替代。 |
| [0007](0007-graph-loop-plugin.md) | 2026-09-08 | Graph／Loop 行为 | 流程和循环约束有效；0014／0015 的 09-09 补充明确复用仓内原生 DAG，RepoMesh 管跨仓及 Loop。 |
| [0008](0008-changeset-attribution-and-history.md) | 2026-09-08 | ChangeSet 归属与历史 | 默认主 ChangeSet、CS1—CS14 保留；创建原子范围按 0016 后续补充，启动与会话关系按 0018／0019。 |
| [0009](0009-skill-engineering-deferred.md) | 2026-09-08 | Skill 工程延后 | H1—H15 方向已同意；整个模块暂不开发，后续设计授权未恢复实施。 |
| [0010](0010-technology-stack-and-modular-monolith.md) | 2026-09-08 | 技术栈与模块化单体 | 选型已收口；0013 细化三类进程，0020 局部增加 Python 分析包，业务与控制仍由 Go 负责。 |
| [0011](0011-agentteams-controlled-integration.md) | 2026-09-08 | AgentTeams 受控接入 | 工具和实际执行由 RepoMesh 约束，必要时维护小范围上游补丁；具体适配待验证。 |
| [0012](0012-issue-scoped-upstream-projects.md) | 2026-09-08 | Issue 仓库委派与上游 Project | 各 Issue 上游工作隔离，多目标应用核查；0019 调整用户会话关系，0015 细化有限 DAG 轮次。 |
| [0013](0013-web-coordinator-host-executor-processes.md) | 2026-09-08 | Web、后台协调、主机执行 | 同一 Go 工程、配套发布；0020 明确额外允许受控 Python 分析进程及对应生命周期操作。 |
| [0014](0014-in-process-graph-plugin.md) | 2026-09-08 | 进程内 Graph 插件 | 09-09 收窄为跨仓协调与循环策略，仓内 DAG 算法复用上游；进程内运行、后台派工及持久化不变。 |
| [0015](0015-round-scoped-upstream-dags.md) | 2026-09-08 | 按获准轮次复用上游有限 DAG | 09-09 明确按最新确定源码复用原生 DAG；RepoMesh 控制后续轮次，限额内推进不另建业务 Plan Version。 |
| [0016](0016-transactional-background-work.md) | 2026-09-08 | 业务与待办同事务 | 本地提交后外部执行，未知先核查；后续补充明确创建来源、快照和幂等结果等原子范围。 |
| [0017](0017-atomic-attempt-resource-reservation.md) | 2026-09-08 | Attempt 与资源统一预留 | 短事务预留，环境在事务外准备，启动前再次核验；超时不等于资源释放。 |
| [0018](0018-provision-instance-after-first-draft.md) | 2026-09-08 | 项目先保存、按需准备实例 | 原 Draft Issue 时序保留历史；09-09 后续补充采用会话首条消息／页面建项提交后的异步准备。 |
| [0019](0019-conversation-issue-separation.md) | 2026-09-09 | 会话与独立 Issue 分离 | 一会话多独立事项、仓库事项及房间导航；原未包含项须结合 0016／0018 后续补充和创建契约读取。 |
| [0020](0020-python-repository-analysis-plugin.md) | 2026-09-09 | 建项前 Python 仓库分析 | 旧项目调查后的用户选择；可选按钮、受控 Python 包与 Go 作业／来源，局部扩展 0010／0013。分析不触发实例准备，历史分析与向量库不作为首期依赖。 |
| [0021](0021-pgvector-rag-storage-plugin.md) | 2026-09-14 | pgvector RAG 存储插件 | 收口决策链语义检索的存储选型与迁移路径（维度契约、HNSW 余弦、SQL 下推、无扩展降级）。Python 控制台已验证；Go 迁移 0008 已实施（见 0023），实施批次与 0020「向量库不作首期依赖」的时序说明不冲突。 |
| [0022](0022-human-checkpoint-resolution-governance.md) | 2026-09-14 | 人工检查点决议治理 | 唯一落库入口、政策漂移存量单管理员兜底、聊天批复二选一定位、策略入口常驻现场。依据 2026-09-14 生产库实证（26 单锁死）；Python 已修复，Go 版 HITL 链路设计第一输入。 |
| [0023](0023-decision-chain-native-module-and-pgvector.md) | 2026-09-15 | 历史决策链原生模块与 pgvector 落地 | Go 原生重建决策链（圈定确认为首生产者、记录即投影、前端开关落库 feature_settings）；实施 0021 的存储选型：迁移 0008 落 vector(1024) + HNSW + JSON 兜底双写，语义检索按 model 过滤，开关与 API 见接口总册 I 板块。 |
| [0024](0024-local-observation-workbench.md) | 2026-09-19 | 全本地观测与评测 | 独立本地工作台替代云实验 Launcher 作为默认入口；观测与规则验收本机运行，模型评审允许 DeepSeek API；保留不可变证据与既有业务边界。 |

## 三条主要演进链

| 主题 | 先后演进 | 当前结论 |
| --- | --- | --- |
| 工作入口与会话 | 0001 的独立 Issue 主房间 → 0006 的 Manager 统一入口 → 0019 的会话／Issue 分离 | 人通过 Manager 沟通；一个会话可关联多项，各 Issue 的计划、执行和交付独立。反向多主会话、Leader 房间共享粒度仍待设计。 |
| 实例准备时点 | 0001 首条正式 Issue → 0018 首条 Draft Issue／消息 → 0019 分离对象、当时暂存触发 → 0018 的 09-09 持续授权补充 | 项目创建不启动；首条会话消息或页面建项提交后异步准备／恢复。创建回执不证明房间就绪或 Manager 接收。 |
| 计划与执行映射 | 0003 许可／生效分开 → 0012 按 Issue 仓库委派隔离上游 → 0014 进程内协调／后台派工 → 0015 逐轮有限 DAG → 09-09 明确复用原生仓内 DAG | 上游提供仓内建图与候选就绪，RepoMesh 管跨仓、结果采纳及循环；换图仍需受控收敛和读回，不直接 pause→replan。 |

## ADR 之外的后续确认

2026-09-09 用户要求按最新版本复用上游 DAG 并清理 Graph／Loop 文档，已定点补充 ADR-0014／0015，不新增 ADR 编号。[Graph／Loop 现行专题](../current/graph-loop-design.md)集中维护职责、确定源码与 release 的区别、原生能力、验证证据及接入缺口；不将复用方向解释为受控适配已经实现。

2026-09-09 的[持续设计授权及采用清单](../current/design-delegation.md)记录 P1—P4、双入口、会话关联和 REST 基线；[创建契约 v1](../current/issue-page-create-api-contract.md)给出已采用的具体路径、字段、错误和 Issue SSE。它们细化 ADR-0016／0018／0019 的相关范围，不把所有后端专项或其他接口一并冻结，也不改变 Skill 暂缓、当前 YOLO、人工 Git 合并和当前权限约束。

完整 MCP Schema、可信来源操作协议、消息及运行恢复等缺口见 [HANDOFF](../current/HANDOFF.md)；本次修订证据与未决事项见[文档评审](../archive/2026-09-12-development-preparation/docs/current/document-review-2026-09-09.md)。
