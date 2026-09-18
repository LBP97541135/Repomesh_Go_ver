# B03 主目录集成 01

状态：`INTEGRATED_LOCAL_VERIFIED`。执行时间为 2026-09-12 PDT。本目录只记录本轮主目录集成；B03 worktree 的 6.7 MB 历史证据和旧发布包保持原位，没有复制或覆盖。主目录 PostgreSQL、前端、真实浏览器与配套发布验证已经通过，[最终独立复核](FINAL-INDEPENDENT-REVIEW.md)确认无开放 P0/P1/P2。`businessReady=false`，本结论不是实际 GitHub 外部验收、部署或整批业务 `VERIFIED`。

## 授权与状态边界

用户已解除 B03 等待 B02 VERIFIED 的旧门槛，并决定以后真实账号验证只使用主账号 A。B02 外部验收保持 `PAUSED_BY_USER` 和 `IN_PROGRESS`。`second-account-02` 的 LIVE-05 历史 `FAIL`、LIVE-08 `NOT_RUN` 与 `RESTORE_IN_PROGRESS` 保留。未来跨账号 LIVE-05 和 LIVE-08-USER-READ 为 `DEFERRED_BY_USER`，不计为通过。

B03 来源 worktree 为 `LOCAL_VERIFIED`。本轮没有把旧 worktree 验证继承为主目录验证，而是在 I0/I1 文件集成后为 I2 至 I4 生成全新证据。真实 GitHub、账号、B04、Issue、运行、AgentTeams、Docker 和 Python 集成均未执行；`businessReady=false`。

## I0 快照

- 目标：`/home/xubohan/projects/Repomesh_Go_ver`，分支 `main`，HEAD `ad9a49b2fe3b5fe743a65c838ded6cb022e79363`。
- 来源：`/home/xubohan/projects/Repomesh_B03`，分支 `codex/b03-project-management`，HEAD `ad9a49b2fe3b5fe743a65c838ded6cb022e79363`。
- 基线清单：427 个路径，SHA-256 `027af830da5f8b948687e0dd78b527872a2668b9c81bfede41f9491e0997361d`。
- 普通文件归档：425 项，归档 SHA-256 `9d32afb894abd4aa4e0e732029428f5d8502b053cd35ac4a43126ea7023403e6`。
- 两份状态文档归档：2 项，归档 SHA-256 `6f7fafe1bc69105b182602ee20768dd3b7489e5592a46641b43e73477f6c0d31`。
- 两份归档共同解到临时目录后逐项对照 427 项清单，实际不匹配为 0。
- 按旧三方统计范围重算：459 个路径，399 个三方未变、58 个 B03-only、0 个 main-only、0 个双方同变且相同、2 个双方变更且不同、0 个删除。
- 将 `docs/archive/` 也按最新保护规则排除后：423 个路径，363 个三方未变，其余分类不变。
- 60 个候选由 58 个 B03-only 路径加 `HANDOFF.md` 与 `IMPLEMENTATION-PLAN.md` 组成。目标中 28 个路径原先存在，32 个路径原先不存在。
- 执行瞬间确认 `docs/development/2026-09-12-b03-integration-01/` 不存在后才创建本目录。

逐路径 base、来源、目标哈希、权限和存在性见 [I0 候选清单](i0-candidates.tsv)。

## 回滚材料

私有回滚目录为 `/home/xubohan/.local/state/repomesh-b03-integration-01.QX8dux`，目录权限 `0700`，文件权限 `0600`。它包含 28 个既有候选路径的精确原字节、60 项允许清单、32 项原先不存在清单，以及两边完整的执行前 Git 状态。未纳入认证配置、秘密正文、数据库、浏览器 profile、发布包或历史证据正文。

| 材料 | SHA-256 |
| --- | --- |
| `target-preimage.tar.gz` | `c06edcbad0fb05309a1be4547a48dfea9cae19d7a08760a68d06638228b29612` |
| `allowed-candidates.txt` | `a59c5c3dfba98e8bcd0c44b0ead114358decc38df449fa0935681304e0d5c7ce` |
| `rollback-files.txt` | `b4eb56345772c841dad9e476e5eacb1226310d6ec1b9d96dabf3afa3fd23806b` |
| `absent-before.txt` | `d6b2fdfba88c1459d3725113411e4e8d0f2a103d372310781180f57de18b14cb` |
| `target-git-status-pre.txt` | `952562d51eaffe1e62ab84a34b25f872702d15803763ee8cced0b23c4e19ee90` |
| `source-git-status-pre.txt` | `6bb719fe3bbfd3457c9ec92ae9e6c7e17b6ef845dd0f29e0f013408200ec6277` |

回滚只允许逐项进行：既有路径只有当前哈希仍等于本轮输出哈希时，才从 `target-preimage.tar.gz` 恢复；新增路径也只有当前哈希仍等于本轮输出哈希时才可删除。回滚不触及允许清单外的并发变化、测试证据、历史发布包或任何外部状态。

## 保护范围

