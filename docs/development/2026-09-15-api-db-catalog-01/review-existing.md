# B00-B03 已有设计章独立只读复核

复核对象：`docs/api-database/b00.md`、`b01.md`、`b02.md`、`b03.md`，以及 `foundations.md`、`README.md` 和四章引用的源码、迁移、采用记录与开发证据。

复核方式：只读核对当前工作树。没有启动服务、数据库、浏览器或容器，没有运行产品测试，没有重跑历史实验。仅运行 `python3 docs/api-database/render.py --check`（只读检查，退出 1）核对导航生成物状态。

基线：git HEAD `43d8c2a`，`docs/api-database/` 为未提交新增目录，工作树含其他作者的未提交修改。

范围边界：B04 至 B11 正文没有通读。为回答 root 提出的开头措辞问题，只读了 `b04.md` 第 1 行至第 5 行、第 41 行，并按关键字定位了 `foundations.md` 与 b00 至 b04 中“错误封装／错误体”的位置。

## 结论

四章主体事实与源码一致，批次标记与采用状态区分正确，没有发现把 B02 或 B03 写成整批 `VERIFIED` 的情况。需要修复的问题集中在响应字段、状态码配对和一处证据引用。

需要修复：

1. `b00.md:154` 把项目接口的错误体写成了认证接口的错误体。
2. `b00.md:157` 引用 B02 验收记录作为当前 503 的证据，但该记录实际是 404。
3. `b02.md:342-343` 把“批次尚无成功扫描结果”的状态码写成 `AUTHORIZATION_UNCONFIRMED`。
4. `b03.md:229` 把 `configuration.effective` 描述为可为 null，源码与前端都要求对象存在。

低级别引文精度问题两条，导航生成物状态问题一条，见后文。

## 需要修复的问题

### I1 B00 项目接口 503 响应体写错（中）

位置：`docs/api-database/b00.md:154`（修复后拆为第 154、155 行）。

原文把 `GET /api/session` 与 `POST /api/projects` 合并成同一响应体 `{"error":{"code":"AUTH_NOT_CONFIGURED","message":"The request could not be confirmed.","details":{}}}`。

源依据：

- 认证接口走 `internal/web/auth.go:179-188` 的 `authError`，message 固定为 “The request could not be confirmed.”，字段是 `details`。
- 项目接口走 `internal/web/projects.go:156-162` 与 `206-226` 的 `writeProjectError`。认证未配置时同样是 503 与 code `AUTH_NOT_CONFIGURED`，但 message 固定为 “The request could not be completed.”，字段是 `fieldErrors` 与 `requestId`。
- 运行证据见 `docs/development/2026-09-12-b03-integration-01/backend-verification-20260913-062605/checks.json` 的 `http-/api/projects` 记录，响应体为 `{"error":{"code":"AUTH_NOT_CONFIGURED","message":"The request could not be completed.","fieldErrors":[],"requestId":"..."}}`。
- 本目录另外两章已经分别记录两种形状：`b02.md:40` 记录 auth 错误体，`b03.md:41` 记录 project 错误体。

最小修改：把该行拆成认证接口与项目接口两行，或把示例写成按接口区分。状态码 503 与 code `AUTH_NOT_CONFIGURED` 本身是对的，不需要改。

### I2 B00 当前 503 的证据链接指错（中）

位置：`docs/api-database/b00.md:157`（表格拆行后为第 158 行）。

原文：“当前源码对同名路径返回 503 `AUTH_NOT_CONFIGURED`，见 [verify-batch.ps1](../../../scripts/verify-batch.ps1) 第 271 行与重新记录的 [B02 验收](../2026-09-12-batch-02/verification-02/checks.json)。”

源依据：

- `scripts/verify-batch.ps1:271` 是当前脚本对 `GET /api/projects` 期望 503 的断言，这一半成立。
- `docs/development/2026-09-12-batch-02/verification-02/checks.json`（recordedAt `2026-09-12T12:30:31Z`）的 `http-/api/projects` 是 404 `{"error":"not_implemented"}`，`verification-01/checks.json`（`2026-09-12T12:18:08Z`）同样是 404。
- 记录 503 与项目错误体的是 `docs/development/2026-09-12-b03-integration-01/backend-verification-20260913-062605/checks.json`（recordedAt `2026-09-13T06:26:52Z`）。
- 因此“B00 记录为 404、B02 记录仍为 404、当前源码与脚本断言为 503”三段各自成立，但当前 503 的验收链接指到了 404 的记录。

