package observability

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// DiscoverySourceVersion identifies the local snapshot contract. It is
// independent of both the AgentLoop export format and any Agent runtime.
const DiscoverySourceVersion = "repomesh-discovery/0.1"

// Fact is immutable evidence from an actual transaction, not a reconstruction
// from the latest business state. Snapshot's exact bytes hash to Fingerprint.
type Fact struct {
	ID            string          `json:"id"`
	ProjectID     string          `json:"project_id"`
	IssueID       string          `json:"issue_id"`
	OccurredAt    time.Time       `json:"occurred_at"`
	Snapshot      json.RawMessage `json:"snapshot"`
	Fingerprint   string          `json:"fingerprint"`
	SourceVersion string          `json:"source_version"`
}

// AppendDiscoveryFact writes through the caller's business transaction only.
// The caller must lock the Issue before updating its discovery state; taking
// the same lock here also serializes other direct append callers. Only an
// adjacent identical snapshot is suppressed: A -> B -> A remains three facts.
func AppendDiscoveryFact(ctx context.Context, tx pgx.Tx, projectID, issueID string, snapshot json.RawMessage) error {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(issueID) == "" {
		return errors.New("observability: project and issue are required")
	}
	var identity struct {
		ProjectID string `json:"project_id"`
		IssueID   string `json:"issue_id"`
	}
	if err := json.Unmarshal(snapshot, &identity); err != nil || identity.ProjectID != projectID || identity.IssueID != issueID {
		return errors.New("observability: snapshot identity does not match its source")
	}
	var locked string
	if err := tx.QueryRow(ctx, `SELECT id FROM repomesh_issues.issues
		WHERE project_id=$1 AND id=$2 FOR NO KEY UPDATE`, projectID, issueID).Scan(&locked); err != nil {
		return fmt.Errorf("observability: lock source issue: %w", err)
	}
	sum := sha256.Sum256(snapshot)
	fingerprint := hex.EncodeToString(sum[:])
	var previous, previousVersion string
	err := tx.QueryRow(ctx, `SELECT fingerprint,source_version FROM repomesh_observability.facts
		WHERE project_id=$1 AND issue_id=$2 ORDER BY id DESC LIMIT 1`, projectID, issueID).Scan(&previous, &previousVersion)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("observability: inspect prior fact: %w", err)
	}
	if err == nil && previous == fingerprint && previousVersion == DiscoverySourceVersion {
		return nil
	}
	_, err = tx.Exec(ctx, `INSERT INTO repomesh_observability.facts
		(project_id,issue_id,source_version,snapshot,fingerprint)
		VALUES($1,$2,$3,$4::json,$5)`, projectID, issueID, DiscoverySourceVersion, string(snapshot), fingerprint)
	if err != nil {
		return fmt.Errorf("observability: append discovery fact: %w", err)
	}
	return nil
}

// ListFacts is the narrow administrative collector adapter. Both exact IDs
// are required; callers supply their own administrative database credentials.
// It is not a browser authorization API. Unknown/mismatched ownership returns
// pgx.ErrNoRows; an existing Issue with no captured history returns an empty
// list. It never fabricates a baseline or uses a max-ID commit watermark.
func (s *Service) ListFacts(ctx context.Context, projectID, issueID string) ([]Fact, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(issueID) == "" {
		return nil, errors.New("observability: project and issue are required")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("observability: begin fact read: %w", err)
	}
	defer tx.Rollback(ctx)
	var found string
	if err := tx.QueryRow(ctx, `SELECT id FROM repomesh_issues.issues WHERE project_id=$1 AND id=$2`, projectID, issueID).Scan(&found); err != nil {
		return nil, fmt.Errorf("observability: find source issue: %w", err)
	}
	rows, err := tx.Query(ctx, `SELECT id::text,project_id,issue_id,occurred_at,snapshot,fingerprint,source_version
		FROM repomesh_observability.facts WHERE project_id=$1 AND issue_id=$2 ORDER BY id`, projectID, issueID)
	if err != nil {
		return nil, fmt.Errorf("observability: list facts: %w", err)
	}
	defer rows.Close()
	facts := []Fact{}
	for rows.Next() {
		var fact Fact
		if err := rows.Scan(&fact.ID, &fact.ProjectID, &fact.IssueID, &fact.OccurredAt, &fact.Snapshot, &fact.Fingerprint, &fact.SourceVersion); err != nil {
			return nil, fmt.Errorf("observability: decode fact: %w", err)
		}
		facts = append(facts, fact)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("observability: read facts: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("observability: finish fact read: %w", err)
	}
	return facts, nil
}
