package repositoryteams

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"repomesh.local/repomesh/internal/agentteams"
)

const (
	minWorkers = 1
	maxWorkers = 20
	// maxTeamsPerEnsure 是**一次调用**最多建几支队伍。
	//
	// 建队会向 AgentTeams 控制面真的申请 runtime（每个 worker 是一个真实实例），
	// 所以不能"一次接入 43 个仓就一口气建 43 支队"。每次最多 5 支，剩下的留给下一次
	// （接入钩子之后还有 2 分钟一次的兜底收敛）—— 语义仍然是"接入即建队"，只是排队
	// 建，不给控制面制造尖峰。
	maxTeamsPerEnsure = 5
	// maxTeamsPerSweep 是兜底扫掠**一次**的上限：比接入钩子更保守。
	//
	// 接入钩子是用户刚刚做的动作（一个项目通常 1-3 个仓，立刻建完最好）；扫掠面对的是
	// 历史积压（线上实测 55 个已接入仓库），一次只建 1 支，慢慢收敛 —— 2026-09-20
	// 实测一次建 5 支就把这台机器压到 load 100+。
	maxTeamsPerSweep = 1
)

var (
	ErrNotFound               = errors.New("repository team not found")
	ErrForbidden              = errors.New("repository team action forbidden")
	ErrConflict               = errors.New("repository team conflict")
	ErrBusyWorkers            = errors.New("repository team has busy workers")
	ErrControllerUnavailable  = errors.New("agentteams controller unavailable")
	ErrReconciliationRequired = errors.New("repository team reconciliation required")
)

// ConflictError returns the locked roster to a caller that attempted a stale
// change. The caller must choose whether to reapply its change to that roster.
type ConflictError struct {
	Current Snapshot
}

func (e *ConflictError) Error() string {
	return "repository team roster revision conflict"
}

func (e *ConflictError) Unwrap() error {
	return ErrConflict
}

// Service owns the long-lived AgentTeams Team for each repository.
type Service struct {
	pool   *pgxpool.Pool
	client *agentteams.Client
}

// New creates a repository Team lifecycle service.
func New(pool *pgxpool.Pool, client *agentteams.Client) *Service {
	return &Service{pool: pool, client: client}
}

// Get returns the stored roster, live controller phases, and active task
// counts. It does not create or mutate any controller resource.
// Get 读一支队的快照：**本地编制照常返回，运行阶段读不到就如实留空**。
//
// 2026-09-21 用户实测：仓库团队页报「团队读取失败」+ HTTP 503，而本地那支队是真实
// 存在的（repository_teams 有行）。根因是这里对 leader 与**每个** worker 各打一次
// 控制面，**错一个就把整页判成 503** —— 于是"某个 worker 在控制面上不存在 / 控制面
// 一时读不到"这种**观察失败**，把"这支队的编制是什么"这个**持久化事实**一起毁掉了。
//
// 观察失败不该销毁事实。所以：编制照常返回，读不到的那几个成员 RuntimePhase 留空
// （前端已有"未接入 / 阶段未回报"的呈现），并把"有几个成员没读到"写进 RuntimeNote，
// 让界面能如实说清这次观察的完整性 —— 而不是假装都读到了，也不是整页打不开。
//
// 注意**不要**在这里吞掉控制面故障本身：控制面不可用时建队/改编制照样会失败，
// 这条只影响**读**的降级。
func (s *Service) Get(ctx context.Context, projectID, repositoryID string) (Snapshot, error) {
	if s.pool == nil {
		return Snapshot{}, fmt.Errorf("%w: database pool is nil", ErrControllerUnavailable)
	}
	snapshot, err := s.loadSnapshot(ctx, s.pool, projectID, repositoryID)
	if err != nil {
		return Snapshot{}, err
	}
	unread := 0
	if phase, phaseErr := s.workerPhase(ctx, snapshot.Leader.ResourceName); phaseErr != nil {
		unread++
	} else {
		snapshot.Leader.RuntimePhase = phase
	}
	for i := range snapshot.Workers {
		if phase, phaseErr := s.workerPhase(ctx, snapshot.Workers[i].ResourceName); phaseErr != nil {
			unread++
		} else {
			snapshot.Workers[i].RuntimePhase = phase
		}
	}
	if unread > 0 {
		snapshot.RuntimeNote = fmt.Sprintf(
			"%d 个成员的运行阶段读不到（控制面未响应，或控制面上没有这个资源）——"+
				"编制是本地持久化事实，不受影响。", unread)
	}
	return snapshot, nil
}

