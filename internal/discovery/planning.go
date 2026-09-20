package discovery

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"repomesh.local/repomesh/internal/decisionchain"
)

// ───────────── 规划期的真实 agent 派发 ─────────────
//
// 2026-09-20 审计：发现链五步此前**全部由 Go 代码算**（① 固定词表判定、
// ② 规则召回、③ 规则分档、④ 模板拼任务），`created_by_agent_id` 只是个图章 ——
// agent 一行代码都没执行。而 infra 的 Governed AgentTeams Flow 要求：
// Scope 由 Organization Leader 提、Specification 与 Task DAG 由 Repository Leader 写，
// 后端只做**校验、门禁与记录**（"Agent prompts are guidance; the role check,
// immutable context bundle, path policy, isolated workspace, and Runner commit
// policy are the enforcement boundaries"）。
//
// 这个文件是那条要求的落点：把「要 agent 产出什么」写成一份**带输出 schema 的
// 提示词**，产物回来后校验、落库、记进决策链。没有兜底模拟 —— 拿不到合格产物
// 就是失败，失败原因如实上屏。

// 会被派发给 agent 的发现链步骤（3=分档审批、5=物化确认是人工门，不派发）。
const (
	PlanningAnalysis   = 1
	PlanningCandidates = 2
	PlanningPlan       = 4
	// PlanningReplan 是收集窗开完之后的**重排步**（协议 §2 步骤 3-5 的下半段）：
	// 人在执行中打断并引入新仓库，收集窗的受影响集合上由 Leader 产出 v2。
	PlanningReplan = 6
)

// PlanningArtifactFile 是 agent 必须写出的产物文件名（工作区根下）。
const PlanningArtifactFile = "planning-artifact.json"

// PlanningRoleFor 返回该步的产出角色与技能（infra 角色表 + 技能库种子）。
func PlanningRoleFor(step int) (role, skillID string) {
	switch step {
	case PlanningAnalysis:
		return "organization_leader", "project-intake"
	case PlanningCandidates:
		return "organization_leader", "cross-repo-planning"
	case PlanningReplan:
		return "repository_leader", "task-decomposition"
	case PlanningPlan:
		return "repository_leader", "task-decomposition"
	}
	return "", ""
}

// PlanningSchemaFor 返回该步**必须**产出的 JSON 形状（给 agent 看的那份）。
//
// 没有 schema，agent 会写一段散文回来，机器读不了；schema 写宽了，界面拿到的是
// 一堆缺字段的对象。所以这里逐字段写死，并要求"只输出这个 JSON"。
func PlanningSchemaFor(step int) string {
	switch step {
	case PlanningAnalysis:
		return `{
  "dimensions": [
    {"name": "业务场景", "covered": true, "note": "一句话说明这段话里哪句覆盖了它"},
    {"name": "行为描述", "covered": true, "note": "..."},
    {"name": "变更类型", "covered": true, "note": "..."},
    {"name": "技术约束", "covered": false, "note": "这段话里没有提到的，就说没有"}
  ],
  "questions": ["只对 covered=false 的维度各问一句，最多 4 句"],
  "extracted_keywords": ["从需求里抽出的关键词，最多 12 个"],
  "analyzed_requirement": "把需求改写成一句可执行的话（保留原意，不要加戏）"
}`
	case PlanningCandidates:
		return `{
  "candidates": [
    {"repository": "owner/name", "tier": "required|maybe|excluded", "score": 0.0,
     "reason": "为什么这个仓库要改/不改，引用你看到的证据"}
  ]
}`
	case PlanningPlan:
		return `{
  "repositories": ["owner/name", "..."],
  "tasks": [
    {"repository": "owner/name", "title": "任务标题", "instruction": "给执行者的完整指令",
     "acceptance": "怎么算做完（可验证的一句话）", "depends_on": ["上游任务的 title"]}
  ]
}`
	case PlanningReplan:
		return `{
  "repositories": ["owner/name", "..."],
  "tasks": [
    {"repository": "owner/name", "title": "任务标题", "instruction": "给执行者的完整指令",
     "acceptance": "怎么算做完（可验证的一句话）", "depends_on": ["上游任务的 title"]}
  ],
  "reason": "为什么这样重排：引用受影响仓库集合与你保留/新增/删除的理由"
}`
	}
	return "{}"
}

