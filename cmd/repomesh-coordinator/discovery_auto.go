package main

import (
	"context"
	"errors"
	"strconv"
	"time"

	"log/slog"

	"repomesh.local/repomesh/internal/discovery"
	"repomesh.local/repomesh/internal/roomnotice"

	"github.com/jackc/pgx/v5/pgxpool"
)

// discoveryAutomator is the auto-host engine behind the console's 自动托管
// toggle: the frontend only renders progress, this loop advances the real
// discovery state machine. One step per issue per tick, issues in FIFO order,
// with a per-issue backoff so a failing step does not spin the log.
type discoveryAutomator struct {
	service *discovery.Service
	pool    *pgxpool.Pool
	// rooms 把门事件(超时代选)投进 issue 的仓库团队房(spec §3.4);
	// nil 时是安全空操作——房间是观察面,缺它不改行为。
	rooms *roomnotice.Notifier
	// wake 是采纳建议为范围后唤醒入选仓库团队的钩子(spec §3.3):选进范围时
	// ensure-ready(wake 失败靠派活前双保险兜底)。nil 时安全跳过。
	wake    func(ctx context.Context, projectID string, repositoryIDs []string)
	backoff map[string]time.Time
	// attempts 记"同一条 issue 的同一个 step 连续跑了几次"。
	//
	// 2026-09-20 线上实测：某条 issue 的 classification 在库里是 JSON `null`，于是
	// `has_classification` 恒为 false —— 协调器**每 3 秒**重跑一次 Classification，
	// 日志刷屏、库被反复写（另一条 issue 的 plan 步骤也一样：反复登记计划意图）。
	// 判据与落库之间只要有这种不一致，就会形成空转环。3 秒地板只压频率，压不住根。
	// 这里再加一条：**同一步连续 3 次都没换步**就退到 5 分钟一次，并把这件事**说出来**
	// （一条 WARN，不再刷屏）—— 空转从"每秒"变成"每 5 分钟"，而且人能看见它在卡哪。
	attempts map[string]int
	// lastStep 记这条 issue 上一次走的是哪一步：换步了说明推进了，计数归零。
	lastStep map[string]int
}

func newDiscoveryAutomator(service *discovery.Service, pool *pgxpool.Pool, rooms *roomnotice.Notifier) *discoveryAutomator {
	return &discoveryAutomator{
		service: service, pool: pool, rooms: rooms,
		backoff:  map[string]time.Time{},
		attempts: map[string]int{},
		lastStep: map[string]int{},
	}
}

// withWake 接上"采纳建议后唤醒入选仓库团队"的钩子(spec §3.3)。nil 时安全跳过。
func (a *discoveryAutomator) withWake(fn func(ctx context.Context, projectID string, repositoryIDs []string)) *discoveryAutomator {
	a.wake = fn
	return a
}

type discoveryProgress struct {
	issueID            string
	hasAnalysis        bool
	sufficient         bool
	forced             bool
	hasCandidates      bool
	hasClassification  bool
	approvalState      string
	evidenceVersion    string
	hasPlan            bool
	hasMaterialization bool
	// hitlMode 是这次 issue 的人审门模式（0053 迁移落列）：ai = 自动托管，
	// hitl = 门等真人。**自动托管循环只处理 ai 的 issue** —— 此前它不看这个字段，
	// 于是人工参与模式下 ③ 分档审批与 ⑤ 物化确认也被无条件代行，人审门形同不存在。
	hitlMode string
	// gatePending / gateExpired 是选仓门投影(Task B2,spec §3.2)。WHERE 已排除
	// 「有截止且未到期、且未被请求」的门,剩下的 pending 只有三种:gatePending=
	// 无截止的门(hitl 语义,等待分支兜住),gateExpired=有截止且已到期的门(超时),
	// gateAIRequested=人在门上点过「让 AI 定」的门(不论有没有截止)。后两个谓词
	// 可以同时成立;switch 的先后顺序按"先等、后生成、再采纳"排。
	gatePending     bool
	gateExpired     bool
	gateAIRequested bool
	// gateSuggestedCount 是门里建议的条数(spec 2026-09-20 修订:0=还没生成,
	// 协调器只登记候选生成意图;>0=已就绪,采纳为范围)。
	gateSuggestedCount int
	// gateManual / gapAuditRecorded 是查漏步(Task B3,spec §3.2)的判据:门被
	// **人工**确认(decided_by=manual)后、③ 分档前先派一次查漏;结论落进
	// gate.audit.missing(键存在)后就不再派。ai/timeout 确认的门不跑查漏。
	gateManual       bool
	gapAuditRecorded bool
}

