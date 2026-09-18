# B05-B11 物理合表与表清单（已归档）

> **归档说明（2026-09-15）：** 本文描述的 63→34 合表方案及其依赖的 `docs/api-database/` 目录已被 [Go 版数据库重构方案](../../RepoMesh_Go版数据库重构方案.html)（44 张目标表）替代，对应接口见 [API 设计](../../current/api-design.md)。下文保留归档时原貌，其中指向 `docs/api-database/` 的链接目标已随目录一并删除，改为纯文本；`docs/development/` 中的制作证据仍在原处。

更新：2026-09-15。用户已批准把 B05-B11 的 63 张候选或提案表按物理承载合到 34 张，净减 29 张，约 46%。本文是合表的长期入口，记录范围、批次、旧表映射、声明格式、计数口径和校验命令。

**状态：design_only。** 合表只改变物理承载。B01-B04 的现有表和已采用逻辑契约不变，B05-B11 的功能候选、提案、未实施与验收状态也不因合表或文档生成而改变。清单和校验是静态设计事实，不是数据库行为验证；清单冻结状态以 `table-manifest.json`（已删除） 的 `status` 字段为准。

## 范围与计数

- 手册基线 36 张 = 35 张业务表加 1 张系统表 `public.repomesh_schema_migrations`，来自 `0001` 至 `0006` 迁移。
- 手册外扫描表 1 张：`repomesh_scan.repositories`，来自 `0007_scan_catalog.sql`。
- 手册外决策链表 3 张：`public.decision_chain_nodes`、`public.decision_embeddings`、`public.feature_settings`，来自 `0008_decision_chain.sql`（origin/main `6899665`）；单独成组，不计入 B05-B11 的 34 张。
- B05-B11 合表 63 张候选或提案表压到 34 张目标物理表。
- 手册范围合计 70 = 36 + 34；全仓含扩展合计 74 = 36 + 1 扫描 + 3 决策链 + 34 设计。

## 批次与目标表

| 批次 | 旧表 | 目标 | 声明章节 |
| --- | --- | --- | --- |
| B05 | 16 | 9 | `b05.md` |
| B06 | 13 | 6 | `b06.md` |
| B07 | 1 | 1 | `b07.md` |
| B08 | 0 | 0 | 不声明 |
| B09 | 13 | 6 | `b09.md` |
| B10 | 13 | 10 | `b10.md` |
| B11 | 7 | 2 | `b11.md` |

B10 与 B11 按首次依赖归属：B10 单轮执行首次需要 `change_set_versions` 与业务计划版本 `business_plan_versions`（ADR-0003 的计划许可与全部目标读回生效、ADR-0015 跨轮复用都需要，原目录漏登且误称 B06 提供）；B11 只为跨仓边与最终采纳新增 `cross_repo_edges`、`result_admissions`。

### B05（16 → 9）

| 目标物理表 | 旧表承载 |
| --- | --- |
| `repomesh_modelbudget.windows` | `modelbudget.windows` |
| `repomesh_models.previews` | `models.previews` |
| `repomesh_models.tests` | `models.tests`、`modelbudget.test_reservations`、`models.test_dispatch`、`models.actor_outstanding_tests`、`models.unknown_test_closures` |
| `repomesh_models.test_observations` | `models.test_observations` |
| `repomesh_models.test_handler_instances` | `models.test_handler_leases` |
| `repomesh_models.application_operations` | `models.application_operations` |
| `repomesh_sources.policy_versions` | `budget_policies`、`time_limit_policies`、`egress_policies`、`request_policy_versions`、`time_policy_versions` |
| `repomesh_sources.policy_bindings` | `test_bindings` |
| `repomesh_sources.policy_imports` | 新增；schema2 导入原操作。旧 `repomesh_sources.imports` 保持 `schema_version=1` |

### B06（13 → 6）与 B07（1 → 1）

| 目标物理表 | 旧表承载 |
| --- | --- |
| `repomesh_issues.conversations` | `repomesh_issues.conversations`；另承载 B09 的会话消息计数列组 |
| `repomesh_issues.issues` | `repomesh_issues.issues`、`creation_operations`、`page_sources`、`conversation_cards`、`continuation_work`、`issue_event_streams` |
| `repomesh_issues.changesets` | `repomesh_issues.changesets` |
| `repomesh_issues.repository_scopes` | `issue_repository_scope`、`issue_content_scope`、`conversation_content_scope` |
| `repomesh_issues.issue_events` | `repomesh_issues.issue_events` |
| `repomesh_issues.project_issue_counters` | `repomesh_issues.project_issue_counters` |
| `repomesh_issues.issue_room_links` | `repomesh_issues.issue_room_links`（B07） |

### B09（13 → 6）

