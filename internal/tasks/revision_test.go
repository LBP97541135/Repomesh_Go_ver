package tasks

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"repomesh.local/repomesh/internal/testdb"
)

// seedOrgProject plants a real account+organization+A-suite project trio —
// the revision flow resolves the plan's organization through its project,
// which since 0030 is repomesh_projects.projects (0010 外键语义).
func seedOrgProject(t *testing.T, pool *pgxpool.Pool) (string, string) {
	t.Helper()
	repos := []string{"gateway", "sdk", "third", "common", "repo-a", "repo-other", "known-repo", "isolated-repo"}
	for i := 0; i < 30; i++ {
		repos = append(repos, fmt.Sprintf("extra-%d", i))
	}
	f := testdb.SeedProject(t, pool, "", "", repos...)
	org, project := f.OrganizationID, f.ID

	return org, project
}

func newPlan(t *testing.T, pool *pgxpool.Pool, store *PostgresStore, key string) ExecutionPlan {
	t.Helper()
	_, project := seedOrgProject(t, pool)
	plan, err := store.CreatePlan(context.Background(), PlanWrite{
		ProjectID:       project,
		RequirementText: "为项目选择合适的仓库并完成改造",
		RequirementKey:  key,
		Batches:         [][]string{{"gateway"}, {"sdk"}},
		DAG:             map[string][]string{"sdk": {"gateway"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func TestValidateBatchesRejectsBrokenGraph(t *testing.T) {
	cases := []struct {
		name    string
		batches [][]string
		dag     map[string][]string
	}{
		{"dependency in later batch", [][]string{{"sdk"}, {"gateway"}},
			map[string][]string{"sdk": {"gateway"}}},
		{"unknown dependency", [][]string{{"gateway"}}, map[string][]string{"gateway": {"ghost"}}},
		{"duplicate repository", [][]string{{"gateway"}, {"gateway"}}, nil},
		{"empty batch", [][]string{{"gateway"}, {}}, nil},
		{"no batches", nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := ValidateBatches(c.batches, c.dag); !errors.Is(err, ErrInvalidPlan) {
				t.Fatalf("err = %v, want ErrInvalidPlan", err)
			}
		})
	}
	if err := ValidateBatches([][]string{{"gateway"}, {"sdk"}},
		map[string][]string{"sdk": {"gateway"}}); err != nil {
		t.Fatalf("valid snapshot rejected: %v", err)
	}
}

func TestApplyRevisionLifecycle(t *testing.T) {
	pool := testdb.Open(t)
	store := NewPostgresStore(pool)
	ctx := context.Background()
	plan := newPlan(t, pool, store, "replan-key")

	// 补充范围：新增 third 仓库（v2 快照）
	second, err := store.ApplyRevision(ctx, RevisionCommand{
		PlanID: plan.ID, ExpectedVersion: "v1",
		NewBatches: [][]string{{"gateway"}, {"sdk", "third"}},
		NewTasks: []TaskSnapshot{
			{TaskUID: "gateway-fix", RepositoryID: "gateway", Title: "网关改造"},
			{TaskUID: "third-migrate", RepositoryID: "third", Title: "新增接入"},
		},
		DAG:   map[string][]string{"third": {"gateway"}},
		Actor: "leader", Reason: "网关撞墙，连带 SDK 与新增 third",
		IdempotencyKey: "rev-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Revision != 1 || second.ResultVersion != "v2" || second.BaseVersion != "v1" {
		t.Fatalf("revision = %+v", second)
	}
	if len(second.AddedRepositories) != 1 || second.AddedRepositories[0] != "third" {
		t.Fatalf("added = %v", second.AddedRepositories)
	}
	if second.CreatedTasks != 2 || second.SupersededTasks != 0 {
		t.Fatalf("task migration = created %d superseded %d, want 2/0",
			second.CreatedTasks, second.SupersededTasks)
	}
	updated, err := store.GetPlan(ctx, plan.ID)
	if err != nil || updated.PlanVersion != "v2" || len(updated.Batches) != 2 {
		t.Fatalf("plan after revision = %+v err %v", updated, err)
	}

	// 幂等重放：同键同含义返回原记录，版本不动。
	replay, err := store.ApplyRevision(ctx, RevisionCommand{
		PlanID: plan.ID, ExpectedVersion: "v1",
		NewBatches: [][]string{{"gateway"}, {"sdk", "third"}},
		NewTasks: []TaskSnapshot{
			{TaskUID: "gateway-fix", RepositoryID: "gateway", Title: "网关改造"},
			{TaskUID: "third-migrate", RepositoryID: "third", Title: "新增接入"},
		},
		DAG:   map[string][]string{"third": {"gateway"}},
		Actor: "leader", Reason: "网关撞墙，连带 SDK 与新增 third",
		IdempotencyKey: "rev-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if replay.ResultVersion != "v2" {
		t.Fatalf("replay = %+v, want the original revision", replay)
	}
	afterReplay, _ := store.GetPlan(ctx, plan.ID)
	if afterReplay.PlanVersion != "v2" {
		t.Fatalf("replay bumped version to %s", afterReplay.PlanVersion)
	}

	// 同键不同含义 → 冲突。
	if _, err := store.ApplyRevision(ctx, RevisionCommand{
		PlanID: plan.ID, ExpectedVersion: "v2",
		NewBatches: [][]string{{"gateway"}}, DAG: map[string][]string{},
		Actor: "leader", Reason: "换了内容", IdempotencyKey: "rev-1",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("key meaning change = %v, want ErrConflict", err)
	}

	// 过期 base 版本 → 冲突。
	if _, err := store.ApplyRevision(ctx, RevisionCommand{
		PlanID: plan.ID, ExpectedVersion: "v1",
		NewBatches: [][]string{{"gateway"}, {"sdk"}}, DAG: map[string][]string{},
		Actor: "leader", Reason: "stale", IdempotencyKey: "rev-stale",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale version = %v, want ErrConflict", err)
	}

	// DAG 违规快照 → 拒绝。
	if _, err := store.ApplyRevision(ctx, RevisionCommand{
		PlanID: plan.ID, ExpectedVersion: "v2",
		NewBatches: [][]string{{"sdk"}, {"gateway"}},
		DAG:        map[string][]string{"sdk": {"gateway"}},
		Actor:      "leader", Reason: "bad dag", IdempotencyKey: "rev-bad",
	}); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("dag violation = %v, want ErrInvalidPlan", err)
	}

	history, err := store.PlanRevisions(ctx, plan.ID)
	if err != nil || len(history) != 1 || history[0].Revision != 1 {
		t.Fatalf("history = %+v err %v", history, err)
	}
}

func TestConcurrentApplySingleWinner(t *testing.T) {
	pool := testdb.Open(t)
	store := NewPostgresStore(pool)
	ctx := context.Background()
	plan := newPlan(t, pool, store, "replan-conc")

	const leaders = 6
	var wg sync.WaitGroup
	wins := make(chan string, leaders)
	losses := make(chan error, leaders)
	for i := 0; i < leaders; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rev, err := store.ApplyRevision(ctx, RevisionCommand{
				PlanID: plan.ID, ExpectedVersion: "v1",
				NewBatches: [][]string{{"gateway"}, {"sdk", fmt.Sprintf("extra-%d", i)}},
				DAG:        map[string][]string{},
				Actor:      fmt.Sprintf("leader-%d", i), Reason: "并发修订",
				IdempotencyKey: fmt.Sprintf("rev-conc-%d", i),
			})
			if err != nil {
				losses <- err
				return
			}
			wins <- rev.ResultVersion
		}(i)
	}
	wg.Wait()
	close(wins)
	close(losses)

	won := 0
	for range wins {
		won++
	}
	if won != 1 {
		t.Fatalf("winners = %d, want exactly 1", won)
	}
	for err := range losses {
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("loser error = %v, want ErrConflict", err)
		}
	}
	final, err := store.GetPlan(ctx, plan.ID)
	if err != nil || final.PlanVersion != "v2" {
		t.Fatalf("final version = %+v err %v, want v2", final, err)
	}
}

func TestApplyMigratesTaskAxis(t *testing.T) {
	pool := testdb.Open(t)
	store := NewPostgresStore(pool)
	ctx := context.Background()
	org, project := seedOrgProject(t, pool)
	plan, err := store.CreatePlan(ctx, PlanWrite{
		ProjectID: project, RequirementText: "为项目选择合适的仓库并完成改造",
		RequirementKey: "replan-key",
		Batches:        [][]string{{"gateway"}, {"sdk"}},
		DAG:            map[string][]string{"sdk": {"gateway"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	// v1 的任务：两个带 uid，一个只有自然键；SDK 在跑。
	gateway, err := store.CreateTask(ctx, TaskWrite{OrganizationID: org, ProjectID: project,
		PlanID: plan.ID, TaskUID: "gateway-fix", RepositoryID: "gateway",
		Title: "改造网关协议", IdempotencyKey: "evt-g1"})
	if err != nil {
		t.Fatal(err)
	}
	sdk, err := store.CreateTask(ctx, TaskWrite{OrganizationID: org, ProjectID: project,
		PlanID: plan.ID, TaskUID: "sdk-fix", RepositoryID: "sdk",
		Title: "同步 SDK 字段", IdempotencyKey: "evt-s1"})
	if err != nil {
		t.Fatal(err)
	}
	sdkAttempt, err := store.Assign(ctx, sdk.ID, newUUIDv4(), newUUIDv4(), sdk.Version, 0, "首派")
	if err != nil {
		t.Fatal(err)
	}
	natural, err := store.CreateTask(ctx, TaskWrite{OrganizationID: org, ProjectID: project,
		PlanID: plan.ID, RepositoryID: "common", Title: "公共库对齐", IdempotencyKey: "evt-n1"})
	if err != nil {
		t.Fatal(err)
	}

	v2Batches := [][]string{{"gateway"}, {"third"}}
	v2Tasks := []TaskSnapshot{
		{TaskUID: "gateway-fix", RepositoryID: "gateway", Title: "改造网关协议 v2"},
		{RepositoryID: "gateway", Title: "网关补充用例"},
		{TaskUID: "third-migrate", RepositoryID: "third", Title: "新增仓库接入"},
	}
	rev, err := store.ApplyRevision(ctx, RevisionCommand{
		PlanID: plan.ID, ExpectedVersion: "v1", NewBatches: v2Batches, NewTasks: v2Tasks,
		DAG:   map[string][]string{"third": {"gateway"}},
		Actor: "leader", Reason: "执行中发现连带范围，局部重排",
		IdempotencyKey: "rev-tasks-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if rev.CreatedTasks != 2 || rev.SupersededTasks != 2 {
		t.Fatalf("migration outcome = created %d superseded %d, want 2/2",
			rev.CreatedTasks, rev.SupersededTasks)
	}
	if len(rev.NewTasks) != 3 {
		t.Fatalf("revision must carry the v2 task snapshot, got %d", len(rev.NewTasks))
	}

	// 同 uid 原位更新：行 ID 不变、标题已换、状态保持。
	inPlace, err := store.GetTask(ctx, gateway.ID)
	if err != nil || inPlace.Status != StatusPending || inPlace.Title != "改造网关协议 v2" {
		t.Fatalf("uid match must update in place: %+v err %v", inPlace, err)
	}

	// 被移出的 SDK 任务 SUPERSEDED，其 attempt 被闸门关闭。
	supersededSDK, err := store.GetTask(ctx, sdk.ID)
	if err != nil || supersededSDK.Status != StatusSuperseded {
		t.Fatalf("removed task = %+v err %v", supersededSDK, err)
	}
	if ok, _ := store.AllowsProjection(ctx, sdk.ID, sdkAttempt.ID, sdkAttempt.Generation); ok {
		t.Fatal("superseded task's attempt must not project")
	}
	if naturalRow, _ := store.GetTask(ctx, natural.ID); naturalRow.Status != StatusSuperseded {
		t.Fatalf("natural-key task = %+v, want superseded", naturalRow)
	}

	// 幂等重放（含任务集）：不重复迁移、不抬版本。
	replay, err := store.ApplyRevision(ctx, RevisionCommand{
		PlanID: plan.ID, ExpectedVersion: "v1", NewBatches: v2Batches, NewTasks: v2Tasks,
		DAG:   map[string][]string{"third": {"gateway"}},
		Actor: "leader", Reason: "执行中发现连带范围，局部重排",
		IdempotencyKey: "rev-tasks-1",
	})
	if err != nil || replay.ResultVersion != "v2" {
		t.Fatalf("replay = %+v err %v, want the original revision", replay, err)
	}
	afterReplay, _ := store.GetPlan(ctx, plan.ID)
	if afterReplay.PlanVersion != "v2" {
		t.Fatalf("replay bumped plan version to %s", afterReplay.PlanVersion)
	}
}

type recordingSink struct {
	mu    sync.Mutex
	calls []PlanRevisedEvent
	err   error
}

func (r *recordingSink) RecordPlanRevised(ctx context.Context, tx pgx.Tx, e PlanRevisedEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, e)
	return r.err
}

func (r *recordingSink) RecordFeedback(ctx context.Context, e FeedbackEvent) (string, time.Time, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return "", time.Time{}, r.err
	}
	return newUUIDv4(), time.Now(), nil
}

func (r *recordingSink) BlockedSince(ctx context.Context, requirementKey string, since time.Time) ([]BlockedFeedback, error) {
	return nil, nil
}

func TestApplyRevisionDecisionHook(t *testing.T) {
	pool := testdb.Open(t)
	store := NewPostgresStore(pool)
	ctx := context.Background()
	org, project := seedOrgProject(t, pool)
	plan, err := store.CreatePlan(ctx, PlanWrite{
		ProjectID: project, RequirementText: "为项目选择合适的仓库并完成改造",
		RequirementKey: "replan-key",
		Batches:        [][]string{{"gateway"}, {"sdk"}},
		DAG:            map[string][]string{"sdk": {"gateway"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	sink := &recordingSink{}
	store.DecisionSink = sink

	// 预置一个同 uid 任务：验证修订原位更新它（org 变量同时被消费）。
	if _, err := store.CreateTask(ctx, TaskWrite{OrganizationID: org, ProjectID: project,
		PlanID: plan.ID, TaskUID: "gateway-fix", RepositoryID: "gateway",
		Title: "网关改造", IdempotencyKey: "evt-hook-0"}); err != nil {
		t.Fatal(err)
	}

	_, err = store.ApplyRevision(ctx, RevisionCommand{
		PlanID: plan.ID, ExpectedVersion: "v1",
		NewBatches: [][]string{{"gateway"}, {"sdk", "third"}},
		NewTasks: []TaskSnapshot{
			{TaskUID: "gateway-fix", RepositoryID: "gateway", Title: "网关改造"},
			{TaskUID: "third-migrate", RepositoryID: "third", Title: "新增接入"},
		},
		DAG:   map[string][]string{"third": {"gateway"}},
		Actor: "leader", Reason: "撞墙连带",
		UpstreamRef:    "blocked-node-id",
		IdempotencyKey: "rev-hook-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.calls) != 1 {
		t.Fatalf("sink calls = %d, want 1", len(sink.calls))
	}
	e := sink.calls[0]
	if e.RequirementKey != plan.RequirementKey || e.Actor != "leader" ||
		e.UpstreamRef != "blocked-node-id" || e.IdempotencyKey != "rev-hook-1:decision" {
		t.Fatalf("sink event = %+v", e)
	}
	if len(e.AffectedRepositories) != 3 {
		t.Fatalf("affected = %v, want gateway/sdk/third", e.AffectedRepositories)
	}
}

func TestApplyRevisionSinkErrorRollsBack(t *testing.T) {
	pool := testdb.Open(t)
	store := NewPostgresStore(pool)
	ctx := context.Background()
	org, project := seedOrgProject(t, pool)
	plan, err := store.CreatePlan(ctx, PlanWrite{
		ProjectID: project, RequirementText: "为项目选择合适的仓库并完成改造",
		RequirementKey: "replan-key",
		Batches:        [][]string{{"gateway"}, {"sdk"}},
		DAG:            map[string][]string{"sdk": {"gateway"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	sink := &recordingSink{err: errors.New("decision store unavailable")}
	store.DecisionSink = sink

	task, err := store.CreateTask(ctx, TaskWrite{OrganizationID: org, ProjectID: project,
		PlanID: plan.ID, TaskUID: "gateway-fix", RepositoryID: "gateway",
		Title: "网关改造", IdempotencyKey: "evt-rb-1"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.ApplyRevision(ctx, RevisionCommand{
		PlanID: plan.ID, ExpectedVersion: "v1",
		NewBatches: [][]string{{"gateway"}, {"third"}},
		NewTasks:   []TaskSnapshot{{TaskUID: "gateway-fix", RepositoryID: "gateway", Title: "网关改造 v2"}},
		DAG:        map[string][]string{"third": {"gateway"}},
		Actor:      "leader", Reason: "回滚验证",
		IdempotencyKey: "rev-rb-1",
	}); err == nil {
		t.Fatal("sink error must fail the revision")
	}

	after, err := store.GetPlan(ctx, plan.ID)
	if err != nil || after.PlanVersion != "v1" {
		t.Fatalf("plan must roll back to v1: %+v err %v", after, err)
	}
	revisions, _ := store.PlanRevisions(ctx, plan.ID)
	if len(revisions) != 0 {
		t.Fatalf("revision entries = %d, want 0", len(revisions))
	}
	kept, _ := store.GetTask(ctx, task.ID)
	if kept.Status != StatusPending || kept.Title != "网关改造" {
		t.Fatalf("task must roll back too: %+v", kept)
	}
}
