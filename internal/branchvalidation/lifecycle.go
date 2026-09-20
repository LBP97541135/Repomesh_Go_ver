package branchvalidation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Start runs the full lifecycle for one candidate: persist the run (idempotent
// by IdempotencyKey), provision the branch, apply migrations, record evidence,
// then clean up. Failure at any stage is persisted with a failure code; a
// cleanup failure sets cleanup_pending and RetryCleanup can finish it later.
func (s *Service) Start(ctx context.Context, command StartCommand) (RunView, error) {
	if command.IdempotencyKey == "" || command.ProjectID == "" || command.RepositoryID == "" {
		return RunView{}, fmt.Errorf("branchvalidation: idempotency key, project and repository are required")
	}
	// 组织从**项目**反查，不要求调用方传。
	//
	// 2026-09-20 线上实测：`database_branch_validations.organization_id` 是
	// NOT NULL uuid，而 INSERT 写的是 `$2::uuid`；路由（pipeline_routes2.go）只填
	// ProjectID、**从不填 OrganizationID**，于是任何调用方（包括控制台）都会撞
	//   ERROR: invalid input syntax for type uuid: ""  (SQLSTATE 22P02)
	// —— 表里因此一行都落不下来。这不是"没人用"，是**这条路根本走不通**。
	// 项目自己带着组织，服务端能查，就不该把这件事推给调用方。
	if command.OrganizationID == "" {
		if err := s.pool.QueryRow(ctx,
			`SELECT COALESCE(organization_id::text,'') FROM repomesh_projects.projects WHERE id=$1::uuid`,
			command.ProjectID).Scan(&command.OrganizationID); err != nil {
			return RunView{}, fmt.Errorf("branchvalidation: 项目组织反查失败：%w", err)
		}
	}
	if command.OrganizationID == "" {
		return RunView{}, fmt.Errorf("branchvalidation: 项目 %s 没有 organization_id，无法落这条验证记录", command.ProjectID)
	}
	runID, err := newID("dbv-")
	if err != nil {
		return RunView{}, err
	}
	// Idempotent insert: a repeated key returns the original run untouched.
	var created bool
	err = s.pool.QueryRow(ctx, `INSERT INTO public.database_branch_validations
		(id, organization_id, project_id, repository_id, task_id, candidate_sha, source_database_ref,
		 provider, idempotency_key, request_hash, status)
		VALUES ($1,$2::uuid,$3::uuid,$4,NULLIF($5,'')::uuid,$6,$7,$8,$9,$10,'provisioning')
		ON CONFLICT (idempotency_key) DO NOTHING RETURNING true`,
		runID, command.OrganizationID, command.ProjectID, command.RepositoryID, command.TaskID,
		command.CandidateSHA, command.SourceDatabaseRef, s.providerName(), command.IdempotencyKey,
		requestHash(command)).Scan(&created)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Genuine conflict: the key already exists; return the original run.
			existing, getErr := s.GetByIdempotencyKey(ctx, command.IdempotencyKey)
			if getErr != nil {
				return RunView{}, getErr
			}
			return existing, nil
		}
		// Real insert failure: surface it, never dress it up as a conflict.
		return RunView{}, fmt.Errorf("branchvalidation: run insert failed: %w", err)
	}
	if !created {
		existing, getErr := s.GetByIdempotencyKey(ctx, command.IdempotencyKey)
		if getErr != nil {
			return RunView{}, getErr
		}
		return existing, nil
	}
	if s.provider == nil {
		return s.fail(runID, "PROVIDER_UNAVAILABLE",
			"no branch provider configured in this deployment"), nil
	}
	branchRef, err := s.provider.ProvisionBranch(ctx, command.SourceDatabaseRef, command.CandidateSHA)
	if err != nil {
		return s.failWithCleanup(runID, branchRef, "PROVISIONING_FAILED", err.Error()), nil
	}
	if _, err := s.pool.Exec(ctx, `UPDATE public.database_branch_validations
		SET provider_branch_ref=$2, status='validating' WHERE id=$1 AND status='provisioning'`,
		runID, branchRef); err != nil {
		return RunView{}, unavailable()
	}
	results, applyErr := s.provider.ApplyMigrations(ctx, branchRef, command.Migrations)
	status := "passed"
	failureCode := ""
	if applyErr != nil {
		status = "failed"
		failureCode = "MIGRATION_FAILED: " + applyErr.Error()
	}
	resultsJSON, _ := json.Marshal(results)
	if _, err := s.pool.Exec(ctx, `UPDATE public.database_branch_validations
		SET status=$2, failure_code=NULLIF($3,''), results=$4::jsonb WHERE id=$1`,
		runID, status, failureCode, resultsJSON); err != nil {
		return RunView{}, unavailable()
	}
	cleanupErr := s.provider.CleanupBranch(ctx, branchRef)
	if cleanupErr != nil {
		if _, err := s.pool.Exec(ctx, `UPDATE public.database_branch_validations
			SET cleanup_pending=true WHERE id=$1`, runID); err != nil {
			return RunView{}, unavailable()
		}
	}
	return s.Get(ctx, runID)
}

