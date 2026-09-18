# ChangeSet 逻辑结构说明

> **物理承载替代说明（2026-09-15 更新）：** 本文的逻辑契约与候选状态不变。物理表承载自 2026-09-15 起以 [Go 版数据库重构方案](../RepoMesh_Go版数据库重构方案.html) 的 44 张目标表为准，对应接口见 [API 设计](api-design.md)；此前的 B05-B11 物理合表已归档到 [历史目录](../archive/2026-09-15-api-database-catalog/README.md)。

依据：[已确认设计](changeset-design.md)及 [ADR-0008](../adr/0008-changeset-attribution-and-history.md)。下面解释这些规则可以怎样组织成可查询的记录；分组、字段名、修订号表示及示例是逻辑结构建议，不是已冻结的数据库表、JSON Schema 或现有接口。

## 三层结构

ChangeSet 主记录提供稳定归属和关联入口；既有 Candidate、Delivery Combination、Task／Attempt 等对象保留各自记录；摘要依据这些记录生成，供 Agent 先看概览，再按需定位明细。

```text
RepoMesh Issue
    |
    +-- ChangeSet 主记录（稳定身份）
           |
           +-- 归属与关联
           |      项目 / Issue / 涉及仓库 / 外部 GitHub issue
           |      相关 ChangeSet：依赖、后续变更、恢复关系
           |
           +-- 工作依据引用
           |      计划版本 / 验收依据 / 许可记录
           |
           +-- Candidate 记录
           |      产物与固定版本 / Task / Attempt / 提交者
           |      初审、接受或未接受的记录 / 修复来源
           |
           +-- Delivery Combination 记录
           |      每仓固定 SHA + 候选或未修改基线引用
           |      Manager 选定该组合的依据
           |
           +-- 验证记录与证据
           |      组合 / 验证 Attempt / 计划与验收版本 / Skill
           |      实际环境清单 / mock 数据版本
           |      检查结果 / 复核 / 证据适用性与来源
           |
           +-- 对外交付记录
           |      PR / head 与合并观察 / 必需检查
           |      部署及数据恢复的独立记录
           |
           +-- 判断、决定与操作历史
           |      谁、何时、根据什么作出判断或决定
           |      操作标识 / 目标 / 输入 / 回执 / 核查
           |
           +-- 摘要视图（由关联记录汇总）
                  执行进展 / 验证结论 / 交付进展
                  未解决事项 / 来源引用 / 观察时间
```

树表示查询关联，不表示把所有对象嵌入一个大文件，也不表示存在嵌套 ChangeSet。相关恢复事项有自己的 Issue 和 ChangeSet，通过关系引用原变更。引用本身不扩大查看权限。

## 主记录与明细记录

| 记录 | 建议表达的最小信息 |
| --- | --- |
| ChangeSet 主记录 | 稳定 ID、Project 引用、RepoMesh Issue 引用、建立时间／来源，以及用于并发校验的记录修订信息。 |
| 仓库与外部关联 | 仓库身份、目标分支、固定基线引用；GitHub issue 的仓库及编号／外部身份、来源或跟踪关系。 |
| Candidate | 稳定候选身份、来源 Task／Attempt、产物引用、提交者和时间、相关初审记录。代码产物有固定 SHA；其他产物使用对应版本／摘要，不强制伪造 Git commit。 |
| Delivery Combination | 稳定组合身份、每个参与仓库的固定 SHA、对应候选或基线引用、选定者与依据。成员版本确定后保留原组合。 |
| 验证记录 | 指向组合及验证 Attempt，并绑定计划、验收、Skill、实际环境和数据记录；关联逐项结论、复核和证据适用性判断。 |
| 证据 | 产物位置及完整性信息、来源执行／检查、实际对象依据、产生时间、可用性及归档信息。正文按需读取。 |
| PR 与交付观察 | 仓库和 PR 身份、实际观察到的 head／base／合并结果及时间、检查来源、替代 PR 关系；部署和数据恢复另记对象与结果。 |
| 判断与决定 | 内容、作出者、时间、依据及适用范围／版本；引用既有审批和计划许可记录，不建立第二套授权机制。 |
| 操作记录 | 逻辑操作 ID、动作、目标、输入摘要、预期目标版本、发起者、进展／结果、远端对象引用和核查记录。请求重试与 Worker 的业务 Attempt 分开表达。 |
| ChangeSet 关系 | 相关 Issue／ChangeSet 的身份、依赖／后续变更／恢复关系及来源依据；不建立父子嵌套执行体系。 |
| 摘要视图 | 分别汇总执行、验证和交付进展，列出阻塞或缺失、相关组合、源记录引用及汇总依据的时间／修订信息。 |

