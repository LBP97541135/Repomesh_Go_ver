package decisionchain

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var uuid4Pattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// RegisterRoutes mounts the 5 decision endpoints and the 2 toggle endpoints:
// GET /api/decision-chains, GET /api/decision-chains/{id},
// GET /api/decision-chains/similar, GET /api/decision-chains/semantic-search,
// POST /api/decision-chains/embeddings/refresh,
// GET|PUT /api/settings/decision-chain.
// Every endpoint is guarded by Authenticate; the five decision endpoints
// additionally answer 503 while the feature toggle is off (D12), the two
// settings endpoints stay reachable so the feature can be switched back on.
func (s *Service) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/decision-chains", s.guarded(s.handleList))
	mux.HandleFunc("GET /api/decision-chains/{id}", s.guarded(s.handleGet))
	mux.HandleFunc("GET /api/decision-chains/similar", s.guarded(s.handleSimilar))
	mux.HandleFunc("GET /api/decision-chains/semantic-search", s.guarded(s.handleSemanticSearch))
	mux.HandleFunc("POST /api/decision-chains/embeddings/refresh", s.guarded(s.handleRefresh))
	mux.HandleFunc("GET /api/settings/decision-chain", s.guarded(s.handleGetSetting))
	mux.HandleFunc("PUT /api/settings/decision-chain", s.guarded(s.handlePutSetting))
}

func (s *Service) guarded(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Authenticate != nil {
			if err := s.Authenticate(r); err != nil {
				writeError(w, http.StatusUnauthorized, err.Error())
				return
			}
		}
		handler(w, r)
	}
}

// readScope resolves the isolation scope for user-facing reads. 当前阶段所有
// 账号同等对待（无管理员放权），返回的 Scope 只带 ActorID；ActorID 注入缺失
// 时回退零值 Scope（仅测试）。
func (s *Service) readScope(r *http.Request) Scope {
	if s.ActorID == nil {
		return Scope{}
	}
	return Scope{ActorID: s.ActorID(r)}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, detail string) {
	writeJSON(w, status, map[string]any{"detail": detail})
}

// requireEnabled answers 503 while the toggle is off (D12).
func (s *Service) requireEnabled(w http.ResponseWriter) bool {
	if !s.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "decision chain is disabled")
		return false
	}
	return true
}

func (s *Service) handleList(w http.ResponseWriter, r *http.Request) {
	if !s.requireEnabled(w) {
		return
	}
	query := r.URL.Query()
	filter := Filter{
		RequirementKey: query.Get("requirementKey"),
		Keyword:        query.Get("keyword"),
		Repository:     query.Get("repository"),
		Scope:          s.readScope(r),
	}
	if raw := query.Get("projectId"); raw != "" {
		if !uuid4Pattern.MatchString(raw) {
			writeError(w, http.StatusBadRequest, "projectId must be a uuid")
			return
		}
		filter.ProjectID = raw
	}
	if raw := query.Get("step"); raw != "" {
		step := DecisionStep(raw)
		if !step.Valid() {
			writeError(w, http.StatusBadRequest, "unknown step: "+raw)
			return
		}
		filter.Step = &step
	}
	if raw := query.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 200 {
			writeError(w, http.StatusBadRequest, "limit must be 1..200")
			return
		}
		filter.Limit = parsed
	}
	if raw := query.Get("offset"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			writeError(w, http.StatusBadRequest, "offset must be >= 0")
			return
		}
		filter.Offset = parsed
	}
	nodes, err := s.store.List(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if nodes == nil {
		nodes = []DecisionNode{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"nodes": nodes, "mode": "list"})
}

func (s *Service) handleGet(w http.ResponseWriter, r *http.Request) {
	if !s.requireEnabled(w) {
		return
	}
	node, err := s.store.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "no such decision node")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, node)
}

func (s *Service) handleSimilar(w http.ResponseWriter, r *http.Request) {
	if !s.requireEnabled(w) {
		return
	}
	query := r.URL.Query()
	q := SimilarQuery{
		Requirement: query.Get("requirement"),
		Mode:        query.Get("mode"),
		Scope:       s.readScope(r),
	}
	if raw := query.Get("repositoryIds"); raw != "" {
		for _, id := range strings.Split(raw, ",") {
			if id = strings.TrimSpace(id); id != "" {
				q.RepositoryNames = append(q.RepositoryNames, id)
			}
		}
	}
	if raw := query.Get("topK"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "topK must be an integer")
			return
		}
		q.TopK = parsed
	}
	if raw := query.Get("minSimilarity"); raw != "" {
		parsed, err := strconv.ParseFloat(raw, 64)
		if err != nil || parsed < 0 || parsed > 1 {
			writeError(w, http.StatusBadRequest, "minSimilarity must be within [0,1]")
			return
		}
		q.MinSimilarity = parsed
	}
	result, err := s.Similar(r.Context(), q)
	if errors.Is(err, ErrBadArgument) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Service) handleSemanticSearch(w http.ResponseWriter, r *http.Request) {
	if !s.requireEnabled(w) {
		return
	}
	query := r.URL.Query()
	topK := 0
	minSimilarity := 0.0
	if raw := query.Get("topK"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "topK must be an integer")
			return
		}
		topK = parsed
	}
	if raw := query.Get("minSimilarity"); raw != "" {
		parsed, err := strconv.ParseFloat(raw, 64)
		if err != nil || parsed < 0 || parsed > 1 {
			writeError(w, http.StatusBadRequest, "minSimilarity must be within [0,1]")
			return
		}
		minSimilarity = parsed
	}
	hits, err := s.SemanticSearch(r.Context(), query.Get("queryText"), topK, minSimilarity, s.readScope(r))
	switch {
	case errors.Is(err, ErrBadArgument):
		writeError(w, http.StatusBadRequest, "queryText is required")
		return
	case errors.Is(err, ErrNotConfigured):
		writeError(w, http.StatusServiceUnavailable, "semantic search requires an embedding endpoint")
		return
	case errors.Is(err, ErrUpstream):
		writeError(w, http.StatusBadGateway, err.Error())
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if hits == nil {
		hits = []SimilarHit{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"mode": "semantic", "hits": hits})
}

func (s *Service) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if !s.requireEnabled(w) {
		return
	}
	result, err := s.Refresh(r.Context(), defaultEmbeddingBatch)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Service) handleGetSetting(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"feature": "decision_chain",
		"enabled": s.Enabled(),
	})
}

func (s *Service) handlePutSetting(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<15))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "body must be {\"enabled\": true|false}: "+err.Error())
		return
	}
	if body.Enabled == nil {
		writeError(w, http.StatusBadRequest, "enabled is required")
		return
	}
	actor := ""
	if s.ActorName != nil {
		actor = s.ActorName(r)
	}
	if err := s.store.SetFeature(r.Context(), "decision_chain", *body.Enabled, actor, time.Now()); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"feature": "decision_chain",
		"enabled": *body.Enabled,
	})
}
