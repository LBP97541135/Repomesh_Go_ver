package branchvalidation

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"time"

	"repomesh.local/repomesh/internal/testdb"
)

// cleanupProvider 是"清理可注入失败"的替身：只为验证**回收治理**的状态机，
// 不碰真数据库（真机制另有 TestPolarProviderProvisionsFromBusinessBaseline 覆盖）。
type cleanupProvider struct {
	fail    bool
	cleaned []string
}

func (p *cleanupProvider) Name() string { return "cleanup-probe" }
func (p *cleanupProvider) ProvisionBranch(context.Context, string, string) (string, error) {
	return "", errors.New("not used")
}
func (p *cleanupProvider) ApplyMigrations(context.Context, string, []string) ([]MigrationResult, error) {
	return nil, errors.New("not used")
}
func (p *cleanupProvider) CleanupBranch(_ context.Context, branchRef string) error {
	if p.fail {
		return errors.New("branch is busy")
	}
	p.cleaned = append(p.cleaned, branchRef)
	return nil
}

// seedCleanupFixture 造一条"分支已经开出来、但还没清掉"的验证行。
func seedCleanupFixture(t *testing.T, pool *pgxpool.Pool, status, branch string, createdAt time.Time, pending bool) string {
	t.Helper()
	ctx := context.Background()
	stamp := time.Now().Format("150405.000000000")
	owner := "acct-sweep-" + stamp
	if _, err := pool.Exec(ctx, `INSERT INTO repomesh_access.accounts (id, github_id, display_name)
		VALUES ($1, floor(random()*700000000)::bigint + 100000000, '回收夹具账号')`, owner); err != nil {
		t.Fatalf("播种账号: %v", err)
	}
	orgID := fixtureSweepUUID(t)
	projectID := fixtureSweepUUID(t)
	configRevision := fixtureSweepUUID(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `INSERT INTO public.organizations (id, name) VALUES ($1,'回收夹具空间')`, orgID); err != nil {
		t.Fatalf("播种空间: %v", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_projects.projects
		 (id, owner, organization_id, name, purpose, revision, creation_context_revision, current_configuration_revision)
		 VALUES ($1,$2,$3,'回收夹具项目','回收治理测试用项目',$4,$5,$6)`,
		projectID, owner, orgID, fixtureSweepUUID(t), fixtureSweepUUID(t), configRevision); err != nil {
		t.Fatalf("播种项目: %v", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_projects.configuration_revisions
		 (project_id, revision, fixed, created_by, created_at)
		 VALUES ($1,$2,'{"fixture":true}'::jsonb,$3,clock_timestamp())`, projectID, configRevision, owner); err != nil {
		t.Fatalf("播种配置修订: %v", err)
	}
	runID := fixtureSweepUUID(t)
	if _, err := tx.Exec(ctx, `INSERT INTO public.database_branch_validations
		 (id, organization_id, project_id, repository_id, candidate_sha, source_database_ref,
		  provider, provider_branch_ref, idempotency_key, status, cleanup_pending, created_at)
		 VALUES ($1,$2,$3,'repo_00000000000000000001','sha1','business_baseline',
		  'cleanup-probe', $4, $5, $6, $7, $8)`,
		runID, orgID, projectID, branch, "sweep-"+runID, status, pending, createdAt); err != nil {
		t.Fatalf("播种验证行: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return runID
}

func TestSweepCleanupRetriesAndKeepsEvidence(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	runID := seedCleanupFixture(t, pool, "passed", "branch_sweep_1", time.Now(), true)

	// 1) 清理失败：失败次数与原因都要留下，行仍在待清理集合里。
	failing := &cleanupProvider{fail: true}
	result, err := New(pool, failing).SweepCleanup(ctx, 10)
	if err != nil {
		t.Fatalf("SweepCleanup(失败路径): %v", err)
	}
	if result.Failed != 1 || result.Reclaimed != 0 {
		t.Fatalf("失败路径结果 = %+v", result)
	}
	var attempts int
	var lastError, status string
	var pending bool
	if err := pool.QueryRow(ctx, `SELECT cleanup_attempts, last_cleanup_error, cleanup_pending, status
		FROM public.database_branch_validations WHERE id=$1::uuid`, runID).
		Scan(&attempts, &lastError, &pending, &status); err != nil {
		t.Fatalf("读回: %v", err)
	}
	if attempts != 1 || lastError == "" || !pending {
		t.Fatalf("失败后状态 = attempts %d / err %q / pending %t", attempts, lastError, pending)
	}
	if status != "passed" {
		t.Fatalf("回收不得改动验证结论：status = %q", status)
	}

	// 2) 清理成功：待清理清掉、记回收时刻、错误清空；**证据仍在**（status 不变）。
	working := &cleanupProvider{}
	result, err = New(pool, working).SweepCleanup(ctx, 10)
	if err != nil {
		t.Fatalf("SweepCleanup(成功路径): %v", err)
	}
	if result.Reclaimed != 1 {
		t.Fatalf("成功路径结果 = %+v", result)
	}
	if len(working.cleaned) != 1 || working.cleaned[0] != "branch_sweep_1" {
		t.Fatalf("没有真去删那个分支：%v", working.cleaned)
	}
	var reclaimed *time.Time
	if err := pool.QueryRow(ctx, `SELECT cleanup_pending, last_cleanup_error, reclaimed_at, status
		FROM public.database_branch_validations WHERE id=$1::uuid`, runID).
		Scan(&pending, &lastError, &reclaimed, &status); err != nil {
		t.Fatalf("读回: %v", err)
	}
	if pending || lastError != "" || reclaimed == nil {
		t.Fatalf("回收后状态 = pending %t / err %q / reclaimed %v", pending, lastError, reclaimed)
	}
	if status != "passed" {
		t.Fatalf("回收后验证结论被改动：status = %q", status)
	}
}

func TestReconcileStaleResumesInterruptedRuns(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	// 一条"开了分支但进程中断"的（该回收）、一条"连分支都没开出来"的（该如实标失败）。
	withBranch := seedCleanupFixture(t, pool, "provisioning", "branch_stale_1", time.Now().Add(-2*time.Hour), false)
	noBranch := seedCleanupFixture(t, pool, "provisioning", "", time.Now().Add(-2*time.Hour), false)
	// 新的一行不该被动（还没超时）。
	fresh := seedCleanupFixture(t, pool, "provisioning", "branch_fresh_1", time.Now(), false)

	reconciled, err := New(pool, &cleanupProvider{}).ReconcileStale(ctx, 30*time.Minute)
	if err != nil {
		t.Fatalf("ReconcileStale: %v", err)
	}
	if reconciled != 2 {
		t.Fatalf("对账行数 = %d，应为 2", reconciled)
	}
	var pending bool
	if err := pool.QueryRow(ctx, `SELECT cleanup_pending FROM public.database_branch_validations
		WHERE id=$1::uuid`, withBranch).Scan(&pending); err != nil || !pending {
		t.Fatalf("中断且已开分支的行应当标成待回收：pending=%t err=%v", pending, err)
	}
	var status, failureCode string
	if err := pool.QueryRow(ctx, `SELECT status, COALESCE(failure_code,'') FROM public.database_branch_validations
		WHERE id=$1::uuid`, noBranch).Scan(&status, &failureCode); err != nil {
		t.Fatalf("读回: %v", err)
	}
	if status != "failed" || failureCode != "ABANDONED_INTERRUPTED" {
		t.Fatalf("没开出分支的中断行 = %s / %s", status, failureCode)
	}
	var freshPending bool
	if err := pool.QueryRow(ctx, `SELECT cleanup_pending FROM public.database_branch_validations
		WHERE id=$1::uuid`, fresh).Scan(&freshPending); err != nil || freshPending {
		t.Fatalf("新行不该被动：pending=%t err=%v", freshPending, err)
	}
}

func fixtureSweepUUID(t *testing.T) string {
	t.Helper()
	id, err := newID("sweep-")
	if err != nil {
		t.Fatalf("uuid: %v", err)
	}
	return id
}