// Several discovery columns (created_by_agent_id, decided_by_agent_id) are
// uuid-typed; the auto-host identity must be a syntactically valid uuid.
const autohostAgent = "00000000-0000-4000-8000-00000000a070"

// step advances at most one issue by one state-machine step. It returns true
// when it performed work.
func (a *discoveryAutomator) step(ctx context.Context) bool {
	now := time.Now()
	candidates, err := a.pendingIssues(ctx)
	if err != nil {
		slog.Info("autohost: no pending discovery", "reason", err.Error())
		return false
	}
	// 头阻塞修复（2026-09-20 线上实测）：此前只取「最旧的那一条」，于是一条永远
	// 过不去的 issue（线上是 iss_2db1 —— 分档把全部仓库都排除了，审批每 10s 撞
	// 一次墙）就把后面所有 issue 全饿死：新 issue 的 ② 永远等不到派发，界面上一直
	// 停在「等待前序」。现在按最旧优先往下看，跳过正处于退避期的那些。
	var p discoveryProgress
	picked := false
	for _, candidate := range candidates {
		if until, ok := a.backoff[candidate.issueID]; ok && now.Before(until) {
			continue
		}
		p, picked = candidate, true
		break
	}
	if !picked {
		return false
	}
	delete(a.backoff, p.issueID)

	step := autohostStep(p)
	done := func(err error) bool {
		if err != nil {
			a.backoff[p.issueID] = time.Now().Add(10 * time.Second)
			slog.Warn("autohost discovery step deferred", "issue", p.issueID, "reason", err.Error())
			return false
		}
		// 同一步连续跑（判据没变）→ 计数；换步了 → 归零。
		key := p.issueID + ":" + strconv.Itoa(step)
		if last, ok := a.lastStep[p.issueID]; ok && last == step {
			a.attempts[key]++
		} else {
			a.attempts[key] = 1
		}
		a.lastStep[p.issueID] = step
		if a.attempts[key] >= 3 {
			// 连续 3 次都没换步：判据与落库不一致（线上实例：classification 是 JSON
			// null，has_classification 恒 false）。退到 5 分钟一次，并**说一次**是哪条
			// issue、卡在第几步 —— 不再每秒刷屏，也不再假装它在推进。
			a.backoff[p.issueID] = time.Now().Add(5 * time.Minute)
			if a.attempts[key] == 3 {
				slog.Warn("autohost: 同一步反复无进展，退到 5 分钟一次",
					"issue", p.issueID, "step", step, "attempts", a.attempts[key])
			}
			return true
		}
		// 每步都留一道**地板间隔**（哪怕这一步"成功"了）。
		//
		// 2026-09-20 线上实测：一条 issue 的 classification 在库里是 JSON `null`，
		// 于是 `has_classification` 恒为 false，协调器**每秒**重跑一次 Classification
		// （日志 `autohost: classifying tiers` 刷屏、库被反复写），把机器白白烧掉。
		// 只要"步骤判据"与"实际落库"之间有任何不一致，就会形成这种空转环 ——
		// 这里给每次推进加 3 秒地板，把它从"每秒"压到"每 3 秒"，同时不影响正常节奏
		// （正常一步是分钟级的）。
		a.backoff[p.issueID] = time.Now().Add(3 * time.Second)
		return true
	}
	// working 与 done 的区别只有一点:**不进熔断计数**。
	//
	// spec 2026-09-20 修订:等建议生成期间,判据(建议为空)会在产物落地前一直为真。
	// 若走 done() 的"同一步连续 3 次"计数,第 3 拍就会退避 5 分钟 —— 建议反而更晚
	// 落地。所以"登记候选生成意图"这类**判据暂未变化但确实在做正确的事**的分支
	// 用 working():给 3 秒地板间隔、返回 true,但不计空转、不触发 5 分钟退避。
	working := func(err error) bool {
		if err != nil {
			a.backoff[p.issueID] = time.Now().Add(10 * time.Second)
			slog.Warn("autohost discovery step deferred", "issue", p.issueID, "reason", err.Error())
			return false
		}
		delete(a.attempts, p.issueID+":"+strconv.Itoa(step))
		a.lastStep[p.issueID] = step
		a.backoff[p.issueID] = time.Now().Add(3 * time.Second)
		return true
	}
	idem := "autohost:" + p.issueID
	switch {
	case !p.hasAnalysis:
		// 2026-09-20：① 需求分析不再由后端词表算，改为**派发给 Organization Leader
		// agent**（infra 的 Governed Flow：Scope 由角色产出，后端只校验与记录）。
		// 这里只登记意图 —— 真正的派发与收产物在 planningDispatcher 里，与状态机
		// 同一拍子。意图是幂等的，重复登记不会把同一步派发两次。
		slog.Info("autohost: enqueue planning analysis", "issue", p.issueID)
		return done(a.service.EnqueuePlanningRun(ctx, p.issueID, discovery.PlanningAnalysis))
	case !p.sufficient && !p.forced:
		// 自动托管 has no human to answer the dimension questions: record the
		// forced continue so Candidates accept the analysis (its designed
		// bypass), then the next tick proceeds to scoring.
		slog.Info("autohost: forcing analysis continue", "issue", p.issueID)
		// A fresh idem key per call: an external writer (e.g. a stale
		// frontend poll) can wipe the forced marker from the analysis block
		// while the ledger still holds the old receipt, and a replayed key
		// would silently refuse to restore it.
		forceKey := idem + ":analysis-force:" + strconv.FormatInt(time.Now().UnixNano(), 36)
		_, err = a.service.Analysis(ctx, p.issueID, autohostAgent, forceKey, nil, true)
		return done(err)
	case p.gatePending && !p.gateAIRequested && !p.gateExpired:
		// 门等待(被捡到时):无截止、且没人点过「让 AI 定」的门(hitl 语义)。
		// 不动状态、**不经 done()**、不进熔断计数——门在等人,不是发现链在空转。
		slog.Info("autohost: 选仓门等待确认", "issue", p.issueID)
		return false
	case (p.gateAIRequested || p.gateExpired) && p.gateSuggestedCount == 0:
		// 门要求 AI 定(人点过「让 AI 定」,或 ai 模式 10 分钟到期),但建议**还没
		// 生成**:只登记候选生成意图(EnqueuePlanningRun 幂等;真正派发在
		// planningDispatcher)。走 working() —— 等建议期间判据暂未变化,不能被
		// 3 次熔断退避当成空转(否则建议反而更晚落地)。
		slog.Info("autohost: 选仓门待生成建议,登记候选意图", "issue", p.issueID)
		return working(a.service.EnqueuePlanningRun(ctx, p.issueID, discovery.PlanningCandidates))
	case p.gateAIRequested || p.gateExpired:
		// 建议已就绪:采纳建议为范围 + 唤醒入选仓库的团队(spec §3.3)。与 A3 批量
		// 确认端点同一条服务路径的直写版:建议集合→项目内仓 id→双表写(整组一把
		// scope_revision)→门 CAS 置 resolved。decided_by 如实记 ai(人点过)或
		// timeout(10 分钟无人点,后端走同一条生成+采纳)。
		decidedBy, notice := "timeout", gateTimeoutNotice
		if p.gateAIRequested {
			decidedBy, notice = "ai", gateAIAdoptedNotice
		}
		slog.Info("autohost: 选仓门按建议采纳为范围", "issue", p.issueID, "decided_by", decidedBy)
		receipt, repositoryIDs, err := a.service.AdoptGateSuggestion(ctx, p.issueID, decidedBy, idem+":gate-adopt:"+decidedBy)
		if err != nil {
			return done(err)
		}
		if len(repositoryIDs) > 0 && a.wake != nil {
			if projectID, _, _, ctxErr := a.service.IssueContext(ctx, p.issueID); ctxErr == nil && projectID != "" {
				a.wake(ctx, projectID, repositoryIDs)
			}
		}
		a.rooms.Notify(ctx, p.issueID, "gate:"+p.issueID+":adopted:"+decidedBy, notice(receipt.RepositoryCount))
		return done(nil)
	case !p.hasCandidates:
		// 2026-09-20：② 候选评分也交给 Organization Leader agent（带仓库名片）。
		slog.Info("autohost: enqueue planning candidates", "issue", p.issueID)
		return done(a.service.EnqueuePlanningRun(ctx, p.issueID, discovery.PlanningCandidates))
	case p.gateManual && !p.hasClassification && !p.gapAuditRecorded:
		// 查漏(③ 前置,仅人工确认的门,Task B3/spec §3.2):门由人确认后,③ 分档前
		// 先派一次 PlanningGapAudit —— Manager 拿着需求 + 已确认范围 + 建议集合找漏。
		// 产物落 gate.audit.missing:有 missing → ③ 停等人(补上/就这样);空 → 自动过门。
		// ai/timeout 确认的门不跑查漏,直接进 ③。
		slog.Info("autohost: enqueue planning gap audit", "issue", p.issueID)
		return done(a.service.EnqueuePlanningRun(ctx, p.issueID, discovery.PlanningGapAudit))
	case !p.hasClassification:
		slog.Info("autohost: classifying tiers", "issue", p.issueID)
		_, err = a.service.Classification(ctx, p.issueID, autohostAgent, idem+":classification")
		return done(err)
	case p.approvalState != "approved":
		if p.evidenceVersion == "" {
			return done(errors.New("classification evidence version missing"))
		}
		slog.Info("autohost: approving tiers", "issue", p.issueID)
		_, err = a.service.Approval(ctx, p.issueID, autohostAgent, idem+":approval",
			"approved", "自动托管：分档审批自动通过", nil, p.evidenceVersion)
		// 「一个仓库都没纳入」有一种可自愈的情形：评分时仓库池还是空的（需求没点名仓库
		// 且当时的口径只取已确认范围），候选因此为空，分档随之全排除。池子口径已经修好，
		// 这里把这种**空候选**打回 ② 重做一次（幂等键保证只重开一次；真评过的候选、
		// 或项目里确实没有仓库的，都不动）。
		if errors.Is(err, discovery.ErrNoRepositories) {
			reopened, reopenErr := a.service.ReopenEmptyCandidates(ctx, p.issueID, autohostAgent, idem+":reopen-candidates")
			if reopenErr != nil {
				slog.Warn("autohost: reopen empty candidates failed", "issue", p.issueID, "reason", reopenErr.Error())
			} else if reopened {
				slog.Info("autohost: reopened empty candidates", "issue", p.issueID)
				return true
			}
		}
		return done(err)
	case !p.hasPlan:
		// 2026-09-20：④ 生成计划交给 Repository Leader agent（任务 DAG 由它拆）。
		slog.Info("autohost: enqueue planning plan", "issue", p.issueID)
		return done(a.service.EnqueuePlanningRun(ctx, p.issueID, discovery.PlanningPlan))
	case !p.hasMaterialization:
		slog.Info("autohost: materializing tasks", "issue", p.issueID)
		_, err = a.service.Materialize(ctx, p.issueID, autohostAgent, idem+":materialize")
		return done(err)
	}
	return false
}

