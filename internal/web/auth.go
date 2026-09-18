package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/jsoninput"
)

const sessionCookie = "__Host-repomesh-session"
const bindingCookie = "__Host-repomesh-binding"

type Auth struct {
	Service *access.Service
	Origin  string
}

func registerAuth(mux *http.ServeMux, auth Auth) {
	route := func(pattern string, handler func(http.ResponseWriter, *http.Request) error) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Referrer-Policy", "no-referrer")
			if auth.Service == nil {
				authError(w, &access.Failure{Status: 503, Code: "AUTH_NOT_CONFIGURED"})
				return
			}
			if r.Method == http.MethodPost && (auth.Origin == "" || r.Header.Get("Origin") != auth.Origin || len(r.Header.Values("Origin")) != 1) {
				authError(w, &access.Failure{Status: 403, Code: "ORIGIN_REJECTED"})
				return
			}
			timeout := 15 * time.Second
			if pattern == "GET /api/repositories/candidates" {
				timeout = 45 * time.Second
			}
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()
			if err := handler(w, r.WithContext(ctx)); err != nil {
				authError(w, err)
			}
		})
	}
	route("GET /api/session", func(w http.ResponseWriter, r *http.Request) error {
		result, err := auth.Service.Session(r.Context(), cookie(r, sessionCookie))
		if err == nil {
			writeJSON(w, 200, result)
		}
		return err
	})
	for _, purpose := range []string{"login", "reconnect"} {
		route("POST /api/auth/github/"+purpose, func(w http.ResponseWriter, r *http.Request) error {
			var body struct {
				Destination json.RawMessage `json:"destination"`
			}
			if err := readJSON(w, r, &body); err != nil {
				return err
			}
			destination, err := access.ParseDestination(body.Destination)
			if err != nil {
				return err
			}
			if len(r.Header.Values("Idempotency-Key")) != 1 {
				return &access.Failure{Status: 422, Code: "VALIDATION_FAILED"}
			}
			result, err := auth.Service.Start(r.Context(), access.StartCommand{ID: r.Header.Get("Idempotency-Key"), Purpose: purpose, BindingCookie: cookie(r, bindingCookie), SessionCookie: cookie(r, sessionCookie), CSRF: r.Header.Get("X-CSRF-Token"), Destination: destination})
			if err != nil {
				return err
			}
			if result.BindingCookie != "" {
				setCookie(w, bindingCookie, result.BindingCookie, 7*24*60*60)
			}
			status := http.StatusOK
			if result.Created {
				status = http.StatusCreated
			}
			writeJSON(w, status, result.Result)
			return nil
		})
	}
	route("GET /api/auth/github/callback", func(w http.ResponseWriter, r *http.Request) error {
		query := r.URL.Query()
		input := access.Callback{BindingCookie: cookie(r, bindingCookie), State: query.Get("state"), Code: query.Get("code"), ProviderError: query.Get("error")}
		for _, key := range []string{"state", "code", "error"} {
			if len(query[key]) > 1 {
				input.State = ""
			}
		}
		result, err := auth.Service.CompleteCallback(r.Context(), input)
		if result.Page == "" {
			result.Page = "/login"
		}
		if result.SessionCookie != "" && err == nil {
			setCookie(w, sessionCookie, result.SessionCookie, 12*60*60)
		}
		http.Redirect(w, r, result.Page, http.StatusSeeOther)
		return nil
	})
	route("GET /api/auth/attempts/{id}", func(w http.ResponseWriter, r *http.Request) error {
		result, err := auth.Service.Attempt(r.Context(), cookie(r, bindingCookie), cookie(r, sessionCookie), r.PathValue("id"))
		if err == nil {
			writeJSON(w, 200, result)
		}
		return err
	})
	route("POST /api/auth/logout", func(w http.ResponseWriter, r *http.Request) error {
		var body struct{}
		if err := readJSON(w, r, &body); err != nil {
			return err
		}
		if err := auth.Service.Logout(r.Context(), cookie(r, sessionCookie), r.Header.Get("X-CSRF-Token")); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	})
	// GET /api/repositories 由 scan 块注册（仓库扫描目录，D 板块 as-built）。
	// B02 的"可参与仓库候选"候选路线挪至 /api/repositories/candidates——
	// 两处同路径注册会在启动时 panic（验收发现）。
	route("GET /api/repositories/candidates", func(w http.ResponseWriter, r *http.Request) error {
		query, err := parseRepositoryQuery(r)
		if err != nil {
			return err
		}
		result, err := auth.Service.Repositories(r.Context(), cookie(r, sessionCookie), query)
		if err == nil {
			writeJSON(w, 200, result)
		}
		return err
	})
}

func parseRepositoryQuery(r *http.Request) (access.RepositoryQuery, error) {
	values := r.URL.Query()
	query := access.RepositoryQuery{Text: values.Get("q"), Cursor: values.Get("cursor"), Limit: 50}
	for key, value := range values {
		if len(value) != 1 || key != "q" && key != "cursor" && key != "limit" && key != "refresh" {
			return query, &access.Failure{Status: 422, Code: "VALIDATION_FAILED"}
		}
	}
	if raw, ok := values["limit"]; ok {
		value, err := strconv.Atoi(raw[0])
		if err != nil {
			return query, &access.Failure{Status: 422, Code: "VALIDATION_FAILED"}
		}
		query.Limit = value
	}
	if raw, ok := values["refresh"]; ok {
		if raw[0] != "1" {
			return query, &access.Failure{Status: 422, Code: "VALIDATION_FAILED"}
		}
		query.Refresh = true
	}
	return query, nil
}

func readJSON(w http.ResponseWriter, r *http.Request, target any) error {
	contentType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" || len(params) > 1 || len(params) == 1 && !strings.EqualFold(params["charset"], "utf-8") {
		return &access.Failure{Status: 415, Code: "UNSUPPORTED_MEDIA_TYPE"}
	}
	const limit = 256 * 1024
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return &access.Failure{Status: 413, Code: "PAYLOAD_TOO_LARGE"}
	}
	data = bytes.TrimSpace(data)
	if err != nil || len(data) == 0 || data[0] != '{' || jsoninput.Decode(data, target, limit) != nil {
		return &access.Failure{Status: 422, Code: "VALIDATION_FAILED"}
	}
	return nil
}

func cookie(r *http.Request, name string) string {
	matches := r.CookiesNamed(name)
	if len(matches) != 1 {
		return ""
	}
	return matches[0].Value
}
func setCookie(w http.ResponseWriter, name, value string, seconds int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: seconds})
}
func authError(w http.ResponseWriter, err error) {
	failure := &access.Failure{Status: 503, Code: "RESULT_UNCONFIRMED"}
	var typed *access.Failure
	if errors.As(err, &typed) {
		failure = typed
	}
	if failure.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(failure.RetryAfter))
	}
	writeJSON(w, failure.Status, map[string]any{"error": map[string]any{"code": failure.Code, "message": "The request could not be confirmed.", "details": map[string]any{}}})
}
