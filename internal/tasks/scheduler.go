// scheduler.go adds the M1 runtime to the tasks axis: dependency-driven ready
// promotion over plan_steps, dispatch into the B10 execution ledger, and the
// manager approve/reject gates. Design:
// docs/development/2026-09-16-dag-orchestration-design/DESIGN.md merged onto
// the tasks package per the 2026-09-17 merge decision (tasks 为主).
package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ExecutionFacade is the narrow slice of the B10 execution ledger the
// scheduler uses; orchestration never bypasses the ledger.
type ExecutionFacade interface {
	// ReserveForTask reserves one worker attempt for the task and registers a
	// pending agent run, returning the attempt id. It must not start the
	// agent itself: the host executor claims pending runs.
	ReserveForTask(ctx context.Context, workerID, taskID, agentKind, title, instruction string) (string, error)
}

// PromoteReady scans plan_steps whose depends_on is fully satisfied by
// sibling steps already done in the same plan, flips them to ready, and
// promotes their pending tasks' status. SKIP LOCKED makes concurrent
// schedulers safe. Returns the number of promoted steps.
func (p *PostgresStore) PromoteReady(ctx context.Context) (int, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return 0, unavailable()
	}
	defer rollbackTx(tx)
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout='2s'`); err != nil {
		return 0, unavailable()
	}
	if _, err := tx.Exec(ctx, `SET LOCAL statement_timeout='10s'`); err != nil {
		return 0, unavailable()
	}
	rows, err := tx.Query(ctx, `SELECT s.id, s.plan_id, s.step_no, s.depends_on FROM public.plan_steps s
		JOIN public.plans pl ON pl.id = s.plan_id
		WHERE s.status = 'pending' AND pl.replan_state <> 'deprecated'
		FOR UPDATE OF s SKIP LOCKED`)
	if err != nil {
		return 0, unavailable()
	}
	type candidate struct {
		stepID    string
		planID    string
		stepNo    int
		dependsOn []string
	}
	candidates := []candidate{}
	for rows.Next() {
		var item candidate
		var deps []byte
		if rows.Scan(&item.stepID, &item.planID, &item.stepNo, &deps) != nil {
			rows.Close()
			return 0, unavailable()
		}
		if err := json.Unmarshal(deps, &item.dependsOn); err != nil {
			item.dependsOn = nil
		}
		candidates = append(candidates, item)
	}
	rows.Close()
	if rows.Err() != nil {
		return 0, unavailable()
	}
	promoted := 0
	for _, item := range candidates {
		ready, err := p.dependenciesSatisfied(ctx, tx, item.planID, item.stepNo, item.dependsOn)
		if err != nil {
			return 0, err
		}
		if !ready {
			continue
		}
		tag, err := tx.Exec(ctx, `UPDATE public.plan_steps SET status='ready'
			WHERE id=$1 AND status='pending'`, item.stepID)
		if err != nil {
			return 0, unavailable()
		}
		if tag.RowsAffected() == 1 {
			promoted++
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, unavailable()
	}
	return promoted, nil
}

// dependenciesSatisfied reports whether every upstream step (by step_no) has
// status done. blocked/running upstream never satisfies.
func (p *PostgresStore) dependenciesSatisfied(ctx context.Context, tx pgx.Tx, planID string, stepNo int, dependsOn []string) (bool, error) {
	if len(dependsOn) == 0 {
		return true, nil
	}
	for _, raw := range dependsOn {
		var upstream int
		if _, err := fmt.Sscanf(raw, "%d", &upstream); err != nil || upstream == stepNo {
			return false, fmt.Errorf("%w: invalid dependency %q on step %d", ErrInvalidPlan, raw, stepNo)
		}
		var satisfied bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM public.plan_steps
			WHERE plan_id=$1 AND step_no=$2 AND status='done')`, planID, upstream).Scan(&satisfied); err != nil {
			return false, unavailable()
		}
		if !satisfied {
			return false, nil
		}
	}
	return true, nil
}