I0/I1 不写 `.codex/`、既有 `docs/development/`、`docs/archive/`、`validation/`、`third_party/`、`dist/`、既有 `web/dist/`、`web/node_modules/`、认证配置、包装根、数据库、Web/coordinator 服务或 Chrome profiles。I2 至 I4 只在本目录新增证据和 runner，按验证需要重建 `web/node_modules/`、`web/dist/`，并新建一个不覆盖旧包的 `dist/repomesh-0.3.0-b03-integrated-20260912-r1/`。不运行旧 `verify-record.py`，不执行 GitHub 操作，不暂存、提交或推送。

运行中的 r3 Web 使用 `/home/xubohan/projects/Repomesh_Go_ver/dist/repomesh-0.2.0-b026-cookie-20260912-r3/web/dist`，未使用主目录 `web/dist`。I2 至 I4 的所有 PostgreSQL、端口、Go fixture、Playwright 临时 profile 与 Web 进程均由本轮单独创建并清理，没有停止或改写 r3 资源。

## I1 结果

60 个候选路径均已写入并与预先计算的输出哈希相符。53 个代码、脚本和前端路径与 B03 来源字节一致；7 份说明或状态文档使用 Astra 已批准的当前语境裁决结果。逐文件记录见 [I1 输出清单](i1-output.tsv)，命令和保护范围复核见 [机械检查](checks.md)。

PowerShell 文件应用时发现补丁工具会改变新行的 CRLF 形式。执行在首次输出哈希不一致时停止，随后只对 `scripts/build.ps1` 与 `scripts/verify-batch.ps1` 对齐来源的逐行行尾形式，内容核对无差异，最终两份文件均与来源 SHA-256 完全相同。该处理没有改变函数接口或产品行为。

## I2 复核发现与修正

集成后的首次后端批次位于主目录 B03 记录的 `backend/verification-20260913-055931/`，结果为 `BACKEND_LOCAL_VERIFIED`，PostgreSQL 72 通过、0 跳过，`cleanupPassed=true`。其 `checks.json` SHA-256 为 `b033f63ed1ff4db4f4d7cd055a583cc19f8fdcddb3fc19146a6a908fb6e610a3`。该结果属于修正前证据，没有覆盖独立预审随后发现的 profile 活动 rows 风险。

独立预审后，Astra 批准并由 Sol 实现三项原接口内修正：

- `Profiles` 先把 `limit+1` 行扫描到局部候选并关闭 rows，再在同一事务逐项检查秘密；分页哨兵也检查，任一检查错误整体返回 503，检查失败不写游标。
- 创建和更新页以 `sending || unknown` 统一锁住请求正文及仓库选择器；父回调与同步锁引用阻止卸载前或同 tick 的晚到 mutation 重置原操作，恢复仍使用原 key。
- 项目概览或设置页的仓库子请求明确返回 `RESOURCE_NOT_FOUND` 时，立即清空已显示仓库、项目资料、设置草稿和冲突资料并进入 inaccessible；一般 503 和 200 受限计数不按失权处理。

产品与回归改动严格限于 `internal/projects/service.go`、`internal/web/project_browser_test.go`、`web/src/CreateProjectPage.tsx`、`web/src/ProjectSettingsPage.tsx` 和 `web/src/ProjectPage.tsx`。没有新增对外接口；测试 fixture 只新增受约束的 expand/remove control action。五个最终 SHA-256 依次为 `ad1f07d5bccdde3776258eae5c41a511196e3dd77cc33ed6ff8710f9289c6fc9`、`259cedcc0ffc32784e554cf9c85b7613a298bd28eb5882bc26e8f72b10446680`、`2431ed534e70ff688473011b4655466a6f52fde61433a3134d2e92a559fb64db`、`9daff0d68a4e35448eb9612ede54df984e7472631a9e406084944165a78f773a`、`fe25b3b4088a540b84dd20a3a7172858472c724f73fa4d2e06ff814974abce9b`。修正前与最终哈希及逐项回滚来源见 [I2 回滚清单](i2-rollback.tsv)；只有当前文件仍等于对应 final hash 时才可从未修改的 B03 worktree 来源恢复。

profile 定向真实 PostgreSQL + HTTP 回归的首轮 [061027](targeted-20260913-061027/) 因夹具原地修改不可变 version 被数据库约束拒绝，分类为 `TEST_HARNESS`；它未进入产品断言，临时库为 0。夹具改为新增 version 并切换 current 后，新轮 [061111](targeted-20260913-061111/) 退出 0：两个完整且绑定既存秘密的同 owner profile 以 limit=1 逐页返回 200，默认项正确，其他 owner 不可见；秘密 disable 和 destroy 后均返回 denied 而非 503。

最终完整后端批次为 [062605](backend-verification-20260913-062605/checks.json)，SHA-256 `a5473d4131c983ea2fbb0148eba65c75ded3e6f29e1f3325b09d562f21cac066`。结果为 `BACKEND_LOCAL_VERIFIED`，PostgreSQL 73 通过、0 跳过，Go build/test/vet、项目 race、四版迁移、重复迁移、数据库重启、ledger、漂移拒绝、未配置 HTTP 和三个入口检查均符合预期，`cleanupPassed=true`。

## I3 前端与真实浏览器

