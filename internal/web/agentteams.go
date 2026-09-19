package web

import (
	"net/http"

	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/agentteams"
	"repomesh.local/repomesh/internal/projects"
	"repomesh.local/repomesh/internal/repositoryteams"
)

// AgentTeams 执行进度数据源(graph-loop-design 既定路线):后端适配器代理
// AgentTeams Controller 的 workflow 只读接口,浏览器不直连上游。
// 未配置 Controller 时端点仍在,返回 503 如实说明,而不是静默 404。
type AgentTeams struct {
	Client          *agentteams.Client
	RepositoryTeams *repositoryteams.Service
}

func registerAgentTeams(mux *http.ServeMux, auth Auth, at AgentTeams) {
	registerRepositoryTeams(mux, auth, at)
	registerProjectRoute(mux, "GET /api/agentteams/projects/{projectId}/workflow", auth, func(w http.ResponseWriter, r *http.Request, _ access.ProjectPrincipal) error {
		if at.Client == nil {
			writeProjectError(w, &projects.Failure{Status: http.StatusServiceUnavailable, Code: "AGENTTEAMS_NOT_CONFIGURED", FieldErrors: []projects.FieldError{}})
			return nil
		}
		body, status, err := at.Client.Workflow(r.Context(), r.PathValue("projectId"), r.URL.Query().Get("team"))
		if err != nil {
			writeJSON(w, status, map[string]string{"detail": err.Error()})
			return nil
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(body)
		return nil
	})
}
