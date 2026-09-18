package web

import (
	"net/http"

	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/jointvalidation"
)

// JointValidation carries the cross-repo contract comparison service.
type JointValidation struct {
	Service *jointvalidation.Service
}

// registerJointValidation exposes the contract comparison endpoint.
func registerJointValidation(mux *http.ServeMux, auth Auth, jv JointValidation) {
	if jv.Service == nil {
		return
	}
	registerProjectRoute(mux, "POST /api/projects/{projectId}/joint-validation", auth, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
		var command jointvalidation.CompareCommand
		if err := decodeBody(w, r, &command); err != nil {
			return err
		}
		result, err := jv.Service.Compare(r.Context(), command)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, result)
		return nil
	})
}
