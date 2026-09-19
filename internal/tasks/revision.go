package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// PlanRevision is one committed plan-axis version change (协议 §2 步骤 5):
// full-snapshot replace, appended to public.plans.revisions (the 47-table
// base's chosen audit home) with the diff and the v2 task snapshot.
type PlanRevision struct {
	Revision            int            `json:"revision"`
	BaseVersion         string         `json:"baseVersion"`
	ResultVersion       string         `json:"resultVersion"`
	Actor               string         `json:"actor"`
	Reason              string         `json:"reason"`
	PreviousBatches     [][]string     `json:"previousBatches"`
	NewBatches          [][]string     `json:"newBatches"`
	NewTasks            []TaskSnapshot `json:"newTasks"`
	AddedRepositories   []string       `json:"addedRepositories"`
	RemovedRepositories []string       `json:"removedRepositories"`
	CreatedTasks        int            `json:"createdTasks"`
	SupersededTasks     int            `json:"supersededTasks"`
	IdempotencyKey      string         `json:"idempotencyKey"`
}

// RevisionCommand is one apply request. IdempotencyKey dedupes: a replay
// returns the committed revision; the same key carrying a different payload
// is a conflict (Python meaning-change guard, kept). NewTasks is the v2
// task snapshot — the task-axis migration keys off it (T2.5, 显式 uid +
// 仓库名/标题兜底匹配). UpstreamRef 指向触发本轮重规划的 BLOCKED 决策节点。
type RevisionCommand struct {
	PlanID          string
	ExpectedVersion string // 'v1','v2',…
	NewBatches      [][]string
	NewTasks        []TaskSnapshot
	DAG             map[string][]string
	Actor           string
	Reason          string
	UpstreamRef     string
	IdempotencyKey  string
}

// PlanRevisedEvent 双轴挂钩事件：计划换代与决策链同事务（协议 §2.4）。
// tasks 不 import 决策链——组合根基于 internal/decisionchain 实现本接口。
type PlanRevisedEvent struct {
	RequirementText      string
	RequirementKey       string
	Actor                string
	Reason               string
	UpstreamRef          string
	AffectedRepositories []string
	IdempotencyKey       string
}

// nextPlanVersion steps the textual version axis: 'v1' → 'v2'.
func nextPlanVersion(current string) (string, error) {
	if !strings.HasPrefix(current, "v") {
		return "", fmt.Errorf("%w: plan version %q is not sequential", ErrConflict, current)
	}
	n, err := strconv.Atoi(current[1:])
	if err != nil {
		return "", fmt.Errorf("%w: plan version %q is not sequential", ErrConflict, current)
	}
	return "v" + strconv.Itoa(n+1), nil
}