// PlanningPrompt 组装派给 agent 的完整提示词。
//
// 三样东西缺一不可：
//  1. **真技能文档**（SKILL.md 原文）—— 在 prompt 里另抄一段"你该做什么"就是
//     第二份真相，技能库改了 agent 拿到的还是旧话术；
//  2. 这次要处理的输入（需求原文、仓库名片）；
//  3. 输出 schema + 落盘要求（产物写到工作区的 planning-artifact.json）。
func PlanningPrompt(step int, requirement string, repoSummaries []byte, skillDoc string, prior ReplanContext) string {
	role, skillID := PlanningRoleFor(step)
	var b strings.Builder
	fmt.Fprintf(&b, "你是 RepoMesh 的 %s。\n\n", roleLabel(role))
	if strings.TrimSpace(skillDoc) != "" {
		fmt.Fprintf(&b, "## 你的技能（%s，技能库原文）\n\n%s\n\n", skillID, strings.TrimSpace(skillDoc))
	} else {
		// 认不出技能时如实说，不编一份假文档。
		fmt.Fprintf(&b, "## 技能\n\n（技能库中没有找到 %s，请按角色职责行事。）\n\n", skillID)
	}
	fmt.Fprintf(&b, "## 本次需求\n\n%s\n\n", strings.TrimSpace(requirement))
	if len(repoSummaries) > 0 {
		fmt.Fprintf(&b, "## 候选仓库名片（扫描产出，含目录/依赖/近期提交）\n\n```json\n%s\n```\n\n", string(repoSummaries))
	}
	if step == PlanningReplan {
		// 重排的输入必须包含**上一版计划**与受影响集合：agent 是在 v1 上改，
		// 不是从零猜一遍 —— 否则每次重排都会把已经定好的批次与任务丢掉。
		if len(prior.PlanJSON) > 0 {
			fmt.Fprintf(&b, "## 上一版计划（%s，你正在它的基础上重排）\n\n```json\n%s\n```\n\n",
				prior.PlanVersion, string(prior.PlanJSON))
		}
		if len(prior.AffectedRepositories) > 0 {
			fmt.Fprintf(&b, "## 本次受影响仓库集合（人工打断的判定结果，扫描已就绪）\n\n%s\n\n",
				strings.Join(prior.AffectedRepositories, ", "))
		}
		b.WriteString("## 重排要求\n\n上一版里已存在、且不受影响的任务**原样保留**（标题保持一致，这样任务身份能在换代时对上）；受影响与新引入的仓库要给出完整任务（标题/指令/验收标准）。\n\n")
	}
	fmt.Fprintf(&b, "## 必须产出的结果\n\n只输出下面这个 JSON（不要解释、不要 markdown 代码块外的文字）：\n\n%s\n\n", PlanningSchemaFor(step))
	fmt.Fprintf(&b, "把这份 JSON 写入当前目录下的 `%s`，然后结束。\n", PlanningArtifactFile)
	b.WriteString("写不出合格内容时，也把 `{}` 写进文件并在 JSON 的 \"error\" 字段里说明原因 —— 不要留空文件。\n")
	return b.String()
}

func roleLabel(role string) string {
	switch role {
	case "organization_leader":
		// 2026-09-20 用户更正命名：总领导叫 **Manager**，仓库领导叫 **Leader**。
		// 此前两个都叫 "Leader"（组织 Leader / 仓库 Leader），界面上分不清谁是谁。
		return "Manager（总领导：负责需求范围与跨仓库规划）"
	case "repository_leader":
		return "Leader（仓库领导：负责仓库规格与任务拆解）"
	case "worker":
		return "Worker（负责在指定仓库内执行任务）"
	}
	return role
}

