# RepoMesh 现行专题索引

本页索引业务设计、接口、开发说明与采用记录。当前完成度见[交接](HANDOFF.md)，施工顺序、前置设计分工和验证清单见[计划导航](../plan/README.md)。首次接手按[全局阅读指南](AGENT-READING-GUIDE.md)建立上下文。

采用状态以具体章节和替代关系为准。旧协作与已替代稿从[历史归档](../archive/README.md)追溯，不作为当前任务指令。

## 开始与当前基线

| 文档 | 用途与范围 |
| --- | --- |
| [Agent 全局阅读指南](AGENT-READING-GUIDE.md) | 首次接手的阅读顺序、产品全貌、架构边界、实现与证据核对，以及按任务深入的入口。 |
| [B04 验收报告](../development/2026-09-13-b04-acceptance-01/README.md) | 已合入 `main` 的 D01—D04／U04.1—U04.4 验收；`INTEGRATED_LOCAL_VERIFIED`，非整批 VERIFIED。 |
| [B05/B06 五项设计收口](../development/2026-09-14-b05-b06-design-closeout-01/README.md) | 固定测试预览与handler、unknown关闭、逐路径锁序与共同owner account边界、schema2 execution及窗口scope设计；产品未实施，运行未验收，其余候选未采用。 |
| [B04 收口 01](../development/2026-09-13-b04-closeout-01/README.md)、[独立复核](../development/2026-09-13-b04-closeout-01/FINAL-INDEPENDENT-REVIEW.md) | B04 INTEGRATED_LOCAL_VERIFIED：夹具浏览器、B03 回归、r2 包；非整批 VERIFIED。 |
| [B04 采用记录](b04-model-sources-adoption.md) | 已采用 D01—D04 与 U04.1—U04.4。不采用 D05—D08。 |
| [B04—B06 设计交接](../development/2026-09-13-b04-b06-handoff-01/HANDOFF.md)、[设计阶段历史 Prompt](../development/2026-09-13-b04-b06-handoff-01/NEXT-TASK-PROMPT.md) | 设计交接仍有效；B04 收口后 B05／B06 仍待各自采用。 |
| [分批施工计划](../plan/IMPLEMENTATION-PLAN.md) | 当前施工范围、依赖、各批状态和验收证据。 |
| [Astra 前置设计分工（必读）](../plan/ASTRA-DESIGN-PREPARATION.md) | B04—B11 的提前设计、函数声明、逐项允许／排除范围、文件／操作边界和交付终点；不自动采用具体候选。 |
| [数据库基础开发](database-development.md) | 显式迁移、核查、失败处理及独立数据库验证。 |
| [项目管理开发](project-development.md) | B03 INTEGRATED_LOCAL_VERIFIED：项目事务、固定配置、恢复机制及最终主目录证据。 |
| [B03 最终独立复核](../development/2026-09-12-b03-integration-01/FINAL-INDEPENDENT-REVIEW.md) | 主目录 PG 73/0、前端 28/28、浏览器 25+1 和 r1 发布；无开放 P0/P1/P2，businessReady=false，非整批业务 VERIFIED。 |
| [开发前必读及行动顺序](../plan/DEVELOPMENT-START.md) | 按角色阅读、首批实现依赖与失败验收。 |
| [当前交接](HANDOFF.md) | 当前完成度和分阶段边界。 |
| [页面／接口交接](HANDOFF-PAGE-API-DESIGN.md) | F01—F15 的唯一专题和后续去向。 |
| [后端交接](HANDOFF-BACKEND-DESIGN.md) | B01—B08 的协议、上游核对与开工条件。 |
| [审查修订](design-readiness-revisions.md) | Key 终结、配置绑定及页面一致性修订的具体状态。 |
| [项目契约检查](../reviews/2026-09-12-project-contracts/README.md) | 项目管理契约一致性、已修订条款及预算／时限摘要和模型预览的未补字段。 |
| [首批完整候选包](first-batch-complete-review.md) | D/T/A/P/S/R 及 P9 的集中采用／调整范围，仍为候选。 |
| [既有设计采用记录](design-delegation.md) | 保留 P1—P4 等原采用来源；旧持续授权流程已替代。 |

