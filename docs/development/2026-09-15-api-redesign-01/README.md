# 2026-09-15：删除 B00-B11 目录，按数据库重构方案重写 API 设计

计划见 [PLAN.md](./PLAN.md)，决定记录见 [decisions.tsv](./decisions.tsv)。分支 `cursor/api-redesign-db-plan-2dfd`，起始基线 `main` 提交 `f0b0e68`，后变基到 `ef3b25a`。

## 改动

| 改动 | 位置 |
| --- | --- |
| 删除 `docs/api-database/` 23 个文件 | 归档说明见 [归档记录](../../archive/2026-09-15-api-database-catalog/README.md) |
| 归档 `b05-b11-storage-consolidation.md` 并加归档说明 | `docs/archive/2026-09-15-api-database-catalog/` |
| 改写 9 份 `docs/current/` 专题顶部的“物理承载替代说明”，指向 HTML 方案与新 API 文档 | `docs/current/` |
| 改写 `docs/README.md`、`docs/current/README.md`、`docs/current/HANDOFF.md`、`docs/archive/README.md` 的相关章节，`AGENTS.md` 增加一行入口 | 各文件 |
| 新增 [API 设计](../../current/api-design.md)：`/api/v1` 通用约定、44 张表的资源小节、5 个附录 | `docs/current/api-design.md` |
| 新增校验脚本与红基线 | [scripts/verify_api_doc.py](./scripts/verify_api_doc.py)、[baseline-red.txt](./baseline-red.txt) |

## 检查与结果

从仓库根目录运行：

```bash
python3 docs/development/2026-09-15-api-redesign-01/scripts/verify_api_doc.py
git diff --check
```

- 红基线（删除与新文档之前）：9 项失败，见 `baseline-red.txt`。
- 最终：`方案表数 44；检查项 6；失败 0`。
- `git diff --check`：无输出。
- 校验脚本只做静态核对：表名与字段名来自 HTML 的 `id="t_*"` 与 `class="cn"` 元素，端点来自正文的反引号形式。它不证明接口语义正确，也不证明任何端点已实现。

## 复核

- 调研：两个 grok 4.6 只读子代理分别整理 23 份 ADR 的 API 约束（其中 0021 至 0023 在变基后补读）和现有 `internal/web` 路由、已采用契约、旧目录端点。
- 成稿：claude 子代理按根代理的写作规格成稿；根代理复核后改正枚举来源归类与措辞。
- 独立复核：grok 4.6 子代理按方案、ADR 与契约逐条挑缺陷，给出 59 条；根代理裁定后采纳 30 条修订，驳回 4 条并记录理由。

## 剩余限制

- 新 API 文档是设计，除附录 C 标“已实现”的端点外都未实现；没有为 44 张目标表写迁移文件，也没有从现有 `0001` 至 `0008` 迁移的 40 张表到目标表的映射。
- 方案与 `CONTEXT.md`、ADR 之间的 16 条冲突和 23 条成稿时发现的缺口列在 API 文档附录 E，需要用户或方案作者裁定。最先影响实现的是角色命名倒置、一项目一仓库、无会话实体、Skill 表与 ADR-0009 的关系。
- ADR 正文没有改动。采用方案后 ADR-0009 的替代关系还没有写入。
- 变基到 `main` 提交 `ef3b25a` 后，决策链 ADR 改号为 0023，新增 ADR-0021（pgvector）、ADR-0022（人工检查点决议治理）与 `docs/plan/B11-REPLAN-PROTOCOL.md`；API 文档已同步（改号、4.5 决议规则、4.3 完整快照与 `POST /api/v1/plans/{planId}/apply`、`tasks.status` 增加 `superseded`）。`feat/replan-mainline` 分支的迁移 0009 列未与本文核对。
- `docs/development/` 中 2026-09-15 的三份旧制作记录仍指向已删除的 `docs/api-database/` 路径，按文档维护规则保留原样。
