package access

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"repomesh.local/repomesh/internal/github"
	"repomesh.local/repomesh/internal/secrets"
)

const (
	// participationCacheTTL 是参与权探测结果的复用窗口。5 分钟足够覆盖
	// "打开表单 → 选仓 → 提交"的交互,又不会让权限变更长时间读不到;
	// 提交路径不依赖它（提交路径有自己的一份 60 秒现验，见 CheckIssueObservationTime）。
	participationCacheTTL = 5 * time.Minute
	// participationFreshness 是**现探**出来的观测还能用多久 —— 写路径
	// （创建/更新/项目仓库读）与提交路径的窗口。
	//
	// 两个窗口必须各校各的：**拿 60 秒去校 5 分钟的缓存，那个窗口里的观测必然全灭。**
	// 2026-09-20 线上实测就是这个错配 —— `issue-creation-options` 整份 503
	// AUTHORIZATION_UNCONFIRMED，重试也不管用（还在缓存窗口里），过几分钟又自己好了。
	// 现在由 CheckProjectObservationFresh / CheckProjectObservationCached 两个入口
	// 把窗口写死，调用方按观测的来源选入口即可，不会再各写一个秒数。
	participationFreshness = 60 * time.Second
	// participationProbeConcurrency 是单次全量探测的最大并发。
	// 8 路够把 49 仓从十几秒压到约 2s,又不至于把 GitHub 打限流。
	participationProbeConcurrency = 8
)

type ProjectPrincipal struct {
	actor       string
	sessionHash string
	binding     string
	generation  int64
}

func (p ProjectPrincipal) ActorID() string { return p.actor }

type RepositoryLocator struct {
	ID         string
	Host       string
	ExternalID int64
	Owner      string
	Name       string
}

type RepositoryObservation struct {
	Locator              RepositoryLocator
	ParticipationStatus  string
	ParticipationReasons []string
	ObservedAt           *time.Time
	Item                 *RepositoryItem
}

type ProjectObservation struct {
	actor               string
	connectionRevision  string
	accessEpoch         int64
	repositories        []RepositoryObservation
	requiresConnection  bool
	authorizationFailed bool
}

func (o ProjectObservation) Repositories() []RepositoryObservation {
	return append([]RepositoryObservation(nil), o.repositories...)
}

// AuthorizationFailed 表示凭据级失败(所有仓都读成 unknown),调用方应整体
// 失败(fail closed)。单仓的 unknown 是数据漂移,只让那个仓不可选。
func (o ProjectObservation) AuthorizationFailed() bool { return o.authorizationFailed }

func (s *Service) AuthenticateProjectRequest(ctx context.Context, cookie, csrf string, write bool) (ProjectPrincipal, error) {
	session, err := s.Session(ctx, cookie)
	if err != nil {
		return ProjectPrincipal{}, err
	}
	if write {
		if err := requireCSRF(session, csrf); err != nil {
			return ProjectPrincipal{}, err
		}
	}
	return ProjectPrincipal{actor: session.User.ID, sessionHash: digest(cookie), binding: session.Binding, generation: session.Generation}, nil
}

func (s *Service) LockProjectPrincipal(ctx context.Context, tx pgx.Tx, principal ProjectPrincipal) error {
	return s.verifyProjectPrincipal(ctx, tx, principal, true)
}

// CheckProjectPrincipal 校验与 LockProjectPrincipal 完全相同的事实，但**不加行锁**。
//
// 2026-09-20 线上实测：读路径此前也走 FOR UPDATE，于是同一个人的并发轮询
// （issue 详情 / 拓扑 / 计划 / 发现链 / 测试证据 会一起发）全都在同一批
// bindings + sessions + accounts 行上排队；只要其中一个请求稍慢，排在后面的
// 请求就撞上 lock_timeout=2s，被判成 503 RESULT_UNCONFIRMED —— 界面显示
// "服务端暂时不可用"，而事实是服务好好的，只是被自己的读请求堵住了。
//
// 行锁对读没有意义：读不写任何东西，不需要"校验通过后到提交前会话不被吊销"
// 这个保证。写路径继续用 LockProjectPrincipal，语义不变。
func (s *Service) CheckProjectPrincipal(ctx context.Context, tx pgx.Tx, principal ProjectPrincipal) error {
	return s.verifyProjectPrincipal(ctx, tx, principal, false)
}

