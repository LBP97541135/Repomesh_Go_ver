# RepoMesh 历史归档

当前设计请从 [HANDOFF](../current/HANDOFF.md)与[现行文档索引](../current/README.md)进入。本目录仅供溯源，历史文件中的 accepted、旧问题、旧 prompt 和“当前”表述均属于当时语境，不作为现行决定或操作指令。

## 2026-09-15：删除 B00-B11 API 与数据库目录

[删除说明与一并归档的合表专题](2026-09-15-api-database-catalog/README.md)记录 `docs/api-database/` 23 个文件的删除、B05-B11 物理合表专题的归档，以及仍留在 `development/` 的制作证据。现行数据库方案是 [Go 版数据库重构方案](../RepoMesh_Go版数据库重构方案.html)，接口见 [API 设计](../current/api-design.md)。

## 2026-09-13：计划目录与管理文档整理

[迁移映射、旧任务提示与原始内容](2026-09-13-plan-organization/README.md)记录 `docs/plan/` 的建立、四份计划迁移、B03 与 B04 旧提示归档，以及交接和施工计划的精简。当前计划从[计划导航](../plan/README.md)进入；业务状态以[当前交接](../current/HANDOFF.md)为准。

## 2026-09-12：开发阅读与活跃目录整理

[本次归档清单、原始字节与校验](2026-09-12-development-preparation/README.md)保存 19 份历史文档、4 份被替代原型，以及 6 份入口整理前快照。当前交接与索引已精简，开发从[阅读与行动指南](../plan/DEVELOPMENT-START.md)开始。全部 ADR、唯一契约、关键候选与必要证据入口保留；归档不是撤销原采用决定。

## 2026-09-11：后端设计换会话交接

以下为历史整理记录；当前开发交接统一从[开发指南](../plan/DEVELOPMENT-START.md)进入。

[旧后端交接全文与校验记录](2026-09-11-backend-design-handoff/README.md)保留模型设置r3采用后的累积交接；正文仅增加历史提示并重定位相对链接。现行入口为[新后端交接](2026-09-12-development-preparation/docs/current/HANDOFF-BACKEND-DESIGN-2026-09-11.md)，原固定路径保留导航。专题、原型与双方通信日志不随本次归档搬走。

## 2026-09-09：文档评审与交接整理

[整理前交接快照](2026-09-09-before-document-review/README.md)保留本次压缩整理前的 HANDOFF 与两份角色 Prompt 原文。评审修复入口关系、实例触发及采用状态的时间漂移，并增加 ADR 演进与现行主题导航；详细发现见[评审报告](2026-09-12-development-preparation/docs/current/document-review-2026-09-09.md)。

## 2026-09-08：I／J 收口后的文档整理

本轮重写当前 handoff、新会话 prompt 与导航，保存修改前快照，归档旧版 PRD 和原型，并修正文档中滞后的阶段表述；未新增产品决定。

| 资料 | 内容 |
| --- | --- |
| [整理前快照](2026-09-08-design-consolidation/README.md) | CONTEXT、导航、全部现行 ADR 和专题，含旧 handoff／prompt；清单记录原始及归档文件摘要。 |
| [旧版 PRD 与原型](legacy-product-design/ARCHIVE-NOTICE.md) | 原 docs/old_ver 的 30 个文件原样迁移，旧路径只留 README 入口。 |
| [当前 handoff](../current/HANDOFF.md) | 现行接手入口；不沿用本次归档时的阶段状态。 |
| [当前页面 prompt](2026-09-12-development-preparation/docs/current/NEXT-SESSION-PROMPT.md) | 当前角色阅读清单与接续要求；原清单仅代表归档时范围。 |

## 较早的交接整理与已收口讨论

| 历史资料 | 当前用途与替代入口 |
| --- | --- |
| [前一轮交接整理快照](2026-09-08-handoff-before-consolidation/README.md) | 当时 A—E、CS1—CS14 已确认，H—K 尚待讨论；这不是本轮最新进度。 |
| [CS1—CS14 确认前清单](2026-09-08-changeset-cs1-cs14-proposal.md) | 原问题和建议；现行规则在 [ChangeSet 设计](../current/changeset-design.md)。 |
| [ChangeSet 初始提案](2026-09-08-changeset-initial-proposal.md) | 初始归属讨论；现行决定为 [ADR-0008](../adr/0008-changeset-attribution-and-history.md)。 |
| [单仓直达 Leader 的旧设计](2026-09-08-leader-direct-issue-routing.md) | 已淘汰的事项入口；现行所有新 Issue 由 Manager 接收并全程统筹。 |
| [早期 handoff](HANDOFF-2026-09-07.md) | 历史上下文，不再作为接手入口。 |

## Manager 与 Skill 方向调整前快照

以下保存较早的模式、角色入口及讨论历史；不恢复其中的 YOLO 人工等待或全部业务解释必须人工确认等已淘汰规则。

| 历史资料 | 内容 |
| --- | --- |
| [v2 handoff](2026-09-08-before-manager-skill/docs/HANDOFF-2026-09-07-v2.md) | 早期交接。 |
| [v3 handoff](2026-09-08-before-manager-skill/docs/HANDOFF-2026-09-07-v3.md) | 累积补充与旧开场。 |
| [旧接手 prompt](2026-09-08-before-manager-skill/docs/NEXT-SESSION-PROMPT-2026-09-07.md) | 当时的未决清单与门禁。 |
| [方向修改讨论稿](2026-09-08-before-manager-skill/docs/manager-entry-and-skill-orchestration-proposal.md) | 现行方向已进入 ADR-0006 及相关专题。 |
| [修改前 ADR-0001](2026-09-08-before-manager-skill/docs/adr/0001-agentteams-issue-concurrency-and-isolation.md) | 团队、入口和隔离历史。 |
| [修改前 ADR-0002](2026-09-08-before-manager-skill/docs/adr/0002-github-app-authorization-and-draft-pr-delivery.md) | 授权讨论历史。 |
| [修改前 ADR-0003](2026-09-08-before-manager-skill/docs/adr/0003-plan-change-authorization-and-activation.md) | 旧模式与计划规则。 |
| [修改前 ADR-0004](2026-09-08-before-manager-skill/docs/adr/0004-acceptance-rule-clarification.md) | 旧业务解释权限。 |
| [修改前 ADR-0005](2026-09-08-before-manager-skill/docs/adr/0005-optional-project-verification-group.md) | 验证小组的早期讨论。 |
| [修改前验证节点](2026-09-08-before-manager-skill/docs/verification-node-design.md) | 早期检查设计。 |
| [修改前术语表](2026-09-08-before-manager-skill/CONTEXT.md) | 旧角色和模式定义。 |

## 参考材料

[Cloud Agent PRD](reference/Cloud-Agent平台产品PRD.md)只作历史能力参考，不是当前 PRD 或开发范围。AgentTeams 静态调研继续保存在 [原调研目录](../agentteams-survey-2026-09-07/agentteams-survey.md)，Cursor 报告保留在界面参考位置；两者均不代表 RepoMesh 已完成运行验收。

归档未删除历史正文或证据；必要的历史提示和相对链接调整均为文档导航整理。旧 Windows 绝对路径与历史 prompt 内的指令只用于溯源。
