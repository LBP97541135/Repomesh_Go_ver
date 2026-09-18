# B02 认证实现独立复核

复核时间：2026-09-12（America/Los_Angeles）  
复核身份：非主要实现者，只读审查；未修改产品源码。  
复核基线：未提交工作树。关键文件 SHA-256：`start.go 8f2afd7f`、`callback.go 1a9da690`、`credential.go 5904b949`、`discovery.go 9784ef82`、`worker.go 84b1f431`、`cleanup.go 704ea4d5`、`session.go 0dbf0e28`、`store.go 6cb2a786`、`roots.go 5b83a51a`、`0003_authentication.sql 0c18e34f`、`postgres_test.go a7d72efc`。

## 结论

没有发现仍开放的 P0 或 P1。此前复核发现的两类 P1 均已在最终源码中关闭：

1. **未提交 SecretVersion 泄漏已关闭。** callback 和 refresh 的确定 CAS 失败会永久 `Destroy` 新版本；提交确认未知时不冒险销毁可能已被引用的版本；coordinator 在最长正常写入窗口之后，按 attempts、connections、app_credentials 三组权威引用扫描并永久销毁无引用版本。`TestPostgresStaleRefreshDiscardsUncommittedSecrets` 覆盖迟到 refresh/CAS 失败，并证明现有连接凭据不被清理。
2. **跨浏览器迟到连接覆盖已关闭。** reconnect 在 Start 保存 `expected_connection_revision`，最终提交在 account/connection 锁内比较；匿名 login 在识别 actor 后比较 attempt 创建时间和 `credential_committed_at`。`TestPostgresReconnectCannotReplaceNewerOtherBrowserConnection` 与 `TestPostgresDelayedAnonymousLoginCannotReplaceReconnectedActor` 覆盖两条跨浏览器路径。迟到流程终结为 superseded，不签 Cookie、不覆盖新连接。

同键 Start 由 binding 行锁、唯一 `(binding,id)` 和重读共同收敛；callback 在外部交换前以 attempt 行锁把 pending 原子推进到 exchanging；unknown 状态不会重新领取交换或刷新。binding 的 identity/attempt 两个代次、session generation、连接 revision/access_epoch 和 discovery claim_version 各自承担不同 fence，最终写入均重新核对。

发现分页使用 GitHub 数字稳定 ID 升序并输出零填充本地 ID；游标绑定 actor、query、batch 和 after_id。旧连接代次、过期批次和旧 claim 都不能返回或发布名称。完成批次在首次实际成功交付前可供先前 503 的同查询读取；标记 delivered 后，后续不重叠首页请求建立新批次。该行为符合采用稿的“503 后续原批、完成交付后新首页新批”，没有引入未约定的 exactly-once 读取。

锁顺序未见反向环：callback 最终提交按 binding → attempt → account → connection；reconnect Start 按 binding → session → account 读取 connection；发现发布按 connection → batch；logout 按 binding → session。refresh 通过 connection 的条件 UPDATE 领取唯一责任，最终结果以 revision、access_epoch、refresh_state CAS 提交。秘密根切换先锁活动根，Seal/Rewrap 以活动根共享锁形成写屏障；仅 `destroyed_at IS NULL` 的版本阻止移除旧根。

## 开放问题

### P2：无效 GitHub 基础配置会在启动失败前留下永久引用的 App 凭据版本

`internal/access/deployment.go` 先执行两次 `importAppCredential`，把密文及 `app_credentials` 引用提交到数据库，之后才调用 `github.New` 验证 `AppID`、`ClientID` 和 callback。可复现路径是：使用权限正确且内容非空的 client-secret/private-key 文件，同时把 `appId` 配成非正整数；`OpenRuntime` 最终因 `github.New` 拒绝配置而失败，但两个 `app_credentials` 行及其 enabled SecretVersion 已经提交。孤儿扫描会把它们视为权威引用，修正成另一个 App ID 后旧版本仍不会清理。

影响限于可信部署配置错误，不构成远程越权，但会让一次未成功启用的配置永久保留敏感版本并消耗包装额度。建议在任何 Seal/INSERT 前完成不依赖秘密内容的 GitHub 配置校验，或让部署凭据导入具有显式激活/回滚生命周期。

## 源码审查证据