func (s *Service) verifyProjectPrincipal(ctx context.Context, tx pgx.Tx, principal ProjectPrincipal, lock bool) error {
	// 锁定与否只差一个后缀：两条路径必须校验同一批事实，不能各写一份 SQL 而漂移。
	suffix := ""
	if lock {
		suffix = " FOR UPDATE"
	}
	var identity int64
	var bindingExpires time.Time
	if err := tx.QueryRow(ctx, `SELECT identity_generation,expires_at FROM repomesh_access.bindings WHERE hash=$1`+suffix, principal.binding).Scan(&identity, &bindingExpires); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return failure(401, "AUTHENTICATION_REQUIRED")
		}
		return unavailable()
	}
	var actor, binding string
	var generation int64
	var expires, lastActive time.Time
	var revoked bool
	if err := tx.QueryRow(ctx, `SELECT actor,binding,generation,expires_at,last_active_at,revoked FROM repomesh_access.sessions WHERE hash=$1`+suffix, principal.sessionHash).Scan(&actor, &binding, &generation, &expires, &lastActive, &revoked); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return failure(401, "AUTHENTICATION_REQUIRED")
		}
		return unavailable()
	}
	var disabled bool
	if err := tx.QueryRow(ctx, `SELECT disabled FROM repomesh_access.accounts WHERE id=$1`+suffix, principal.actor).Scan(&disabled); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return failure(401, "AUTHENTICATION_REQUIRED")
		}
		return unavailable()
	}
	now := time.Now()
	if disabled || revoked || actor != principal.actor || binding != principal.binding || generation != principal.generation || identity != principal.generation || !bindingExpires.After(now) || !expires.After(now) || !lastActive.After(now.Add(-30*time.Minute)) {
		return failure(401, "AUTHENTICATION_REQUIRED")
	}
	return nil
}

func (s *Service) RecheckProjectPrincipal(ctx context.Context, principal ProjectPrincipal) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return unavailable()
	}
	defer tx.Rollback(ctx)
	// 非锁定校验：这里的事务在校验后立刻提交，行锁随之释放，它本来就保护不到
	// 调用方后续的写入（那些写入在另一个事务里，并且自己会调
	// LockProjectPrincipal）。所以对读、对写都只需要事实校验，不需要行锁。
	if err := s.CheckProjectPrincipal(ctx, tx, principal); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return unavailable()
	}
	return nil
}

func (s *Service) ResolveSelectedRepositories(ctx context.Context, principal ProjectPrincipal, ids []string) ([]RepositoryLocator, error) {
	if err := s.RecheckProjectPrincipal(ctx, principal); err != nil {
		return nil, err
	}
	result := make([]RepositoryLocator, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if seen[id] || !strings.HasPrefix(id, "repo_") || len(id) != 25 {
			return nil, failure(404, "RESOURCE_NOT_FOUND")
		}
		external, err := strconv.ParseInt(strings.TrimPrefix(id, "repo_"), 10, 64)
		if err != nil || external <= 0 || id != fmt.Sprintf("repo_%020d", external) {
			return nil, failure(404, "RESOURCE_NOT_FOUND")
		}
		seen[id] = true
		var locator RepositoryLocator
		locator.ID, locator.Host, locator.ExternalID = id, "github.com", external
		err = s.pool.QueryRow(ctx, `SELECT r.owner,r.name FROM repomesh_access.discovered_repositories r
			JOIN repomesh_access.discovery_batches b ON b.id=r.batch
			JOIN repomesh_access.connections c ON c.actor=b.actor
			WHERE b.actor=$1 AND r.github_id=$2 AND b.connection_revision=c.revision AND b.access_epoch=c.access_epoch
			ORDER BY r.observed_at DESC LIMIT 1`, principal.actor, external).Scan(&locator.Owner, &locator.Name)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, failure(404, "RESOURCE_NOT_FOUND")
		}
		if err != nil {
			return nil, unavailable()
		}
		result = append(result, locator)
	}
	return result, nil
}

