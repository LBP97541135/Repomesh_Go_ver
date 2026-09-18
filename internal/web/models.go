package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/models"
	"repomesh.local/repomesh/internal/projects"
)

type Models struct {
	Service      *models.Service
	Tests        *models.TestService
	Applications *models.ApplicationService
}

type modelHandler func(http.ResponseWriter, *http.Request, access.ProjectPrincipal) error

func registerModels(mux *http.ServeMux, auth Auth, api Models) {
	if api.Service == nil {
		return
	}
	registerModelRoute(mux, "GET /api/model-providers", auth, func(w http.ResponseWriter, r *http.Request, principal access.ProjectPrincipal) error {
		query, err := models.ParseListQuery(r.URL.Query().Get("q"), r.URL.Query().Get("cursor"), r.URL.Query().Get("limit"))
		if err != nil {
			return err
		}
		result, err := api.Service.List(r.Context(), principal, query)
		if err == nil {
			writeJSON(w, http.StatusOK, result)
		}
		return err
	})
	registerModelRoute(mux, "GET /api/model-providers/{id}", auth, func(w http.ResponseWriter, r *http.Request, principal access.ProjectPrincipal) error {
		result, err := api.Service.Get(r.Context(), principal, r.PathValue("id"))
		if err == nil {
			writeJSON(w, http.StatusOK, result)
		}
		return err
	})
	registerModelRoute(mux, "GET /api/model-providers/{id}/versions/{revision}", auth, func(w http.ResponseWriter, r *http.Request, principal access.ProjectPrincipal) error {
		result, err := api.Service.GetVersion(r.Context(), principal, r.PathValue("id"), r.PathValue("revision"))
		if err == nil {
			writeJSON(w, http.StatusOK, result)
		}
		return err
	})
	registerModelRoute(mux, "POST /api/model-provider-saves", auth, func(w http.ResponseWriter, r *http.Request, principal access.ProjectPrincipal) error {
		body, err := readModelBody(w, r)
		if err != nil {
			return err
		}
		key, err := projectIdempotencyKey(r)
		if err != nil {
			return err
		}
		command, err := models.NewSaveCommand(key, "", body)
		if err != nil {
			return err
		}
		result, err := api.Service.Save(r.Context(), principal, command)
		if err != nil {
			return err
		}
		writeJSON(w, result.HTTPStatus, result.Receipt)
		return nil
	})
	registerModelRoute(mux, "GET /api/model-provider-saves/{saveId}", auth, func(w http.ResponseWriter, r *http.Request, principal access.ProjectPrincipal) error {
		result, err := api.Service.GetSave(r.Context(), principal, r.PathValue("saveId"))
		if err == nil {
			writeJSON(w, http.StatusOK, result)
		}
		return err
	})
	registerModelRoute(mux, "POST /api/model-provider-saves/{saveId}/close", auth, func(w http.ResponseWriter, r *http.Request, principal access.ProjectPrincipal) error {
		data, err := readCloseBody(w, r)
		if err != nil {
			return err
		}
		key, err := projectIdempotencyKey(r)
		if err != nil {
			return err
		}
		command, err := models.NewCloseCommand(r.PathValue("saveId"), key, data)
		if err != nil {
			return err
		}
		result, err := api.Service.CloseSave(r.Context(), principal, command)
		if err == nil {
			writeJSON(w, http.StatusOK, result)
		}
		return err
	})
	if api.Tests != nil {
		registerModelRoute(mux, "POST /api/model-test-previews", auth, func(w http.ResponseWriter, r *http.Request, principal access.ProjectPrincipal) error {
			data, err := readSnapshotBody(w, r)
			if err != nil {
				return err
			}
			target, err := models.ReadSnapshotTarget(data)
			if err != nil {
				return err
			}
			result, err := api.Tests.Preview(r.Context(), principal, target)
			if err == nil {
				writeJSON(w, http.StatusOK, result)
			}
			return err
		})
		registerModelRoute(mux, "POST /api/model-tests", auth, func(w http.ResponseWriter, r *http.Request, principal access.ProjectPrincipal) error {
			data, err := readSnapshotBody(w, r)
			if err != nil {
				return err
			}
			key, err := projectIdempotencyKey(r)
			if err != nil {
				return err
			}
			command, err := models.ParseTestCommand(key, data)
			if err != nil {
				return err
			}
			result, err := api.Tests.Submit(r.Context(), principal, command)
			if err == nil {
				writeJSON(w, http.StatusAccepted, result)
			}
			return err
		})
		registerModelRoute(mux, "GET /api/model-tests/{testId}", auth, func(w http.ResponseWriter, r *http.Request, principal access.ProjectPrincipal) error {
			result, err := api.Tests.Get(r.Context(), principal, r.PathValue("testId"))
			if err == nil {
				writeJSON(w, http.StatusOK, result)
			}
			return err
		})
	}
	if api.Applications != nil {
		registerProjectRoute(mux, "POST /api/projects/{projectId}/model-application-previews", auth, func(w http.ResponseWriter, r *http.Request, principal access.ProjectPrincipal) error {
			data, err := readSnapshotBody(w, r)
			if err != nil {
				return err
			}
			target, err := models.ReadSnapshotTarget(data)
			if err != nil {
				return err
			}
			result, err := api.Applications.Preview(r.Context(), principal, r.PathValue("projectId"), target)
			if err == nil {
				writeJSON(w, http.StatusOK, result)
			}
			return err
		})
		registerProjectRoute(mux, "POST /api/projects/{projectId}/model-applications", auth, func(w http.ResponseWriter, r *http.Request, principal access.ProjectPrincipal) error {
			data, err := readSnapshotBody(w, r)
			if err != nil {
				return err
			}
			key, err := projectIdempotencyKey(r)
			if err != nil {
				return err
			}
			command, err := models.ParseApplicationCommand(r.PathValue("projectId"), key, newRequestID(), data)
			if err != nil {
				return err
			}
			result, err := api.Applications.Apply(r.Context(), principal, command)
			if err == nil {
				writeJSON(w, http.StatusOK, result)
			}
			return err
		})
		registerProjectRoute(mux, "GET /api/projects/{projectId}/model-applications/{applicationId}", auth, func(w http.ResponseWriter, r *http.Request, principal access.ProjectPrincipal) error {
			result, err := api.Applications.Get(r.Context(), principal, r.PathValue("projectId"), r.PathValue("applicationId"))
			if err == nil {
				writeJSON(w, http.StatusOK, result)
			}
			return err
		})
	}
}

