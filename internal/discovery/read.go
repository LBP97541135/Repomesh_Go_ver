package discovery

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// BeginRead opens a read transaction over the issue schema.
func (s *Service) BeginRead(ctx context.Context) (pgx.Tx, error) {
	return s.pool.Begin(ctx)
}

// EnsureIssueRead verifies the issue exists inside a read transaction.
func (s *Service) EnsureIssueRead(ctx context.Context, tx pgx.Tx, issueID string) (bool, error) {
	var exists bool
	err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM repomesh_issues.issues WHERE id=$1)", issueID).Scan(&exists)
	return exists, err
}

// LoadRead loads the discovery state inside a read transaction; a missing
// row yields an empty state (200 with idle step per contract 7.2).
func (s *Service) LoadRead(ctx context.Context, tx pgx.Tx, issueID string) (*State, error) {
	st, err := s.load(ctx, tx, issueID)
	if err != nil {
		return nil, err
	}
	if st == nil {
		st = &State{IssueID: issueID}
	}
	return st, nil
}
