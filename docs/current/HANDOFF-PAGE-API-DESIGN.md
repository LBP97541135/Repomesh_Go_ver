# 页面与接口开发交接

当前采用与实现状态见[当前交接](HANDOFF.md)和[施工计划](../plan/IMPLEMENTATION-PLAN.md)。B04 的 D01—D04 已采用并完成本地集成验收，见[B04 采用记录](b04-model-sources-adoption.md)；B05、B06 的后续范围仍待采用。原[B04—B06 设计交付](../development/2026-09-13-b04-b06-design-01/README.md)保留设计时的候选及复核范围。

更新：2026-09-13。先读[开发指南](../plan/DEVELOPMENT-START.md)、[当前交接](HANDOFF.md)和[原型导航](../prototypes/README.md)。累计讨论和旧协作安排见[整理前全文](../archive/2026-09-12-development-preparation/docs/current/HANDOFF-PAGE-API-DESIGN.md)，不要求开发者按历史顺序重读。

## 当前基线

主视觉沿会话与独立 Issue；创建使用已采用弹窗、关联控件及提交反馈。F01 登录恢复、F02 项目表单、F03 仓库选择、F04 模型应用中列明的 UI 已采用；认证和模型保存分别按[B02](b02-authentication-adoption.md)、[B04](b04-model-sources-adoption.md)的采用范围已实现；模型测试、应用、详情及其余恢复细化仍按对应候选判断。[09-12 修订](design-readiness-revisions.md)未把新协议变成已采用；内存原型与业务 API 分开。

浏览器字段唯一来源为[首批契约](first-batch-browser-api-contract.md)、[Issue 创建契约](issue-page-create-api-contract.md)及相应认证／模型候选。完整 configuration PATCH 与仅换模型的专用应用分别实现；原型样例不能增加另一套字段或枚举。

## F01—F15 的去向

| 范围 | 当前阅读与实施边界 |
| --- | --- |
| F01 登录／恢复 | [登录页面](login-recovery-page-design.md)、[认证候选](authentication-browser-api-draft.md)。认证已按 B02 采用范围实现；保持当前账号、回调结果与原业务结果分离。 |
| F02—F03 项目表单／选仓 | [项目配置](project-configuration-design.md)、[仓库选择](repository-picker-design.md)。保留已有范围，明确增仓；实现并发修订、权限未知和原更新恢复。 |
| F04—F05 模型应用／Key | [应用](model-project-apply-design.md)、[Key](model-key-save-design.md)、[模型字段候选](model-settings-browser-api-draft.md)。固定快照、独立测试观察、原操作终结及精确保留 execution。 |
| F06 执行配置 | [配置来源候选](backend-first-batch-sources-draft.md)。首批只覆盖必要来源与有效值摘要，完整数值管理页后置。 |
| F07—F08 列表／Issue 创建 | [会话与 Issue](conversation-issue-separation-design.md)、[创建契约](issue-page-create-api-contract.md)、[恢复](first-batch-recovery-design.md)。先接基本列表、创建、详情和原键恢复。 |
| F09 最小详情 | [最小概览候选](issue-overview-minimal-design.md)。新 UI 范围仍待采用；记录保存、会话可读与运行就绪独立显示。 |
| F10—F11 消息／多 Issue／更正 | [消息契约](conversation-message-clarification-api-contract.md)、[目标专题](conversation-message-target-design.md)。已有会话首批只读，首条消息、多源与更正属于后续批次。 |
| F12 运行准备／Leader 房间 | [执行门槛 G1—G4](execution-integration-gates.md)、[后端交接](HANDOFF-BACKEND-DESIGN.md)。dock 布局采用不等于运行状态、房间映射已经完成。 |
| F13 DAG／执行面板 | [Graph／Loop](graph-loop-design.md)。只读图布局保留，动态查询和执行操作等运行协议收口后再接；不新增直接编辑图。 |
| F14 验证／交付 | [验证节点](verification-node-design.md)、[ChangeSet](changeset-design.md)、[Draft PR](draft-pr-review-design.md)。完整浏览器协议和 UI 仍需后续细化。 |
| F15 可选仓库分析 | [仓库分析](issue-creation-repository-analysis.md)。受控 Python 方向已采用，具体表单与协议未完成；失败不能阻止手动建项。 |

实现必须覆盖加载、空、错误、失权、未知、原操作恢复及晚到响应隔离；错误响应不能泄露旧主体内容。前端可按已采用范围并行开发，真实管理联调按开发指南推进，不需要先完成全部高级面板。
