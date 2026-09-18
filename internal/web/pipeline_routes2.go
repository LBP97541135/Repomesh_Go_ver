package web

import (
	"encoding/json"
	"io"
	"net/http"

	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/branchvalidation"
	"repomesh.local/repomesh/internal/delivery"
	"repomesh.local/repomesh/internal/interfacedoc"
)

// pipeline_routes2.go: M7 interface-doc, M8 branch validation, M9
// observability and M6 delivery routes.

func registerPipelineRoutes2(mux *http.ServeMux, auth Auth, pipeline Pipeline) {
	if pipeline.InterfaceDoc != nil {
		registerProjectRoute(mux, "POST /api/projects/{projectId}/interface-documents", auth, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
			var command interfacedoc.CreateCommand
			command.ProjectID = r.PathValue("projectId")
			command.AuthorID = claims.ActorID()
			if err := decodeBody(w, r, &command); err != nil {
				return err
			}
			doc, err := pipeline.InterfaceDoc.Create(r.Context(), command)
			if err != nil {
				return err
			}
			writeJSON(w, http.StatusCreated, doc)
			return nil
		})
		registerProjectRoute(mux, "POST /api/projects/{projectId}/interface-documents/{docId}/approve", auth, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
			doc, err := pipeline.InterfaceDoc.Approve(r.Context(), r.PathValue("docId"), claims.ActorID())
			if err != nil {
				return err
			}
			writeJSON(w, http.StatusOK, doc)
			return nil
		})
		registerProjectRoute(mux, "GET /api/projects/{projectId}/interface-documents/{docId}", auth, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
			doc, err := pipeline.InterfaceDoc.Get(r.Context(), r.PathValue("docId"))
			if err != nil {
				return err
			}
			writeJSON(w, http.StatusOK, doc)
			return nil
		})
	}

	if pipeline.BranchValid != nil {
		registerProjectRoute(mux, "POST /api/projects/{projectId}/branch-validations", auth, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
			var command branchvalidation.StartCommand
			command.ProjectID = r.PathValue("projectId")
			if err := decodeBody(w, r, &command); err != nil {
				return err
			}
			run, err := pipeline.BranchValid.Start(r.Context(), command)
			if err != nil {
				return err
			}
			writeJSON(w, http.StatusCreated, run)
			return nil
		})
		registerProjectRoute(mux, "GET /api/projects/{projectId}/branch-validations/{runId}", auth, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
			run, err := pipeline.BranchValid.Get(r.Context(), r.PathValue("runId"))
			if err != nil {
				return err
			}
			writeJSON(w, http.StatusOK, run)
			return nil
		})
		registerProjectRoute(mux, "POST /api/projects/{projectId}/branch-validations/{runId}/retry-cleanup", auth, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
			run, err := pipeline.BranchValid.RetryCleanup(r.Context(), r.PathValue("runId"))
			if err != nil {
				return err
			}
			writeJSON(w, http.StatusOK, run)
			return nil
		})
	}

	if pipeline.Observation != nil {
		mux.HandleFunc("GET /api/observability/traces", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			if !authorizePipeline(w, r, auth) {
				return
			}
			sessions, err := pipeline.Observation.ListTraceSessions(r.Context(), atoiDefault(r.URL.Query().Get("limit"), 50))
			if err != nil {
				writePipelineError(w, 500, "QUERY_FAILED")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"items": sessions})
		})
		mux.HandleFunc("GET /api/observability/traces/{sessionId}/events", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			if !authorizePipeline(w, r, auth) {
				return
			}
			events, err := pipeline.Observation.ListTraceEvents(r.Context(), r.PathValue("sessionId"), atoiDefault(r.URL.Query().Get("limit"), 200))
			if err != nil {
				writePipelineError(w, 500, "QUERY_FAILED")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"items": events})
		})
		mux.HandleFunc("GET /api/observability/logs", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			if !authorizePipeline(w, r, auth) {
				return
			}
			logs, err := pipeline.Observation.ListLogs(r.Context(), r.URL.Query().Get("level"), atoiDefault(r.URL.Query().Get("limit"), 200))
			if err != nil {
				writePipelineError(w, 500, "QUERY_FAILED")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"items": logs})
		})
		mux.HandleFunc("GET /api/observability/alerts", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			if !authorizePipeline(w, r, auth) {
				return
			}
			events, err := pipeline.Observation.ListAlertEvents(r.Context(), r.URL.Query().Get("orgId"), r.URL.Query().Get("open") == "1", atoiDefault(r.URL.Query().Get("limit"), 200))
			if err != nil {
				writePipelineError(w, 500, "QUERY_FAILED")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"items": events})
		})
	}

	if pipeline.Delivery != nil {
		registerProjectRoute(mux, "POST /api/projects/{projectId}/releases", auth, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
			var command delivery.ReleaseCommand
			if err := decodeBody(w, r, &command); err != nil {
				return err
			}
			// The SCM token comes from the platform's credential store, never
			// from the request body; the release route accepts the workspace
			// coordinates only. Token resolution is a wiring-time concern.
			command.Token = pipeline.DeliveryToken
			result, err := pipeline.Delivery.Release(r.Context(), command)
			if err != nil {
				return err
			}
			writeJSON(w, http.StatusCreated, result)
			return nil
		})
	}
}

func decodeBody(w http.ResponseWriter, r *http.Request, target any) error {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 256*1024))
	if err != nil {
		writePipelineError(w, 400, "INVALID_JSON")
		return errStopped
	}
	if err := json.Unmarshal(data, target); err != nil {
		writePipelineError(w, 400, "INVALID_JSON")
		return errStopped
	}
	return nil
}

var errStopped = &pipelineStopped{}

type pipelineStopped struct{}

func (*pipelineStopped) Error() string { return "pipeline: response already written" }

func writePipelineError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{"code": code, "message": "The request could not be completed."},
	})
}

// authorizePipeline reuses the project-session authentication for the
// non-project-scoped pipeline routes (assembly/observability). Project-scoped
// routes go through registerProjectRoute instead.
func authorizePipeline(w http.ResponseWriter, r *http.Request, auth Auth) bool {
	if auth.Service == nil {
		writePipelineError(w, 503, "AUTH_NOT_CONFIGURED")
		return false
	}
	_, err := auth.Service.AuthenticateProjectRequest(r.Context(), cookie(r, sessionCookie), r.Header.Get("X-CSRF-Token"), false)
	if err != nil {
		writePipelineError(w, 401, "AUTHENTICATION_REQUIRED")
		return false
	}
	return true
}

var _ = access.ProjectPrincipal{}
