package web

import (
	"net/http"

	"repomesh.local/repomesh/internal/responsibility"
)

var responsibilitySvc *responsibility.Service

// SetResponsibilityService wires the composition root pool.
func SetResponsibilityService(svc *responsibility.Service) { responsibilitySvc = svc }

func registerResponsibilityRoutes(mux *http.ServeMux, svc *responsibility.Service) {
	if svc != nil {
		svc.RegisterRoutes(mux)
	}
}
