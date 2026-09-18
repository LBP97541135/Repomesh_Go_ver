package branchvalidation

import (
	"context"
	"encoding/json"
	"fmt"
)

// GetByIdempotencyKey returns the run created for a key (replay path).
func (s *Service) GetByIdempotencyKey(ctx context.Context, key string) (RunView, error) {
	var runID string
	if err := s.pool.QueryRow(ctx, `SELECT id FROM public.database_branch_validations
		WHERE idempotency_key=$1`, key).Scan(&runID); err != nil {
		return RunView{}, fmt.Errorf("branchvalidation: original run lookup failed: %w", err)
	}
	return s.Get(ctx, runID)
}

// Get returns the run projection.
func (s *Service) Get(ctx context.Context, runID string) (RunView, error) {
	var view RunView
	var results []byte
	var failureCode *string
	var branchRef string
	err := s.pool.QueryRow(ctx, `SELECT id, repository_id, candidate_sha, provider,
		COALESCE(provider_branch_ref,''), status, failure_code, cleanup_pending, results
		FROM public.database_branch_validations WHERE id=$1`, runID).Scan(
		&view.ID, &view.RepositoryID, &view.CandidateSHA, &view.Provider,
		&branchRef, &view.Status, &failureCode, &view.CleanupPending, &results)
	if err != nil {
		return RunView{}, fmt.Errorf("branchvalidation: run lookup failed: %w", err)
	}
	view.BranchRef = branchRef
	if failureCode != nil {
		view.FailureCode = *failureCode
	}
	view.MigrationResults = []MigrationResult{}
	if len(results) > 0 {
		var parsed struct {
			Results []MigrationResult `json:"results"`
		}
		if json.Unmarshal(results, &parsed) == nil {
			view.MigrationResults = parsed.Results
		}
	}
	return view, nil
}