// autohostStep 把"这条 issue 现在该走哪一步"折成一个小整数，**只用于去重计数**
// （判断协调器是不是在同一步上空转），与 discovery 的 PlanningXxx 常量无关。
// 判据必须与上面 switch 的分支**逐一对应**，否则计数会错位。
func autohostStep(p discoveryProgress) int {
	switch {
	case !p.hasAnalysis:
		return 1 // ① 需求分析
	case !p.sufficient && !p.forced:
		return 2 // 分析不足 → 强制继续
	case p.gatePending && !p.gateAIRequested && !p.gateExpired:
		return 3 // 门等待(无截止且未被请求)
	case (p.gateAIRequested || p.gateExpired) && p.gateSuggestedCount == 0:
		return 4 // 门要求 AI 定但建议未生成 → 登记候选意图
	case p.gateAIRequested || p.gateExpired:
		return 5 // 建议就绪 → 采纳为范围
	case !p.hasCandidates:
		return 6 // ② 候选评分
	case p.gateManual && !p.hasClassification && !p.gapAuditRecorded:
		return 7 // ③ 前的查漏(人工确认的门)
	case !p.hasClassification:
		return 8 // ③ 分档
	case p.approvalState != "approved":
		return 9 // ③ 审批
	case !p.hasPlan:
		return 10 // ④ 生成计划
	case !p.hasMaterialization:
		return 11 // ⑤ 物化
	}
	return 0
}

