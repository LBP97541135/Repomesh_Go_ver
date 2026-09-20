package repositoryteams

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
func (s *Service) Get(ctx context.Context, repositoryID string) (Snapshot, error) {
	if s.pool == nil {
		return Snapshot{}, fmt.Errorf("%w: database pool is nil", ErrControllerUnavailable)
	}
	snapshot, err := s.loadSnapshot(ctx, s.pool, repositoryID)
	if err != nil {
		return Snapshot{}, err
	}
	if snapshot.Leader.RuntimePhase, err = s.workerPhase(ctx, snapshot.Leader.ResourceName); err != nil {
		return Snapshot{}, err
	}
	for i := range snapshot.Workers {
		if snapshot.Workers[i].RuntimePhase, err = s.workerPhase(ctx, snapshot.Workers[i].ResourceName); err != nil {
			return Snapshot{}, err
		}
	}
	return snapshot, nil
}

// Create creates one remote Team with exactly one Leader and the requested
// number of active Workers. The repository ID, not its display name, owns all
// local and remote associations.
func (s *Service) Create(ctx context.Context, repositoryID string, workerCount int) (Snapshot, error) {
	tx, err := s.beginLocked(ctx, repositoryID)
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
		"SELECT EXISTS (SELECT 1 FROM public.repository_teams WHERE repository_id = $1)", repositoryID,
	).Scan(&exists); err != nil {
		return Snapshot{}, fmt.Errorf("check repository team: %w", err)
	}
	if exists {
		return Snapshot{}, fmt.Errorf("%w: repository team already exists", ErrConflict)
	}

	prefix := remotePrefix(repositoryID)
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
		(repository_id, agentteams_team_name, leader_id, leader_resource_name)
		VALUES ($1, $2, $3::uuid, $4)`, repositoryID, prefix, leaderID, leaderName); err != nil {
		return Snapshot{}, reconciliationError(fmt.Errorf("persist repository team: %w", err))
	}
	for _, worker := range workers {
		if _, err := tx.Exec(ctx, `
			INSERT INTO public.repository_team_workers
			(id, repository_id, resource_name, creation_sequence, display_order)
			VALUES ($1::uuid, $2, $3, $4, $5)`,
			worker.id, repositoryID, worker.resourceName, worker.creationSequence, worker.displayOrder,
		); err != nil {
			return Snapshot{}, reconciliationError(fmt.Errorf("persist repository Worker: %w", err))
		}
	}

	snapshot, err := s.loadSnapshot(ctx, tx, repositoryID)
	if err != nil {
		return Snapshot{}, reconciliationError(fmt.Errorf("load created roster: %w", err))
	}
	if err := tx.Commit(ctx); err != nil {
		return Snapshot{}, reconciliationError(fmt.Errorf("commit created roster: %w", err))
	}
	return snapshot, nil
}

// repoTeamResolutionQuery 把**项目侧**仓库解析成**扫描侧**仓库（按 URL 对齐）。
//
// 两个 id 空间不同（项目侧 "repo_0000…"、扫描侧 32 位十六进制），而建队只认扫描侧
// id（Create 用它查仓库名、repository_teams.repository_id 存的也是它）。规则与
// internal/discovery/recall.go 的 loadRepoPool 完全一致：同一组织下，把扫描 URL 去掉
// 结尾斜杠与 .git 后，与 https/http/ssh 三种写法之一相等。
const repoTeamResolutionQuery = `
	SELECT pr.project_id AS project_id, pr.repository_id AS project_side, COALESCE(s.id, '') AS scan_side
	FROM repomesh_projects.project_repositories pr
	JOIN repomesh_projects.repositories r ON r.id = pr.repository_id
	JOIN repomesh_projects.projects p ON p.id = pr.project_id
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

