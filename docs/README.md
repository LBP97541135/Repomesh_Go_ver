# RepoMesh 文档导航

本页说明文档放在哪里、从哪里开始查。当前实现与验收状态统一见[当前交接](current/HANDOFF.md)，施工批次与依赖见[计划导航](plan/README.md)。

## 按任务进入

| 目的 | 入口 |
| --- | --- |
| 首次接手、了解全局 | [Agent 全局阅读指南](current/AGENT-READING-GUIDE.md) |
| 查计划、设计分工和开发顺序 | [计划导航](plan/README.md)、[施工计划](plan/IMPLEMENTATION-PLAN.md)、[开发行动指南](plan/DEVELOPMENT-START.md) |
| 查已实现能力、验收范围和剩余条件 | [当前交接](current/HANDOFF.md) |
| 查业务设计、接口和采用范围 | [现行专题索引](current/README.md) |
| 构建、启动和修改工程 | [根 README](../README.md)、[工程导航](../AGENTS.md)、[开发说明](current/development-scaffold.md) |
| 查页面基线及可运行原型 | [原型导航](prototypes/README.md)、[串联预览](prototypes/index.html) |
| 查领域术语和架构取舍 | [领域语言](../CONTEXT.md)、[ADR 索引](adr/README.md) |
| 查前端、接口与后端专题分工 | [页面交接](current/HANDOFF-PAGE-API-DESIGN.md)、[后端交接](current/HANDOFF-BACKEND-DESIGN.md) |
| 查 AgentTeams 调研和验证范围 | [调研](agentteams-survey-2026-09-07/agentteams-survey.md)、[验证计划](plan/agentteams-validation-plan.md)、[证据索引](current/scaffold-agentteams-evidence.md) |
| 查设计审查与修订证据 | [原始审查](reviews/2026-09-12-design-readiness/README.md)、[修订验证](reviews/2026-09-12-design-fixes/README.md) |
| 查旧文档、原话与迁移路径 | [历史归档](archive/README.md)、[本次计划目录整理](archive/2026-09-13-plan-organization/README.md) |

## 目录职责

| 目录 | 保存内容 | 维护方式 |
| --- | --- | --- |
| `plan/` | 跨任务的施工计划、设计分工、开发顺序和验证计划 | 持续更新同一计划，链接采用记录与实际证据。 |
| `current/` | 当前接手入口、专题设计、接口契约、开发说明和采用记录 | 每个主题保留唯一入口，区分已采用章节与候选。 |
| `adr/` | 架构决定及其演进 | 记录明确的采用范围和后续替代关系。 |
| `prototypes/` | 页面原型、来源片段和预览入口 | 从原型导航确认基线；模拟行为不等于产品接口。 |
| `development/` | 按日期和批次保存的实施过程、单次计划、复核和验收证据 | 同轮计划、脚本与结果一起保存，后续运行新增记录。 |
| `reviews/` | 专题审查、问题清单及修订验证 | 保留审查时的源码版本、范围与结论。 |
| `research/` | 专题调研 | 写明来源和适用范围；调研结论不自动成为产品决定。 |
| `archive/` | 已被替代的文档、旧任务提示及整理前快照 | 明确历史身份、原路径、替代入口和原始内容位置。 |

`agentteams-survey-2026-09-07/` 保留固定版本的上游调研与附件。仓库根目录的 `validation/` 保存本机历史实验，`third_party/AgentTeams/` 是独立上游克隆；它们不属于产品发布内容，部分资料不会随 Git checkout 提供。

## 文档维护规则

1. 新增文件前先查[计划导航](plan/README.md)和[现行专题索引](current/README.md)。已有主题优先更新原文，避免复制第二份契约或进度表。
2. 工程排期、任务依赖和验证方案放入 `plan/`。产品中的 Plan Version、Graph 和计划生效规则放入业务专题或 ADR。
3. 单次开发、实验的 `PLAN.md` 与该轮证据放在同一个 `development/<日期-主题>/` 目录。完成后的任务提示归档，并链接当前替代入口。
4. 更新完成状态时同步[当前交接](current/HANDOFF.md)和[施工计划](plan/IMPLEMENTATION-PLAN.md)，附实际源码基线与证据。其他导航只指向这两处，不再复制验收日志。
5. 移动文件时同时更新入站链接、文件内相对链接、章节锚点及入口索引。历史日志、脚本和校验清单保留当时的路径和哈希，用迁移表解释其去向。
6. 纯文档修改核对本地链接、命令与源码事实，并检查 `git diff --check`。区分原有缺失材料和本次新增断链，无需重跑产品或外部实验。

采用状态按具体章节和后续替代关系判断。计划完成、设计通过复核、代码已实现和运行验收通过分别记录；目录整理不改变业务决定。

## 数据库方案与 API 设计

[RepoMesh_Go版数据库重构方案.html](./RepoMesh_Go版数据库重构方案.html) 是正式数据库方案，按 7 个功能方向定义 44 张目标表。[API 设计](./current/api-design.md) 按同样的方向定义 HTTP 接口，并维护表与资源的映射。修改任一方时同步另一方，并运行 `python3 docs/development/2026-09-15-api-redesign-01/scripts/verify_api_doc.py` 核对映射、ADR 引用和链接。

原 `docs/api-database/` 目录已于 2026-09-15 删除，说明与一并归档的专题见 [归档记录](./archive/2026-09-15-api-database-catalog/README.md)；此前的制作证据仍在 `development/` 原处。
