package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
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
	repos, err := selectedTierNames(st.EffectiveTiers)
	if err != nil {
		return nil, err
	}
	if err := validateRepositories(ctx, tx, st, repos); err != nil {
		return nil, err
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
		"INSERT INTO public.plans (id, project_id, plan_version, requirement_text, specs, task_dag, execution_batches, revisions, created_by_agent_id, issue_id)"+
			" VALUES (gen_random_uuid(), $1, 'v1', $2, '{}'::jsonb, $3::jsonb, '[]'::jsonb, '[]'::jsonb, $4, $5) RETURNING id::text",
		st.ProjectID, st.RequirementText, string(planJSON), createdByAgent, issueID).Scan(&planID)
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
			if adjustment.Tier != "required" && adjustment.Tier != "maybe" && adjustment.Tier != "excluded" {
				return nil, ErrConflict
			}
			// 2026-09-20：把某个仓库调成「排除」不产生改动，越界也无害 ——
			// 要求它必须在范围内，等于让人没法把一个越界候选赶出去。
			if adjustment.Tier != "excluded" {
				if err := validateRepositories(ctx, tx, st, []string{adjustment.Repository}); err != nil {
					return nil, err
				}
			}
			st.EffectiveTiers = applyAdjustment(st.EffectiveTiers, adjustment)
		}
		// 同上：只校验**真会被改动的那两档**。排除档越界不该让审批 409 ——
		// 线上实测就是这条把「批准分档」按钮点成了永远 409。
		if err := validateRepositories(ctx, tx, st, includedTierNames(st.EffectiveTiers)); err != nil {
			return nil, err
		}
		if !tiersHaveSelection(st.EffectiveTiers) {
			return nil, fmt.Errorf("%w：本次没有任何仓库被纳入改动（全部为「排除」）。请把至少一个仓库调整为「必需」或「可能」后再确认", ErrNoRepositories)
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

// includedTierNames 只取**真会被改动**的那两档（required / maybe）。
//
// 范围校验用它而不是 tierNames：排除档不进计划、不进任务，一个越界的候选被
// 排除掉恰恰是正确结论，不该反过来把整步卡死（2026-09-20 线上实测）。
func includedTierNames(tiers []any) []string {
	names := []string{}
	for _, tierAny := range tiers {
		tier, ok := tierAny.(map[string]any)
		if !ok {
			continue
		}
		switch tier["tier"] {
		case "required", "maybe":
		default:
			continue
		}
		if name, ok := tier["repository"].(string); ok && name != "" {
			names = append(names, name)
		}
	}
	return names
}

// tiersHaveSelection reports whether the effective tiers leave anything to do.
// "excluded" everywhere means an empty plan would follow — refuse it upstream.
func tiersHaveSelection(tiers []any) bool {
	for _, tierAny := range tiers {
		tier, ok := tierAny.(map[string]any)
		if !ok {
			continue
		}
		switch tier["tier"] {
		case "required", "maybe":
			return true
		}
	}
	return false
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
	// 老快照里的 plan_id 是 "iss_…:plan:v1" 这种非 uuid 字符串（见 newPlanID 的
	// 注释）。tasks/plan_steps 的 plan_id 是 uuid 列，所以这里**确定性地**折算成
	// 合法 uuid 并写回快照 —— 界面上的「计划 v1」与任务树此后指同一把 id。
	if !isUUID(planID) {
		version, _ := st.Plan["plan_version"].(string)
		if strings.TrimSpace(version) == "" {
			version = "v1"
		}
		planID = newPlanID(issueID, version)
		st.Plan["plan_id"] = planID
	}
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
	if err := validateRepositories(ctx, tx, st, repositories); err != nil {
		return nil, err
	}
	approved, err := selectedTierNames(st.EffectiveTiers)
	if err != nil {
		return nil, err
	}
	approvedSet := map[string]bool{}
	for _, name := range approved {
		approvedSet[name] = true
	}
	if st.Approval["state"] != "approved" || len(approvedSet) != len(repositories) {
		return nil, fmt.Errorf("%w: 审批范围已变化，请重新生成计划", ErrConflict)
	}
	for _, name := range repositories {
		if !approvedSet[name] {
			return nil, fmt.Errorf("%w: 审批范围已变化，请重新生成计划", ErrConflict)
		}
	}
	var orgID string
	if err := tx.QueryRow(ctx, `SELECT organization_id::text FROM repomesh_projects.projects WHERE id=$1`, st.ProjectID).Scan(&orgID); err != nil {
		return nil, err
	}
	// 2026-09-20：物化采用 **agent 拆出来的任务 DAG**（plan.tasks），不再按仓库
	// 模板拼一句"Implement changes for X"。任务的标题/指令/验收标准都是 Repository
	// Leader 写的 —— 执行面拿到的才是它真正规划的东西。
	// agent 没给 tasks 的老快照（或仓库清单为空）仍按仓库回退，不假装有 DAG。
	type plannedTask struct {
		Repository  string
		Title       string
		Instruction string
		Acceptance  string
	}
	planned := []plannedTask{}
	if raw, ok := st.Plan["tasks"].([]any); ok && len(raw) > 0 {
		for _, entry := range raw {
			task, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			repo, _ := task["repository"].(string)
			title, _ := task["title"].(string)
			if strings.TrimSpace(repo) == "" || strings.TrimSpace(title) == "" {
				continue
			}
			instruction, _ := task["instruction"].(string)
			acceptance, _ := task["acceptance"].(string)
			planned = append(planned, plannedTask{
				Repository: repo, Title: title, Instruction: instruction, Acceptance: acceptance,
			})
		}
	}
	if len(planned) == 0 {
		for _, repo := range repositories {
			planned = append(planned, plannedTask{
				Repository: repo,
				Title:      "Implement changes for " + repo,
				Instruction: "针对需求「" + st.RequirementText + "」在仓库 " + repo +
					" 上实现所需改动，完成后提交变更说明。",
				Acceptance: "改动已提交并通过该仓库既有检查，附变更说明。",
			})
		}
	}
	// agent 的任务同样受**审批范围**约束：它只能改人批过的那些仓库。
	// 少了这一条，agent 就能在计划里塞一个没被审过的仓库，把范围审批架空。
	for _, planTask := range planned {
		if !approvedSet[planTask.Repository] {
			return nil, fmt.Errorf("%w: 计划里出现未经审批的仓库 %s，请重新生成计划",
				ErrConflict, planTask.Repository)
		}
	}
	taskIDs := []string{}
	for index, planTask := range planned {
		repo := planTask.Repository
		title := planTask.Title
		instruction := planTask.Instruction
		acceptance := planTask.Acceptance
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
			orgID, st.ProjectID, planID, planID+":"+repo+":"+strconv.Itoa(index), repo, title, instruction, acceptance,
			fmt.Sprintf(`{"issueId":%q}`, issueID), planID+":"+repo+":"+strconv.Itoa(index)).Scan(&taskID)
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
