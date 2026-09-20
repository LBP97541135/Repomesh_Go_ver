package web

import (
	"net/http"

	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/gates"
	"repomesh.local/repomesh/internal/spec"
)

// PipelineExtensions carries the P1 additions (spec lifecycle, project gate).
type PipelineExtensions struct {
	Spec  *spec.Service
	Gates *gates.Service
}

// registerPipelineExtensions wires the P1 routes into the same authenticated
// project-route family.
func registerPipelineExtensions(mux *http.ServeMux, auth Auth, extensions PipelineExtensions) {
	if extensions.Spec != nil {
		registerProjectRoute(mux, "POST /api/projects/{projectId}/specs", auth, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
			var command spec.CreateCommand
			// body: { issueId, title, content, repository? } —— issueId 是身份（规格以 issue 为单位）
			command.ProjectID = r.PathValue("projectId")
			command.AuthorID = claims.ActorID()
			if err := decodeBody(w, r, &command); err != nil {
				return err
			}
			view, err := extensions.Spec.Create(r.Context(), command)
			if err != nil {
				return err
			}
			writeJSON(w, http.StatusCreated, view)
			return nil
		})
		registerProjectRoute(mux, "POST /api/projects/{projectId}/specs/{specId}/approve", auth, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
			view, err := extensions.Spec.Approve(r.Context(), r.PathValue("specId"), claims.ActorID())
			if err != nil {
				return err
			}
			writeJSON(w, http.StatusOK, view)
			return nil
		})
		registerProjectRoute(mux, "GET /api/projects/{projectId}/specs/current", auth, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
			// 规格以 **issue** 为单位（2026-09-20 用户裁定）：读面按 issueId 取当前生效规格。
			view, err := extensions.Spec.Current(r.Context(), r.PathValue("projectId"), r.URL.Query().Get("issueId"))
			if err != nil {
				return err
			}
			writeJSON(w, http.StatusOK, view)
			return nil
		})
	}

	if extensions.Gates != nil {
		registerProjectRoute(mux, "GET /api/projects/{projectId}/release-gate", auth, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
			view, err := extensions.Gates.Evaluate(r.Context(), r.PathValue("projectId"))
			if err != nil {
				return err
			}
			writeJSON(w, http.StatusOK, view)
			return nil
		})
		registerProjectRoute(mux, "POST /api/projects/{projectId}/release-gate", auth, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
			var body struct {
				Decision string `json:"decision"`
				Summary  string `json:"summary"`
			}
			if err := decodeBody(w, r, &body); err != nil {
				return err
			}
			view, err := extensions.Gates.Record(r.Context(), r.PathValue("projectId"), claims.ActorID(), body.Decision, body.Summary)
			if err != nil {
				return err
			}
			writeJSON(w, http.StatusOK, view)
			return nil
		})
	}
}