// ObserveProjectRepositories 读 5 分钟 actor×repo 缓存后返回参与权。
// 只有「issue 创建条件列表」这一条产品路径该走它：几十个仓全量现探会把
// GitHub 请求预算打爆。创建/更新/项目仓库读这些要落 60 秒提交窗口的路径
// 必须走 ObserveProjectRepositoriesFresh。
func (s *Service) ObserveProjectRepositories(ctx context.Context, principal ProjectPrincipal, repositories []RepositoryLocator) (ProjectObservation, error) {
	return s.observeProjectRepositories(ctx, principal, repositories, true)
}

// ObserveProjectRepositoriesFresh 对每个仓都现探 GitHub，不读缓存。
// **写路径必须走它**：CheckProjectObservationFresh 要求观测落在 60 秒窗口内，命中一条
// 5 分钟的旧缓存会跳过现探，既会让注入 repositoryOverride 的测试挂住，也可能放进一条
// 已经过期的 allowed 记录。
// （读路径走上面的缓存版，配的是 CheckProjectObservationCached —— 窗口与缓存一致。）
func (s *Service) ObserveProjectRepositoriesFresh(ctx context.Context, principal ProjectPrincipal, repositories []RepositoryLocator) (ProjectObservation, error) {
	return s.observeProjectRepositories(ctx, principal, repositories, false)
}

func (s *Service) observeProjectRepositories(ctx context.Context, principal ProjectPrincipal, repositories []RepositoryLocator, useCache bool) (ProjectObservation, error) {
	result := ProjectObservation{actor: principal.actor, repositories: make([]RepositoryObservation, 0, len(repositories))}
	if len(repositories) == 0 {
		return result, nil
	}
	credential, err := s.credential(ctx, principal.actor, false)
	if err != nil {
		var denied *Failure
		if errors.As(err, &denied) && denied.Status == 503 {
			result.authorizationFailed = true
			for _, locator := range repositories {
				result.repositories = append(result.repositories, RepositoryObservation{Locator: locator, ParticipationStatus: "unknown", ParticipationReasons: []string{"AUTHORIZATION_UNCONFIRMED"}})
			}
			return result, nil
		}
		return ProjectObservation{}, err
	}
	result.connectionRevision, result.accessEpoch, result.requiresConnection = credential.revision, credential.epoch, true
	// 2026-09-20:目录 49 仓时串行探测 300ms×N 打爆请求 15s 预算,options 整体
	// 503。先读缓存:TTL 内且已验的仓直接复用,只有过期/未验的仓才并发现验,
	// 现验结果尽力回填。热加载退化成纯 DB 读,冷加载约 2s。
	// useCache=false 时跳过读(全部进 stale),写回依然尽力——现探结果总是最新的。
	cached := map[string]RepositoryObservation{}
	if useCache {
		cached = s.loadParticipations(ctx, principal.actor)
	}
	observations := make([]RepositoryObservation, len(repositories))
	stale := make([]int, 0, len(repositories))
	// appOnly:参与权命中缓存、但 App 能力必须现探的槽位。
	//
	// App 能力是**部署级**事实(这个 GitHub App 覆盖没覆盖这个仓),不是 actor
	// 级的,所以它不能从 actor×repo 的缓存里重建 —— 重建会把"App 已失去该仓
	// 授权"盖成 allowed,界面上显示可选、建 issue 时才硬失败。主线能整体缓存是
	// 因为它把这张表判的改成了 actor 级的 UserWorkCapability(OAuth App 分支
	// 的改动),LBP 这条线走的是 App 级判定,所以只缓存参与权、App 每次现探。
	appOnly := make([]int, 0, len(repositories))
	for index, locator := range repositories {
		if entry, ok := cached[locator.ID]; ok && entry.ObservedAt != nil && time.Since(*entry.ObservedAt) < participationCacheTTL {
			observations[index] = entry
			if entry.ParticipationStatus == "allowed" {
				appOnly = append(appOnly, index)
			}
			continue
		}
		stale = append(stale, index)
	}
	if len(stale) > 0 {
		// 有界并发:每个 goroutine 只写自己的槽位(observations[index]),
		// 下标写回是为了保持输入顺序 —— options 的分页游标假设按仓 id 有序。
		sem := make(chan struct{}, participationProbeConcurrency)
		var wg sync.WaitGroup
		for _, index := range stale {
			locator := repositories[index]
			wg.Add(1)
			go func(index int, locator RepositoryLocator) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				observations[index] = s.probeRepository(ctx, principal, credential, locator)
			}(index, locator)
		}
		wg.Wait()
		fresh := make([]RepositoryObservation, 0, len(stale))
		for _, index := range stale {
			fresh = append(fresh, observations[index])
		}
		s.storeParticipations(ctx, principal.actor, fresh)
	}
	if len(appOnly) > 0 {
		// 同一档并发,只探 App 安装覆盖,省掉参与权那次 /repos 调用。
		sem := make(chan struct{}, participationProbeConcurrency)
		var wg sync.WaitGroup
		for _, index := range appOnly {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				entry := observations[index]
				app, appErr := s.provider.AppCapability(ctx, entry.Locator.Owner, entry.Locator.Name)
				if appErr != nil {
					app = github.Capability{Status: "unknown", ReasonCodes: []string{"APP_AUTHORIZATION_UNCONFIRMED"}}
				}
				item := RepositoryItem{ID: entry.Locator.ID, DisplayName: entry.Locator.Owner + "/" + entry.Locator.Name,
					UserParticipation: github.Capability{Status: "allowed", ReasonCodes: []string{}, ObservedAt: entry.ObservedAt},
					AppCapability:     app}
				entry.Item = &item
				observations[index] = entry
			}(index)
		}
		wg.Wait()
	}
	result.repositories = append(result.repositories, observations...)
	return result, nil
}

