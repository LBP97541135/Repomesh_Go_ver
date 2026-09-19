package web

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/repositoryteams"
)

type createRepositoryTeamRequest struct {
	WorkerCount *int `json:"worker_count"`
}

type changeRepositoryTeamRequest struct {
	WorkerCount    *int   `json:"worker_count"`
	RosterRevision *int64 `json:"roster_revision"`
}

func registerRepositoryTeams(mux *http.ServeMux, auth Auth, api AgentTeams) {
	register := func(pattern string, write bool, handler func(http.ResponseWriter, *http.Request)) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			if api.RepositoryTeams == nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "service_not_configured"})
				return
			}
			if auth.Service == nil {
				writeRepositoryTeamError(w, &access.Failure{Status: http.StatusServiceUnavailable, Code: "AUTH_NOT_CONFIGURED"})
				return
			}
			if write && (auth.Origin == "" || r.Header.Get("Origin") != auth.Origin || len(r.Header.Values("Origin")) != 1) {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "origin_rejected"})
				return
			}
			principal, err := auth.Service.AuthenticateProjectRequest(r.Context(), cookie(r, sessionCookie), r.Header.Get("X-CSRF-Token"), write)
			if err != nil {
				writeRepositoryTeamError(w, err)
				return
			}
			if write {
				isAdmin, err := auth.Service.IsAdmin(r.Context(), principal.ActorID())
				if err != nil {
					writeRepositoryTeamError(w, err)
					return
				}
				if !isAdmin {
					writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin_required"})
					return
				}
			}
			handler(w, r)
		})
	}

	register("GET /api/repositories/{repositoryId}/agent-team", false, func(w http.ResponseWriter, r *http.Request) {
		snapshot, err := api.RepositoryTeams.Get(r.Context(), r.PathValue("repositoryId"))
		if err != nil {
			writeRepositoryTeamError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, snapshot)
	})

	register("POST /api/repositories/{repositoryId}/agent-team", true, func(w http.ResponseWriter, r *http.Request) {
		var input createRepositoryTeamRequest
		if !decodeRepositoryTeamRequest(w, r, &input) || input.WorkerCount == nil || !validRepositoryTeamWorkerCount(*input.WorkerCount) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
			return
		}
		snapshot, err := api.RepositoryTeams.Create(r.Context(), r.PathValue("repositoryId"), *input.WorkerCount)
		if err != nil {
			writeRepositoryTeamError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, snapshot)
	})

	register("PATCH /api/repositories/{repositoryId}/agent-team", true, func(w http.ResponseWriter, r *http.Request) {
		var input changeRepositoryTeamRequest
		if !decodeRepositoryTeamRequest(w, r, &input) || input.WorkerCount == nil || input.RosterRevision == nil || !validRepositoryTeamWorkerCount(*input.WorkerCount) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
			return
		}
		snapshot, err := api.RepositoryTeams.Change(r.Context(), r.PathValue("repositoryId"), repositoryteams.ChangeCommand{
			WorkerCount: *input.WorkerCount, RosterRevision: *input.RosterRevision,
		})
		if err != nil {
			writeRepositoryTeamError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, snapshot)
	})
}

func decodeRepositoryTeamRequest(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return false
	}
	return decoder.Decode(&struct{}{}) == io.EOF
}

func validRepositoryTeamWorkerCount(workerCount int) bool {
	return workerCount >= 1 && workerCount <= 20
}

func writeRepositoryTeamError(w http.ResponseWriter, err error) {
	var accessFailure *access.Failure
	if errors.As(err, &accessFailure) {
		writeJSON(w, accessFailure.Status, map[string]string{"error": strings.ToLower(accessFailure.Code)})
		return
	}
	var conflict *repositoryteams.ConflictError
	if errors.As(err, &conflict) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "stale_roster", "current": conflict.Current})
		return
	}
	var busyWorkers *repositoryteams.BusyWorkersError
	if errors.As(err, &busyWorkers) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "workers_busy", "workers": busyWorkers.Labels})
		return
	}
	switch {
	case errors.Is(err, repositoryteams.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "team_not_found"})
	case errors.Is(err, repositoryteams.ErrBusyWorkers):
		writeJSON(w, http.StatusConflict, map[string]any{"error": "workers_busy", "workers": []string{}})
	case errors.Is(err, repositoryteams.ErrReconciliationRequired):
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "reconciliation_required"})
	case errors.Is(err, repositoryteams.ErrControllerUnavailable):
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "controller_unavailable"})
	case errors.Is(err, repositoryteams.ErrForbidden):
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
	case errors.Is(err, repositoryteams.ErrConflict):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "roster_conflict"})
	default:
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "service_unavailable"})
	}
}
