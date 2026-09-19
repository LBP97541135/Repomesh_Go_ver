package web

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/discovery"
	"repomesh.local/repomesh/internal/humancontrol"
)

// writeDiscoveryError 把发现链的错误翻译成**如实的 HTTP 状态 + 人能看懂的话**。
//
// 2026-09-19 事故：发现链此前复用 humancontrol 的错误写出器，而那个写出器
// 只认 access.Failure / pgx.ErrNoRows，discovery 自己的 ErrConflict /
// ErrNoRepositories 全部落到兜底的 **500 {"error":"internal"}**——于是
// "计划里没有仓库"这种纯业务前提问题，在界面上显示成"服务端暂时不可用"，
// 用户完全无法自救。这里逐类映射，并带上 message 让前端能原样展示。
func writeDiscoveryError(w http.ResponseWriter, err error) {
	slog.Warn("discovery request failed", "error", err.Error())
	var failure *access.Failure
	if errors.As(err, &failure) {
		writeJSON(w, failure.Status, map[string]string{"error": strings.ToLower(failure.Code)})
		return
	}
	var local *accessFailure
	if errors.As(err, &local) {
		writeJSON(w, local.status, map[string]string{"error": local.code})
		return
	}
	switch {
	case errors.Is(err, discovery.ErrNoRepositories):
		writeJSON(w, http.StatusConflict, map[string]string{
			"error":   "no_repositories_selected",
			"message": strings.TrimPrefix(err.Error(), discovery.ErrNoRepositories.Error()+"："),
		})
	case errors.Is(err, discovery.ErrDrifted):
		writeJSON(w, http.StatusConflict, map[string]string{
			"error":   "evidence_drifted",
			"message": "候选证据已变化：请刷新页面，重新确认第 3 步的分档后再继续",
		})
	case errors.Is(err, discovery.ErrConflict):
		writeJSON(w, http.StatusConflict, map[string]string{
			"error":   "precondition_not_met",
			"message": strings.TrimPrefix(err.Error(), discovery.ErrConflict.Error()+": "),
		})
	case errors.Is(err, pgx.ErrNoRows):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "resource_not_found"})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
	}
}

// Discovery carries the discovery-chain service and the maintenance facade
// into the web layer; zero values skip every route.
type Discovery struct {
	Service     *discovery.Service
	Maintenance *discovery.Maintenance
	// Reviews 是审核台：发现链的**人工步骤**要镜像成它的待审项。
	//
	// 2026-09-19 事故：审核台读的 review_requests 全仓没有任何生产者（线上实测
	// 0 行），而人工步骤只发生在 issue 页面（③ 分档审批 / ⑤ 物化确认）——
	// 用户在 issue 里看到"待人工"，审核台却永远"没有待审事项"。Nil = 不镜像。
	Reviews *humancontrol.Service
}

// discoveryActorKey 把会话主体塞进请求上下文：审核单要记"谁销的账"。
type discoveryActorKey struct{}

func withDiscoveryActor(ctx context.Context, actor string) context.Context {
	return context.WithValue(ctx, discoveryActorKey{}, actor)
}

func discoveryActor(ctx context.Context) string {
	actor, _ := ctx.Value(discoveryActorKey{}).(string)
	return actor
}

// raiseReview 在**上游步骤跑完**后落一张待审单（例如候选评分完成 → 分档待审批）。
// 一律 fail-open：审核台的写入绝不能反过来打断发现链。
func (d Discovery) raiseReview(ctx context.Context, issueID, checkpoint, label string) {
	if d.Reviews == nil || d.Service == nil {
		return
	}
	projectID, title, owner, err := d.Service.IssueContext(ctx, issueID)
	if err != nil || projectID == "" {
		return
	}
	_, _ = d.Reviews.Request(ctx, humancontrol.RequestCommand{
		ProjectID:  projectID,
		Checkpoint: checkpoint,
		Title:      label + "：" + title,
		Summary:    "由发现链自动登记：这一步在 issue 页面完成（不是审核台上按按钮），审核台只做登记与回看。",
		// 指派给项目属主：审核台对非管理员只显示自己名下的待审项。
		Assignee: owner,
		// 出处写进 request_content：审核台据此**指回 issue**。流水线在那边推进，
		// 在这边给一个「通过」按钮只会推不动它（见 humancontrol.ReviewView 注释）。
		IssueID: issueID,
		Origin:  "discovery",
	})
}

