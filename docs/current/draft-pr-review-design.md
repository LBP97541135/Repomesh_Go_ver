# Draft PR 与人工审查时机

状态：E1—E5 已由用户确认；2026-09-08 记录。ChangeSet 归属与记录整合边界见 [ADR-0008](../adr/0008-changeset-attribution-and-history.md)；CS1—CS14 也已全部确认，合并与恢复按 [ChangeSet 设计](changeset-design.md)处理。

## E1：创建时机

各仓 Leader 初审后，形成首个具有明确改动范围和自测记录的候选 commit，就创建 draft PR。跨仓验证可以随后继续，PR 明确展示尚未完成的验证。无需等待全部跨仓验证通过才产生可查看的 draft。

## E2：持续更新

默认一条 issue 在一个仓库对应一个持续更新的 PR，后续候选追加提交。保留各轮 commit 与验证结果的关系，不为每次 Attempt 新开 PR。ChangeSet 统一关联这些 PR、候选及交付组合，保留交付历史；执行、验证、交付进展分开汇总；PR 的实际 head／合并结果变化需重新匹配候选与证据。字段与记录拆分示意见[结构说明](changeset-structure.md)。

## E3：推荐正式审查

约定必检项全部具有适用于当前组合的有效通过证据，Agent 内部复核完成，相关必需仓库检查满足要求后，由 Manager 汇总“可审查”结果及关联 PR。用户始终可以提前查看 draft。

测试通过、可审查、人工合并及 issue 完成分别记录；最终完成条件按 CS9：约定交付物和验收、必做事项完成，代码变更的所有必需 PR 已人工合并且最终实际组合证据有效；部署状态另记。

## E4：GitHub draft 状态转换

首期由 RepoMesh 展示“建议正式审查”，GitHub 上将 draft 转为正式 PR 的动作由人完成。是否进一步授权 Agent 转换，留待后续明确；不能把未验证接口能力设为本期前提。

## E5：审查反馈

反馈回到原 issue，由 Manager 分类为缺陷、补证或需求变更，再按模式和当前计划处理。审查评论不自动授予额外权限或预算，也不自动放宽验收条件。

YOLO 可以完成自身执行并交出可审查结果，人的审查和合并作为后续交付状态记录，不增加逐项人工执行门禁。合并仍遵守 ADR-0002；合并按兼容性选顺序，部分合并后依据实际版本继续处理，取消与恢复分别按 CS10—CS12 记录和执行。

相关：[验证节点设计](verification-node-design.md)、[Graph／Loop](../adr/0007-graph-loop-plugin.md)、[ChangeSet 设计](changeset-design.md)。
