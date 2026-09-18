# B05-B11 物理合表文档与校验工具制作记录

状态：已冻结（`design_only`）。B05-B11 的物理合表设计已接入公共阅读目录、机器可校验清单、首页计数和 current 专题入口；34/34 正文声明齐备，最终 HTML 已生成并固定 digest 与 SHA-256，已完成浏览器25/25并回填证据。合表方案由 Astra 负责；本代理只写文档和工具，不改变合表决定。

执行计划见 [PLAN.md](./PLAN.md)，决定记录见 [decisions.tsv](./decisions.tsv)，Astra 方案副本见 [analysis/](./analysis/)。`baseline.json`、`verification*.json`、浏览器脚本、浏览器记录和截图由父代理独占，本代理不覆盖。

独立复核副本包括 [review-b05-b08.md](./analysis/review-b05-b08.md)、[review-b09.md](./analysis/review-b09.md)、[review-b10-b11.md](./analysis/review-b10-b11.md) 与 [review-business-plan.md](./analysis/review-business-plan.md)。最终设计以 [b05-b11-storage-consolidation.md](../../current/b05-b11-storage-consolidation.md) 和各批次正文为准；analysis 是过程证据，不作为永久优先级来源。

## 公共规则

- 用户已授权物理合表设计，范围是 B05-B11 的 63 张候选或提案表；B01-B04 的 36 张手册基线表（35 张业务表加 1 张系统表 `public.repomesh_schema_migrations`）和 `0007` 扫描表 `repomesh_scan.repositories` 保持不动。
- 合表只改变物理承载。已采用的逻辑身份、唯一性、作用域、事务、恢复和清理语义继续有效；候选、提案、未实施和验收状态不因合表或文档生成而改变。
- 不隐藏分类。schema2 导入回执（B05 `repomesh_sources.policy_imports`）、候选版本与组合版本（B10 `repomesh_execution.change_set_versions` 的 candidate/combination 两个子型）、业务计划版本（B10 `repomesh_execution.business_plan_versions`，标为 `new_required_dependency` 并链接 design_ref）都在 manifest 中显式登记，废弃的 `round_combinations` 不再作为物理表出现。
- 业务计划版本与执行层 `plan_revisions` 分离。`execution_records` 用 `business_plan_decision` 与 `business_plan_target` 两个子型承载许可/激活决定和逐目标绑定，不新增物理表；B10 实施时给 B06 新 `issues` 增补 `current_business_plan_version_id`、`business_plan_pointer_revision`、`next_business_plan_version_index` 三列，不改变 B06 表数。
- 清单以迁移和快照为事实来源。37 张已有表从迁移源码清点，63 张旧表从合表前的 `19cac6e` 正文快照逐行取证，34 张目标表按 Astra 方案与独立复核填写。
- 每张目标物理表只在所属批次正文用独立一行声明，格式是行首 `物理表：` 加反引号包裹的物理表名。B00-B04 与 B08 不写声明，跨批次复用不重复声明。
- 计数分列。手册范围 70 = 手册基线 36 + 设计 34；全仓含扫描 71 = 36 + 1 + 34；旧表映射仍为 63 张，净减 29 张。首页的“设计专题”“文档卡片”“说明表格”只是文档统计，不是 API 数或已实现表数。
- 日常检查不读取历史 baseline；本轮 417 项受保护文件审计用显式 `--baseline` 执行，不成为今后每次文档更新的固定门槛。

## 当前进度

| 项目 | 数量 | 来源 |
| --- | --- | --- |
| 手册基线表 | 36 = 35 业务 + 1 系统表 | `internal/database/migrations/0001` 至 `0006` |
| 扫描表（手册外） | 1 | `internal/database/migrations/0007_scan_catalog.sql` |
| 全仓已有 | 37 | 36 + 1 |
| 合并前 B05-B11 | 63 = 16 + 13 + 1 + 0 + 13 + 13 + 7 | `19cac6e` 的 b05 至 b11 正文 |
| 合表目标 | 34 = 9 + 6 + 1 + 0 + 6 + 10 + 2 | Astra 方案与独立复核；b10/b11 按首次依赖归属 |
| 手册范围合计 | 70 | 36 + 34 |
| 全仓含扫描合计 | 71 | 37 + 34 |

## 交付物

| 文件 | 状态 |
| --- | --- |
| [table-manifest.json](../../api-database/table-manifest.json) | 已冻结：37 张已有表、63 张旧表、34 张目标表；`status=design_only`，正文声明 34/34 |
| [verify_design.py](../../api-database/verify_design.py) | 已完成：日常检查与显式 `--baseline` 审计均通过 |
| [render.py](../../api-database/render.py) | 已完成：首页统计为 36 / 1 / 34 / 70 / 71 |
| [verify_renderer.py](../../api-database/verify_renderer.py) | 已完成：20/20 用例通过 |
| [README.md](../../api-database/README.md)、[foundations.md](../../api-database/foundations.md) | 已完成 |
| [b05-b11-storage-consolidation.md](../../current/b05-b11-storage-consolidation.md) 与 9 个 current 专题顶部替代说明 | 已完成 |
| current/README 与 HANDOFF 本轮入口 | 已完成 |

## 检查

| 检查 | 结果 |
| --- | --- |
| `python3 docs/api-database/verify_design.py --strict --baseline docs/development/2026-09-15-table-consolidation-01/baseline.json` | 通过；34/34 声明、0 待办；手册基线 36（35 业务 + 1 系统表）+ 扫描表 1 = 全仓 37；手册范围 70、全仓含扫描 71；417 项受保护文件 SHA-256 全部匹配 |
| `python3 docs/api-database/render.py --check` | 通过；digest `3fcaa0b15384`（完整 `3fcaa0b1538417331fcdc62037f55d69c69b7f1f7ef3b793a023f0d54e647c09`），首页统计 36 / 1 / 34 / 70 / 71 |
| `docs/api-database/index.html` SHA-256 | `475948a66d800a4db3d35e522130cfd219d3c97ada15b064ec51c343632ab180` |
| `python3 docs/api-database/verify_renderer.py` | 20/20 通过（ADR-0003 坏链接修复后重跑，前一分支失败已消失） |
| `python3 docs/api-database/verify_design_tests.py` | 12/12 通过（引用前轮结果；此后校验器代码未再修改，按父要求不重复运行） |
| `verification-boundaries.json`（父代理） | 417 个受保护文件不变；12 batch HTTP 路由词法集合无增删 |
| 浏览器检查（父代理执行，`scripts/browser-checks.cjs`） | 25/25 通过；[browser-checks.json](./browser-checks.json) 已写入，HTML SHA-256 与冻结值 `475948a66d800a4db3d35e522130cfd219d3c97ada15b064ec51c343632ab180` 相同；父代理已人工查看 `final-b10-1440.png` 与 `final-b10-390.png`，布局、导航与中文正常 |
| `git diff --check` | 通过 |

检查记录见 [render-checks.json](./render-checks.json)。manifest 已冻结为 `status=design_only`。本轮四份独立复核（review-b05-b08、review-b09、review-b10-b11、review-business-plan）均已纳入 analysis 并落实到最终正文；最终设计以正文为准，analysis 只是过程证据。未运行实际 SQL、数据库、服务或外部恢复。检查通过不代表 B05-B11 已实现、已采用或已通过数据库验证，也不代表任何迁移已在 PostgreSQL 执行；正文声明和清单是静态设计事实，不是数据库行为证据。
