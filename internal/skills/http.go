package skill

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

// RegisterRoutes mounts the 20 skill-governance endpoints under /api/skills
// plus the feature toggle at /api/settings/skill-governance. Every endpoint is
// guarded by Authenticate; the skill endpoints additionally answer 503 while
// the skill_governance toggle is off, the two settings endpoints stay reachable
// so the feature can be switched back on.
func (s *Service) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/skills/versions", s.guarded(s.handleListVersions))
	// 2026-09-19：补上"列出全部技能目录"。此前只有 /skills/versions 且**必须带
	// ?skill=<name> 或 ?skill_id=<id>**，于是控制台无法枚举那 15 个种子技能——
	// 技能管理页第一屏因此没有数据源。Store.ListSkills 早已存在，这里只是把它暴露出来。
	mux.HandleFunc("GET /api/skills", s.guarded(s.handleListSkills))
	mux.HandleFunc("POST /api/skills", s.guarded(s.handleRegisterSkill))
	mux.HandleFunc("POST /api/skills/versions", s.guarded(s.handleRegisterVersion))
	mux.HandleFunc("POST /api/skills/versions/{id}/evaluate", s.guarded(s.handleEvaluate))
	mux.HandleFunc("POST /api/skills/versions/{id}/evaluations", s.guarded(s.handleRecordRun))
	mux.HandleFunc("POST /api/skills/versions/{id}/canary", s.guarded(s.handleCanary))
	mux.HandleFunc("POST /api/skills/versions/{id}/promote", s.guarded(s.handlePromote))
	mux.HandleFunc("POST /api/skills/versions/{id}/rollback", s.guarded(s.handleRollback))
	mux.HandleFunc("POST /api/skills/versions/{id}/release", s.guarded(s.handleRelease))
	mux.HandleFunc("POST /api/skills/versions/{id}/approvals", s.guarded(s.handleCreateApproval))
	mux.HandleFunc("POST /api/skills/versions/{id}/questions", s.guarded(s.handleAddQuestion))
	mux.HandleFunc("POST /api/skills/approvals/{id}/decide", s.guarded(s.handleDecideApproval))
	mux.HandleFunc("POST /api/skills/arms", s.guarded(s.handleArms))
	mux.HandleFunc("GET /api/skills/bindings", s.guarded(s.handleListBindings))
	mux.HandleFunc("POST /api/skills/bindings", s.guarded(s.handleBind))
	mux.HandleFunc("DELETE /api/skills/bindings/{id}", s.guarded(s.handleUnbind))
	mux.HandleFunc("POST /api/skills/suggestions", s.guarded(s.handleAddSuggestion))
	mux.HandleFunc("POST /api/skills/suggestions/{id}/decide", s.guarded(s.handleDecideSuggestion))
	mux.HandleFunc("GET /api/skills/mcp-policies", s.guarded(s.handleMcpPolicies))
	mux.HandleFunc("POST /api/skills/assemble", s.guarded(s.handleAssemble))
	mux.HandleFunc("GET /api/settings/skill-governance", s.guarded(s.handleGetSetting))
	mux.HandleFunc("PUT /api/settings/skill-governance", s.guarded(s.handlePutSetting))
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

func (s *Service) actor(r *http.Request) string {
	if s.ActorName != nil {
		return s.ActorName(r)
	}
	return ""
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, detail string) {
	writeJSON(w, status, map[string]any{"detail": detail})
}

func writeCoded(w http.ResponseWriter, status int, code, detail string) {
	writeJSON(w, status, map[string]any{"code": code, "detail": detail})
}

// fail maps store/service errors onto the API contract:
// LifecycleRefused→409, ErrNoRows→404, anything else→500.
func (s *Service) fail(w http.ResponseWriter, err error) {
	var refused *LifecycleRefused
	if errors.As(err, &refused) {
		writeCoded(w, http.StatusConflict, refused.Code, refused.Message)
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "no such skill resource")
		return
	}
	writeCoded(w, http.StatusInternalServerError, "internal", err.Error())
}

