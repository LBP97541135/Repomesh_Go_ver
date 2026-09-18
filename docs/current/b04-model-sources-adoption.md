# B04 精确采用记录

日期：2026-09-13。用户原话：“推进B04,并告诉我验收标准是什么才能进入B05”。本记录采用 D01—D04 及 B04 六个 HTTP／Key UI／schema 1 范围。不采用 D05—D08、S06、C05、C06、P9。

状态：`DESIGN_ADOPTED`（B04 范围）。实现为 `IMPLEMENTED`。本地集成为 `INTEGRATED_LOCAL_VERIFIED`。用户已确认该授权范围结束。S01 到 S12 已有真实 PostgreSQL 证据，并有夹具浏览器、B03 原回归、配套发布、收口后 P1／P2 修复。不能写成整批 `VERIFIED`。

## 采用

| 决定 | 采用内容 | 替代 |
| --- | --- | --- |
| D01 | `projects` 拥有目录和项目固定配置。`models`／`sources` 只经 `CatalogWriter` 写目录 | 不迁解析器，不建通用配置包 |
| D02 | 独立 `operation-input` vault 做精确比较。不保存裸 Key 哈希 | 替代内部稿 §3.1 MAC 方案 |
| D03 | `Prepare` 在业务锁外预扣。`InsertPrepared` 加入调用方事务。预扣不退款。认证 `Seal`／`Open`／`Destroy` 保留 | 替代 Seal 独立提交后清孤儿 |
| D04 | schema 1 每项单 owner 执行元数据。新默认必须 `pinned_version`。旧 null 保留 B03 inherit→head | 不采用 S03 全量多 owner／预算／出站 |

HTTP 仅六端点：`GET /api/model-providers`、`GET /api/model-providers/{id}`、`GET /api/model-providers/{id}/versions/{revision}`、`POST /api/model-provider-saves`、`GET /api/model-provider-saves/{saveId}`、`POST /api/model-provider-saves/{saveId}/close`。字段唯一来源是 [模型浏览器稿](model-settings-browser-api-draft.md) 前六项。部署导入只走 `repomesh-web sources import|result`，无浏览器导入。

Key UI 沿供应商分栏与小弹窗。发出即清内存。未知只查原 `saveId` 或明确 close。close 与 save 争同一槽。空槽 `closed_without_save` 阻挡迟到保存。成功不能被 close 撤销。

## 未采用

D05 次数预算与外发、D06 专用应用、C05／C06、D07／P9、D08 Issue 事务、S06 新建仓库门槛。B05 实施前须另作采用。

## 实现与本地验证

U04.1 到 U04.4 已落地。迁移 `0005_models.sql`，收口后另加 `0006_complete_save_vault.sql`。六个模型 HTTP 端点。`repomesh-web sources import|result`。Key 页面发出即清；校验未通过的保存留在设置页并显示 `VALIDATION_FAILED`。新建模型目录行只经 `CatalogWriter.RegisterModelVersion`。合入 `main` 后的验收见 [B04 验收报告](../development/2026-09-13-b04-acceptance-01/README.md)。收口证据见 [B04 收口 01](../development/2026-09-13-b04-closeout-01/README.md)。收口后修复见 [收口后修复 01](../development/2026-09-13-b04-validation-stay-01/README.md)。当前头复跑 `go test ./...` 为 260 通过、0 失败、2 跳过。外部模型与真实 GitHub 仍未跑。`businessReady=false`。
