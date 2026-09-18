# 合表文档随 origin/main 决策链三表的统计同步

状态：已完成。origin/main `6899665`（Merge branch 'feat/decisionchain-complete'）带来 `internal/database/migrations/0008_decision_chain.sql` 和 3 张手册外表，本目录的表清单与首页统计因此从 37/71 同步到 40/74。

设计不变：B05-B11 仍是 63 张候选或提案表合到 34 张，手册基线仍是 36 张，手册范围合计仍是 70 张。0008 的 3 张决策链表按“手册外”单独成组，不计入这 34 张，也不改变 B05-B11 的候选、提案、未实施与验收状态。

## 上游事实

- `0008_decision_chain.sql` 建 3 张表：`public.decision_chain_nodes`（第 17 行）、`public.decision_embeddings`（第 55 行）、`public.feature_settings`（第 70 行），并 `CREATE EXTENSION IF NOT EXISTS vector`。
- 本仓库与 origin/main 的差异只在文档：`git diff origin/main -- cmd internal web configs scripts go.mod go.sum` 为空；`docs/api-database/b00.md` 至 `b04.md` 与 origin/main 字节一致。产品代码、SQL 迁移与 `docs/development` 既有证据都不在本次改动内。
- 本次是授权范围内的上游同步，不是决策链设计评审，也不复核 0008 的表结构、索引或 API。

## 统计变化

| 口径 | 同步前 | 同步后 |
| --- | --- | --- |
| 手册基线（`0001` 至 `0006`） | 36 = 35 业务 + 1 系统表 | 36 = 35 业务 + 1 系统表 |
| 手册外扫描表（`0007`） | 1 | 1 |
| 手册外决策链表（`0008`） | 未登记 | 3 |
| 既有表合计 | 37 | 40 |
| B05-B11 设计表 | 34 | 34 |
| 手册范围合计 | 70 | 70 |
| 全仓含扩展合计 | 71 | 74 |

## 改动范围

| 文件 | 改动 |
| --- | --- |
| [table-manifest.json](../api-database/table-manifest.json) | `existing` 增加 `decision_chain` 分组（显式 migration 清单、3 张表名、`expected_count`），`existing.tables` 增加 3 行，`expected_total` 37 → 40，`totals.repo_scope_total` 74 |
| [verify_design.py](../api-database/verify_design.py) | 已有表改按显式分组清单分类；漏登记迁移或未归组表直接失败；HTML 统计标签增加决策链表并改名为「全仓含扩展合计」 |
| [render.py](../api-database/render.py) | 首页统计增加「决策链表（手册外）3」，「全仓含扩展合计」74，副标题同步口径 |
| [verify_design_tests.py](../api-database/verify_design_tests.py) | 覆盖新分组：未登记迁移拒绝、0008 未归组、分组计数与表名不符、表行分组错标 |
| [verify_renderer.py](../api-database/verify_renderer.py) | 覆盖决策链统计与全仓含扩展合计跟随 manifest |
| [README.md](../api-database/README.md)、[foundations.md](../api-database/foundations.md) | 计数口径与分组说明同步 |
| [b05-b11-storage-consolidation.md](../current/b05-b11-storage-consolidation.md)、[HANDOFF.md](../current/HANDOFF.md)、[current/README.md](../current/README.md) | 合到 33 张的笔误改为 34 张，并补 40/74 口径与本次同步入口 |

## 校验结果

2026-09-15 在工作树执行，四条命令全部通过：

```text
python3 docs/api-database/render.py --check
OK: index.html 与 13 个内容源一致（digest 6a8e9f98b54b）

python3 docs/api-database/verify_design.py --strict
清单: 旧表 63 张 → 目标 34 张；已有手册基线 36 张（业务 35 + 系统 1）+ 扫描表 1 + 决策链表 3 = 全仓 40
统计口径: 手册范围 70 = 36 + 34；全仓含扩展 74
正文声明: 已声明 34 / 待声明 0
OK: 清单结构、迁移事实、快照来源、正文声明与统计口径通过（待办 0）

python3 docs/api-database/verify_design_tests.py     17/17 用例通过
python3 docs/api-database/verify_renderer.py         21/21 用例通过
```

生成的 `index.html` 为 LF、1 136 920 字节，内容摘要 `6a8e9f98b54bf5a7d6ea828000f0b2793378bc3a9231f309691d0ba9960d660e`，文件 SHA-256 `8d8dc8aa16fe98751ee234844449114def288b697208ca86b2431b5aec6e2384`。

父代理的独立浏览器检查（2026-09-15，临时脚本与原始结果 `/tmp/repomesh-main-sync-browser.json`，不入库）核对了同一份产物：实际 HTML 的 SHA-256 为 `8d8dc8aa16fe98751ee234844449114def288b697208ca86b2431b5aec6e2384`，与上面的文件摘要一致；首页统计读到 36／1／3／34／70／74；1440 与 390 两种视口没有横向溢出，也没有 `pageerror`。该检查只覆盖页面呈现与统计口径，不证明数据库行为。

`verify_design_tests.py` 的 `missing-declaration-strict` 用例在改动前就是红的：用例假设“进行中状态”不因待办失败，而清单 `status=design_only` 时校验器强制 strict。已确认红灯早于本次同步（在 `21f266b` 的独立 worktree 里，`positive-control` 与 `missing-declaration-strict` 两条失败，10/12）。本次只把该用例的阶段清单显式设成 `in_progress`，保留它要验证的语义；`design_only` 下的 `--strict` 仍由 `positive-control` 覆盖。

## 边界

- 本次不使用历史 baseline。`docs/development/2026-09-15-table-consolidation-01/baseline.json` 是合表轮次 417 项受保护文件的裸字节 SHA-256，对应 `0001` 至 `0007` 的上游状态；远端 `0008` 是授权的上游变更，在旧 baseline 下判失败没有意义。该目录的 `PLAN.md`、`baseline.json`、证据、截图与摘要按历史快照保留，不因本次同步改写。
- 只做静态文档检查，不执行数据库迁移，不证明 pgvector 依赖、HNSW 索引或任何决策链行为已在 PostgreSQL 验证。
- 本次没有改动产品代码与 SQL，因此没有重跑 Go、前端或合表前的历史实验。
