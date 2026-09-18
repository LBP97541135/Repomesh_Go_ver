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
