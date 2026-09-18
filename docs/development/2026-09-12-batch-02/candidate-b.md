# B02 方案 B：访问聚合模块

## Problem

B02 把 GitHub 登录、加密凭据、服务端会话和可续扫的仓库发现接到当前数据库迁移和静态 Web 骨架。回调同时受浏览器绑定代次、尝试领取、账号绑定、秘密包装配额、连接代次和旧会话注销约束；发现又不能依赖某个 HTTP 请求继续存活。方案 B 由 internal/access 集中拥有这些状态转换和外部调用的次序。HTTP 与 coordinator 是适配器，不能自行拼事务。它不把尚未配置的 GitHub App、真实 OAuth 或项目/Issue 权利写成已可用。

## Usage (caller's view)

~~~go
// HTTP: start, callback, session, logout, and repository projection.
started, err := access.Start(ctx, start)
outcome, err := access.CompleteCallback(ctx, callback)
page, err := access.Repositories(ctx, actor, query)

// coordinator: a bounded durable unit; raw tokens are never supplied.
err := access.ContinueDiscovery(ctx, task)
~~~

The handler validates adopted JSON, Origin and CSRF, maps typed errors to the browser contract, and issues a cookie only after a confirmed outcome. It never opens a transaction, decrypts a token, generates an OAuth URL, or schedules work. Coordinator receives only a leased task identity.

## Shape

~~~text
internal/access/
  service.go       public operations and domain types
  store.go         PostgreSQL records and transactions; uses database.DB.Pool()
  secrets.go       root validation, envelope crypto, durable wrap reservations
  github.go        narrow outbound port; provider wire structures private
  discovery.go     batches, CAS claims, projection and task persistence
~~~

The database package remains responsible for opening/closing a verified pool and migrations. Its narrow proposed addition is:

~~~go
func (db *DB) Pool() *pgxpool.Pool
~~~

access.Store accepts that pool and starts short transactions. This avoids duplicating migration or connection policy, while keeping B02 schema knowledge out of internal/database.

~~~go
type Service struct { /* Store, SecretStore, GitHubClient, Clock; unexported */ }

type ActorID string
type AttemptID string
type ConnectionRevision string
type AccessEpoch int64
type DiscoveryBatchID string
type ClaimVersion int64

type StartRequest struct {
    AttemptID AttemptID
    Purpose AttemptPurpose // login | reconnect
    Binding BrowserBinding
    Actor *ActorID         // required for reconnect
    Destination Destination // closed domain form of the adopted union
}
type StartResult struct {
    AttemptID AttemptID
    AuthorizationURL string
    ExpiresAt time.Time
    ResultPage string
}

func (s *Service) Start(context.Context, StartRequest) (StartResult, error)
func (s *Service) CompleteCallback(context.Context, CallbackRequest) (CallbackOutcome, error)
func (s *Service) Attempt(context.Context, AttemptReader, AttemptID) (AttemptResult, error)
func (s *Service) Logout(context.Context, SessionProof) error
func (s *Service) Session(context.Context, SessionProof) (SessionView, error)
func (s *Service) Repositories(context.Context, ActorID, RepositoryQuery) (RepositoryPage, error)
func (s *Service) ContinueDiscovery(context.Context, DiscoveryTask) error
~~~

Start canonicalizes idempotency input and, in one transaction, writes/replays a browser binding and attempt, encrypts state/PKCE verifier, and advances binding generation to supersede older unfinished attempts. Reconnect snapshots actor, external account, and connection revision. The authorization URL comes only from fixed deployment App configuration.

CompleteCallback hides a necessary three-step protocol: (1) short transaction CAS-claims a live attempt as exchanging; (2) GitHub exchange and identity lookup outside a transaction; (3) short transaction rereads claim, binding generation, account and connection revision, then commits credentials and a session. A consumed, expired, cancelled, or superseded attempt never exchanges a second code. Indeterminate exchange or commit becomes unknown; retry does not reuse its code. Logout revokes its request session and only advances unfinished attempts if that session's binding generation is still current, so a delayed old response cannot revoke a replacement login.

### Secret boundary

~~~go
type SecretRef struct { versionID string } // opaque; no ciphertext, key, or String method
type SecretPurpose uint8
const (
    GitHubAccessToken SecretPurpose = iota + 1
    GitHubRefreshToken
    GitHubAppClientSecret
    GitHubAppPrivateKey
)
type SecretOwner struct { Kind OwnerKind; ID string }

type SecretStore interface {
    Seal(context.Context, SecretOwner, SecretPurpose, []byte) (SecretRef, error)
    WithPlaintext(context.Context, SecretRef, SecretOwner, SecretPurpose, func([]byte) error) error
}
~~~

This is compatible with independent Secrets.Seal/Open work: retain Seal as above and name Open as WithPlaintext, so callers cannot retain plaintext beyond a callback. If another implementation requires Open, its return type must still be an unexported scoped handle, never a byte slice available to routes or tasks. The GitHub adapter is similarly narrow:

