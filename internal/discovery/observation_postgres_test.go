package discovery

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"repomesh.local/repomesh/internal/observability"
	"repomesh.local/repomesh/internal/testdb"
)

func TestPostgresDiscoveryObservationKeepsSelectionInputs(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	const issue = "iss_observation_inputs"
	const project = "99999999-9999-4999-8999-999999999999"
	seedRecallFixture(t, ctx, pool, issue, "checkout payment discount", []string{"checkout", "payment"})
	service, observations := New(pool), observability.New(pool)
	before, err := observations.ListFacts(ctx, project, issue)
	if err != nil || len(before) != 0 {
		t.Fatalf("migration/collection invented old events: %+v, %v", before, err)
	}
	if _, err := service.Candidates(ctx, issue, agentID, "selection-one", 1, nil); err != nil {
		t.Fatal(err)
	}
	facts, err := observations.ListFacts(ctx, project, issue)
	if err != nil || len(facts) != 1 {
		t.Fatalf("actual candidate save missing: %+v, %v", facts, err)
	}
	var saved struct {
		SchemaVersion string `json:"schema_version"`
		EventKind     string `json:"event_kind"`
		Candidates    struct {
			Items          []map[string]any `json:"items"`
			AllScoredItems []map[string]any `json:"all_scored_items"`
			InputPool      []repoCard       `json:"input_pool"`
			RenderedCards  []string         `json:"rendered_cards"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(facts[0].Snapshot, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.SchemaVersion != observability.DiscoverySourceVersion || saved.EventKind != "discovery.state_saved" {
		t.Fatalf("missing source contract: %+v", saved)
	}
	if len(saved.Candidates.Items) != 1 || len(saved.Candidates.InputPool) != 2 || len(saved.Candidates.AllScoredItems) != 2 || len(saved.Candidates.RenderedCards) != 2 {
		t.Fatalf("limited result discarded original evidence: %+v", saved.Candidates)
	}
	for _, card := range saved.Candidates.InputPool {
		if card.ID == "" || card.ScanID == nil || card.ScanFingerprint == nil || card.ScanProfiledAt == nil || card.AutoCard == nil {
			t.Fatalf("incomplete scan provenance: %+v", card)
		}
		if card.Name == "acme/checkout" && (*card.ScanFingerprint != "fp-checkout" || !strings.Contains(cardText(card), "满减活动")) {
			t.Fatalf("did not preserve actual input: %+v", card)
		}
		if card.Name != "acme/checkout" && card.Name != "acme/shared-lib" {
			t.Fatalf("scope leaked into observation: %+v", card)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE repomesh_scan.repositories SET fingerprint='new-head',
		metadata=jsonb_set(metadata,'{recentCommits}','["new scan content"]'),profiled_at=clock_timestamp()
		WHERE id='scan-checkout'`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Candidates(ctx, issue, agentID, "selection-one", 1, nil); err != nil {
		t.Fatal(err)
	}
	afterReplay, err := observations.ListFacts(ctx, project, issue)
	if err != nil || len(afterReplay) != 1 {
		t.Fatalf("idempotent replay re-recorded selection: %+v, %v", afterReplay, err)
	}
	if _, err := service.Candidates(ctx, issue, agentID, "selection-two", 1, nil); err != nil {
		t.Fatal(err)
	}
	after, err := observations.ListFacts(ctx, project, issue)
	if err != nil || len(after) != 2 || !bytes.Equal(after[0].Snapshot, facts[0].Snapshot) {
		t.Fatalf("new scan overwrote prior decision inputs: %+v, %v", after, err)
	}
	if !bytes.Contains(after[1].Snapshot, []byte("new-head")) || !bytes.Contains(after[0].Snapshot, []byte("fp-checkout")) {
		t.Fatal("snapshot version did not match the actually consumed scan")
	}
}

func TestPostgresDiscoveryObservationRollsBackWithState(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	f := testdb.SeedProject(t, pool, "", "", "facts/rollback")
	const issue = "iss_observation_rollback"
	testdb.SeedIssue(t, pool, f, issue, "facts/rollback")
	service, observations := New(pool), observability.New(pool)
	st := &State{IssueID: issue, ProjectID: f.ID, RequirementText: "actual request", Analysis: map[string]any{"state": "A"}}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err := service.save(ctx, tx, st); err != nil {
		t.Fatal(err)
	}
	var inTransaction int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM repomesh_observability.facts WHERE issue_id=$1`, issue).Scan(&inTransaction); err != nil || inTransaction != 1 {
		t.Fatalf("fact not part of business transaction: count=%d err=%v", inTransaction, err)
	}
	if facts, err := observations.ListFacts(ctx, f.ID, issue); err != nil || len(facts) != 0 {
		t.Fatalf("uncommitted business evidence exposed: %+v %v", facts, err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var stateCount, factCount int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM repomesh_issues.issue_discoveries WHERE issue_id=$1),
		(SELECT count(*) FROM repomesh_observability.facts WHERE issue_id=$1)`, issue).Scan(&stateCount, &factCount); err != nil || stateCount != 0 || factCount != 0 {
		t.Fatalf("rollback left partial evidence: state=%d facts=%d err=%v", stateCount, factCount, err)
	}
	commitSave := func() {
		t.Helper()
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if err := service.save(ctx, tx, st); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	commitSave()
	st.Idempotency = map[string]any{"new-receipt": "does not change the decision"}
	commitSave()
	if facts, err := observations.ListFacts(ctx, f.ID, issue); err != nil || len(facts) != 1 {
		t.Fatalf("receipt-only save duplicated unchanged fact: %+v %v", facts, err)
	}
}

func TestPostgresDiscoveryObservationSupportsAuthorizedPurgeCascade(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	f := testdb.SeedProject(t, pool, "", "", "facts/purge")
	// The existing purge audit uses a UUID aggregate identity.
	const issue = "43ab51db-a35c-47e8-9e57-dfb2d22328a1"
	testdb.SeedIssue(t, pool, f, issue, "facts/purge")
	if _, err := New(pool).Analysis(ctx, issue, agentID, "analysis", nil, false); err != nil {
		t.Fatal(err)
	}
	if facts, err := observability.New(pool).ListFacts(ctx, f.ID, issue); err != nil || len(facts) != 1 {
		t.Fatalf("setup did not create provenance: %+v %v", facts, err)
	}
	if _, err := NewMaintenance(pool).Archive(ctx, issue); err != nil {
		t.Fatal(err)
	}
	// Verify only the newly added FK/trigger contract, using the same flag as
	// Maintenance.Purge. Roll back this partial aggregate deletion: complete
	// purge ordering is an existing, separate business concern.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SET LOCAL repomesh.purge_mode='on'`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM repomesh_issues.issues WHERE id=$1`, issue); err != nil {
		t.Fatalf("observation history blocked authorized cascade: %v", err)
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM repomesh_observability.facts WHERE issue_id=$1`, issue).Scan(&count); err != nil || count != 0 {
		t.Fatalf("cascade left private historical snapshot: count=%d err=%v", count, err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if facts, err := observability.New(pool).ListFacts(ctx, f.ID, issue); err != nil || len(facts) != 1 {
		t.Fatalf("rolled-back purge lost evidence: %+v %v", facts, err)
	}
}

func TestPostgresDiscoveryObservationConcurrentPlanSaves(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	f := testdb.SeedProject(t, pool, "", "", "facts/concurrent")
	const issue = "iss_observation_concurrent_plans"
	testdb.SeedIssue(t, pool, f, issue, "facts/concurrent")
	service := New(pool)
	type pending struct {
		tx     pgx.Tx
		planID string
	}
	plans := make([]pending, 0, 2)
	for i := 0; i < 2; i++ {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(context.Background())
		var planID string
		// Plan writes its plan row before save. The Issue foreign key takes a
		// KEY SHARE lock in each transaction; serializing observation history
		// must not upgrade both of those locks to conflicting FOR UPDATE locks.
		if err := tx.QueryRow(ctx, `INSERT INTO public.plans
			(id,project_id,plan_version,requirement_text,specs,task_dag,execution_batches,revisions,issue_id)
			VALUES(gen_random_uuid(),$1,'v1','concurrent plan','{}','{}','[]','[]',$2)
			RETURNING id::text`, f.ID, issue).Scan(&planID); err != nil {
			t.Fatal(err)
		}
		plans = append(plans, pending{tx: tx, planID: planID})
	}
	start := make(chan struct{})
	results := make(chan error, len(plans))
	for _, plan := range plans {
		go func(plan pending) {
			<-start
			err := service.save(ctx, plan.tx, &State{
				IssueID: issue, ProjectID: f.ID, RequirementText: "concurrent plan",
				Plan: map[string]any{"plan_id": plan.planID},
			})
			if err == nil {
				err = plan.tx.Commit(ctx)
			} else {
				_ = plan.tx.Rollback(context.Background())
			}
			results <- err
		}(plan)
	}
	close(start)
	for range plans {
		if err := <-results; err != nil {
			t.Errorf("observation save failed for concurrent plan: %v", err)
		}
	}
	if t.Failed() {
		return
	}
	facts, err := observability.New(pool).ListFacts(ctx, f.ID, issue)
	if err != nil || len(facts) != len(plans) {
		t.Fatalf("concurrent plan facts missing: %+v %v", facts, err)
	}
	seen := map[string]bool{}
	for _, fact := range facts {
		var saved struct {
			Plan struct {
				ID string `json:"plan_id"`
			} `json:"plan"`
		}
		if err := json.Unmarshal(fact.Snapshot, &saved); err != nil {
			t.Fatal(err)
		}
		seen[saved.Plan.ID] = true
	}
	for _, plan := range plans {
		if !seen[plan.planID] {
			t.Fatalf("committed plan %s lost its historical snapshot", plan.planID)
		}
	}
}
