package web

import (
	"context"
	"net/http"
	"time"

	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/issues"
)

// registerIssueRoutes wires the B07 read surface: the project issue list, the
// minimal issue detail and the rooms view. All are read-only; none create
// objects or start runtime work.
func registerIssueRoutes(mux *http.ServeMux, auth Auth, issueAPI Issues) {
	registerProjectRoute(mux, "GET /api/projects/{projectId}/issues", auth, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
		return listIssues(w, r, issueAPI.Service, claims)
	})

	// ③ 执行中人工打断的落点：把**人确认过**的新仓库追加进 issue 的仓库范围。
	// 2026-09-20 之前范围只在建 issue 时写入（insertWorkScope），没有任何追加路径 ——
	// "动态引入新仓库"因此整条接不上。这里只做追加，不做"替调用方挂仓库"
	// （挂仓库是另一个动作，服务层会以 409 REPOSITORY_NOT_IN_PROJECT 明确拒绝）。
	registerProjectRoute(mux, "POST /api/projects/{projectId}/issues/{issueId}/scope/repositories", auth, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
		var body struct {
			RepositoryID string "json:\"repositoryId\""
		}
		if err := decodeBody(w, r, &body); err != nil {
			return err
		}
		if err := issueAPI.Service.AppendRepository(r.Context(), claims, r.PathValue("projectId"), r.PathValue("issueId"), body.RepositoryID); err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "appended"})
		return nil
	})
	mux.HandleFunc("GET /api/issues/{issueId}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if auth.Service == nil || issueAPI.Service == nil {
			writeProjectError(w, &access.Failure{Status: 503, Code: "AUTH_NOT_CONFIGURED"})
			return
		}
		projectID := r.URL.Query().Get("projectId")
		if projectID == "" {
			writeProjectError(w, &issues.Failure{Status: 422, Code: "VALIDATION_FAILED"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		claims, err := auth.Service.AuthenticateProjectRequest(ctx, cookie(r, sessionCookie), r.Header.Get("X-CSRF-Token"), false)
		if err != nil {
			writeProjectError(w, err)
			return
		}
		detail, err := issueAPI.Service.GetIssue(ctx, claims, projectID, r.PathValue("issueId"))
		if err != nil {
			writeProjectError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, detail)
	})
	mux.HandleFunc("GET /api/issues/{issueId}/rooms", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if auth.Service == nil || issueAPI.Service == nil {
			writeProjectError(w, &access.Failure{Status: 503, Code: "AUTH_NOT_CONFIGURED"})
			return
		}
		projectID := r.URL.Query().Get("projectId")
		if projectID == "" {
			writeProjectError(w, &issues.Failure{Status: 422, Code: "VALIDATION_FAILED"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		claims, err := auth.Service.AuthenticateProjectRequest(ctx, cookie(r, sessionCookie), r.Header.Get("X-CSRF-Token"), false)
		if err != nil {
			writeProjectError(w, err)
			return
		}
		view, err := issueAPI.Service.GetIssueRooms(ctx, claims, projectID, r.PathValue("issueId"))
		if err != nil {
			writeProjectError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	})
}

func listIssues(w http.ResponseWriter, r *http.Request, service *issues.Service, claims access.ProjectPrincipal) error {
	projectID := r.PathValue("projectId")
	query, err := issues.ParseIssueListQuery(r.URL.Query().Get("q"), r.URL.Query().Get("repositoryId"), r.URL.Query().Get("cursor"), atoiDefault(r.URL.Query().Get("limit"), 50))
	if err != nil {
		return err
	}
	page, err := service.ListIssues(r.Context(), claims, projectID, query)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, page)
	return nil
}
