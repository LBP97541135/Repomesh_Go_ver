package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5"

	"repomesh.local/repomesh/internal/decisionchain"
)

// ---- step 3: plan generation, approval, materialization ----

// Plan generates the execution plan from the approved tiers and stores it as
// the plans table row (plan snapshot). Requires approval state approved.
func (s *Service) Plan(ctx context.Context, issueID, agentID, idempotencyKey string) (map[string]any, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	st, err := s.load(ctx, tx, issueID)
	if err != nil {
		return nil, err
	}
	if st == nil || st.Classification == nil {
		return nil, fmt.Errorf("%w: classification has not run", ErrConflict)
	}
	state, _ := st.Approval["state"].(string)
	if state != "approved" {
		return nil, fmt.Errorf("%w: tier approval must be approved before plan generation", ErrConflict)
	}
	if receipt, ok := replay(st, idempotencyKey); ok {
		tx.Rollback(ctx)
		return receipt, nil
	}
	required := []map[string]any{}
	maybe := []map[string]any{}
	if raw, _ := st.Classification["required"].([]any); raw != nil {
		for _, entry := range raw {
			if m, ok := entry.(map[string]any); ok {
				required = append(required, m)
			}
		}
	}
	if raw, _ := st.Classification["maybe"].([]any); raw != nil {
		for _, entry := range raw {
			if m, ok := entry.(map[string]any); ok {
				maybe = append(maybe, m)
			}
		}
	}
	repos := []string{}
	for _, entry := range required {
		if name, ok := entry["repository"].(string); ok {
			repos = append(repos, name)
		}
	}
	for _, entry := range maybe {
		if name, ok := entry["repository"].(string); ok {
			repos = append(repos, name)
		}
	}
	nodes := []any{}
	for _, name := range repos {
		nodes = append(nodes, map[string]any{"repository": name, "task_count": 1})
	}
	dag := map[string]any{"nodes": nodes, "edges": []any{}}
	plan := map[string]any{
		"repositories": repos, "dag": dag, "generated_at": time.Now().UTC(), "by_agent_id": agentID,
	}
	planJSON, _ := json.Marshal(plan)
	// created_by_agent_id 是 uuid 列:路由把 body 里的 null 解码成空字符串,
	// 直接传入会被 Postgres 拒绝(22P02),空值必须落成 SQL NULL。
	var createdByAgent any
	if agentID != "" {
		createdByAgent = agentID
	}
	var planID string
	err = tx.QueryRow(ctx,
		"INSERT INTO public.plans (id, project_id, plan_version, requirement_text, specs, task_dag, execution_batches, revisions, created_by_agent_id)"+
			" VALUES (gen_random_uuid(), $1, 'v1', $2, '{}'::jsonb, $3::jsonb, '[]'::jsonb, '[]'::jsonb, $4) RETURNING id::text",
		st.ProjectID, st.RequirementText, string(planJSON), createdByAgent).Scan(&planID)
	if err != nil {
		return nil, err
	}
	plan["plan_id"] = planID
	st.Plan = plan
	st.Integration = map[string]any{"task_dag_count": 1, "batch_count": 0, "contract_count": 0}
	receipt := map[string]any{"task_id": nil, "step": 4, "status": "accepted"}
	recordReceipt(st, idempotencyKey, receipt)
	if err := s.save(ctx, tx, st); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return receipt, nil
}

// Approval records the tier decision synchronously; adjustments overwrite the
// effective tiers and the request evidence_version must match the current
// classification fingerprint (409 on drift).
func (s *Service) Approval(ctx context.Context, issueID, agentID, idempotencyKey, decision, reason string, adjustments []Adjustment, evidenceVersion string) (map[string]any, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	st, err := s.load(ctx, tx, issueID)
	if err != nil {
		return nil, err
	}
	if st == nil || st.Classification == nil || st.EvidenceVersion == nil {
		return nil, fmt.Errorf("%w: classification has not run", ErrConflict)
	}
	if receipt, ok := replay(st, idempotencyKey); ok {
		tx.Rollback(ctx)
		return receipt, nil
	}
	if evidenceVersion != *st.EvidenceVersion {
		return nil, ErrDrifted
	}
	if decision != "approved" && decision != "changes_requested" {
		return nil, fmt.Errorf("discovery: invalid decision %q", decision)
	}
	now := time.Now().UTC()
	approval := map[string]any{
		"state": decision, "evidence_version": evidenceVersion,
		"decided_by_agent_id": agentID, "reason": reason, "decided_at": now,
	}
	if decision == "approved" {
		for _, adjustment := range adjustments {
			st.EffectiveTiers = applyAdjustment(st.EffectiveTiers, adjustment)
		}
	}
	st.Approval = approval
	receipt := map[string]any{"task_id": nil, "step": 3, "status": "accepted"}
	recordReceipt(st, idempotencyKey, receipt)
	if err := s.save(ctx, tx, st); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	s.recordDecision(ctx, st, "approval:"+issueID+":"+idempotencyKey,
		decisionchain.StepIntegration, tierStatus(decision), "分级审批",
		map[string]any{"evidence_version": evidenceVersion, "reason": reason},
		tierNames(st.EffectiveTiers))
	return receipt, nil
}

