# B03 主目录集成最终独立复核

结论：`INTEGRATED_LOCAL_VERIFIED`。复核日期为 2026-09-12 PDT；复核者为 GPT-6 Astra，未承担主要 Sol 实现。此前两项 P1、一项 P2 已闭环，本次复核未发现开放 P0/P1/P2。本结论覆盖主目录实现、真实 PostgreSQL、真实本地 HTTP、浏览器与 Linux 配套发布，不表示真实 GitHub 验收、部署或整批业务 `VERIFIED`；`businessReady=false`。

B02 保持 `PAUSED_BY_USER / IN_PROGRESS`。second-account-02 的历史 `FAIL`、`NOT_RUN` 和 `RESTORE_IN_PROGRESS` 保留，不据此声称 App、B 安装、协作者或 OAuth grant 已恢复。用户已经明确允许 B03 集成，未来真实账号验证只使用 A；本次测试中的 A/B 是本地夹具身份，没有新的真实账号操作。B04 候选未采用，本轮没有实现 B04。

## 复核方式与来源

复核结合此前源码预审，重新检查五个修复文件、相关事务、认证桥接、HTTP 和浏览器恢复路径、测试实现与最终输出；没有重新启动服务或历史实验。检查 [I0/I1 记录](README.md)、[I1 清单](i1-output.tsv)、[I2 回滚清单](i2-rollback.tsv)，以及下列最终证据。没有只依据汇总中的 PASS 判定。

- [后端 062605](backend-verification-20260913-062605/checks.json)：40 项检查；真实 PostgreSQL 测试 73 通过、0 跳过，完整 Go build/test/vet 与 race 通过，`cleanupPassed=true`。实际 go-test JSON 包含项目事务、HTTP 契约、配置秘密分页、授权观察新鲜度和进程重启测试的 PASS。
- [前端 062547](frontend-verification-20260913-062547/test.txt)：npm ci、typecheck、test、build 的退出文件均为 0；测试实际输出 28/28、0 失败、0 跳过。
- [普通浏览器 062417](browser-normal-20260913-062417/browser-result.json)：25 个事件通过。[空配置目录 062531](browser-empty-20260913-062531/browser-result.json)：单独运行的空目录创建场景通过。均为真实 PostgreSQL、真实 Web handler、受控 GitHub 替身和临时 Playwright 上下文。
- [发布构建 062726](release-verification-20260913-062726/build.txt)及[最终发布验收 062934](release-validation-20260913-062934/summary.json)：构建退出 0，唯一新包为 `dist/repomesh-0.3.0-b03-integrated-20260912-r1/`。

## 预审发现闭环

| 发现 | 修复与实际验证 | 结论 |
| --- | --- | --- |
| P1：Profiles 活动 Rows 内复用同一事务查询秘密，触发 conn busy | `internal/projects/service.go` 先扫描 limit+1 候选并检查 rows.Err、关闭 Rows，再逐项 InspectProjectSecret；检查失败整体 503，之后才生成游标和提交。新真实 PG/HTTP 测试验证两个完整 profile 的 limit=1 分页、默认、owner 隔离、秘密禁用和销毁后的 denied。 | CLOSED |
| P1：sending/unknown 可被编辑或 picker 过期回调重置，进而产生新 key | 创建和设置页同步锁引用覆盖 review/send/正文修改，控件禁用且 picker 卸载；父回调也检查锁。浏览器脚本实际断言 hold request 与丢已提交响应后锁态、picker 数量为 0、原链接/key 稳定、写请求各为 1，并按原操作恢复。 | CLOSED |
| P2：仓库子请求或冲突读取明确失权后保留旧名称/草稿 | 概览立即清除 readable 与仓库并进入 inaccessible；设置页统一清除草稿、冲突资料、选择器、字段、仓库和原输入，避免 ready 快照再次填回。新增真实 handler 同页仓库 404、下一页 404 场景验证名称、用途、草稿和仓库立即消失。普通 503 与 200 受限计数仍按原语义处理。 | CLOSED |

