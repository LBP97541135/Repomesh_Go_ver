package web

import (
	"net/http"

	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/handoff"
)

// HandoffDocs carries the P1 handoff document service.
type HandoffDocs struct {
	Service *handoff.Service
}

// registerHandoffRoutes wires create/list for handoff documents.
func registerHandoffRoutes(mux *http.ServeMux, auth Auth, docs HandoffDocs) {
	if docs.Service == nil {
		return
	}
	registerProjectRoute(mux, "POST /api/projects/{projectId}/plans/{planId}/handoffs", auth, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
		var command handoff.CreateCommand
		command.ProjectID = r.PathValue("projectId")
		command.PlanID = r.PathValue("planId")
		command.AuthorID = claims.ActorID()
		if err := decodeBody(w, r, &command); err != nil {
			return err
		}
		view, err := docs.Service.Create(r.Context(), command)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusCreated, view)
		return nil
	})
	registerProjectRoute(mux, "GET /api/projects/{projectId}/plans/{planId}/handoffs", auth, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
		items, err := docs.Service.List(r.Context(), r.PathValue("planId"))
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
		return nil
	})
}