// newRequestID generates the 32-hex request identifier ParseApplicationCommand
// requires; the application service journals it alongside the operation.
func newRequestID() string {
	var data [16]byte
	rand.Read(data[:])
	return hex.EncodeToString(data[:])
}

func registerModelRoute(mux *http.ServeMux, pattern string, auth Auth, handler modelHandler) {
	mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if auth.Service == nil {
			writeProjectError(w, &access.Failure{Status: 503, Code: "AUTH_NOT_CONFIGURED"})
			return
		}
		write := r.Method == http.MethodPost
		if write && (auth.Origin == "" || len(r.Header.Values("Origin")) != 1 || r.Header.Get("Origin") != auth.Origin) {
			writeProjectError(w, &access.Failure{Status: 403, Code: "ORIGIN_REJECTED"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		principal, err := auth.Service.AuthenticateProjectRequest(ctx, cookie(r, sessionCookie), r.Header.Get("X-CSRF-Token"), write)
		if err != nil {
			writeProjectError(w, err)
			return
		}
		if err := handler(w, r.WithContext(ctx), principal); err != nil {
			writeModelError(w, err)
		}
	})
}

func readModelBody(w http.ResponseWriter, r *http.Request) (models.RawSaveBody, error) {
	data, err := readJSONBody(w, r, 256*1024)
	if err != nil {
		return models.RawSaveBody{}, err
	}
	return models.ReadSaveBody(data)
}

// readSnapshotBody reads the shared preview/submit JSON body with the same
// media-type and size discipline as the save endpoints.
func readSnapshotBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	return readJSONBody(w, r, 256*1024)
}

func readJSONBody(w http.ResponseWriter, r *http.Request, limit int64) ([]byte, error) {
	contentType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" || len(params) > 1 || len(params) == 1 && !strings.EqualFold(params["charset"], "utf-8") {
		return nil, &models.Failure{Status: 415, Code: "UNSUPPORTED_MEDIA_TYPE", FieldErrors: []models.FieldError{}}
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return nil, &models.Failure{Status: 413, Code: "REQUEST_TOO_LARGE", FieldErrors: []models.FieldError{}}
	}
	if err != nil {
		return nil, &models.Failure{Status: 400, Code: "INVALID_JSON", FieldErrors: []models.FieldError{}}
	}
	return data, nil
}

func readCloseBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	return readJSONBody(w, r, 256)
}

func writeModelError(w http.ResponseWriter, err error) {
	var modelFailure *models.Failure
	if errors.As(err, &modelFailure) {
		fields := make([]projects.FieldError, 0, len(modelFailure.FieldErrors))
		for _, field := range modelFailure.FieldErrors {
			fields = append(fields, projects.FieldError{Field: field.Field, Code: field.Code})
		}
		writeProjectError(w, &projects.Failure{Status: modelFailure.Status, Code: modelFailure.Code, FieldErrors: fields})
		return
	}
	var submitFailure *models.TestSubmitFailure
	if errors.As(err, &submitFailure) {
		fields := make([]projects.FieldError, 0, len(submitFailure.FieldErrors))
		for _, field := range submitFailure.FieldErrors {
			fields = append(fields, projects.FieldError{Field: field.Field, Code: field.Code})
		}
		writeProjectError(w, &projects.Failure{Status: submitFailure.Status, Code: submitFailure.Code, FieldErrors: fields, Details: submitFailure.Details})
		return
	}
	writeProjectError(w, err)
}

func modelBrowserRoute(path string) bool {
	if path == "/settings/models" || path == "/settings/model-tests" {
		return true
	}
	if strings.HasPrefix(path, "/settings/model-tests/") {
		id := strings.TrimPrefix(path, "/settings/model-tests/")
		return browserOperationID(id)
	}
	if !strings.HasPrefix(path, "/settings/model-saves/") || strings.HasSuffix(path, "/") || strings.Contains(path, "%") {
		return false
	}
	id := strings.TrimPrefix(path, "/settings/model-saves/")
	return browserOperationID(id)
}

func browserOperationID(id string) bool {
	if strings.HasSuffix(id, "/") || strings.Contains(id, "/") || strings.Contains(id, "%") || !utf8.ValidString(id) || len(id) != 36 {
		return false
	}
	return access.ValidID(strings.ToLower(id))
}