// EnsureForProject 保证"本项目里每个已接入的仓库都有一支自己的团队"。
//
// 2026-09-20（用户）：仓库**接入的时候**就该自动建队，不该让人再去仓库页点一次。
// 幂等：已经有团队的仓库直接跳过；Create 自己会撞 ErrConflict，所以并发/重试也安全。
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
	rows, err := s.pool.Query(ctx, repoTeamResolutionQuery+`
		WHERE pr.project_id=$1
		  AND s.id IS NOT NULL
		  AND NOT EXISTS (SELECT 1 FROM public.repository_teams t WHERE t.repository_id=s.id)
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
		if _, err := s.Create(ctx, id, workerCount); err != nil {
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

// EnsureAll 扫**所有**项目，把还没建队的仓库补上。服务重启后、或本次改动之前接入的
// 仓库，都靠它收敛（幂等，重复跑没有副作用）。
func (s *Service) EnsureAll(ctx context.Context, workerCount int) ([]string, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("%w: database pool is nil", ErrControllerUnavailable)
	}
	// 与 EnsureForProject 同一条解析规则：只挑"真的还有可建队的仓库"的项目，
	// 否则每轮都会把没有扫描记录的仓库再算一遍（永远建不出来，纯空转）。
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT resolved.project_id
		FROM (`+repoTeamResolutionQuery+`) resolved
		WHERE resolved.scan_side <> ''
		  AND NOT EXISTS (SELECT 1 FROM public.repository_teams t WHERE t.repository_id=resolved.scan_side)`)
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
func (s *Service) Change(ctx context.Context, repositoryID string, command ChangeCommand) (Snapshot, error) {
	tx, err := s.beginLocked(ctx, repositoryID)
	if err != nil {
		return Snapshot{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	current, err := s.loadSnapshot(ctx, tx, repositoryID)
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
		return s.scaleUp(ctx, tx, repositoryID, current, delta)
	default:
		return s.scaleDown(ctx, tx, repositoryID, current, -delta)
	}
}

func (s *Service) scaleUp(ctx context.Context, tx pgx.Tx, repositoryID string, current Snapshot, delta int) (Snapshot, error) {
	var highestSequence int
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(creation_sequence), 0)
		FROM public.repository_team_workers
		WHERE repository_id = $1`, repositoryID,
	).Scan(&highestSequence); err != nil {
		return Snapshot{}, fmt.Errorf("load Worker creation sequence: %w", err)
	}

	prefix := remotePrefix(repositoryID)
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
	if err := s.updateRemoteTeam(ctx, remotePrefix(repositoryID), teamMembers(current.Leader.ResourceName, current.Workers, workers)); err != nil {
		if errors.Is(err, ErrReconciliationRequired) {
			return Snapshot{}, err
		}
		return Snapshot{}, s.compensateWorkers(ctx, createdResources, err)
	}

	for _, worker := range workers {
		if _, err := tx.Exec(ctx, `
			INSERT INTO public.repository_team_workers
			(id, repository_id, resource_name, creation_sequence, display_order)
			VALUES ($1::uuid, $2, $3, $4, $5)`,
			worker.id, repositoryID, worker.resourceName, worker.creationSequence, worker.displayOrder,
		); err != nil {
			return Snapshot{}, reconciliationError(fmt.Errorf("persist scaled Worker: %w", err))
		}
	}
	if err := advanceRevision(ctx, tx, repositoryID, current.RosterRevision); err != nil {
		return Snapshot{}, reconciliationError(err)
	}
	updated, err := s.loadSnapshot(ctx, tx, repositoryID)
	if err != nil {
		return Snapshot{}, reconciliationError(fmt.Errorf("load scaled roster: %w", err))
	}
	if err := tx.Commit(ctx); err != nil {
		return Snapshot{}, reconciliationError(fmt.Errorf("commit scaled roster: %w", err))
	}
	return updated, nil
}

func (s *Service) scaleDown(ctx context.Context, tx pgx.Tx, repositoryID string, current Snapshot, delta int) (Snapshot, error) {
	selected, err := selectedWorkers(ctx, tx, repositoryID, delta)
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
	if err := s.updateRemoteTeam(ctx, remotePrefix(repositoryID), teamMembers(current.Leader.ResourceName, remaining, nil)); err != nil {
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
			WHERE id = $1::uuid AND repository_id = $2 AND status = 'active'`, worker.id, repositoryID); err != nil {
			return Snapshot{}, reconciliationError(fmt.Errorf("disable Worker: %w", err))
		}
	}
	if err := advanceRevision(ctx, tx, repositoryID, current.RosterRevision); err != nil {
		return Snapshot{}, reconciliationError(err)
	}
	updated, err := s.loadSnapshot(ctx, tx, repositoryID)
	if err != nil {
		return Snapshot{}, reconciliationError(fmt.Errorf("load reduced roster: %w", err))
	}
	if err := tx.Commit(ctx); err != nil {
		return Snapshot{}, reconciliationError(fmt.Errorf("commit reduced roster: %w", err))
	}
	return updated, nil
}

func (s *Service) beginLocked(ctx context.Context, repositoryID string) (pgx.Tx, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("database pool is nil")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin repository team transaction: %w", err)
	}
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", repositoryID); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("lock repository team: %w", err)
	}
	return tx, nil
}

type snapshotQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (s *Service) loadSnapshot(ctx context.Context, db snapshotQuerier, repositoryID string) (Snapshot, error) {
	var snapshot Snapshot
	var leaderID string
	err := db.QueryRow(ctx, `
		SELECT t.repository_id, r.name, t.roster_revision, t.runtime_status,
		       t.leader_id::text, t.leader_resource_name
		FROM public.repository_teams t
		JOIN repomesh_scan.repositories r ON r.id = t.repository_id
		WHERE t.repository_id = $1`, repositoryID,
	).Scan(
		&snapshot.RepositoryID,
		&snapshot.RepositoryName,
		&snapshot.RosterRevision,
		&snapshot.RuntimeStatus,
		&leaderID,
		&snapshot.Leader.ResourceName,
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
		WHERE repository_id = $1 AND status = 'active'
		ORDER BY display_order`, repositoryID,
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

func selectedWorkers(ctx context.Context, tx pgx.Tx, repositoryID string, count int) ([]workerRecord, error) {
	rows, err := tx.Query(ctx, `
		SELECT id::text, resource_name, creation_sequence, display_order
		FROM public.repository_team_workers
		WHERE repository_id = $1 AND status = 'active'
		ORDER BY display_order DESC
		LIMIT $2`, repositoryID, count)
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

func advanceRevision(ctx context.Context, tx pgx.Tx, repositoryID string, expectedRevision int64) error {
	result, err := tx.Exec(ctx, `
		UPDATE public.repository_teams
		SET roster_revision = roster_revision + 1, updated_at = now()
		WHERE repository_id = $1 AND roster_revision = $2`, repositoryID, expectedRevision)
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

func (s *Service) createRemoteWorker(ctx context.Context, name string) error {
	if s.client == nil {
		return fmt.Errorf("%w: controller client is nil", ErrControllerUnavailable)
	}
	_, status, err := s.client.CreateWorker(ctx, agentteams.WorkerSpec{Name: name})
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

func remotePrefix(repositoryID string) string {
	digest := sha256.Sum256([]byte(repositoryID))
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
