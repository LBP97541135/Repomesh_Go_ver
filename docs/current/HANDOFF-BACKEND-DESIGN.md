# 后端开发交接

当前采用与实现状态见[当前交接](HANDOFF.md)和[施工计划](../plan/IMPLEMENTATION-PLAN.md)。B04 的 D01—D04 已采用并完成本地集成验收，见[B04 采用记录](b04-model-sources-adoption.md)；B05、B06 的后续范围仍待采用。原[B04—B06 设计交付](../development/2026-09-13-b04-b06-design-01/README.md)保留设计时的候选及复核范围。

更新：2026-09-13。先读[开发指南](../plan/DEVELOPMENT-START.md)、[当前交接](HANDOFF.md)，再按下表进入唯一设计。旧阶段、通信与完整历史表保留于[09-11 后端交接](../archive/2026-09-12-development-preparation/docs/current/HANDOFF-BACKEND-DESIGN-2026-09-11.md)，不恢复旧任务身份。

## B01—B08 的去向

| 范围 | 设计入口与开工边界 |
| --- | --- |
| B01 身份／权限／模型配置 | [身份内部候选](backend-first-development-access-draft.md)、[认证浏览器候选](authentication-browser-api-draft.md)、[来源候选](backend-first-batch-sources-draft.md)、[模型内部候选](backend-model-operations-draft.md)。认证身份与秘密基础按 B02 已采用范围实现，模型保存与版本按 B04 采用范围实现；模型测试、应用及费用相关规则仍待后续采用。 |
| B02 最小关系 | [首批持久化](backend-first-batch-persistence.md)、[配置绑定候选](issue-configuration-binding-design.md)。首批关系已有采用范围；接收未来可接续的真实 Issue 前落实同事务固定配置，不能用创建条件令牌代替外键。 |
| B03 事务／持久待办 | [首批持久化](backend-first-batch-persistence.md)。落实操作、业务、事件、待办原子提交及 DB01—17；未接消费者如实 blocked，外部未知保留责任。 |
| B04 浏览器用例 | [首批契约](first-batch-browser-api-contract.md)、[创建契约](issue-page-create-api-contract.md)、[页面恢复](first-batch-recovery-design.md)。按唯一字段和权限规则交付；认证按 B02、模型保存按 B04 的采用范围已实现，模型测试与专用应用仍待后续采用。 |
| B05 消息与 Manager | [消息内部设计](backend-message-clarification-design.md)、[消息五端点](conversation-message-clarification-api-contract.md)、[Manager 工具](manager-create-issue-tool-design.md)。最小本地协议不覆盖首条消息、真实投递、完整 MCP、多源和更正；先完成 G1—G2。 |
| B06 运行准备／受限主机 | [架构](architecture-design-v1.md)、[执行门槛](execution-integration-gates.md)。补实例／room/session 映射、受限动作、实际配置消费、资源生命周期和恢复；共享 Manager 更新不提供按 Issue 固定配置。 |
| B07 受控执行／Graph | [Graph／Loop](graph-loop-design.md)、[团队执行](team-execution-policy.md)、[验证](verification-node-design.md)。先 G3—G4 的单仓单轮，再 G5 的第二轮／跨仓部分失败；不重写仓内 DAG。 |
| B08 仓库分析 | [分析专题](issue-creation-repository-analysis.md)、[ADR-0020](../adr/0020-python-repository-analysis-plugin.md)。受控 Python 方向已采用，Schema、快照、作业及恢复未全部定义；不依赖 AgentTeams Ready。 |

## 上游核对与验证

必须结合[09-07 调研](../agentteams-survey-2026-09-07/agentteams-survey.md)、[API／CLI 附录](../agentteams-survey-2026-09-07/agentteams-api-cli-survey-2026-09-07.md)、[确定来源版本](../../third_party/agentteams-source.json)和[现有证据索引](scaffold-agentteams-evidence.md)核对。09-07 静态调查与 517caff 后续证据分开；源码存在、历史实验与产品运行接入不互相替代。

数据库连接、显式迁移和核查已实现，见[数据库说明](database-development.md)和[施工批次表](../plan/IMPLEMENTATION-PLAN.md)。认证、秘密、项目和 B04 模型来源相关业务表已实现；Issue、外部投递及受限执行器尚未实现。[审查修订](design-readiness-revisions.md)补齐的是候选和文档语义，真实故障验收按开发指南和各专题进行。旧归档未运行 Go/npm、数据库或历史上游实验；本次数据库施工的实际验收见[B01 记录](../development/2026-09-12-batch-01/README.md)。
