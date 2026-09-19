package tasks

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// validatePlanScope covers both initial plans and later full-snapshot revisions.
// An issue-bound plan remains inside that issue even when the project grows.
func validatePlanScope(ctx context.Context, tx pgx.Tx, projectID, planID string, names []string) error {
	var issueID *string
	if planID != "" {
		if err := tx.QueryRow(ctx, `SELECT issue_id FROM public.plans WHERE id=$1 AND project_id::text=$2`, planID, projectID).Scan(&issueID); err != nil {
			return fmt.Errorf("%w: plan outside project", ErrInvalidPlan)
		}
	}
	rows, err := tx.Query(ctx, `SELECT r.id, r.owner || '/' || r.name, r.name
		FROM repomesh_projects.project_repositories pr
		JOIN repomesh_projects.repositories r ON r.id=pr.repository_id
		WHERE pr.project_id=$1 AND ($2::text IS NULL OR EXISTS (
		 SELECT 1 FROM repomesh_issues.issue_repository_scope scope
		 WHERE scope.project_id=pr.project_id AND scope.repository_id=r.id AND scope.issue_id=$2))
		FOR KEY SHARE OF pr`, projectID, issueID)
	if err != nil {
		return err
	}
	counts := map[string]int{}
	for rows.Next() {
		var id, fullName, name string
		if err := rows.Scan(&id, &fullName, &name); err != nil {
			rows.Close()
			return err
		}
		for _, alias := range []string{id, fullName, name} {
			counts[alias]++
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, name := range names {
		if counts[name] != 1 {
			return fmt.Errorf("%w: repository outside confirmed scope or ambiguous", ErrInvalidPlan)
		}
	}
	return nil
}

func batchRepositories(batches [][]string) []string {
	names := []string{}
	for _, batch := range batches {
		names = append(names, batch...)
	}
	return names
}
