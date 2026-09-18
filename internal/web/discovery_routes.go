package web

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	"repomesh.local/repomesh/internal/discovery"
)

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
	guard := func(w http.ResponseWriter, r *http.Request) error {
		if auth.Service == nil {
			return &accessFailure{status: 503, code: "auth_not_configured"}
		}
		write := r.Method != http.MethodGet && r.Method != http.MethodHead
		_, err := auth.Service.AuthenticateProjectRequest(r.Context(), cookie(r, sessionCookie), r.Header.Get("X-CSRF-Token"), write)
		return err
	}
	register := func(pattern string, handler http.HandlerFunc) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			if err := guard(w, r); err != nil {
				writeHumanControlError(w, err)
				return
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
			writeHumanControlError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	})
	register("GET /api/issues/{issueId}/discovery", func(w http.ResponseWriter, r *http.Request) {
		tx, err := discoveryAPI.Service.BeginRead(r.Context())
		if err != nil {
			writeHumanControlError(w, err)
			return
		}
		defer tx.Rollback(r.Context())
		if _, err := discoveryAPI.Service.EnsureIssueRead(r.Context(), tx, r.PathValue("issueId")); err != nil {
			writeHumanControlError(w, err)
			return
		}
		state, err := discoveryAPI.Service.LoadRead(r.Context(), tx, r.PathValue("issueId"))
		if err != nil {
			writeHumanControlError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, state.View())
	})
	register("GET /api/issues/{issueId}/discovery/tasks/{taskId}", func(w http.ResponseWriter, r *http.Request) {
		view, err := discoveryAPI.Service.TaskView(r.Context(), r.PathValue("issueId"), r.PathValue("taskId"))
		if err != nil {
			writeHumanControlError(w, err)
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
			writeHumanControlError(w, err)
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
			writeHumanControlError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"issue_id": r.PathValue("issueId"), "archived_at": archivedAt})
	})
	register("POST /api/issues/{issueId}/purge", func(w http.ResponseWriter, r *http.Request) {
		result, err := discoveryAPI.Maintenance.Purge(r.Context(), r.PathValue("issueId"))
		if err != nil {
			writeHumanControlError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
}

func writeDiscoveryReceipt(w http.ResponseWriter, receipt map[string]any, err error) {
	if err != nil {
		writeHumanControlError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, receipt)
}

var _ = errors.Is
var _ = pgx.ErrNoRows