// ParsePlanningArtifact 校验 agent 产物并抽出要落库的字段。
//
// 校验是**结构性的**（这一步该有什么字段），不是内容性的 —— 内容对不对由人工门
// （③ 分档审批 / ⑤ 物化确认）与复核角色判，后端不替 agent 做业务判断。
func ParsePlanningArtifact(step int, raw []byte) (map[string]any, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil, fmt.Errorf("产物为空文件")
	}
	// agent 有时会把 JSON 包在 ```json 里，也可能把**思考过程**一起写进产物文件
	// （2026-09-20 线上实测：MiniMax-M2 写出 `<think>…</think>` 后接 JSON，解析直接失败，
	// 界面显示"候选评分失败"）。推理不是产物的一部分：先剥思考块，再取第一个配平的
	// JSON 对象 —— 前后夹散文也救得回来。
	trimmed = stripReasoning(trimmed)
	trimmed = strings.TrimPrefix(trimmed, "```json")
	trimmed = strings.TrimPrefix(trimmed, "```")
	trimmed = strings.TrimSuffix(trimmed, "```")
	trimmed = strings.TrimSpace(trimmed)
	if candidate, ok := extractJSONObject(trimmed); ok {
		trimmed = candidate
	}
	var artifact map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(trimmed)), &artifact); err != nil {
		return nil, fmt.Errorf("产物不是合法 JSON：%w", err)
	}
	if msg, _ := artifact["error"].(string); strings.TrimSpace(msg) != "" {
		return nil, fmt.Errorf("agent 报告失败：%s", strings.TrimSpace(msg))
	}
	switch step {
	case PlanningAnalysis:
		if dims, _ := artifact["dimensions"].([]any); len(dims) == 0 {
			return nil, fmt.Errorf("缺少 dimensions（四维结论）")
		}
	case PlanningCandidates:
		if items, _ := artifact["candidates"].([]any); len(items) == 0 {
			return nil, fmt.Errorf("缺少 candidates（候选仓库分档）")
		}
	case PlanningPlan, PlanningReplan:
		if tasks, _ := artifact["tasks"].([]any); len(tasks) == 0 {
			return nil, fmt.Errorf("缺少 tasks（任务 DAG）")
		}
	}
	return artifact, nil
}

// ReplanContext 是重排步（第 6 步）的额外输入：上一版计划快照与受影响仓库集合。
// 其它步留空 —— 不为"统一"给不需要它的步硬塞上下文。
type ReplanContext struct {
	PlanVersion          string
	PlanJSON             []byte
	AffectedRepositories []string
}

// PlanningProvenance 是这次产物的出处（审计用：谁产的、用的哪把技能、哪个 run）。
type PlanningProvenance struct {
	Role      string
	SkillID   string
	RunID     string
	AgentKind string
}

