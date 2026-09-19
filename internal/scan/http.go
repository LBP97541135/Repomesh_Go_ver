package scan

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"repomesh.local/repomesh/internal/reposcan"
)

// HTTP exposes the scan block over HTTP (API contract: docs/前端接口文档
// -总册-2026-09-15.md §3.D). It is platform-neutral: mount RegisterRoutes
// on any mux; session authentication is injected via Authenticate.
type HTTP struct {
	Service *Service
	Store   CatalogStore
	Runner  *Runner
	Jobs    *JobRegistry
	// Suggester is the 辅助推荐 engine; nil or assist-off answers 503.
	Suggester ScopeSuggester
	// Fetcher (usually a platform Router) and channel table for jobs that
	// construct fresh Runners per scan.
	Fetcher      reposcan.Fetcher
	MaxWorkers   int
	IncludeForks bool
	// Guard inputs: the scannable-host allowlist and declared platforms.
	Allowlist     []string
	PlatformExtra map[string]string

	// Authenticate guards write endpoints; injected by assembly (session +
	// Origin/CSRF). Nil means writes pass unauthenticated — tests only.
	Authenticate func(r *http.Request) error

	// ActorToken resolves the **requesting user's** GitHub token, so a scan runs
	// as that user (5000 requests/hour, per-account isolation) instead of with
	// the deployment-level credential (or anonymous, at 60/hour).
	// Nil means "no per-user credential available" — the shared Fetcher is used.
	ActorToken func(r *http.Request) (string, error)

	// ActorOrganization resolves the requesting user's space, so the catalog read
	// and every registration are scoped to it.
	//
	// 2026-09-19 账号隔离：repomesh_scan.repositories.organization_id 一直存在却
	// 从没写过 —— 扫描目录对所有人可见。读面按空间裁剪、写入盖章才闭合。
	// Nil 或空串 = 没有空间：读面返回空集（宁可少给，也不越权多给）。
	ActorOrganization func(r *http.Request) string

	// OnScopeDecided is the 历史决策 producer seam (方案清单 §3 写路径①):
	// fired once per accepted submission with the raw request, so the
	// composition root can resolve the actor from the session. Nil keeps
	// the v1 behavior (nothing persisted).
	OnScopeDecided func(r *http.Request, decision ScopeDecision)

	assist atomic.Bool
	mu     sync.Mutex
	// scopeReceipts is the in-process idempotency cache for scope
	// submissions (design §1.2: replay returns the same result; the cache
	// lives as long as the process — restarts invalidate keys).
	scopeReceipts map[string]scopeReceipt
}

type scopeReceipt struct {
	Confirmed     bool     `json:"confirmed"`
	RepositoryIDs []string `json:"repositoryIds"`
	DecidedAt     string   `json:"decidedAt"`
}

// ScopeDecision is one accepted scope submission on its way to the 历史决策
// store. The adapter stays identity-agnostic: it hands over the raw request
// and the composition root resolves the actor from the session.
type ScopeDecision struct {
	Requirement    string   // raw text; the decision chain normalizes it
	IdempotencyKey string   // stored as event_id, deduped on the far side
	RepositoryIDs  []string // as submitted; names resolved by the chain
	Accepted       bool
}

// NewHTTP assembles the HTTP adapter.
func NewHTTP(service *Service, runner *Runner, jobs *JobRegistry, suggester ScopeSuggester) *HTTP {
	return &HTTP{
		Service:       service,
		Runner:        runner,
		Jobs:          jobs,
		Suggester:     suggester,
		assist:        atomic.Bool{},
		scopeReceipts: map[string]scopeReceipt{},
	}
}

// SetAssistEnabled flips the 辅助选仓 switch at runtime.
func (h *HTTP) SetAssistEnabled(enabled bool) { h.assist.Store(enabled) }

// AssistEnabled reports the switch state.
func (h *HTTP) AssistEnabled() bool { return h.assist.Load() }

