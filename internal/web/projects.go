package web

import (
	"context"
	"crypto/rand"
	"log"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/issues"
	"repomesh.local/repomesh/internal/models"
	"repomesh.local/repomesh/internal/projects"
)

type Projects struct {
	Service *projects.Service
	// OnRepositoriesAttached 在项目**新建/更新成功之后**被调用（best-effort，异步）。
	//
	// 2026-09-20（用户）：仓库接入的时候要自动为它建一支智能体团队，不该让人再去
	// 仓库页点一次。这里只发一个"这个项目刚变过"的信号，具体做什么由装配方决定
	// （main.go 接到 repositoryteams.EnsureForProject）——web 层不认识 AgentTeams。
	// 失败不回滚项目：建队是后续动作，不是项目写入的一部分。
	OnRepositoriesAttached func(ctx context.Context, projectID string)
}

// notifyRepositoriesAttached 异步通知装配方。用 context.WithoutCancel 保证请求
// 返回后这次建队仍能跑完（它不该被 HTTP 的 15 秒超时打断）。
func (p Projects) notifyRepositoriesAttached(ctx context.Context, projectID string) {
	if p.OnRepositoriesAttached == nil || projectID == "" {
		return
	}
	hook := p.OnRepositoriesAttached
	go hook(context.WithoutCancel(ctx), projectID)
}
type projectHandler func(http.ResponseWriter, *http.Request, access.ProjectPrincipal) error
type projectErrorBody struct {
	Error projectError `json:"error"`
}
type projectError struct {
	Code        string                         `json:"code"`
	Message     string                         `json:"message"`
	FieldErrors []projects.FieldError          `json:"fieldErrors"`
	RequestID   string                         `json:"requestId"`
	Details     *models.TestOutstandingDetails `json:"details,omitempty"`
}