// ApplyPlanningArtifact 把校验过的产物落进发现链状态。
//
// 只做两件事：**按既有形状填字段**（界面不用改）与**记进决策链**（谁产的、
// 哪把技能、哪个 run 都能追）。不做二次加工 —— 那等于把 agent 的结论再算一遍，
// 用户看到的就不是 agent 的结论了。
func (s *Service) ApplyPlanningArtifact(ctx context.Context, tx pgx.Tx, st *State, step int, artifact map[string]any, prov PlanningProvenance) error {
	switch step {
	case PlanningAnalysis:
		dimensions, _ := artifact["dimensions"].([]any)
		covered, missing := 0, []string{}
		for _, raw := range dimensions {
			dim, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if isCovered, _ := dim["covered"].(bool); isCovered {
				covered++
			} else if name, _ := dim["name"].(string); name != "" {
				missing = append(missing, name)
			}
		}
		total := len(dimensions)
		confidence := 0.0
		if total > 0 {
			confidence = float64(covered) / float64(total)
		}
		// sufficient 由**信息量**判（见 steps.go 的 minInformativeRunes）：
		// 词面覆盖只作提示。这里沿用同一条判据，避免两处标准打架。
		analyzed, _ := artifact["analyzed_requirement"].(string)
		if strings.TrimSpace(analyzed) == "" {
			analyzed = st.RequirementText
		}
		questions, _ := artifact["questions"].([]any)
		keywords, _ := artifact["extracted_keywords"].([]any)
		st.Analysis = map[string]any{
			"sufficient":           len([]rune(strings.TrimSpace(analyzed))) >= minInformativeRunes,
			"informative":          len([]rune(strings.TrimSpace(analyzed))) >= minInformativeRunes,
			"confidence":           confidence,
			"missing_dimensions":   missing,
			"dimensions":           dimensions,
			"questions":            questions,
			"extracted_keywords":   keywords,
			"answers":              nil,
			"analyzed_requirement": analyzed,
			"forced_continue":      nil,
			"error":                nil,
			"ran_at":               time.Now().UTC(),
			"by_agent_id":          nil,
			"producer": map[string]any{
				"role": prov.Role, "skill_id": prov.SkillID,
				"run_id": prov.RunID, "agent_kind": prov.AgentKind,
			},
		}
		text := analyzed
		st.AnalyzedText = &text
	case PlanningCandidates:
		// 把 agent 的分档结论映射成界面既有形状（items[] 带 score/rationale）。
		// `agent_tier` 原样保留：③ 分档审批据此**直接采用 agent 的判断**，
		// 不再用分数阈值二次推断 —— 那等于把 agent 的结论又算了一遍。
		raw, _ := artifact["candidates"].([]any)
		items := make([]any, 0, len(raw))
		for _, entry := range raw {
			candidate, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			name, _ := candidate["repository"].(string)
			if strings.TrimSpace(name) == "" {
				continue
			}
			score, _ := candidate["score"].(float64)
			reason, _ := candidate["reason"].(string)
			tier, _ := candidate["tier"].(string)
			item := map[string]any{
				"repository_name": name,
				"score":           score,
				"rationale":       reason,
				"agent_tier":      strings.ToLower(strings.TrimSpace(tier)),
				"auto_card":       true,
			}
			if repositoryID := s.repositoryIDByName(ctx, tx, name); repositoryID != "" {
				item["repository_id"] = repositoryID
			}
			items = append(items, item)
		}
		st.Candidates = map[string]any{
			"items":     items,
			"llm_used":  true,
			"pool_size": len(items),
			"ran_at":    time.Now().UTC(),
			"producer": map[string]any{
				"role": prov.Role, "skill_id": prov.SkillID,
				"run_id": prov.RunID, "agent_kind": prov.AgentKind,
			},
		}
	case PlanningPlan:
		tasks, _ := artifact["tasks"].([]any)
		repositories, _ := artifact["repositories"].([]any)
		if len(repositories) == 0 {
			// 仓库清单缺失时从任务里反推（agent 偶尔只给 tasks）。
			seen := map[string]bool{}
			for _, entry := range tasks {
				task, ok := entry.(map[string]any)
				if !ok {
					continue
				}
				if name, _ := task["repository"].(string); name != "" && !seen[name] {
					seen[name] = true
					repositories = append(repositories, name)
				}
			}
		}
		planID := newPlanID(st.IssueID, "v1")
		st.Plan = map[string]any{
			"plan_id":      planID,
			"plan_version": "v1",
			"repositories": repositories,
			"tasks":        tasks,
			"ran_at":       time.Now().UTC(),
			"producer": map[string]any{
				"role": prov.Role, "skill_id": prov.SkillID,
				"run_id": prov.RunID, "agent_kind": prov.AgentKind,
			},
		}
		// integration 是界面读的"计划已就绪"信号（步进器与物化卡都看它）。
		st.Integration = map[string]any{
			"task_dag_count": len(tasks),
			"batch_count":    1,
			"contract_count": 0,
			"producer": map[string]any{
				"role": prov.Role, "skill_id": prov.SkillID,
				"run_id": prov.RunID, "agent_kind": prov.AgentKind,
			},
		}
	case PlanningReplan:
		tasks, _ := artifact["tasks"].([]any)
		repositories, _ := artifact["repositories"].([]any)
		if len(repositories) == 0 {
			seen := map[string]bool{}
			for _, entry := range tasks {
				task, ok := entry.(map[string]any)
				if !ok {
					continue
				}
				if name, _ := task["repository"].(string); name != "" && !seen[name] {
					seen[name] = true
					repositories = append(repositories, name)
				}
			}
		}
		// v2 是**同一把计划**的新版本，不是另开一把计划：plan_id 必须沿用上一版。
		planID, _ := st.Plan["plan_id"].(string)
		if strings.TrimSpace(planID) == "" {
			return fmt.Errorf("discovery: 没有上一版计划快照，无法定位要换代的计划（重排被拒）")
		}
		if s.replanner == nil {
			// 端口未接线：产物读到了但没有 v2 落库。**如实失败** —— 报成功而计划
			// 一动没动，比能力缺失更坏（人会以为已经生效）。
			return fmt.Errorf("discovery: 重排端口未接线，v2 未落库")
		}
		runContext, err := planningRunContextTx(ctx, tx, st.IssueID, PlanningReplan)
		if err != nil {
			return err
		}
		repoNames := []string{}
		for _, raw := range repositories {
			if name, ok := raw.(string); ok && strings.TrimSpace(name) != "" {
				repoNames = append(repoNames, name)
			}
		}
		replanTasks, dag, err := replanTasksFromArtifact(tasks)
		if err != nil {
			return err
		}
		reason, _ := artifact["reason"].(string)
		upstream, _ := runContext["upstream_ref"].(string)
		result, err := s.replanner.ApplyReplan(ctx, ReplanRequest{
			PlanID:         planID,
			Actor:          prov.Role,
			Reason:         reason,
			UpstreamRef:    upstream,
			IdempotencyKey: "replan:" + prov.RunID,
			Repositories:   repoNames,
			Tasks:          replanTasks,
			DAG:            dag,
		})
		if err != nil {
			return err
		}
		previousVersion, _ := st.Plan["plan_version"].(string)
		st.Plan = map[string]any{
			"plan_id":        planID,
			"plan_version":   result.ResultVersion,
			"repositories":   repositories,
			"tasks":          tasks,
			"replanned_from": previousVersion,
			"replan_reason":  reason,
			"ran_at":         time.Now().UTC(),
			"producer": map[string]any{
				"role": prov.Role, "skill_id": prov.SkillID,
				"run_id": prov.RunID, "agent_kind": prov.AgentKind,
			},
		}
		st.Integration = map[string]any{
			"task_dag_count":   len(tasks),
			"batch_count":      1,
			"contract_count":   0,
			"revision":         result.ResultVersion,
			"created_tasks":    result.CreatedTasks,
			"superseded_tasks": result.SupersededTasks,
			"producer": map[string]any{
				"role": prov.Role, "skill_id": prov.SkillID,
				"run_id": prov.RunID, "agent_kind": prov.AgentKind,
			},
		}
	default:
		return fmt.Errorf("discovery: step %d 不是可派发的规划步", step)
	}
	// 决策链：这一步的结论由哪个角色、哪把技能、哪个 run 产出 —— 审计的主键。
	stepName := map[int]string{PlanningAnalysis: "analysis", PlanningCandidates: "candidates", PlanningPlan: "plan", PlanningReplan: "replan"}[step]
	s.recordDecision(ctx, st, fmt.Sprintf("planning:%s:%s:%s", st.IssueID, stepName, prov.RunID),
		planningDecisionStep(step), decisionchain.StatusConfirmed,
		"由 "+prov.Role+" 产出（技能 "+prov.SkillID+"）",
		map[string]any{"run_id": prov.RunID, "role": prov.Role, "skill_id": prov.SkillID},
		planningRepositories(artifact))
	return nil
}

