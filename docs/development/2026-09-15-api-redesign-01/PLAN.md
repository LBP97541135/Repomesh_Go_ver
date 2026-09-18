# 删除 B00-B11 目录并按数据库重构方案重写 API 设计的执行计划

用户决定：删除 `docs/api-database/`，把 [Go 版数据库重构方案](../../RepoMesh_Go版数据库重构方案.html) 定为正式数据库方案，并按 `CONTEXT.md` 与全部 ADR 重新设计 API，写成一份新文档。调研由 grok 4.6 子代理承担，成稿由 claude 子代理承担，根代理拥有设计与复核。

## 完成判据

1. `docs/api-database/` 不存在；`docs/current/`、`docs/plan/`、`docs/adr/` 与根目录文档没有指向该目录的链接。
2. `docs/README.md`、`docs/current/README.md`、`docs/current/HANDOFF.md`、`AGENTS.md` 指向 HTML 方案与新 API 文档。
3. `docs/current/api-design.md` 存在，且 [verify_api_doc.py](./scripts/verify_api_doc.py) 通过：方案 44 张表全部映射到资源或标为内部表；正文每个端点的顶层资源出现在映射表；每张表小节写全方案字段；23 份 ADR 全部被引用；相关文档本地链接可解析。
4. `git diff --check` 无空白问题。

## 执行流程

1. Read the Principles section of the poteto-mode skill。
2. Phase A: Frame。核对方案表数、现有迁移表数与旧目录依赖关系，写出上面的完成判据。
3. Phase B: Design the workflow。先写校验脚本并记录红基线；再删旧目录、归档合表专题、改索引；再写新文档；再跨模型复核；最后补本记录。
4. Phase C: Run the loop。每个单元单独提交，提交前跑校验脚本。
5. Phase D: Keep the audit trail。[decisions.tsv](./decisions.tsv) 记录影响范围与采用边界的决定。
6. Phase E: Verify and hand back。校验脚本全绿，交付说明写清改动、检查与剩余限制。

## 范围与吞吐检查点

- 先行阻塞步骤：两个只读调研（ADR 约束、现有路由与契约）和校验脚本，都在写文档之前完成。
- 独立工作流：删除与改链接（根代理）和新文档成稿（子代理）分别进行，前者先提交，后者只写 `docs/current/api-design.md` 一个文件。
- 共享可变状态：`docs/current/` 的索引与 9 份专题顶部说明只由根代理修改；子代理不碰。
- 最小分解：一个写作子代理成稿，一个不同模型家族的复核子代理找缺陷，根代理裁定后由写作子代理落实。

本轮不实施产品功能、不写迁移文件、不改 ADR 正文、不启动服务。方案与 CONTEXT.md、ADR 之间的冲突记入 API 文档附录 E，等用户裁定。