## 浏览器契约与页面

| 文档 | 用途与范围 |
| --- | --- |
| [首批浏览器契约](first-batch-browser-api-contract.md) | 已采用的项目、列表、配置引用及原操作恢复。 |
| [Issue 创建契约](issue-page-create-api-contract.md) | 已采用创建、详情、rooms、SSE；新增绑定候选另列。 |
| [认证与仓库发现候选](authentication-browser-api-draft.md) | 登录／重连、Destination 和查询恢复的唯一字段源，B02 子集已采用。 |
| [模型浏览器候选](model-settings-browser-api-draft.md) | 保存／安全终结、单模型测试、专用应用的唯一候选字段。 |
| [首批页面恢复](first-batch-recovery-design.md) | 错误、未知、权限、原操作与只读会话的候选安排。 |
| [会话与独立 Issue](conversation-issue-separation-design.md) | 当前页面关系、采用视觉及创建入口。 |
| [项目配置](project-configuration-design.md) | 接入、增仓、授权变化、F02 表单。 |
| [模型连接与参数](model-connection-settings-design.md) | 已采用分栏与参数、项目应用边界。 |
| [认证配置与验证](authentication-development.md) | B02 Linux App、HTTPS、秘密文件、后台维护及真实验收条件。 |
| [登录恢复页面](login-recovery-page-design.md) | 已采用 UI 与未采用认证细化分开。 |
| [仓库选择](repository-picker-design.md) | 选择摘要、发现覆盖及授权观察。 |
| [模型用于项目](model-project-apply-design.md) | 固定快照预览、测试观察与原应用恢复。 |
| [Key 保存页面](model-key-save-design.md) | 显式草稿、保存未知与安全终结候选。 |
| [最小 Issue 概览](issue-overview-minimal-design.md) | 首批详情 UI 候选，业务与运行观察分开。 |
| [原型串联说明](prototype-walkthrough-design.md) | 源片段、生成、路由与历史原稿的边界。 |

## 持久化、身份与模型内部设计

| 文档 | 用途与范围 |
| --- | --- |
| [首批持久化](backend-first-batch-persistence.md) | 已采用关系、事务、持久待办；§2.5 配置绑定候选。 |
| [Issue 配置绑定候选](issue-configuration-binding-design.md) | 创建事务固定引用，并发、重启、秘密失效与历史修复。 |
| [会话／Issue 后端专项](draft-conversation-backend-design.md) | 现行身份、内容权限、创建及后端衔接，保留历史文件名。 |
| [身份内部候选](backend-first-development-access-draft.md) | 仍包含内部会话／尝试记录和凭据刷新互斥，不被 HTTP 稿完全替代。 |
| [配置来源候选](backend-first-batch-sources-draft.md) | 秘密存储、导入、有限预算、App 与出站。 |
| [模型内部操作候选](backend-model-operations-draft.md) | 唯一槽位、保存终结、测试外发和精确应用事务。 |

## 后续会话、执行与交付

