# 2026-09-15：删除 B00-B11 API 与数据库目录

本次按用户决定删除 `docs/api-database/`，并把 [Go 版数据库重构方案](../../RepoMesh_Go版数据库重构方案.html) 定为正式数据库方案。对应的接口设计重写为 [API 设计](../../current/api-design.md)。制作与校验证据见 [本轮记录](../../development/2026-09-15-api-redesign-01/README.md)。

## 删除了什么

`docs/api-database/` 的 23 个文件，最后版本见 `main` 提交 `f0b0e68`：

- 正文 13 份：`foundations.md`、`b00.md` 至 `b11.md`。
- 生成结果与模板 5 份：`index.html`、`templates/app.js`、`templates/index.html.tmpl`、`templates/style.css`、`README.md`。
- 工具与清单 5 份：`render.py`、`verify_design.py`、`verify_design_tests.py`、`verify_renderer.py`、`table-manifest.json`。

这些文件按 B00 至 B11 批次组织 API 与表设计，并以 63 张候选表合到 34 张为前提。新方案把全库重排为 7 个功能方向 44 张目标表，批次视角与合表口径都不再适用。

## 一并归档的专题

[B05-B11 物理合表与表清单](b05-b11-storage-consolidation.md) 从 `docs/current/` 移到本目录。它依赖 `table-manifest.json` 与批次正文，正文顶部已加归档说明，链接改为纯文本。`docs/current/` 中 9 份专题顶部的“物理承载替代说明”改为指向新方案与新 API 设计。

## 仍保留在原处的材料

`docs/development/2026-09-15-api-db-catalog-01/`、`docs/development/2026-09-15-table-consolidation-01/` 和 `docs/development/2026-09-15-table-consolidation-main-sync.md` 是当时的制作记录。它们引用的 `docs/api-database/` 路径已不存在，按文档维护规则保留当时路径，不改写历史记录。

## 什么没有改变

- 已实现的 B01-B04 迁移（`internal/database/migrations/0001` 至 `0008`，40 张表）与代码没有改动。新方案中的 44 张目标表尚未有迁移文件，也没有从现有 40 张表到目标表的迁移映射。
- 已采用的浏览器契约（项目、Issue 创建、消息与澄清、认证、模型设置）继续由 `docs/current/` 各自的专题维护。新 API 设计给出了它们到新资源的对应关系，采用与替代关系以 [API 设计](../../current/api-design.md) 的说明为准。