// planningDecisionStep 把规划步映到决策链的五个步骤上（决策链的步骤集是固定的
// 五步，不为规划新开一类）：① 分析与 ② 候选都属于"分类"这一段，④ 计划属于"任务"。
func planningDecisionStep(step int) decisionchain.DecisionStep {
	if step == PlanningPlan {
		return decisionchain.StepTask
	}
	return decisionchain.StepClassification
}

// planningRepositories 从产物里抽出涉及的仓库名（决策链的 affected_repositories）。
func planningRepositories(artifact map[string]any) []string {
	names := []string{}
	if repos, ok := artifact["repositories"].([]any); ok {
		for _, raw := range repos {
			if name, ok := raw.(string); ok && name != "" {
				names = append(names, name)
			}
		}
	}
	if items, ok := artifact["candidates"].([]any); ok {
		for _, raw := range items {
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if tier, _ := item["tier"].(string); tier == "excluded" {
				continue
			}
			if name, _ := item["repository"].(string); name != "" {
				names = append(names, name)
			}
		}
	}
	if tasks, ok := artifact["tasks"].([]any); ok {
		for _, raw := range tasks {
			task, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if name, _ := task["repository"].(string); name != "" {
				names = append(names, name)
			}
		}
	}
	return names
}

// EnqueuePlanningRun 登记一次规划派发的**意图**（不派发）。
//
// 为什么拆成"入队"与"派发"两件事：web 进程没有 host-executor 的派发能力，
// 而 coordinator 才是那个有台账与工作区的人。web 只写意图，coordinator 每 tick
// 取一条 pending 去派发 —— 与发现链状态机由 coordinator 驱动是同一个形状。
//
// 幂等：同一 (issue, step) 已有 pending 就什么都不做（前端 driver 与 coordinator
// autohost 会同时推进同一条链，重复入队会把同一步派发两次）。
func (s *Service) EnqueuePlanningRun(ctx context.Context, issueID string, step int) error {
	return s.EnqueuePlanningRunWithContext(ctx, issueID, step, nil)
}