// Create creates one remote Team with exactly one Leader and the requested
// number of active Workers. The repository ID, not its display name, owns all
// local and remote associations.
//
// 2026-09-20（0057）：团队按 **(项目, 仓库)** 归属。同一份仓库挂到两个项目时，
// 各建一支自己的队；此前的键只有仓库，第二个项目会被判定"已有队"而**静默跳过**。
func (s *Service) Create(ctx context.Context, projectID, repositoryID string, workerCount int) (Snapshot, error) {
	tx, err := s.beginLocked(ctx, projectID, repositoryID)
	if err != nil {
		return Snapshot{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if !validWorkerCount(workerCount) {
		return Snapshot{}, fmt.Errorf("%w: worker count must be between %d and %d", ErrConflict, minWorkers, maxWorkers)
	}

	var repositoryName string
	if err := tx.QueryRow(ctx,
		"SELECT name FROM repomesh_scan.repositories WHERE id = $1", repositoryID,
	).Scan(&repositoryName); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Snapshot{}, fmt.Errorf("%w: repository %q", ErrNotFound, repositoryID)
		}
		return Snapshot{}, fmt.Errorf("load repository: %w", err)
	}
	var exists bool
	if err := tx.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM public.repository_teams WHERE project_id = $1 AND repository_id = $2)",
		projectID, repositoryID,
	).Scan(&exists); err != nil {
		return Snapshot{}, fmt.Errorf("check repository team: %w", err)
	}
	if exists {
		return Snapshot{}, fmt.Errorf("%w: repository team already exists", ErrConflict)
	}

	prefix := remotePrefix(projectID, repositoryID)
	leaderID, err := randomUUID()
	if err != nil {
		return Snapshot{}, fmt.Errorf("create Leader identity: %w", err)
	}
	leaderName := prefix + "-leader"
	workers := make([]workerRecord, workerCount)
	for i := range workers {
		workerID, err := randomUUID()
		if err != nil {
			return Snapshot{}, fmt.Errorf("create Worker identity: %w", err)
		}
		sequence := i + 1
		workers[i] = workerRecord{
			id:               workerID,
			resourceName:     fmt.Sprintf("%s-w-%04d", prefix, sequence),
			creationSequence: sequence,
			displayOrder:     sequence,
		}
	}

	createdResources := make([]string, 0, workerCount+1)
	createdResources = append(createdResources, leaderName)
	if err := s.createRemoteWorker(ctx, leaderName); err != nil {
		return Snapshot{}, s.compensateWorkers(ctx, cleanupResourcesAfterWorkerCreate(createdResources, err), err)
	}
	for _, worker := range workers {
		createdResources = append(createdResources, worker.resourceName)
		if err := s.createRemoteWorker(ctx, worker.resourceName); err != nil {
			return Snapshot{}, s.compensateWorkers(ctx, cleanupResourcesAfterWorkerCreate(createdResources, err), err)
		}
	}
	if err := s.createRemoteTeam(ctx, prefix, teamMembers(leaderName, nil, workers)); err != nil {
		if errors.Is(err, ErrReconciliationRequired) {
			return Snapshot{}, err
		}
		return Snapshot{}, s.compensateWorkers(ctx, createdResources, err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO public.repository_teams
		(project_id, repository_id, agentteams_team_name, leader_id, leader_resource_name)
		VALUES ($1, $2, $3, $4::uuid, $5)`, projectID, repositoryID, prefix, leaderID, leaderName); err != nil {
		return Snapshot{}, reconciliationError(fmt.Errorf("persist repository team: %w", err))
	}
	for _, worker := range workers {
		if _, err := tx.Exec(ctx, `
			INSERT INTO public.repository_team_workers
			(project_id, id, repository_id, resource_name, creation_sequence, display_order)
			VALUES ($1, $2::uuid, $3, $4, $5, $6)`,
			projectID, worker.id, repositoryID, worker.resourceName, worker.creationSequence, worker.displayOrder,
		); err != nil {
			return Snapshot{}, reconciliationError(fmt.Errorf("persist repository Worker: %w", err))
		}
	}
	if err := s.persistRooms(ctx, tx, projectID, repositoryID, prefix); err != nil {
		return Snapshot{}, reconciliationError(err)
	}

	snapshot, err := s.loadSnapshot(ctx, tx, projectID, repositoryID)
	if err != nil {
		return Snapshot{}, reconciliationError(fmt.Errorf("load created roster: %w", err))
	}
	if err := tx.Commit(ctx); err != nil {
		return Snapshot{}, reconciliationError(fmt.Errorf("commit created roster: %w", err))
	}
	return snapshot, nil
}

// EnsureForRepository 给**一个**仓库建队（幂等）。
//
// 这是"确认接入即建队"的唯一入口（2026-09-20 用户裁定）：
//
//	不是扫描就建队，是**确认接入**的时候才建队。
//
// 仓库页单仓「接入本项目」= 人在确认；扫描目录、批量「全部接入」都**不是**确认，
// 不该建队。所以这里只接受"调用方明确点名的那一个仓库"，并且要先把项目侧 id
// （repo_…）解析成扫描侧 id（32 位十六进制）—— 建队只认后者。
//
// 返回 true 表示这次真的建了一支；**本项目已经有队**、仓库没扫到、或远端拒绝都返回
// false（远端拒绝会把原因带在 error 里，不吞）。
//
// 2026-09-20（0057）：幂等判定从"这个仓库有没有队"改成"**这个项目的**这个仓库有没有队"。
// 否则同一仓库挂到第二个项目时，这里判定"已有队"直接跳过，第二个项目**永远建不出队**
// 而且不报错 —— 这就是"同仓两项目会串"的真实形态。
func (s *Service) EnsureForRepository(ctx context.Context, projectID, projectRepositoryID string, workerCount int) (bool, error) {
	if s.pool == nil {
		return false, fmt.Errorf("%w: database pool is nil", ErrControllerUnavailable)
	}
	if !validWorkerCount(workerCount) {
		workerCount = minWorkers
	}
	var scanID, projectIDOut, projectSide string
	if err := s.pool.QueryRow(ctx, RepoTeamResolutionQuery+`
		WHERE pr.project_id=$1 AND pr.repository_id=$2
		LIMIT 1`, projectID, projectRepositoryID).Scan(&projectIDOut, &projectSide, &scanID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// 这个仓库还没被扫描到：没有扫描侧身份就建不了队，如实返回。
			return false, nil
		}
		return false, fmt.Errorf("resolve repository %s: %w", projectRepositoryID, err)
	}
	if scanID == "" {
		return false, nil
	}
	var exists bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM public.repository_teams WHERE project_id=$1 AND repository_id=$2)`,
		projectID, scanID).Scan(&exists); err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}
	if _, err := s.Create(ctx, projectID, scanID, workerCount); err != nil {
		if errors.Is(err, ErrConflict) || errors.Is(err, ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// repoTeamResolutionQuery 把**项目侧**仓库解析成**扫描侧**仓库（按 URL 对齐）。
//
// 两个 id 空间不同（项目侧 "repo_0000…"、扫描侧 32 位十六进制），而建队只认扫描侧
// id（Create 用它查仓库名、repository_teams.repository_id 存的也是它）。规则与
// internal/discovery/recall.go 的 loadRepoPool 完全一致：同一组织下，把扫描 URL 去掉
// 结尾斜杠与 .git 后，与 https/http/ssh 三种写法之一相等。
// RepoTeamResolutionQuery 把项目侧仓库 id(repo_…)解析成扫描侧 id 的公共片段:
// 两个 id 空间不同,凡要 join repository_teams 的读面都得先过这一步(0057 起
// 该表键为 (project_id, 扫描侧 repository_id))。导出给 issues/roomnotice 复用,
// 单一来源,别处不许自己再写一份。
const RepoTeamResolutionQuery = `
	SELECT pr.project_id AS project_id, pr.repository_id AS project_side, COALESCE(s.id, '') AS scan_side
	FROM repomesh_projects.project_repositories pr
	JOIN repomesh_projects.repositories r ON r.id = pr.repository_id
	JOIN repomesh_projects.projects p ON p.id = pr.project_id
	-- 2026-09-21：**已归档项目不参与**。归档即退出在役，而这条片段此前没有这个过滤 ——
	-- 线上实测：把测试项目归档之后，扫掠照样会给它名下 48 个仓库建队，白建 48 支队 +
	-- 48 个 worker 资源。归档的语义对**所有**读面都成立，所以过滤加在这一处（唯一来源）。
	AND p.removed_at IS NULL
	JOIN repomesh_access.accounts a ON a.id = p.owner
	LEFT JOIN LATERAL (
	  SELECT scan.id FROM repomesh_scan.repositories scan
	  WHERE scan.organization_id = a.organization_id
	    AND lower(regexp_replace(rtrim(scan.url, '/'), '\.git$', '')) IN
	      (lower('https://' || r.host || '/' || r.owner || '/' || r.name),
	       lower('http://' || r.host || '/' || r.owner || '/' || r.name),
	       lower('git@' || r.host || ':' || r.owner || '/' || r.name))
	  ORDER BY scan.profiled_at DESC, scan.id LIMIT 1
	) s ON true`

// ensureInterval 是接入建队的**逐仓间隔**(选仓门 §3.3:串行 + 每仓间隔 2s)。
//
// 2026-09-20 事故教训仍然有效:一次接入几十个仓会把 embedded AgentTeams 灌爆
// (42 队 / 124 worker、load 100+)。选仓门把建队时机改回"接入即建(单仓/批量
// 都建)",但靠两件事不再重演:worker 一律 **Sleeping**(不起真 runtime) +
// 这里逐仓串行限速。包级变量是为了测试能调小 —— 2s 是给线上控制面的喘息,
// 不是语义本身。
var ensureInterval = 2 * time.Second

// EnsureOutcome 是接入钩子里**单个仓库**的建队结果。
//
// 错误放在结果里而不是打断整批:调用方(接入钩子)按它逐仓记 WARN ——
// 一个仓的 AgentTeams 配额/过载问题不该让整批接入失败。
type EnsureOutcome struct {
	// RepositoryID 是调用方传入的**项目侧**仓库 id(原样带回,便于对日志)。
	RepositoryID string
	// Created 表示这次真的建出了一支队(已有队/没扫描到 = false 且无错)。
	Created bool
	// Err 是这个仓建队失败的如实原因(没有队可建不算失败)。
	Err error
}

// EnsureForRepositories 是接入钩子的批量入口(选仓门 C2):对 added 的每个仓库
// 逐仓建队,单仓与批量接入共用这一条路。
//
// 语义:
//   - 解析沿用 RepoTeamResolutionQuery(0057 的 (project_id, 扫描侧 id) 口径),
//     不自己另写一份;
//   - 逐仓**串行**,每仓之间隔 ensureInterval(默认 2s),不给控制面制造尖峰;
//   - 单仓失败记在该仓的 Outcome.Err 里,**不阻断其余仓库**;
//   - 建出的 worker 一律 Sleeping(createRemoteWorker),选进 issue 范围时再唤醒。
//
// context 被取消时,未处理的仓库也各带一条 Err 返回(交给收敛兜底),不装作建过。
func (s *Service) EnsureForRepositories(ctx context.Context, projectID string, projectRepositoryIDs []string, workerCount int) []EnsureOutcome {
	outcomes := make([]EnsureOutcome, 0, len(projectRepositoryIDs))
	for index, repositoryID := range projectRepositoryIDs {
		if index > 0 {
			select {
			case <-ctx.Done():
				for _, remaining := range projectRepositoryIDs[index:] {
					outcomes = append(outcomes, EnsureOutcome{RepositoryID: remaining, Err: ctx.Err()})
				}
				return outcomes
			case <-time.After(ensureInterval):
			}
		}
		created, err := s.EnsureForRepository(ctx, projectID, repositoryID, workerCount)
		outcomes = append(outcomes, EnsureOutcome{RepositoryID: repositoryID, Created: created, Err: err})
	}
	return outcomes
}

// WakeTeamsForRepositories 唤醒一组仓库的整支队(选仓门的"选用时唤醒")。
//
// fire-and-forget:内部起 goroutine,**绝不占调用方时间**(确认端点 200 不能
// 被唤醒拖住);失败只 slog.WARN,不向调用方报错 —— 唤醒是后续动作,不是
// 范围确认的一部分,失败靠 coordinator 派活前的双保险兜住(spec §3.3)。
func (s *Service) WakeTeamsForRepositories(ctx context.Context, projectID string, projectRepositoryIDs ...string) {
	if s.pool == nil || s.client == nil || len(projectRepositoryIDs) == 0 {
		return
	}
	wakeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
	go func() {
		defer cancel()
		for _, repositoryID := range projectRepositoryIDs {
			if err := s.wakeTeam(wakeCtx, projectID, repositoryID); err != nil {
				slog.Warn("repository team wake deferred",
					"project", projectID, "repository", repositoryID, "reason", err.Error())
			}
		}
	}()
}

// wakeTeam 唤醒一个仓库的队(同步实现,便于确定性测试):项目侧 id 解析成
// 扫描侧(RepoTeamResolutionQuery),再对 leader 与全部在编 worker 逐个
// EnsureReadyOrWake —— 先 ensure-ready,没生效回退 wake(上游语义)。
//
// 没扫描到、没建过队都不是错误(没有可唤醒的东西);一个成员唤醒失败不挡
// 其余成员,错误聚合返回。
func (s *Service) wakeTeam(ctx context.Context, projectID, projectRepositoryID string) error {
	if s.pool == nil || s.client == nil {
		return fmt.Errorf("%w: database pool or controller client is nil", ErrControllerUnavailable)
	}
	var scanID string
	if err := s.pool.QueryRow(ctx, RepoTeamResolutionQuery+`
		WHERE pr.project_id=$1 AND pr.repository_id=$2
		LIMIT 1`, projectID, projectRepositoryID).Scan(new(string), new(string), &scanID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("resolve repository %s: %w", projectRepositoryID, err)
	}
	if scanID == "" {
		return nil
	}
	snapshot, err := s.loadSnapshot(ctx, s.pool, projectID, scanID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	members := make([]string, 0, 1+len(snapshot.Workers))
	members = append(members, snapshot.Leader.ResourceName)
	for _, worker := range snapshot.Workers {
		members = append(members, worker.ResourceName)
	}
	var failures []error
	for _, member := range members {
		if _, err := s.client.EnsureReadyOrWake(ctx, member); err != nil {
			failures = append(failures, fmt.Errorf("worker %s: %w", member, err))
		}
	}
	return errors.Join(failures...)
}


// EnsureForProject 保证"本项目里每个已接入的仓库都有一支自己的团队"。
//
// 2026-09-20（用户）：仓库**接入的时候**就该自动建队，不该让人再去仓库页点一次。
// 幂等：已经有团队的仓库直接跳过；Create 自己会撞 ErrConflict，所以并发/重试也安全。
//
// ⚠️ 当前**没有调用方**（20c63ba1 把"批量接入/扫描/新建项目也建队"和后台扫掠都撤了，
// 只留逐仓确认接入 → EnsureForRepository）。保留它是为了收敛逻辑，别误以为它还在跑。
//
// 返回真正新建了团队的仓库 id 列表。单个仓库建失败不吞掉原因 —— 记在返回的错误里，
// 但**不阻断其它仓库**（一个仓的 AT 配额问题不该让整批接入失败）。
func (s *Service) EnsureForProject(ctx context.Context, projectID string, workerCount int) ([]string, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("%w: database pool is nil", ErrControllerUnavailable)
	}
	if !validWorkerCount(workerCount) {
		workerCount = minWorkers
	}
	// 注意两个 id 空间（2026-09-20 线上实测，第一次接错就是死在这里）：
	//   · 项目侧：repomesh_projects.project_repositories.repository_id = "repo_00000000001329478693"
	//   · 扫描侧：repomesh_scan.repositories.id                    = "38d0b82ca1d61fdc04d83cef1d2245a7"
	// 而 repository_teams.repository_id 用的是**扫描侧** id（Create 也按它查仓库名）。
	// 所以这里必须先把项目侧仓库解析成扫描侧仓库（按 URL 对齐，与 discovery 的
	// loadRepoPool 同一条规则），不能拿 "repo_..." 直接去建队 —— 那样 Create 只会
	// 回 ErrNotFound，团队一支也建不出来。
	rows, err := s.pool.Query(ctx, RepoTeamResolutionQuery+`
		WHERE pr.project_id=$1
		  AND s.id IS NOT NULL
		  AND NOT EXISTS (SELECT 1 FROM public.repository_teams t
		                  WHERE t.project_id=$1 AND t.repository_id=s.id)
		ORDER BY s.id`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list repositories without team: %w", err)
	}
	ids := []string{}
	for rows.Next() {
		var projectIDOut, projectSide, scanSide string
		if err := rows.Scan(&projectIDOut, &projectSide, &scanSide); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan repository id: %w", err)
		}
		_ = projectSide
		if scanSide == "" {
			continue
		}
		ids = append(ids, scanSide)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	created := []string{}
	var firstErr error
	for _, id := range ids {
		if len(created) >= maxTeamsPerEnsure {
			break
		}
		if _, err := s.Create(ctx, projectID, id, workerCount); err != nil {
			if errors.Is(err, ErrConflict) || errors.Is(err, ErrNotFound) {
				// 并发下别人先建好了 / 仓库行已不在：都不是错误。
				continue
			}
			if firstErr == nil {
				firstErr = fmt.Errorf("repository %s: %w", id, err)
			}
			continue
		}
		created = append(created, id)
	}
	return created, firstErr
}

// BackfillRooms 给"已经有团队但还没记住房间号"的行补一次回读。返回补齐的条数。
//
// 为什么必须有这条：房间由 AgentTeams 控制器**异步**建，建队那一刻通常还没建好，
// 于是 Create 里的 persistRooms 读到空、room_id 留 NULL；而建队路径会跳过"已经有
// 团队"的仓库，那个 NULL 就再也没人来填。没有这条兜底，房间号恒空，"进房间看
// 对话"永远是间进不去的房。
//
// 由组合根的独立低频循环调用（不走自动建队那道开关）：它只 GET、不写远端，
// 不建任何东西，重演不了 2026-09-20 那场"灌爆 AgentTeams"的事故。
//
// 2026-09-20（0057）：团队改成按 **(项目, 仓库)** 归属，所以这一支也必须带上项目 ——
// 只按 repository_id 读写，同一仓库挂到两个项目时会漏掉另一个项目的行（SELECT 端
// 会把两支队的房间号互相覆盖到同一行上）。
func (s *Service) BackfillRooms(ctx context.Context) (int, error) {
	if s.pool == nil || s.client == nil {
		return 0, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT project_id::text, repository_id, agentteams_team_name
		FROM public.repository_teams
		WHERE COALESCE(team_room_id, '') = ''
		ORDER BY project_id, repository_id`)
	if err != nil {
		return 0, fmt.Errorf("list teams without rooms: %w", err)
	}
	type pendingTeam struct{ projectID, repositoryID, teamName string }
	pending := []pendingTeam{}
	for rows.Next() {
		var team pendingTeam
		if err := rows.Scan(&team.projectID, &team.repositoryID, &team.teamName); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan team without room: %w", err)
		}
		pending = append(pending, team)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	filled := 0
	for _, team := range pending {
		view, status, err := s.client.GetTeam(ctx, team.teamName)
		if err != nil || status != http.StatusOK {
			continue
		}
		if view.TeamRoomID == "" && view.LeaderDMRoomID == "" {
			// 房间还没建好。下一轮再来，不当作错误。
			continue
		}
		if _, err := s.pool.Exec(ctx, `
			UPDATE public.repository_teams
			SET team_room_id = COALESCE(NULLIF($3, ''), team_room_id),
			    leader_dm_room_id = COALESCE(NULLIF($4, ''), leader_dm_room_id),
			    updated_at = now()
			WHERE project_id = $1 AND repository_id = $2`,
			team.projectID, team.repositoryID, view.TeamRoomID, view.LeaderDMRoomID); err != nil {
			return filled, fmt.Errorf("persist backfilled rooms: %w", err)
		}
		filled++
	}
	return filled, nil
}

// EnsureAll 扫**所有**项目，把还没建队的仓库补上。服务重启后、或本次改动之前接入的
// 仓库，都靠它收敛（幂等，重复跑没有副作用）。
//
// ⚠️ 当前**没有调用方**：20c63ba1 已把后台扫掠撤掉（它正是把机器压到 load 100+ 的那条
// 路径之一）。保留但不接线，见 EnsureForProject 的说明。
func (s *Service) EnsureAll(ctx context.Context, workerCount int) ([]string, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("%w: database pool is nil", ErrControllerUnavailable)
	}
	// 与 EnsureForProject 同一条解析规则：只挑"真的还有可建队的仓库"的项目，
	// 否则每轮都会把没有扫描记录的仓库再算一遍（永远建不出来，纯空转）。
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT resolved.project_id
		FROM (`+RepoTeamResolutionQuery+`) resolved
		WHERE resolved.scan_side <> ''
		  AND NOT EXISTS (SELECT 1 FROM public.repository_teams t
		                  WHERE t.project_id=resolved.project_id
		                    AND t.repository_id=resolved.scan_side)`)
	if err != nil {
		return nil, fmt.Errorf("list projects with teamless repositories: %w", err)
	}
	projects := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		projects = append(projects, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	created := []string{}
	var firstErr error
	for _, projectID := range projects {
		if len(created) >= maxTeamsPerSweep {
			// 一次扫掠也有总量上限：收敛可以慢，但不能给控制面制造尖峰。
			break
		}
		ids, err := s.EnsureForProject(ctx, projectID, workerCount)
		created = append(created, ids...)
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return created, firstErr
}

// Change applies one explicitly versioned capacity change. It never retries a
// stale request: the caller receives the current roster and decides whether to
// submit another change.
func (s *Service) Change(ctx context.Context, projectID, repositoryID string, command ChangeCommand) (Snapshot, error) {
	tx, err := s.beginLocked(ctx, projectID, repositoryID)
	if err != nil {
		return Snapshot{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	current, err := s.loadSnapshot(ctx, tx, projectID, repositoryID)
	if err != nil {
		return Snapshot{}, err
	}
	// 队名一律以库里存的那一行为准（0057），**不要重算**：存量队的名字不含项目维度，
	// 重算会指向一个不存在的远端队 —— 等于把存量队变成孤儿，且远端资源再也没人管。
	teamName, err := loadTeamName(ctx, tx, projectID, repositoryID)
	if err != nil {
		return Snapshot{}, err
	}
	if command.RosterRevision != current.RosterRevision {
		return Snapshot{}, &ConflictError{Current: current}
	}
	if !validWorkerCount(command.WorkerCount) {
		return Snapshot{}, fmt.Errorf("%w: worker count must be between %d and %d", ErrConflict, minWorkers, maxWorkers)
	}

	delta := command.WorkerCount - len(current.Workers)
	switch {
	case delta == 0:
		return current, nil
	case delta > 0:
		return s.scaleUp(ctx, tx, projectID, repositoryID, teamName, current, delta)
	default:
		return s.scaleDown(ctx, tx, projectID, repositoryID, teamName, current, -delta)
	}
}

// loadTeamName 读出**远端队名的唯一真相**。
//
// 0057 起代码不再自行 remotePrefix 重算：存量队（约 42 支，2026-09-20 自动建队事故留下）
// 的名字不含项目维度，重算就会指错远端队。新队建的时候把名字写进这一列，之后一律读它。
func loadTeamName(ctx context.Context, db snapshotQuerier, projectID, repositoryID string) (string, error) {
	var name string
	err := db.QueryRow(ctx, `
		SELECT agentteams_team_name FROM public.repository_teams
		WHERE project_id = $1 AND repository_id = $2`, projectID, repositoryID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("%w: repository %q", ErrNotFound, repositoryID)
	}
	if err != nil {
		return "", fmt.Errorf("load team name: %w", err)
	}
	return name, nil
}

func (s *Service) scaleUp(ctx context.Context, tx pgx.Tx, projectID, repositoryID, teamName string, current Snapshot, delta int) (Snapshot, error) {
	var highestSequence int
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(creation_sequence), 0)
		FROM public.repository_team_workers
		WHERE project_id = $1 AND repository_id = $2`, projectID, repositoryID,
	).Scan(&highestSequence); err != nil {
		return Snapshot{}, fmt.Errorf("load Worker creation sequence: %w", err)
	}

	// 新 worker 的名字跟着**这支队已有的队名**走（0057）：存量队继续用旧命名，
	// 新队用含项目的命名。这样不会在同一支队里混出两套名字。
	prefix := teamName
	workers := make([]workerRecord, delta)
	for i := range workers {
		workerID, err := randomUUID()
		if err != nil {
			return Snapshot{}, fmt.Errorf("create Worker identity: %w", err)
		}
		sequence := highestSequence + i + 1
		workers[i] = workerRecord{
			id:               workerID,
			resourceName:     fmt.Sprintf("%s-w-%04d", prefix, sequence),
			creationSequence: sequence,
			displayOrder:     len(current.Workers) + i + 1,
		}
	}

	createdResources := make([]string, 0, delta)
	for _, worker := range workers {
		createdResources = append(createdResources, worker.resourceName)
		if err := s.createRemoteWorker(ctx, worker.resourceName); err != nil {
			return Snapshot{}, s.compensateWorkers(ctx, cleanupResourcesAfterWorkerCreate(createdResources, err), err)
		}
	}
	if err := s.updateRemoteTeam(ctx, teamName, teamMembers(current.Leader.ResourceName, current.Workers, workers)); err != nil {
		if errors.Is(err, ErrReconciliationRequired) {
			return Snapshot{}, err
		}
		return Snapshot{}, s.compensateWorkers(ctx, createdResources, err)
	}

	for _, worker := range workers {
		if _, err := tx.Exec(ctx, `
			INSERT INTO public.repository_team_workers
			(project_id, id, repository_id, resource_name, creation_sequence, display_order)
			VALUES ($1, $2::uuid, $3, $4, $5, $6)`,
			projectID, worker.id, repositoryID, worker.resourceName, worker.creationSequence, worker.displayOrder,
		); err != nil {
			return Snapshot{}, reconciliationError(fmt.Errorf("persist scaled Worker: %w", err))
		}
	}
	if err := advanceRevision(ctx, tx, projectID, repositoryID, current.RosterRevision); err != nil {
		return Snapshot{}, reconciliationError(err)
	}
	if err := s.persistRooms(ctx, tx, projectID, repositoryID, teamName); err != nil {
		return Snapshot{}, reconciliationError(err)
	}
	updated, err := s.loadSnapshot(ctx, tx, projectID, repositoryID)
	if err != nil {
		return Snapshot{}, reconciliationError(fmt.Errorf("load scaled roster: %w", err))
	}
	if err := tx.Commit(ctx); err != nil {
		return Snapshot{}, reconciliationError(fmt.Errorf("commit scaled roster: %w", err))
	}
	return updated, nil
}

func (s *Service) scaleDown(ctx context.Context, tx pgx.Tx, projectID, repositoryID, teamName string, current Snapshot, delta int) (Snapshot, error) {
	selected, err := selectedWorkers(ctx, tx, projectID, repositoryID, delta)
	if err != nil {
		return Snapshot{}, err
	}
	busyLabels := make([]string, 0, len(selected))
	for _, worker := range selected {
		activeTasks, err := activeTaskCount(ctx, tx, worker.id)
		if err != nil {
			return Snapshot{}, fmt.Errorf("count Worker tasks: %w", err)
		}
		if activeTasks > 0 {
			busyLabels = append(busyLabels, WorkerLabel(worker.displayOrder))
		}
	}
	if len(busyLabels) > 0 {
		return Snapshot{}, &BusyWorkersError{Labels: busyLabels}
	}
	for _, worker := range selected {
		phase, err := s.workerPhase(ctx, worker.resourceName)
		if err != nil {
			return Snapshot{}, err
		}
		if isExecutingPhase(phase) {
			busyLabels = append(busyLabels, WorkerLabel(worker.displayOrder))
		}
	}
	if len(busyLabels) > 0 {
		return Snapshot{}, &BusyWorkersError{Labels: busyLabels}
	}

	removed := make(map[string]struct{}, len(selected))
	for _, worker := range selected {
		removed[worker.id] = struct{}{}
	}
	remaining := make([]Member, 0, len(current.Workers)-len(selected))
	for _, worker := range current.Workers {
		if _, removing := removed[worker.ID]; !removing {
			remaining = append(remaining, worker)
		}
	}
	if err := s.updateRemoteTeam(ctx, teamName, teamMembers(current.Leader.ResourceName, remaining, nil)); err != nil {
		return Snapshot{}, err
	}
	for _, worker := range selected {
		if err := s.deleteRemoteWorker(ctx, worker.resourceName); err != nil {
			return Snapshot{}, reconciliationError(err)
		}
	}
	for _, worker := range selected {
		if _, err := tx.Exec(ctx, `
			UPDATE public.repository_team_workers
			SET status = 'disabled', display_order = NULL, disabled_at = now()
			WHERE id = $1::uuid AND project_id = $2 AND repository_id = $3 AND status = 'active'`,
			worker.id, projectID, repositoryID); err != nil {
			return Snapshot{}, reconciliationError(fmt.Errorf("disable Worker: %w", err))
		}
	}
	if err := advanceRevision(ctx, tx, projectID, repositoryID, current.RosterRevision); err != nil {
		return Snapshot{}, reconciliationError(err)
	}
	if err := s.persistRooms(ctx, tx, projectID, repositoryID, teamName); err != nil {
		return Snapshot{}, reconciliationError(err)
	}
	updated, err := s.loadSnapshot(ctx, tx, projectID, repositoryID)
	if err != nil {
		return Snapshot{}, reconciliationError(fmt.Errorf("load reduced roster: %w", err))
	}
	if err := tx.Commit(ctx); err != nil {
		return Snapshot{}, reconciliationError(fmt.Errorf("commit reduced roster: %w", err))
	}
	return updated, nil
}

func (s *Service) beginLocked(ctx context.Context, projectID, repositoryID string) (pgx.Tx, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("database pool is nil")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin repository team transaction: %w", err)
	}
	// 锁粒度也跟着改成 (项目, 仓库)（0057）：只按仓库加锁的话，
	// 两个项目对同一仓库的操作会**互相阻塞**，而它们本来互不相关。
	lockKey := projectID + "/" + repositoryID
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", lockKey); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("lock repository team: %w", err)
	}
	return tx, nil
}

type snapshotQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (s *Service) loadSnapshot(ctx context.Context, db snapshotQuerier, projectID, repositoryID string) (Snapshot, error) {
	var snapshot Snapshot
	var leaderID string
	err := db.QueryRow(ctx, `
		SELECT t.repository_id, r.name, t.roster_revision, t.runtime_status,
		       t.leader_id::text, t.leader_resource_name,
		       COALESCE(t.team_room_id, ''), COALESCE(t.leader_dm_room_id, '')
		FROM public.repository_teams t
		JOIN repomesh_scan.repositories r ON r.id = t.repository_id
		WHERE t.project_id = $1 AND t.repository_id = $2`, projectID, repositoryID,
	).Scan(
		&snapshot.RepositoryID,
		&snapshot.RepositoryName,
		&snapshot.RosterRevision,
		&snapshot.RuntimeStatus,
		&leaderID,
		&snapshot.Leader.ResourceName,
		&snapshot.TeamRoomID,
		&snapshot.LeaderDMRoomID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Snapshot{}, fmt.Errorf("%w: repository %q", ErrNotFound, repositoryID)
	}
	if err != nil {
		return Snapshot{}, fmt.Errorf("load repository team: %w", err)
	}
	snapshot.Leader.ID = leaderID
	snapshot.Leader.DisplayLabel = "Leader"

	rows, err := db.Query(ctx, `
		SELECT id::text, resource_name, display_order
		FROM public.repository_team_workers
		WHERE project_id = $1 AND repository_id = $2 AND status = 'active'
		ORDER BY display_order`, projectID, repositoryID,
	)
	if err != nil {
		return Snapshot{}, fmt.Errorf("load active repository Workers: %w", err)
	}
	snapshot.Workers, err = loadActiveWorkers(rows, func(workerID string) (int, error) {
		return activeTaskCount(ctx, db, workerID)
	})
	if err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

type activeWorkerRows interface {
	Close()
	Err() error
	Next() bool
	Scan(...any) error
}

func loadActiveWorkers(rows activeWorkerRows, taskCount func(string) (int, error)) ([]Member, error) {
	defer rows.Close()
	workers := []Member{}
	for rows.Next() {
		var worker Member
		var displayOrder int
		if err := rows.Scan(&worker.ID, &worker.ResourceName, &displayOrder); err != nil {
			return nil, fmt.Errorf("scan active repository Worker: %w", err)
		}
		worker.DisplayLabel = WorkerLabel(displayOrder)
		workers = append(workers, worker)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active repository Workers: %w", err)
	}
	for i := range workers {
		var err error
		workers[i].ActiveTaskCount, err = taskCount(workers[i].ID)
		if err != nil {
			return nil, fmt.Errorf("count active Worker tasks: %w", err)
		}
	}
	return workers, nil
}

func activeTaskCount(ctx context.Context, db interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, workerID string) (int, error) {
	var count int
	if err := db.QueryRow(ctx, `
		SELECT count(*)
		FROM public.tasks
		WHERE assignee_agent_id = $1::uuid
		  AND status IN ('assigned', 'running')`, workerID,
	).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

type workerRecord struct {
	id               string
	resourceName     string
	creationSequence int
	displayOrder     int
}

func selectedWorkers(ctx context.Context, tx pgx.Tx, projectID, repositoryID string, count int) ([]workerRecord, error) {
	rows, err := tx.Query(ctx, `
		SELECT id::text, resource_name, creation_sequence, display_order
		FROM public.repository_team_workers
		WHERE project_id = $1 AND repository_id = $2 AND status = 'active'
		ORDER BY display_order DESC
		LIMIT $3`, projectID, repositoryID, count)
	if err != nil {
		return nil, fmt.Errorf("select Workers to remove: %w", err)
	}
	defer rows.Close()
	workers := make([]workerRecord, 0, count)
	for rows.Next() {
		var worker workerRecord
		if err := rows.Scan(&worker.id, &worker.resourceName, &worker.creationSequence, &worker.displayOrder); err != nil {
			return nil, fmt.Errorf("scan Worker to remove: %w", err)
		}
		workers = append(workers, worker)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Workers to remove: %w", err)
	}
	if len(workers) != count {
		return nil, fmt.Errorf("%w: active roster changed", ErrConflict)
	}
	return workers, nil
}

func advanceRevision(ctx context.Context, tx pgx.Tx, projectID, repositoryID string, expectedRevision int64) error {
	result, err := tx.Exec(ctx, `
		UPDATE public.repository_teams
		SET roster_revision = roster_revision + 1, updated_at = now()
		WHERE project_id = $1 AND repository_id = $2 AND roster_revision = $3`,
		projectID, repositoryID, expectedRevision)
	if err != nil {
		return fmt.Errorf("advance roster revision: %w", err)
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("%w: roster revision changed", ErrConflict)
	}
	return nil
}

func teamMembers(leaderName string, existing []Member, added []workerRecord) []agentteams.TeamMember {
	members := make([]agentteams.TeamMember, 0, 1+len(existing)+len(added))
	members = append(members, agentteams.TeamMember{Name: leaderName, Role: "team_leader"})
	for _, worker := range existing {
		members = append(members, agentteams.TeamMember{Name: worker.ResourceName, Role: "worker"})
	}
	for _, worker := range added {
		members = append(members, agentteams.TeamMember{Name: worker.resourceName, Role: "worker"})
	}
	return members
}

// createRemoteWorker 建一个远端 worker,**一律建为休眠**(选仓门 §3.3):
// 上游建 worker 的请求原生带 state 字段,一步建出 Sleeping,不起真 runtime;
// 选进 issue 范围 / 派活前再唤醒(WakeTeamsForRepositories / PreDispatchCheck)。
func (s *Service) createRemoteWorker(ctx context.Context, name string) error {
	if s.client == nil {
		return fmt.Errorf("%w: controller client is nil", ErrControllerUnavailable)
	}
	_, status, err := s.client.CreateWorkerSleeping(ctx, name)
	return workerWriteResultError("create Worker", status, err)
}

func (s *Service) deleteRemoteWorker(ctx context.Context, name string) error {
	if s.client == nil {
		return fmt.Errorf("%w: controller client is nil", ErrControllerUnavailable)
	}
	_, status, err := s.client.DeleteWorker(ctx, name)
	return controllerResultError("delete Worker", status, err)
}

func (s *Service) createRemoteTeam(ctx context.Context, name string, members []agentteams.TeamMember) error {
	if s.client == nil {
		return fmt.Errorf("%w: controller client is nil", ErrControllerUnavailable)
	}
	_, status, err := s.client.CreateTeam(ctx, agentteams.TeamSpec{Name: name, WorkerMembers: members})
	return teamWriteResultError("create Team", status, err)
}

func (s *Service) updateRemoteTeam(ctx context.Context, name string, members []agentteams.TeamMember) error {
	if s.client == nil {
		return fmt.Errorf("%w: controller client is nil", ErrControllerUnavailable)
	}
	_, status, err := s.client.UpdateTeam(ctx, name, members)
	return teamWriteResultError("replace Team membership", status, err)
}

// persistRooms 回读远端团队，把房间号落到本行。
//
// 房间由 AgentTeams 控制器**异步**建立 Matrix 房之后回填进 Team CR 的 status，
// 所以 `POST/PUT /api/v1/teams` 的响应里根本没有它 —— 建完立刻回读通常也还是空的。
// 因此这里读不到就当"还没建好"：不报错、不重试、不覆盖已有值，等下一次改编制再刷。
// 房间号是观察事实，拿不到就保持 NULL，不拿团队名拼一个假的出来。
//
// 2026-09-20（0057）：落库条件跟着归属走 —— 同一个仓库可以被两个项目各建一支队，
// 只按 repository_id 更新会把 A 项目的房间号写到 B 项目那一行上。
func (s *Service) persistRooms(ctx context.Context, tx pgx.Tx, projectID, repositoryID, teamName string) error {
	if s.client == nil {
		return nil
	}
	view, status, err := s.client.GetTeam(ctx, teamName)
	if err != nil || status != http.StatusOK {
		return nil
	}
	if view.TeamRoomID == "" && view.LeaderDMRoomID == "" {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		UPDATE public.repository_teams
		SET team_room_id = COALESCE(NULLIF($3, ''), team_room_id),
		    leader_dm_room_id = COALESCE(NULLIF($4, ''), leader_dm_room_id),
		    updated_at = now()
		WHERE project_id = $1 AND repository_id = $2`,
		projectID, repositoryID, view.TeamRoomID, view.LeaderDMRoomID); err != nil {
		return fmt.Errorf("persist repository team rooms: %w", err)
	}
	return nil
}

func (s *Service) workerPhase(ctx context.Context, name string) (string, error) {
	if s.client == nil {
		return "", fmt.Errorf("%w: controller client is nil", ErrControllerUnavailable)
	}
	body, status, err := s.client.WorkerStatus(ctx, name)
	if err := controllerResultError("read Worker status", status, err); err != nil {
		return "", err
	}
	var response struct {
		Status struct {
			Phase string `json:"phase"`
		} `json:"status"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("%w: parse Worker status: %v", ErrControllerUnavailable, err)
	}
	if response.Status.Phase == "" {
		return "", fmt.Errorf("%w: Worker status has no phase", ErrControllerUnavailable)
	}
	return response.Status.Phase, nil
}

func (s *Service) compensateWorkers(ctx context.Context, resources []string, cause error) error {
	cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	var cleanupErr error
	for i := len(resources) - 1; i >= 0; i-- {
		if err := s.deleteRemoteWorker(cleanupContext, resources[i]); err != nil {
			cleanupErr = fmt.Errorf("clean up %s: %w", resources[i], err)
		}
	}
	if cleanupErr != nil {
		return reconciliationError(fmt.Errorf("%v; %w", cause, cleanupErr))
	}
	if isIndeterminateWorkerWrite(cause) {
		return reconciliationError(cause)
	}
	return cause
}

func controllerResultError(operation string, status int, err error) error {
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrControllerUnavailable, operation, err)
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return fmt.Errorf("%w: %s returned %d", ErrForbidden, operation, status)
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return fmt.Errorf("%w: %s returned %d", ErrControllerUnavailable, operation, status)
	}
	return nil
}

func teamWriteResultError(operation string, status int, err error) error {
	result := controllerResultError(operation, status, err)
	if result == nil || !indeterminateControllerResult(status, err) {
		return result
	}
	return reconciliationError(result)
}

func indeterminateControllerResult(status int, err error) bool {
	return err != nil || status >= http.StatusInternalServerError
}

type indeterminateWorkerWriteError struct {
	cause error
}

func (e *indeterminateWorkerWriteError) Error() string {
	return e.cause.Error()
}

func (e *indeterminateWorkerWriteError) Unwrap() error {
	return e.cause
}

func workerWriteResultError(operation string, status int, err error) error {
	result := controllerResultError(operation, status, err)
	if result == nil || !indeterminateControllerResult(status, err) {
		return result
	}
	return &indeterminateWorkerWriteError{cause: result}
}

func isIndeterminateWorkerWrite(err error) bool {
	var target *indeterminateWorkerWriteError
	return errors.As(err, &target)
}

func cleanupResourcesAfterWorkerCreate(resources []string, err error) []string {
	if isIndeterminateWorkerWrite(err) || len(resources) == 0 {
		return resources
	}
	return resources[:len(resources)-1]
}

func reconciliationError(err error) error {
	return fmt.Errorf("%w: %w", ErrReconciliationRequired, err)
}

func isExecutingPhase(phase string) bool {
	switch phase {
	case "Pending", "Starting", "Running", "Updating", "Stopping":
		return true
	default:
		return false
	}
}

func validWorkerCount(workerCount int) bool {
	return workerCount >= minWorkers && workerCount <= maxWorkers
}

// remotePrefix 算**新建队**的远端名。
//
// 0057 起掺入项目 id：同一仓库可以被多个项目各建一支队，只喂仓库 id 会让两个项目算出
// **同一个名字**，在远端互相覆盖。分隔符用 NUL —— 与仓库里其它指纹的写法一致，
// 也不会与 id 里可能出现的字符冲突。
//
// ⚠️ 已存在的队**不重算**（队名以库里的 agentteams_team_name 为准，见 loadTeamName）：
// 存量队名仍是老的 `repomesh-r-<仓库hash>` 形状，改名会让它们变成孤儿。
func remotePrefix(projectID, repositoryID string) string {
	digest := sha256.Sum256([]byte(projectID + "\x00" + repositoryID))
	return "repomesh-r-" + hex.EncodeToString(digest[:8])
}

func randomUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}
