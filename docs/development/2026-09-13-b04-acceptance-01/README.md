# B04 验收报告

日期：2026-09-13 UTC。对象是已合入 `main` 的授权范围 D01—D04 与 U04.1—U04.4。`main` 头为 `621592d`（`Merge pull request #3`）。本报告不把历史 FAIL／NOT_RUN 改写成通过，也不把本批标成整批 `VERIFIED`。

状态：`INTEGRATED_LOCAL_VERIFIED`。授权范围已结束。`businessReady=false`。

## 合并事实

| 项 | 值 |
| --- | --- |
| 收口 PR | https://github.com/catbobyman/Repomesh_Go_ver/pull/2 ，合并提交 `416e096` |
| 收口后修复 PR | https://github.com/catbobyman/Repomesh_Go_ver/pull/3 ，合并提交 `621592d` |
| 当前 `origin/main` | `621592d0ceb4a29d8ea8f9f7b79f73b5ea8a1c13` |

PR #2 把 B04 从 `LOCAL_VERIFIED` 收到 `INTEGRATED_LOCAL_VERIFIED`。PR #3 修掉收口合并后另一次独立复核发现的 1 项 P1 与 3 项 P2，并记录授权范围结束。本报告在该头上复跑工程检查。

## 结论

已采用的 B04 可以按本地集成验收结束：

- 六个模型 HTTP 端点、Key 发出即清、原操作查询／close、schema 1 部署导入均已落地。
- S01—S12 有真实 PostgreSQL 证据。
- U04.4 夹具覆盖丢响应恢复、空槽 close、跨 actor 404。
- U04.2 校验失败留在设置页，不再跳到不存在的原操作。
- B03 普通／空目录夹具在收口时仍通过，本轮未重跑浏览器以免覆盖旧证据。

不能写成：

- 整批业务 `VERIFIED`
- 真实模型请求或真实供应商 Key 可用
- 真实 GitHub／B02 外部验收通过
- B05／B06 已采用或已实施
- `businessReady=true` 或 `/readyz` 200

进入 B05 仍须另作 D05／D06／C05／C06 采用。

## 采用范围

| 编号 | 结果 | 说明 |
| --- | --- | --- |
| D01 | 通过 | 新建模型目录行只经 `CatalogWriter.RegisterModelVersion`。收口复核的直接插 `profiles` P2 已在 `3127e0a` 关闭。 |
| D02 | 通过 | 独立 `operation-input` vault 做精确比较。`0006` 要求 replace 有 vault、keep 无 vault。 |
| D03 | 通过 | `Prepare` 在业务锁外，`InsertPrepared` 进调用方事务，预扣不退。 |
| D04 | 通过 | schema 1 单 owner；新默认 `pinned_version`；旧 null 仍 inherit→head。 |
| D05—D08 | 未采用 | 不在本报告通过范围内。 |

HTTP 仅六端点。部署导入只走 `repomesh-web sources import|result`。

## U04

| 单元 | 结果 | 证据 |
| --- | --- | --- |
| U04.1 事务内秘密 | 通过 | `internal/secrets` PostgreSQL 用例；`Prepare`／`InsertPrepared`；认证回归随完整 `go test` |
| U04.2 保存／close／目录 | 通过 | `internal/models` 与 `internal/web/models_test.go`；校验失败留页见下 |
| U04.3 执行导入 | 通过 | `internal/sources` 与 CLI `cmd/repomesh-web/sources.go` |
| U04.4 六端点与浏览器恢复 | 通过 | 收口 [browser-b04-04](../2026-09-13-b04-closeout-01/browser-b04-04/)；修复后 [browser-b04-validation](../2026-09-13-b04-validation-stay-01/browser-b04-validation/) |

U04.2 修复前，`http://` 等 `422 VALIDATION_FAILED` 会跳到从未建槽的恢复页，密钥已清、表单字段丢失。修复后留在 `/settings/models`，alert 为「这次保存没有受理…」，名称／模型仍在，API Key 已空。

## S01—S12（真实 PostgreSQL）

| ID | 结果 | 主要测试 |
| --- | --- | --- |
| S01 | 通过 | `TestPostgresTwentyWaySameSave` |
| S02 | 通过 | `TestPostgresDifferentInputConflictComparesVault` |
| S03 | 通过 | `TestPostgresSaveRollsBackAtEveryPhase` |
| S04 | 通过 | `TestPostgresSaveIdempotentAndClose`、`TestPostgresCloseVersusLateSave` |
| S05 | 通过 | `TestPostgresPreparedRootRetiredBeforeInsert` |
| S06 | 通过 | `TestPostgresReplayWithMissingVaultRoot` |
| S07 | 通过 | `TestPostgresModelSaveLostResponseAndRestart`；夹具 `drop_response` |
| S08 | 通过 | `TestPostgresRemoveSaveResultKeepsBusinessSecret`、`TestPostgresRemoveRejectedSaveResultDestroysVault` |
| S09 | 通过 | `TestPostgresImportReplayAndPinnedDefault`、`TestPostgresImportRelistedOlderVersionKeepsHead` |
| S10 | 通过 | `TestPostgresConcurrentImportAndProviderSave`、`TestPostgresImportHoldsOwnersWhileOtherOwnerSaves` |
| S11 | 通过 | `TestPostgresDeploymentRoleRejected` |
| S12 | 通过 | `TestPostgresInheritFollowsPinnedOrCurrentVersion` |