// RegisterRoutes mounts the scan block's endpoints. It also seeds the assist
// switch from the service config: the env-provided boot default lives on
// Service, the runtime override on the atomic. Registration happens before
// any request can arrive, and every construction style (the composite
// literal in main.go bypasses NewHTTP) routes through here, so this is the
// one place the default cannot be skipped.
func (h *HTTP) RegisterRoutes(mux *http.ServeMux) {
	if h.Service != nil {
		h.assist.Store(h.Service.cfg.ScopeAssistEnabled)
	}
	mux.HandleFunc("GET /api/scan/repositories", h.handleRepositoryList)
	mux.HandleFunc("GET /api/repositories/url-type", h.handleURLType)
	mux.HandleFunc("GET /api/repositories/dependents", h.handleRepositoryDependents)
	mux.HandleFunc("POST /api/repositories", h.guarded(h.handleRepositoryCreate))
	mux.HandleFunc("POST /api/scan-jobs", h.guarded(h.handleScanJobCreate))
	mux.HandleFunc("GET /api/scan-jobs/{id}", h.handleScanJobGet)
	mux.HandleFunc("POST /api/scope/suggestions", h.guarded(h.handleScopeSuggestions))
	mux.HandleFunc("POST /api/scope/check", h.guarded(h.handleScopeCheck))
	mux.HandleFunc("POST /api/scope", h.guarded(h.handleScopeSubmit))
	mux.HandleFunc("GET /api/settings/scope-assist", h.handleAssistGet)
	mux.HandleFunc("PUT /api/settings/scope-assist", h.guarded(h.handleAssistPut))
}