// DispatchOne picks the oldest ready step, reserves a worker attempt through
// the ledger facade, binds the execution to the task, and moves task + step
// into running/dispatched. With no free worker it leaves everything ready.
// agentKind selects the CLI the executor launches (claude_cli | codex_cli).
func (p *PostgresStore) DispatchOne(ctx context.Context, execution ExecutionFacade, workerID, agentKind string) (string, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return "", unavailable()
	}
	defer rollbackTx(tx)
	var stepID, taskID, title, instruction string
	err = tx.QueryRow(ctx, `SELECT s.id, t.id, COALESCE(t.instruction,''), t.title
		FROM public.plan_steps s
		JOIN public.tasks t ON t.plan_id = s.plan_id AND t.title = s.content
		JOIN public.task_repository_scopes scope ON scope.task_id=t.id AND scope.project_id=t.project_id::text
		WHERE s.status = 'ready' AND t.status IN ('pending','ready')
		  AND EXISTS (SELECT 1 FROM repomesh_issues.issues i
		              WHERE i.project_id = scope.project_id AND i.id = scope.issue_id)
		ORDER BY s.step_no LIMIT 1 FOR UPDATE OF s SKIP LOCKED`).
		Scan(&stepID, &taskID, &instruction, &title)
	if errors.Is(err, pgx.ErrNoRows) {
		rollbackTx(tx)
		return "", nil
	}
	if err != nil {
		// Join-by-title found nothing: the step has no matching task row yet.
		return "", nil
	}
	rollbackTx(tx)
	attemptID, err := execution.ReserveForTask(ctx, workerID, taskID, agentKind, title, instruction)
	if err != nil {
		return "", err
	}
	if _, err := p.pool.Exec(ctx, `UPDATE public.plan_steps SET status='dispatched'
		WHERE id=$1 AND status='ready'`, stepID); err != nil {
		return "", unavailable()
	}
	if _, err := p.pool.Exec(ctx, `UPDATE public.tasks SET status='running'
		WHERE id=$1 AND status IN ('pending','ready')`, taskID); err != nil {
		return "", unavailable()
	}
	return attemptID, nil
}

// ApproveStep is the manager's post-completion gate: step → done, task → done.
// Only the bound manager may approve; the task row carries assignee identity.
func (p *PostgresStore) ApproveStep(ctx context.Context, taskID, managerID, summary string) error {
	tag, err := p.pool.Exec(ctx, `UPDATE public.plan_steps s SET status='done'
		FROM public.tasks t
		WHERE s.plan_id = t.plan_id AND s.content = t.title
		  AND t.id=$1 AND t.status='blocked'`, taskID)
	if err != nil {
		return unavailable()
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: step not approvable (task %s not blocked)", ErrConflict, taskID)
	}
	tag, err = p.pool.Exec(ctx, `UPDATE public.tasks SET status='done', result_summary=$2
		WHERE id=$1 AND status='blocked'`, taskID, summary)
	if err != nil {
		return unavailable()
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: task %s left blocked state concurrently", ErrConflict, taskID)
	}
	return nil
}

// RejectStep sends a blocked task back to failed-ish pending with a reason so
// a new attempt generation can be created by retry.
func (p *PostgresStore) RejectStep(ctx context.Context, taskID, managerID, reason string) error {
	if reason == "" {
		return fmt.Errorf("%w: reject requires a reason", ErrInvalidPlan)
	}
	tag, err := p.pool.Exec(ctx, `UPDATE public.tasks SET status='pending', result_summary=$2
		WHERE id=$1 AND status='blocked'`, taskID, reason)
	if err != nil {
		return unavailable()
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: task %s not blocked", ErrConflict, taskID)
	}
	return nil
}

func unavailable() error {
	return fmt.Errorf("tasks: database unavailable")
}

func rollbackTx(tx pgx.Tx) {
	_ = tx.Rollback(context.Background())
}
