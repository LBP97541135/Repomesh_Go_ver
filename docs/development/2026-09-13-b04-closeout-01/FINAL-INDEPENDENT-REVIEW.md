# B04 收口独立复核

日期：2026-09-13。复核者不是本批主要实现者。范围只覆盖已采用的 D01—D04 与 U04.1—U04.4。没有把 `LOCAL_VERIFIED` 写成整批 `VERIFIED`。

## 材料

读完采用记录、ACCEPTANCE S01—S12、B04 设计，以及 `internal/models/`、`internal/sources/`、`internal/secrets/` 的 Prepare／InsertPrepared、`internal/web/models.go`、`cmd/repomesh-web` sources CLI、`0005_models.sql`、`web/src` 模型页。另在本环境复跑 `go test ./...`（259／0／2）与前端 31／31。

复核代理：`bc-20428d58-e9ad-5f71-bdf2-40728eb19b48`。

## 首轮

裁决：FAIL。开放 1 项 P2，无 P0／P1。

P2：`internal/models/service.go` 的 `writeProvider` 在新建模型行时直接 `INSERT INTO repomesh_projects.profiles`，再调用 `CatalogWriter.RegisterModelVersion`。D01 要求 models／sources 只经 `CatalogWriter` 写目录。该写入与目录锁、同一事务回滚兼容，不丢数据、不泄 Key、不破坏恢复，因此不是 P0／P1。它破坏单写者边界，必须在宣称 `INTEGRATED_LOCAL_VERIFIED` 前修掉。

已核对且符合采用范围的项：D02 独立 vault 精确比较、D03 锁外 Prepare／事务内 InsertPrepared／预扣不退、D04 新默认 `pinned_version` 与旧 null head、S01—S12 的真实 PostgreSQL 测试、六端点且无浏览器导入、前端不持久化 Key。

明确不作为缺陷：B05／B06、D05—D08、真实 GitHub、真实模型 HTTP、`businessReady=false`。

## 修复与复核

提交 `3127e0a` 删除 models 对 `repomesh_projects.profiles` 的直接写入。新建行先 `RegisterModelVersion`，再插入 `model_rows`，以满足 FK。`internal/models` 中不再出现该 INSERT。

复核对 `writeProvider` 的裁决：PASS，无开放 P0／P1／P2。复核者未把本文件当成整批 `VERIFIED`。

修复后本环境再跑 models／sources／web／projects／secrets、完整 `go test ./...`、相关 `-race`，以及 B04 夹具浏览器 `browser-b04-04`，均退出 0。