// tierStatus maps an approval decision onto the decision-chain status set.
func tierStatus(decision string) decisionchain.DecisionStatus {
	if decision == "approved" {
		return decisionchain.StatusConfirmed
	}
	return decisionchain.StatusChangesRequested
}

// tierNames pulls the repository names out of the effective tiers.
func tierNames(tiers []any) []string {
	names := []string{}
	for _, tierAny := range tiers {
		tier, ok := tierAny.(map[string]any)
		if !ok {
			continue
		}
		if name, ok := tier["repository"].(string); ok && name != "" {
			names = append(names, name)
		}
	}
	return names
}

// recordDecision writes one decision node, fail-open (方案清单 F3, same as
// the scan scope seam): toggle-off is silent, anything else logs to stderr
// and never fails the discovery step.
func (s *Service) recordDecision(ctx context.Context, st *State, eventKey string,
	step decisionchain.DecisionStep, status decisionchain.DecisionStatus,
	action string, contextRef map[string]any, repositories []string) {
	if s.decisions == nil {
		return
	}
	err := s.decisions.Record(context.WithoutCancel(ctx), decisionchain.Event{
		Requirement:    st.RequirementText,
		Actor:          "discovery",
		ActorType:      "service",
		IdempotencyKey: eventKey,
		RepositoryIDs:  repositories,
		ProjectID:      st.ProjectID,
		Step:           step,
		Status:         status,
		Action:         action,
		ContextRef:     contextRef,
	})
	if err != nil && !errors.Is(err, decisionchain.ErrDisabled) {
		fmt.Fprintf(os.Stderr, "discovery: decision chain record failed: %v (key=%s step=%s)\n",
			err, eventKey, string(step))
	}
}

// Adjustment is one tier change from the approval request.
type Adjustment struct {
	Repository string `json:"repository"`
	Tier       string `json:"tier"`
}

func applyAdjustment(tiers []any, adjustment Adjustment) []any {
	result := []any{}
	found := false
	for _, tierAny := range tiers {
		tier, ok := tierAny.(map[string]any)
		if !ok {
			continue
		}
		name, _ := tier["repository"].(string)
		if name == adjustment.Repository {
			original := tier["tier"]
			tier["adjusted"] = true
			tier["original_tier"] = original
			tier["tier"] = adjustment.Tier
			found = true
		}
		result = append(result, tier)
	}
	if !found {
		result = append(result, map[string]any{
			"repository": adjustment.Repository, "tier": adjustment.Tier,
			"adjusted": true, "original_tier": nil,
		})
	}
	return result
}

