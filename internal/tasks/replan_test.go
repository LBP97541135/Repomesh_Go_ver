package tasks

import (
	"context"
	"errors"
	"testing"

	"repomesh.local/repomesh/internal/testdb"
)

// DeriveBatches 的读法与 ValidateBatches 一致：dag[仓库] = 它依赖的仓库，
// 被依赖者必须排在同一批或更早的批。
func TestDeriveBatchesLayersByDependency(t *testing.T) {
	batches, cycles := DeriveBatches([]string{"web", "sdk", "gateway"},
		map[string][]string{"gateway": {"sdk"}, "web": {"gateway"}})
	if len(cycles) != 0 {
		t.Fatalf("unexpected cycles: %v", cycles)
	}
	if len(batches) != 3 {
		t.Fatalf("batches = %v, want 3 layers", batches)
	}
	pos := map[string]int{}
	for i, batch := range batches {
		for _, repo := range batch {
			pos[repo] = i
		}
	}
	if !(pos["sdk"] < pos["gateway"] && pos["gateway"] < pos["web"]) {
		t.Fatalf("layer order wrong: %v", batches)
	}
	if err := ValidateBatches(batches, map[string][]string{"gateway": {"sdk"}, "web": {"gateway"}}); err != nil {
		t.Fatalf("derived batches must satisfy the mechanical gate: %v", err)
	}
}

// 成环不猜：环成员按名字整批落在最后并原样回报，调用方据此拒绝落库。
func TestDeriveBatchesReportsCycles(t *testing.T) {
	batches, cycles := DeriveBatches([]string{"a", "b", "c"},
		map[string][]string{"a": {"b"}, "b": {"a"}})
	if len(cycles) != 2 || cycles[0] != "a" || cycles[1] != "b" {
		t.Fatalf("cycles = %v, want [a b]", cycles)
	}
	if len(batches) != 2 || len(batches[0]) != 1 || batches[0][0] != "c" {
		t.Fatalf("batches = %v, want c first", batches)
	}
}

// Replan 拒绝"批次里的仓库没有任务"：快照替换会把没被认领的 v1 任务置
// superseded，少一个仓库的任务就等于把那个仓库从计划里悄悄删掉。
func TestReplanRejectsRepositoryWithoutTask(t *testing.T) {
	pool := testdb.Open(t)
	store := NewPostgresStore(pool)
	plan := newPlan(t, pool, store, "replan-reject-key")
	_, err := store.Replan(context.Background(), ReplanCommand{
		PlanID:         plan.ID,
		IdempotencyKey: "replan-reject-1",
		Repositories:   []string{"gateway", "sdk"},
		Tasks:          []TaskSnapshot{{TaskUID: "gateway-fix", RepositoryID: "gateway", Title: "网关改造"}},
		DAG:            map[string][]string{"sdk": {"gateway"}},
	})
	if !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("err = %v, want ErrInvalidPlan", err)
	}
}

// Replan 落 v2：新任务带着 Leader 写的指令与验收标准进库，计划换代后收窗。
func TestPostgresReplanLandsV2WithTaskInstructions(t *testing.T) {
	pool := testdb.Open(t)
	store := NewPostgresStore(pool)
	ctx := context.Background()
	plan := newPlan(t, pool, store, "replan-land-key")

	// 收集窗开着（人工打断把计划置成 deprecated），重排落库后应当收窗。
	if err := store.MarkDeprecated(ctx, plan.ID); err != nil {
		t.Fatal(err)
	}
	revision, err := store.Replan(ctx, ReplanCommand{
		PlanID:         plan.ID,
		Actor:          "repository_leader",
		Reason:         "人工打断引入 third，gateway 的接口变更连带 sdk",
		UpstreamRef:    "node-interrupt-1",
		IdempotencyKey: "replan-land-1",
		Repositories:   []string{"gateway", "sdk", "third"},
		Tasks: []TaskSnapshot{
			{TaskUID: "gateway-fix", RepositoryID: "gateway", Title: "网关改造",
				Instruction: "在 gateway 暴露满减接口", Acceptance: "接口单测通过"},
			{TaskUID: "sdk-bump", RepositoryID: "sdk", Title: "SDK 升版",
				Instruction: "sdk 跟随网关接口升版", Acceptance: "sdk 单测通过"},
			{TaskUID: "third-migrate", RepositoryID: "third", Title: "新增接入",
				Instruction: "third 接入网关新接口", Acceptance: "third 联调通过"},
		},
		DAG: map[string][]string{"third": {"gateway"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if revision.ResultVersion != "v2" || revision.BaseVersion != "v1" {
		t.Fatalf("revision = %+v, want v1→v2", revision)
	}
	if revision.CreatedTasks != 3 {
		t.Fatalf("created = %d, want 3", revision.CreatedTasks)
	}

	after, err := store.GetPlan(ctx, plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.PlanVersion != "v2" {
		t.Fatalf("plan version = %q, want v2", after.PlanVersion)
	}
	if after.ReplanState != ReplanStateNormal {
		t.Fatalf("replan_state = %q, want window closed", after.ReplanState)
	}
	// 批次按依赖分层：third 依赖 gateway，必须在更晚的批。
	pos := map[string]int{}
	for i, batch := range after.Batches {
		for _, repo := range batch {
			pos[repo] = i
		}
	}
	if !(pos["gateway"] < pos["third"]) {
		t.Fatalf("batches = %v, want gateway before third", after.Batches)
	}

	// 指令与验收标准**真的进了 tasks 行**（此前 TaskSnapshot 只带标题，
	// v2 新长出来的任务拿到的是空指令）。
	tasks, err := store.ListPlanTasks(ctx, plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	byUID := map[string]Task{}
	for _, task := range tasks {
		byUID[task.TaskUID] = task
	}
	third, ok := byUID["third-migrate"]
	if !ok {
		t.Fatalf("v2 task missing: %+v", tasks)
	}
	if third.Instruction == "" || third.Acceptance == "" {
		t.Fatalf("task instruction/acceptance not migrated: %+v", third)
	}
	if third.RepositoryID != "third" {
		t.Fatalf("task repository = %q, want third", third.RepositoryID)
	}

	// 幂等重放：同键同含义返回原记录，版本不动。
	replay, err := store.Replan(ctx, ReplanCommand{
		PlanID:         plan.ID,
		Actor:          "repository_leader",
		Reason:         "人工打断引入 third，gateway 的接口变更连带 sdk",
		UpstreamRef:    "node-interrupt-1",
		IdempotencyKey: "replan-land-1",
		Repositories:   []string{"gateway", "sdk", "third"},
		Tasks: []TaskSnapshot{
			{TaskUID: "gateway-fix", RepositoryID: "gateway", Title: "网关改造",
				Instruction: "在 gateway 暴露满减接口", Acceptance: "接口单测通过"},
			{TaskUID: "sdk-bump", RepositoryID: "sdk", Title: "SDK 升版",
				Instruction: "sdk 跟随网关接口升版", Acceptance: "sdk 单测通过"},
			{TaskUID: "third-migrate", RepositoryID: "third", Title: "新增接入",
				Instruction: "third 接入网关新接口", Acceptance: "third 联调通过"},
		},
		DAG: map[string][]string{"third": {"gateway"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if replay.ResultVersion != "v2" || replay.Revision != revision.Revision {
		t.Fatalf("replay = %+v, want same revision", replay)
	}
}