// EnqueuePlanningRunWithContext 带上**派发上下文**：重排步（第 6 步）需要把
// 上一版计划、受影响仓库集合与触发它的打断决策单随意图一起落库 —— web 只写
// 意图、coordinator 才派发，中间隔着进程边界，放内存就等于"重启后丢一半"。
func (s *Service) EnqueuePlanningRunWithContext(ctx context.Context, issueID string, step int, runContext map[string]any) error {
	role, skillID := PlanningRoleFor(step)
	if role == "" {
		return fmt.Errorf("discovery: step %d 不是可派发的规划步", step)
	}
	payload := "{}"
	if len(runContext) > 0 {
		encoded, err := json.Marshal(runContext)
		if err != nil {
			return fmt.Errorf("discovery: encode planning run context: %w", err)
		}
		payload = string(encoded)
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO repomesh_issues.planning_runs
		(id, issue_id, step, role, skill_id, state, context)
		SELECT $1::uuid, $2, $3, $4, $5, 'pending', $6::jsonb
		WHERE NOT EXISTS (
			SELECT 1 FROM repomesh_issues.planning_runs
			WHERE issue_id=$2 AND step=$3 AND state='pending')`,
		newPlanningID(), issueID, step, role, skillID, payload)
	if err != nil {
		return fmt.Errorf("discovery: enqueue planning run: %w", err)
	}
	return nil
}

// PlanningRunContext 读回该 issue 该步最近一次派发的上下文（{} = 没有）。
func (s *Service) PlanningRunContext(ctx context.Context, issueID string, step int) (map[string]any, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT COALESCE((
		SELECT context FROM repomesh_issues.planning_runs
		WHERE issue_id=$1 AND step=$2 ORDER BY created_at DESC LIMIT 1), '{}'::jsonb)`,
		issueID, step).Scan(&raw)
	if err != nil {
		return nil, fmt.Errorf("discovery: planning run context: %w", err)
	}
	out := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return out, nil
}

// ApplyPlanningRun 收产物的**事务入口**：读状态 → 应用 → 落库。
func (s *Service) ApplyPlanningRun(ctx context.Context, issueID string, step int, artifact map[string]any, prov PlanningProvenance) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	st, err := s.ensureState(ctx, tx, issueID)
	if err != nil {
		return err
	}
	if err := s.ApplyPlanningArtifact(ctx, tx, st, step, artifact, prov); err != nil {
		return err
	}
	// **必须真的落库**：ApplyPlanningArtifact 只改内存里的 state 并记决策链，
	// 不写库。少了这一句，事务里一个写操作都没有 —— 线上实测就是
	// 「planning_runs 说 succeeded、决策链有节点、issue_discoveries 一个字节没动」。
	if err := s.save(ctx, tx, st); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// FailPlanningRun 把失败**如实写进发现链状态**。
