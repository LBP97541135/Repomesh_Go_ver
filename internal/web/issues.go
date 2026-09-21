package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/agentteams"
	"repomesh.local/repomesh/internal/docparse"
	"repomesh.local/repomesh/internal/issues"
	"repomesh.local/repomesh/internal/projects"
	"repomesh.local/repomesh/internal/roomnotice"
)

// Issues carries the issue creation service into the web layer; the zero value
// skips every route so unconfigured modes keep working.
type Issues struct {
	Service *issues.Service
	// Matrix 读 AgentTeams 房间消息用。为 nil 时房间消息端点返 503（如实说"没配"），
	// 不影响房间关联本身（那部分只读本库）。
	Matrix *agentteams.MatrixSession
	// Rooms 把"建项成功"这类事件投进团队房。nil 时静默不投，建项行为不变。
	Rooms *roomnotice.Notifier
	// OnScopeConfirmed 是选仓门确认成功后的回调(组合根接"唤醒入选仓库的团队"):
	// spec 2026-09-20 §3.3 —— 团队建的时候 worker 是 Sleeping 的,不唤醒就没有可接活的
	// runtime。走 hook 而不是让 issues 包直接依赖 repositoryteams(域不依赖适配器)。nil 时
	// 静默跳过,确认行为不变。
	OnScopeConfirmed func(ctx context.Context, projectID string, repositoryIDs []string)
}

func registerIssues(mux *http.ServeMux, auth Auth, issueAPI Issues) {
	if issueAPI.Service == nil {
		return
	}
	registerProjectRoute(mux, "POST /api/projects/{projectId}/issue-creations", auth, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
		return createIssue(w, r, issueAPI.Service, claims, issueAPI.Rooms)
	})
	registerProjectRoute(mux, "GET /api/projects/{projectId}/issue-creations/{creationId}", auth, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
		return getIssueCreation(w, r, issueAPI.Service, claims)
	})
	// 这条读路由要为**每个仓库**现探一次 App 覆盖（见 ObserveProjectRepositories），
	// 是唯一按仓库数线性扇出的读端点，所以给它自己的预算（projectFanOutTimeout）。
	registerProjectRouteWithTimeout(mux, "GET /api/projects/{projectId}/issue-creation-options", auth, projectFanOutTimeout, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
		return issueOptions(w, r, issueAPI.Service, claims)
	})
	registerProjectRoute(mux, "GET /api/projects/{projectId}/issue-conversations", auth, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
		return issueConversations(w, r, issueAPI.Service, claims)
	})
	registerIssueRoutes(mux, auth, issueAPI)
	registerProjectRoute(mux, "POST /api/issues/parse-document", auth, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
		return parseIssueDocument(w, r)
	})
}

// parseIssueDocument extracts requirement text from an uploaded document
// (contract v0.3 §1 companion: the intake write takes plain requirement_text;
// this endpoint is the client's document→text converter). The document is
// parsed in memory and never stored. Refusals: 413 over the size cap, 415
// unsupported format, 422 when the file yields no text (e.g. a scanned PDF).
func parseIssueDocument(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, docparse.MaxBytes+(1<<20))
	reader, err := r.MultipartReader()
	if err != nil {
		return writeParseError(w, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE",
			"expected multipart/form-data with a `file` field")
	}
	var file *multipart.Part
	var filename string
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return writeParseError(w, http.StatusBadRequest, "INVALID_MULTIPART", "malformed multipart body")
		}
		if part.FormName() == "file" {
			file, filename = part, part.FileName()
			break
		}
		_, _ = io.Copy(io.Discard, part)
		part.Close()
	}
	if file == nil {
		return writeParseError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED",
			"multipart field `file` is required")
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, docparse.MaxBytes+1))
	if err != nil {
		return writeParseError(w, http.StatusBadRequest, "INVALID_MULTIPART", "could not read uploaded file")
	}
	parsed, err := docparse.Extract(filename, content)
	switch {
	case errors.Is(err, docparse.ErrUnsupported):
		return writeParseError(w, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", err.Error())
	case errors.Is(err, docparse.ErrTooLarge):
		return writeParseError(w, http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", err.Error())
	case errors.Is(err, docparse.ErrNoText):
		return writeParseError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", err.Error())
	case err != nil:
		return writeParseError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", err.Error())
	}
	writeJSON(w, http.StatusOK, parsed)
	return nil
}

// writeParseError emits the shared project error envelope, but with the real
// refusal reason in `message` — the composer surfaces it to the user verbatim.
func writeParseError(w http.ResponseWriter, status int, code, message string) error {
	var request [16]byte
	_, _ = rand.Read(request[:])
	writeJSON(w, status, projectErrorBody{Error: projectError{
		Code: code, Message: message, FieldErrors: []projects.FieldError{}, RequestID: hex.EncodeToString(request[:]),
	}})
	return nil
}

func createIssue(w http.ResponseWriter, r *http.Request, service *issues.Service, claims access.ProjectPrincipal, rooms *roomnotice.Notifier) error {
	projectID := r.PathValue("projectId")
	key, err := projectIdempotencyKey(r)
	if err != nil {
		return err
	}
	input, err := readIssueInput(w, r)
	if err != nil {
		return err
	}
	log.Printf("issues: create body project=%s bytes=%d head=%q", projectID, len(input), string(input[:min(220, len(input))]))
	command, err := issues.ParsePageCommand(projectID, key, newIssueRequestID(), input)
	if err != nil {
		return err
	}
	result, err := service.CreatePage(r.Context(), claims, command)
	if err != nil {
		return err
	}
	w.Header().Set("Location", result.Receipt.IssueLocation())
	status := http.StatusCreated
	if result.Replayed {
		status = http.StatusOK
	}
	writeIssueCreation(w, status, result)
	// 需求进房（B 第二片）：建项成功后把"人发来了需求"如实投进该 issue 的仓库
	// 团队房。重放（同幂等键再来一次）不重投；仓库还没建队就静默跳过 ——
	// 房间号都没有，谈不上"投进房"。Notify 自己带 goroutine，不占这个请求的时间。
	if rooms != nil && !result.Replayed {
		rooms.Notify(r.Context(), result.Receipt.IssueID(),
			"issue-created:"+result.Receipt.IssueID(), requirementOpeningNotice(input))
	}
	return nil
}

// requirementOpeningNotice 是进房的那句"我这边发来了需求"。
// 正文带需求首行（截断），发送者身份是 RepoMesh 的服务账号 —— 不冒充用户，
// 也不冒充 Manager，正文自报家门说清是谁在转述。
func requirementOpeningNotice(createBody []byte) string {
	var input struct {
		Description string `json:"description"`
	}
	_ = json.Unmarshal(createBody, &input)
	firstLine := strings.TrimSpace(strings.SplitN(input.Description, "\n", 2)[0])
	if firstLine == "" {
		return "【RepoMesh】收到新需求（正文为空，见 issue 页）。"
	}
	runes := []rune(firstLine)
	if len(runes) > 80 {
		firstLine = string(runes[:80]) + "…"
	}
	return "【RepoMesh】收到新需求：" + firstLine
}

func newIssueRequestID() string {
	return time.Now().UTC().Format("20060102T150405.000000000Z")
}

func readIssueInput(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	contentType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" || len(params) > 1 || len(params) == 1 && !strings.EqualFold(params["charset"], "utf-8") {
		return nil, &issues.Failure{Status: 415, Code: "UNSUPPORTED_MEDIA_TYPE"}
	}
	const limit = 256 * 1024
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return nil, &issues.Failure{Status: 413, Code: "REQUEST_TOO_LARGE"}
	}
	if err != nil {
		return nil, &issues.Failure{Status: 400, Code: "INVALID_JSON"}
	}
	return data, nil
}