// guarded wraps write endpoints with the injected authentication check.
func (h *HTTP) guarded(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.Authenticate != nil {
			if err := h.Authenticate(r); err != nil {
				writeError(w, http.StatusUnauthorized, err.Error())
				return
			}
		}
		handler(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, detail string) {
	writeJSON(w, status, map[string]any{"detail": detail})
}

// handleRepositoryList implements D-1: the catalog, the manual-selection
// checkbox source.
func (h *HTTP) handleRepositoryList(w http.ResponseWriter, r *http.Request) {
	// 按调用者的空间裁剪；调用者没有空间时返回空集，而不是全库。
	cards, err := h.catalogForRequest(r.Context(), r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cards)
}

// organizationScopedLister 是可选能力：按空间列出卡片（PostgresCatalog 实现；
// 测试用的内存目录不实现，那时退回全局 List）。
type organizationScopedLister interface {
	ListInOrganization(ctx context.Context, organizationID string) ([]RepositoryCard, error)
}

// organizationScopedCatalog 在"按空间读"之外再加"按空间写"。
type organizationScopedCatalog interface {
	organizationScopedLister
	AddInOrganization(ctx context.Context, organizationID string, card RepositoryCard) error
}

// catalogForRequest 取"调用者空间可见的卡片"。
//
// 2026-09-19 账号隔离：扫描目录的读面（列表 / 依赖图 / 手动登记的查重）此前
// 全部走全局 List —— 于是别人的仓库既能看到，也能被当成"已存在"而挡住自己登记。
func (h *HTTP) catalogForRequest(ctx context.Context, r *http.Request) ([]RepositoryCard, error) {
	if lister, ok := h.Store.(organizationScopedLister); ok {
		return lister.ListInOrganization(ctx, h.organization(r))
	}
	return h.Store.List(ctx)
}

// organization 取调用者的空间标识；未注入 seam 时为空串。
func (h *HTTP) organization(r *http.Request) string {
	if h.ActorOrganization != nil {
		return h.ActorOrganization(r)
	}
	return ""
}

// handleURLType implements D-2: the offline URL badge.
func (h *HTTP) handleURLType(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(r.URL.Query().Get("url"))
	platform, _, err := reposcan.DetectPlatform(raw, h.PlatformExtra)
	response := map[string]any{"url": raw, "url_type": "unknown", "platform": platform.String()}
	if err != nil || platform == reposcan.PlatformUnknown || platform == reposcan.PlatformUnsupported {
		writeJSON(w, http.StatusOK, response)
		return
	}
	normalized := strings.TrimSuffix(strings.TrimRight(raw, "/"), ".git")
	segments, ok := reposcan.SplitRepoPath(normalized)
	switch {
	case platform == reposcan.PlatformLocal:
		response["url_type"] = "unknown"
	case ok && len(segments) >= 2:
		response["url_type"] = "single_repo"
		response["repository_name"] = segments[len(segments)-1]
	case ok:
		response["url_type"] = "group"
	}
	writeJSON(w, http.StatusOK, response)
}

// handleScanJobCreate implements D-3: guard the target, then start a
// background scan job.
func (h *HTTP) handleScanJobCreate(w http.ResponseWriter, r *http.Request) {
	// Unknown fields are rejected (extra="forbid" semantics): a browser
	// sending a personal token must get a 422 naming the field, not a
	// silently ignored field and a mysteriously empty result.
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var body struct {
		Kind       string `json:"kind"`
		URL        string `json:"url"`
		MaxWorkers int    `json:"maxWorkers"`
	}
	if err := decoder.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	kind := strings.ToLower(strings.TrimSpace(body.Kind))
	if kind != "organization" && kind != "repository" {
		writeError(w, http.StatusBadRequest, "kind must be organization or repository")
		return
	}
	platform, normalized, err := reposcan.Guard(body.URL, h.Allowlist, h.PlatformExtra)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = platform
	_ = normalized

	maxWorkers := body.MaxWorkers
	if maxWorkers <= 0 {
		maxWorkers = 5
	}
	if maxWorkers > 20 {
		maxWorkers = 20
	}

	// 2026-09-19：扫描以**发起人本人的身份**跑。兜底链：
	// 用户令牌 → 部署级令牌（启动时 Fetcher 上配的）→ 匿名。
	// tokenSource 会写进任务记录，用户看到"N 个失败"时能知道用的是哪种凭据。
	fetcher, tokenSource := h.fetcherForRequest(r)
	// 空间在**进入闭包之前**解析：作业跑在后台 goroutine 里，那时请求早已结束。
	organizationID := h.organization(r)
	jobID := h.Jobs.StartWithTokenSource(kind, body.URL, tokenSource, func(ctx context.Context, progress func(done, total int, name string)) (RegistrationCounts, error) {
		runner := &Runner{
			Fetcher:        fetcher,
			Channels:       h.Service.Channels(),
			Store:          h.Store,
			MaxWorkers:     maxWorkers,
			IncludeForks:   h.IncludeForks,
			OnProgress:     progress,
			OrganizationID: organizationID,
		}
		if kind == "organization" {
			return runner.ScanOrganization(ctx, body.URL)
		}
		return runner.ScanSingle(ctx, body.URL)
	})
	job, _ := h.Jobs.Get(jobID)
	writeJSON(w, http.StatusAccepted, job)
}

// StartRepositoryScan 供**平台内部**触发一次单仓扫描 —— 升级梯的 onboarding
// （动态引入新仓库）用它。与 POST /api/scan-jobs 走同一套作业登记与 runner 构造，
// 区别只有两点，都不粉饰：
//   · 不经过 HTTP 会话，因此**没有发起人令牌** —— 用部署级凭据（拿不到就匿名），
//     与 fetcherForRequest 的降级口径一致，不假装有更强的凭据；
//   · 调用方给的是仓库名（owner/name），URL 在这里拼。
func (h *HTTP) StartRepositoryScan(ctx context.Context, repository, organizationID string) (string, error) {
	if h.Jobs == nil {
		return "", errors.New("scan: job registry is not configured")
	}
	name := strings.TrimSpace(repository)
	if name == "" {
		return "", errors.New("scan: repository name is required")
	}
	url := name
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = "https://github.com/" + name
	}
	jobID := h.Jobs.Start("repository", url,
		func(ctx context.Context, progress func(done, total int, name string)) (RegistrationCounts, error) {
			runner := &Runner{
				Fetcher:        h.Fetcher,
				Store:          h.Store,
				MaxWorkers:     h.MaxWorkers,
				IncludeForks:   h.IncludeForks,
				OnProgress:     progress,
				OrganizationID: organizationID,
			}
			return runner.ScanSingle(ctx, url)
		})
	return jobID, nil
}

// tokenBoundFetcher is the optional capability of a fetcher that can be
// re-bound to a credential for one run (today: *reposcan.Router). Keeping it
// as an interface assertion means the seam stays optional — a bare fetcher in
// tests keeps working and simply never claims a user credential.
type tokenBoundFetcher interface {
	WithToken(string) reposcan.Fetcher
	HasToken() bool
}

// fetcherForRequest returns the fetcher one scan request should use, together
// with an honest label of the credential it carries. It never claims a
// stronger credential than it actually has.
func (h *HTTP) fetcherForRequest(r *http.Request) (reposcan.Fetcher, string) {
	bound, bindable := h.Fetcher.(tokenBoundFetcher)
	if h.ActorToken != nil && bindable {
		if token, err := h.ActorToken(r); err == nil && token != "" {
			return bound.WithToken(token), "user"
		}
	}
	if bindable && bound.HasToken() {
		return h.Fetcher, "deployment"
	}
	return h.Fetcher, "anonymous"
}