最小修改：把“重新记录的 B02 验收”换成 B03 后端验收记录；或改成“B02 记录仍是 404，当前源码与脚本断言为 503，运行证据见 B03 后端验收”。B00 运行探针指向 404 的那句话保持不变。

### I3 B02 仓库发现错误码配对错误（中）

位置：`docs/api-database/b02.md:342-343`。

原文把 503 `AUTHORIZATION_UNCONFIRMED` 的触发写成“批次尚无成功扫描结果，或本条连接无法确认”，把 503 `RESULT_UNCONFIRMED` 写成“数据库失败”。

源依据：

- `internal/access/discovery.go:98-100` 对 `!b.hadSuccess` 返回 503 `RESULT_UNCONFIRMED`。
- `AUTHORIZATION_UNCONFIRMED` 来自连接无法确认，见 `internal/access/credential.go:25`、`:31`、`:36`、`:43`，以及 `internal/access/discovery.go:163-165` 的“全部条目无法确认且没有可披露结果”。
- `RESULT_UNCONFIRMED` 也覆盖数据库读取失败，见 `internal/access/discovery.go:106-108`、`:119`、`:126`。

最小修改：把“批次尚无成功扫描结果”移到 `RESULT_UNCONFIRMED` 行，并补 `discovery.go:98` 依据；`AUTHORIZATION_UNCONFIRMED` 行保留“本条连接无法确认，或全部条目无法确认且没有可披露结果”，补 `discovery.go:163` 依据。

### I4 B03 effective 为 null 的表述与源码不符（低到中）

位置：`docs/api-database/b03.md:229`。

原文：“固定版本读取结果：`configurationRevision` 加 `modelProfileId`、`executionProfileId`、`workerConcurrency`、`budgetPolicyId`、`timeLimitPolicyId`、`verificationGroupEnabled`；无法解析时为 null，null 不等于无限预算或可执行。”

源依据：

- `internal/projects/types.go:106-111` 的 `ConfigurationView.Effective` 是非指针 `EffectiveConfiguration`，没有 `omitempty`，JSON 中始终是对象。
- `web/src/projectApi.ts:82` 用 `object(data.effective)` 校验响应，null 会被前端判为无效。
- `docs/current/first-batch-browser-api-contract.md:154` 写的是“`effective.configurationRevision` 为不透明字符串，其余 effective 引用字段可为 null；并发 number／验证开关 boolean 在不能解析时也为 null”。可为 null 的是对象内部字段。

最小修改：改为“`effective` 对象内的引用与数值字段在无法解析时为 null，对象本身始终存在。null 不等于无限预算、0 额度、关闭校验或可执行”。这样与契约第 154 行和当前实现同时一致。

### I5 B02 superseded 状态写入位置不准确（低）

位置：`docs/api-database/b02.md:216`。

原文称在身份确认阶段发现绑定代次或尝试代次已经变化时，“尝试被置为 `superseded`，不会签出会话，见第 165 行”。

源依据：

- `internal/access/callback.go:165-167` 在该条件下只执行 `return result, nil`，不写状态。
- 写 `superseded` 的位置是 `internal/access/callback.go:181-188`（连接修订变化）与 `internal/access/session.go:74-79`（注销且代次仍匹配）。
- 同一绑定上的新尝试还会在 `internal/access/start.go:134` 把旧未完成尝试置为 `superseded`，并推进尝试代次。

最小修改：改成“该路径不写状态也不签出会话，直接返回结果页；旧尝试通常已由注销或新尝试置为 `superseded`（见 `session.go` 第 74 行、`start.go` 第 134 行）”，或直接删去“尝试被置为 superseded”，保留“不会签出会话”。

### I6 行号指向偏差（低）

- `b00.md:92` 用 `cmd/repomesh-host-executor/main.go:11` 支撑“不带 `--version` 时打印未实现并退出 1”。第 11 行是 `func main()` 起始行，消息在 `:18`，退出码在 `:19`。
- `b02.md:385` 用 `cmd/repomesh-coordinator/main.go:46` 支撑“每轮等待 100 毫秒到 1 秒”。第 46 行是主循环起始行，延时取值在 `:53-56`。

