# B02 问题关闭与证据归属

2026-09-12。原始复核报告保留发现时的源码行号和判断，下表记录最终修复与证据。认证与秘密均由非主要实现者复核；主代理负责集成，不把自己的测试标成复核者运行结果。

| 来源／问题 | 最终处理 | 证据 |
| --- | --- | --- |
| [认证复核](auth-review.md)：确定未提交的凭据密文及崩溃孤儿残留 | 明确 CAS 失败立即 Destroy；提交回执未知保留，超过 15 分钟后按当前权威引用清理。 | `TestPostgresStaleRefreshDiscardsUncommittedSecrets`；最终标准验收。 |
| 同上：另一浏览器的旧回调覆盖更新连接 | 重连保存 expected connection revision；匿名登录识别账号后比较凭据提交时刻，最终 account/connection 锁内复核，旧结果 superseded 且不签 Cookie。 | `TestPostgresReconnectCannotReplaceNewerOtherBrowserConnection`、`TestPostgresDelayedAnonymousLoginCannotReplaceReconnectedActor`。 |
| 同上：无效 App 基础参数仍导入秘密 | 在打开数据库、注册根和 Seal 前完成纯 GitHub 配置校验。 | `TestPostgresDeploymentInvalidAppDoesNotImportSecrets` 断言秘密版本、App 引用和 wrap_count 均为 0；独立增量复核 PASS。 |
| [秘密复核](secrets-review.md)：根引用预检与并发自检写入交错 | advisory lock 内先锁活动根行，再检查引用和切根。包装预扣仍独立提交。 | `TestPostgresSecretRotationWaitsForExistingWriter`；复现失败后修复，最终标准验收及认证复核通过。 |
| [文档复核](documentation-review.md)：Windows 支持表述过宽 | 当前认证仅 Linux；Windows B01 是历史源码证据。 | 根 README、数据库说明、认证说明及非 Linux 文件读取实现。 |
| 同上：当前发布包仍指历史骨架版本 | 构建新的 B02 Linux 配套产物，最终使用 r1。 | [发布核验](release-verification-r1.json)、[同包浏览器验证](browser-06/checks.json)。 |
| 同上：根失败回滚承诺过宽 | 明确仅引用预检失败不切根；切根提交后其他启动失败不回滚。旧根删除要求所有未销毁版本及自检均无引用，并保留历史备份解密根。 | [认证说明](../../current/authentication-development.md)。 |
| 同上：未配置 POST 实际先返回 Origin 403 | 未配置服务先返回 AUTH_NOT_CONFIGURED 503，再进入已配置接口校验。 | Web 未配置接口测试及浏览器真实 Go API 场景通过。 |
| 同上：race 缺少工具前置 | 补充 cgo 和可用 C 编译器要求。 | [认证说明](../../current/authentication-development.md)与批次复跑说明。 |
| 浏览器新增冷却场景：旧组件在 session 代次改变后仍查询，消费首次 429 | refresh 前保存代次，返回后先判断代次再查询 attempt，由新组件保留轮询责任。 | [修复前记录](browser-04/polling-debug.json)、[修复后九场景](browser-05/checks.json)、最终 [browser-06](browser-06/checks.json)及独立 UI 增量复核 PASS。 |
| no-comments 清理 | 删除两条冗余注释和 `req.GetBody=nil`，保留解释数据库写屏障的注释。删除只镜像该赋值的断言，保留实际线上请求不重发／不重定向的行为测试。 | GitHub 定向 race 和最终标准验收通过，独立复核确认。 |

## 保留的失败记录

- `go-tests-first.jsonl` 是旧沙箱网络／进程权限失败；`go-tests-postgres-01.jsonl` 记录随后真实 PostgreSQL 运行。不能将前者改写为通过。
- `final-postgres-race.jsonl` 为主任务真实 PostgreSQL race 通过记录，补齐复核任务因 network namespace 无法访问实例而未能运行的旧 claim 用例。
- `final-postgres-race-02.jsonl` 中 access、secrets、web 通过，GitHub 因尚未移除的 `GetBody` 实现镜像断言失败。该断言随后移除，实际不重发行为测试仍保留；独立 GitHub race 通过，最终 [verification-02](verification-02/checks.json)全项通过。
- `browser-01` 七场景通过但中文字体缺失；`browser-02` 修正临时字体后通过。`browser-03`／`browser-04` 新增冷却场景复现 UI 代次竞态；`browser-05` 修复后九场景通过；`browser-06` 使用最终 r1 包的二进制和前端资源再次通过。
- `verification-01` 与无 r1 后缀的发布核验是中间版本证据。最终验收使用 verification-02、browser-06 和 release-verification-r1。

最终认证复核在原始发现后追加了关闭判断与 UI 增量检查，结论为 PASS、无开放 P0/P1/P2。[最终文档复核](documentation-final-review.md)确认先前五项均关闭，独立解析验收统计并重新计算 r1 包 11 个文件哈希，全部匹配。受控时钟、上游替身和数据库测试不构成真实 OAuth、进程 kill、真实提交回执丢失或长期运行验收。真实 GitHub App 仍未配置，B02 外部单元继续 BLOCKED。
