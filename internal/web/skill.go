package web

import "net/http"

// Skills carries the skill-governance block's HTTP registration into the
// server assembly. A zero Skills mounts no routes (the block is optional).
type Skills struct {
	// API mounts the skill endpoints onto the server mux. Implemented
	// by internal/skills' Service.
	API interface {
		RegisterRoutes(mux *http.ServeMux)
	}
}

func registerSkillRoutes(mux *http.ServeMux, skills Skills) {
	if skills.API != nil {
		skills.API.RegisterRoutes(mux)
	}
}
