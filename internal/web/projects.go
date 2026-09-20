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
	// OnRepositoriesConfirmed 在项目更新**新增了仓库**之后被调用（best-effort，异步）。
	//
	// 2026-09-20 用户裁定："不是扫描就建队，是**确认接入**的时候才建队。"
	// 判据就是 added 的数量：
	//   · 恰好 1 个 = 人在仓库页**逐仓确认**接入（「接入本项目」）→ 该建队；
	//   · 一次几十个 = 批量「全部接入本项目」/ 扫描后自动挂载 → **不是确认，不建队**。
	// web 层只把事实交出去（新增了哪些仓库），要不要建队由装配方决定 —— web 不认识
	// AgentTeams。失败不回滚项目：建队是后续动作，不是项目写入的一部分。
	OnRepositoriesConfirmed func(ctx context.Context, projectID string, added []string)
}

// notifyRepositoriesConfirmed 异步通知装配方。用 context.WithoutCancel 保证请求
// 返回后这次建队仍能跑完（它不该被 HTTP 的 15 秒超时打断）。
func (p Projects) notifyRepositoriesConfirmed(ctx context.Context, projectID string, added []string) {
	if p.OnRepositoriesConfirmed == nil || projectID == "" || len(added) == 0 {
		return
	}
	hook := p.OnRepositoriesConfirmed
	ids := append([]string(nil), added...)
	go hook(context.WithoutCancel(ctx), projectID, ids)
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
		// 新建项目**不建队**：用户裁定的是"确认接入才建队"，而项目创建时勾选的仓库
		// 是"这个项目有哪些仓"，不是逐仓确认（线上正是"新建/批量接入一次 43 个仓"
		// 把控制面灌爆的）。建队只发生在仓库页**逐仓「接入本项目」**那一步。
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
			// 仓库页单仓「接入本项目」走的就是这条：新增恰好 1 个仓 = 人确认接入。
			// 批量「全部接入」会一次新增几十个 —— 那不是逐仓确认，装配方会自己跳过。
			projectAPI.notifyRepositoriesConfirmed(r.Context(), result.ProjectID, result.AddedRepositoryIDs)
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
	registerProjectRoute(mux, "POST /api/projects/{projectId}/archive", auth, func(w http.ResponseWriter, r *http.Request, principal access.ProjectPrincipal) error {
		if projectAPI.Service == nil {
			return &projects.Failure{Status: 503, Code: "RESULT_UNCONFIRMED", FieldErrors: []projects.FieldError{}}
		}
		result, err := projectAPI.Service.Archive(r.Context(), principal, r.PathValue("projectId"))
		if err == nil {
			writeJSON(w, http.StatusOK, result)
		}
		return err
	})
	registerProjectRoute(mux, "POST /api/projects/{projectId}/restore", auth, func(w http.ResponseWriter, r *http.Request, principal access.ProjectPrincipal) error {
		if projectAPI.Service == nil {
			return &projects.Failure{Status: 503, Code: "RESULT_UNCONFIRMED", FieldErrors: []projects.FieldError{}}
		}
		result, err := projectAPI.Service.Restore(r.Context(), principal, r.PathValue("projectId"))
		if err == nil {
			writeJSON(w, http.StatusOK, result)
		}
		return err
	})
	// 字面量段「archived」比 {projectId} 更具体，Go 的 ServeMux 会优先匹配它，
	// 所以这条路不会被项目详情路由吃掉。
	registerProjectRoute(mux, "GET /api/projects/archived", auth, func(w http.ResponseWriter, r *http.Request, principal access.ProjectPrincipal) error {
		if projectAPI.Service == nil {
			return &projects.Failure{Status: 503, Code: "RESULT_UNCONFIRMED", FieldErrors: []projects.FieldError{}}
		}
		result, err := projectAPI.Service.Archived(r.Context(), principal)
		if err == nil {
			writeJSON(w, http.StatusOK, result)
		}
		return err
	})
}

func registerProjectRoute(mux *http.ServeMux, pattern string, auth Auth, handler projectHandler) {
	registerProjectRouteWithTimeout(mux, pattern, auth, projectRouteTimeout, handler)
}

// projectRouteTimeout 是**普通**项目路由的请求预算。
const projectRouteTimeout = 15 * time.Second

// projectFanOutTimeout 是「一个请求要打 N 次 GitHub」那类读路由的预算。
//
// 2026-09-20 线上实测：`GET /projects/{id}/issue-creation-options` 返回
// `503 RESULT_UNCONFIRMED`，日志里对得上的一条是
// `err=issues: RESULT_UNCONFIRMED: context deadline exceeded`（17:15:47）。
// 原因是这条路由按 15s 封顶，而它要为**每个仓库**现探一次 App 覆盖
// （`ObserveProjectRepositories` 的 appOnly 分支，并发上限 8）：
// 目录 48 仓就是 6 波，任何一波慢一点就整体超时，界面显示"服务端暂时不可用"，
// 而事实是它只是还没探完。仓库越多越必现，属于**结构性的预算不够**，
// 不是偶发抖动 —— 所以按路由给预算，别用一把尺子量所有端点。
const projectFanOutTimeout = 60 * time.Second

func registerProjectRouteWithTimeout(mux *http.ServeMux, pattern string, auth Auth, timeout time.Duration, handler projectHandler) {
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
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
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
	//
	// 2026-09-20：**预期内的"还没有"不要记成失败**。拓扑读面按契约就是
	// 「404 = 还没有拓扑，不是错误，正是'来得及设卡点'的窗口」，策略草稿同理；
	// 而这里把每一个非 2xx 都打成 "project request failed"，于是工作台每轮轮询
	// 都在日志里刷"请求失败"，把真故障淹掉（与 MANIFEST_NOT_FOUND 同一族问题）。
	// 这几类只在 debug 留一行，其余照旧。
	if !expectedEmptyState(status, code) {
		log.Printf("project request failed: requestID=%s status=%d code=%s err=%v", requestID, status, code, err)
	}
	body := projectErrorBody{Error: projectError{Code: code, Message: "The request could not be completed.", FieldErrors: fields, RequestID: requestID, Details: details}}
	writeJSON(w, status, body)
}

// expectedEmptyState 判断"这个错误码其实是**正常空态**，不是故障"。
//
// 只收**契约里明确写成空态**的那几个；其余 404（例如项目/issue 真的不存在）
// 仍然是失败，照旧记日志 —— 别把真故障一起静音。
func expectedEmptyState(status int, code string) bool {
	if status != http.StatusNotFound {
		return false
	}
	switch code {
	case "NOT_FOUND",              // GET /api/projects/{id}/topology —— 还没有拓扑
		"POLICY_DRAFT_NOT_FOUND",  // 还没有监管策略草稿
		"PROJECT_CREATION_NOT_FOUND", // 创建还在提交中（读面按契约回 404）
		"MANIFEST_NOT_FOUND":      // 交付清单还没物化
		return true
	default:
		return false
	}
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