两处不影响结论，按真实行号定位即可。

## b00 与 b04 开头的“错误封装”归属修复措辞

现状：

- `docs/api-database/b00.md:5` 与 `docs/api-database/b04.md:5` 都写“跨批公共规则（…、错误封装、…）见 [公共规则](../../api-database/foundations.md)”。
- `docs/api-database/foundations.md` 没有错误体或错误封装内容。`grep -ni "error\|错误体\|fieldErrors\|requestId\|details" docs/api-database/foundations.md` 无命中；第 113、131 行只把“错误语义”列为后续批次要写清楚的检查项，不是错误体形状。
- 错误体形状由各接口章节自持：`b00.md:152-156` 与第 158 行（本文件未配置认证行为）、`b02.md:40` 对应 `internal/web/auth.go:179-188`、`b03.md:41` 对应 `internal/web/projects.go:206`、`b04.md:41` 对应 `internal/web/projects.go:206` 与 `internal/web/models.go:156`。

对“是否只需改链接到本章真实源”的回答：只改链接不够。当前句子的语义是“错误封装这项规则由 foundations 统一规定”，把链接改到本章源之后还要把它从 foundations 的清单里拿掉，或者写成并列说明，否则 foundations 仍然不拥有该形状。

`docs/api-database/b00.md:5` 最小措辞建议：

“跨批公共规则（身份作用域、事务与重放、执行边界）见 [公共规则](../../api-database/foundations.md)；错误体形状按接口所属章节记录，本文件见下节未配置认证时的 /api 行为。”

`docs/api-database/b04.md:5` 最小措辞建议：

“跨批公共规则（身份作用域、事务与重放、秘密用途白名单）见 [公共规则](../../api-database/foundations.md)；错误体形状见本章公共约定的错误体行。”

备选方案：在 `foundations.md` 增加一个“HTTP 错误体”小节，写清认证体 `{"error":{"code","message","details"}}` 与项目及模型体 `{"error":{"code","message","fieldErrors","requestId"}}` 两种形状，再保留两处开头的句子不改。这个改动比改两句大，但能让后续章节继续引用同一个锚点。不建议只替换链接。

## 已核对通过的状态与事实

### 批次标记与采用状态

- B00 标题“已有实现，VERIFIED”与 `docs/development/2026-09-12-batch-01/b00-script-verification-r2/checks.json`（`B00`、`VERIFIED`、21 项检查、`cleanupPassed=true`）一致；`b00-runtime.json` 的探针结果与 `b00.md:236` 一致。
- B01 标题“已有实现，VERIFIED”与 `docs/development/2026-09-12-batch-01/b01-verification/checks.json`（`B01`、`VERIFIED`、40 项，含 `db-check-after-server-restart`）及 `release-verification.json` 一致。
- B02 的 `PAUSED_BY_USER` 与 `IN_PROGRESS` 口径成立。`verification-02/checks.json` 是 `LOCAL_VERIFIED` 且 `externalAcceptance` 为 `NOT_RUN: real GitHub App and browser OAuth acceptance required`；`docs/current/HANDOFF.md:24-26` 保留 `second-account-02` 的 LIVE-05 历史 `FAIL`、LIVE-08 `NOT_RUN`、`RESTORE_IN_PROGRESS` 与 `DEFERRED_BY_USER`。
- B03 标题“本地集成验证，非整批 VERIFIED”与批次 `README.md:3` 的 `INTEGRATED_LOCAL_VERIFIED`、`FINAL-INDEPENDENT-REVIEW.md:3` 的“未发现开放 P0/P1/P2”一致。证据数字可复核：后端 `postgres-test-gate` 为 73 通过、0 跳过；前端 `test.txt` 为 28 项通过；浏览器普通 25 项、空配置 1 项；发布 `artifact-check.json` 为 11 项哈希匹配。

### B00

