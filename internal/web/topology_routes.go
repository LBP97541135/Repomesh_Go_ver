package web

import (
	"net/http"

	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/assembly"
)

// registerTopologyRoutes exposes the M4 assembly as the project topology API
// (Py: human_control.py /projects/topologies + automatic-topologies + {id}/topology).
func registerTopologyRoutes(mux *http.ServeMux, auth Auth, assemblySvc *assembly.Service) {
	if assemblySvc == nil {
		return
	}
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