上述用例在本报告复跑的 `go test ./...` 中全部包含，包结果均为 `ok`。设计表原文仍见 [ACCEPTANCE.md](../2026-09-13-b04-b06-design-01/ACCEPTANCE.md)，该文件保持设计映射，不回写成设计当时已跑。

## 本头复跑

在 `621592d`、`REPOMESH_TEST_DATABASE_URL` 指向本机开发 PostgreSQL 时：

| 检查 | 结果 | 证据 |
| --- | --- | --- |
| `go vet ./...` | 退出 0 | [eng/go-vet-test.txt](eng/go-vet-test.txt)，SHA-256 `f2a83de7c53e22c6e6d1ba847d71d6e95663a38f254afa999d295433cbc93430` |
| `go test ./... -count=1` | 260 通过、0 失败、2 跳过 | [eng/go-test-counts.txt](eng/go-test-counts.txt)，SHA-256 `bcb1065f25cadee9c06a778fbf25110bd68e88e67f355567f1b7c4b29bc927d7`。跳过 `TestRootFileOwner`、`TestProjectBrowserServer`，均非缺库 |
| `npm --prefix web test` | 32 通过、0 失败 | [eng/frontend-test.txt](eng/frontend-test.txt)，SHA-256 `baec188e37f2453e28ff28dc0957974d0592e619d326f7d97ae99e2c1c037225` |
| `npm --prefix web run typecheck` | 退出 0 | [eng/frontend-typecheck.txt](eng/frontend-typecheck.txt)，SHA-256 `e0de5c21e986411ad3bb449879f4fab332a97835f46b73aac53105b6b82df4dc` |

本轮未重跑 `-race`、未重打发布包。收口 race 与 r2 包记录仍在 [收口 01](../2026-09-13-b04-closeout-01/README.md)。`dist/` 被 Git 忽略。

## 浏览器夹具

| 轮次 | 结果 | 说明 |
| --- | --- | --- |
| [收口 browser-b04-04](../2026-09-13-b04-closeout-01/browser-b04-04/) | `browser_exit=0` | U04.4 丢响应、空槽 close、跨 actor。`browser-result.json` SHA-256 `2a077cd73e62be27cda5d3104ce4289f1c7f184f3114579e0adfb7a1a605ccb5` |
| [修复 browser-b04-validation](../2026-09-13-b04-validation-stay-01/browser-b04-validation/) | `browser_exit=0` | 增加 U04.2 `422 VALIDATION_FAILED` 留页；原 U04.4 仍过。网络含 `422:VALIDATION_FAILED=1`。`browser-result.json` SHA-256 `f63989fcf1ae17c14d024c72f9e41e14df905ed5d506c32025449e3480c42af4` |
| [收口 B03 普通](../2026-09-13-b04-closeout-01/browser-b03-normal/) | 通过 | UI01—UI07，未在本头重跑 |
| [收口 B03 空目录](../2026-09-13-b04-closeout-01/browser-b03-empty/) | 通过 | 未在本头重跑 |

失败轮次保留：`browser-b04` 为 `TEST_ASSERTION`，`browser-b04-02` 为 `TEST_FIXTURE_TIMING`。没有打开真实 GitHub，没有发送真实模型请求。

## 独立复核

| 复核 | 裁决 | 处理 |
| --- | --- | --- |
| 收口 [FINAL-INDEPENDENT-REVIEW.md](../2026-09-13-b04-closeout-01/FINAL-INDEPENDENT-REVIEW.md) | 先 1 项 P2，修复后 PASS | `writeProvider` 只经 `CatalogWriter`。不改写成当时已无后续缺陷 |
| 收口合并后 [Independent B04 review](bc-7ec07cce-0d1b-5468-8050-6d7bf4a6cb51) | FAIL：1 P1 + 3 P2 | PR #3 已修：校验留页、`0006` vault 不变量、拒绝回执可清理、锁序注释 |

本报告不再声称「收口当时独立复核已覆盖全部后续缺陷」。

## 仍未证明

- 整批 B04 `VERIFIED`
- 真实模型 HTTP、真实 Key、计费
- 真实 GitHub、B02 外部验收
- B05／B06、Issue、运行、AgentTeams、Docker、Python 仓库分析
- `businessReady=true`、`/readyz` 200、host-executor 已实现
