package web

import (
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"repomesh.local/repomesh/internal/access"
)

// AgentSettings controls which CLI and model the delivery pipeline dispatches
// for one project. Defaults apply when no row exists yet.
type AgentSettings struct {
	AgentKind string `json:"agentKind"`
	Model     string `json:"model"`
}

const (
	defaultAgentKind = "codex_cli"
	defaultModel     = "MiniMax-M2"
)

// agentSettingsPool is injected by the composition root. Nil pool degrades
// GET to defaults and POST to 503 — the page stays usable either way.
var agentSettingsPool *pgxpool.Pool

// SetAgentSettingsPool wires the composition-root pool for the per-project
// agent settings routes.
func SetAgentSettingsPool(pool *pgxpool.Pool) { agentSettingsPool = pool }

func validAgentKind(kind string) bool {
	return kind == "codex_cli" || kind == "claude_cli"
}

// registerAgentSettings exposes the per-project agent execution controls used
// by the /app/ project page; the coordinator ledger reads the same table on
// every dispatch so the choice takes effect without redeploying.
func registerAgentSettings(mux *http.ServeMux, auth Auth) {
	read := func(w http.ResponseWriter, r *http.Request, projectID string) error {
		var settings AgentSettings
		err := agentSettingsPool.QueryRow(r.Context(), `SELECT agent_kind, model FROM repomesh_projects.agent_settings WHERE project_id=$1`, projectID).
			Scan(&settings.AgentKind, &settings.Model)
		if err != nil {
			settings = AgentSettings{AgentKind: defaultAgentKind, Model: defaultModel}
		}
		writeJSON(w, http.StatusOK, settings)
		return nil
	}
	registerProjectRoute(mux, "GET /api/projects/{projectId}/agent-settings", auth, func(w http.ResponseWriter, r *http.Request, principal access.ProjectPrincipal) error {
		return read(w, r, r.PathValue("projectId"))
	})
	registerProjectRoute(mux, "POST /api/projects/{projectId}/agent-settings", auth, func(w http.ResponseWriter, r *http.Request, principal access.ProjectPrincipal) error {
		var body AgentSettings
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil {
			return &access.Failure{Status: http.StatusBadRequest, Code: "VALIDATION_FAILED"}
		}
		if !validAgentKind(body.AgentKind) || len(body.Model) < 1 || len(body.Model) > 64 {
			return &access.Failure{Status: http.StatusUnprocessableEntity, Code: "VALIDATION_FAILED"}
		}
		if agentSettingsPool == nil {
			return &access.Failure{Status: http.StatusServiceUnavailable, Code: "AGENT_SETTINGS_UNAVAILABLE"}
		}
		if _, err := agentSettingsPool.Exec(r.Context(), `INSERT INTO repomesh_projects.agent_settings (project_id, agent_kind, model, updated_at)
			VALUES ($1,$2,$3, now())
			ON CONFLICT (project_id) DO UPDATE SET agent_kind=$2, model=$3, updated_at=now()`,
			r.PathValue("projectId"), body.AgentKind, body.Model); err != nil {
			return &access.Failure{Status: http.StatusServiceUnavailable, Code: "AGENT_SETTINGS_UNAVAILABLE"}
		}
		writeJSON(w, http.StatusOK, body)
		return nil
	})
}
