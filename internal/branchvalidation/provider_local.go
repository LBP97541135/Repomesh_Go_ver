// provider_local.go is the verifiable local branch provider: it creates a
// real PostgreSQL database cloned from the source via TEMPLATE, applies the
// migrations, and drops the database. This proves the control plane without
// PolarDB; its results are local evidence only (spec constraint).
package branchvalidation

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// LocalProvider provisions branches as PostgreSQL databases in the same
// instance via CREATE DATABASE ... TEMPLATE.
type LocalProvider struct {
	// AdminDSN connects to the maintenance database (e.g. postgres) with
	// CREATE DATABASE rights.
	AdminDSN string
}

func (p *LocalProvider) Name() string { return "local-postgres-template" }

// ProvisionBranch creates db branch_<sha-prefix>_<ts> from the template.
func (p *LocalProvider) ProvisionBranch(ctx context.Context, sourceDatabaseRef, candidateSHA string) (string, error) {
	databaseName := branchName(candidateSHA)
	if err := p.execAdmin(ctx, fmt.Sprintf(
		`CREATE DATABASE %q TEMPLATE %q`, databaseName, sourceDatabaseRef)); err != nil {
		return "", fmt.Errorf("branchvalidation: branch creation failed: %w", err)
	}
	return databaseName, nil
}

// ApplyMigrations runs each statement on the branch database, stopping at the
// first failure but recording every result.
func (p *LocalProvider) ApplyMigrations(ctx context.Context, branchRef string, migrations []string) ([]MigrationResult, error) {
	results := []MigrationResult{}
	branchDSN := replaceDatabase(p.AdminDSN, branchRef)
	var firstErr error
	for _, statement := range migrations {
		err := execOn(ctx, branchDSN, statement)
		result := MigrationResult{Statement: statement, OK: err == nil}
		if err != nil {
			result.Error = err.Error()
			if firstErr == nil {
				firstErr = err
			}
		}
		results = append(results, result)
	}
	if firstErr != nil {
		return results, firstErr
	}
	return results, nil
}

// CleanupBranch drops the branch database.
func (p *LocalProvider) CleanupBranch(ctx context.Context, branchRef string) error {
	if !strings.HasPrefix(branchRef, "branch_") {
		return fmt.Errorf("branchvalidation: refusing to drop non-branch database %q", branchRef)
	}
	return p.execAdmin(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS %q`, branchRef))
}

func (p *LocalProvider) execAdmin(ctx context.Context, statement string) error {
	return execOn(ctx, p.AdminDSN, statement)
}

func branchName(candidateSHA string) string {
	prefix := candidateSHA
	if len(prefix) > 8 {
		prefix = prefix[:8]
	}
	safe := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		return -1
	}, strings.ToLower(prefix))
	if safe == "" {
		safe = "noid"
	}
	return fmt.Sprintf("branch_%s_%d", safe, time.Now().UnixNano()%1000000)
}
