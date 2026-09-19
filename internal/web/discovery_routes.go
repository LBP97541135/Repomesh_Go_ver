package web

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/discovery"
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
			handler(w, r)
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
		receipt, err := discoveryAPI.Service.Analysis(r.Context(), r.PathValue("issueId"), body.CreatedByAgentID, body.IdempotencyKey, body.Answers, body.ForceContinue)
		writeDiscoveryReceipt(w, receipt, err)
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
		receipt, err := discoveryAPI.Service.Candidates(r.Context(), r.PathValue("issueId"), body.CreatedByAgentID, body.IdempotencyKey, body.Limit, body.EntryPoint)
		writeDiscoveryReceipt(w, receipt, err)
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
		receipt, err := discoveryAPI.Service.Plan(r.Context(), r.PathValue("issueId"), body.CreatedByAgentID, body.IdempotencyKey)
		writeDiscoveryReceipt(w, receipt, err)
	})
	register("POST /api/issues/{issueId}/discovery/approval", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			DecidedByAgentID string                `json:"decided_by_agent_id"`
			IdempotencyKey   string                `json:"idempotency_key"`
			Decision         string                `json:"decision"`
			Reason           string                `json:"reason"`
			Adjustments      []discovery.Adjustment `json:"adjustments"`
			EvidenceVersion  string                `json:"evidence_version"`
		}
		if err := decodeBody(w, r, &body); err != nil {
			return
		}
		receipt, err := discoveryAPI.Service.Approval(r.Context(), r.PathValue("issueId"), body.DecidedByAgentID, body.IdempotencyKey, body.Decision, body.Reason, body.Adjustments, body.EvidenceVersion)
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