秘密分页测试只引用非生产夹具中已有 purpose 的秘密版本验证可用性；不把它称为有效模型凭据，不执行模型请求，也不据此开放 Issue 创建。

## 验收映射

编号沿 B03 原验收矩阵，而非持久化专题中同名的 Issue/后台矩阵。以下各项均为本次主目录 `LOCAL_VERIFIED`。

| 编号 | 本次核对依据 |
| --- | --- |
| DB01 | HTTPContract 的创建和更新同键同输入各 20 并发；事务唯一操作作用域、原键查询和稳定重放。 |
| DB02 | 创建/更新同键不同输入竞争；赢家正文保留，其他冲突或未知后查原结果。 |
| DB03 | PostgresProjectTransactionsAndAuthorizationInterleaving 的分阶段故障与不完整操作提交拒绝；项目、范围、配置、回执同事务。 |
| DB04 | HTTPContract 的写事务前请求屏障先查 404、提交屏障下身份锁等待及最终恢复；丢响应后按原键查询；ProcessRestart 实际启动两个不同 PID 的 Web 二进制。 |
| DB05 | 已提交操作先重放，再判断新输入的 expected revision；旧成功回执不随后续项目修订改变。 |
| DB06 | 无变化更新保存操作回执而不增加项目 revision；HTTP 与浏览器均有断言。 |
| DB07 | 新增仓库 denied/unknown 返回失败，整次更新回滚，原范围和资料不变；客户端只提交明确增量。 |
| DB08 | 原范围受限时资料修改、显式配置修复继续；服务器只披露本次 allowed 项，完整原范围保留。 |
| DB09 | 固定 profile/version/configuration revision 与秘密引用；默认/版本变化不暗换绑定，只有显式双配置输入重新解析。 |
| DB10 | 事务内锁并重验 binding/session/account、凭据 epoch/revision 与连接状态；重新登录交错、凭据变化及授权观察时间边界测试拒绝旧观察。 |
| HTTP01 | 创建 201、重放 200、更新 200、GET/列表/两种原操作查询、不可变时间/revision与 no-store；API 和浏览器恢复链接分别生成。 |
| HTTP02 | 严格 JSON、重复属性/Unicode/未知字段、大小、UUID key、字段错误结构；结构边界检查后已清理操作 410 优先于业务输入比较。 |
| HTTP03 | Origin/CSRF/session 和 actor 作用域；跨 actor 的资源/回执/墓碑不可见，不暴露秘密或旧正文。 |
| HTTP04 | 稳定 ID 排序与分页、字面搜索、游标作用域/有效期和项目范围修订核对。 |
| UI01 | 两步创建、回执、列表和详情；空目录可保存待配置项目，无伪造 Issue/运行成功。 |
| UI02 | 资料、明确增仓、受限原范围、显式配置与固定版本、搜索分页选择、无变化保存。 |
| UI03 | 请求前暂时 404、服务端丢响应及客户端丢已提交响应、原输入存在/丢失、原键查询；本轮补足操作锁。 |
| UI04 | sessionStorage/localStorage/clipboard 拒绝时，发送前展示可选择的浏览器恢复链接；输入清理不依赖索引成功。 |
| UI05 | 401、失权清理、夹具 actor 隔离、搜索/跨项目/注销后晚到响应、back/forward；本轮补足子请求失权。 |
| UI06 | 加载、可靠空列表、错误、未知与只读外壳区分。PROJECT_UPDATE_NOT_ALLOWED 的客户端渲染场景仍属模拟错误展示，不充当真实服务器角色授权验收。 |
| UI07 | 桌面及 390×844 移动视口、控件可用与横向溢出检查、原操作直达。 |

DB04 的先 404 窗口在产品写事务进入之前，不声称读请求绕过身份锁。两个 Web 进程均为 Linux 上的实际进程，不等于双操作系统或双平台验证。

## 工件、范围与保护核查

