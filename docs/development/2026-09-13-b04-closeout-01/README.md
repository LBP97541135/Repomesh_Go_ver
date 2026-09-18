# B04 完整收口 01

状态：`INTEGRATED_LOCAL_VERIFIED`。执行时间为 2026-09-13 UTC。本目录只记录本轮主目录收口。B03 历史证据与旧发布包保持原位，没有覆盖。`businessReady=false`。本结论不是真实 GitHub、真实模型调用、部署或整批业务 `VERIFIED`。

## 授权与状态边界

用户要求按 B03 类比做完整收口，使用既有 TLS 浏览器夹具，写报告后合并 PR。授权范围仍是 D01—D04 与 U04.1—U04.4。没有授权 B05／B06，没有授权真实模型 HTTP，没有授权改写历史 FAIL／NOT_RUN。

B04 由此从 `LOCAL_VERIFIED` 升到 `INTEGRATED_LOCAL_VERIFIED`。进入 B05 仍须另作 D05／D06／C05／C06 采用。`/readyz` 仍为 503。host-executor 仍未实现。B02 外部验收仍为 `PAUSED_BY_USER`。

## 工程检查

| 检查 | 结果 | 证据 |
| --- | --- | --- |
| `go build ./...` | 退出 0 | [eng/eng-go.txt](eng/eng-go.txt) |
| `go vet ./...` | 退出 0 | 同上，以及 P2 修复后复跑退出 0 |
| `go test ./... -count=1` | 259 通过、0 失败、2 跳过 | [eng/go-test-counts.txt](eng/go-test-counts.txt)，SHA-256 `ae0ee61682c7b8be279f8ef74c31925df4acc0addf87f7e3d738a22dd8605ddc`。跳过项是 `TestRootFileOwner` 与 `TestProjectBrowserServer`，均非缺库 |
| P2 修复后 `go test ./...` | 全部包 ok | [eng/post-p2-go-test.txt](eng/post-p2-go-test.txt)，SHA-256 `1356c6048c8758dbc18b662605c11548fc04967dd32064423a868d2b545041a8` |
| `go test -race ./...` | `race_exit=0` | [eng/go-race.txt](eng/go-race.txt)；修复后 models／sources／web 再跑 [eng/post-p2-race.txt](eng/post-p2-race.txt) |
| `npm --prefix web test` | 31 通过、0 失败 | [eng/frontend-test.txt](eng/frontend-test.txt) |
| `npm --prefix web run typecheck` 与 `build` | 退出 0 | 发布脚本输出；产物在 `dist/repomesh-0.4.0-b04-integrated-20260913-r2/` |

## B03 原回归

| 检查 | 结果 | 证据 |
| --- | --- | --- |
| `TestPostgres*`（projects／web／access） | 退出 0 | [eng/b03-regression.txt](eng/b03-regression.txt) |
| 具名 HTTP／事务 | `TestPostgresProjectHTTPContract`、`TestPostgresProjectProcessRestart`、`TestPostgresConfigurationProfileSecretAvailability`、`TestPostgresProjectTransactionsAndAuthorizationInterleaving`、`TestPostgresInheritFollowsPinnedOrCurrentVersion` 退出 0 | [eng/b03-named-http.txt](eng/b03-named-http.txt) |
| 普通浏览器夹具 UI01—UI07 | `passed=true`，白名单 unexpected=0 | [browser-b03-normal/](browser-b03-normal/)，`browser-result.json` SHA-256 `9cbe6b5ab475d817fb9e3d15e74742b12d6d8153ac3a355a6836d58cb3fcb046`，白名单 SHA-256 `59f6a3ae87263c342b3ee8a9743481dd98be670fbe475d360aff9a73044f10c2` |
| 空配置目录 | `EMPTY_CATALOG` 通过 | [browser-b03-empty/](browser-b03-empty/)，`browser-result.json` SHA-256 `61a09dcd5352f0d7294683d533348cbfdf14157aac5187a6958cb43b5267ddde` |

