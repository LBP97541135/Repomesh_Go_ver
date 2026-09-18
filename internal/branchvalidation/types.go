// Package branchvalidation implements M8: per-candidate isolated database
// branch validation (Py: review_validation.DatabaseBranchValidationService,
// judge suggestion ①). The control plane owns the lifecycle and evidence; the
// actual branch provisioning is a provider port — the local PostgreSQL
// provider is verifiable without enterprise PolarDB access, whose adapter
// stays a port (design: database-branch-validation-spec.md).
package branchvalidation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Service implements the control plane. The provider port keeps it
// provider-neutral; PolarDB plugs in as an adapter once enterprise access is
// authorized (spec: the local provider is verifiable, Polar results must
// never be claimed from it).
type Service struct {
	pool     *pgxpool.Pool
	provider BranchProvider
}

// New wires the service. A nil provider leaves every run in provisioning
// failure with the real reason instead of faking success.
func New(pool *pgxpool.Pool, provider BranchProvider) *Service {
	return &Service{pool: pool, provider: provider}
}

// StartCommand requests one validation run.
type StartCommand struct {
	OrganizationID    string
	ProjectID         string
	RepositoryID      string
	TaskID            string
	CandidateSHA      string
	SourceDatabaseRef string
	IdempotencyKey    string
	Migrations        []string
}

// MigrationResult captures one statement outcome as evidence.
type MigrationResult struct {
	Statement string `json:"statement"`
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
}

// RunView is the read projection of one validation run.
type RunView struct {
	ID               string            `json:"id"`
	RepositoryID     string            `json:"repositoryId"`
	CandidateSHA     string            `json:"candidateSha"`
	Provider         string            `json:"provider"`
	BranchRef        string            `json:"branchRef,omitempty"`
	Status           string            `json:"status"`
	FailureCode      string            `json:"failureCode,omitempty"`
	CleanupPending   bool              `json:"cleanupPending"`
	MigrationResults []MigrationResult `json:"migrationResults"`
}

func newID(prefix string) (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("branchvalidation: id generation failed: %w", err)
	}
	buffer[6] = (buffer[6] & 0x0f) | 0x40
	buffer[8] = (buffer[8] & 0x3f) | 0x80
	hexSum := hex.EncodeToString(buffer)
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hexSum[0:8], hexSum[8:12], hexSum[12:16], hexSum[16:20], hexSum[20:32]), nil
}

var _ = context.Background
