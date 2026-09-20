package observeui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"repomesh.local/repomesh/internal/observepipe"
)

func newRecordID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

func (s *Server) testJev(w http.ResponseWriter, r *http.Request, c modelConfig) {
	if c.Key == "" {
		failHTTP(w, 409, "save a Jev API key first")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.typesafe.ai/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+c.Key)
	client := *s.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := client.Do(req)
	if err != nil {
		failHTTP(w, 502, "TypeSafe connection unavailable")
		return
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		failHTTP(w, 502, "TypeSafe model-list request failed")
		return
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil || len(data) == 1<<20 {
		failHTTP(w, 502, "invalid model list")
		return
	}
	var payload struct {
		Models *[]struct {
			Name        string  `json:"name"`
			Description *string `json:"description"`
			ReleaseDate *string `json:"release_date"`
		} `json:"models"`
	}
	if json.Unmarshal(data, &payload) != nil || payload.Models == nil {
		failHTTP(w, 502, "invalid model list")
		return
	}
	models := []string{}
	found := false
	for _, m := range *payload.Models {
		if m.Name == "" || m.Description == nil || m.ReleaseDate == nil {
			failHTTP(w, 502, "invalid model list")
			return
		}
		models = append(models, m.Name)
		if m.Name == c.Model {
			found = true
		}
	}
	listing := "not_listed_by_contract"
	versioned := regexp.MustCompile(`^jev-[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(c.Model)
	if !found && !versioned {
		listing = "alias_unavailable"
	}
	if found {
		listing = "listed"
	}
	writeJSON(w, 200, map[string]any{"ok": found || versioned, "authentication_ok": true, "models": models, "selected_model_listing": listing, "provider": "typesafe", "grading_verified": false, "message": "模型列表验证了连接；固定版本的可执行性由实际评分单独验证。"})
}

func (s *Server) trialMaterial(a *archive, id string) (map[string]any, map[string]bool, string, []string, error) {
	var archived observepipe.ArchivedTrial
	if err := a.Journal.ReadRecord("trials", id, &archived); err != nil {
		return nil, nil, "", nil, err
	}
	var report observepipe.TrialResult
	if err := a.Journal.ReadEvidence(archived.ReportRef, &report); err != nil || report.TrialID != id {
		return nil, nil, "", nil, errors.New("invalid trial evidence")
	}
	// Check the full graph before using an individual report.
	if _, err := observepipe.DatasetRows(a.Journal); err != nil {
		return nil, nil, "", nil, err
	}
	checks := []map[string]any{}
	contract := false
	assembly := true
	verification := false
	for _, c := range report.Checks {
		checks = append(checks, map[string]any{"check_id": c.ID, "expected": c.Expected, "actual": c.Actual, "reason_code": c.ReasonCode, "status": c.Status})
		if c.ID == "order.contract" && c.Actual != nil {
			contract = true
		}
		if strings.HasSuffix(c.ID, ".identity") && c.Verdict != "pass" {
			assembly = false
		}
		if c.ID == "order.persisted_amount" && c.Actual != nil {
			verification = true
		}
	}
	state := map[string]any{"public_task": report.PublicTask, "subject_kind": report.SubjectKind, "manifest": report.Manifest, "checks": checks, "http_observations": report.HTTPObservations, "limitations": report.Limitations}
	available := map[string]bool{"contract_evidence": contract && assembly, "verification": verification && assembly, "evidence": len(report.HTTPObservations) > 0, "tool_trajectory": false}
	if report.SubjectKind == "synthetic_fixed_product" {
		available["not_applicable:tool_efficiency"] = true
	}
	return state, available, archived.ReportRef, []string{archived.ReportRef, archived.ManifestRef}, nil
}

func (s *Server) judgeJev(w http.ResponseWriter, r *http.Request, a *archive, c modelConfig, trial, trace, expected, attemptKey string) {
	if (trial == "") == (trace == "") {
		failHTTP(w, 400, "select exactly one trial or trace")
		return
	}
	if !s.running.TryLock() {
		failHTTP(w, 409, "an evaluation is already running")
		return
	}
	defer s.running.Unlock()
	s.mu.Lock()
	var state map[string]any
	var available map[string]bool
	var revision string
	var evidence []string
	var err error
	if trial != "" {
		state, available, revision, evidence, err = s.trialMaterial(a, trial)
	} else {
		state, available, revision, err = s.traceMaterial(a, trace)
	}
	s.mu.Unlock()
	if err != nil {
		failHTTP(w, 409, "selected evidence missing, ineligible or corrupted")
		return
	}
	if trace != "" && state["binding_status"] != "exact" {
		failHTTP(w, 409, "trace requires a trusted task/attempt binding before paid grading")
		return
	}
	if expected != "" && expected != revision {
		failHTTP(w, 409, "evidence changed; refresh before grading")
		return
	}
	if containsSecret(state, c.Key) {
		failHTTP(w, 422, "selected evidence contains a grading credential")
		return
	}
	result, err := s.evaluateJev(r.Context(), a, c, trial, trace, revision, state, available, evidence, attemptKey)
	if err != nil {
		failHTTP(w, 409, err.Error())
		return
	}
	writeJSON(w, 201, result)
}

func (s *Server) evaluateJev(ctx context.Context, a *archive, c modelConfig, trial, trace, revision string, state map[string]any, available map[string]bool, evidence []string, attemptKey string) (Judgment, error) {
	state = s.modelPayload(state, c.Key)
	rubric := observepipe.ObservationRubric()
	questions, grades := observepipe.RubricQuestions(rubric, available)
	input := observepipe.JevRequest{Model: c.Model, State: state, Questions: questions}
	j := s.archives[0].Journal
	inputRef, err := j.PutJSON(map[string]any{"request": input, "rubric": rubric, "input_projection_version": "repomesh-jevrubric/3", "subject_revision": revision, "execution_purpose": "judge"})
	if err != nil {
		return Judgment{}, errors.New("cannot persist grading input")
	}
	claim := PaidAttempt{ResultID: newRecordID()}
	replayed := false
	var claimErr error
	if len(questions) > 0 {
		claim, replayed, claimErr = s.reservePaid("jev-rubric", c.Model, inputRef, trial+":"+trace+":"+revision, attemptKey)
	}
	if claimErr != nil {
		return Judgment{}, claimErr
	}
	if replayed {
		var prior Judgment
		if err := j.ReadRecord("judgments", claim.ResultID, &prior); err != nil {
			return Judgment{}, errPaidPending
		}
		return prior, nil
	}
	result := Judgment{ID: claim.ResultID, ClaimID: claim.ID, RequestedModel: c.Model, Provider: "typesafe", TrialID: trial, TraceID: trace, ArchiveID: a.ID, SubjectRevision: revision, ExecutionPurpose: "judge", Model: c.Model, Rubric: rubric.ID + "/" + rubric.Version, RubricSnapshot: &rubric, Status: "not_evaluated", Verdict: "unknown", InputRef: inputRef, EvidenceRefs: evidence, MissingEvidence: []string{}, CreatedAt: time.Now().UTC(), Dimensions: grades}
	if result.ID == "" {
		return Judgment{}, errors.New("cannot allocate grading identity")
	}
	started := time.Now()
	if len(questions) > 0 {
		response, callErr := observepipe.AskJev(ctx, s.client, c.Key, input)
		result.DurationMS = float64(time.Since(started)) / float64(time.Millisecond)
		if callErr != nil {
			result.Status = "error"
			result.Explanation = safeExplanation(callErr)
		} else {
			result.Status = "complete"
			result.Model = response.Model
			result.TypedAnswers = response.Answers
			result.Usage, _ = json.Marshal(response.Usage)
			result.RawRef, err = j.PutRawJSON(response.Raw)
			if err != nil {
				return Judgment{}, errors.New("cannot persist typed response")
			}
			result.Dimensions = observepipe.ApplyRubric(rubric, response, grades)
		}
	}
	if result.Status != "error" {
		result.Verdict, result.Explanation = observepipe.RubricSummary(result.Dimensions)
	}
	for _, g := range result.Dimensions {
		result.MissingEvidence = append(result.MissingEvidence, g.MissingFields...)
	}
	if err = j.PutRecord("judgments", result.ID, result); err != nil {
		return Judgment{}, errors.New("cannot persist grading result")
	}
	return result, nil
}

func containsSecret(value any, key string) bool {
	if key == "" {
		return false
	}
	b, _ := json.Marshal(value)
	return strings.Contains(string(b), key)
}