- `/healthz` 的 200、字段与固定 `businessReady=false` 见 `internal/web/server.go:99-104`；`/readyz` 的 503 与固定文案见 `:105-108`；未注册 `/api/*` 的 404 `{"error":"not_implemented"}` 见 `:112-114`。
- 页面路由回退到 `index.html`、`Cache-Control: no-store`、`Referrer-Policy: no-referrer`、非 GET／HEAD 的 405 与 `Allow: GET, HEAD` 见 `internal/web/server.go:116-138`；JSON 统一 `no-store` 见 `:142-146`。
- 验证脚本的引用行成立：工具版本在 `scripts/verify-batch.ps1:152-154`，工程检查在 `:187-204`，三入口在 `:213-221`，HTTP 探针在 `:271-290`，`businessReady` 断言在 `:277`，清理在 `:297`，结果语义在 `:332-339`（B02 写 `LOCAL_VERIFIED`、B03 写 `BACKEND_LOCAL_VERIFIED`）。
- `scripts/build.ps1` 的版本模式在 `:2`，拒绝覆盖在 `:21-23`，ldflags 版本注入在 `:34`，`release.json` 与 `businessReady=false` 在 `:77-83`。
- Web CLI 的 `db` 与 `sources` 分派在 `cmd/repomesh-web/main.go:33-37`，旗标在 `:39-44`，位置参数拒绝在 `:55-57`；coordinator 未配置退出 1 在 `cmd/repomesh-coordinator/main.go:31-33`，`RunOne` 循环在 `:46-62`；host-executor 在 `cmd/repomesh-host-executor/main.go:14-19`。
- 版本变量与三入口共享见 `internal/buildinfo/version.go:1-5`。

### B01

- 帮助文本与退出码见 `cmd/repomesh-web/main.go:90-93`（帮助 0）、`:95-97`（未知子命令 2）、`:114-116`（非法参数与 `--timeout <= 0` 为 2）、`:129-135`（`ErrInvalidConfig` 为 2，其余连接失败为 1）、`:144-157`（历史错误为 1，`check` 的 pending 为 1，成功为 0）。
- `Check` 使用 `RepeatableRead` 加 `ReadOnly`，见 `internal/database/database.go:80`；`Migrate` 使用 `ReadCommitted`、先取固定 advisory lock（`0x5245504f4d455348`）再读历史，见 `internal/database/migrations.go:21`、`:107`、`:112-116`；每个文件的 SQL 与账本 `INSERT` 在同一事务，提交失败返回 `ErrCommitUnconfirmed`，见 `:120-131`；回滚使用 5 秒独立 context，见 `internal/database/database.go:102-106`。
- 账本字段与约束（`version` 主键且大于 0、`name` 唯一、`checksum` 32 字节、`applied_at` 默认 `now()`）见 `internal/database/migrations/0001_schema_migrations.sql:1-6`。迁移 SQL 固定 LF 见 `.gitattributes:2`。
- 清单当前为 6 条，目录可见 `0001` 至 `0006`；B01 记录时刻 `target=1`，B03 记录时刻为 `4/4/0`，分别见两份 checks.json 与 `b03-integration-01/release-validation-20260913-062934/summary.json`。
- `postgres_test.go` 的引用行成立：20 并发迁移 `:88`，SQL 失败回滚 `:121`，账本插入失败回滚 `:133`，多文件原子升级 `:144`，历史篡改 `:175`，执行中取消 `:212`，锁等待取消 `:235`，锁等待超时 `:269`，错误密码脱敏 `:288`，多库隔离 `:303`。

### B02

