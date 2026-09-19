package web

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/assembly"
)

// registerTopologyRoutes exposes the M4 assembly as the project topology API
// (Py: human_control.py /projects/topologies + automatic-topologies + {id}/topology).
func registerTopologyRoutes(mux *http.ServeMux, auth Auth, assemblySvc *assembly.Service) {
	if assemblySvc == nil {
		return
	}
	// GET /api/agents —— 智能体花名册（org 作用域）。
	// 2026-09-19 补：前端 `api/agents.ts` 一直在打这条路径，而后端**从未注册**它
	// ——查询（internal/assembly/roster.go 的 Roster）与投影类型（AgentRosterRow，
	// 字段与前端逐字一致）早就写好了，只是没接出来，于是智能体页的花名册恒 404。
	// 这里用与 console 路由同一套会话鉴权接上（读操作，CSRF 传 false）。
	mux.HandleFunc("GET /api/agents", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if auth.Service == nil {
			writeHumanControlError(w, &accessFailure{status: 503, code: "auth_not_configured"})
			return
		}
		principal, err := auth.Service.AuthenticateProjectRequest(
			r.Context(), cookie(r, sessionCookie), r.Header.Get("X-CSRF-Token"), false)
		if err != nil {
			writeHumanControlError(w, err)
			return
		}
		rows, err := assemblySvc.Roster(r.Context(), principal.ActorID(),
			r.URL.Query().Get("role"), r.URL.Query().Get("repositoryId"), r.URL.Query().Get("status"))
		if err != nil {
			writeHumanControlError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, rows)
	})
	// 智能体写面（2026-09-19 补）：新建 / 删除。
	// 约定与其它写面一致——会话 + CSRF + Origin 严格相等。注意 registerProjectRoute
	// 只把 POST/PATCH 当写操作，**DELETE 不在其列**；这里显式把 DELETE 也按写处理，
	// 否则删除会绕过 CSRF。
	agentWrite := func(w http.ResponseWriter, r *http.Request) bool {
		if auth.Service == nil {
			writeHumanControlError(w, &accessFailure{status: 503, code: "auth_not_configured"})
			return false
		}
		if auth.Origin == "" || len(r.Header.Values("Origin")) != 1 || r.Header.Get("Origin") != auth.Origin {
			writeHumanControlError(w, &accessFailure{status: 403, code: "origin_rejected"})
			return false
		}
		if _, err := auth.Service.AuthenticateProjectRequest(
			r.Context(), cookie(r, sessionCookie), r.Header.Get("X-CSRF-Token"), true); err != nil {
			writeHumanControlError(w, err)
			return false
		}
		return true
	}
	mux.HandleFunc("POST /api/agents", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !agentWrite(w, r) {
			return
		}
		var body struct {
			OrganizationID string `json:"organizationId"`
			Role           string `json:"role"`
			RepositoryID   string `json:"repositoryId"`
			Name           string `json:"name"`
		}
		if err := decodeBody(w, r, &body); err != nil {
			return
		}
		agentID, err := assemblySvc.CreateAgent(r.Context(), body.OrganizationID, body.Role, body.RepositoryID, body.Name)
		if err != nil {
			writeHumanControlError(w, &accessFailure{status: 422, code: "VALIDATION_FAILED"})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"id": agentID})
	})
	mux.HandleFunc("DELETE /api/agents/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !agentWrite(w, r) {
			return
		}
		if err := assemblySvc.DeleteAgent(r.Context(), r.PathValue("id")); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeHumanControlError(w, &accessFailure{status: 404, code: "NOT_FOUND"})
				return
			}
			writeHumanControlError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	// PATCH /api/agents/{id} —— 设置智能体的预设提示词与 CLI 工具（迁移 0035）。
	// 同一套写守卫（会话 + CSRF + Origin）；PATCH 本就在 registerProjectRoute 的
	// 写集合里，这里沿用显式守卫以保持一致。
	mux.HandleFunc("PATCH /api/agents/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !agentWrite(w, r) {
			return
		}
		var body struct {
			Prompt  string `json:"prompt"`
			CLIKind string `json:"cliKind"`
		}
		if err := decodeBody(w, r, &body); err != nil {
			return
		}
		if err := assemblySvc.UpdateAgentProfile(r.Context(), r.PathValue("id"), body.Prompt, body.CLIKind); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeHumanControlError(w, &accessFailure{status: 404, code: "NOT_FOUND"})
				return
			}
			writeHumanControlError(w, &accessFailure{status: 422, code: "VALIDATION_FAILED"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	registerProjectRoute(mux, "POST /api/projects/{projectId}/topologies", auth, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
		var command struct {
			OrganizationID string   `json:"organizationId"`
			Repositories   []string `json:"repositories"`
			WorkersPerRepo int      `json:"workersPerRepo"`
			LeaderName     string   `json:"leaderName"`
		}
		if err := decodeBody(w, r, &command); err != nil {
			return err
		}
		if command.OrganizationID == "" {
			command.OrganizationID = r.PathValue("projectId")
		}
		result, err := assemblySvc.Assemble(r.Context(), assembly.AssemblyCommand{
			OrganizationID: command.OrganizationID,
			// 团队行需要项目作用域；路由本身就是 /api/projects/{projectId}/topologies，
			// 项目 id 直接取路径值——不必让前端再传一遍（前端传了也不用）。
			ProjectID:      r.PathValue("projectId"),
			Repositories:   command.Repositories,
			WorkersPerRepo: command.WorkersPerRepo,
			LeaderName:     command.LeaderName,
		})
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusCreated, result)
		return nil
	})
	registerProjectRoute(mux, "GET /api/projects/{projectId}/topology", auth, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
		rows, err := assemblySvc.ListTopology(r.Context(), r.PathValue("projectId"))
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": rows})
		return nil
	})
}