// freezePolicy 在首次物化时把监管策略定死（§3.4：过了物化这一步，策略就定死了）。
//
// 这句话此前只是前端卡片上的文案 —— 后端没有任何东西阻止物化之后再来改策略。
// 策略决定的是「这个需求会停在哪几处、谁能批」，跑起来之后中途改强度，会让已经在
// 旧强度下做过的决策无从解释。同样 fail-open：定死失败不该反过来打断物化。
func (d Discovery) freezePolicy(ctx context.Context, issueID string) {
	if d.Reviews == nil || d.Service == nil {
		return
	}
	projectID, _, _, err := d.Service.IssueContext(ctx, issueID)
	if err != nil || projectID == "" {
		return
	}
	_ = d.Reviews.FreezePolicyDraft(ctx, projectID)
}

// settleReview 在**人工步骤完成后**销掉那张待审单（③ 审批完 / ⑤ 物化完）。
// 同样 fail-open。
func (d Discovery) settleReview(ctx context.Context, issueID, checkpoint, decision, reason string) {
	if d.Reviews == nil || d.Service == nil {
		return
	}
	projectID, _, _, err := d.Service.IssueContext(ctx, issueID)
	if err != nil || projectID == "" {
		return
	}
	_ = d.Reviews.ResolveForCheckpoint(ctx, projectID, checkpoint, discoveryActor(ctx), decision, reason)
}

