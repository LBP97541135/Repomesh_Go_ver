package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"repomesh.local/repomesh/internal/tasks"
	"repomesh.local/repomesh/internal/testdb"
)

// ───────────── A1(g)：重排 v2 的端到端真跑 ─────────────
//
// 走的是**真实代码路径**：发现链第 6 步的入队与收产物 → 重排端口 → tasks.Replan →
// ApplyRevision（快照替换 + 任务轴迁移）。唯一的替身是"agent 的那份产物 JSON" ——
// 那正是 agent 该写出来的东西（见 PlanningSchemaFor(PlanningReplan)），不是实现替身。

// replanStore 与 cmd/repomesh-web 的 replanAdapter 同一形状（跨域适配）。
type replanStore struct{ store *tasks.PostgresStore }

func (r replanStore) ApplyReplan(ctx context.Context, req ReplanRequest) (ReplanResult, error) {
	snapshots := make([]tasks.TaskSnapshot, 0, len(req.Tasks))
	for _, task := range req.Tasks {
		snapshots = append(snapshots, tasks.TaskSnapshot{
			TaskUID: task.TaskUID, RepositoryID: task.Repository, Title: task.Title,
			Instruction: task.Instruction, Acceptance: task.Acceptance,
		})
	}
	rev, err := r.store.Replan(ctx, tasks.ReplanCommand{
		PlanID:         req.PlanID,
		Actor:          req.Actor,
		Reason:         req.Reason,
		UpstreamRef:    req.UpstreamRef,
		IdempotencyKey: req.IdempotencyKey,
		Repositories:   req.Repositories,
		Tasks:          snapshots,
		DAG:            req.DAG,
	})
	if err != nil {
		return ReplanResult{}, err
	}
	return ReplanResult{
		ResultVersion:   rev.ResultVersion,
		CreatedTasks:    rev.CreatedTasks,
		SupersededTasks: rev.SupersededTasks,
	}, nil
}

