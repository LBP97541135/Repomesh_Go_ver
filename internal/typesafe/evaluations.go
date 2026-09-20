package typesafe

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"repomesh.local/repomesh/internal/secrets"
)

const evalColumns = `id::text,project_id,issue_id,run_id,request_id,revision,skill_hash,template_version,input,input_hash,status,error_code,response,latency_ms,created_at,purpose,task_id`

func scanEvaluation(row pgx.Row) (Evaluation, error) {
	var e Evaluation
	var raw, response []byte
	err := row.Scan(&e.ID, &e.ProjectID, &e.IssueID, &e.RunID, &e.RequestID, &e.Revision, &e.SkillHash, &e.TemplateVersion, &raw, &e.InputHash, &e.Status, &e.ErrorCode, &response, &e.LatencyMS, &e.CreatedAt, &e.Purpose, &e.TaskID)
	if err != nil {
		return e, err
	}
	if json.Unmarshal(raw, &e.Input) != nil {
		return Evaluation{}, unavailable()
	}
	if len(response) > 0 && json.Unmarshal(response, &e.Response) != nil {
		return Evaluation{}, unavailable()
	}
	if e.Status == "pending" && time.Since(e.CreatedAt) > CallTimeout+10*time.Second {
		e.Status = "unknown"
		e.ErrorCode = "TYPESAFE_OUTCOME_UNKNOWN"
	}
	return e, nil
}

func (s *Service) Evaluate(ctx context.Context, token string, input Input) (Evaluation, error) {
	if len(token) != 43 || s.secrets == nil {
		return Evaluation{}, fail(401, "TYPESAFE_GRANT_REJECTED")
	}
	in, err := input.normalized()
	if err != nil {
		return Evaluation{}, err
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return Evaluation{}, fail(422, "TYPESAFE_INVALID_INPUT")
	}
	hash := sha256.Sum256(raw)
	inputHash := hex.EncodeToString(hash[:])
	tokenHash := sha256.Sum256([]byte(token))
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Evaluation{}, unavailable()
	}
	defer rollback(tx)
	var runID, project, issue, skillHash, secret, model, purpose, taskID string
	var revision int64
	var calls int
	err = tx.QueryRow(ctx, `SELECT g.run_id,g.project_id,g.issue_id,g.revision,g.skill_hash,g.calls,c.secret_version,c.model,g.purpose,g.task_id
		FROM repomesh_typesafe.run_grants g
		JOIN repomesh_execution.agent_runs r ON r.id=g.run_id AND r.agent_kind IN ('test_agent','review_agent') AND r.state='running'
		JOIN repomesh_execution.attempts a ON a.id=r.attempt_id AND a.project_id=g.project_id AND a.issue_id=g.issue_id
		JOIN repomesh_projects.projects p ON p.id=g.project_id AND p.removed_at IS NULL
		JOIN repomesh_access.accounts account ON account.id=p.owner AND NOT account.disabled
		JOIN repomesh_typesafe.settings c ON c.project_id=g.project_id AND c.revision=g.revision AND c.enabled
		WHERE g.token_hash=$1 AND g.revoked_at IS NULL AND g.expires_at>clock_timestamp()
		AND a.state IN ('launch_verified','running')
		FOR UPDATE OF g FOR SHARE OF c`, tokenHash[:]).Scan(&runID, &project, &issue, &revision, &skillHash, &calls, &secret, &model, &purpose, &taskID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Evaluation{}, fail(401, "TYPESAFE_GRANT_REJECTED")
	}
	if err != nil {
		return Evaluation{}, unavailable()
	}
	existing, err := scanEvaluation(tx.QueryRow(ctx, `SELECT `+evalColumns+` FROM repomesh_typesafe.evaluations WHERE run_id=$1 AND request_id=$2`, runID, in.RequestID))
	if err == nil {
		if existing.InputHash != inputHash {
			return Evaluation{}, fail(409, "TYPESAFE_REQUEST_CONFLICT")
		}
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Evaluation{}, unavailable()
	}
	if calls >= MaxCalls {
		return Evaluation{}, fail(429, "TYPESAFE_RUN_LIMIT")
	}
	key, err := s.secrets.OpenInTx(ctx, tx, secrets.VersionID(secret), owner(project), secrets.TypeSafeAPIKey)
	if err != nil {
		return Evaluation{}, fail(503, "TYPESAFE_KEY_UNAVAILABLE")
	}
	defer clear(key)
	row, err := scanEvaluation(tx.QueryRow(ctx, `INSERT INTO repomesh_typesafe.evaluations
		(project_id,issue_id,run_id,request_id,revision,skill_hash,template_version,input,input_hash,status,purpose,task_id)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'pending',$10,$11) RETURNING `+evalColumns,
		project, issue, runID, in.RequestID, revision, skillHash, TemplateVersion, raw, inputHash, purpose, taskID))
	if err != nil {
		return Evaluation{}, unavailable()
	}
	_, err = tx.Exec(ctx, `UPDATE repomesh_typesafe.run_grants SET calls=calls+1 WHERE run_id=$1`, runID)
	if err != nil {
		return Evaluation{}, unavailable()
	}
	if err = tx.Commit(ctx); err != nil {
		return Evaluation{}, unavailable()
	}
	start := time.Now()
	result, callErr := s.client.Evaluate(ctx, key, model, in)
	status, code := "completed", ""
	var encoded any
	if callErr != nil {
		status, code = "failed", errorCode(callErr)
		if code == "TYPESAFE_OUTCOME_UNKNOWN" {
			status = "unknown"
		}
	} else {
		body, _ := json.Marshal(result)
		encoded = body
	}
	finish, cancel := finishContext(ctx)
	defer cancel()
	_, err = s.pool.Exec(finish, `UPDATE repomesh_typesafe.evaluations SET status=$2,error_code=$3,response=$4,latency_ms=$5,completed_at=clock_timestamp()
		WHERE id=$1::uuid AND status='pending'`, row.ID, status, code, encoded, time.Since(start).Milliseconds())
	if err != nil {
		return Evaluation{}, unavailable()
	}
	row, err = scanEvaluation(s.pool.QueryRow(finish, `SELECT `+evalColumns+` FROM repomesh_typesafe.evaluations WHERE id=$1::uuid`, row.ID))
	if err != nil {
		return Evaluation{}, unavailable()
	}
	return row, nil
}

func (s *Service) List(ctx context.Context, actor, project, issue string) ([]Evaluation, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, unavailable()
	}
	defer rollback(tx)
	if err = projectAccess(ctx, tx, actor, project, false); err != nil {
		return nil, err
	}
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM repomesh_issues.issues WHERE project_id=$1 AND id=$2)`, project, issue).Scan(&exists); err != nil {
		return nil, unavailable()
	}
	if !exists {
		return nil, fail(404, "RESOURCE_NOT_FOUND")
	}
	rows, err := tx.Query(ctx, `SELECT `+evalColumns+` FROM repomesh_typesafe.evaluations WHERE project_id=$1 AND issue_id=$2 ORDER BY created_at DESC LIMIT 100`, project, issue)
	if err != nil {
		return nil, unavailable()
	}
	defer rows.Close()
	items := []Evaluation{}
	for rows.Next() {
		item, err := scanEvaluation(rows)
		if err != nil {
			return nil, unavailable()
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, unavailable()
	}
	return items, nil
}