~~~go
type GitHubClient interface {
    Exchange(context.Context, OAuthExchange) (TokenSet, error)
    CurrentAccount(context.Context, SecretRef) (ExternalAccount, error)
    Refresh(context.Context, SecretRef) (TokenSet, error)
    ListRepositories(context.Context, SecretRef, string) (GitHubRepositoryPage, error)
}
~~~

Only access can obtain plaintext through SecretStore to implement this port. Thus Secrets and GitHubClient can be implemented independently without leaking transaction choreography to either.

Before each envelope seal, SecretStore commits a separate conservative root-wrap reservation. It then uses a fresh DEK and AES-256-GCM with canonical length-delimited AAD over format, secret version, owner and purpose; wrapped DEK AAD also includes root-key ID. Reservation counts are durable and never refunded after a business rollback/crash. Root registry stores ID, fingerprint, active/retired state and count; root body never enters DB, config, errors or logs. Startup validates file permission/fingerprint and self-test. Rotation rewraps DEKs by CAS without changing SecretRef; disable advances secret availability/access epoch without deleting ciphertext.

### Connection refresh and discovery

A connection owns connectionRevision, refreshClaim, refresh state, secret references, and accessEpoch. Refresh is private to Service: it CAS-claims that exact revision before the external call. A claim with unknown external outcome blocks a parallel retry even after a lease time; controlled reconnect alone creates a newer revision. Successful refresh atomically replaces token refs and advances connection revision/access epoch, invalidating observations. This prevents GitHub refresh-token rotation from being raced.

~~~go
type DiscoveryTask struct { BatchID DiscoveryBatchID; Claim ClaimVersion }
type DiscoveryBatch struct {
    ID DiscoveryBatchID; Actor ActorID; Connection ConnectionRevision
    Epoch AccessEpoch; Query CanonicalRepositoryQuery
    UpstreamCursor string; State BatchState; ExpiresAt time.Time
}
~~~

First-page queries reuse a live batch keyed by actor, connection revision, epoch and canonical query, otherwise create it. ContinueDiscovery CAS-leases a batch, reads at most two upstream pages or five seconds, writes stable GitHub repository IDs and observations, then persists next cursor and exactly one finite follow-up task in one transaction. Stale claim, expiry, or changed epoch cannot write. Read projection rechecks current actor/epoch and returns names only for currently allowed repositories; no reliable result is 503, never empty complete data. Cursor binds actor, batch, query/filter and connection epoch.

The interface is deliberately deep: it hides canonical input comparison, crypto/AAD, reservation accounting, transactions, external I/O boundaries, CAS, and durable scheduling behind six use cases. This follows boundary-discipline and idempotent-transition rules while leaving HTTP transport concerns at the HTTP edge.

## Synthesis decision

This candidate chooses an aggregate access module over separate auth/token/discovery services. A callback must atomically coordinate attempt state, secret version, connection revision, session, observation invalidation and follow-up work. Splitting those responsibilities makes routes or callers compose the transaction and recheck rules. The aggregate preserves independently testable SecretStore and GitHubClient ports and supports separate ownership of HTTP adapters, secrets, and coordinator wiring.

## Tradeoffs accepted

- We accept a larger internal module in exchange for callers that cannot combine stale sessions, credentials and discovery writes.
- We accept lost wrap reservations and unknown refresh claims in exchange for no counter reset or parallel refresh after a crash.
- We accept partial durable discovery in exchange for bounded work that survives HTTP completion and restart.
- We accept no logout response cookie deletion in exchange for preventing an old response from deleting a newer login cookie.

## Alternatives considered

1. Route-owned transactions plus TokenStore/DiscoveryWorker helpers exposes crypto ordering and recheck timing to every route; shallow interface, rejected.
2. A transaction held over token exchange causes pool contention and cannot resolve unknown exchange results; rejected.
3. HTTP-spawned discovery goroutines lose state on request/process end and permit an old result to overwrite a reconnect; rejected.

## Counterexamples and tests

- Concurrent identical starts yield one stored attempt and same URL; same key with changed purpose/destination yields idempotency conflict.
- Callback A is claimed, then logout/new start advances binding generation: A cannot create a session or connection; replay makes no second exchange.
- Reconnect with another stable GitHub account preserves old connection and returns account mismatch.
- Commit-ack loss after exchange leaves unknown; old code is never retried.
- A failed seal after reservation still increments durable count; restart does not reset it. Wrong root/AAD/owner/purpose and disabled secret reveal no plaintext.
- Unknown refresh blocks same-revision refresh; reconnect advances revision; late refresh cannot overwrite it.
- Kill coordinator after upstream-page commit: one follow-up task remains. Concurrent claim has one writer; epoch change makes old task harmless. Verify partial coverage, empty page with next cursor, 503 for no reliable data, and no old repository name after access loss.

## Open questions and risks

- What development-only root-file and HTTPS/Cookie setup preserves production Host-cookie guarantees?
- Which deployment mechanism registers App configuration and root-key fingerprint before a real GitHub App exists?
- Is the one-million wrapping ceiling fixed in B02 migration or an audited deployment setting?

## Next implementation step

Add B02 records and PostgreSQL tests for root registry, secret versions/availability, bindings, attempts, sessions, connections, batches and leased tasks; prove transaction/CAS cases before routes or coordinator startup.

