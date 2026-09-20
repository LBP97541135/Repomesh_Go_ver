package humancontrol

import (
	"context"
	"errors"
	"testing"

	"repomesh.local/repomesh/internal/testdb"
)

func TestPostgresDecisionRejectsEvidenceChangedSinceRead(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	actor := seedPolicyAccount(t, pool, 885501)
	project := seedPolicyProject(t, pool, actor)
	service := New(pool)
	review, err := service.Request(ctx, RequestCommand{ProjectID: project, Checkpoint: "test-evidence", EvidenceVersion: "candidate-v1", Title: "Review frozen candidate", Assignee: actor})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE public.review_requests SET evidence_version='candidate-v2' WHERE id=$1`, review.ID); err != nil {
		t.Fatal(err)
	}
	_, err = service.Decide(ctx, actor, project, DecisionCommand{ReviewRequestID: review.ID, ExpectedEvidenceVersion: "candidate-v1", Decision: "approved", Reason: "Saw v1"})
	if !errors.Is(err, ErrEvidenceDrifted) {
		t.Fatalf("stale evidence accepted: %v", err)
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM public.checkpoint_decisions WHERE review_request_id=$1`, review.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("stale approval left a decision")
	}
	decision, err := service.Decide(ctx, actor, project, DecisionCommand{ReviewRequestID: review.ID, ExpectedEvidenceVersion: "candidate-v2", Decision: "changes_requested", Reason: "Review v2"})
	if err != nil || decision.EvidenceVersion != "candidate-v2" {
		t.Fatalf("fresh decision failed: %v", err)
	}
	_, err = service.Decide(ctx, actor, project, DecisionCommand{ReviewRequestID: review.ID, ExpectedEvidenceVersion: "candidate-v2", Decision: "approved"})
	if !errors.Is(err, ErrEvidenceDrifted) {
		t.Fatal("double decision accepted")
	}
}