// pendingIssues 取还没走完发现链的 issue，**最旧优先**，一次多取几条。
// 取多条是为了让退避中的 issue 能被跳过而不是堵住队首（见 step 的注释）。
func (a *discoveryAutomator) pendingIssues(ctx context.Context) ([]discoveryProgress, error) {
	rows, err := a.pool.Query(ctx, `
		SELECT d.issue_id,
		       (d.analysis IS NOT NULL AND d.analysis <> 'null'::jsonb)                    AS has_analysis,
		       COALESCE((d.analysis->>'sufficient')::bool, false)                          AS sufficient,
		       COALESCE(jsonb_typeof(d.analysis->'forced_continue') = 'object', false)     AS forced,
		       (d.candidates IS NOT NULL AND d.candidates <> 'null'::jsonb)                AS has_candidates,
		       (d.classification IS NOT NULL AND d.classification <> 'null'::jsonb)        AS has_classification,
		       COALESCE(d.approval->>'state', '')                                          AS approval_state,
		       COALESCE(d.classification_evidence_version, '')                             AS evidence_version,
		       (d.plan IS NOT NULL AND d.plan <> 'null'::jsonb)                            AS has_plan,
		       (d.materialization IS NOT NULL AND d.materialization <> 'null'::jsonb)      AS has_materialization,
		       COALESCE(i.hitl_mode, 'hitl')                                               AS hitl_mode,
		       (d.scope_gate->>'state' = 'pending'
		          AND d.scope_gate->>'deadline_at' IS NULL)                           AS gate_pending,
		       (d.scope_gate->>'state' = 'pending'
		          AND d.scope_gate->>'deadline_at' IS NOT NULL
		          AND now() >= (d.scope_gate->>'deadline_at')::timestamptz)              AS gate_expired,
		       COALESCE(d.scope_gate->>'state' = 'resolved'
		          AND d.scope_gate->>'decided_by' = 'manual', false)                   AS gate_manual,
		       COALESCE(d.scope_gate->'audit' ? 'missing', false)                       AS gap_audit_recorded,
		       -- 人点过「让 AI 定」的门(spec 2026-09-20 修订):门先出现,点了才生成建议。
		       COALESCE(d.scope_gate->>'state' = 'pending'
		          AND COALESCE((d.scope_gate->>'ai_requested')::bool, false), false)     AS gate_ai_requested,
		       -- 建议条数:0=还没生成(协调器只登记候选意图);>0=就绪,采纳为范围。
		       COALESCE(CASE WHEN jsonb_typeof(d.scope_gate->'suggested') = 'array'
		                     THEN jsonb_array_length(d.scope_gate->'suggested') ELSE 0 END, 0) AS gate_suggested_count
		FROM repomesh_issues.issue_discoveries d
		JOIN repomesh_issues.issues i ON i.id = d.issue_id
		WHERE i.removed_at IS NULL
		  -- 自动托管只处理 ai 模式的 issue：人工参与（hitl）的 issue 由人自己推门，
		  -- 这里连一步都不代行（0053 之前它无条件代行 ③ 与 ⑤）。
		  AND COALESCE(i.hitl_mode, 'hitl') = 'ai'
		  -- 选仓门未到期直接排除(Task B2,spec §3.2):门在等人时连 tick 都不捡,
		  -- 不入队、不空转。但**人点过「让 AI 定」(ai_requested)的门不排除** ——
		  -- 那正是要驱动"生成建议并采纳"的门(spec 2026-09-20 修订)。无截止的门
		  -- (hitl 语义)不排除,由 switch 的等待分支兜住。
		  AND NOT (d.scope_gate->>'state' = 'pending'
		      AND NOT COALESCE((d.scope_gate->>'ai_requested')::bool, false)
		      AND d.scope_gate->>'deadline_at' IS NOT NULL
		      AND now() < (d.scope_gate->>'deadline_at')::timestamptz)
		  AND NOT (d.analysis IS NOT NULL AND d.analysis <> 'null'::jsonb
		  AND (COALESCE((d.analysis->>'sufficient')::bool, false) OR jsonb_typeof(d.analysis->'forced_continue') = 'object')
		           AND d.candidates IS NOT NULL AND d.candidates <> 'null'::jsonb
		           AND d.classification IS NOT NULL AND d.classification <> 'null'::jsonb
		           AND d.approval->>'state' = 'approved'
		           AND d.plan IS NOT NULL AND d.plan <> 'null'::jsonb
		           AND d.materialization IS NOT NULL AND d.materialization <> 'null'::jsonb)
		ORDER BY d.updated_at
		LIMIT 20`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	pending := []discoveryProgress{}
	for rows.Next() {
		var p discoveryProgress
		if err := rows.Scan(
			&p.issueID, &p.hasAnalysis, &p.sufficient, &p.forced, &p.hasCandidates,
			&p.hasClassification, &p.approvalState, &p.evidenceVersion, &p.hasPlan, &p.hasMaterialization,
			&p.hitlMode, &p.gatePending, &p.gateExpired, &p.gateManual, &p.gapAuditRecorded,
			&p.gateAIRequested, &p.gateSuggestedCount); err != nil {
			return nil, err
		}
		pending = append(pending, p)
	}
	return pending, rows.Err()
}
