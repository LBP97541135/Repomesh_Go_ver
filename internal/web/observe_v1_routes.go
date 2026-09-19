package web

import (
	"net/http"
	"os"
	"strconv"
	"strings"

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

	// GET /api/v1/observe/agentloop/config —— 观测面统一走 AgentLoop 控制台
	//（2026-09-19 补，用户裁定「所有的观测都使用 agentloop 实现」）。
	// 前端 ObserveHome 一直在打这条路径（api/agentloop.ts，带 Bearer 头），而 Go
	// 从未实现 → 404 → 观测首页的 AgentLoop 入口永远取不到地址。
	//
	// 地址来源优先级：显式覆盖（REPOMESH_AGENTLOOP_CONSOLE_URL）> 从 OTLP 配置推导
	// > unconfigured。**本部署未配任何 OTLP/AgentLoop 变量**，所以如实回 unconfigured
	// 与 null——由前端配置弹层的本机覆盖（localStorage）兜底，绝不编一个地址出来。
	register("GET /api/v1/observe/agentloop/config", func(w http.ResponseWriter, r *http.Request) {
		optional := func(name string) *string {
			value := strings.TrimSpace(os.Getenv(name))
			if value == "" {
				return nil
			}
			return &value
		}
		consoleURL := optional("REPOMESH_AGENTLOOP_CONSOLE_URL")
		source := "unconfigured"
		if consoleURL != nil {
			source = "override"
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"configured":  consoleURL != nil,
			"console_url": consoleURL,
			"region":      optional("REPOMESH_AGENTLOOP_REGION"),
			"project":     optional("REPOMESH_AGENTLOOP_PROJECT"),
			"workspace":   optional("REPOMESH_AGENTLOOP_WORKSPACE"),
			"source":      source,
		})
	})

	// GET /api/v1/runtime/v1/external-members/readiness —— 外部成员（本地 CLI）就绪租约板。
	// 2026-09-19 补：前端 LocalCliPage 一直在打（client.ts 的
	// getExternalMemberReadiness，注释自述「路径里两段 v1 不是笔误：/api/v1 是全站前缀，
	// /runtime/v1 是 runtime 面自己的版本段」），Go 从未实现 → 404。
	//
	// 租约是**外部成员自己上报**的（launcher 面的 members/start 上报、members/stop 撤销），
	// 而 launcher 面在本部署同样未实现 → 从来没有成员报过到。
	// 所以这里**如实回空板**：`{members: []}`，而不是编几条假成员让面板看着"有数据"。
	// launcher 面补齐后，本端点只需改为读那张租约表即可。
	register("GET /api/v1/runtime/v1/external-members/readiness", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"members": []any{}})
	})

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
