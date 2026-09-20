# RepoMesh 计划导航

本目录集中维护工程施工顺序、前置设计分工和验证计划。当前完成状态见[当前交接](../current/HANDOFF.md)，业务设计与接口见[现行专题索引](../current/README.md)。计划中的排期和勾选不构成新任务授权，也不表示候选已经采用。

## 按目的查计划

| 目的 | 文档 | 维护内容 |
| --- | --- | --- |
| 扩展观测平台、验收接入与规划真实实验 | [观测平台验收与实验计划](observation-platform-validation-and-experiments.md) | 对应全本地 Spec v1.1：P0—P4、OBS01—OBS28 与 E0—E4；分项执行状态见交接和实施记录。 |
| 建立本地观测与评估 | [观测评估开发计划](agentloop-observability-implementation.md) | 最小本体改动、全本地工作台、OTLP／数据集、折扣验收与可选 DeepSeek 评审。 |
| 修复项目、仓库、Issue 从属范围 | [范围修复计划](project-repository-scope-repair.md) | 操作顺序、后台范围约束、数据库兼容和验收标准。 |
| 查批次状态、依赖和验收入口 | [分批施工计划](IMPLEMENTATION-PLAN.md) | B00—B11 的实施表、设计待办及实际证据。 |
| 确定开发前阅读和行动顺序 | [开发前阅读与行动指南](DEVELOPMENT-START.md) | 按角色选择材料，落实前置条件、失败验收和分阶段接入。 |
| 分配前置设计工作 | [Astra 前置设计分工](ASTRA-DESIGN-PREPARATION.md) | B04—B11 各项允许范围、排除项、设计交付和实现交接。 |
| 核对 AgentTeams 验证覆盖 | [源码核查与待验证清单](agentteams-validation-plan.md) | AT-01—AT-12 的场景与通过条件，保留固定版本的源码和实验背景。 |

首次接手先读[全局阅读指南](../current/AGENT-READING-GUIDE.md)，再从本目录选择任务所需计划。后续运行接入的协议与完成条件仍由[执行接入门槛](../current/execution-integration-gates.md)维护。

## 计划与证据的归属

- 跨任务持续维护的施工、设计分工和验证计划放在本目录；新增计划同时加入上表。
- 业务中的 Plan Version、Graph 和计划生效规则属于产品设计，继续放在 `docs/current/` 或 `docs/adr/`。
- 单次开发或实验的 `PLAN.md` 与该轮结果一起保存在 `docs/development/<日期-主题>/`。这里链接对应记录，不拆散历史证据。
- 已失效的接手提示和已被替代的计划移入 `docs/archive/`，在归档说明中记录替代入口。B03、B04 旧提示见[本次归档](../archive/2026-09-13-plan-organization/README.md)。

## 本次目录迁移

2026-09-13，上表四份文件从 `docs/current/` 移至本目录，文件名保持不变。现行与历史 Markdown 的导航链接已按新位置调整；历史命令、日志和校验清单中的旧路径保留编制时含义。迁移映射见[归档与整理说明](../archive/2026-09-13-plan-organization/README.md)。
