# B02 候选 A：领域包直连 PostgreSQL

## Problem

B02 要在当前只有迁移器和静态 HTTP 骨架的工程中一次建立秘密、持久认证、GitHub 连接及持久发现。难点不是端点数量，而是几类失败语义不同：根密钥包装次数必须在业务写入前独立、保守地消耗；OAuth code 和 refresh token 的外部交换一旦结果未知就不能再领；仓库发现是只读工作，可以由 coordinator 持久续扫；浏览器晚到的回调、Cookie 和注销响应不能推翻较新的代次。GitHub App 尚未配置，因此实现只能报告配置缺失或 unknown，不能制造已登录、已连接、完整 coverage 或目标有权。

## Usage (caller's view)

Web 用现有 `database.DB` 打开并关闭连接，通过只读 `DB.Pool()` 取得共享 pool；领域包各自持有 `*pgxpool.Pool`，HTTP 只解析边界值、调用一个用例并映射领域结果：

```go
db, _ := database.Open(ctx, databaseURL)
pool := db.Pool()
secrets := secrets.New(pool, roots)
auth := auth.New(pool, secrets, github.NewClient(httpClient, appConfig), clock)
repos := discovery.New(pool, auth, clock)
handler := web.NewHandler(assets, auth, repos)

start, cookie, err := auth.StartLogin(ctx, auth.StartCommand{
    AttemptID: idempotencyKey, BindingCookie: requestCookie,
    Origin: origin, Destination: destination,
})
result, sessionCookie, err := auth.CompleteCallback(ctx, auth.CallbackCommand{
    BindingCookie: requestCookie, Code: code, State: state,
})
err = auth.Logout(ctx, auth.LogoutCommand{SessionCookie: c, Origin: o, CSRF: token})
```

Coordinator 只运行可持久接管的后台职责：

```go
worker := discovery.NewWorker(pool, secrets, githubClient, clock)
for worker.RunOne(ctx, coordinatorID) { /* bounded claim */ }
refreshWorker := auth.NewRefreshWorker(pool, secrets, githubClient, clock)
```

`GET /api/repositories` 调 `repos.Page(ctx, actor, query)`；它可以创建或复用批次并入队，但不等待 coordinator 完成全部扫描。`GET /api/auth/attempts/{id}` 只读本地状态，不交换、刷新或写业务。

## Shape

### Modules and public signatures

```text
internal/database       DB.Pool() *pgxpool.Pool；DB 仍独占打开、迁移与关闭
internal/secrets        根加载、自检、Seal/Open；不认识 GitHub 或 HTTP
internal/auth           账号、binding/session/attempt/connection、交换与刷新状态机
internal/github         github.com 固定协议适配；wire DTO 不越过本包
internal/discovery      批次、游标、仓库投影及 coordinator 工作
internal/web            严格 JSON/Cookie/Origin/CSRF、固定路由和错误映射
cmd/repomesh-web        配置检查、pool 和上述对象组装
cmd/repomesh-coordinator 同一配置检查、pool、发现与刷新 worker 循环
```

```go
// secrets
func (s *Store) Seal(ctx context.Context, owner Owner, purpose Purpose, plaintext []byte) (VersionID, error)
func (s *Store) Open(ctx context.Context, id VersionID, expected OwnerPurpose) ([]byte, error)
func (s *Store) SetAvailability(ctx context.Context, id VersionID, enabled bool, reason Reason) error

// auth
func (s *Service) StartLogin(context.Context, StartCommand) (StartResult, BindingCookie, error)
func (s *Service) StartReconnect(context.Context, ReconnectCommand) (StartResult, error)
func (s *Service) CompleteCallback(context.Context, CallbackCommand) (CallbackResult, *SessionCookie, error)
func (s *Service) Attempt(context.Context, AttemptQuery) (AttemptResult, error)
func (s *Service) Session(context.Context, SessionCookie) (SessionView, error)
func (s *Service) Logout(context.Context, LogoutCommand) error
func (s *Service) Credential(context.Context, ActorID) (CredentialResult, error) // current, enqueue-refresh, or unknown

// discovery
func (s *Service) Page(context.Context, Actor, RepositoryQuery) (RepositoryPage, error)
func (w *Worker) RunOne(context.Context, WorkerID) bool
```

`Destination` 是封闭 Go 联合（每种结构独立类型），解析器拒绝额外字段、路径字符和 B02 未开放的 `conversation_message`。资格解析接口返回 `allowed/denied/unknown`；只有 allowed 才生成固定 `nextPage`，其余分别回 `/` 或 `null`。尚未实现的 project/issue/conversation/provider 用一个 `DestinationAuthorizer` 返回 unknown，而不是伪造有权。

### Data and constraints

