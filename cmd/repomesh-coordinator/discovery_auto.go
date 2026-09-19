package main

import (
	"context"
	"errors"
	"strconv"
	"time"

	"log/slog"

	"repomesh.local/repomesh/internal/discovery"

	"github.com/jackc/pgx/v5/pgxpool"
)

// discoveryAutomator is the auto-host engine behind the console's 自动托管
// toggle: the frontend only renders progress, this loop advances the real
// discovery state machine. One step per issue per tick, issues in FIFO order,
// with a per-issue backoff so a failing step does not spin the log.
type discoveryAutomator struct {
	service *discovery.Service
	pool    *pgxpool.Pool
	backoff map[string]time.Time
}

func newDiscoveryAutomator(service *discovery.Service, pool *pgxpool.Pool) *discoveryAutomator {
	return &discoveryAutomator{service: service, pool: pool, backoff: map[string]time.Time{}}
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
}

// Several discovery columns (created_by_agent_id, decided_by_agent_id) are
// uuid-typed; the auto-host identity must be a syntactically valid uuid.
const autohostAgent = "00000000-0000-4000-8000-00000000a070"

// step advances at most one issue by one state-machine step. It returns true
// when it performed work.
func (a *discoveryAutomator) step(ctx context.Context) bool {
	now := time.Now()
	var p discoveryProgress
	err := a.pool.QueryRow(ctx, `
		SELECT d.issue_id,
		       (d.analysis IS NOT NULL AND d.analysis <> 'null'::jsonb)                    AS has_analysis,
		       COALESCE((d.analysis->>'sufficient')::bool, false)                          AS sufficient,
		       COALESCE(jsonb_typeof(d.analysis->'forced_continue') = 'object', false)     AS forced,
		       (d.candidates IS NOT NULL AND d.candidates <> 'null'::jsonb)                AS has_candidates,
		       (d.classification IS NOT NULL AND d.classification <> 'null'::jsonb)        AS has_classification,
		       COALESCE(d.approval->>'state', '')                                          AS approval_state,
		       COALESCE(d.classification_evidence_version, '')                             AS evidence_version,
		       (d.plan IS NOT NULL AND d.plan <> 'null'::jsonb)                            AS has_plan,
		       (d.materialization IS NOT NULL AND d.materialization <> 'null'::jsonb)      AS has_materialization
		FROM repomesh_issues.issue_discoveries d
		JOIN repomesh_issues.issues i ON i.id = d.issue_id
		WHERE i.removed_at IS NULL
		  AND NOT (d.analysis IS NOT NULL AND d.analysis <> 'null'::jsonb
		  AND (COALESCE((d.analysis->>'sufficient')::bool, false) OR jsonb_typeof(d.analysis->'forced_continue') = 'object')
		           AND d.candidates IS NOT NULL AND d.candidates <> 'null'::jsonb
		           AND d.classification IS NOT NULL AND d.classification <> 'null'::jsonb
		           AND d.approval->>'state' = 'approved'
		           AND d.plan IS NOT NULL AND d.plan <> 'null'::jsonb
		           AND d.materialization IS NOT NULL AND d.materialization <> 'null'::jsonb)
		ORDER BY d.updated_at
		LIMIT 1`).Scan(
		&p.issueID, &p.hasAnalysis, &p.sufficient, &p.forced, &p.hasCandidates,
		&p.hasClassification, &p.approvalState, &p.evidenceVersion, &p.hasPlan, &p.hasMaterialization)
	if err != nil {
		slog.Info("autohost: no pending discovery", "reason", err.Error())
		return false
	}
	if until, ok := a.backoff[p.issueID]; ok && now.Before(until) {
		return false
	}
	delete(a.backoff, p.issueID)

	done := func(err error) bool {
		if err != nil {
			a.backoff[p.issueID] = time.Now().Add(10 * time.Second)
			slog.Warn("autohost discovery step deferred", "issue", p.issueID, "reason", err.Error())
			return false
		}
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
	case !p.hasCandidates:
		// 2026-09-20：② 候选评分也交给 Organization Leader agent（带仓库名片）。
		slog.Info("autohost: enqueue planning candidates", "issue", p.issueID)
		return done(a.service.EnqueuePlanningRun(ctx, p.issueID, discovery.PlanningCandidates))
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