func registerProjects(mux *http.ServeMux, auth Auth, projectAPI Projects) {
	registerProjectRoute(mux, "POST /api/projects", auth, func(w http.ResponseWriter, r *http.Request, principal access.ProjectPrincipal) error {
		if projectAPI.Service == nil {
			return &projects.Failure{Status: 503, Code: "RESULT_UNCONFIRMED", FieldErrors: []projects.FieldError{}}
		}
		input, err := readProjectInput(w, r)
		if err != nil {
			return err
		}
		key, err := projectIdempotencyKey(r)
		if err != nil {
			return err
		}
		command, err := projects.PrepareCreate(input, key)
		if err != nil {
			return err
		}
		result, err := projectAPI.Service.Create(r.Context(), principal, command)
		if err != nil {
			return err
		}
		status := http.StatusOK
		if result.FirstCommit {
			status = http.StatusCreated
		}
		writeJSON(w, status, result.Receipt)
		// 新建项目就把它的仓库接入同一件事：给每个仓库自动建一支智能体团队。
		projectAPI.notifyRepositoriesAttached(r.Context(), result.Receipt.ProjectID)
		return nil
	})
	registerProjectRoute(mux, "GET /api/project-creations/{projectCreationId}", auth, func(w http.ResponseWriter, r *http.Request, principal access.ProjectPrincipal) error {
		if projectAPI.Service == nil {
			return &projects.Failure{Status: 503, Code: "RESULT_UNCONFIRMED", FieldErrors: []projects.FieldError{}}
		}
		result, err := projectAPI.Service.Creation(r.Context(), principal, r.PathValue("projectCreationId"))
		if err == nil {
			writeJSON(w, http.StatusOK, result)
		}
		return err
	})
	registerProjectRoute(mux, "PATCH /api/projects/{projectId}", auth, func(w http.ResponseWriter, r *http.Request, principal access.ProjectPrincipal) error {
		if projectAPI.Service == nil {
			return &projects.Failure{Status: 503, Code: "RESULT_UNCONFIRMED", FieldErrors: []projects.FieldError{}}
		}
		input, err := readProjectInput(w, r)
		if err != nil {
			return err
		}
		key, err := projectIdempotencyKey(r)
		if err != nil {
			return err
		}
		command, err := projects.PrepareUpdate(input, r.PathValue("projectId"), key)
		if err != nil {
			return err
		}
		result, err := projectAPI.Service.Update(r.Context(), principal, command)
		if err == nil {
			writeJSON(w, http.StatusOK, result)
			// 更新项目也可能是在**追加仓库**（仓库页的"接入本项目"就走这条）：
			// 成功之后同样给新仓库自动建队。幂等，已建过的会被跳过。
			projectAPI.notifyRepositoriesAttached(r.Context(), result.ProjectID)
		}
		return err
	})
	registerProjectRoute(mux, "GET /api/projects/{projectId}/updates/{updateId}", auth, func(w http.ResponseWriter, r *http.Request, principal access.ProjectPrincipal) error {
		if projectAPI.Service == nil {
			return &projects.Failure{Status: 503, Code: "RESULT_UNCONFIRMED", FieldErrors: []projects.FieldError{}}
		}
		result, err := projectAPI.Service.UpdateResult(r.Context(), principal, r.PathValue("projectId"), r.PathValue("updateId"))
		if err == nil {
			writeJSON(w, http.StatusOK, result)
		}
		return err
	})
	registerProjectRoute(mux, "GET /api/projects/{projectId}", auth, func(w http.ResponseWriter, r *http.Request, principal access.ProjectPrincipal) error {
		if projectAPI.Service == nil {
			return &projects.Failure{Status: 503, Code: "RESULT_UNCONFIRMED", FieldErrors: []projects.FieldError{}}
		}
		result, err := projectAPI.Service.Get(r.Context(), principal, r.PathValue("projectId"))
		if err == nil {
			writeJSON(w, http.StatusOK, result)
		}
		return err
	})
	registerProjectRoute(mux, "GET /api/projects", auth, func(w http.ResponseWriter, r *http.Request, principal access.ProjectPrincipal) error {
		if projectAPI.Service == nil {
			return &projects.Failure{Status: 503, Code: "RESULT_UNCONFIRMED", FieldErrors: []projects.FieldError{}}
		}
		query, err := projects.ParseListQuery(r.URL.Query())
		if err != nil {
			return err
		}
		result, err := projectAPI.Service.List(r.Context(), principal, query)
		if err == nil {
			writeJSON(w, http.StatusOK, result)
		}
		return err
	})
	registerProjectRoute(mux, "GET /api/projects/{projectId}/repositories", auth, func(w http.ResponseWriter, r *http.Request, principal access.ProjectPrincipal) error {
		if projectAPI.Service == nil {
			return &projects.Failure{Status: 503, Code: "RESULT_UNCONFIRMED", FieldErrors: []projects.FieldError{}}
		}
		query, err := projects.ParsePageQuery(r.URL.Query())
		if err != nil {
			return err
		}
		result, err := projectAPI.Service.Repositories(r.Context(), principal, r.PathValue("projectId"), query)
		if err == nil {
			writeJSON(w, http.StatusOK, result)
		}
		return err
	})
	registerProjectRoute(mux, "GET /api/configuration-profiles", auth, func(w http.ResponseWriter, r *http.Request, principal access.ProjectPrincipal) error {
		if projectAPI.Service == nil {
			return &projects.Failure{Status: 503, Code: "RESULT_UNCONFIRMED", FieldErrors: []projects.FieldError{}}
		}
		query, err := projects.ParseProfileQuery(r.URL.Query())
		if err != nil {
			return err
		}
		result, err := projectAPI.Service.Profiles(r.Context(), principal, query)
		if err == nil {
			writeJSON(w, http.StatusOK, result)
		}
		return err
	})
}

func registerProjectRoute(mux *http.ServeMux, pattern string, auth Auth, handler projectHandler) {
	mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if auth.Service == nil {
			writeProjectError(w, &access.Failure{Status: 503, Code: "AUTH_NOT_CONFIGURED"})
			return
		}
		write := r.Method == http.MethodPost || r.Method == http.MethodPatch
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
			writeProjectError(w, err)
		}
	})
}

func readProjectInput(w http.ResponseWriter, r *http.Request) (projects.RawInput, error) {
	contentType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" || len(params) > 1 || len(params) == 1 && !strings.EqualFold(params["charset"], "utf-8") {
		return projects.RawInput{}, &projects.Failure{Status: 415, Code: "UNSUPPORTED_MEDIA_TYPE", FieldErrors: []projects.FieldError{}}
	}
	const limit = 256 * 1024
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return projects.RawInput{}, &projects.Failure{Status: 413, Code: "REQUEST_TOO_LARGE", FieldErrors: []projects.FieldError{}}
	}
	if err != nil {
		return projects.RawInput{}, &projects.Failure{Status: 400, Code: "INVALID_JSON", FieldErrors: []projects.FieldError{}}
	}
	return projects.ParseRawInput(data)
}

