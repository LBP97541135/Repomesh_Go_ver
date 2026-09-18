package web

import (
	"net/http"

	"repomesh.local/repomesh/internal/console"
)

// Console carries the directory read services into the web layer.
type Console struct {
	Service *console.Service
}

// registerConsoleRoutes wires the contract v0.2 console directory endpoints
// (organizations / repositories / teams / agents) with session auth.
func registerConsoleRoutes(mux *http.ServeMux, auth Auth, consoleAPI Console) {
	if consoleAPI.Service == nil {
		return
	}
	guard := func(w http.ResponseWriter, r *http.Request) error {
		if auth.Service == nil {
			return &accessFailure{status: 503, code: "auth_not_configured"}
		}
		_, err := auth.Service.AuthenticateProjectRequest(r.Context(), cookie(r, sessionCookie), r.Header.Get("X-CSRF-Token"), false)
		return err
	}
	register := func(pattern string, handler http.HandlerFunc) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			if err := guard(w, r); err != nil {
				writeHumanControlError(w, err)
				return
			}
			handler(w, r)
		})
	}
	withRuntime := func(r *http.Request) bool {
		return r.URL.Query().Get("with_runtime") != "false"
	}

	register("GET /api/console/organizations", func(w http.ResponseWriter, r *http.Request) {
		result, err := consoleAPI.Service.Organizations(r.Context())
		if err != nil {
			writeHumanControlError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	register("GET /api/console/agents", func(w http.ResponseWriter, r *http.Request) {
		result, err := consoleAPI.Service.Agents(r.Context(), withRuntime(r))
		if err != nil {
			writeHumanControlError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	register("GET /api/console/teams", func(w http.ResponseWriter, r *http.Request) {
		result, err := consoleAPI.Service.Teams(r.Context(), withRuntime(r))
		if err != nil {
			writeHumanControlError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	register("GET /api/console/repositories", func(w http.ResponseWriter, r *http.Request) {
		result, err := consoleAPI.Service.Repositories(r.Context())
		if err != nil {
			writeHumanControlError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
}