// handleScanJobGet implements D-4.
func (h *HTTP) handleScanJobGet(w http.ResponseWriter, r *http.Request) {
	job, ok := h.Jobs.Get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "scan job not found (jobs do not survive a restart)")
		return
	}
	writeJSON(w, http.StatusOK, job)
}

// handleRepositoryCreate implements D-5: manual registration without a scan.
func (h *HTTP) handleRepositoryCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name         string   `json:"name"`
		URL          string   `json:"url"`
		Description  string   `json:"description"`
		Topics       []string `json:"topics"`
		TestCommands []string `json:"testCommands"`
		TestPaths    []string `json:"testPaths"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	// The stored URL is normalized the same way the scanner normalizes
	// targets, so "https://x.git" and "https://x" are one repository.
	body.URL = strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(body.URL), "/"), ".git")
	if body.Name == "" || body.URL == "" {
		writeError(w, http.StatusBadRequest, "name and url are required")
		return
	}
	// 查重只看**自己空间**里的卡片：别人的同名仓库不该挡住自己登记。
	cards, err := h.catalogForRequest(r.Context(), r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, card := range cards {
		if card.Name == body.Name || card.URL == body.URL {
			writeError(w, http.StatusConflict, "repository already registered")
			return
		}
	}
	card := RepositoryCard{
		ID:           newScanID(),
		Name:         body.Name,
		URL:          body.URL,
		Description:  body.Description,
		Topics:       body.Topics,
		TestCommands: body.TestCommands,
		TestPaths:    body.TestPaths,
		ScanStatus:   ScanStatusOK,
	}
	// 写入时盖章：不盖章的卡片读面看不见（ListInOrganization 按空间过滤）。
	var addErr error
	if scoped, ok := h.Store.(organizationScopedCatalog); ok {
		addErr = scoped.AddInOrganization(r.Context(), h.organization(r), card)
	} else {
		addErr = h.Store.Add(r.Context(), card)
	}
	if addErr != nil {
		writeError(w, http.StatusInternalServerError, addErr.Error())
		return
	}
	writeJSON(w, http.StatusCreated, card)
}

// handleScopeSuggestions implements D-6: the 辅助推荐 engine. Off or not
// configured answers 503 so the frontend falls back to manual selection.
func (h *HTTP) handleScopeSuggestions(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Requirement string `json:"requirement"`
		Limit       int    `json:"limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(body.Requirement) == "" {
		writeError(w, http.StatusUnprocessableEntity, "requirement is required")
		return
	}
	if !h.AssistEnabled() || h.Suggester == nil {
		writeError(w, http.StatusServiceUnavailable, "scope assist is disabled")
		return
	}
	if body.Limit <= 0 {
		body.Limit = 5
	}
	if body.Limit > 50 {
		body.Limit = 50
	}
	suggestions, err := h.Suggester.Suggest(body.Requirement, body.Limit)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, suggestions)
}