| 目标物理表 | 旧表承载 |
| --- | --- |
| `repomesh_messaging.conversation_messages` | `message_submissions`、`conversation_messages` |
| `repomesh_messaging.message_processing_entries` | `message_processing_entries`、`logical_work_requests`、`manager_call_authorizations` |
| `repomesh_messaging.clarifications` | `clarifications`、`clarification_answers` |
| `repomesh_messaging.control_operations` | `target_resolutions`、`control_operations` |
| `repomesh_messaging.manager_sessions` | `manager_sessions` |
| `repomesh_messaging.manager_executions` | `message_deliveries`、`manager_calls` |

`conversation_message_counters` 不建新表，落到 B06 `repomesh_issues.conversations` 的 `last_message_sequence` 字段组。

### B10（13 → 10）与 B11（7 → 2）

B10/B11 的旧 20 张提案先合到 11 张承载，再补 1 张业务计划版本，共 12 张；B05-B11 合计 34 张。

| 目标物理表 | 旧表承载 |
| --- | --- |
| `repomesh_execution.rounds` | `rounds`；另存 B11 轮次转换与停止的最新摘要 |
| `repomesh_execution.repository_issues` | `repository_issues` |
| `repomesh_execution.plan_revisions` | `plan_revisions`、`round_plan_pointers`、`target_applications` |
| `repomesh_execution.execution_tasks` | `execution_tasks` |
| `repomesh_execution.attempts` | `attempts` |
| `repomesh_execution.resource_reservations` | `worker_leases`、`resource_reservations` |
| `repomesh_execution.execution_operations` | `dispatch_operations`、`host_operations` |
| `repomesh_execution.execution_records` | `attempt_observations`、`attempt_artifacts`、`write_capability_revocations`、`round_transitions`、`stop_decisions` |
| `repomesh_execution.change_set_versions` | `round_candidate_refs`、`round_combinations`；candidate 与 combination 两个明确子型。废弃 `round_combinations` 物理表 |
| `repomesh_execution.business_plan_versions` | 新增必需依赖；业务 Plan Version。原目录漏登，由 Issue 计划用例写、执行层只读；与执行层技术 `plan_revisions` 分离 |
| `repomesh_execution.cross_repo_edges` | `cross_repo_edges`（B11） |
| `repomesh_execution.result_admissions` | `result_admissions`（B11） |

### 业务计划版本与执行层计划修订

- `repomesh_execution.business_plan_versions` 是业务 Plan Version：保存业务目标、范围、验收依据、政策引用和循环上限，以及许可、激活和当前生效摘要。B10 的单轮执行首次需要，B11 跨轮复用。最终设计见 `b10.md`（已删除）；设计决策与复核过程见 [business-plan-versions.md](../../development/2026-09-15-table-consolidation-01/analysis/business-plan-versions.md) 和 [review-business-plan.md](../../development/2026-09-15-table-consolidation-01/analysis/review-business-plan.md)，以最终正文为准。
- `repomesh_execution.plan_revisions` 是执行层的技术安排：一轮内每个仓库的上游绑定、任务集合和当前指针。两者身份、写入者和生命周期不同，不互相改名，也不合并成一张表。
- 许可决定与逐目标绑定不新增物理表：`execution_records` 增加 `business_plan_decision` 与 `business_plan_target` 两个明确子型，执行观察仍归计划应用组。
- B06 的 `repomesh_issues.issues` 在 B10 实施时增补 3 列：`current_business_plan_version_id`、`business_plan_pointer_revision`、`next_business_plan_version_index`。它们不改变 B06 的 6 张表计数，首次建 Issue 时业务指针为空。
- 该表的来源是原 B10/B11 引用而未建模的已采用业务 Plan Version（ADR-0003、ADR-0015、Graph §5），不从原 63 张旧表映射里虚构旧表名。净新增 1 张，B10 从 9 张增到 10 张。

## 声明与机器清单

完整的 63 条映射、批次归属和快照来源在 `table-manifest.json`（已删除）。每张目标物理表只在所属批次正文声明一次，独立一行：

```text
物理表：`repomesh_issues.issues`
```

跨批次复用不重复声明；B00-B04 与 B08 不写声明。B06 的 `conversations` 同时承载 B09 的消息计数列组，但只由 B06 声明一次。

校验命令：

```bash
python3 docs/api-database/verify_design.py
python3 docs/api-database/verify_design.py --strict
python3 docs/api-database/verify_design_tests.py
python3 docs/api-database/render.py --check
```

日常检查不读取历史 baseline。本轮合表的受保护文件范围审计单独执行：

```bash
python3 docs/api-database/verify_design.py --baseline docs/development/2026-09-15-table-consolidation-01/baseline.json
```

## 边界

- 不改产品代码、SQL 迁移、已实现表或历史证据。
- 不把 B05-B11 的候选、提案或未实施状态升级为已采用或已实现。
- 静态清单与生成检查不执行迁移，不证明并发、恢复或任何数据库行为已经验证。
- 合表设计与逐条裁决副本见 [本轮证据](../../development/2026-09-15-table-consolidation-01/README.md)。