// probeRepository 跑单个仓的参与权探测(原串行循环体,原样抽出来给并发路径用)。
// 它只返回观测、不返回 error:某个仓探测失败(drift / 瞬时错误)只让那个仓
// 变成不可选,不会让整份 options 失败。凭据级失败由 credential() 在进入探测
// 之前拦下。
func (s *Service) probeRepository(ctx context.Context, principal ProjectPrincipal, credential credential, locator RepositoryLocator) RepositoryObservation {
	observation := RepositoryObservation{Locator: locator, ParticipationStatus: "unknown", ParticipationReasons: []string{"AUTHORIZATION_UNCONFIRMED"}}
	repository, repositoryErr := s.provider.Repository(ctx, credential.token, locator.Owner, locator.Name)
	observed := time.Now().UTC()
	if repositoryErr != nil {
		log.Printf("access: participation probe failed actor=%s repo=%s/%s err=%v", principal.ActorID(), locator.Owner, locator.Name, repositoryErr)
		var providerErr *github.Error
		if errors.As(repositoryErr, &providerErr) && providerErr.Kind == "denied" {
			observation.ParticipationStatus = "denied"
			observation.ParticipationReasons = []string{"REPOSITORY_ACCESS_DENIED"}
			observation.ObservedAt = &observed
		} else if errors.As(repositoryErr, &providerErr) && providerErr.Kind == "unauthorized" {
			s.rejectCredential(ctx, credential)
		}
		// 其余错误(含 unauthorized)保持 unknown 且不带 observed_at:
		// 未验结果不进缓存,下次自然重探。
		return observation
	}
	if repository.ID != locator.ExternalID {
		log.Printf("access: external id mismatch repo=%s/%s stored=%d github=%d", locator.Owner, locator.Name, locator.ExternalID, repository.ID)
		// 目录漂移:这个仓无法确认参与权,如实标未验(ObservedAt 为 nil),
		// 既不进缓存也不冒充 denied,下次加载自然重探。
		return observation
	}
	locator.Owner, locator.Name = repository.Owner, repository.Name
	observation.Locator = locator
	observation.ParticipationStatus = "allowed"
	observation.ParticipationReasons = []string{}
	observation.ObservedAt = &observed
	app, appErr := s.provider.AppCapability(ctx, repository.Owner, repository.Name)
	if appErr != nil {
		app = github.Capability{Status: "unknown", ReasonCodes: []string{"APP_AUTHORIZATION_UNCONFIRMED"}}
	}
	item := RepositoryItem{ID: locator.ID, DisplayName: repository.FullName,
		UserParticipation: github.Capability{Status: "allowed", ReasonCodes: []string{}, ObservedAt: &observed}, AppCapability: app}
	observation.Item = &item
	return observation
}

