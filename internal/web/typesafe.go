package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/typesafe"
)

func decodeTypeSafe(w http.ResponseWriter, r *http.Request, target any) error {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, typesafe.MaxBody))
	d.DisallowUnknownFields()
	if d.Decode(target) != nil || d.Decode(new(any)) != io.EOF {
		return &access.Failure{Status: 400, Code: "TYPESAFE_INVALID_INPUT"}
	}
	return nil
}
func typeSafeError(err error) error {
	if err == nil {
		return nil
	}
	var f *typesafe.Failure
	if errors.As(err, &f) {
		return &access.Failure{Status: f.Status, Code: f.Code}
	}
	return &access.Failure{Status: 503, Code: "TYPESAFE_UNAVAILABLE"}
}

func registerTypeSafe(mux *http.ServeMux, auth Auth, service *typesafe.Service) {
	register := func(pattern string, fn projectHandler) {
		registerProjectRouteWithTimeout(mux, pattern, auth, 30*time.Second, func(w http.ResponseWriter, r *http.Request, p access.ProjectPrincipal) error {
			if service == nil {
				return &access.Failure{Status: 503, Code: "TYPESAFE_UNAVAILABLE"}
			}
			return fn(w, r, p)
		})
	}
	register("GET /api/projects/{projectId}/typesafe", func(w http.ResponseWriter, r *http.Request, p access.ProjectPrincipal) error {
		v, err := service.Settings(r.Context(), p.ActorID(), r.PathValue("projectId"))
		if err != nil {
			return typeSafeError(err)
		}
		writeJSON(w, 200, v)
		return nil
	})
	register("POST /api/projects/{projectId}/typesafe", func(w http.ResponseWriter, r *http.Request, p access.ProjectPrincipal) error {
		var body typesafe.SaveInput
		if err := decodeTypeSafe(w, r, &body); err != nil {
			return err
		}
		v, err := service.Save(r.Context(), p.ActorID(), r.PathValue("projectId"), body)
		if err != nil {
			return typeSafeError(err)
		}
		writeJSON(w, 200, v)
		return nil
	})
	register("POST /api/projects/{projectId}/typesafe/check", func(w http.ResponseWriter, r *http.Request, p access.ProjectPrincipal) error {
		var body struct {
			ExpectedRevision int64 `json:"expectedRevision"`
		}
		if err := decodeTypeSafe(w, r, &body); err != nil {
			return err
		}
		v, err := service.Check(r.Context(), p.ActorID(), r.PathValue("projectId"), body.ExpectedRevision)
		if err != nil {
			return typeSafeError(err)
		}
		writeJSON(w, 200, v)
		return nil
	})
	register("GET /api/projects/{projectId}/typesafe/evaluations", func(w http.ResponseWriter, r *http.Request, p access.ProjectPrincipal) error {
		items, err := service.List(r.Context(), p.ActorID(), r.PathValue("projectId"), r.URL.Query().Get("issueId"))
		if err != nil {
			return typeSafeError(err)
		}
		writeJSON(w, 200, map[string]any{"items": items})
		return nil
	})
	// The tool endpoint accepts only its scoped bearer grant, never browser cookies.
	mux.HandleFunc("POST /api/typesafe/evaluations", func(w http.ResponseWriter, r *http.Request) {
		if service == nil {
			writeJSON(w, 503, map[string]string{"code": "TYPESAFE_UNAVAILABLE"})
			return
		}
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || len(r.Header.Values("Authorization")) != 1 || len(token) != 43 {
			writeJSON(w, 401, map[string]string{"code": "TYPESAFE_GRANT_REJECTED"})
			return
		}
		var in typesafe.Input
		if err := decodeTypeSafe(w, r, &in); err != nil {
			writeProjectError(w, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		defer cancel()
		result, err := service.Evaluate(ctx, token, in)
		if err != nil {
			writeProjectError(w, typeSafeError(err))
			return
		}
		writeJSON(w, 200, result)
	})
}