- 七个端点与 `registerAuth` 见 `internal/web/auth.go:27`、`:47`、`:54-55`、`:84`、`:102`、`:109`、`:120`；15 秒请求超时在 `:40`；`AUTH_NOT_CONFIGURED` 在 `:32-34`；Origin 校验在 `:36-39`；请求体上限与内容类型在 `:151-166`；Cookie 属性在 `:169-177`；`authError` 在 `:179-188`。
- 会话有效性条件（未撤销、未禁用、未过期、30 分钟空闲、绑定未过期、代次匹配）见 `internal/access/session.go:16-21`；CSRF 摘要见 `:28`；注销与代次推进见 `:44-85`；登录首次 201／重放 200 见 `internal/web/auth.go:76-80`；重放与冲突见 `internal/access/start.go:157-196`；限流 5／分钟与 10／分钟见 `:131-132`；`Idempotency-Key` 形状见 `internal/access/types.go:134-152`。
- 回调的 303、取消与未知、单一交换责任、代次与修订校验、确认事务见 `internal/web/auth.go:99` 与 `internal/access/callback.go:48-49`、`:51-60`、`:62-67`、`:89-100`、`:181-213`、`:251-269`。
- 发现端点参数校验、批次复用、60 秒重核、10 分钟有效期、游标作用域与 `CURSOR_EXPIRED` 见 `internal/access/discovery.go:50-52`、`:57-66`、`:78-97`、`:132`、`:136-165`、`:170-185`；coordinator 顺序与租约见 `internal/access/worker.go:14-58`、`:132-147`、`:155-203`。
- 秘密表字段与约束逐项核对通过（`repomesh_secrets` 四表见 `internal/database/migrations/0002_auth_secrets.sql:3-46`，`repomesh_access` 九表见 `0003_authentication.sql:3-113`）。根注册、指纹比对、自检与退休见 `internal/secrets/roots.go:16-97`、`:104-124`；用途白名单与销毁幂等见 `internal/secrets/store.go:76-90`、`:226-248`；轮换的比较交换见 `:250-306`；AAD 见 `internal/secrets/crypto.go:23-35`；孤儿清理窗口见 `internal/access/cleanup.go:20-29`；私密文件校验见 `internal/access/private_file_linux.go:14-37`。
- 部署配置字段、HTTPS origin 与 callback 校验、TLS 成对配置、启动顺序（配置、数据库 `Check`、秘密初始化、凭据导入）见 `internal/access/deployment.go:19-33`、`:48-116`、`:119-156`。

### B03

- 八个项目端点与 `registerProjects` 见 `internal/web/projects.go:32-153`；错误体生成见 `:206-226`；请求体规则见 `:182-197`；幂等头见 `:199-204`；身份锁序（绑定、会话、账号）见 `internal/access/project_access.go:67-95`。
- 创建、更新、回执查询、详情、列表、范围分页、配置列表与目标解析的实现行与文档引用一致：`internal/projects/service.go:102-195`（创建写事务）、`:126-139`（并发抢键重放）、`:304-315`（重放优先与修订冲突）、`:343-375`（无变化与配置修订）、`:421-447`（创建回执查询）、`:450-477`（更新回执查询）、`:479-506`（详情）、`:508-557`（列表）、`:559-648`（范围分页）、`:650-737`（配置列表）、`:739-772`（目标解析）。
- 事务设置（READ COMMITTED、`lock_timeout=2s`、`statement_timeout=5s`、`synchronous_commit=on`）见 `internal/projects/storage.go:17-27`；回执生成见 `:99-137`；观察时效与连接复核见 `internal/access/project_access.go:200-226`；目录行 `FOR SHARE` 见 `internal/projects/storage.go:258-264`；游标作用域与过期见 `:452-469`。
- `0004_projects.sql` 的 11 个关系、复合唯一约束、不可变触发器与延迟操作触发器逐项核对通过：schema 与秘密唯一约束 `:1-5`，目录 `:7-10`，profiles 与延迟外键 `:12-48`，defaults `:50-57`，projects `:59-71`，repositories `:73-80`，project_repositories `:82-88`，configuration_revisions `:90-119`，creation_operations `:121-133`，update_operations `:135-147`，cursors `:149-159`，触发器与函数 `:161-205`。
- B04 负责的 `pinned_version` 列与 `cursors.kind` 扩展见 `internal/database/migrations/0005_models.sql:4-16`，B03 只读取的口径与 `internal/projects/storage.go:296-337` 一致。
- 目标解析差异如实记录。认证稿 `docs/current/authentication-browser-api-draft.md:40` 把 `{"kind":"project"}` 写成 `/projects/{projectId}/issues`，当前实现返回 `/projects/{projectId}`（`internal/projects/service.go:751`），`b03.md:351` 已把它标为待同步差异，没有单方面改写采用稿。
- `PROJECT_CREATE_NOT_ALLOWED` 与 `PROJECT_UPDATE_NOT_ALLOWED` 只有前端类型（`web/src/projectApi.ts:52`）与模拟渲染，服务端没有分支；`b03.md:570` 与 `FINAL-INDEPENDENT-REVIEW.md:51` 的 UI06 说明一致。C05 是 `docs/current/first-batch-browser-api-contract.md:313-315` 的 `PROPOSED_NOT_ADOPTED` 候选，`b03.md:571` 没有把它写成已采用。

## 导航与生成物状态