// CheckProjectObservationFresh 校验一份**现探**出来的观测：窗口是 participationFreshness
// （60 秒）。创建/更新/项目仓库读这些要落 60 秒提交窗口的路径走它。
func (s *Service) CheckProjectObservationFresh(ctx context.Context, tx pgx.Tx, principal ProjectPrincipal, observation ProjectObservation) error {
	return s.checkProjectObservation(ctx, tx, principal, observation, participationFreshness)
}

// CheckProjectObservationCached 校验一份**从缓存复用**出来的观测：窗口是
// participationCacheTTL —— 也就是缓存自己承诺的复用窗口。
//
// 只有「issue 创建条件列表」这一条读路径走它（见 ObserveProjectRepositories 的注释）。
// **不能拿更严的窗口去校缓存**：缓存允许"5 分钟内观测过"的仓直接复用，用 60 秒去校，
// 落在 1~5 分钟那段里的每一个仓都会把整份列表判死 —— 2026-09-20 线上实测的
// issue-creation-options 503 就是这个。提交路径另有一份自己的 60 秒现验
// （CheckIssueObservationTime），不依赖这里。
func (s *Service) CheckProjectObservationCached(ctx context.Context, tx pgx.Tx, principal ProjectPrincipal, observation ProjectObservation) error {
	return s.checkProjectObservation(ctx, tx, principal, observation, participationCacheTTL)
}

func (s *Service) checkProjectObservation(ctx context.Context, tx pgx.Tx, principal ProjectPrincipal, observation ProjectObservation, maxAge time.Duration) error {
	// 这一档返回的全是裸 503，界面上看不出任何差别，所以每条拒绝都留一行日志。
	// 2026-09-20 查一个同款 503 花了好几轮，就是因为这里一声不响。
	if observation.actor != principal.actor {
		log.Printf("access: observation actor mismatch observation=%s principal=%s", observation.actor, principal.actor)
		return failure(503, "AUTHORIZATION_UNCONFIRMED")
	}
	if observation.authorizationFailed {
		log.Printf("access: observation authorization failed actor=%s", principal.actor)
		return failure(503, "AUTHORIZATION_UNCONFIRMED")
	}
	if !observation.requiresConnection {
		return nil
	}
	var current bool
	var transactionNow time.Time
	if err := tx.QueryRow(ctx, `SELECT revision=$2 AND access_epoch=$3 AND status='connected' AND refresh_state='idle', transaction_timestamp()
		FROM repomesh_access.connections WHERE actor=$1 FOR UPDATE`, principal.actor, observation.connectionRevision, observation.accessEpoch).Scan(&current, &transactionNow); err != nil {
		log.Printf("access: observation connection check failed actor=%s: %v", principal.actor, err)
		return failure(503, "AUTHORIZATION_UNCONFIRMED")
	}
	if !current {
		log.Printf("access: observation credential moved on actor=%s revision=%s epoch=%d", principal.actor, observation.connectionRevision, observation.accessEpoch)
		return failure(503, "AUTHORIZATION_UNCONFIRMED")
	}
	oldestAllowed := transactionNow.Add(-maxAge)
	for _, repository := range observation.repositories {
		if repository.ParticipationStatus != "allowed" {
			continue
		}
		observed := "nil"
		if repository.ObservedAt != nil {
			observed = repository.ObservedAt.Format(time.RFC3339)
		}
		if repository.ObservedAt == nil || repository.ObservedAt.Before(oldestAllowed) || repository.ObservedAt.After(transactionNow) {
			log.Printf("access: observation out of window actor=%s repo=%s/%s observed=%s window=%s now=%s",
				principal.actor, repository.Locator.Owner, repository.Locator.Name, observed, maxAge, transactionNow.Format(time.RFC3339))
			return failure(503, "AUTHORIZATION_UNCONFIRMED")
		}
	}
	return nil
}

