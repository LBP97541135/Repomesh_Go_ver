package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"repomesh.local/repomesh/internal/discovery"
	"repomesh.local/repomesh/internal/humancontrol"
	"repomesh.local/repomesh/internal/roomnotice"
	"repomesh.local/repomesh/internal/skills"
)

// planningDispatcher 把「规划期由角色 agent 产出」这件事接到**执行期同一本台账**上。
//
// 与 ReserveForTask 的差别只有三点（其余全部复用，不另起一套）：
//  1. 不克隆仓库、不需要 installation token（RepoFullName 留空，executor 就不铸令牌）；
//  2. prompt 写进工作区的 prompt.txt（**不是**拼进命令行）—— 规划提示词里带着输出
//     schema 的 JSON，而 buildAgentCommand 那道 sanitize 会把双引号抹掉，JSON 就没法看了；
//  3. 产物由 coordinator 在 run 退出后从工作区读回、校验、落库。**没有兜底模拟**：
//     拿不到合格产物就是失败，原因如实写进发现链状态（界面本来就读 error 字段）。
type planningDispatcher struct {
	pool    *pgxpool.Pool
	service *discovery.Service
	// reviews 是审核台（humancontrol）。产物落地后在这里落一张待审单 ——
	// agent 产出之后才真的需要人看，落单点因此从"端点返回"搬到"产物入库"。
	reviews *humancontrol.Service
	// workspaceRoot 与执行期同一约定（executor 的 prepareWorkspace 用的也是它）。
	workspaceRoot string
	// rooms 把"派下去 / 成了 / 没成"如实投进该 issue 的仓库团队房。
	// 为 nil 时整条链路行为不变 —— 房间是观察面，缺它不该改变规划行为。
	rooms *roomnotice.Notifier
}

func newPlanningDispatcher(pool *pgxpool.Pool, service *discovery.Service, reviews *humancontrol.Service, rooms *roomnotice.Notifier) *planningDispatcher {
	return &planningDispatcher{pool: pool, service: service, reviews: reviews, workspaceRoot: "/opt/repomesh/workspaces", rooms: rooms}
}

// raiseReview 在**产物入库之后**落一张待审单。
//
// ② 落「分档待审批」（卡点 repository_scope：③ 决定的正是哪些仓库在范围内）；
// ④ 落「物化待确认」（卡点 execution：物化确认是放行执行的那道门）。
// fail-open：审核台写失败不能反过来打断发现链。
func (d *planningDispatcher) raiseReview(ctx context.Context, issueID string, step int) {
	if d.reviews == nil {
		return
	}
	checkpoint, label := "", ""
	switch step {
	case discovery.PlanningCandidates:
		checkpoint, label = "repository_scope", "分档待审批"
	case discovery.PlanningPlan:
		checkpoint, label = "execution", "物化待确认"
	default:
		return
	}
	projectID, title, owner, err := d.service.IssueContext(ctx, issueID)
	if err != nil || projectID == "" {
		return
	}
	_, _ = d.reviews.Request(ctx, humancontrol.RequestCommand{
		ProjectID:  projectID,
		Checkpoint: checkpoint,
		Title:      label + "：" + title,
		Summary:    "由发现链自动登记：这一步在 issue 页面完成（不是审核台上按按钮），审核台只做登记与回看。",
		Assignee:   owner,
		IssueID:    issueID,
		Origin:     "discovery",
	})
}

// activeExecutorWorker 找一个**活跃的 host_executor worker**。
//
// 为什么必须挂在它名下：executor 的 ClaimAgentLaunch 只认领
// `attempts.worker_id = 自己` 的 pending run（agent_run.go 的认领查询）。
// 挂一个自造的 worker id 上去，那条 run 就永远不会被任何人认领 —— 派发了等于没派发。
// 找不到活跃 executor 时不建 attempt：让意图留在 pending，下一轮再试（如实延后，
// 不是假装成功）。
func (d *planningDispatcher) activeExecutorWorker(ctx context.Context) (string, error) {
	var workerID string
	err := d.pool.QueryRow(ctx, `SELECT id FROM repomesh_execution.workers
		WHERE kind='host_executor' AND retired_at IS NULL
		  AND heartbeat_at > now() - interval '5 minutes'
		ORDER BY heartbeat_at DESC LIMIT 1`).Scan(&workerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return workerID, nil
}

type planningRow struct {
	// context 是入队时随意图落库的派发上下文（重排步用它带上一版计划与受影响集合）。
	context   []byte
	id        string
	issueID   string
	step      int
	role      string
	skillID   string
	attemptID *string
	runID     *string
}

// tick 每轮做两件事：先把还没派发的意图派出去，再把跑完的收回来。
func (d *planningDispatcher) tick(ctx context.Context) bool {
	did := false
	if reserved, err := d.reservePending(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "coordinator: planning reserve failed: %v\n", err)
	} else if reserved {
		did = true
	}
	if collected, err := d.collectFinished(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "coordinator: planning collect failed: %v\n", err)
	} else if collected {
		did = true
	}
	return did
}