表格描述查询时需要表达的信息，不要求每个分组各建一张表。候选、检查、审批、执行日志可以仍由各自的记录负责，ChangeSet 通过稳定引用连接。

## 版本怎样区分

- ChangeSet 身份贯穿本次变更。建议的记录修订信息只服务于并发校验和摘要定位，不是另一套代码候选版本。
- Plan Version 说明如何组织工作，许可记录说明哪些工作已经获准；其身份和生效记录沿用现有设计。
- Candidate 固定本次提交的产物。Delivery Combination 固定被验证的仓库版本集合。
- 仓库版本变化建立新组合；同一组合采用不同验收、Skill、环境或数据上下文时，新建相应验证记录，重新判断证据。补证与证据复用保留来源和适用性记录。
- PR 的新 head、实际合并结果和历史更正作为新事实记录保留，摘要据此更新；不回写旧组合或抹去旧证据。

## 支付筛选示例

以下 ID 和 F1／B1／B2 均为说明用代号；实际代码版本使用固定完整 commit SHA。示例假设各候选已通过仓库内部初审，B1 随后在跨仓验证中暴露缺陷。字段名称尚未冻结。

```yaml
change_set:
  id: CS-001
  project_ref: P-01
  issue_ref: RM-42
  repository_refs: [frontend, backend]
  candidate_refs: [F1, B1, B2]
  combination_refs: [C1, C2]
  verification_refs: [V1, V2]
  pr_refs: [PR-F, PR-B]
  operation_refs: [OP-create-PR-F, OP-create-PR-B]
  related_change_set_refs: []

combinations:
  C1: {frontend: F1, backend: B1}
  C2: {frontend: F1, backend: B2}

verification_records:
  V1: {combination_ref: C1, result: 业务缺陷, evidence_refs: [E1]}
  V2: {combination_ref: C2, result: 通过, evidence_refs: [E2]}

summary_view:
  execution_progress: 本轮执行已结束
  verification_outcome: C2 的约定必检项有效通过且复核完成
  delivery_progress: draft PR 尚未合并
  source_refs: [V2, PR-F, PR-B]
```

示例省略计划、环境、数据及复核明细，但实际验证记录必须关联这些依据。V2 通过不代表 PR 已合并或整个 Issue 已完成。F1 可以同时出现在 C1 和 C2 中，B2 不覆盖 B1，E1 不被静默改写成对 C2 的证据。

Agent 要了解 B2 的来由时，沿 B2 的来源记录找到修复 Task／Attempt，再定位触发修复的 C1 检查、E1 和当时验收依据。要判断创建 PR 的请求是否已经成功，则查看对应操作记录及远端核查，不根据摘要猜测。

## 职责与实现边界

Agent 提交产物和证据，并按职责提出判断或工作安排；RepoMesh 记录可核查事实、执行有效权限和版本校验，并生成有来源的摘要。外部结果未知时保留未知并核查；查询历史不会自行启动操作或激活旧计划。

本结构解释不决定执行模式或页面刷新规则；I 当前全自动与审批能力延后的决定见 [ADR-0006](../adr/0006-manager-entry-modes-and-skill-driven-execution.md)。Skill 工程 H1—H15 的方向见 [ADR-0009](../adr/0009-skill-engineering-deferred.md)，整个模块待定占位、暂不开发。存储、事务、查询工具和 AgentTeams／Graph 插件适配仍待实现前细化与运行验证。