func getIssueCreation(w http.ResponseWriter, r *http.Request, service *issues.Service, claims access.ProjectPrincipal) error {
	projectID := r.PathValue("projectId")
	creationID := r.PathValue("creationId")
	receipt, err := service.GetPageCreation(r.Context(), claims, projectID, issues.OperationID(creationID))
	if err != nil {
		return err
	}
	writeIssueCreation(w, http.StatusOK, issues.CreationResult{Receipt: receipt})
	return nil
}

func issueOptions(w http.ResponseWriter, r *http.Request, service *issues.Service, claims access.ProjectPrincipal) error {
	projectID := r.PathValue("projectId")
	query := issues.ParsePageQuery(r.URL.Query().Get("cursor"), atoiDefault(r.URL.Query().Get("limit"), 50))
	options, err := service.Options(r.Context(), claims, projectID, query)
	if err != nil {
		return err
	}
	log.Printf("issues: options response project=%s revision=%q repos=%d canSubmit=%v", projectID, options.CreationContextRevision, len(options.Repositories), options.CanSubmit)
	writeJSON(w, http.StatusOK, options)
	return nil
}

func issueConversations(w http.ResponseWriter, r *http.Request, service *issues.Service, claims access.ProjectPrincipal) error {
	projectID := r.PathValue("projectId")
	query := issues.ParsePageQuery(r.URL.Query().Get("cursor"), atoiDefault(r.URL.Query().Get("limit"), 50))
	page, err := service.Conversations(r.Context(), claims, projectID, query)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, page)
	return nil
}

func atoiDefault(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > 100 {
		return fallback
	}
	return value
}

// writeIssueCreation emits the envelope the contract's §4 defines: the storage
// receipt plus the browser-facing link block.
func writeIssueCreation(w http.ResponseWriter, status int, result issues.CreationResult) {
	receipt := result.Receipt
	writeJSON(w, status, map[string]any{
		"creationId":    receipt.OperationID,
		"status":        "committed",
		"projectId":     receipt.ProjectID,
		"createdAt":     receipt.CreatedAt(),
		"source":        map[string]string{"kind": "issue_page"},
		"issue":         map[string]any{"id": receipt.IssueID(), "number": receipt.IssueNumber()},
		"mainChangeSet": map[string]string{"id": receipt.ChangeSetID()},
		"conversation":  map[string]string{"id": receipt.ConversationID()},
		"links": map[string]string{
			"issue":     receipt.IssueLocation(),
			"operation": "/api/projects/" + receipt.ProjectID + "/issue-creations/" + receipt.OperationID,
		},
	})
}

// writeIssueError maps service failures onto the shared error envelope. The
// issues Failure type projects through the projects.Failure shape so field
// errors and request IDs stay identical to the other project routes.
func writeIssueError(w http.ResponseWriter, err error) {
	var issueFailure *issues.Failure
	if errors.As(err, &issueFailure) {
		writeProjectError(w, &projects.Failure{Status: issueFailure.Status, Code: issueFailure.Code, FieldErrors: issueFailure.Fields})
		return
	}
	writeProjectError(w, err)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