- `docs/api-database/README.md:23-29` 的“使用边界”成立：已实现与提案分开，采用语义回到 current 索引，生成 HTML 不构成采用决定，B04 本地证据没有重跑，B05 至 B11 不能当作已实现或已验收证明。`README.md:12-21` 的批次状态标注与各章标题一致，没有把 B02、B03 写成整批 `VERIFIED`。
- 但 `README.md:33-43` 给出的检查命令当前不通过。运行 `python3 docs/api-database/render.py --check` 退出 1，首行错误为“index.html 已陈旧：与当前 Markdown 或模板不一致”，随后列出 foundations 与 b00 至 b11 的“渲染结果缺少文字”。文件时间与结论一致：`index.html` 为 01:03，`b00.md` 为 01:14，`README.md` 为 01:19。
- 影响：root 追加导航并提交前，需要先运行 `python3 docs/api-database/render.py` 重新生成 `index.html`，再运行 `--check`。否则 README 第 3 行“HTML 与 Markdown 共用内容”在交付状态下不成立。这一条是工作树状态问题，不是 b00 至 b03 正文的语义错误。

## 未执行与剩余风险

- 没有运行 `go build`、`go test`、`go vet`、前端测试或任何服务，也没有连接数据库或浏览器。本报告的事实来自源码、迁移、采用记录和已保存证据文件的只读核对。
- B02 的真实 GitHub、App 安装与浏览器往返仍是 `PAUSED_BY_USER`；B03 的结论仍是本地集成验证。本报告不改变这两个批次的状态。
- B04 至 B11 正文的状态与字段复核属于另一位作者的 `integrated-review.md`，本报告没有覆盖。

## 修复记录（2026-09-15，复核后）

已按本报告修正，只改动 `b00.md`、`b02.md`、`b03.md`、`b04.md` 四个文档。

- `b00.md:5` 去掉把“错误封装”归给 foundations 的表述。其余公共规则仍链接公共规则，错误体形状改指本文件「未配置认证时的 /api 行为」一节。
- `b00.md:92` 行号改为 host-executor `main.go` 第 18 行与第 19 行。
- `b00.md:154-155` 拆成认证接口与项目接口两行。项目行使用 `writeProjectError` 的 message、fieldErrors 与 requestId 形状，认证行引用 `auth.go` 第 32 行与第 179 行，项目行引用 `projects.go` 第 160 行与第 224 行。
- `b00.md:158`（原第 157 行，表格拆行后下移）当前 503 的证据改为 `verify-batch.ps1` 第 271 行与 B03 后端验收的 `http-/api/projects` 记录；同时注明 B00 记录与 B02 两次验收记录均为 404，并链接两份 B02 checks.json。
- `b02.md:341-342` 交换状态码配对。`AUTHORIZATION_UNCONFIRMED` 为本条连接或全部条目无法确认，补 `credential.go` 第 25 行与 `discovery.go` 第 163 行；`RESULT_UNCONFIRMED` 为批次尚无成功扫描或数据库读取失败，补 `discovery.go` 第 98 行。
- `b02.md:384` 行号改为 coordinator `main.go` 第 53 行至第 56 行。
- `b03.md:229` 改为 effective 对象内的引用与数值字段可为 null，对象本身始终存在。
- `b04.md:5` 去掉错误封装归属。foundations 列表改用“身份作用域、事务与重放、版本与执行边界”，错误体形状指向本章「公共约定」的错误体行。

没有修改 `foundations.md`（没有新增通用错误规范）、`README.md`、`index.html` 或其他章节。按指示没有重新生成 HTML，`render.py --check` 仍会报告 `index.html` 陈旧，这一项留给 root 收尾。

校验方式：编辑后重新打开四个文件的目标行核对；`b00.md` 的 /api 行为表仍为 3 列表格。没有运行产品测试。

复核期间 `foundations.md` 由其他作者更新（01:30），相关行号后移 1 行，本报告引用已改为当前版本的第 113 行与第 131 行。本目录其他章节没有由本次修复改动。

父代理补齐漏项：b02.md 的 callback.go 第165行仅描述代次检查返回；superseded 写入分别引用已由独立复核确认的 start.go:134、session.go:74-79、callback.go:181-188。未扩展源码调查范围。

归档说明：引用片段中的相对链接已按本报告位置调整，原始问题与修复结论不变。
