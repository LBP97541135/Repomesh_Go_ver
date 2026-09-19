package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// PlanStatus is the collection-window state on the plan axis (协议 §2 步骤
// 3): ” = normal, 'deprecated' = window open, in-flight tasks continue.
// The version axis (plan_version 'v1'→'v2') supersedes implicitly on apply.
const (
	ReplanStateNormal     = ""
	ReplanStateDeprecated = "deprecated"
)

// ErrInvalidPlan reports a snapshot that fails mechanical validation.
var ErrInvalidPlan = errors.New("tasks: plan snapshot failed validation")

// ExecutionPlan mirrors the 47-table base's public.plans (0009) with the
// protocol's retrieval increment (0021: requirement_key, replan_state).
type ExecutionPlan struct {
	ID              string              `json:"id"`
	ProjectID       string              `json:"projectId"`
	PlanVersion     string              `json:"planVersion"` // 'v1','v2',…
	RequirementText string              `json:"requirementText"`
	RequirementKey  string              `json:"requirementKey"`
	ReplanState     string              `json:"replanState"`
	Batches         [][]string          `json:"batches"`
	DAG             map[string][]string `json:"dag"`
}

// ValidateBatches enforces the mechanical guarantees of a snapshot:
// at least one batch, no repository listed twice, and every DAG dependency
// positioned in the same or an earlier batch.
func ValidateBatches(batches [][]string, dag map[string][]string) error {
	if len(batches) == 0 {
		return fmt.Errorf("%w: plan has no batches", ErrInvalidPlan)
	}
	pos := map[string]int{}
	for i, batch := range batches {
		if len(batch) == 0 {
			return fmt.Errorf("%w: batch %d is empty", ErrInvalidPlan, i)
		}
		for _, repo := range batch {
			if _, dup := pos[repo]; dup {
				return fmt.Errorf("%w: repository %q listed twice", ErrInvalidPlan, repo)
			}
			pos[repo] = i
		}
	}
	for repo, deps := range dag {
		at, ok := pos[repo]
		if !ok {
			continue // 依赖图允许描述暂未纳入计划的仓库
		}
		for _, dep := range deps {
			depAt, ok := pos[dep]
			if !ok {
				return fmt.Errorf("%w: %q depends on %q which is not in the plan", ErrInvalidPlan, repo, dep)
			}
			if depAt > at {
				return fmt.Errorf("%w: %q depends on %q scheduled in a later batch", ErrInvalidPlan, repo, dep)
			}
		}
	}
	// 机械 DAG 校验补全（审查 #5）：跨批环已被上方晚位检查拒绝，这里再对
	// 计划内仓库做 DFS 找同批/邻批的有向环（A→B→A 等）。
	color := map[string]int{} // 1=在栈 2=完成
	var cyclic func(repo string) bool
	cyclic = func(repo string) bool {
		color[repo] = 1
		for _, dep := range dag[repo] {
			if _, planned := pos[dep]; !planned {
				continue
			}
			switch color[dep] {
			case 1:
				return true
			case 0:
				if cyclic(dep) {
					return true
				}
			}
		}
		color[repo] = 2
		return false
	}
	for repo := range pos {
		if color[repo] == 0 && cyclic(repo) {
			return fmt.Errorf("%w: dependency cycle involving %q", ErrInvalidPlan, repo)
		}
	}
	return nil
}

// DiffRepositories returns what a snapshot revision adds and removes at the
// repository level (the task-axis migration keys off this in T2.5).
func DiffRepositories(oldBatches, newBatches [][]string) (added, removed []string) {
	oldSet := map[string]bool{}
	for _, batch := range oldBatches {
		for _, repo := range batch {
			oldSet[repo] = true
		}
	}
	newSet := map[string]bool{}
	for _, batch := range newBatches {
		for _, repo := range batch {
			newSet[repo] = true
			if !oldSet[repo] {
				added = append(added, repo)
			}
		}
	}
	for _, batch := range oldBatches {
		for _, repo := range batch {
			if !newSet[repo] {
				removed = append(removed, repo)
			}
		}
	}
	return added, removed
}

const planColumns = `id, project_id::text, plan_version, requirement_text,
  COALESCE(requirement_key, ''), replan_state, execution_batches, task_dag`

