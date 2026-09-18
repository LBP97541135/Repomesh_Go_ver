package web

import "net/http"

// Decision carries the 历史决策 block's HTTP registration into the server
// assembly. A zero Decision mounts no routes (the block is optional).
type Decision struct {
	// API mounts the decision endpoints onto the server mux. Implemented
	// by internal/decisionchain's Service.
	API interface {
		RegisterRoutes(mux *http.ServeMux)
	}
}

func registerDecisionRoutes(mux *http.ServeMux, decision Decision) {
	if decision.API != nil {
		decision.API.RegisterRoutes(mux)
	}
}
