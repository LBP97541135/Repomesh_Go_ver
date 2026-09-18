package decisionchain

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"repomesh.local/repomesh/internal/testdb"
)

func TestPostgresRecordIdempotentAndVersioned(t *testing.T) {
	pool := testdb.Open(t)
	store := NewPostgresStore(pool)
	ctx := context.Background()

	base := nodeWrite{
		RequirementText:      "为项目选择合适的仓库",
		RequirementKey:       RequirementKey("为项目选择合适的仓库"),
		Step:                 StepConfirmation,
		Status:               StatusConfirmed,
		ActorID:              "xiaochen",
		Action:               "范围圈定确认",
		Rationale:            "为项目选择合适的仓库",
		ContextRef:           map[string]any{"sourceIds": []string{"card-1"}},
		AffectedRepositories: []string{"ts-common", "ts-api"},
		Source:               SourceEvent,
	}

	first := base
	first.EventID = "evt-1"
	stored, err := store.Record(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ID == "" || stored.Version != 1 || stored.ActorType != "human" {
		t.Fatalf("unexpected first node: %+v", stored)
	}
	if len(stored.AffectedRepositories) != 2 || stored.ContextRef["sourceIds"] == nil {
		t.Fatalf("json columns lost in round trip: %+v", stored)
	}

	// Idempotent redelivery: same event_id returns the stored row untouched.
	again := base
	again.EventID = "evt-1"
	again.Status = StatusRejected
	reread, err := store.Record(ctx, again)
	if err != nil {
		t.Fatal(err)
	}
	if reread.ID != stored.ID || reread.Status != StatusConfirmed {
		t.Fatalf("redelivery must return the original row: %+v", reread)
	}

	// Same requirement, new event: version bumps to 2.
	second := base
	second.EventID = "evt-2"
	v2, err := store.Record(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	if v2.Version != 2 {
		t.Fatalf("second event version = %d, want 2", v2.Version)
	}
}

func TestPostgresGetList(t *testing.T) {
	pool := testdb.Open(t)
	store := NewPostgresStore(pool)
	ctx := context.Background()

	key := RequirementKey("列表检索用需求")
	w := nodeWrite{
		EventID: "evt-list-1", RequirementText: "列表检索用需求", RequirementKey: key,
		Step: StepConfirmation, Status: StatusConfirmed, ActorID: "u1",
		Action: "范围圈定确认", Rationale: "r", ContextRef: map[string]any{},
		AffectedRepositories: []string{"repo-a", "repo-b"}, Source: SourceEvent,
	}
	node, err := store.Record(ctx, w)
	if err != nil {
		t.Fatal(err)
	}

	got, err := store.Get(ctx, node.ID)
	if err != nil || got == nil || got.ID != node.ID {
		t.Fatalf("Get failed: %v", err)
	}
	if _, err := store.Get(ctx, NewUUIDv4()); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("missing id = %v, want ErrNoRows", err)
	}

	byRepo, err := store.List(ctx, Filter{Repository: "repo-b", Limit: 10})
	if err != nil || len(byRepo) != 1 {
		t.Fatalf("filter by repository: %d hits, err %v", len(byRepo), err)
	}
	none, err := store.List(ctx, Filter{Repository: "repo-zzz", Limit: 10})
	if err != nil || len(none) != 0 {
		t.Fatalf("unmatched repository filter: %d hits, err %v", len(none), err)
	}
	byKey, err := store.List(ctx, Filter{RequirementKey: key})
	if err != nil || len(byKey) != 1 {
		t.Fatalf("filter by key: %d hits, err %v", len(byKey), err)
	}
}

func TestPostgresFeatureSettings(t *testing.T) {
	pool := testdb.Open(t)
	store := NewPostgresStore(pool)
	ctx := context.Background()

	// Seeded row is on; an unknown feature counts as on (D12).
	on, err := store.FeatureEnabled(ctx, "decision_chain")
	if err != nil || !on {
		t.Fatalf("seeded toggle = %v, err %v", on, err)
	}
	if on, _ := store.FeatureEnabled(ctx, "no_such_feature"); !on {
		t.Fatal("missing feature row must count as enabled")
	}

	if err := store.SetFeature(ctx, "decision_chain", false, "xiaochen", time.Now()); err != nil {
		t.Fatal(err)
	}
	if on, _ := store.FeatureEnabled(ctx, "decision_chain"); on {
		t.Fatal("toggle off did not persist")
	}
	if err := store.SetFeature(ctx, "decision_chain", true, "xiaochen", time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresEmbeddingAndRecall(t *testing.T) {
	pool := testdb.Open(t)
	store := NewPostgresStore(pool)
	ctx := context.Background()

	node, err := store.Record(ctx, nodeWrite{
		EventID: "evt-emb-1", RequirementText: "向量检索用需求",
		RequirementKey: RequirementKey("向量检索用需求"),
		Step:           StepConfirmation, Status: StatusConfirmed, ActorID: "u1",
		Action: "范围圈定确认", Rationale: "r", ContextRef: map[string]any{},
		AffectedRepositories: []string{"repo-vec"}, Source: SourceEvent,
	})
	if err != nil {
		t.Fatal(err)
	}

	pending, err := store.PendingEmbeddings(ctx, "m1", 10)
	if err != nil || len(pending) != 1 || pending[0].ID != node.ID {
		t.Fatalf("pending before embed: %d hits, err %v", len(pending), err)
	}

	vec := make([]float32, 1024)
	vec[0] = 1
	if err := store.UpsertEmbedding(ctx, node.ID, "m1", vec, time.Now()); err != nil {
		t.Fatalf("upsert embedding: %v", err)
	}
	if pending, _ = store.PendingEmbeddings(ctx, "m1", 10); len(pending) != 0 {
		t.Fatalf("pending after embed with current model: %d", len(pending))
	}
	// Model mismatch re-surfaces the node for re-embedding (F5).
	if pending, _ = store.PendingEmbeddings(ctx, "m2", 10); len(pending) != 1 {
		t.Fatal("model mismatch must resurface the node")
	}

	hits, err := store.SemanticCandidates(ctx, "m1", vec, 5)
	if err != nil || len(hits) != 1 || hits[0].Node.ID != node.ID {
		t.Fatalf("semantic candidates: %d hits, err %v", len(hits), err)
	}
	if hits[0].Score < 0.999 {
		t.Fatalf("identical vector score = %f, want ~1", hits[0].Score)
	}
	if hits, _ = store.SemanticCandidates(ctx, "m2", vec, 5); len(hits) != 0 {
		t.Fatal("model filter must hide other-model vectors")
	}

	structural, err := store.StructuralCandidates(ctx, []string{"repo-vec"}, 5)
	if err != nil || len(structural) != 1 || structural[0].ID != node.ID {
		t.Fatalf("structural candidates: %d hits, err %v", len(structural), err)
	}
	if structural, _ = store.StructuralCandidates(ctx, []string{"no-such-repo"}, 5); len(structural) != 0 {
		t.Fatal("unmatched repository must not hit")
	}

	// Re-embed: same row replaced in place, not duplicated.
	vec2 := make([]float32, 1024)
	vec2[1] = 1
	if err := store.UpsertEmbedding(ctx, node.ID, "m1", vec2, time.Now()); err != nil {
		t.Fatal(err)
	}
	hits, err = store.SemanticCandidates(ctx, "m1", vec2, 5)
	if err != nil || len(hits) != 1 || hits[0].Score < 0.999 {
		t.Fatalf("re-embed should replace in place: %d hits, err %v", len(hits), err)
	}
}

func TestPostgresConcurrentConfirmationsGetDistinctVersions(t *testing.T) {
	pool := testdb.Open(t)
	store := NewPostgresStore(pool)
	ctx := context.Background()

	const writers = 8
	key := RequirementKey("并发圈定需求")
	var wg sync.WaitGroup
	versions := make(chan int, writers)
	errs := make(chan error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			node, err := store.Record(ctx, nodeWrite{
				EventID: fmt.Sprintf("evt-c-%d", i), RequirementText: key,
				RequirementKey: key, Step: StepConfirmation, Status: StatusConfirmed,
				ActorID: "u", Action: "范围圈定确认", Rationale: "并发",
				ContextRef: map[string]any{}, AffectedRepositories: []string{},
				Source: SourceEvent,
			})
			if err != nil {
				errs <- err
				return
			}
			versions <- node.Version
		}(i)
	}
	wg.Wait()
	close(errs)
	if err := <-errs; err != nil {
		t.Fatalf("concurrent record failed: %v", err)
	}
	close(versions)
	seen := map[int]bool{}
	for v := range versions {
		if v < 1 {
			t.Fatalf("bad version %d", v)
		}
		if seen[v] {
			t.Fatalf("duplicate version %d", v)
		}
		seen[v] = true
	}
	if len(seen) != writers {
		t.Fatalf("got %d distinct versions, want %d", len(seen), writers)
	}
}

func TestRecordPlanRevisedInTx(t *testing.T) {
	pool := testdb.Open(t)
	store := NewPostgresStore(pool)
	ctx := context.Background()

	// 触发源：一条 BLOCKED 上报节点。
	blocked, err := store.Record(ctx, nodeWrite{
		EventID: "dec-block-t3", RequirementText: "网关改造需求",
		RequirementKey: RequirementKey("网关改造需求"),
		Step:           StepConfirmation, Status: StatusBlocked, ActorType: "human",
		ActorID: "tm-1", Action: "范围圈定确认", Rationale: "发现需要连带 SDK",
		ContextRef:           map[string]any{},
		AffectedRepositories: []string{"gateway"}, Source: SourceEvent,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 同事务落 adjusted 修订节点，upstream 指 BLOCKED。
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	adjusted, err := store.RecordPlanRevisedInTx(ctx, tx, Event{
		Requirement: "网关改造需求", Actor: "leader", ActorType: "llm",
		IdempotencyKey: "dec-adj-t3", RepositoryIDs: []string{"gateway", "sdk"},
		Accepted: true, Status: StatusAdjusted, UpstreamRef: blocked.ID,
	})
	if err != nil {
		tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	got, err := store.Get(ctx, adjusted.ID)
	if err != nil || got == nil {
		t.Fatalf("adjusted node missing: %v", err)
	}
	if got.Status != StatusAdjusted || got.ParentNodeID != blocked.ID || got.ActorType != "llm" {
		t.Fatalf("adjusted node = %+v", got)
	}
	if len(got.AffectedRepositories) != 2 {
		t.Fatalf("repos = %v", got.AffectedRepositories)
	}

	// 回滚验证：事务内落、回滚后不存在的节点确实不存在。
	tx2, _ := pool.Begin(ctx)
	if _, err := store.RecordPlanRevisedInTx(ctx, tx2, Event{
		Requirement: "网关改造需求", Actor: "leader",
		IdempotencyKey: "dec-adj-rollback", Accepted: true, Status: StatusAdjusted,
	}); err != nil {
		tx2.Rollback(ctx)
		t.Fatal(err)
	}
	tx2.Rollback(ctx)
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM public.decision_chain_nodes WHERE event_id = 'dec-adj-rollback'`,
	).Scan(&n); err != nil || n != 0 {
		t.Fatalf("rolled-back node visible: n=%d err=%v", n, err)
	}
}
