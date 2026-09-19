package tasks

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore implements Store on the shared pool. Tables belong to
// migrations 0009/0010 (base) + 0015 (protocol increment); all statements
// are public.-qualified because the pool pins search_path to pg_catalog.
// DecisionSink, when wired by the composition root, lands a decision-chain
// node inside every revision transaction (双轴挂钩, 协议 §2.4).
type PostgresStore struct {
	pool         *pgxpool.Pool
	DecisionSink DecisionSink
}

// NewPostgresStore builds the store (composition root only).
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

const taskColumns = `id, organization_id::text, project_id::text,
  COALESCE(plan_id::text, ''), COALESCE(task_uid, ''), COALESCE(repository_id, ''),
  title, instruction, acceptance, status,
  COALESCE(assignee_agent_id::text, ''), version, COALESCE(idempotency_key, ''),
  batch_no, COALESCE(conversation_id, ''), COALESCE(leader_label, ''), COALESCE(worker_label, '')`

func scanTask(row pgx.Row) (Task, error) {
	var t Task
	var batchNo *int
	err := row.Scan(&t.ID, &t.OrganizationID, &t.ProjectID, &t.PlanID, &t.TaskUID,
		&t.RepositoryID, &t.Title, &t.Instruction, &t.Acceptance, &t.Status,
		&t.AssigneeAgentID, &t.Version, &t.IdempotencyKey,
		&batchNo, &t.ConversationID, &t.LeaderLabel, &t.WorkerLabel)
	t.BatchNo = batchNo
	return t, err
}

// scanTaskWithAssignee 是 ListPlanTasks 的扫描器：在 taskColumns 之后多读一列
// 真实执行者（见 ListPlanTasks 的注释）。
func scanTaskWithAssignee(row pgx.Row) (Task, error) {
	var t Task
	var batchNo *int
	err := row.Scan(&t.ID, &t.OrganizationID, &t.ProjectID, &t.PlanID, &t.TaskUID,
		&t.RepositoryID, &t.Title, &t.Instruction, &t.Acceptance, &t.Status,
		&t.AssigneeAgentID, &t.Version, &t.IdempotencyKey,
		&batchNo, &t.ConversationID, &t.LeaderLabel, &t.WorkerLabel, &t.Assignee)
	t.BatchNo = batchNo
	return t, err
}

func scanAttempt(row pgx.Row) (Attempt, error) {
	var (
		a          Attempt
		previous   *string
		execution  *string
		finishedAt *time.Time
	)
	err := row.Scan(&a.ID, &a.TaskID, &a.ManagerAgentID, &a.WorkerAgentID,
		&a.Generation, &a.State, &a.Reason, &previous, &execution,
		&finishedAt)
	if err != nil {
		return Attempt{}, err
	}
	if previous != nil {
		a.PreviousAttemptID = *previous
	}
	if execution != nil {
		a.ExecutionID = *execution
	}
	a.FinishedAt = finishedAt
	return a, nil
}

// CreateTask inserts one task, idempotent by idempotency key.
func (p *PostgresStore) CreateTask(ctx context.Context, w TaskWrite) (Task, error) {
	id := newUUIDv4()
	tag, err := p.pool.Exec(ctx, `
		INSERT INTO public.tasks
		  (id, organization_id, project_id, plan_id, task_uid, repository_id,
		   title, instruction, acceptance, idempotency_key)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7, $8, $9, NULLIF($10, ''))
		ON CONFLICT (idempotency_key) DO NOTHING`,
		id, w.OrganizationID, w.ProjectID, w.PlanID, w.TaskUID,
		w.RepositoryID, w.Title, w.Instruction, w.Acceptance, w.IdempotencyKey)
	if err != nil {
		return Task{}, err
	}
	if tag.RowsAffected() == 1 {
		return p.getTask(ctx, id)
	}
	var storedID string
	if err := p.pool.QueryRow(ctx,
		`SELECT id FROM public.tasks WHERE idempotency_key = $1`, w.IdempotencyKey,
	).Scan(&storedID); err != nil {
		return Task{}, err
	}
	return p.getTask(ctx, storedID)
}

func (p *PostgresStore) getTask(ctx context.Context, id string) (Task, error) {
	return scanTask(p.pool.QueryRow(ctx,
		`SELECT `+taskColumns+` FROM public.tasks WHERE id = $1`, id))
}

