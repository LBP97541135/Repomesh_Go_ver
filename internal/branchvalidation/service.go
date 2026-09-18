package branchvalidation

import "context"

// BranchProvider provisions one isolated database branch and runs ordered
// migration statements on it.
type BranchProvider interface {
	// ProvisionBranch creates the branch from the source database and returns
	// its connection reference (a DSN or provider-specific handle).
	ProvisionBranch(ctx context.Context, sourceDatabaseRef, candidateSHA string) (branchRef string, err error)
	// ApplyMigrations runs the ordered statements on the branch and returns
	// the per-statement results (empty when all succeeded).
	ApplyMigrations(ctx context.Context, branchRef string, migrations []string) ([]MigrationResult, error)
	// CleanupBranch deletes the branch. Cleanup failure is recorded, never
	// silent (spec: cleanup recovery).
	CleanupBranch(ctx context.Context, branchRef string) error
}