// requireEnabled answers 503 while the skill_governance toggle is off.
func (s *Service) requireEnabled(w http.ResponseWriter, r *http.Request) bool {
	enabled, err := s.Store.FeatureEnabled(r.Context(), "skill_governance")
	if err != nil {
		writeCoded(w, http.StatusInternalServerError, "internal", err.Error())
		return false
	}
	if !enabled {
		writeError(w, http.StatusServiceUnavailable, "skill governance is disabled")
		return false
	}
	return true
}

func decodeBody(w http.ResponseWriter, r *http.Request, into any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<15))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		writeCoded(w, http.StatusBadRequest, "bad_request", err.Error())
		return false
	}
	return true
}

// handleListSkills 列出技能目录（全部技能，不分版本）。控制台技能管理页的
// 第一屏数据源；版本明细再按 skill_id 调 /api/skills/versions。
func (s *Service) handleListSkills(w http.ResponseWriter, r *http.Request) {
	if !s.requireEnabled(w, r) {
		return
	}
	skills, err := s.Store.ListSkills(r.Context(), s.organization(r))
	if err != nil {
		s.fail(w, err)
		return
	}
	if skills == nil {
		skills = []Skill{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": skills})
}

func (s *Service) handleListVersions(w http.ResponseWriter, r *http.Request) {
	if !s.requireEnabled(w, r) {
		return
	}
	var skillID string
	if name := strings.TrimSpace(r.URL.Query().Get("skill")); name != "" {
		sk, err := s.Store.GetSkillByName(r.Context(), s.organization(r), name)
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "no such skill")
			return
		}
		if err != nil {
			s.fail(w, err)
			return
		}
		skillID = sk.ID
	} else if id := strings.TrimSpace(r.URL.Query().Get("skill_id")); id != "" {
		skillID = id
	} else {
		writeCoded(w, http.StatusBadRequest, "bad_request", "specify ?skill=<name> or ?skill_id=<id>")
		return
	}
	versions, err := s.Store.ListVersions(r.Context(), skillID)
	if err != nil {
		s.fail(w, err)
		return
	}
	if versions == nil {
		versions = []SkillVersion{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": versions})
}

func (s *Service) handleRegisterSkill(w http.ResponseWriter, r *http.Request) {
	if !s.requireEnabled(w, r) {
		return
	}
	var body struct {
		Name            string `json:"name"`
		Scenario        string `json:"scenario"`
		TargetAgentRole string `json:"target_agent_role"`
		CreatedBy       string `json:"created_by"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.Name == "" || body.Scenario == "" {
		writeCoded(w, http.StatusBadRequest, "bad_request", "name and scenario are required")
		return
	}
	switch body.TargetAgentRole {
	case RoleLeader, RoleManager, RoleWorker:
	default:
		writeCoded(w, http.StatusBadRequest, "bad_request",
			"target_agent_role must be leader, manager or worker")
		return
	}
	createdBy := body.CreatedBy
	if createdBy == "" {
		createdBy = s.actor(r)
	}
	sk, err := s.Store.RegisterSkill(r.Context(), s.organization(r), body.Name, body.Scenario, body.TargetAgentRole, createdBy)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sk)
}

func (s *Service) handleRegisterVersion(w http.ResponseWriter, r *http.Request) {
	if !s.requireEnabled(w, r) {
		return
	}
	var body struct {
		SkillID   string `json:"skill_id"`
		Version   string `json:"version"`
		Content   string `json:"content"`
		CreatedBy string `json:"created_by"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	createdBy := body.CreatedBy
	if createdBy == "" {
		createdBy = s.actor(r)
	}
	v, err := s.RegisterVersion(r.Context(), s.organization(r), body.SkillID, body.Version, body.Content, createdBy)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Service) handleEvaluate(w http.ResponseWriter, r *http.Request) {
	if !s.requireEnabled(w, r) {
		return
	}
	v, err := s.StartEvaluation(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Service) handleRecordRun(w http.ResponseWriter, r *http.Request) {
	if !s.requireEnabled(w, r) {
		return
	}
	var body struct {
		QuestionID   string         `json:"question_id"`
		Arm          string         `json:"arm"`
		BlindedLabel string         `json:"blinded_label"`
		Answer       map[string]any `json:"answer"`
		JudgedBy     *string        `json:"judged_by"`
		Result       string         `json:"result"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	run, err := s.RecordRun(r.Context(), r.PathValue("id"), body.QuestionID, body.Arm,
		body.BlindedLabel, body.Answer, body.JudgedBy, body.Result)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Service) handleCanary(w http.ResponseWriter, r *http.Request) {
	if !s.requireEnabled(w, r) {
		return
	}
	v, err := s.EnterCanary(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Service) handlePromote(w http.ResponseWriter, r *http.Request) {
	if !s.requireEnabled(w, r) {
		return
	}
	v, err := s.Promote(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Service) handleRollback(w http.ResponseWriter, r *http.Request) {
	if !s.requireEnabled(w, r) {
		return
	}
	v, err := s.Rollback(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Service) handleRelease(w http.ResponseWriter, r *http.Request) {
	if !s.requireEnabled(w, r) {
		return
	}
	var body struct {
		AgentID string `json:"agent_id"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.AgentID == "" {
		writeCoded(w, http.StatusBadRequest, "bad_request", "agent_id is required")
		return
	}
	source, err := s.ReleaseOrAutoPass(r.Context(), r.PathValue("id"), body.AgentID)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version_id": r.PathValue("id"), "agent_id": body.AgentID, "source": source,
	})
}

func (s *Service) handleCreateApproval(w http.ResponseWriter, r *http.Request) {
	if !s.requireEnabled(w, r) {
		return
	}
	versionID := r.PathValue("id")
	v, err := s.Store.GetVersion(r.Context(), versionID)
	if err != nil {
		s.fail(w, err)
		return
	}
	sk, err := s.Store.getSkillByID(r.Context(), v.SkillID)
	if err != nil {
		s.fail(w, err)
		return
	}
	ap, err := s.Store.CreateApproval(r.Context(), versionID, sk.TargetAgentRole)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ap)
}

func (s *Service) handleDecideApproval(w http.ResponseWriter, r *http.Request) {
	if !s.requireEnabled(w, r) {
		return
	}
	var body struct {
		Recused        bool    `json:"recused"`
		Note           string  `json:"note"`
		Conclusion     string  `json:"conclusion"`
		ReviewerUserID *string `json:"reviewer_user_id"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	ap, err := s.Store.GetApprovalByID(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	ap, err = s.Store.DecideApproval(r.Context(), ap.VersionID, body.Recused,
		body.Note, body.Conclusion, body.ReviewerUserID)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ap)
}

func (s *Service) handleAddQuestion(w http.ResponseWriter, r *http.Request) {
	if !s.requireEnabled(w, r) {
		return
	}
	var body struct {
		Kind       string         `json:"kind"`
		Question   string         `json:"question"`
		Expected   map[string]any `json:"expected"`
		ProvidedBy string         `json:"provided_by"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	v, err := s.Store.GetVersion(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	providedBy := body.ProvidedBy
	if providedBy == "" {
		providedBy = s.actor(r)
	}
	q, err := s.Store.AddQuestion(r.Context(), v.SkillID, QuestionKind(body.Kind),
		body.Question, body.Expected, providedBy)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, q)
}

func (s *Service) handleArms(w http.ResponseWriter, r *http.Request) {
	if !s.requireEnabled(w, r) {
		return
	}
	var body struct {
		QuestionID string `json:"question_id"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	withLabel, withoutLabel, labelA, labelB, err := s.Store.BuildArms(r.Context(), body.QuestionID)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"question_id":   body.QuestionID,
		"with_label":    withLabel,
		"without_label": withoutLabel,
		"label_a":       labelA,
		"label_b":       labelB,
	})
}

func (s *Service) handleListBindings(w http.ResponseWriter, r *http.Request) {
	if !s.requireEnabled(w, r) {
		return
	}
	agentID := strings.TrimSpace(r.URL.Query().Get("agent_id"))
	if agentID == "" {
		writeCoded(w, http.StatusBadRequest, "bad_request", "specify ?agent_id=<id>")
		return
	}
	bindings, err := s.Store.ActiveBindings(r.Context(), agentID)
	if err != nil {
		s.fail(w, err)
		return
	}
	if bindings == nil {
		bindings = []map[string]any{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"bindings": bindings})
}

func (s *Service) handleBind(w http.ResponseWriter, r *http.Request) {
	if !s.requireEnabled(w, r) {
		return
	}
	var body struct {
		AgentID   string `json:"agent_id"`
		VersionID string `json:"version_id"`
		Source    string `json:"source"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.AgentID == "" || body.VersionID == "" || body.Source == "" {
		writeCoded(w, http.StatusBadRequest, "bad_request",
			"agent_id, version_id and source are required")
		return
	}
	if err := s.Store.BindAgent(r.Context(), body.AgentID, body.VersionID, body.Source); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"agent_id": body.AgentID, "version_id": body.VersionID,
		"source": body.Source, "status": "bound",
	})
}

func (s *Service) handleUnbind(w http.ResponseWriter, r *http.Request) {
	if !s.requireEnabled(w, r) {
		return
	}
	id := r.PathValue("id")
	if err := s.Store.UnbindAgent(r.Context(), id); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": "unbound"})
}

func (s *Service) handleAddSuggestion(w http.ResponseWriter, r *http.Request) {
	if !s.requireEnabled(w, r) {
		return
	}
	var body struct {
		TaskID     *string `json:"task_id"`
		SkillID    string  `json:"skill_id"`
		Suggestion string  `json:"suggestion"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.SkillID == "" || body.Suggestion == "" {
		writeCoded(w, http.StatusBadRequest, "bad_request", "skill_id and suggestion are required")
		return
	}
	var taskID *string
	if body.TaskID != nil && *body.TaskID != "" {
		taskID = body.TaskID
	}
	sug, err := s.Store.AddSuggestion(r.Context(), taskID, body.SkillID, body.Suggestion)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sug)
}

func (s *Service) handleDecideSuggestion(w http.ResponseWriter, r *http.Request) {
	if !s.requireEnabled(w, r) {
		return
	}
	var body struct {
		Decision string `json:"decision"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	id := r.PathValue("id")
	if err := s.Store.DecideSuggestion(r.Context(), id, body.Decision, s.actor(r)); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": body.Decision})
}

func (s *Service) handleMcpPolicies(w http.ResponseWriter, r *http.Request) {
	if !s.requireEnabled(w, r) {
		return
	}
	policies, err := s.Store.ListMcpPolicies(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	if policies == nil {
		policies = []McpPolicy{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"policies": policies})
}

func (s *Service) handleAssemble(w http.ResponseWriter, r *http.Request) {
	if !s.requireEnabled(w, r) {
		return
	}
	var body struct {
		Role         string   `json:"role"`
		Profile      string   `json:"profile"`
		TaskFeatures []string `json:"task_features"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	bundle, err := Assemble(body.Role, body.Profile, body.TaskFeatures)
	if err != nil {
		if strings.HasPrefix(err.Error(), "permission refused") {
			writeCoded(w, http.StatusForbidden, "permission_refused", err.Error())
			return
		}
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, bundle)
}

func (s *Service) handleGetSetting(w http.ResponseWriter, r *http.Request) {
	enabled, err := s.Store.FeatureEnabled(r.Context(), "skill_governance")
	if err != nil {
		writeCoded(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"feature": "skill_governance", "enabled": enabled})
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
	if err := s.Store.SetFeature(r.Context(), "skill_governance", *body.Enabled, s.actor(r)); err != nil {
		writeCoded(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"feature": "skill_governance", "enabled": *body.Enabled})
}
