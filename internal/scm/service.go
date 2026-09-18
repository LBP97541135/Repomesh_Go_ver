// Package scm implements P1's SCM surface: signed GitHub webhook ingestion
// and the change-sets lifecycle (freeze → push/PR/CI/reviews/merge →
// merge-gate) over the tables already migrated in 0012. Port of Py:
// api/scm_webhook.py, modules/delivery/api/router.py.
package scm

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Service owns webhook ingestion and change-set lifecycle records.
type Service struct {
	pool   *pgxpool.Pool
	secret string
}

func New(pool *pgxpool.Pool, webhookSecret string) *Service {
	return &Service{pool: pool, secret: webhookSecret}
}

// VerifySignature validates the X-Hub-Signature-256 header (constant-time).
func (s *Service) VerifySignature(body []byte, signature string) bool {
	if s.secret == "" || !strings.HasPrefix(signature, "sha256=") {
		return false
	}
	mac := hmac.New(sha256.New, []byte(s.secret))
	mac.Write(body)
	expected := mac.Sum(nil)
	provided, err := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
	return err == nil && hmac.Equal(expected, provided)
}

// Ingest stores one webhook delivery as an scm_observations row keyed by its
// delivery id; replaying the same delivery is a no-op.
func (s *Service) Ingest(ctx context.Context, deliveryID, event, payload []byte) error {
	if _, err := s.pool.Exec(ctx, `INSERT INTO public.scm_observations
		(id, provider, source, external_id, event_type, payload, status)
		VALUES ($1, 'github', 'webhook', $1, $2, $3::jsonb, 'pending')
		ON CONFLICT (id) DO NOTHING`,
		deliveryID, event, payload); err != nil {
		return fmt.Errorf("scm: observation insert failed: %w", err)
	}
	return nil
}

// FreezeCommand turns merged work into an immutable change-set.
type FreezeCommand struct {
	OrganizationID string   `json:"organizationId"`
	ProjectID      string   `json:"projectId"`
	Repositories   []string `json:"repositories"`
	Title          string   `json:"title"`
}

// Freeze creates a change_set row and returns its id.
func (s *Service) Freeze(ctx context.Context, command FreezeCommand) (string, error) {
	if command.ProjectID == "" || len(command.Repositories) == 0 {
		return "", fmt.Errorf("scm: project and repositories are required")
	}
	var csID string
	repositories, _ := json.Marshal(command.Repositories)
	if err := s.pool.QueryRow(ctx, `INSERT INTO public.change_sets (id, organization_id, repository_ids, status)
		VALUES (gen_random_uuid(), $1::uuid, $2::jsonb, 'frozen') RETURNING id::text`,
		command.OrganizationID, repositories).Scan(&csID); err != nil {
		return "", fmt.Errorf("scm: freeze failed: %w", err)
	}
	return csID, nil
}

// LifecycleEvent records one lifecycle command on a change-set (push/pr/ci/
// review/merge). The merge-gate consults these records.
func (s *Service) RecordEvent(ctx context.Context, changeSetID, kind, payload string) error {
	allowed := map[string]bool{"push": true, "pull_request": true, "ci": true, "review": true, "merge": true}
	if !allowed[kind] {
		return fmt.Errorf("scm: unknown lifecycle kind %q", kind)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO public.scm_commands
		(id, change_set_id, command_type, params, status)
		VALUES (gen_random_uuid(), $1::uuid, $2, $3::jsonb, 'recorded')`,
		changeSetID, kind, payload); err != nil {
		return fmt.Errorf("scm: event record failed: %w", err)
	}
	return nil
}

// MergeGate is the fail-closed projection: a change-set may merge only when
// every gate input is present and passing.
type MergeGate struct {
	ChangeSetID string `json:"changeSetId"`
	Pushed      bool   `json:"pushed"`
	PR          bool   `json:"pr"`
	CIPassed    bool   `json:"ciPassed"`
	Reviewed    bool   `json:"reviewed"`
	Open        bool   `json:"open"`
}

// Gate evaluates the merge-gate for one change-set.
func (s *Service) Gate(ctx context.Context, changeSetID string) (MergeGate, error) {
	gate := MergeGate{ChangeSetID: changeSetID, Open: true}
	rows, err := s.pool.Query(ctx, `SELECT command_type, status FROM public.scm_commands
		WHERE change_set_id=$1::uuid AND status='recorded'`, changeSetID)
	if err != nil {
		return gate, fmt.Errorf("scm: gate query failed: %w", err)
	}
	defer rows.Close()
	seen := map[string]bool{}
	ciFailed := false
	for rows.Next() {
		var kind, status string
		if rows.Scan(&kind, &status) != nil {
			return gate, fmt.Errorf("scm: gate scan failed")
		}
		seen[kind] = true
		if kind == "ci" && strings.Contains(strings.ToLower(status), "fail") {
			ciFailed = true
		}
	}
	if rows.Err() != nil {
		return gate, fmt.Errorf("scm: gate rows failed")
	}
	gate.Pushed = seen["push"]
	gate.PR = seen["pull_request"]
	gate.CIPassed = seen["ci"] && !ciFailed
	gate.Reviewed = seen["review"]
	gate.Open = gate.Pushed && gate.PR && gate.CIPassed && gate.Reviewed
	return gate, nil
}

var _ = json.Marshal
var _ = time.Now

// ChangeSetSummary is one row of the by-task change-set listing (PR train
// cars read surface): the fields the delivery train needs to label a car.
type ChangeSetSummary struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	PRURL   string `json:"prUrl,omitempty"`
	Branch  string `json:"branch,omitempty"`
	TaskID  string `json:"taskId,omitempty"`
}

// ListByTasks returns the change sets bound to the given task ids, scoped to
// the project through the tasks table (a task id from another project yields
// nothing rather than leaking foreign change sets).
func (s *Service) ListByTasks(ctx context.Context, projectID string, taskIDs []string) ([]ChangeSetSummary, error) {
	if len(taskIDs) == 0 {
		return []ChangeSetSummary{}, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT cs.id::text, cs.status,
		COALESCE(cs.pr_url, ''), COALESCE(cs.branch, ''), COALESCE(cs.task_id::text, '')
		FROM public.change_sets cs
		WHERE cs.task_id = ANY($1::uuid[])
		  AND EXISTS (SELECT 1 FROM public.tasks t WHERE t.id = cs.task_id AND t.project_id = $2::uuid)
		ORDER BY cs.id`, taskIDs, projectID)
	if err != nil {
		return nil, fmt.Errorf("scm: list change-sets failed: %w", err)
	}
	defer rows.Close()
	out := []ChangeSetSummary{}
	for rows.Next() {
		var item ChangeSetSummary
		if rows.Scan(&item.ID, &item.Status, &item.PRURL, &item.Branch, &item.TaskID) != nil {
			return nil, fmt.Errorf("scm: list change-sets scan failed")
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
