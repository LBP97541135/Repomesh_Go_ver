package web

import (
	"net/http"
	"strconv"

	"repomesh.local/repomesh/internal/observability"
)

// ObserveV1 carries the v1 observe read surface into the web layer.
type ObserveV1 struct {
	Service *observability.Service
}

// registerObserveV1 wires the /api/observe/* endpoints the Observe*
// frontend pages call (session cookie auth; same-origin CSRF on writes).
func registerObserveV1(mux *http.ServeMux, auth Auth, observe ObserveV1) {
	if observe.Service == nil {
		return
	}
	guard := func(w http.ResponseWriter, r *http.Request) error {
		if auth.Service == nil {
			return &accessFailure{status: 503, code: "auth_not_configured"}
		}
		write := r.Method != http.MethodGet && r.Method != http.MethodHead
		_, err := auth.Service.AuthenticateProjectRequest(r.Context(), cookie(r, sessionCookie), r.Header.Get("X-CSRF-Token"), write)
		return err
	}
	writeError := writeHumanControlError

	queryInt := func(r *http.Request, name string, fallback int) int {
		if raw := r.URL.Query().Get(name); raw != "" {
			if value, err := strconv.Atoi(raw); err == nil {
				return value
			}
		}
		return fallback
	}

	register := func(pattern string, handler http.HandlerFunc) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			if err := guard(w, r); err != nil {
				writeError(w, err)
				return
			}
			handler(w, r)
		})
	}

	register("GET /api/observe/summary", func(w http.ResponseWriter, r *http.Request) {
		summary, err := observe.Service.Summary(r.Context(), queryInt(r, "days", 7))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, summary)
	})
	register("GET /api/observe/issues", func(w http.ResponseWriter, r *http.Request) {
		result, err := observe.Service.Issues(r.Context())
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	register("GET /api/observe/logs/issues", func(w http.ResponseWriter, r *http.Request) {
		result, err := observe.Service.LogIssueGroups(r.Context())
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	register("GET /api/observe/logs", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		result, err := observe.Service.Logs(r.Context(), q.Get("level"), q.Get("source"), q.Get("issue_id"), q.Get("query"), queryInt(r, "limit", 100), q.Get("cursor"))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	register("GET /api/observe/alert-rules", func(w http.ResponseWriter, r *http.Request) {
		result, err := observe.Service.AlertRules(r.Context())
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	register("POST /api/observe/alert-rules", func(w http.ResponseWriter, r *http.Request) {
		var payload observability.AlertRulePayload
		if err := decodeBody(w, r, &payload); err != nil {
			return
		}
		rule, err := observe.Service.CreateAlertRule(r.Context(), payload)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, rule)
	})
	register("PUT /api/observe/alert-rules/{ruleId}", func(w http.ResponseWriter, r *http.Request) {
		var payload observability.AlertRulePayload
		if err := decodeBody(w, r, &payload); err != nil {
			return
		}
		rule, err := observe.Service.UpdateAlertRule(r.Context(), r.PathValue("ruleId"), payload)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, rule)
	})
	register("DELETE /api/observe/alert-rules/{ruleId}", func(w http.ResponseWriter, r *http.Request) {
		if err := observe.Service.DeleteAlertRule(r.Context(), r.PathValue("ruleId")); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	})
	register("GET /api/observe/alerts", func(w http.ResponseWriter, r *http.Request) {
		result, err := observe.Service.AlertEvents(r.Context(), queryInt(r, "days", 7))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	register("GET /api/observe/alerts/active", func(w http.ResponseWriter, r *http.Request) {
		result, err := observe.Service.ActiveAlerts(r.Context())
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	register("POST /api/observe/alerts/evaluate", func(w http.ResponseWriter, r *http.Request) {
		result, err := observe.Service.EvaluateAlerts(r.Context())
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	register("GET /api/observe/trace/sessions", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		result, err := observe.Service.TraceSessions(r.Context(), q.Get("agent_name"), q.Get("issue_id"), queryInt(r, "limit", 50), q.Get("cursor"))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	register("GET /api/observe/trace/issues", func(w http.ResponseWriter, r *http.Request) {
		result, err := observe.Service.TraceIssueGroups(r.Context())
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	register("GET /api/observe/trace/sessions/{sessionId}/events", func(w http.ResponseWriter, r *http.Request) {
		result, err := observe.Service.TraceSessionEvents(r.Context(), r.PathValue("sessionId"), queryInt(r, "limit", 200), queryInt(r, "after_seq", 0))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	register("GET /api/observe/trace/events", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		result, err := observe.Service.TraceEvents(r.Context(), q.Get("event_type"), q.Get("status"), q.Get("agent_name"), queryInt(r, "limit", 100), q.Get("cursor"))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
}