func projectIdempotencyKey(r *http.Request) (string, error) {
	if len(r.Header.Values("Idempotency-Key")) != 1 {
		return "", &projects.Failure{Status: 400, Code: "INVALID_IDEMPOTENCY_KEY", FieldErrors: []projects.FieldError{}}
	}
	return r.Header.Get("Idempotency-Key"), nil
}

func writeProjectError(w http.ResponseWriter, err error) {
	status, code := 503, "RESULT_UNCONFIRMED"
	fields := []projects.FieldError{}
	var details *models.TestOutstandingDetails
	var projectFailure *projects.Failure
	var accessFailure *access.Failure
	if errors.As(err, &projectFailure) {
		status, code = projectFailure.Status, projectFailure.Code
		if projectFailure.FieldErrors != nil {
			fields = projectFailure.FieldErrors
		}
		if d, ok := projectFailure.Details.(*models.TestOutstandingDetails); ok {
			details = d
		}
	} else if errors.As(err, &accessFailure) {
		status, code = accessFailure.Status, accessFailure.Code
		if accessFailure.RetryAfter > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(accessFailure.RetryAfter))
		}
	}
	// issues.Failure 原来只有 writeIssueError 认识,而那个函数没有任何调用方——
	// issue 路由的错误全落进默认 503(422 校验错也变成 RESULT_UNCONFIRMED)。
	// 在共享出口识别一次,所有 issue 端点受益。
	var issueFailure *issues.Failure
	if errors.As(err, &issueFailure) {
		status, code = issueFailure.Status, issueFailure.Code
		if issueFailure.Fields != nil {
			fields = issueFailure.Fields
		}
	}
	var request [16]byte
	_, _ = rand.Read(request[:])
	requestID := hex.EncodeToString(request[:])
	// 底层错误只在这里落日志:前端拿 requestId,排障回服务端日志对行。
	log.Printf("project request failed: requestID=%s status=%d code=%s err=%v", requestID, status, code, err)
	body := projectErrorBody{Error: projectError{Code: code, Message: "The request could not be completed.", FieldErrors: fields, RequestID: requestID, Details: details}}
	writeJSON(w, status, body)
}

func projectBrowserRoute(path string) bool {
	if path == "/projects" {
		return true
	}
	if !strings.HasPrefix(path, "/") || strings.HasSuffix(path, "/") || strings.Contains(path, "%") {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	validResource := func(value string) bool {
		if value == "" || value == "." || value == ".." || !utf8.ValidString(value) || utf8.RuneCountInString(value) > 128 || strings.ContainsAny(value, "/%\\?#\x00") {
			return false
		}
		for _, char := range value {
			if char < 32 || char == 127 {
				return false
			}
		}
		return true
	}
	validUUID := func(value string) bool {
		value = strings.ToLower(value)
		if len(value) != 36 {
			return false
		}
		for index, char := range value {
			if index == 8 || index == 13 || index == 18 || index == 23 {
				if char != '-' {
					return false
				}
				continue
			}
			if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
				return false
			}
		}
		return true
	}
	if len(parts) == 2 && parts[0] == "project-creations" {
		return validUUID(parts[1])
	}
	if len(parts) == 2 && parts[0] == "projects" {
		return parts[1] == "new" || validResource(parts[1])
	}
	if len(parts) == 3 && parts[0] == "projects" && parts[2] == "settings" {
		return parts[1] != "new" && validResource(parts[1])
	}
	if len(parts) == 4 && parts[0] == "projects" && parts[1] != "new" && parts[2] == "updates" {
		return validResource(parts[1]) && validUUID(parts[3])
	}
	if len(parts) == 4 && parts[0] == "projects" && parts[1] != "new" && parts[2] == "issue-creations" {
		return validResource(parts[1]) && validResource(parts[3])
	}
	if len(parts) == 4 && parts[0] == "projects" && parts[1] != "new" && parts[2] == "issues" {
		return validResource(parts[1]) && validResource(parts[3])
	}
	return len(parts) == 4 && parts[0] == "projects" && parts[1] != "new" && parts[2] == "model-applications" && validResource(parts[1]) && validUUID(parts[3])
}
