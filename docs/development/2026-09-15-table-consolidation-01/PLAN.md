# B05-B11 物理合表文档、清单与校验工具计划

用户已授权对 B05-B11 的物理存储设计做合表：原有 63 张候选或提案表压到 34 张，B01-B04 的现有表、已采用逻辑、API 和字段语义保持不变。合表方案由 Astra 提供并负责裁决；本代理负责公共文档、生成器统计、机器可校验清单及其校验工具，不改变合表决定。

状态：已完成并冻结（`design_only`）。34/34 正文声明齐备，最终 HTML 已生成；交付与检查结果写入 [制作记录](./README.md)，浏览器25/25已通过且证据已回填。

## 计数口径

数量以迁移源码的 `CREATE TABLE` 清点和父代理核验为准，不在文档里手工加总。

| 口径 | 数量 | 依据 |
| --- | --- | --- |
| 手册基线（B01-B04） | 36 | `0001` 至 `0006` 迁移：35 张业务表加 1 张系统表 `public.repomesh_schema_migrations` |
| 扫描表（手册外） | 1 | `0007_scan_catalog.sql`：`repomesh_scan.repositories` |
| 全仓已有 | 37 | 36 + 1 |
| B05-B11 设计表 | 34 | B05 9、B06 6、B07 1、B08 0、B09 6、B10 10、B11 2 |
| 手册范围合计 | 70 | 手册基线 36 + 设计 34 |
| 全仓含扫描合计 | 71 | 36 + 1 + 34 |

## 交付条件

| 交付物 | 完成条件 |
| --- | --- |
| [table-manifest.json](../../api-database/table-manifest.json) | 唯一机器可校验清单。含 37 张已有表（分手册基线与扫描表两组）、63 张旧表及其快照来源、34 张目标表与批次归属；`business_plan_versions` 标为 `new_required_dependency` 并链接本目录 `analysis/business-plan-versions.md`，路径全部仓库相对 |
| [verify_design.py](../../api-database/verify_design.py) | 只用 Python 3 标准库；日常检查覆盖旧项覆盖、目标存在、去重、批次归属、正文声明与 HTML 统计口径；本轮范围保护审计用显式 `--baseline` 核对受保护文件 SHA-256 |
| [render.py](../../api-database/render.py) | 首页显示手册基线、扫描表、设计表、手册范围合计与全仓合计；文档计数标为说明统计；manifest 进入生成摘要 |
| [README.md](../../api-database/README.md) 与 [foundations.md](../../api-database/foundations.md) | 写明授权边界、物理合表规则、声明格式、计数口径和日常检查与本轮审计的差别 |
| [b05-b11-storage-consolidation.md](../../current/b05-b11-storage-consolidation.md) 与 current 专题顶部替代说明 | 只把 B05 与物理承载、锁序指向新专题；B01-B04、已采用逻辑契约、B05 候选与历史证据状态不变 |
| 本目录 | PLAN、制作记录、决定记录、Astra 方案副本与四份独立复核（含 review-business-plan.md）、检查报告；baseline 与 verification JSON、浏览器脚本与截图由父代理独占 |

## 范围与所有权

| 范围 | 所有者 |
| --- | --- |
| B05-B11 合表设计、每批目标表与保留理由 | Astra；本代理按已交付方案填写 manifest |
| `docs/api-database/README.md`、`foundations.md`、`render.py`、`verify_renderer.py`、`templates/` | 本代理 |
| `docs/api-database/table-manifest.json`、`verify_design.py` | 本代理（新增） |
| `docs/current/b05-b11-storage-consolidation.md` 与 9 个相关 current 专题顶部替代说明 | 本代理 |
| `docs/current/README.md`、`HANDOFF.md` | 本代理只追加本轮入口 |
| 本目录 PLAN、README、decisions、analysis、检查报告 | 本代理 |
| 本目录 `baseline.json`、`verification*.json`、`scripts/browser-checks.cjs`、`browser-checks.json`、`screenshots/` | 父代理；本代理不覆盖 |
| `b00.md` 至 `b11.md` 正文与表声明 | 章节作者；本代理不改正文 |
| 产品代码、SQL 迁移、旧证据、其余 current 专题 | 本轮不动 |

## 执行步骤

| 步骤 | 状态 |
| --- | --- |
| 1. 建立本轮计划、制作记录与公共规则 | 已完成 |
| 2. 确认 manifest 字段与正文声明规范 | 已完成（可见正文行） |
| 3. 从迁移清点 37 张已有表并分组 | 已完成 |
| 4. 从 `19cac6e` 快照提取 63 张旧表与逐行来源 | 已完成 |
| 5. 填入 Astra 方案与复核的 34 张目标表与批次归属 | 已完成 |
| 6. 把 Astra 方案副本保存到 `analysis/` 并去掉 `/tmp` 依赖 | 已完成 |
| 7. 编写 verify_design.py 日常检查与本轮审计模式 | 已完成：日常检查与 `--strict --baseline` 审计均通过 |
| 8. render.py 接入 manifest 与统计口径，重生成 index.html | 已完成：digest `3fcaa0b15384`，SHA-256 `475948a6…ab180` |
| 9. 扩展 verify_renderer.py 用例 | 已完成：20/20 用例通过 |
| 10. 写 new current 专题与 9 个专题顶部替代说明，追加 current 入口 | 已完成 |
| 11. 运行检查并记录结果 | 已完成：render --check、verify_design --strict --baseline、verify_renderer 20/20；verify_design_tests 引用前轮 12/12 |
| 12. 父代理浏览器检查并回填证据 | 已完成：25/25 通过，HTML SHA-256 与冻结值一致 |

## 检查清单

日常检查不读取历史 baseline，也不把全部产品代码纳入文档更新门槛：

```bash
python3 docs/api-database/verify_design.py
python3 docs/api-database/render.py --check
python3 docs/api-database/verify_renderer.py
python3 docs/api-database/verify_design_tests.py
```

本轮交付的范围保护审计显式给出 baseline：

```bash
python3 docs/api-database/verify_design.py --baseline docs/development/2026-09-15-table-consolidation-01/baseline.json
```

manifest 冻结后把 `status` 从 `in_progress` 改为 `design_only`，`verify_design.py --strict` 同时要求所有正文声明到位。检查只覆盖静态清单、源码清点和文档生成，不执行数据库迁移，也不证明数据库行为已验证。