//
// 2026-09-20 审计里最刺眼的一条就是"什么都不显示"：前端 driver 静默吞错、
// coordinator autohost 只写 backoff 与日志，界面拿不到原因。所以这一步失败必须
// 落进状态里 —— 界面本来就读 analysis.error / candidates.error / plan.error。
func (s *Service) FailPlanningRun(ctx context.Context, issueID string, step int, reason string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	st, err := s.ensureState(ctx, tx, issueID)
	if err != nil {
		return err
	}
	block := map[string]any{
		"error": reason, "ran_at": time.Now().UTC(),
		"producer": map[string]any{"role": "unavailable"},
	}
	switch step {
	case PlanningAnalysis:
		st.Analysis = block
	case PlanningCandidates:
		st.Candidates = block
	case PlanningPlan:
		st.Plan = block
	default:
		return fmt.Errorf("discovery: step %d 不是可派发的规划步", step)
	}
	if err := s.save(ctx, tx, st); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func newPlanningID() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return ""
	}
	buffer[6] = buffer[6]&15 | 64
	buffer[8] = buffer[8]&63 | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", buffer[:4], buffer[4:6], buffer[6:8], buffer[8:10], buffer[10:])
}

// newPlanID 给一版计划一个**合法的 uuid**（同一 issue 同一版本永远同一个）。
//
// 2026-09-20 线上实测：这里此前是 st.IssueID + ":plan:v1"，而 public.tasks.plan_id
// 与 public.plan_steps.plan_id 都是 uuid 列 —— 点「确认物化并开工」必然
// 22P02（invalid input syntax for type uuid: "iss_…:plan:v1"），界面只看到
// 「服务端暂时不可用（HTTP 500）」。哈希成确定的 uuid 而不是每次随机：物化失败
// 重试必须落回同一把 plan_id，否则任务树会挂到一版并不存在的计划上。
func newPlanID(issueID, version string) string {
	sum := sha256.Sum256([]byte(issueID + ":plan:" + version))
	hexSum := hex.EncodeToString(sum[:])
	return fmt.Sprintf("%s-%s-4%s-8%s-%s",
		hexSum[0:8], hexSum[8:12], hexSum[13:16], hexSum[17:20], hexSum[20:32])
}

// isUUID 粗判一个字符串能不能直接进 uuid 列（只认标准 8-4-4-4-12 形状）。
func isUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, char := range value {
		switch index {
		case 8, 13, 18, 23:
			if char != '-' {
				return false
			}
		default:
			if !strings.ContainsRune("0123456789abcdefABCDEF", char) {
				return false
			}
		}
	}
	return true
}

// AppendAnalysisAnswers 把追问的回答并进需求文本，并把旧分析清掉（该重算了）。
//
// 追问是"人补信息"这条回路：补进来的话必须**进需求原文**，否则 agent 拿到的还是
// 那份信息不足的文本，问了等于没问。
func (s *Service) AppendAnalysisAnswers(ctx context.Context, issueID string, answers []Answer) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	st, err := s.ensureState(ctx, tx, issueID)
	if err != nil {
		return err
	}
	text := st.RequirementText
	appended := false
	for _, answer := range answers {
		if strings.TrimSpace(answer.Answer) == "" {
			continue
		}
		text += "\n" + answer.Question + ": " + answer.Answer
		appended = true
	}
	if !appended {
		return nil
	}
	st.RequirementText = text
	st.Analysis = nil
	st.AnalyzedText = nil
	if err := s.save(ctx, tx, st); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// repositoryIDByName 把 "owner/name" 映射到仓库目录里的 id（拿不到就返回空串，
// 界面按名字回退 —— 不编一个假 id）。
func (s *Service) repositoryIDByName(ctx context.Context, tx pgx.Tx, name string) string {
	owner, repo, found := strings.Cut(name, "/")
	if !found {
		return ""
	}
	var id string
	if err := tx.QueryRow(ctx, `SELECT id FROM repomesh_projects.repositories
		WHERE lower(owner)=lower($1) AND lower(name)=lower($2) LIMIT 1`, owner, repo).Scan(&id); err != nil {
		return ""
	}
	return id
}