func (s *Service) InspectProjectSecret(ctx context.Context, tx pgx.Tx, id secrets.VersionID, owner secrets.Owner, purpose secrets.Purpose) (secrets.VersionInspection, error) {
	return s.secrets.InspectVersion(ctx, tx, id, owner, purpose)
}

// loadParticipations 读一个 actor 的参与权缓存(缺行 = 未命中)。
// 任何读取失败都当未命中返回:缓存只是省一次探测,读不到就现探,
// 绝不让缓存层把请求搞失败。
func (s *Service) loadParticipations(ctx context.Context, actor string) map[string]RepositoryObservation {
	out := map[string]RepositoryObservation{}
	rows, err := s.pool.Query(ctx, `SELECT repository_id,status,external_id,display_name,observed_at,host,owner,name
		FROM repomesh_access.participation_observations WHERE actor=$1`, actor)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var repoID, status, displayName, host, owner, name string
		var externalID *int64
		var observedAt time.Time
		if rows.Scan(&repoID, &status, &externalID, &displayName, &observedAt, &host, &owner, &name) != nil {
			continue
		}
		if host == "" || owner == "" || name == "" {
			continue // 定位不全的缓存行无法重建观测,当未命中现探
		}
		locator := RepositoryLocator{ID: repoID, Host: host, Owner: owner, Name: name}
		if externalID != nil {
			locator.ExternalID = *externalID
		}
		item := &RepositoryItem{ID: repoID, DisplayName: displayName}
		if status == "allowed" {
			// 只重建 actor 级的参与权结论。App 能力(部署级)不在这里重建 ——
			// 调用方拿到这条缓存后发现 Item.AppCapability 是空的,会现探补上
			// (见 observeProjectRepositories 的 appOnly 一趟)。
			item.UserParticipation = github.Capability{Status: "allowed", ReasonCodes: []string{}, ObservedAt: &observedAt}
		}
		out[repoID] = RepositoryObservation{
			Locator:              locator,
			ParticipationStatus:  status,
			ParticipationReasons: []string{},
			ObservedAt:           &observedAt,
			Item:                 item,
		}
	}
	return out
}

// storeParticipations 回填这次现探的结果,尽力而为:写失败只代价一次重探,
// 绝不影响正确性(本次结果已经返回给调用方了)。
func (s *Service) storeParticipations(ctx context.Context, actor string, observations []RepositoryObservation) {
	for _, o := range observations {
		if o.Locator.ID == "" || o.ObservedAt == nil {
			continue
		}
		// 存的是 actor 级的参与权结论,不含 App 能力 —— App 能力每次现探,
		// 所以这里不需要"App 也 allowed"才会缓存。
		displayName := ""
		if o.Item != nil {
			displayName = o.Item.DisplayName
		}
		_, _ = s.pool.Exec(ctx, `INSERT INTO repomesh_access.participation_observations
			(actor, repository_id, status, external_id, display_name, observed_at, host, owner, name)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
			ON CONFLICT (actor, repository_id) DO UPDATE SET
			  status=EXCLUDED.status, external_id=EXCLUDED.external_id,
			  display_name=EXCLUDED.display_name, observed_at=EXCLUDED.observed_at,
			  host=EXCLUDED.host, owner=EXCLUDED.owner, name=EXCLUDED.name`,
			actor, o.Locator.ID, o.ParticipationStatus, o.Locator.ExternalID, displayName, o.ObservedAt,
			o.Locator.Host, o.Locator.Owner, o.Locator.Name)
	}
}