// Materialize converts the generated plan into executable rows: tasks for
// the DAG scheduler. Re-runs after a failure complete the remainder
// (reentrancy), guarded by the plan_id receipt.
func (s *Service) Materialize(ctx context.Context, issueID, agentID, idempotencyKey string) (map[string]any, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	st, err := s.load(ctx, tx, issueID)
	if err != nil {
		return nil, err
	}
	if st == nil || st.Plan == nil {
		return nil, fmt.Errorf("%w: plan has not been generated", ErrConflict)
	}
	if receipt, ok := replay(st, idempotencyKey); ok {
		tx.Rollback(ctx)
		if status, _ := receipt["status"].(string); status == "replayed" {
			if st.Materialization != nil {
				if prev, ok := st.Materialization["receipt"].(map[string]any); ok {
					return prev, nil
				}
			}
		}
		return receipt, nil
	}
	planID, _ := st.Plan["plan_id"].(string)
	reposAny, _ := st.Plan["repositories"].([]any)
	repositories := []string{}
	for _, repoAny := range reposAny {
		if repo, ok := repoAny.(string); ok {
			repositories = append(repositories, repo)
		}
	}
	if planID == "" || len(repositories) == 0 {
		st.Materialization = map[string]any{
			"status": "failed", "at": time.Now().UTC(), "by_agent_id": agentID,
			"error": "plan is empty or missing plan_id", "plan_id": nil,
		}
		if err := s.save(ctx, tx, st); err != nil {
			return nil, err
		}
		tx.Commit(ctx)
		return nil, fmt.Errorf("%w: plan is empty or missing plan_id", ErrConflict)
	}
	taskIDs := []string{}
	for _, repo := range repositories {
		orgID, err := s.resolveOrgID(ctx, tx)
		if err != nil {
			return nil, err
		}
		title := "Implement changes for " + repo
		instruction := "针对需求「" + st.RequirementText + "」在仓库 " + repo +
			" 上实现所需改动，完成后提交变更说明。"
		acceptance := "改动已提交并通过该仓库既有检查，附变更说明。"
		var taskID string
		// tasks 真实列（0010/0021）：organization_id/project_id NOT NULL；
		// idempotency_key 与 task_uid 都用 planID:repo——前者保证重放幂等，
		// 后者满足 uq_tasks_task_uid(project_id, task_uid) 的全局唯一约束
		//（task_uid NOT NULL，不能用空串或 NULL），派发缝靠 plan_steps.content = tasks.title 关联。
		// C7 fix: source_ref carries the issue linkage. The dispatch ledger
		// (repomesh_execution.attempts) has an FK to (project_id, issue_id);
		// without this ref the coordinator cannot resolve the real issue and
		// every dispatch aborts with a placeholder foreign key.
		err = tx.QueryRow(ctx,
			"INSERT INTO public.tasks (id, organization_id, project_id, plan_id, task_uid, repository_id, title, instruction, acceptance, source_ref, idempotency_key)"+
				" VALUES (gen_random_uuid(), $1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7, $8, $9::jsonb, $10)"+
				" ON CONFLICT (idempotency_key) DO UPDATE SET title = public.tasks.title RETURNING id::text",
			orgID, st.ProjectID, planID, planID+":"+repo, repo, title, instruction, acceptance,
			fmt.Sprintf(`{"issueId":%q}`, issueID), planID+":"+repo).Scan(&taskID)
		if err != nil {
			return nil, err
		}
		stepNo := len(taskIDs) + 1
		// plan_steps 无唯一约束，物化失败重试会插重：按 content 判重。
		if _, err := tx.Exec(ctx,
			"INSERT INTO public.plan_steps (id, plan_id, step_no, content, assignee_role, depends_on, status)"+
				" SELECT gen_random_uuid(), $1::uuid, $2, $3, 'worker', '[]'::jsonb, 'pending'"+
				" WHERE NOT EXISTS (SELECT 1 FROM public.plan_steps WHERE plan_id=$1::uuid AND content=$3)",
			planID, stepNo, title); err != nil {
			return nil, err
		}
		taskIDs = append(taskIDs, taskID)
	}
	receipt := map[string]any{
		"plan_id": planID, "task_ids": taskIDs, "team_count": len(repositories),
		"repositories": repositories, "status": "materialized",
	}
	st.Materialization = map[string]any{
		"status": "materialized", "at": time.Now().UTC(), "by_agent_id": agentID,
		"error": nil, "plan_id": planID, "receipt": receipt,
	}
	if st.Idempotency == nil {
		st.Idempotency = map[string]any{}
	}
	st.Idempotency[idempotencyKey] = receipt
	if err := s.save(ctx, tx, st); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	s.recordDecision(ctx, st, "materialize:"+issueID+":"+idempotencyKey,
		decisionchain.StepTask, decisionchain.StatusConfirmed, "任务物化",
		map[string]any{"plan_id": planID, "task_ids": taskIDs}, repositories)
	return receipt, nil
}

// resolveOrgID returns the first organization's id, lazily creating the
// default organization when the table is empty (no production writer seeds
// it; tasks.organization_id is NOT NULL).
func (s *Service) resolveOrgID(ctx context.Context, tx pgx.Tx) (string, error) {
	var orgID string
	err := tx.QueryRow(ctx,
		"SELECT id::text FROM public.organizations ORDER BY created_at LIMIT 1").Scan(&orgID)
	if err == nil {
		return orgID, nil
	}
	if err != pgx.ErrNoRows {
		return "", err
	}
	err = tx.QueryRow(ctx,
		"INSERT INTO public.organizations (id, name) VALUES (gen_random_uuid(), '默认组织') RETURNING id::text").
		Scan(&orgID)
	return orgID, err
}

// TaskView is the GET /issues/{id}/discovery/tasks/{taskId} polling view.
// The Go implementation executes steps synchronously, so a task id resolves
// immediately to its terminal state (contract 4.5 shape preserved).
func (s *Service) TaskView(ctx context.Context, issueID, taskID string) (map[string]any, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	st, err := s.load(ctx, tx, issueID)
	if err != nil {
		return nil, err
	}
	if st == nil || st.Idempotency == nil {
		return nil, pgx.ErrNoRows
	}
	receipt, ok := st.Idempotency[taskID].(map[string]any)
	if !ok {
		return nil, pgx.ErrNoRows
	}
	step, _ := receipt["step"].(int)
	if step == 0 {
		if f, ok := receipt["step"].(float64); ok {
			step = int(f)
		}
	}
	status, _ := receipt["status"].(string)
	viewStatus := "succeeded"
	if status == "replayed" {
		viewStatus = "succeeded"
	}
	return map[string]any{
		"task_id": taskID, "issue_id": issueID, "step": step,
		"status": viewStatus, "progress": map[string]any{"done": 1, "total": 1, "label": "同步执行"},
		"error": nil, "started_at": st.UpdatedAt, "finished_at": st.UpdatedAt,
	}, nil
}