迁移 0002 建以下核心表（ID 用 UUID 或受长度约束的 text；所有时间为 `timestamptz`）：

```sql
root_keys(root_key_id PK, fingerprint bytea UNIQUE CHECK(length(fingerprint)=32),
          state CHECK(state IN ('active','unwrap_only','retired')),
          wrap_count bigint CHECK(wrap_count BETWEEN 0 AND 1000000));
secret_versions(version_id PK, secret_id, owner_kind, owner_id, purpose,
  ciphertext bytea, wrapped_dek bytea, root_key_id REFERENCES root_keys,
  format_version int, created_at, UNIQUE(secret_id,version_id));
secret_availability(version_id PK REFERENCES secret_versions, enabled, access_epoch bigint, checked_at, reason);

accounts(account_id PK, disabled_epoch bigint, created_at);
github_identities(github_account_id bigint UNIQUE, account_id UNIQUE REFERENCES accounts);
browser_bindings(binding_hash bytea PK CHECK(length(binding_hash)=32),
  identity_generation bigint, attempt_generation bigint, expires_at);
sessions(session_hash bytea PK, account_id REFERENCES accounts, binding_hash, binding_identity_generation,
  account_epoch, csrf_hash bytea, created_at, last_active_at, idle_expires_at, absolute_expires_at, revoked_at);
auth_attempts(attempt_id uuid, binding_hash, binding_identity_generation, binding_attempt_generation,
  purpose, account_id,
  expected_github_account_id bigint, destination_kind, project_id, object_id, operation_kind,
  material_secret_version_id REFERENCES secret_versions, state_hash bytea,
  status, reason, exchange_claim_id uuid, connection_revision bigint, expires_at, observed_at,
  PRIMARY KEY(binding_hash,purpose,attempt_id),
  UNIQUE(state_hash),
  CHECK(destination columns form exactly one adopted union member));
github_connections(account_id PK REFERENCES accounts, github_account_id bigint UNIQUE,
  revision bigint, access_epoch bigint, access_secret_version_id REFERENCES secret_versions,
  refresh_secret_version_id REFERENCES secret_versions, access_expires_at, refresh_expires_at,
  state, observed_at);
refresh_responsibilities(account_id, connection_revision, claim_id uuid, status, result_revision,
  claimed_at, observed_at, PRIMARY KEY(account_id,connection_revision));

discovery_batches(batch_id PK, account_id, connection_revision, access_epoch, query_hash,
  status, coverage, coverage_reason, upstream_cursor_secret_version_id, expires_at);
CREATE UNIQUE INDEX one_active_discovery_batch
  ON discovery_batches(account_id,connection_revision,access_epoch,query_hash)
  WHERE status IN ('queued','scanning');
discovered_repositories(batch_id, repository_id bigint, owner, name, visibility, permission_observation_id,
  PRIMARY KEY(batch_id,repository_id));
discovery_work(batch_id PK REFERENCES discovery_batches, claim_version bigint, lease_until,
  next_run_at, last_error_code);
permission_observations(id PK, account_id, repository_id, connection_revision, access_epoch,
  capability, decision, reason, observed_at);
```

包装配额的唯一 SQL 是独立事务中的条件更新：

```sql
UPDATE root_keys SET wrap_count = wrap_count + 1
WHERE root_key_id=$1 AND state='active' AND wrap_count < 1000000
RETURNING fingerprint, wrap_count;
```

该事务先提交，之后才在内存生成 DEK、AES-256-GCM 密封正文和 DEK，并插入 `secret_versions`。加密、插入、调用方业务事务失败都消耗额度；自检和重包也走同一入口。AAD 从数据库权威 owner/purpose、format/version/root ID 作长度编码。秘密版本可先成为无引用孤儿，受保留期 GC；业务可见性只来自连接或尝试的外键引用。

### State transitions and transactions

`StartLogin` 先做限流和原键只读快查；可重放直接返回，明显冲突直接拒绝。确需创建时才生成并 `Seal` 一份 `{state,pkceVerifier}` 材料（一次包装），再在短事务锁 `browser_bindings` 作权威复核。同键同输入且仍可导航就读回胜者原材料并返回同一 URL；不同输入冲突；新 attempt 只递增 `attempt_generation`，并把旧未完成 attempt 改为 superseded。`identity_generation` 表示当前浏览器身份，不能被另一个 start 偷换。并发落败者允许白白消耗包装额度，但不得产生第二个有效 attempt。已登录 login 返回 `SESSION_ALREADY_ACTIVE`；换账号只能 logout 后新 login。Reconnect 额外固定 account、GitHub stable ID 和原连接 revision。