- `internal/jsoninput/decode.go` 对 UTF-8、256 KiB、重复对象键、非法 surrogate、深度、尾随值和未知字段采取失败关闭；HTTP 层还限定 JSON content type、Origin、CSRF 和单值 Idempotency-Key。
- Cookie 使用 `__Host-`、Secure、HttpOnly、SameSite=Lax、Path=/ 且无 Domain；callback 只消费有界 state/code/error，固定 303 落点并设置 no-store/no-referrer。
- GitHub 客户端固定 HTTPS 主机、禁环境代理与重定向、限制响应头/体和 5 秒请求时间；错误不携带上游正文。官方当前文档允许 JWT `iss` 使用 Client ID，并确认用户仓库列表使用 Metadata read、每页最大 100，因此这两处实现与当前 cloud 文档一致。
- migration 0002/0003 的外键、唯一键、状态 CHECK、版本/代次列和部分索引与调用方 SQL 一致。未发现 SQL 参数拼接进入业务查询；唯一动态 SQL 是测试数据库标识符，使用 pgx Identifier 清洗。
- `git diff --check` 通过，仅出现仓库既有 LF/CRLF 转换提示；目标包 `go vet` 通过。

## 实测证据

已完成并通过：

- `GOCACHE=/tmp/repomesh-b02-review-go-cache go test -count=1 -race ./internal/jsoninput ./internal/github ./internal/secrets ./internal/access ./internal/web`
  - 无数据库环境时：jsoninput、github 通过；PostgreSQL 用例按辅助器规则跳过。
- 使用 `/tmp/repomesh-b02-postgres.json` 私密连接注入环境、由各测试辅助器创建独立数据库：
  - `go test -count=1 -race ./internal/access ./internal/secrets ./internal/web`
  - access `ok 6.072s`，secrets `ok 4.927s`，web `ok 1.719s`。

最终仅新增的 `TestPostgresDiscoveryOldClaimCannotPublish` 已做静态路径核对：旧 worker 的 claim=1 在新 worker 把 claim_version 推进并提交后，会在最终 batch 行锁内观察不匹配并回滚，无法发布旧结果。本复核任务恢复后尝试单跑该测试，但当前工具 PID/network namespace 看不到父任务仍在运行的 PostgreSQL，测试在“cannot connect test database administrator”处退出，未进入测试体；没有启动或停止实例，也没有输出连接 URL。因此该新增测试的最终运行证据须由能访问原实例的主任务补充，不能把这次环境失败记作产品失败或 PASS。

## 未验证边界

- 用户已确认没有配置真实 GitHub App；未执行真实 OAuth 授权、code 交换、refresh、App 安装/权限、私有仓库发现或浏览器跨站往返。替身与本地 HTTP 测试只能证明 RepoMesh 本地协议和故障语义。
- 未做数据库进程 kill、真实 commit-ack 丢失、系统时钟回拨或长期 15 分钟孤儿窗口实验；这些路径按源码状态机和短事务边界审查。
- 未把 `/healthz`、`/readyz` 或后续项目/Issue/模型批次解释为 B02 已实现能力。

## `no-comments` 手工审查

任务明确要求不安排本复核自己的 agent，因此没有按 skill 默认流程启动 comment-sicko，而是按其规则手工检查本批新增 Go 源码注释。源码只读，实际删除 0，恢复 0，重跑 0，未调用 architect。

接受两处后续清理建议：

1. `internal/github/client.go:117` 的 “A lost token response...” 把“不重发”归因到 `req.GetBody=nil`，但固定客户端已拒绝重定向，POST 也没有 net/http 自动重试资格；该行及注释都是冗余，测试已编码真正的不重试行为。
2. `internal/secrets/crypto.go:114` 解释本地语句顺序；可直接删除注释，认证解密失败即返回已经编码了 rewrap 前校验正文的约束。

`internal/secrets/roots.go:25` 保留：紧邻的 `SELECT ... FOR UPDATE` 没有读取结果，其唯一作用依赖 PostgreSQL 外部锁语义，注释解释了无法从 Go 数据流看出的跨进程写屏障。build tags 属于编译器指令，不计普通注释。

## 最终增量复核与关闭状态

本节追加于上述历史审查之后，不改写当时的发现和测试条件。最终增量源码 SHA-256 基线为：`deployment.go a76a000b`、`client.go 16a78385`、`crypto.go 9f1579e6`、`deployment_test.go 0d248343`。

此前唯一开放的 P2 已关闭。`OpenRuntime` 现在于打开数据库、注册根、Seal 或写入 `app_credentials` 之前先调用 `github.New`；App ID、Client ID 和 callback 的纯配置校验失败直接返回。传给构造器的秘密闭包捕获局部 `store`、`clientRef` 和 `privateRef`，但 `github.New` 当前只验证字段而不调用闭包；两个引用完成导入之后才把 provider 赋给 Service 并通过完整 Runtime 对外返回，因此不存在可达的 nil Store/空引用调用窗口。