// registerDiscoveryRoutes wires the contract v0.4 discovery chain endpoints
// and the v0.5 archive/purge maintenance endpoints. All authenticate with
// the session cookie (writes also check CSRF).
func registerDiscoveryRoutes(mux *http.ServeMux, auth Auth, discoveryAPI Discovery) {
	guard := func(w http.ResponseWriter, r *http.Request) (string, error) {
		if auth.Service == nil {
			return "", &accessFailure{status: 503, code: "auth_not_configured"}
		}
		write := r.Method != http.MethodGet && r.Method != http.MethodHead
		principal, err := auth.Service.AuthenticateProjectRequest(r.Context(), cookie(r, sessionCookie), r.Header.Get("X-CSRF-Token"), write)
		if err != nil {
			return "", err
		}
		return principal.ActorID(), nil
	}
	register := func(pattern string, handler http.HandlerFunc) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			actor, err := guard(w, r)
			if err != nil {
				writeDiscoveryError(w, err)
				return
			}
			// 2026-09-19 账号隔离：issue 作用域的端点必须落在**调用者名下的项目**上。
			// 此前只认 issue id —— 拿到别人的 id 就能读它的发现状态、替它跑规划、
			// 甚至归档它。回 404 而不是 403：不泄露"这个 issue 存在，只是不是你的"。
			if issueID := r.PathValue("issueId"); issueID != "" {
				owned, checkErr := auth.Service.IssueInOrganization(r.Context(), actor, issueID)
				if checkErr != nil {
					writeDiscoveryError(w, checkErr)
					return
				}
				if !owned {
					writeJSON(w, http.StatusNotFound, map[string]string{"error": "resource_not_found"})
					return
				}
			}
			handler(w, r.WithContext(withDiscoveryActor(r.Context(), actor)))
		})
	}

	if discoveryAPI.Service == nil {
		return
	}
	// 契约 §5.4 计划纸面(仓库粒度 DAG):物化收据指向的快照;无快照 404,
	// 前端胶囊把 404 归为「还没有计划」而非错误。
	register("GET /api/issues/{issueId}/repositories/{repositoryId}/plan", func(w http.ResponseWriter, r *http.Request) {
		view, err := discoveryAPI.Service.RepositoryPlan(r.Context(), r.PathValue("issueId"), r.PathValue("repositoryId"))
		if err != nil {
			writeDiscoveryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	})
	register("GET /api/issues/{issueId}/discovery", func(w http.ResponseWriter, r *http.Request) {
		tx, err := discoveryAPI.Service.BeginRead(r.Context())
		if err != nil {
			writeDiscoveryError(w, err)
			return
		}
		defer tx.Rollback(r.Context())
		if _, err := discoveryAPI.Service.EnsureIssueRead(r.Context(), tx, r.PathValue("issueId")); err != nil {
			writeDiscoveryError(w, err)
			return
		}
		state, err := discoveryAPI.Service.LoadRead(r.Context(), tx, r.PathValue("issueId"))
		if err != nil {
			writeDiscoveryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, state.View())
	})
	register("GET /api/issues/{issueId}/discovery/tasks/{taskId}", func(w http.ResponseWriter, r *http.Request) {
		view, err := discoveryAPI.Service.TaskView(r.Context(), r.PathValue("issueId"), r.PathValue("taskId"))
		if err != nil {
			writeDiscoveryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	})
	register("POST /api/issues/{issueId}/discovery/analysis", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			CreatedByAgentID string             `json:"created_by_agent_id"`
			IdempotencyKey   string             `json:"idempotency_key"`
			Answers          []discovery.Answer `json:"answers"`
			ForceContinue    bool               `json:"force_continue"`
		}
		if err := decodeBody(w, r, &body); err != nil {
			return
		}
		// 2026-09-20：① 需求分析改由 Organization Leader agent 产出（infra 的
		// Governed Flow）。这里不再由后端算，只做两件事：
		//   · 追问回答 → 并进需求原文（否则 agent 拿到的还是那份信息不足的文本）；
		//   · 登记派发意图 → coordinator 每 tick 派发并收产物。
		// force_continue 例外：它不含任何计算（只是"人选择忽略追问"这个事实），
		// 仍然就地记录。
		if body.ForceContinue {
			receipt, err := discoveryAPI.Service.Analysis(r.Context(), r.PathValue("issueId"), body.CreatedByAgentID, body.IdempotencyKey, nil, true)
			writeDiscoveryReceipt(w, receipt, err)
			return
		}
		if len(body.Answers) > 0 {
			if err := discoveryAPI.Service.AppendAnalysisAnswers(r.Context(), r.PathValue("issueId"), body.Answers); err != nil {
				writeDiscoveryError(w, err)
				return
			}
		}
		if err := discoveryAPI.Service.EnqueuePlanningRun(r.Context(), r.PathValue("issueId"), discovery.PlanningAnalysis); err != nil {
			writeDiscoveryError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"task_id": nil, "step": 1, "status": "accepted"})
	})
	register("POST /api/issues/{issueId}/discovery/candidates", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			CreatedByAgentID string  `json:"created_by_agent_id"`
			IdempotencyKey   string  `json:"idempotency_key"`
			Limit            int     `json:"limit"`
			EntryPoint       *string `json:"entry_point"`
		}
		if err := decodeBody(w, r, &body); err != nil {
			return
		}
		// 2026-09-20：② 改由 Organization Leader agent 产出（这里只登记意图）。
		if err := discoveryAPI.Service.EnqueuePlanningRun(r.Context(), r.PathValue("issueId"), discovery.PlanningCandidates); err != nil {
			writeDiscoveryError(w, err)
			return
		}
		// 审核单不在这里落：产物是**异步**回来的，落单要等 agent 真的产出
		//（见 coordinator 的 planningDispatcher.collectFinished）。
		writeJSON(w, http.StatusAccepted, map[string]any{"task_id": nil, "step": 2, "status": "accepted"})
	})
	register("POST /api/issues/{issueId}/discovery/classification", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			CreatedByAgentID string `json:"created_by_agent_id"`
			IdempotencyKey   string `json:"idempotency_key"`
		}
		if err := decodeBody(w, r, &body); err != nil {
			return
		}
		receipt, err := discoveryAPI.Service.Classification(r.Context(), r.PathValue("issueId"), body.CreatedByAgentID, body.IdempotencyKey)
		writeDiscoveryReceipt(w, receipt, err)
	})
	register("POST /api/issues/{issueId}/discovery/plan", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			CreatedByAgentID string `json:"created_by_agent_id"`
			IdempotencyKey   string `json:"idempotency_key"`
		}
		if err := decodeBody(w, r, &body); err != nil {
			return
		}
		// 2026-09-20：④ 改由 Repository Leader agent 产出（这里只登记意图）。
		if err := discoveryAPI.Service.EnqueuePlanningRun(r.Context(), r.PathValue("issueId"), discovery.PlanningPlan); err != nil {
			writeDiscoveryError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"task_id": nil, "step": 4, "status": "accepted"})
	})
	register("POST /api/issues/{issueId}/discovery/approval", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			DecidedByAgentID string                 `json:"decided_by_agent_id"`
			IdempotencyKey   string                 `json:"idempotency_key"`
			Decision         string                 `json:"decision"`
			Reason           string                 `json:"reason"`
			Adjustments      []discovery.Adjustment `json:"adjustments"`
			EvidenceVersion  string                 `json:"evidence_version"`
		}
		if err := decodeBody(w, r, &body); err != nil {
			return
		}
		receipt, err := discoveryAPI.Service.Approval(r.Context(), r.PathValue("issueId"), body.DecidedByAgentID, body.IdempotencyKey, body.Decision, body.Reason, body.Adjustments, body.EvidenceVersion)
		if err == nil {
			// 人工已在 issue 页面完成分档审批 → 销掉那张待审单。
			discoveryAPI.settleReview(r.Context(), r.PathValue("issueId"), "repository_scope", body.Decision, body.Reason)
		}
		writeDiscoveryReceipt(w, receipt, err)
	})
	register("POST /api/issues/{issueId}/discovery/materialize", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			CreatedByAgentID string `json:"created_by_agent_id"`
			IdempotencyKey   string `json:"idempotency_key"`
		}
		if err := decodeBody(w, r, &body); err != nil {
			return
		}
		receipt, err := discoveryAPI.Service.Materialize(r.Context(), r.PathValue("issueId"), body.CreatedByAgentID, body.IdempotencyKey)
		if err != nil {
			writeDiscoveryError(w, err)
			return
		}
		// 物化完成 = ⑤ 已确认 → 销掉那张待审单（人工在 issue 页面点的）。
		discoveryAPI.settleReview(r.Context(), r.PathValue("issueId"), "execution", "approved", "")
		// 物化同时把监管策略定死：卡点与审核人此后不可改（见 freezePolicy）。
		discoveryAPI.freezePolicy(r.Context(), r.PathValue("issueId"))
		writeJSON(w, http.StatusOK, receipt)
	})

	if discoveryAPI.Maintenance == nil {
		return
	}
	register("POST /api/issues/{issueId}/archive", func(w http.ResponseWriter, r *http.Request) {
		archivedAt, err := discoveryAPI.Maintenance.Archive(r.Context(), r.PathValue("issueId"))
		if err != nil {
			writeDiscoveryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"issue_id": r.PathValue("issueId"), "archived_at": archivedAt})
	})
	register("POST /api/issues/{issueId}/purge", func(w http.ResponseWriter, r *http.Request) {
		result, err := discoveryAPI.Maintenance.Purge(r.Context(), r.PathValue("issueId"))
		if err != nil {
			writeDiscoveryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
}

func writeDiscoveryReceipt(w http.ResponseWriter, receipt map[string]any, err error) {
	if err != nil {
		writeDiscoveryError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, receipt)
}

var _ = errors.Is
var _ = pgx.ErrNoRows
