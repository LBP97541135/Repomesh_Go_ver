# 计划目录与管理文档整理

日期：2026-09-13。整理基于提交 `3ad89bd`。本次建立 `docs/plan/`，更新文档导航和 B04 状态描述，并归档过期的接手提示及整理前的交接、施工计划。

当前阅读入口为[文档总导航](../../README.md)、[计划导航](../../plan/README.md)和[当前交接](../../current/HANDOFF.md)。本目录保存历史语境，旧任务指令不自动恢复。

## 文件去向

| 原路径 | 当前位置 | 处理方式 |
| --- | --- | --- |
| `docs/current/IMPLEMENTATION-PLAN.md` | [施工计划](../../plan/IMPLEMENTATION-PLAN.md) | 迁至计划目录，清理已过期的临时协作安排。 |
| `docs/current/ASTRA-DESIGN-PREPARATION.md` | [前置设计分工](../../plan/ASTRA-DESIGN-PREPARATION.md) | 迁至计划目录，对齐 B04 已采用、已实现的范围。 |
| `docs/current/DEVELOPMENT-START.md` | [开发行动指南](../../plan/DEVELOPMENT-START.md) | 迁至计划目录，区分模型保存与后续测试、应用。 |
| `docs/current/agentteams-validation-plan.md` | [AgentTeams 验证清单](../../plan/agentteams-validation-plan.md) | 迁至计划目录，仅调整相对链接，原版本与结论保留。 |
| `docs/current/NEXT-TASK-B03-PROMPT.md` | [B03 旧提示](NEXT-TASK-B03-PROMPT.md) | 已完成批次的历史提示，不作为默认接手入口。 |
| `docs/current/NEXT-TASK-B04-PROMPT.md` | [B04 旧提示](NEXT-TASK-B04-PROMPT.md) | 已被后续采用与验收替代的准备提示。 |
| `docs/current/HANDOFF.md` | [整理前交接](HANDOFF.md) | 当前路径保留精简交接，本副本保存原来的累计记录。 |
| `docs/current/IMPLEMENTATION-PLAN.md` | [整理前施工计划](IMPLEMENTATION-PLAN.md) | 保存当时的批次表、临时分工和旧阶段说明。 |

## 原始内容与导航

[originals.zip](originals.zip)保存两份旧提示、交接和施工计划在 `3ad89bd` 的原始字节。[manifest.json](manifest.json)列出这四份文件的原路径和 SHA-256。归档 Markdown 副本只增加历史提示并调整相对链接，历史命令中的路径和时间仍指当时环境。

其他历史 Markdown 仅修正指向已迁移文件的导航链接。原始 ZIP、日志、截图、JSON 校验清单、脚本及实验结果保持原位和原字节；其中记录的旧路径可按上表追溯。旧验收中的哈希证明当时的文件，不能当作后续编辑后的哈希。

本次只整理文档，不改变产品采用范围，不执行产品构建、数据库、浏览器或外部验收。

## 检查结果

本次检查 Git 跟踪的 Markdown 和新增文档的本地相对链接，并核对变更文件涉及的章节锚点。迁移未新增断链；旧归档中可确定目标的路径错误已修正。归档原文与 `3ad89bd` 的文件及清单哈希一致，AgentTeams 验证清单仅调整导航。`git -c core.safecrlf=false diff --check` 通过；该参数仅关闭本次检查的换行提示，不修改 Git 配置。

仍缺失的材料是 B04 验收报告引用的外部审查任务 `bc-7ec07cce-0d1b-5468-8050-6d7bf4a6cb51`，以及插件 NOTICE 引用的 `plugins/pstack/CHANGES.md`。本次不补造任务内容或第三方插件附件；现行入口没有缺失的本地链接。