// handleScopeCheck implements D-7: advisory completeness verification.
func (h *HTTP) handleScopeCheck(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RepositoryIDs []string `json:"repositoryIds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if len(body.RepositoryIDs) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "repositoryIds must not be empty")
		return
	}
	cards, err := h.Store.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	registry := BuildAliasRegistry(cards)
	report, err := CheckCompleteness(r.Context(), h.Store, registry, body.RepositoryIDs)
	var unknown *UnknownRepositoriesError
	if errors.As(err, &unknown) {
		writeError(w, http.StatusUnprocessableEntity,
			"范围已变化，请刷新列表后重选（未知仓库: "+strings.Join(unknown.IDs, ", ")+"）")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// handleRepositoryDependents serves the replan protocol's Step 4b input:
// who depends on the change-main repository — every edge into it, with
// mechanisms and whether any confirmed edge backs the dependency.
func (h *HTTP) handleRepositoryDependents(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	// 依赖图只看**自己空间**的卡片：别人的仓库依赖关系不该出现在这里。
	cards, err := h.catalogForRequest(r.Context(), r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	registry := BuildAliasRegistry(cards)
	target, ok := registry.Resolve(name)
	if !ok {
		writeError(w, http.StatusNotFound, "未知仓库: "+name)
		return
	}
	byID := make(map[string]RepositoryCard, len(cards))
	for _, card := range cards {
		byID[card.ID] = card
	}
	type dependent struct {
		Repository string   `json:"repository"`
		Mechanisms []string `json:"mechanisms"`
		Confirmed  bool     `json:"confirmed"`
	}
	merged := map[string]*dependent{}
	for _, edge := range BuildGraph(cards, registry).Dependents(target.ID) {
		d := merged[edge.FromID]
		if d == nil {
			source := byID[edge.FromID]
			d = &dependent{Repository: source.Name}
			merged[edge.FromID] = d
		}
		d.Mechanisms = append(d.Mechanisms, string(edge.Mechanism))
		if edge.Confidence == ConfidenceConfirmed {
			d.Confirmed = true
		}
	}
	dependents := make([]dependent, 0, len(merged))
	for _, d := range merged {
		d.Mechanisms = dedupeStrings(d.Mechanisms)
		sort.Strings(d.Mechanisms)
		dependents = append(dependents, *d)
	}
	sort.Slice(dependents, func(i, j int) bool {
		return dependents[i].Repository < dependents[j].Repository
	})
	writeJSON(w, http.StatusOK, map[string]any{"target": target.Name, "dependents": dependents})
}

// handleScopeSubmit implements D-8: the submission boundary. Valid ids are
// confirmed; the durable home of a confirmed scope is the 历史决策 store,
// reached through OnScopeDecided. A replay of the same idempotency key
// returns the original receipt without re-firing the seam.
func (h *HTTP) handleScopeSubmit(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Requirement    string   `json:"requirement"`
		RepositoryIDs  []string `json:"repositoryIds"`
		IdempotencyKey string   `json:"idempotencyKey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if len(body.RepositoryIDs) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "repositoryIds must not be empty")
		return
	}
	if strings.TrimSpace(body.IdempotencyKey) == "" {
		writeError(w, http.StatusUnprocessableEntity, "idempotencyKey is required")
		return
	}
	h.mu.Lock()
	receipt, replay := h.scopeReceipts[body.IdempotencyKey]
	h.mu.Unlock()
	if replay {
		writeJSON(w, http.StatusOK, receipt)
		return
	}
	err := SubmitScope(r.Context(), h.Store, body.RepositoryIDs, func(confirmed []string) error {
		// The accept callback owns nothing anymore: persistence flows
		// through OnScopeDecided below.
		return nil
	})
	var unknown *UnknownRepositoriesError
	if errors.As(err, &unknown) {
		writeError(w, http.StatusUnprocessableEntity,
			"范围已变化，请刷新列表后重选（未知仓库: "+strings.Join(unknown.IDs, ", ")+"）")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	receipt = scopeReceipt{
		Confirmed:     true,
		RepositoryIDs: body.RepositoryIDs,
		DecidedAt:     nowRFC3339(),
	}
	if h.OnScopeDecided != nil {
		h.fireScopeDecision(r, ScopeDecision{
			Requirement:    body.Requirement,
			IdempotencyKey: body.IdempotencyKey,
			RepositoryIDs:  body.RepositoryIDs,
			Accepted:       true,
		})
	}
	h.mu.Lock()
	// The composite literal in main.go bypasses NewHTTP, so the cache map
	// may be nil on the production path — reads tolerate that, writes don't.
	if h.scopeReceipts == nil {
		h.scopeReceipts = map[string]scopeReceipt{}
	}
	h.scopeReceipts[body.IdempotencyKey] = receipt
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, receipt)
}

// fireScopeDecision invokes the seam with panic isolation (F3 fail-open):
// a consumer bug must not fail the user's scope submission — the panic is
// logged and the submission proceeds (the receipt still caches).
func (h *HTTP) fireScopeDecision(r *http.Request, d ScopeDecision) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("scope decision seam panicked",
				"panic", rec, "idempotencyKey", d.IdempotencyKey,
				"repositoryIds", d.RepositoryIDs)
		}
	}()
	h.OnScopeDecided(r, d)
}

// handleAssistGet / handleAssistPut implement D-9.
func (h *HTTP) handleAssistGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"enabled": h.AssistEnabled()})
}

func (h *HTTP) handleAssistPut(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	h.SetAssistEnabled(body.Enabled)
	writeJSON(w, http.StatusOK, map[string]any{"enabled": h.AssistEnabled()})
}

// mustCards is the catalog accessor the scope flow reads through.
func (h *HTTP) mustCards(r *http.Request) []RepositoryCard {
	cards, _ := h.Store.List(r.Context())
	return cards
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}