// decodePlanDAG 容忍 task_dag 的两种 as-built 形状（2026-09-19 修）：
//
//  1. 简单邻接表 {"1": ["2","3"], …}          ← PlanWrite 写入的形状
//  2. 富描述     {"dag":{"nodes":[{repository,task_count}],"edges":[]},
//     "by_agent_id":…,"generated_at":…,"repositories":[…]}  ← 物化写入的形状
//
// 旧代码把两种都按 1) 解，遇到 2) 就 `cannot unmarshal object into Go value of
// type []string` 整条失败——GET /plans/{id}/tasks 因此每 60 秒报一次 503。
// 这里两种都收：**富形状没有邻接语义，DAG 留空**（绝不从 nodes 反推依赖，那是编造）。
func decodePlanDAG(raw []byte) (map[string][]string, error) {
	if len(raw) == 0 {
		return map[string][]string{}, nil
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, fmt.Errorf("decode json: %w", err)
	}
	if _, isRich := probe["dag"]; isRich {
		return map[string][]string{}, nil
	}
	if _, isRich := probe["by_agent_id"]; isRich {
		return map[string][]string{}, nil
	}
	out := map[string][]string{}
	for key, value := range probe {
		var deps []string
		if err := json.Unmarshal(value, &deps); err != nil {
			continue // 逐键跳过：单个键形状怪不该让整条计划读不出来
		}
		out[key] = deps
	}
	return out, nil
}

func scanPlan(row pgx.Row) (ExecutionPlan, error) {
	var (
		p            ExecutionPlan
		batches, dag []byte
	)
	err := row.Scan(&p.ID, &p.ProjectID, &p.PlanVersion, &p.RequirementText,
		&p.RequirementKey, &p.ReplanState, &batches, &dag)
	if err != nil {
		return ExecutionPlan{}, err
	}
	if err := jsonUnmarshalInto(batches, &p.Batches); err != nil {
		return ExecutionPlan{}, err
	}
	decodedDAG, err := decodePlanDAG(dag)
	if err != nil {
		return ExecutionPlan{}, err
	}
	p.DAG = decodedDAG
	if p.DAG == nil {
		p.DAG = map[string][]string{}
	}
	return p, nil
}

// PlanWrite is the v1 creation shape.
type PlanWrite struct {
	ProjectID       string
	RequirementText string
	RequirementKey  string
	Batches         [][]string
	DAG             map[string][]string
}

// CreatePlan inserts the v1 snapshot (public.plans, plan_version 'v1').
func (p *PostgresStore) CreatePlan(ctx context.Context, w PlanWrite) (ExecutionPlan, error) {
	if err := ValidateBatches(w.Batches, w.DAG); err != nil {
		return ExecutionPlan{}, err
	}
	batchesJSON, err := jsonMarshal(w.Batches)
	if err != nil {
		return ExecutionPlan{}, err
	}
	dagJSON, err := jsonMarshal(w.DAG)
	if err != nil {
		return ExecutionPlan{}, err
	}
	id := newUUIDv4()
	_, err = p.pool.Exec(ctx, `
		INSERT INTO public.plans
		  (id, project_id, plan_version, requirement_text, requirement_key,
		   replan_state, execution_batches, task_dag)
		VALUES ($1, $2, 'v1', $3, $4, '', $5::jsonb, $6::jsonb)`,
		id, w.ProjectID, w.RequirementText, w.RequirementKey, batchesJSON, dagJSON)
	if err != nil {
		return ExecutionPlan{}, err
	}
	return scanPlan(p.pool.QueryRow(ctx,
		`SELECT `+planColumns+` FROM public.plans WHERE id = $1`, id))
}

// GetPlan reads one plan; pgx.ErrNoRows when absent.
func (p *PostgresStore) GetPlan(ctx context.Context, id string) (*ExecutionPlan, error) {
	plan, err := scanPlan(p.pool.QueryRow(ctx,
		`SELECT `+planColumns+` FROM public.plans WHERE id = $1`, id))
	if err != nil {
		return nil, err
	}
	return &plan, nil
}

// MarkDeprecated opens the collection window (协议 §2 步骤 3): replan_state
// ” → 'deprecated' without touching the version axis; in-flight tasks
// continue. Idempotent — a second call leaves the window as is.
func (p *PostgresStore) MarkDeprecated(ctx context.Context, planID string) error {
	tag, err := p.pool.Exec(ctx, `
		UPDATE public.plans SET replan_state = 'deprecated'
		WHERE id = $1 AND replan_state = ''`, planID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		// Already deprecated or superseded: the window stays as is.
		var state string
		err = p.pool.QueryRow(ctx,
			`SELECT replan_state FROM public.plans WHERE id = $1`, planID).Scan(&state)
		if err == nil && state != ReplanStateDeprecated {
			return fmt.Errorf("%w: plan is %q, not an open window", ErrConflict, state)
		}
	}
	return nil
}