Callback 事务一按 binding、唯一 `state_hash`、两个代次校验 attempt，再用 `UPDATE ... WHERE status='pending' RETURNING` 原子写入 `exchange_claim_id/status='exchanging'`。事务外只交换一次 code。交换响应未知时写 `unknown/EXCHANGE_UNCONFIRMED`，该责任永久不可重领。交换成功后令牌先经 SecretStore 封装；身份读取可在 attempt 到期前用已保护 token 重试。最终短事务重新锁 binding/attempt/account/connection：两个代次仍匹配才原子写 identity、连接 revision、session 摘要和 confirmed，同时递增 `identity_generation`，撤销该 binding 的旧 session，并让新 session 捕获新代次；reconnect 账号不同只写 rejected，旧连接不动。提交成功后才发 session Cookie 和 303。

Logout 用 session_hash 锁定请求所带会话，只撤销它；仅当 binding 当前 `identity_generation` 等于该 session 捕获值时，才递增身份代次并 supersede 该身份代次下未完成尝试。旧/已撤销 session 幂等 204，不发删除 Cookie，不触碰新代 session。

Refresh 由 coordinator 以 `(account_id, connection_revision)` 插入一次 `refresh_responsibilities` 领取。外部结果未知即 unknown，绝不因租约超时重刷旧 token；显式 reconnect 产生更高连接 revision。成功令牌封装后，以 `WHERE revision=$old AND access_epoch=$epoch` CAS 提交新 revision；CAS 失败的迟到结果只留下不可用秘密版本。Web 只读取可用凭据或持久请求刷新，不自行刷新。

发现工作同外部交换不同：它只做幂等 GET，所以 `discovery_work` 可用有限租约和递增 `claim_version` 重领。每次最多两页/五秒，事务提交仓库、上游续点和下一任务；写入必须同时匹配 batch、连接 revision、access epoch、claim_version。首页复用同 actor/revision/epoch/q 的未完批，完成或十分钟过期才建新批；旧 cursor 在重连/epoch 变化后返回 409。扫描结束仍按采用范围写 `coverage=partial, reason=APP_INSTALLATION_SCOPE`，不能升级 complete。

HTTP 层统一设置 no-store，严格限制 256 KiB JSON、Origin/CSRF、Cookie 属性、查询长度和日志字段。限流桶按 binding/account 持久化更新；重放先核原 attempt，再决定是否消耗新发起额度。`/readyz` 只有数据库、根自检、GitHub 非秘密配置与 coordinator 心跳满足 B02 定义后才可单独表达认证就绪；不得改称整个产品业务 ready。

## Synthesis decision

本候选刻意采用“领域包直接持有 pool、HTTP 薄适配”的 A 形状。它用数据库约束和每个领域的一层深接口隐藏事务、加密及竞态；调用链保持在 handler → domain → PostgreSQL/GitHub 三层内。后续 `secrets`、`github`、`discovery`、`auth`、`web/UI` 可按目录和迁移表所有权并行实现；共享处只有已冻结的领域类型、SQL 表和组装函数。

## Tradeoffs accepted

- 我们接受各领域包重复少量 pgx 错误映射，以换取事务边界留在真正拥有不变量的包内。
- 我们接受并发落败、业务回滚和孤儿密文消耗包装额度，以换取计数绝不低估。
- 我们接受未知 exchange/refresh 需要新登录或重连，以换取不重复消费一次性上游凭据。
- 我们接受发现结果在单 App 范围内长期为 partial，以换取不虚构完整账号覆盖。

## Alternatives considered

- 单一 repository/service 层：它表面统一数据库访问，却把 attempt 代次、秘密预扣和发现 claim 的事务细节暴露给上层编排，接口更宽、调用链更长。
- 全部外部工作共用通用租约队列：它隐藏队列代码，却错误地允许一次性交换在租约后重领；读扫描和不可重复写责任必须是两种状态机。
- Web 同步完成发现和刷新：实现文件少，但请求生命周期承担持久责任，崩溃后无法区分未知交换，也无法满足 coordinator 续扫。

## Open questions and risks

- 部署是否允许 Web 和 coordinator 读取同一 root-key 集合，并以数据库权限限制各自可引用的 purpose？若不能，需要每进程独立 rootKeyId 和重包计划。
- GitHub App 是否启用 expiring user tokens？若未启用，refresh worker 应保持无任务，而不能虚构 refresh token。
- `permission_observations` 的 B02 保留期和 coordinator 心跳阈值取何值，才能支持审计而不把旧观察误当当前权限？

## Next implementation step

先提交 `DB.Pool()`、0002 迁移及 `internal/secrets`（含真实 PostgreSQL 并发预扣、AAD 篡改、自检和根轮换测试）；B01 迁移算法测试固定使用 0001 fixture，另加针对完整产品迁移集的集成测试，再让 auth 只依赖稳定 `Seal/Open` 接口。