// ApplyRevision lands one full-snapshot revision in a single transaction
// (协议 §2 步骤 3-5): lock plan → idempotency replay (audit lives in
// plans.revisions) → optimistic version check → DAG validation → snapshot
// replace (version/batches/dag/revisions||entry) → task-axis migration →
// decision-chain hook (双轴挂钩).
func (p *PostgresStore) ApplyRevision(ctx context.Context, cmd RevisionCommand) (PlanRevision, error) {
	if cmd.IdempotencyKey == "" {
		return PlanRevision{}, fmt.Errorf("%w: idempotency key is required", ErrConflict)
	}
	if err := ValidateBatches(cmd.NewBatches, cmd.DAG); err != nil {
		return PlanRevision{}, err
	}
	if err := validateTaskSnapshots(cmd.NewTasks); err != nil {
		return PlanRevision{}, err
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return PlanRevision{}, err
	}
	defer tx.Rollback(ctx)

	var (
		plan                             ExecutionPlan
		revisionsRaw, batchesRaw, dagRaw []byte
	)
	err = tx.QueryRow(ctx, `SELECT id, project_id::text, plan_version,
		  requirement_text, COALESCE(requirement_key, ''), replan_state,
		  execution_batches, task_dag, revisions
		FROM public.plans WHERE id = $1 FOR UPDATE`, cmd.PlanID).Scan(
		&plan.ID, &plan.ProjectID, &plan.PlanVersion, &plan.RequirementText,
		&plan.RequirementKey, &plan.ReplanState, &batchesRaw, &dagRaw, &revisionsRaw)
	if errors.Is(err, pgx.ErrNoRows) {
		return PlanRevision{}, fmt.Errorf("%w: plan not found", ErrConflict)
	}
	if err != nil {
		return PlanRevision{}, err
	}
	if err := jsonUnmarshalInto(batchesRaw, &plan.Batches); err != nil {
		return PlanRevision{}, err
	}
	// 同一处 as-built 形状分歧（见 plans.go 的 decodePlanDAG）：物化写富描述、
	// 读取方旧代码只认邻接表。这里复用同一个容忍解码，避免第二条读面继续 503。
	decodedDAG, err := decodePlanDAG(dagRaw)
	if err != nil {
		return PlanRevision{}, err
	}
	plan.DAG = decodedDAG

	history, err := decodeRevisions(revisionsRaw)
	if err != nil {
		return PlanRevision{}, err
	}
	for _, entry := range history {
		if entry.IdempotencyKey != cmd.IdempotencyKey {
			continue
		}
		newBatchesJSON, _ := jsonMarshal(cmd.NewBatches)
		entryBatchesJSON, _ := jsonMarshal(entry.NewBatches)
		newTasksJSON, _ := jsonMarshal(cmd.NewTasks)
		entryTasksJSON, _ := jsonMarshal(entry.NewTasks)
		if entry.BaseVersion != cmd.ExpectedVersion ||
			entry.Actor != cmd.Actor ||
			entry.Reason != cmd.Reason ||
			string(entryBatchesJSON) != string(newBatchesJSON) ||
			string(entryTasksJSON) != string(newTasksJSON) {
			return PlanRevision{}, fmt.Errorf("%w: revision idempotency key changed meaning", ErrConflict)
		}
		entry.CreatedTasks, entry.SupersededTasks = 0, 0 // replay reports the migration once, at apply time
		return entry, tx.Commit(ctx)
	}

	if plan.PlanVersion != cmd.ExpectedVersion {
		return PlanRevision{}, fmt.Errorf("%w: plan version moved (have %s, expected %s)",
			ErrConflict, plan.PlanVersion, cmd.ExpectedVersion)
	}
	resultVersion, err := nextPlanVersion(plan.PlanVersion)
	if err != nil {
		return PlanRevision{}, err
	}
	names := batchRepositories(cmd.NewBatches)
	for _, task := range cmd.NewTasks {
		names = append(names, task.RepositoryID)
	}
	if err := validatePlanScope(ctx, tx, plan.ProjectID, plan.ID, names); err != nil {
		return PlanRevision{}, err
	}

	// 任务轴迁移（T2.5）：uid 相同 = 同一任务原位更新；缺 uid 回退仓库名+标题；
	// 未被 v2 认领的任务 SUPERSEDED，其 active attempt 一并关闭（投影闸生效）。
	// 需要 organization_id 归属（projects 表持有）。
	var organizationID string
	if err := tx.QueryRow(ctx,
		`SELECT organization_id::text FROM repomesh_projects.projects WHERE id = $1`,
		plan.ProjectID).Scan(&organizationID); err != nil {
		return PlanRevision{}, err
	}
	created, superseded, err := migrateTasksTx(ctx, tx, organizationID,
		plan.ProjectID, cmd.PlanID, cmd.NewTasks)
	if err != nil {
		// 0021 的 task_uid 唯一索引按 (project_id, task_uid) 分域后仍可能被
		// 并发/脏数据触发——统一映射为可解释的冲突（审查 #4）。
		if isUniqueViolation(err) {
			return PlanRevision{}, fmt.Errorf("%w: task_uid conflict during task migration", ErrConflict)
		}
		return PlanRevision{}, err
	}

	// 双轴挂钩（T3）：决策链节点与计划换代同事务——失败即整体回滚。
	if p.DecisionSink != nil {
		repos := make([]string, 0, len(cmd.NewBatches)*2)
		for _, batch := range cmd.NewBatches {
			repos = append(repos, batch...)
		}
		if err := p.DecisionSink.RecordPlanRevised(ctx, tx, PlanRevisedEvent{
			RequirementText:      plan.RequirementText,
			RequirementKey:       plan.RequirementKey,
			Actor:                cmd.Actor,
			Reason:               cmd.Reason,
			UpstreamRef:          cmd.UpstreamRef,
			AffectedRepositories: repos,
			IdempotencyKey:       cmd.IdempotencyKey + ":decision",
		}); err != nil {
			return PlanRevision{}, err
		}
	}

	revision := len(history) + 1
	added, removed := DiffRepositories(plan.Batches, cmd.NewBatches)
	entry := PlanRevision{
		Revision:            revision,
		BaseVersion:         plan.PlanVersion,
		ResultVersion:       resultVersion,
		Actor:               cmd.Actor,
		Reason:              cmd.Reason,
		PreviousBatches:     plan.Batches,
		NewBatches:          cmd.NewBatches,
		NewTasks:            cmd.NewTasks,
		AddedRepositories:   added,
		RemovedRepositories: removed,
		CreatedTasks:        created,
		SupersededTasks:     superseded,
		IdempotencyKey:      cmd.IdempotencyKey,
	}
	entryJSON, err := jsonMarshal(entry)
	if err != nil {
		return PlanRevision{}, err
	}
	newBatchesJSON, err := jsonMarshal(cmd.NewBatches)
	if err != nil {
		return PlanRevision{}, err
	}
	dagJSON, err := jsonMarshal(cmd.DAG)
	if err != nil {
		return PlanRevision{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE public.plans
		SET plan_version = $2, execution_batches = $3::jsonb, task_dag = $4::jsonb,
		    revisions = revisions || $5::jsonb
		WHERE id = $1 AND plan_version = $6`,
		cmd.PlanID, resultVersion, newBatchesJSON, dagJSON, entryJSON, cmd.ExpectedVersion); err != nil {
		return PlanRevision{}, err
	}
	return entry, tx.Commit(ctx)
}

// PlanRevisions lists a plan's committed revisions (audit home:
// public.plans.revisions).
func (p *PostgresStore) PlanRevisions(ctx context.Context, planID string) ([]PlanRevision, error) {
	var raw []byte
	err := p.pool.QueryRow(ctx,
		`SELECT revisions FROM public.plans WHERE id = $1`, planID).Scan(&raw)
	if err != nil {
		return nil, err
	}
	return decodeRevisions(raw)
}

func decodeRevisions(raw []byte) ([]PlanRevision, error) {
	if len(raw) == 0 {
		return []PlanRevision{}, nil
	}
	out := []PlanRevision{}
	if err := jsonUnmarshalInto(raw, &out); err != nil {
		return nil, fmt.Errorf("decode revisions: %w", err)
	}
	for i := range out {
		if out[i].NewTasks == nil {
			out[i].NewTasks = []TaskSnapshot{}
		}
		if out[i].AddedRepositories == nil {
			out[i].AddedRepositories = []string{}
		}
		if out[i].RemovedRepositories == nil {
			out[i].RemovedRepositories = []string{}
		}
	}
	return out, nil
}

// validateTaskSnapshots rejects ambiguous v2 task sets at the gate: every
// task needs a repository; duplicate uids are invalid; natural-key
// (repository+title) duplicates without a uid cannot be told apart by the
// fallback matcher.
func validateTaskSnapshots(snapshots []TaskSnapshot) error {
	seenUID := map[string]bool{}
	seenNatural := map[string]bool{}
	for i, in := range snapshots {
		if in.RepositoryID == "" {
			return fmt.Errorf("%w: task #%d has no repository", ErrInvalidPlan, i)
		}
		if in.TaskUID != "" {
			if seenUID[in.TaskUID] {
				return fmt.Errorf("%w: duplicate task uid %q", ErrInvalidPlan, in.TaskUID)
			}
			seenUID[in.TaskUID] = true
		}
		natural := in.RepositoryID + "\x1f" + in.Title
		if in.TaskUID == "" && seenNatural[natural] {
			return fmt.Errorf("%w: duplicate task %q/%q without uid", ErrInvalidPlan, in.RepositoryID, in.Title)
		}
		seenNatural[natural] = true
	}
	return nil
}

// migrateTasksTx executes the task-axis migration inside the revision
// transaction (T2.5, 身份规则 = 显式 uid + 仓库名/标题兜底): matched tasks
// continue in place (uid backfilled, content refreshed); unmatched v2 tasks
// are created queued; unmatched live v1 tasks are superseded together with
// their active attempt, so the projection gate stops their results.
func migrateTasksTx(ctx context.Context, tx pgx.Tx, organizationID, projectID, planID string,
	incoming []TaskSnapshot) (created, superseded int, err error) {
	rows, err := tx.Query(ctx, `SELECT `+taskColumns+` FROM public.tasks
		WHERE plan_id = $1 AND status <> 'superseded' FOR UPDATE`, planID)
	if err != nil {
		return 0, 0, err
	}
	current := []Task{}
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			rows.Close()
			return 0, 0, err
		}
		current = append(current, task)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}

	byUID := map[string]*Task{}
	byNatural := map[string]*Task{}
	for i := range current {
		t := &current[i]
		if t.TaskUID != "" {
			byUID[t.TaskUID] = t
		}
		byNatural[t.RepositoryID+"\x1f"+t.Title] = t
	}

	claimed := map[string]bool{}
	for _, in := range incoming {
		var match *Task
		if in.TaskUID != "" {
			match = byUID[in.TaskUID]
		}
		if match == nil {
			match = byNatural[in.RepositoryID+"\x1f"+in.Title]
		}
		if match == nil || claimed[match.ID] {
			if _, err := tx.Exec(ctx, `
				INSERT INTO public.tasks
				  (id, organization_id, project_id, plan_id, task_uid, repository_id, title, status)
				VALUES ($1, $2, $3, $4, $5, $6, $7, 'queued')`,
				newUUIDv4(), organizationID, projectID, planID, in.TaskUID,
				in.RepositoryID, in.Title); err != nil {
				return 0, 0, err
			}
			created++
			continue
		}
		claimed[match.ID] = true
		if _, err := tx.Exec(ctx, `
			UPDATE public.tasks
			SET task_uid = $2, repository_id = $3, title = $4
			WHERE id = $1`, match.ID, in.TaskUID, in.RepositoryID, in.Title); err != nil {
			return 0, 0, err
		}
	}
	for _, t := range current {
		if claimed[t.ID] {
			continue
		}
		if _, err := tx.Exec(ctx, `
			UPDATE public.tasks SET status = 'superseded' WHERE id = $1`, t.ID); err != nil {
			return 0, 0, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE public.task_assignments
			SET attempt_state = 'superseded', finished_at = now()
			WHERE task_id = $1 AND attempt_state <> 'superseded'`, t.ID); err != nil {
			return 0, 0, err
		}
		superseded++
	}
	return created, superseded, nil
}

func jsonMarshal(v any) ([]byte, error) {
	encoded, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encode json: %w", err)
	}
	return encoded, nil
}

func jsonUnmarshalInto(raw []byte, dest any) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return fmt.Errorf("decode json: %w", err)
	}
	return nil
}

// isUniqueViolation reports PostgreSQL unique-constraint violations (23505) —
// the task_uid partial index can still fire under concurrent/dirty data; the
// caller maps it to ErrConflict for an explainable failure (审查 #4).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