// RepoSummaries 返回**本项目已接入仓库**的**名片**（扫描产出的目录/依赖/近期提交），
// 作为规划 agent 的输入。
//
// 为什么必须有：agent 看不到证据就只能凭仓库名猜相关性 —— 那正是"找仓库不准"
// 的老问题（线上实测的候选评分里，两个仓库的关键词命中都是 []）。取不到就返回空
// 切片，调用方如实降级（prompt 里就没有名片段），不编造内容。
//
// 2026-09-20 线上实测：这里此前**没有按项目过滤**，SQL 是"全部已登记仓库"，而且
// 扫描名片只按 `url LIKE '%owner/name%'` 模糊对上（不看组织）。结果是别的项目的
// 仓库也进了本项目 issue 的候选名单 —— 线上那条 issue 的候选里出现了
// LBP97541135/fastgpt-plugin，而它根本不在该项目的仓库列表里。名片的取法与
// loadRepoPool 保持一致：同项目 + 同组织 + URL 精确比对。
func (s *Service) RepoSummaries(ctx context.Context, projectID string) ([]byte, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.owner || '/' || r.name,
		       COALESCE(s.description, ''),
		       COALESCE(s.metadata->'topDirs', '[]'::jsonb),
		       COALESCE(s.metadata->'deps', '[]'::jsonb),
		       COALESCE(s.metadata->'recentCommits', '[]'::jsonb)
		FROM repomesh_projects.project_repositories pr
		JOIN repomesh_projects.repositories r ON r.id = pr.repository_id
		JOIN repomesh_projects.projects p ON p.id = pr.project_id
		JOIN repomesh_access.accounts a ON a.id = p.owner
		LEFT JOIN LATERAL (
		  SELECT scan.* FROM repomesh_scan.repositories scan
		  WHERE scan.organization_id = a.organization_id
		    AND lower(regexp_replace(rtrim(scan.url, '/'), '\.git$', '')) IN
		      (lower('https://' || r.host || '/' || r.owner || '/' || r.name),
		       lower('http://' || r.host || '/' || r.owner || '/' || r.name),
		       lower('git@' || r.host || ':' || r.owner || '/' || r.name))
		  ORDER BY scan.profiled_at DESC, scan.id LIMIT 1
		) s ON true
		WHERE pr.project_id = $1
		ORDER BY r.id`, projectID)
	if err != nil {
		return nil, fmt.Errorf("discovery: repo summaries: %w", err)
	}
	defer rows.Close()
	summaries := []map[string]any{}
	for rows.Next() {
		var name, description string
		var topDirs, deps, commits []byte
		if err := rows.Scan(&name, &description, &topDirs, &deps, &commits); err != nil {
			return nil, fmt.Errorf("discovery: repo summaries scan: %w", err)
		}
		summary := map[string]any{"repository": name, "description": description}
		for key, raw := range map[string][]byte{"top_dirs": topDirs, "deps": deps, "recent_commits": commits} {
			var decoded any
			if json.Unmarshal(raw, &decoded) == nil {
				summary[key] = decoded
			}
		}
		summaries = append(summaries, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("discovery: repo summaries: %w", err)
	}
	return json.Marshal(summaries)
}

// stripReasoning 去掉模型写进产物的思考块（<think>…</think>）。
// 没闭合的思考块按"从这里到文件末尾都是推理"处理 —— 宁可丢掉后面，也不把推理
// 当产物解析（那只会得到一个更晦涩的 JSON 报错）。
func stripReasoning(text string) string {
	for {
		start := strings.Index(text, "<think>")
		if start < 0 {
			break
		}
		rest := text[start:]
		end := strings.Index(rest, "</think>")
		if end < 0 {
			text = text[:start]
			break
		}
		text = text[:start] + rest[end+len("</think>"):]
	}
	return text
}

// extractJSONObject 取第一个**配平的** JSON 对象（字符串与转义都算在内）。
// 取不到就返回 false，调用方按原文去解析并如实报错 —— 不编一个空对象出来。
func extractJSONObject(text string) (string, bool) {
	start := strings.Index(text, "{")
	if start < 0 {
		return "", false
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(text); i++ {
		switch {
		case escaped:
			escaped = false
		case inString && text[i] == '\\':
			escaped = true
		case text[i] == '"':
			inString = !inString
		case inString:
			// 字符串内的大括号不算层级
		case text[i] == '{':
			depth++
		case text[i] == '}':
			depth--
			if depth == 0 {
				return text[start : i+1], true
			}
		}
	}
	return "", false
}