| 文档 | 用途与范围 |
| --- | --- |
| [消息目标](conversation-message-target-design.md) | 自然语言目标、澄清及页面选择边界。 |
| [消息／澄清浏览器契约](conversation-message-clarification-api-contract.md) | 已采用的已有会话五端点，首次会话与外部投递仍缺。 |
| [消息／澄清内部设计](backend-message-clarification-design.md) | 本地控制、可信依据、租约和恢复。 |
| [Manager 创建工具](manager-create-issue-tool-design.md) | 双入口、来源与恢复；完整 MCP 及可信运行上下文仍需闭合。 |
| [真实执行接入门槛](execution-integration-gates.md) | G1—G5：配置消费、消息工具、写方、生命周期、换图。 |
| [Graph／Loop](graph-loop-design.md) | 原生仓内 DAG 与 RepoMesh 跨仓协调及有界循环。 |
| [团队执行策略](team-execution-policy.md) | 成员、并发、调度与故障。 |
| [验证节点](verification-node-design.md) | 检查、证据、独立验证及 Skill 暂缓适用说明。 |
| [联调环境](integration-environment-design.md) | 固定组合、隔离环境、配置和数据。 |
| [Draft PR 与人工审查](draft-pr-review-design.md) | 交付检查与人工合并职责。 |
| [ChangeSet 规则](changeset-design.md) | CS1—CS14 归属、交付与恢复。 |
| [ChangeSet 逻辑结构](changeset-structure.md) | 记录关系及未冻结的存储／查询边界。 |
| [建项前仓库分析](issue-creation-repository-analysis.md) | 可选按钮与受控 Python 方向，具体协议仍需细化。 |

## 工程、架构与证据

| 文档 | 用途与范围 |
| --- | --- |
| [工程开发说明](development-scaffold.md) | 三进程组装、工具、配置与配套构建。 |
| [WSL2 开发环境建议](wsl-development-recommendation.md) | 保留最初建议；WSL 副本、Linux 工具、专用 SSH 和桌面项目已验证，当前接手及 B02 前置见 HANDOFF。 |
| [骨架验收记录](scaffold-verification.md) | 既有工程检查的条件与结果，非业务验收。 |
| [AgentTeams 证据索引](scaffold-agentteams-evidence.md) | 确定源码与历史实测的范围及追溯入口。 |
| [AgentTeams 验证清单](../plan/agentteams-validation-plan.md) | AT01—12 的具体用例；G1—G5 不替代该清单。 |
| [架构方案](architecture-design-v1.md) | 各章节的已采用方向、候选及未验证能力。 |
| [技术选型](technology-selection.md) | 已采用技术方向。 |
| [暂缓的 Skill 工程](skill-engineering-design.md) | H1—H15 方向保留，按 ADR-0009 暂缓；不是本批前置。 |

## 目录外入口

| 入口 | 用途 |
| --- | --- |
| [领域语言](../../CONTEXT.md) · [ADR 索引](../adr/README.md) | 术语与全部架构决定；ADR 本次未归档。 |
| [原型目录](../prototypes/README.md) | 当前 11 份原稿、串联源及 4 份历史归档。 |
| [AgentTeams 调研](../agentteams-survey-2026-09-07/agentteams-survey.md) · [API／CLI 附录](../agentteams-survey-2026-09-07/agentteams-api-cli-survey-2026-09-07.md) | 后端需结合确定源码和后续实测核对，不能当作已集成。 |
| [09-12 审查](../reviews/2026-09-12-design-readiness/README.md) · [修订检查](../reviews/2026-09-12-design-fixes/README.md) | 历史证据及限定检查，不代表真实业务通过。 |
| [计划目录整理与旧任务提示](../archive/2026-09-13-plan-organization/README.md) · [全部历史](../archive/README.md) | 已过期 B03、B04 提示，整理前交接、计划、迁移映射与原始字节；更早归档从历史总导航进入。 |

## 数据库方案与 API 设计

| 文档 | 用途与范围 |
| --- | --- |
| [Go 版数据库重构方案](../RepoMesh_Go版数据库重构方案.html) | 正式数据库方案：7 个功能方向 44 张目标表，每张表的字段、来源与关联。目标表尚无迁移文件。 |
| [API 设计](api-design.md) | 按同样 7 个方向组织的 HTTP 接口设计：通用约定、逐资源端点与字段、状态机、表与资源映射、与现有端点的对应、待决事项。除标注“已实现”的端点外均未实现。 |

原 `docs/api-database/` 目录与 B05-B11 物理合表专题已于 2026-09-15 删除或归档，见 [归档记录](../archive/2026-09-15-api-database-catalog/README.md)。