// RetryCleanup finishes the cleanup of a run whose branch teardown failed.
// The original validation result is never recomputed or overwritten.
func (s *Service) RetryCleanup(ctx context.Context, runID string) (RunView, error) {
	view, err := s.Get(ctx, runID)
	if err != nil {
		return RunView{}, err
	}
	if !view.CleanupPending {
		return view, nil
	}
	if s.provider == nil {
		return view, fmt.Errorf("branchvalidation: no provider configured")
	}
	if err := s.provider.CleanupBranch(ctx, view.BranchRef); err != nil {
		return view, err
	}
	if _, err := s.pool.Exec(ctx, `UPDATE public.database_branch_validations
		SET cleanup_pending=false WHERE id=$1`, runID); err != nil {
		return RunView{}, unavailable()
	}
	return s.Get(ctx, runID)
}

func (s *Service) fail(runID, code, reason string) RunView {
	_, _ = s.pool.Exec(context.Background(), `UPDATE public.database_branch_validations
		SET status='failed', failure_code=$2, results=$3::jsonb WHERE id=$1`,
		runID, code, mustJSON(map[string]any{"reason": reason}))
	view, err := s.Get(context.Background(), runID)
	if err != nil {
		return RunView{ID: runID, Status: "failed", FailureCode: code}
	}
	return view
}

func (s *Service) failWithCleanup(runID, branchRef, code, reason string) RunView {
	if branchRef != "" && s.provider != nil {
		if cleanupErr := s.provider.CleanupBranch(context.Background(), branchRef); cleanupErr != nil {
			_, _ = s.pool.Exec(context.Background(), `UPDATE public.database_branch_validations
				SET cleanup_pending=true WHERE id=$1`, runID)
		}
	}
	_, _ = s.pool.Exec(context.Background(), `UPDATE public.database_branch_validations
		SET status='failed', failure_code=$2, results=$3::jsonb WHERE id=$1`,
		runID, code, mustJSON(map[string]any{"reason": reason}))
	view, err := s.Get(context.Background(), runID)
	if err != nil {
		return RunView{ID: runID, Status: "failed", FailureCode: code}
	}
	return view
}

func (s *Service) providerName() string {
	if s.provider == nil {
		return "none"
	}
	if named, ok := s.provider.(interface{ Name() string }); ok {
		return named.Name()
	}
	return "custom"
}

func requestHash(command StartCommand) string {
	encoded, _ := json.Marshal(map[string]any{
		"repository": command.RepositoryID, "sha": command.CandidateSHA,
		"source": command.SourceDatabaseRef, "migrations": len(command.Migrations),
	})
	return string(encoded)
}

func mustJSON(value any) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		return []byte("{}")
	}
	return encoded
}

func unavailable() error {
	return fmt.Errorf("branchvalidation: database unavailable")
}