// seedReplanFixture 造"计划 v1 已物化、执行中被打断"的最小真实聚合：
// 项目挂两个仓库 → 建 v1 计划（绑 issue）→ 写 issue 仓库范围 → 写发现链快照。
func seedReplanFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (issueID, projectID, planID string) {
	t.Helper()
	issueID = seedIssueWithoutDiscoveryState(t, ctx, pool)
	if err := pool.QueryRow(ctx,
		"SELECT project_id FROM repomesh_issues.issues WHERE id=$1", issueID).Scan(&projectID); err != nil {
		t.Fatalf("fixture project lookup: %v", err)
	}

	// 两个仓库：a 已在计划里，c 是人工打断新引入的（人确认后才进范围）。
	names := []string{"a", "c"}
	for index, name := range names {
		repositoryID := fmt.Sprintf("repo_%020d", 800000+index)
		if _, err := pool.Exec(ctx, "INSERT INTO repomesh_projects.repositories"+
			" (id, host, github_id, owner, name) VALUES ($1,'github.com',$2,'owner',$3)",
			repositoryID, 900000+index, name); err != nil {
			t.Fatalf("fixture repository: %v", err)
		}
		if _, err := pool.Exec(ctx, "INSERT INTO repomesh_projects.project_repositories"+
			" (project_id, repository_id, joined_revision, joined_at) VALUES ($1,$2,'rev-1', now())",
			projectID, repositoryID); err != nil {
			t.Fatalf("fixture project_repository: %v", err)
		}
		if _, err := pool.Exec(ctx, "INSERT INTO repomesh_issues.issue_repository_scope"+
			" (issue_id, repository_id, project_id, scope_revision) VALUES ($1,$2,$3,'scope-1')",
			issueID, repositoryID, projectID); err != nil {
			t.Fatalf("fixture scope: %v", err)
		}
	}

	store := tasks.NewPostgresStore(pool)
	plan, err := store.CreatePlan(ctx, tasks.PlanWrite{
		ProjectID:       projectID,
		RequirementText: "为报价计算增加满 900 免运费",
		RequirementKey:  "replan-e2e-" + issueID,
		Batches:         [][]string{{"owner/a"}},
		DAG:             map[string][]string{},
	})
	if err != nil {
		t.Fatalf("fixture plan: %v", err)
	}
	planID = plan.ID
	if _, err := pool.Exec(ctx, "UPDATE public.plans SET issue_id=$2 WHERE id=$1",
		planID, issueID); err != nil {
		t.Fatalf("fixture plan issue binding: %v", err)
	}
	snapshot, err := json.Marshal(map[string]any{
		"plan_id":      planID,
		"plan_version": "v1",
		"repositories": []string{"owner/a"},
		"tasks": []any{
			map[string]any{"repository": "owner/a", "title": "改运费计算", "instruction": "改 a", "acceptance": "a 单测过"},
		},
	})
	if err != nil {
		t.Fatalf("fixture snapshot encode: %v", err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO repomesh_issues.issue_discoveries"+
		" (issue_id, project_id, requirement_text, plan) VALUES ($1,$2,$3,$4::jsonb)",
		issueID, projectID, "为报价计算增加满 900 免运费", string(snapshot)); err != nil {
		t.Fatalf("fixture discovery state: %v", err)
	}
	return issueID, projectID, planID
}

func TestReplanChainEndToEnd(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	issueID, _, planID := seedReplanFixture(t, ctx, pool)
	store := tasks.NewPostgresStore(pool)
	service := New(pool).WithReplanner(replanStore{store: store})

	// 1) 人工打断之后登记重排意图（web 的 ReplanHook 做的那一步）。
	if err := service.EnqueueReplan(ctx, planID, "node-interrupt-1", []string{"owner/a", "owner/c"}); err != nil {
		t.Fatalf("EnqueueReplan: %v", err)
	}
	var (
		step    int
		role    string
		runState      string
		runContextRaw []byte
	)
	if err := pool.QueryRow(ctx, "SELECT step, role, state, context"+
		" FROM repomesh_issues.planning_runs WHERE issue_id=$1 ORDER BY created_at DESC LIMIT 1",
		issueID).Scan(&step, &role, &runState, &runContextRaw); err != nil {
		t.Fatalf("planning run read: %v", err)
	}
	if step != PlanningReplan || role != "repository_leader" || runState != "pending" {
		t.Fatalf("planning run = step %d role %q state %q", step, role, runState)
	}
	var runContext map[string]any
	if err := json.Unmarshal(runContextRaw, &runContext); err != nil {
		t.Fatalf("run context decode: %v", err)
	}
	if runContext["plan_id"] != planID || runContext["upstream_ref"] != "node-interrupt-1" {
		t.Fatalf("run context = %v", runContext)
	}
	// 派发前必须先有上一版计划可读（提示词要带它）。
	if prior, err := service.PlanSnapshot(ctx, issueID); err != nil || prior["plan_version"] != "v1" {
		t.Fatalf("prior plan = %v err %v", prior, err)
	}

	// 2) Leader 的 v2 产物回来（形状 = PlanningSchemaFor(PlanningReplan)）。
	artifact := map[string]any{
		"repositories": []any{"owner/a", "owner/c"},
		"tasks": []any{
			map[string]any{"repository": "owner/a", "title": "改运费计算", "instruction": "改 a", "acceptance": "a 单测过"},
			map[string]any{"repository": "owner/c", "title": "新增接入", "instruction": "c 接入", "acceptance": "c 联调过"},
		},
		"reason": "人工打断引入 owner/c，与 owner/a 有接口耦合",
	}
	if err := service.ApplyPlanningRun(ctx, issueID, PlanningReplan, artifact,
		PlanningProvenance{Role: "repository_leader", SkillID: "task-decomposition",
			RunID: "run_replan_e2e", AgentKind: "planning_agent"}); err != nil {
		t.Fatalf("ApplyPlanningRun(replan): %v", err)
	}

	// 3) 计划真的换代了：v2、收集窗收掉、任务带着 Leader 写的指令与验收标准。
	plan, err := store.GetPlan(ctx, planID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if plan.PlanVersion != "v2" || plan.ReplanState != tasks.ReplanStateNormal {
		t.Fatalf("plan = version %q replan_state %q", plan.PlanVersion, plan.ReplanState)
	}
	revisions, err := store.PlanRevisions(ctx, planID)
	if err != nil || len(revisions) != 1 {
		t.Fatalf("revisions = %d err %v", len(revisions), err)
	}
	if revisions[0].UpstreamRef != "node-interrupt-1" || revisions[0].Actor != "repository_leader" {
		t.Fatalf("revision provenance = %+v", revisions[0])
	}
	planTasks, err := store.ListPlanTasks(ctx, planID)
	if err != nil {
		t.Fatalf("ListPlanTasks: %v", err)
	}
	byRepo := map[string]tasks.Task{}
	for _, task := range planTasks {
		byRepo[task.RepositoryID] = task
	}
	added, ok := byRepo["owner/c"]
	if !ok {
		t.Fatalf("v2 任务没长出来：%+v", planTasks)
	}
	if added.Instruction == "" || added.Acceptance == "" {
		t.Fatalf("新任务没有指令/验收标准：%+v", added)
	}

	// 4) 发现链快照同步换代（界面读的就是它）。
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("read tx: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	state, err := service.LoadRead(ctx, tx, issueID)
	if err != nil {
		t.Fatalf("LoadRead: %v", err)
	}
	if state.Plan["plan_version"] != "v2" {
		t.Fatalf("discovery plan snapshot = %v", state.Plan)
	}
	if state.Plan["replanned_from"] != "v1" {
		t.Fatalf("replanned_from = %v", state.Plan["replanned_from"])
	}
}