// GetTask reads one task; pgx.ErrNoRows when absent.
func (p *PostgresStore) GetTask(ctx context.Context, id string) (*Task, error) {
	task, err := scanTask(p.pool.QueryRow(ctx,
		`SELECT `+taskColumns+` FROM public.tasks WHERE id = $1`, id))
	if err != nil {
		return nil, err
	}
	return &task, nil
}

// ListPlanTasks returns the plan's live tasks (protocol migration input).
//
// 2026-09-20 线上实测：任务树的行数据此前只读 worker_label，而物化写入端从不填它
// ——界面上每条**已经跑完**的任务都写着「待指派」。执行者不该是编出来的名字：
// 这里把它换成真的跑过这条任务的那个 agent（agent_runs 里 task_package_ref =
// 任务 id 的最近一条开发 run；测试 run 是观察者，不算执行者）。
func (p *PostgresStore) ListPlanTasks(ctx context.Context, planID string) ([]Task, error) {
	rows, err := p.pool.Query(ctx, `SELECT `+taskColumns+`,
		       COALESCE(run.agent_kind, '')
		FROM public.tasks t
		LEFT JOIN LATERAL (
		  SELECT r.agent_kind FROM repomesh_execution.agent_runs r
		  WHERE r.task_package_ref = t.id::text AND r.agent_kind <> 'test_agent'
		  ORDER BY r.created_at DESC LIMIT 1
		) run ON true
		WHERE t.plan_id = $1 AND t.status <> 'superseded'`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Task{}
	for rows.Next() {
		task, err := scanTaskWithAssignee(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, task)
	}
	return out, rows.Err()
}

// Assign creates the first attempt or supersedes the current worker, in one
// locked transaction (协议 §2 步骤 5-6). Manager/worker are agent principals.
func (p *PostgresStore) Assign(ctx context.Context, taskID, managerAgentID, workerAgentID string,
	expectedTaskVersion, expectedGeneration int, reason string) (Attempt, error) {
	if workerAgentID == "" || managerAgentID == "" {
		return Attempt{}, ErrConflict
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return Attempt{}, err
	}
	defer tx.Rollback(ctx)

	var taskVersion int
	var currentWorker *string
	err = tx.QueryRow(ctx,
		`SELECT version, assignee_agent_id::text FROM public.tasks WHERE id = $1 FOR UPDATE`, taskID,
	).Scan(&taskVersion, &currentWorker)
	if errors.Is(err, pgx.ErrNoRows) {
		return Attempt{}, fmt.Errorf("%w: task not found", ErrConflict)
	}
	if err != nil {
		return Attempt{}, err
	}
	if taskVersion != expectedTaskVersion {
		return Attempt{}, fmt.Errorf("%w: task version moved", ErrConflict)
	}

	var (
		activeID    *string
		activeGen   int
		activeState AttemptState
	)
	err = tx.QueryRow(ctx, `
		SELECT id, generation, attempt_state FROM public.task_assignments
		WHERE task_id = $1 AND attempt_state <> 'superseded'
		FOR UPDATE`, taskID,
	).Scan(&activeID, &activeGen, &activeState)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Attempt{}, err
	}
	if activeID != nil {
		if activeGen != expectedGeneration {
			return Attempt{}, fmt.Errorf("%w: attempt generation moved", ErrConflict)
		}
		if currentWorker != nil && *currentWorker == workerAgentID {
			return Attempt{}, fmt.Errorf("%w: replacement worker must differ from current assignee", ErrConflict)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE public.task_assignments
			SET attempt_state = 'superseded', finished_at = now()
			WHERE id = $1`, *activeID); err != nil {
			return Attempt{}, err
		}
	} else if expectedGeneration != 0 {
		return Attempt{}, fmt.Errorf("%w: task assignment does not exist", ErrConflict)
	}

	newGen := activeGen + 1
	attemptID := newUUIDv4()
	if _, err := tx.Exec(ctx, `
		INSERT INTO public.task_assignments
		  (id, task_id, manager_agent_id, worker_agent_id, generation,
		   attempt_state, attempt_reason, previous_attempt_id)
		VALUES ($1, $2, $3, $4, $5, 'dispatched', $6, $7)`,
		attemptID, taskID, managerAgentID, workerAgentID, newGen, reason, activeID); err != nil {
		return Attempt{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE public.tasks
		SET status = 'assigned', assignee_agent_id = $2, version = version + 1
		WHERE id = $1`, taskID, workerAgentID); err != nil {
		return Attempt{}, err
	}

	attempt, err := scanAttempt(tx.QueryRow(ctx, `
		SELECT id, task_id::text, manager_agent_id::text, worker_agent_id::text,
		  generation, attempt_state, COALESCE(attempt_reason, ''),
		  previous_attempt_id::text, execution_id::text, finished_at
		FROM public.task_assignments WHERE id = $1`, attemptID))
	if err != nil {
		return Attempt{}, err
	}
	return attempt, tx.Commit(ctx)
}

// ReopenSameAssignment returns a task to its current worker.
func (p *PostgresStore) ReopenSameAssignment(ctx context.Context, taskID string,
	expectedTaskVersion, expectedGeneration int) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var taskVersion int
	err = tx.QueryRow(ctx,
		`SELECT version FROM public.tasks WHERE id = $1 FOR UPDATE`, taskID).Scan(&taskVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: task not found", ErrConflict)
	}
	if err != nil {
		return err
	}
	if taskVersion != expectedTaskVersion {
		return fmt.Errorf("%w: task version moved", ErrConflict)
	}
	var generation int
	err = tx.QueryRow(ctx, `
		SELECT generation FROM public.task_assignments
		WHERE task_id = $1 AND attempt_state <> 'superseded' FOR UPDATE`, taskID).Scan(&generation)
	if err != nil {
		return err
	}
	if generation != expectedGeneration {
		return fmt.Errorf("%w: attempt generation moved", ErrConflict)
	}
	_, err = tx.Exec(ctx, `
		UPDATE public.tasks
		SET status = 'assigned', version = version + 1
		WHERE id = $1`, taskID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// BindExecution links the active attempt to a concrete execution run.
func (p *PostgresStore) BindExecution(ctx context.Context, taskID string,
	expectedGeneration int, executionID string) error {
	tag, err := p.pool.Exec(ctx, `
		UPDATE public.task_assignments SET execution_id = $3
		WHERE task_id = $1 AND attempt_state <> 'superseded' AND generation = $2`,
		taskID, expectedGeneration, executionID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: attempt generation moved", ErrConflict)
	}
	return nil
}

// AllowsProjection is the supersede gate (协议 §2 步骤 6): only the
// current-generation, non-superseded attempt may project results.
func (p *PostgresStore) AllowsProjection(ctx context.Context, taskID, attemptID string, generation int) (bool, error) {
	var ok bool
	err := p.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM public.task_assignments
			WHERE id = $1 AND task_id = $2 AND generation = $3 AND attempt_state <> 'superseded'
		)`, attemptID, taskID, generation).Scan(&ok)
	return ok, err
}

// CompleteCurrent marks the active attempt completed; a concurrent supersede
// is a conflict, completing twice is a no-op.
func (p *PostgresStore) CompleteCurrent(ctx context.Context, taskID, attemptID string, generation int) error {
	tag, err := p.pool.Exec(ctx, `
		UPDATE public.task_assignments
		SET attempt_state = 'completed', finished_at = now()
		WHERE id = $2 AND task_id = $1 AND generation = $3 AND attempt_state = 'dispatched'`,
		taskID, attemptID, generation)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var state AttemptState
		err := p.pool.QueryRow(ctx,
			`SELECT attempt_state FROM public.task_assignments WHERE id = $1`, attemptID).Scan(&state)
		if err == nil && state == AttemptCompleted {
			return nil
		}
		return fmt.Errorf("%w: attempt generation moved", ErrConflict)
	}
	return nil
}

// ActiveAttempt returns the current non-superseded attempt.
func (p *PostgresStore) ActiveAttempt(ctx context.Context, taskID string) (*Attempt, error) {
	row := p.pool.QueryRow(ctx, `
		SELECT id, task_id::text, manager_agent_id::text, worker_agent_id::text,
		  generation, attempt_state, COALESCE(attempt_reason, ''),
		  previous_attempt_id::text, execution_id::text, finished_at
		FROM public.task_assignments
		WHERE task_id = $1 AND attempt_state <> 'superseded'`, taskID)
	attempt, err := scanAttempt(row)
	if err != nil {
		return nil, err
	}
	return &attempt, nil
}
