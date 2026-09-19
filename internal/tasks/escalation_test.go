package tasks

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"repomesh.local/repomesh/internal/decisionchain"
	"repomesh.local/repomesh/internal/testdb"
)

// chainSink adapts internal/decisionchain to tasks.DecisionSink — the same
// composition the server assembly uses: 梯子、窗口与重规划共用一条决策链。
type chainSink struct {
	chain *decisionchain.PostgresStore
}

func (s chainSink) RecordPlanRevised(ctx context.Context, tx pgx.Tx, e PlanRevisedEvent) error {
	_, err := s.chain.RecordPlanRevisedInTx(ctx, tx, decisionchain.Event{
		Requirement: e.RequirementText, Actor: e.Actor,
		IdempotencyKey: e.IdempotencyKey, RepositoryIDs: e.AffectedRepositories,
		Accepted: true, Status: decisionchain.StatusAdjusted, UpstreamRef: e.UpstreamRef,
	})
	return err
}

func (s chainSink) RecordFeedback(ctx context.Context, e FeedbackEvent) (string, time.Time, error) {
	node, err := s.chain.RecordFeedback(ctx, decisionchain.Event{
		Requirement: e.RequirementText, Actor: e.ReporterID,
		IdempotencyKey: e.IdempotencyKey, RepositoryIDs: e.AffectedRepositories,
		Accepted: false, Status: decisionchain.DecisionStatus(e.Status),
		UpstreamRef: e.UpstreamRef, Rationale: e.Reason,
		ContextRef: map[string]any{"role": string(e.Role), "sourceIds": e.AffectedRepositories},
	})
	if err != nil {
		return "", time.Time{}, err
	}
	return node.ID, node.CreatedAt, nil
}

func (s chainSink) BlockedSince(ctx context.Context, requirementKey string, since time.Time) ([]BlockedFeedback, error) {
	nodes, err := s.chain.BlockedSince(ctx, requirementKey, since)
	if err != nil {
		return nil, err
	}
	out := make([]BlockedFeedback, 0, len(nodes))
	for _, node := range nodes {
		role, _ := node.ContextRef["role"].(string)
		out = append(out, BlockedFeedback{
			NodeID: node.ID, ReporterID: node.ActorID, Role: role,
			Reason: node.Rationale, UpstreamRef: node.ParentNodeID,
			AffectedRepositories: node.AffectedRepositories, CreatedAt: node.CreatedAt,
		})
	}
	return out, nil
}

