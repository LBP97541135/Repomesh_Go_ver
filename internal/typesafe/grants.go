package typesafe

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Grant struct {
	Purpose   string
	TaskID    string
	Token     string
	ProjectID string
	IssueID   string
	Revision  int64
}

// IssueGrant is used by the registered host executor, not exposed as an HTTP
// endpoint. The test run, its assignment and configuration determine the scope.
func IssueGrant(ctx context.Context, pool *pgxpool.Pool, workerID, runID string) (*Grant, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, unavailable()
	}
	defer rollback(tx)
	var g Grant
	err = tx.QueryRow(ctx, `SELECT a.project_id,a.issue_id,c.revision,CASE r.agent_kind WHEN 'review_agent' THEN 'code_review' ELSE 'test' END,
 CASE WHEN r.task_package_ref LIKE 'plan:%' THEN '' ELSE r.task_package_ref END FROM repomesh_execution.agent_runs r
		JOIN repomesh_execution.attempts a ON a.id=r.attempt_id
		JOIN repomesh_projects.projects p ON p.id=a.project_id AND p.removed_at IS NULL
		JOIN repomesh_access.accounts account ON account.id=p.owner AND NOT account.disabled
		JOIN repomesh_typesafe.settings c ON c.project_id=a.project_id
		WHERE r.id=$1 AND a.worker_id=$2 AND r.agent_kind IN ('test_agent','review_agent') AND r.state IN ('pending','running')
		AND a.state IN ('launch_verified','running') AND c.enabled AND c.secret_version IS NOT NULL
		FOR SHARE OF c`, runID, workerID).Scan(&g.ProjectID, &g.IssueID, &g.Revision, &g.Purpose, &g.TaskID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, unavailable()
	}
	var raw [32]byte
	if _, err = rand.Read(raw[:]); err != nil {
		return nil, unavailable()
	}
	g.Token = base64.RawURLEncoding.EncodeToString(raw[:])
	hash := sha256.Sum256([]byte(g.Token))
	_, err = tx.Exec(ctx, `INSERT INTO repomesh_typesafe.run_grants(run_id,project_id,issue_id,revision,token_hash,skill_hash,expires_at,purpose,task_id)
		VALUES($1,$2,$3,$4,$5,$6,clock_timestamp()+interval '2 hours',$7,$8)`, runID, g.ProjectID, g.IssueID, g.Revision, hash[:], SkillHash(), g.Purpose, g.TaskID)
	if err != nil {
		return nil, unavailable()
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, unavailable()
	}
	return &g, nil
}

// FinishGrant makes a token unusable and records a missing invocation rather
// than letting a configured but unused tool look like successful verification.
func FinishGrant(ctx context.Context, pool *pgxpool.Pool, runID, reason string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return unavailable()
	}
	defer rollback(tx)
	_, err = tx.Exec(ctx, `UPDATE repomesh_typesafe.run_grants SET revoked_at=clock_timestamp() WHERE run_id=$1`, runID)
	if err != nil {
		return unavailable()
	}
	if reason == "" {
		reason = "TYPESAFE_NOT_INVOKED"
	}
	_, err = tx.Exec(ctx, `INSERT INTO repomesh_typesafe.evaluations
		(project_id,issue_id,run_id,request_id,revision,skill_hash,template_version,input,input_hash,status,error_code,completed_at,purpose,task_id)
		SELECT project_id,issue_id,run_id,'runtime-status',revision,skill_hash,$2,'{}','', 'unavailable',$3,clock_timestamp(),purpose,task_id
		FROM repomesh_typesafe.run_grants g WHERE run_id=$1
		AND NOT EXISTS(SELECT 1 FROM repomesh_typesafe.evaluations e WHERE e.run_id=g.run_id)
		ON CONFLICT(run_id,request_id) DO NOTHING`, runID, TemplateVersion, reason)
	if err != nil {
		return unavailable()
	}
	_, err = tx.Exec(ctx, `UPDATE repomesh_typesafe.evaluations SET status='unknown',error_code='TYPESAFE_OUTCOME_UNKNOWN',completed_at=clock_timestamp()
		WHERE run_id=$1 AND status='pending'`, runID)
	if err != nil {
		return unavailable()
	}
	if err = tx.Commit(ctx); err != nil {
		return unavailable()
	}
	return nil
}

func finishContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
}