独立重算发布清单 11 项文件哈希，集合及每项哈希全部匹配；包内 web/dist 与主目录生成资源逐字节相同。`release.json` SHA-256 为 `1982d4fc0c8553bc8bbf356c9edd0b30bf89471eed33bc3bc6a05bcdbc9e1457`。这证明当前配套文件一致性，不是可复现构建或源码到二进制的密码学来源证明。

[包内进程测试实际输出](release-validation-20260913-062934/process-restart.txt)记录 PID 561437、561446 均正常退出，同 actor、同创建/更新回执及唯一数据库操作全部为 true。迁移实际输出从 0/4/4 到 4/4/0，重复执行仍为 4/4/0。HTTP 实际状态为 healthz 200、readyz 503、未配置项目 API 503、项目及恢复页面 200；coordinator/host-executor 均按未配置/未实现语义退出 1。

I0/I1 清单哈希复核一致。将 I2 的五项 final hash 覆盖 I1 后，60 项中 58 项当前字节吻合；另两项为主代理正在维护的 HANDOFF、IMPLEMENTATION-PLAN 状态文档。五项回滚来源 pre-fix hash 也逐项吻合。当前 HEAD 仍为 `ad9a49b2fe3b5fe743a65c838ded6cb022e79363`，暂存 diff 为空。此快照与执行日志支持未暂存/提交声明，不将单次本地快照描述为远端操作历史的独立证明。

只散列保护文件、不读取其正文：`.codex/config.toml` 仍为 `58c3d47686d46de2074c7acc7bb897d41807b4ed89fad8d20482448048e5cbad`，second-account-02/PAUSE.md 仍为 `3da54c323316e1ae8a2f5ea131c85495cc409958315917886b8e87341b9d7cbc`，均与 I1 相同。旧 B02 包和历史证据沿用 I0/I1 的保护清单，验证脚本只管理自身临时实例；没有授权或执行对 B02 服务、数据库、浏览器 profile 的替换。运行中的 r3 使用其包内资源，不消费主目录 web/dist。

普通/空目录浏览器实际网络分别 226/25 条，仅 method、path、status、code、key 五字段；无 Cookie、token、OAuth code/state、csrfToken、authorizationUrl、授权头、请求正文或原始 HAR。真实本地测试秘密只用于受控临时目录和进程环境。最终两套浏览器、发布和定向测试 cleanup 记录均确认测试库为 0、专用 PostgreSQL 停止、临时目录清除；后端 cleanupPassed=true。

排除范围保持：没有模型 Key 管理或调用、预算编辑、配置导入管理、Issue、运行准备、Manager/Worker、P9 或 AgentTeams 接入。空 profile 目录是有效状态；fixture 不构成采用新配置协议。

## 失败历史与结论边界

定向 061027 的不可变版本夹具修改失败保留；061111 改为新版本插入后通过。浏览器 061719 环境缺共享库、061911 文案断言、062159 不存在的 locator、062231 发现观察窗口下额外创建未进入发送，均保留；最终复用已有项目完成新增失权场景。发布 062726 的版本输出格式断言和 062912 的诊断文案断言失败保留，后续复用同一不可变新包验证通过，没有覆盖重建。

后端最终 checks SHA-256：`a5473d4131c983ea2fbb0148eba65c75ded3e6f29e1f3325b09d562f21cac066`；普通浏览器结果：`8ab06bc7e68449b9d5f3a6a5a98aea4ec86ce17391d4d1afcfa86383dae0d0bb`；空目录结果：`7c0bd1d7a8008c5ba4f993d063431ec1aa4d9c926c196f0bd9c029f9e14dae58`；发布最终 summary：`cb44c8984b7813e2f9ca938379d2e6f31eb9ec00c41233c8f28c5048dbbd3e72`。

实现由 Sol 承担，架构和修复裁决由 Astra 承担。本次独立复核保留早期 worktree 两轮 helper 实现先于 Astra 声明、随后替换/批准的历史偏差，不声称全程顺序合规。现行状态文档应使用本报告的 `INTEGRATED_LOCAL_VERIFIED` 并保留 B02 暂停和整批业务未 VERIFIED；早期“其余验证已完成”的无阶段限定表述须按实际阶段解释和更新。
