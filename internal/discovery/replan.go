package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// ───────────── 重排 v2 的发现链侧（A1(d) 的另一半）─────────────
//
// 收集窗开完之后要有人把 v2 **产出来**：这里定义发现链与任务轴之间的端口。
// 组合根把 tasks.PostgresStore.Replan 适配成本端口 —— discovery 不 import tasks
// （两个域各持自己的类型），跨域只走这层接口。

// PlanReplanner 是重排 v2 的落库端口（组合根接 tasks 侧）。
// nil = 未接线：产物只落发现链快照，不产生 v2，**并且如实报错** ——
// 不假装计划已经重排（2026-09-20 之前的 /plans/{id}/interrupt 就是那么坏的）。
type PlanReplanner interface {
	ApplyReplan(ctx context.Context, req ReplanRequest) (ReplanResult, error)
}

// ReplanRequest 是一次 v2 落库请求（仓库全集 + 任务全集 + 仓库级依赖）。
type ReplanRequest struct {
	PlanID         string
	Actor          string
	Reason         string
	UpstreamRef    string
	IdempotencyKey string
	Repositories   []string
	Tasks          []ReplanTask
	// DAG 是**仓库级依赖**：键 = 仓库名，值 = 它依赖的仓库名（与
	// tasks.ValidateBatches 的读法一致：被依赖者必须排在同一批或更早的批）。
	DAG map[string][]string
}

// ReplanTask 是 v2 里的一条任务（标题/指令/验收标准都是 Leader 写的）。
type ReplanTask struct {
	TaskUID     string
	Repository  string
	Title       string
	Instruction string
	Acceptance  string
}

// ReplanResult 是落库结果（新版本号与任务轴迁移计数，原样回给界面）。
type ReplanResult struct {
	ResultVersion   string `json:"resultVersion"`
	CreatedTasks    int    `json:"createdTasks"`
	SupersededTasks int    `json:"supersededTasks"`
}

// PriorPlan 返回该 issue 当前计划快照的 JSON（重排提示词的输入之一：
// agent 要在上一版的基础上改，而不是从零猜一遍）。
func (s *Service) PriorPlan(ctx context.Context, issueID string) ([]byte, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(plan, '{}'::jsonb) FROM repomesh_issues.issue_discoveries WHERE issue_id=$1`,
		issueID).Scan(&raw)
	if err != nil {
		return nil, fmt.Errorf("discovery: prior plan: %w", err)
	}
	return raw, nil
}

// PlanSnapshot 是发现链里当前计划快照的**只读视图**（重排要用它的 plan_id：
// v2 是同一把计划的新版本，不是另开一把计划）。
func (s *Service) PlanSnapshot(ctx context.Context, issueID string) (map[string]any, error) {
	raw, err := s.PriorPlan(ctx, issueID)
	if err != nil {
		return nil, err
	}
	snapshot := map[string]any{}
	if len(raw) == 0 {
		return snapshot, nil
	}
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return nil, fmt.Errorf("discovery: prior plan is not JSON: %w", err)
	}
	return snapshot, nil
}

// EnqueueReplan 登记一次**重排 v2** 的派发意图（发现链第 6 步）。
//
// 触发点只有一处：执行中人工打断判定"影响当前计划"之后（见 internal/web 的
// /plans/{id}/interrupt）。**新仓库必须由人点名** —— agent 自己"发现"的缺失仓库
// 走升级梯，梯子本身不会开收集窗，也就不会走到这里。
//
// planID → issue_id 在库内解析（web 只知道 plan）；上下文随意图落库：
// upstream_ref（触发它的打断决策单，v2 的决策节点要指回那一跳）与
// affected_repositories（判定出的受影响集合，agent 据此重排）。
func (s *Service) EnqueueReplan(ctx context.Context, planID, upstreamNodeID string, affected []string) error {
	var issueID string
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(issue_id, '') FROM public.plans WHERE id=$1::uuid`, planID).Scan(&issueID)
	if err != nil {
		return fmt.Errorf("discovery: replan plan lookup: %w", err)
	}
	if strings.TrimSpace(issueID) == "" {
		return fmt.Errorf("discovery: 计划 %s 没有绑定 issue，无法重排", planID)
	}
	return s.EnqueuePlanningRunWithContext(ctx, issueID, PlanningReplan, map[string]any{
		"plan_id":               planID,
		"upstream_ref":          upstreamNodeID,
		"affected_repositories": affected,
	})
}

// planningRunContextTx 在**调用方的事务里**读回该 issue 该步的派发上下文。
//
// 为什么读库而不是让调用方传参：收产物的是 coordinator 的另一个进程/另一个循环，
// 上下文只存在于入队时写下的那一行里（见迁移 0045）。
func planningRunContextTx(ctx context.Context, tx pgx.Tx, issueID string, step int) (map[string]any, error) {
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT COALESCE((
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

// replanTasksFromArtifact 把 agent 的 v2 产物折成任务全集 + **仓库级依赖**。
//
// 依赖方向与 tasks.ValidateBatches 的读法一致：dag[仓库] = 它依赖的仓库（被依赖者
// 必须排在同一批或更早的批）。任务级 depends_on 写的是**上游任务的标题**，认不出的
// 标题不产生边 —— 不猜依赖；自环不连。
func replanTasksFromArtifact(tasks []any) ([]ReplanTask, map[string][]string, error) {
	out := make([]ReplanTask, 0, len(tasks))
	repoByTitle := map[string]string{}
	for _, entry := range tasks {
		task, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		repo, _ := task["repository"].(string)
		title, _ := task["title"].(string)
		if strings.TrimSpace(repo) == "" || strings.TrimSpace(title) == "" {
			return nil, nil, fmt.Errorf("discovery: v2 任务缺少 repository 或 title（重排被拒）")
		}
		repoByTitle[title] = repo
	}
	dag := map[string][]string{}
	for _, entry := range tasks {
		task, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		repo, _ := task["repository"].(string)
		instruction, _ := task["instruction"].(string)
		acceptance, _ := task["acceptance"].(string)
		title, _ := task["title"].(string)
		uid, _ := task["task_uid"].(string)
		out = append(out, ReplanTask{
			TaskUID: uid, Repository: repo, Title: title,
			Instruction: instruction, Acceptance: acceptance,
		})
		if _, ok := dag[repo]; !ok {
			dag[repo] = []string{}
		}
		rawDeps, _ := task["depends_on"].([]any)
		for _, depAny := range rawDeps {
			dep, ok := depAny.(string)
			if !ok {
				continue
			}
			from, ok := repoByTitle[strings.TrimSpace(dep)]
			if !ok || from == repo || strings.TrimSpace(from) == "" {
				continue
			}
			dup := false
			for _, existing := range dag[repo] {
				if existing == from {
					dup = true
					break
				}
			}
			if !dup {
				dag[repo] = append(dag[repo], from)
			}
		}
	}
	if len(out) == 0 {
		return nil, nil, fmt.Errorf("discovery: v2 产物里没有可用的任务（重排被拒）")
	}
	return out, dag, nil
}