夹具沿用 `TestProjectBrowserServer` 与 `__test/login`。Playwright 使用本机 `playwright-core@1.49.1` 与 Chromium 1148。没有打开真实 GitHub。

## B04 夹具浏览器（U04.4）

最终轮次 [browser-b04-04/](browser-b04-04/)。`browser_exit=0`，`go_test_exit=0`。`browser-result.json` SHA-256 `2a077cd73e62be27cda5d3104ce4289f1c7f184f3114579e0adfb7a1a605ccb5`。白名单 SHA-256 `d069893f1cf88f44d34970838ee01b3ebb3d84debff80dfc9a10d5a07b17c2df`。

覆盖：

- 登录后点「模型设置」，先 hold GET `/api/model-providers` 看到「正在读取供应商…」，释放后看到空列表。
- `drop_response` 丢掉 POST `/api/model-provider-saves` 的 2xx 后，仍按原 `saveId` 恢复到「已保存供应商 …」。写入次数为 1。
- API Key 不进 `sessionStorage`／`localStorage`。会话草稿只有 `secretMode=replace`。
- 已提交后 close 仍返回 `committed`。列表出现「夹具供应商甲」。
- 空槽 close 先 GET 404 `MODEL_SAVE_NOT_FOUND`，再 `closed_without_save`，迟到保存 409 `MODEL_SAVE_CLOSED`。确认文案为「已经保存会返回原结果；尚未保存会阻止这次请求以后保存。它不会撤销已保存配置。」
- Actor B 读 Actor A 的原操作得到 404 `MODEL_SAVE_NOT_FOUND`，页面不出现供应商名。

失败轮次保留，不改写成通过：

- [browser-b04/](browser-b04/) 把夹具丢响应后的 200 当成失败，分类为 `TEST_ASSERTION`。
- [browser-b04-02/](browser-b04-02/) 在 checking 态就断言 404，分类为 `TEST_FIXTURE_TIMING`。
- [browser-b04-03/](browser-b04-03/) 是 P2 修复前的通过轮次，保留对照。

本环境没有 `pwsh`。浏览器与发布脚本是 bash 复现，不改 `scripts/build.ps1` 的拒绝覆盖规则。

## 独立复核

见 [FINAL-INDEPENDENT-REVIEW.md](FINAL-INDEPENDENT-REVIEW.md)。非作者复核先给出 1 项 P2：`writeProvider` 直接插入 `repomesh_projects.profiles`，违反 D01 只经 `CatalogWriter` 写目录。已在 `3127e0a` 改为先 `RegisterModelVersion` 再插入 `model_rows`。复核确认该 P2 关闭，无开放 P0／P1／P2。这不是整批 `VERIFIED`。

## 配套发布

本环境没有 PowerShell 7。`package-release.sh` 按 `scripts/build.ps1` 的目录、ldflags 与拒绝覆盖规则生成新包。

| 包 | 用途 |
| --- | --- |
| `dist/repomesh-0.4.0-b04-integrated-20260913-r1/` | P2 修复前，保留不覆盖 |
| `dist/repomesh-0.4.0-b04-integrated-20260913-r2/` | 收口包 |

r2：`version=0.4.0-b04-integrated-20260913-r2`，`stage=model-sources-local`，`businessReady=false`，`target=linux/amd64`，11 个文件哈希见 [package/release.json](package/release.json)，SHA-256 `7a0627098ddbad78a51a8b6cd2bad42b3e5213de48a90e5a12d46ec1d201fca1`。三个二进制 `--version` 均打印该标签。`dist/` 被 Git 忽略，仓库只保存清单。

## 仍未证明

- 整批 B04 `VERIFIED`
- 真实模型请求或真实 Key
- 真实 GitHub／B02 外部验收
- B05／B06、Issue、运行、AgentTeams、Docker、Python
- `businessReady=true` 或 `/readyz` 200