func TestEscalationLadderAndCollectionWindow(t *testing.T) {
	pool := testdb.Open(t)
	store := NewPostgresStore(pool)
	svc := &EscalationService{
		Store:  store,
		Sink:   chainSink{chain: decisionchain.NewPostgresStore(pool)},
		Window: 50 * time.Millisecond,
	}
	ctx := context.Background()

	_, project := seedOrgProject(t, pool)
	plan, err := store.CreatePlan(ctx, PlanWrite{
		ProjectID:       project,
		RequirementText: "为项目选择合适的仓库并完成改造",
		RequirementKey:  decisionchain.RequirementKey("为项目选择合适的仓库并完成改造"),
		Batches:         [][]string{{"gateway"}, {"sdk"}},
		DAG:             map[string][]string{"sdk": {"gateway"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	// 两个 Worker 各报一条 BLOCKED。
	first, firstAt, err := svc.ReportBlocked(ctx, BlockedReport{PlanID: plan.ID, ReporterID: "worker-1",
		Role: RoleWorker, Reason: "要改的文件在 SDK 仓库",
		AffectedRepositories: []string{"gateway", "sdk"}})
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := svc.ReportBlocked(ctx, BlockedReport{PlanID: plan.ID, ReporterID: "worker-2",
		Role: RoleWorker, Reason: "依赖缺失", AffectedRepositories: []string{"sdk"}})
	if err != nil {
		t.Fatal(err)
	}

	// TM 消化第二条（closed，不出现在收集结果），升级第一条（blocked，上游指 w1）。
	if err := svc.ResolveWithinRepo(ctx, plan.ID, second, "tm-gateway", "本仓可消化", "tm-digest-2"); err != nil {
		t.Fatal(err)
	}
	escalated, _, err := svc.ReportBlocked(ctx, BlockedReport{PlanID: plan.ID, ReporterID: "tm-gateway",
		Role: RoleTM, Reason: "确认超范围，需重规划",
		AffectedRepositories: []string{"gateway", "sdk"}, UpstreamRef: first})
	if err != nil {
		t.Fatal(err)
	}

	feedback, err := svc.MarkDeprecatedAndCollect(ctx, plan.ID, firstAt)
	if err != nil {
		t.Fatal(err)
	}

	planAfter, err := store.GetPlan(ctx, plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if planAfter.ReplanState != ReplanStateDeprecated {
		t.Fatalf("plan replan_state = %q, want deprecated", planAfter.ReplanState)
	}

	// 收集的是梯子末梢：TM 升级节点 + 未升级的 worker-2 并发反馈（30 秒窗
	// 合并语义）；w1 原节点作为被升级的父节点剔除；被消化的 closed 节点不出现。
	if len(feedback) != 2 {
		t.Fatalf("ladder leaves = %d (%+v), want 2", len(feedback), feedback)
	}
	ids := map[string]BlockedFeedback{}
	for _, f := range feedback {
		ids[f.NodeID] = f
	}
	esc, ok := ids[escalated]
	if !ok || esc.Role != string(RoleTM) {
		t.Fatalf("TM escalation leaf missing: %+v", feedback)
	}
	if _, ok := ids[second]; !ok {
		t.Fatalf("concurrent worker-2 feedback missing: %+v", feedback)
	}
	if _, ok := ids[first]; ok {
		t.Fatal("escalated parent must be replaced by its child")
	}

	// 双轴挂钩照常工作：同一 plan 上走一次重规划，决策链落 adjusted 节点。
	rev, err := store.ApplyRevision(ctx, RevisionCommand{
		PlanID: plan.ID, ExpectedVersion: "v1",
		NewBatches: [][]string{{"gateway"}, {"sdk", "third"}},
		NewTasks: []TaskSnapshot{
			{TaskUID: "gateway-fix", RepositoryID: "gateway", Title: "网关改造"},
			{TaskUID: "third-migrate", RepositoryID: "third", Title: "新增接入"},
		},
		DAG:   map[string][]string{"third": {"gateway"}},
		Actor: "leader", Reason: "收集窗后局部重排",
		UpstreamRef:    escalated,
		IdempotencyKey: "rev-ladder-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if rev.CreatedTasks != 2 || rev.SupersededTasks != 0 {
		t.Fatalf("revision migration = created %d superseded %d, want 2/0", rev.CreatedTasks, rev.SupersededTasks)
	}
}

type fakeCatalog struct{ ready map[string]bool }

func (f fakeCatalog) Ready(ctx context.Context, name string) (bool, error) {
	return f.ready[name], nil
}

func TestGateReadySplitsReadyAndPending(t *testing.T) {
	svc := &EscalationService{Catalog: fakeCatalog{ready: map[string]bool{"ready-repo": true}}}
	ready, pending, err := svc.GateReady(context.Background(), []string{"ready-repo", "pending-repo"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ready) != 1 || ready[0] != "ready-repo" || len(pending) != 1 || pending[0] != "pending-repo" {
		t.Fatalf("gate split = %v / %v", ready, pending)
	}
	// Catalog 未配置：全部视为就绪（第 1 期默认）。
	all := &EscalationService{}
	if ready, pending, _ = all.GateReady(context.Background(), []string{"any"}); len(ready) != 1 || len(pending) != 0 {
		t.Fatalf("nil catalog gate = %v / %v", ready, pending)
	}
}

func TestDropReposExcludesPendingFromBatches(t *testing.T) {
	got := DropRepos([][]string{{"a", "b"}, {"c"}}, map[string]bool{"b": true})
	if len(got) != 2 || got[0][0] != "a" || got[1][0] != "c" {
		t.Fatalf("DropRepos = %v", got)
	}
	if got := DropRepos([][]string{{"b"}}, map[string]bool{"b": true}); len(got) != 0 {
		t.Fatalf("empty batch must be dropped, got %v", got)
	}
}

func TestWindowTriggersOnboardingForMissing(t *testing.T) {
	pool := testdb.Open(t)
	store := NewPostgresStore(pool)
	sink := chainSink{decisionchain.NewPostgresStore(pool)}
	svc := &EscalationService{
		Store:   store,
		Sink:    sink,
		Window:  50 * time.Millisecond,
		Catalog: fakeCatalog{ready: map[string]bool{"known-repo": true}},
	}
	ctx := context.Background()

	_, project := seedOrgProject(t, pool)
	plan, err := store.CreatePlan(ctx, PlanWrite{
		ProjectID: project, RequirementText: "需求",
		RequirementKey: decisionchain.RequirementKey("需求"),
		Batches:        [][]string{{"known-repo"}}, DAG: map[string][]string{},
	})
	if err != nil {
		t.Fatal(err)
	}

	onboarded := make(chan string, 4)
	svc.OnboardMissing = func(ctx context.Context, planID, name string) { onboarded <- name }

	_, at, err := svc.ReportBlocked(ctx, BlockedReport{
		PlanID: plan.ID, ReporterID: "worker-1", Role: RoleWorker,
		Reason:               "缺 missing-repo 的实现",
		AffectedRepositories: []string{"known-repo", "missing-repo"},
		IdempotencyKey:       newUUIDv4(),
	})
	if err != nil {
		t.Fatal(err)
	}

	leaves, err := svc.MarkDeprecatedAndCollect(ctx, plan.ID, at)
	if err != nil {
		t.Fatal(err)
	}
	if len(leaves) != 1 {
		t.Fatalf("leaves = %d, want 1", len(leaves))
	}

	select {
	case name := <-onboarded:
		if name != "missing-repo" {
			t.Fatalf("onboarded = %q, want missing-repo", name)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("onboarding was not triggered for the missing repository")
	}
}

type fakeAdjacency struct {
	dependsOn, dependedBy []string
}

func (f fakeAdjacency) Neighbors(ctx context.Context, name string) ([]string, []string, error) {
	return f.dependsOn, f.dependedBy, nil
}

func TestHumanInterruptOnboardsWaitsAndRecords(t *testing.T) {
	pool := testdb.Open(t)
	store := NewPostgresStore(pool)
	sink := chainSink{decisionchain.NewPostgresStore(pool)}

	// X 未注册：OnboardMissing 触发后异步就绪（模拟扫描完成）。
	catalog := fakeCatalog{ready: map[string]bool{}}
	svc := &EscalationService{
		Store: store, Sink: sink, Catalog: catalog,
		Window: 50 * time.Millisecond, InterruptWait: 2 * time.Second,
	}
	ctx := context.Background()

	_, project := seedOrgProject(t, pool)
	plan, err := store.CreatePlan(ctx, PlanWrite{
		ProjectID: project, RequirementText: "需求",
		RequirementKey: "1b2f7d9c8a4e5f60b3c2d1e0f7a8b9c0",
		Batches:        [][]string{{"gateway"}}, DAG: map[string][]string{},
	})
	if err != nil {
		t.Fatal(err)
	}

	svc.OnboardMissing = func(ctx context.Context, planID, name string) {
		catalog.ready[name] = true // 模拟扫描完成
	}

	out, err := svc.InterruptPlanRepo(ctx, HumanInterrupt{
		PlanID: plan.ID, UserID: "kk", RepoName: "new-sdk",
		Note: "执行中发现要改的文件在新仓库", IdempotencyKey: newUUIDv4(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Onboarded || !out.Ready {
		t.Fatalf("onboard/ready = %v/%v, want true/true", out.Onboarded, out.Ready)
	}
}

func TestHumanInterruptNoImpactParksRepository(t *testing.T) {
	pool := testdb.Open(t)
	store := NewPostgresStore(pool)
	sink := chainSink{decisionchain.NewPostgresStore(pool)}
	// X 就绪（已注册）；邻接与计划仓库无交集 → 判定无影响 → 暂定不重排。
	catalog := fakeCatalog{ready: map[string]bool{"new-sdk": true}}
	svc := &EscalationService{
		Store: store, Sink: sink, Catalog: catalog,
		Window: 50 * time.Millisecond, InterruptWait: time.Second,
		Adjacency: fakeAdjacency{dependedBy: []string{"unrelated-repo"}},
	}
	ctx := context.Background()

	_, project := seedOrgProject(t, pool)
	plan, err := store.CreatePlan(ctx, PlanWrite{
		ProjectID: project, RequirementText: "独立需求",
		RequirementKey: decisionchain.RequirementKey("独立需求"),
		Batches:        [][]string{{"isolated-repo"}}, DAG: map[string][]string{},
	})
	if err != nil {
		t.Fatal(err)
	}

	out, err := svc.InterruptPlanRepo(ctx, HumanInterrupt{
		PlanID: plan.ID, UserID: "kk", RepoName: "new-sdk",
		Note: "顺手加的新仓库", IdempotencyKey: newUUIDv4(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Ready != true || out.AffectsPlan != false {
		t.Fatalf("outcome = ready %v affects %v, want ready/no-impact", out.Ready, out.AffectsPlan)
	}
	after, err := store.GetPlan(ctx, plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.ReplanState != ReplanStateNormal {
		t.Fatalf("replan_state = %q, want unchanged (no replan)", after.ReplanState)
	}
	if len(out.AffectedSet) == 0 || out.AffectedSet[0] != "new-sdk" {
		t.Fatalf("affected set = %v", out.AffectedSet)
	}
}

func TestHumanInterruptAffectingPlanOpensWindow(t *testing.T) {
	pool := testdb.Open(t)
	store := NewPostgresStore(pool)
	sink := chainSink{decisionchain.NewPostgresStore(pool)}
	// X 已就绪；其被依赖方 "gateway" 就在计划 v1 里 → 判定有改动 → 开窗重排。
	catalog := fakeCatalog{ready: map[string]bool{"new-sdk": true}}
	svc := &EscalationService{
		Store: store, Sink: sink, Catalog: catalog,
		Window: 50 * time.Millisecond, InterruptWait: time.Second,
		Adjacency: fakeAdjacency{dependedBy: []string{"gateway"}},
	}
	ctx := context.Background()

	_, project := seedOrgProject(t, pool)
	plan, err := store.CreatePlan(ctx, PlanWrite{
		ProjectID: project, RequirementText: "需求",
		RequirementKey: decisionchain.RequirementKey("需求"),
		Batches:        [][]string{{"gateway"}}, DAG: map[string][]string{},
	})
	if err != nil {
		t.Fatal(err)
	}

	out, err := svc.InterruptPlanRepo(ctx, HumanInterrupt{
		PlanID: plan.ID, UserID: "kk", RepoName: "new-sdk",
		Note: "执行中发现要改的文件在新仓库", IdempotencyKey: newUUIDv4(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Ready != true || out.AffectsPlan != true {
		t.Fatalf("outcome = ready %v affects %v, want true/true", out.Ready, out.AffectsPlan)
	}
	after, err := store.GetPlan(ctx, plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.ReplanState != ReplanStateDeprecated {
		t.Fatalf("replan_state = %q, want deprecated (window opened)", after.ReplanState)
	}
	if len(out.AffectedSet) < 2 {
		t.Fatalf("affected set = %v, want at least X + gateway", out.AffectedSet)
	}
}

func TestHumanInterruptWaitTimeoutParksRepository(t *testing.T) {
	pool := testdb.Open(t)
	store := NewPostgresStore(pool)
	sink := chainSink{decisionchain.NewPostgresStore(pool)}
	// X 永不就绪：等待超时 → 暂定（Ready=false，不重排，决策单已记录）。
	svc := &EscalationService{
		Store: store, Sink: sink,
		Window: 50 * time.Millisecond, InterruptWait: 80 * time.Millisecond,
	}
	ctx := context.Background()

	_, project := seedOrgProject(t, pool)
	plan, err := store.CreatePlan(ctx, PlanWrite{
		ProjectID: project, RequirementText: "超时需求",
		RequirementKey: decisionchain.RequirementKey("超时需求"),
		Batches:        [][]string{{"known-repo"}}, DAG: map[string][]string{},
	})
	if err != nil {
		t.Fatal(err)
	}

	out, err := svc.InterruptPlanRepo(ctx, HumanInterrupt{
		PlanID: plan.ID, UserID: "kk", RepoName: "never-ready",
		Note: "打断但 X 永不就绪", IdempotencyKey: newUUIDv4(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Ready != false || out.AffectsPlan != false {
		t.Fatalf("timeout outcome = ready %v affects %v, want false/false", out.Ready, out.AffectsPlan)
	}
}