// reservePending 给每条还没派发的意图建 attempt + agent_run，并把 prompt 写进工作区。
func (d *planningDispatcher) reservePending(ctx context.Context) (bool, error) {
	rows, err := d.pool.Query(ctx, `SELECT id::text, issue_id, step, role, skill_id, COALESCE(context, '{}'::jsonb)
		FROM repomesh_issues.planning_runs
		WHERE state='pending' AND run_id IS NULL
		ORDER BY created_at LIMIT 1`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	var pending *planningRow
	for rows.Next() {
		row := planningRow{}
		if err := rows.Scan(&row.id, &row.issueID, &row.step, &row.role, &row.skillID, &row.context); err != nil {
			return false, err
		}
		pending = &row
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	if pending == nil {
		return false, nil
	}
	return d.dispatch(ctx, *pending)
}

func (d *planningDispatcher) dispatch(ctx context.Context, row planningRow) (bool, error) {
	// 需求原文与项目上下文：发现链状态里存的是**拼好的需求文本**（标题+描述），
	// 拿不到就退回 issue 的标题+描述，不编内容。
	var projectID, revision, requirement, issueTitle string
	err := d.pool.QueryRow(ctx, `SELECT i.project_id, i.initial_configuration_revision, i.title,
		       COALESCE(NULLIF(d.requirement_text,''), i.title || E'\n' || i.description)
		FROM repomesh_issues.issues i
		LEFT JOIN repomesh_issues.issue_discoveries d ON d.issue_id = i.id
		WHERE i.id=$1`, row.issueID).
		Scan(&projectID, &revision, &issueTitle, &requirement)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// issue 没了：把意图收掉，别让它永远 pending。
			return d.finishRun(ctx, row.id, "", "failed", "issue 已不存在")
		}
		return false, err
	}
	var agentKind, model string
	_ = d.pool.QueryRow(ctx, `SELECT COALESCE(agent_kind,''), COALESCE(model,'')
		FROM repomesh_projects.agent_settings WHERE project_id=$1`, projectID).Scan(&agentKind, &model)
	if agentKind != "codex_cli" && agentKind != "claude_cli" {
		agentKind = "codex_cli"
	}
	if strings.TrimSpace(model) == "" {
		model = "MiniMax-M2"
	}

	workerID, err := d.activeExecutorWorker(ctx)
	if err != nil {
		return false, err
	}
	if workerID == "" {
		// 没有活跃 executor：如实延后（下一轮 tick 再试），不建一条没人认领的 attempt。
		return false, nil
	}
	attemptID, err := newRunID("att_plan_")
	if err != nil {
		return false, err
	}
	workspace := filepath.Join(d.workspaceRoot, attemptID)
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		return false, fmt.Errorf("coordinator: planning workspace prepare failed: %w", err)
	}
	// ② 候选评分与 ④ 生成计划必须看到仓库名片（扫描产出的目录/依赖/近期提交），
	// 否则 agent 只能凭仓库名猜相关性 —— 那正是"找仓库不准"的老问题。
	summaries := []byte(nil)
	if row.step == discovery.PlanningCandidates || row.step == discovery.PlanningGapAudit ||
		row.step == discovery.PlanningPlan || row.step == discovery.PlanningReplan {
		if encoded, err := d.service.RepoSummaries(ctx, projectID); err == nil {
			summaries = encoded
		}
	}
	// 重排步的额外输入：上一版计划快照（agent 是在 v1 上改）与受影响仓库集合
	// （人工打断的判定结果，随意图落库）。读不到就留空 —— 不编一份"上一版计划"。
	prior := discovery.ReplanContext{}
	if row.step == discovery.PlanningReplan {
		if snapshot, err := d.service.PlanSnapshot(ctx, row.issueID); err == nil && len(snapshot) > 0 {
			if encoded, err := json.Marshal(snapshot); err == nil {
				prior.PlanJSON = encoded
			}
			if version, _ := snapshot["plan_version"].(string); version != "" {
				prior.PlanVersion = version
			}
		}
		var runContext struct {
			AffectedRepositories []string `json:"affected_repositories"`
		}
		if len(row.context) > 0 {
			_ = json.Unmarshal(row.context, &runContext)
		}
		prior.AffectedRepositories = runContext.AffectedRepositories
	}
	prompt := discovery.PlanningPrompt(row.step, requirement, summaries, skill.SeedDoc(row.skillID), prior)
	if err := os.WriteFile(filepath.Join(workspace, "prompt.txt"), []byte(prompt), 0o644); err != nil {
		return false, fmt.Errorf("coordinator: planning prompt write failed: %w", err)
	}
	command := buildPlanningCommand(agentKind, model)
	runID, err := newRunID("run_plan_")
	if err != nil {
		return false, err
	}

	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_execution.attempts
		(id, project_id, issue_id, worker_id, configuration_revision, state, reservation_generation, launch_verified_at)
		VALUES ($1,$2,$3,$4,$5,'launch_verified',
		 COALESCE((SELECT MAX(reservation_generation)+1 FROM repomesh_execution.attempts WHERE project_id=$2 AND issue_id=$3), 1),
		 clock_timestamp())
		ON CONFLICT (project_id, issue_id, reservation_generation) DO NOTHING`,
		attemptID, projectID, row.issueID, workerID, revision); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_execution.agent_runs
		(id, attempt_id, agent_kind, command, workspace, task_package_ref, state)
		VALUES ($1,$2,'planning_agent',$3,$4,$5,'pending')`,
		runID, attemptID, command, workspace, fmt.Sprintf("planning:%s:%d", row.issueID, row.step)); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE repomesh_issues.planning_runs
		SET attempt_id=$2, run_id=$3 WHERE id=$1::uuid`, row.id, attemptID, runID); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	fmt.Fprintf(os.Stderr, "coordinator: planning dispatched issue=%s step=%d role=%s run=%s\n",
		row.issueID, row.step, row.role, runID)
	d.rooms.Notify(ctx, row.issueID, "planning-dispatch:"+runID, planningDispatchedNotice(row.step, row.role))
	return true, nil
}

// collectFinished 收回跑完的规划 run：读产物 → 校验 → 落库；失败则如实上屏。
func (d *planningDispatcher) collectFinished(ctx context.Context) (bool, error) {
	rows, err := d.pool.Query(ctx, `SELECT p.id::text, p.issue_id, p.step, p.role, p.skill_id,
		       p.attempt_id::text, p.run_id::text,
		       r.state, r.workspace, COALESCE(r.exit_code, 0)
		FROM repomesh_issues.planning_runs p
		JOIN repomesh_execution.agent_runs r ON r.id = p.run_id
		WHERE p.state='pending' AND p.run_id IS NOT NULL
		  AND r.state IN ('exited','failed_launch','killed')
		ORDER BY p.created_at LIMIT 1`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	type finished struct {
		planningRow
		runState  string
		workspace string
		exitCode  int
	}
	var one *finished
	for rows.Next() {
		item := finished{}
		if err := rows.Scan(&item.id, &item.issueID, &item.step, &item.role, &item.skillID,
			&item.attemptID, &item.runID, &item.runState, &item.workspace, &item.exitCode); err != nil {
			return false, err
		}
		one = &item
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	if one == nil {
		return false, nil
	}
	reason := ""
	if one.runState != "exited" || one.exitCode != 0 {
		reason = fmt.Sprintf("agent 进程未正常结束（state=%s exit=%d）：%s",
			one.runState, one.exitCode, tailOfLog(filepath.Join(one.workspace, "agent-stderr.log")))
	}
	if reason == "" {
		raw, err := os.ReadFile(filepath.Join(one.workspace, discovery.PlanningArtifactFile))
		if err != nil {
			reason = fmt.Sprintf("agent 没有写出 %s（工作区 %s）", discovery.PlanningArtifactFile, one.workspace)
		} else {
			artifact, parseErr := discovery.ParsePlanningArtifact(one.step, raw)
			if parseErr != nil {
				// 两种失败分开说：agent **明确报告**它做不到（产物里有 error 字段）是 Leader 的
				// 结论，人该看到理由与下一步；"产物读不出来"才是系统侧的不合格。
				if message, ok := discovery.AgentReportedFailure(parseErr); ok {
					reason = fmt.Sprintf("Leader 判定这一步做不了：%s"+"（认可这个判断就补充相应仓库后重跑；"+"不认可就点重试，让它带着现有仓库重新判一次）", message)
				} else {
					reason = fmt.Sprintf("产物不合格：%v；原始产物：%s", parseErr, tailText(string(raw), 600))
				}
			} else {
				runID := ""
				if one.runID != nil {
					runID = *one.runID
				}
				prov := discovery.PlanningProvenance{
					Role: one.role, SkillID: one.skillID, RunID: runID, AgentKind: "planning_agent",
				}
				if err := d.service.ApplyPlanningRun(ctx, one.issueID, one.step, artifact, prov); err != nil {
					reason = fmt.Sprintf("产物入库失败：%v", err)
				} else {
					d.raiseReview(ctx, one.issueID, one.step)
					// 门事件也进房间(spec §3.4):选仓门刚开一条、查漏有漏一条。
					// 幂等键按 run id,重投不刷屏。
					switch one.step {
					case discovery.PlanningCandidates:
						d.rooms.Notify(ctx, one.issueID, "gate:"+one.issueID+":opened", gateOpenedNotice(len(gateSuggested(artifact))))
					case discovery.PlanningGapAudit:
						if missing := gateSuggested(artifact); len(missing) > 0 {
							d.rooms.Notify(ctx, one.issueID, "gate:"+one.issueID+":audit:"+one.id, gateAuditNotice(missing))
						}
					}
				}
			}
		}
	}
	if reason != "" {
		if err := d.service.FailPlanningRun(ctx, one.issueID, one.step, reason); err != nil {
			fmt.Fprintf(os.Stderr, "coordinator: planning failure record failed: %v\n", err)
		}
		fmt.Fprintf(os.Stderr, "coordinator: planning failed issue=%s step=%d reason=%s\n",
			one.issueID, one.step, reason)
		// 幂等键用 planning_runs 行 id：一次规划尝试一条，重投不刷屏。
		d.rooms.Notify(ctx, one.issueID, "planning-failed:"+one.id, planningFailedNotice(one.step, reason))
		return d.finishRun(ctx, one.id, "", "failed", reason)
	}
	fmt.Fprintf(os.Stderr, "coordinator: planning collected issue=%s step=%d role=%s\n",
		one.issueID, one.step, one.role)
	d.rooms.Notify(ctx, one.issueID, "planning-done:"+one.id, planningCompletedNotice(one.step, one.role))
	return d.finishRun(ctx, one.id, "", "succeeded", "")
}

func (d *planningDispatcher) finishRun(ctx context.Context, id, _, state, reason string) (bool, error) {
	if _, err := d.pool.Exec(ctx, `UPDATE repomesh_issues.planning_runs
		SET state=$2, error=$3, collected_at=clock_timestamp() WHERE id=$1::uuid`, id, state, reason); err != nil {
		return false, err
	}
	return true, nil
}

// buildPlanningCommand 组装规划 run 的命令行。
//
// prompt 走文件（"$(cat prompt.txt)"）而不是拼进 argv：规划提示词里带着输出 schema
// 的 JSON，而 buildAgentCommand 那道 sanitize 会把双引号抹掉 —— schema 就没法看了。
func buildPlanningCommand(agentKind, model string) string {
	var agentLine string
	switch agentKind {
	case "claude_cli":
		agentLine = fmt.Sprintf("claude -p \"$(cat prompt.txt)\" --model %s --dangerously-skip-permissions", model)
	case "dsh":
		// 2026-09-20 补：**规划 agent 此前没有 dsh 分支** —— 项目把 agent_kind
		// 选成 dsh 之后，规划这一步照样走 else 去跑 codex，而且不告诉任何人。
		// 那正是"切到 DSH"名不副实的地方。DSH 0.1.1-rc.2 的真实形状见 ledger.go
		// 的注释（`dsh --profile headless "<任务>"`，模型由 profile 配置决定）。
		dshCmd := os.Getenv("REPOMESH_DSH_COMMAND")
		if dshCmd == "" {
			dshCmd = "dsh --profile headless \"$(cat prompt.txt)\""
		}
		if strings.Count(dshCmd, "%s") == 1 {
			agentLine = fmt.Sprintf(dshCmd, "$(cat prompt.txt)")
		} else {
			agentLine = dshCmd
		}
	default:
		agentLine = fmt.Sprintf("codex exec -c model_provider=minimax -c model=%s --skip-git-repo-check --dangerously-bypass-approvals-and-sandbox \"$(cat prompt.txt)\"", model)
	}
	return "bash -c '" + agentLine + "'"
}

// tailOfLog 取日志末尾若干行作为失败原因（读不到就如实说读不到）。
func tailOfLog(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "（没有 agent-stderr.log）"
	}
	return tailText(string(raw), 800)
}

func tailText(text string, limit int) string {
	text = strings.TrimSpace(text)
	if len([]rune(text)) <= limit {
		return text
	}
	runes := []rune(text)
	return "…" + string(runes[len(runes)-limit:])
}

var _ = time.Now

// gateSuggested 从规划产物里取出"仓库名列表":候选步取 items[].repository_name,
// 查漏步取 missing[].repository。取不到就返回空——通知宁缺勿编。
func gateSuggested(artifact map[string]any) []string {
	keys := []string{"items", "missing"}
	for _, key := range keys {
		raw, ok := artifact[key].([]any)
		if !ok {
			continue
		}
		names := make([]string, 0, len(raw))
		for _, entry := range raw {
			entryMap, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			for _, field := range []string{"repository_name", "repository"} {
				if name, ok := entryMap[field].(string); ok && name != "" {
					names = append(names, name)
					break
				}
			}
		}
		if len(names) > 0 {
			return names
		}
	}
	return nil
}