`TestPostgresDeploymentInvalidAppDoesNotImportSecrets` 使用合法私密文件和非法 App ID 调用完整 `OpenRuntime`，随后直接断言 `app_credentials` 引用数、SecretVersion 数和根 `wrap_count` 均为 0。该断言覆盖了原 P2 的持久副作用，而非只检查错误返回。

主任务的真实 PostgreSQL `-race` 证据归属如下；本复核独立读取并核对了 JSONL 事件，没有把本 agent 先前受 network namespace 限制的失败改写为自己的 PASS：

- `docs/development/2026-09-12-batch-02/final-postgres-race.jsonl`：`TestPostgresDiscoveryOldClaimCannotPublish` PASS（0.21s）；access、secrets、web 包分别 PASS（6.807s、4.156s、1.662s）。这关闭了上文“须由主任务补充”的旧 claim 运行证据缺口。
- `docs/development/2026-09-12-batch-02/final-postgres-race-02.jsonl`：`TestPostgresDeploymentInvalidAppDoesNotImportSecrets` PASS（0.26s），旧 claim 再次 PASS（0.20s）；access、secrets、web 包分别 PASS（7.494s、4.498s、1.802s）。
- 本复核另独立运行不需 PostgreSQL 的 deployment JSON/HTTPS callback 定向测试，PASS；最终 `git diff --check` 仍通过，仅有既有换行提示。

`no-comments` 的两条接受建议也已落实：`client.go` 中误归因“不重发”的注释及冗余 `req.GetBody=nil` 已删除，`crypto.go` 的本地语句顺序注释已删除；根写屏障注释按原审查结论保留。最终增量删除相关注释 2、冗余赋值 1，恢复 0，未产生新的未编码约束。

**最终判定：PASS。B02 核心产品代码没有开放的 P0、P1 或 P2 finding。** 真实 GitHub App 尚未配置，因此真实 OAuth、refresh、App 安装权限和私有仓库发现仍是外部验收限制，不影响本次本地实现复核的 PASS，也不能据此宣称真实 GitHub 链路已 VERIFIED。

## 最终 UI 代次并发子项

本子项按 `pstack:typescript-best-practices` 及其 `principle-type-system-discipline` 前置规则，只读复核 `AuthResult` 的 session/attempt 查询交接，不扩大到其他前端实现。源码 SHA-256 基线：`AuthResult.tsx dfb1245f`、`session.ts 0a14aad6`、`main.tsx 5bcc1b22`。

`AuthResult.run` 现在于 `refresh()` 前捕获 `startedGeneration`，refresh 返回后先比较 `currentGeneration()`；若首次 session 读取把状态从 checking/anonymous 推进到新的已认证代次，旧 effect 立即返回，尚未创建 attempt 请求的 AbortController，也不会消费随后一次 429。`useSession.update` 在身份或 CSRF 身份发生变化时同步推进 ref 版本并更新 `generation`；`AuthResult` effect 依赖该 generation，且 `main.tsx` 用 `${attemptId}:${generation}` 作为组件 key。代次更新会清理旧 effect 的 timer/controller，并以全新局部 `retryAt`、`inFlight` 和 pollingStep 挂载新组件；同代次 refresh 才继续执行 `readAttempt`，其响应仍由 `isCurrent(expected)` 二次隔离。该两级 fence 没有 cast、`any` 或新增不可表示状态。

主任务浏览器证据归属如下；本复核没有在受限 namespace 中另启浏览器：

- `browser-04/polling-debug.json` 记录修复前复现：attempt 查询数为 3、POST 为 0，证明旧组件与新组件查询重叠，同时证明未自动重发写请求。
- `browser-05/checks.json` 记录 Chromium 153 下 9 个场景全部 PASS，包括 Retry-After 冷却、页面 hidden 暂停、无 POST 重发，以及注销后迟到仓库响应隔离；`externalGitHub` 明确为 `NOT_RUN`。
- 本复核独立运行 `npm --prefix web run typecheck`，PASS。

**UI 子项判定：PASS，无新增开放 finding。** 修复阻止旧 session 代次的 AuthResult 查询抢先消费 429，新代次组件保留唯一的轮询与冷却责任；真实 GitHub 浏览器往返限制仍与前述最终结论一致。