最终前端轮次为 [062547](frontend-verification-20260913-062547/)：`npm ci`、typecheck、28 项测试和 production build 均退出 0。浏览器脚本先通过 `apply_patch` 从旧 worktree 来源建立本轮副本；来源 SHA-256 为 `877d7d6af19fa048ad1ed996021e32605952b08a6766850f7000dd025f3c1da2`，扩展后的最终 SHA-256 为 `92f0daf77c9c61aa60e2b5854d693b9e25042249edfe5da224f561da0117d5e7`。

失败轮次均保留：

- [061719](browser-normal-20260913-061719/) 因 Chromium 缺少共享库退出，分类为 `ENVIRONMENT`，未进入产品场景。
- [061911](browser-normal-20260913-061911/) 的锁态 picker 已正确卸载，但脚本预期了另一按钮文案，分类为 `TEST_ASSERTION`。
- [062159](browser-normal-20260913-062159/) 已完成新增产品场景，最终使用了 Playwright 不存在的 locator 方法，分类为 `TEST_HARNESS`。
- [062231](browser-normal-20260913-062231/) 在长链路末尾新建额外项目时没有生成保存摘要；网络没有 POST，分类为发现观察窗口下的 `TEST_FIXTURE_TIMING`。最终场景改为复用早先已提交且后续不再使用的项目，没有用睡眠掩盖。

最终 normal 轮次为 [062417](browser-normal-20260913-062417/)，25 个事件全部通过。新增检查确认 create/update 在 hold request 和客户端丢响应后的 sending/unknown 期间字段、配置与仓库选择器不可改变，picker 已卸载，恢复链接和 key 不变，每个写入计数仍为 1；项目概览同页重查和设置页真实下一页仓库子请求返回 `RESOURCE_NOT_FOUND` 后，旧仓名、资料与 draft 立即消失。其 `browser-result.json` SHA-256 为 `8ab06bc7e68449b9d5f3a6a5a98aea4ec86ce17391d4d1afcfa86383dae0d0bb`。网络白名单 unexpected 为 0，结果 SHA-256 为 `59f6a3ae87263c342b3ee8a9743481dd98be670fbe475d360aff9a73044f10c2`。

最终 empty-catalog 轮次为 [062531](browser-empty-20260913-062531/)，1 个完整空目录事件通过；`browser-result.json` SHA-256 为 `7c0bd1d7a8008c5ba4f993d063431ec1aa4d9c926c196f0bd9c029f9e14dae58`，网络白名单 unexpected 为 0。两套轮次均使用新的临时 PostgreSQL、端口和 Playwright 临时 profile；清理记录均为测试数据库 0、PostgreSQL fast stop、runtime/profile 删除完成。

## I4 配套发布

构建前确认 `dist/repomesh-0.3.0-b03-integrated-20260912-r1/` 不存在。`scripts/build.ps1` 退出 0 后创建该固定包，没有覆盖旧标签。`release.json` SHA-256 为 `1982d4fc0c8553bc8bbf356c9edd0b30bf89471eed33bc3bc6a05bcdbc9e1457`，记录 `stage=project-management-local`、`target=linux/amd64` 和 `businessReady=false`。

[062726](release-verification-20260913-062726/) 的构建与 11/11 独立哈希检查通过，随后因 version harness 只预期版本字符串、遗漏二进制名称而退出；没有启动 PostgreSQL。[062912](release-validation-20260913-062912/) 的全部产品命令实际通过，但 runner 最后使用旧大写诊断分类匹配当前 stderr 而退出。这两轮均分类为 `TEST_HARNESS` 并保留。

最终发布轮次为 [062934](release-validation-20260913-062934/summary.json)，SHA-256 `cb44c8984b7813e2f9ca938379d2e6f31eb9ec00c41233c8f28c5048dbbd3e72`。清单与实际文件均为 11 项且集合和哈希一致；三个入口版本一致；独立 SCRAM PostgreSQL 从 0/4/4 迁移到 4/4/0，重复迁移和最终检查保持 4/4/0；包内 Web 与资源的双进程重启测试 1 通过、0 跳过；未配置 HTTP 为 200/503/503/200/200，`healthz.businessReady=false`；coordinator 与 host-executor 均按当前声明退出 1。测试数据库为 0，PostgreSQL 和临时目录已清理。

## 最终边界

I2 至 I4 除上述五个经 Astra 批准的产品/回归路径外，没有产品写入；其余新写入均为本目录证据、验证 runner、生成的 `web/dist`/`web/node_modules` 和唯一新发布包。没有访问秘密正文、认证配置、旧 B02 证据或 Chrome profile，没有停止既有服务，没有 GitHub/账号操作，也没有 stage、commit、push、reset、checkout 或 clean。

最终结论为主目录 `INTEGRATED_LOCAL_VERIFIED`。[最终独立复核](FINAL-INDEPENDENT-REVIEW.md)已完成，两项 P1、一项 P2 均闭环，无开放 P0/P1/P2；`businessReady=false`。本轮未执行真实 GitHub 验收或部署，不将本地集成验证表述为整批业务 `VERIFIED`。
