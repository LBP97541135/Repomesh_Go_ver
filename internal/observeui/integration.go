package observeui

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"repomesh.local/repomesh/internal/observepipe"
)

type integrationConfig struct {
	Mode      string            `json:"mode"`
	Workspace string            `json:"workspace"`
	Links     map[string]string `json:"links"`
}

func (s *Server) loadIntegration() (integrationConfig, error) {
	c := integrationConfig{Mode: "local", Links: map[string]string{}}
	path := filepath.Join(s.archives[0].Journal.Dir, "integration.json")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 16384 {
		return c, errors.New("invalid integration configuration file")
	}
	b, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(b, &c) != nil {
		return c, errors.New("invalid integration configuration")
	}
	return c, validateIntegration(c)
}

func validateIntegration(c integrationConfig) error {
	if c.Mode != "local" || c.Workspace != "" || len(c.Links) != 0 {
		return errors.New("observation is local-only; cloud workspace and console links are disabled")
	}
	return nil
}

func (s *Server) saveIntegration(w http.ResponseWriter, r *http.Request) {
	var c integrationConfig
	if !readJSON(w, r, &c) {
		return
	}
	if err := validateIntegration(c); err != nil {
		failHTTP(w, 400, err.Error())
		return
	}
	s.modelMu.Lock()
	defer s.modelMu.Unlock()
	b, _ := json.Marshal(c)
	f, err := os.CreateTemp(s.archives[0].Journal.Dir, ".integration-")
	if err != nil {
		failHTTP(w, 500, "cannot save integration")
		return
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(f.Name(), filepath.Join(s.archives[0].Journal.Dir, "integration.json"))
	}
	if err != nil {
		failHTTP(w, 500, "cannot save integration")
		return
	}
	writeJSON(w, 200, map[string]any{"config": c, "mode": "local"})
}

func (s *Server) connectionStatus(w http.ResponseWriter, r *http.Request) {
	s.modelMu.Lock()
	c, err := s.loadIntegration()
	s.modelMu.Unlock()
	if err != nil {
		failHTTP(w, 409, err.Error())
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	calls, faults := s.calls()
	sources := map[string]map[string]any{}
	spansCount := 0
	eventCount := 0
	imported := 0
	for _, a := range s.archives {
		spans, e := readSpans(a.Journal)
		if e == nil {
			spansCount += len(spans)
		}
		events, e := a.Journal.Events()
		if e == nil {
			eventCount += len(events)
		}
		runs, e := a.Journal.Records("platform_runs")
		if e == nil {
			imported += len(runs)
		}
	}
	for _, call := range calls {
		row := sources[call.SourceID]
		if row == nil {
			row = map[string]any{"source_id": call.SourceID, "calls": 0, "last_seen_at": call.Start, "subject_kind": call.SubjectKind, "binding": "unverified", "runtime_acceptance": "not_run"}
			sources[call.SourceID] = row
		}
		row["calls"] = row["calls"].(int) + 1
		if call.Start.After(row["last_seen_at"].(time.Time)) {
			row["last_seen_at"] = call.Start
		}
	}
	health := []map[string]any{}
	for _, a := range s.archives {
		paths, _ := filepath.Glob(filepath.Join(a.Journal.Dir, "capture-*.json"))
		for _, path := range paths {
			info, err := os.Lstat(path)
			if err != nil || !info.Mode().IsRegular() || info.Size() > 8192 {
				continue
			}
			b, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			var value map[string]any
			if json.Unmarshal(b, &value) == nil {
				value["archive"] = a.ID
				health = append(health, value)
			}
		}
	}
	attempts := []map[string]any{}
	rawAttempts, readErr := s.archives[0].Journal.Records("paid_attempts")
	if readErr != nil {
		faults = append(faults, map[string]string{"error": "paid attempt ledger unreadable"})
	}
	for _, raw := range rawAttempts {
		var attempt PaidAttempt
		if json.Unmarshal(raw, &attempt) != nil {
			continue
		}
		status := "outcome_unknown_or_interrupted"
		if attempt.Kind == "jev-rubric" {
			var result Judgment
			if s.archives[0].Journal.ReadRecord("judgments", attempt.ResultID, &result) == nil && result.ClaimID == attempt.ID {
				status = result.Status
			}
		} else {
			var result LocalAnalysis
			if s.archives[0].Journal.ReadRecord("analyses", attempt.ResultID, &result) == nil && result.ClaimID == attempt.ID {
				status = result.Status
			}
		}
		attempts = append(attempts, map[string]any{"attempt": attempt, "status": status})
	}
	writeJSON(w, 200, map[string]any{"checked_at": time.Now().UTC(), "config": c, "local_archive": "available", "otlp_received_spans": spansCount, "committed_events": eventCount, "sources": sources, "errors": faults, "capture_health": health, "paid_attempts": attempts, "historical_imports": imported, "cloud_required": false, "runtime": map[string]string{"native_dsh": "not_connected", "gate": "G2_NOT_RUN"}})
}

type PlatformRun struct {
	ID               string   `json:"id"`
	SampleID         string   `json:"sample_id"`
	SubjectRevision  string   `json:"subject_revision"`
	Workspace        string   `json:"workspace"`
	TaskID           string   `json:"task_id"`
	RunID            string   `json:"run_id"`
	ResultID         string   `json:"result_id"`
	EvaluatorID      string   `json:"evaluator_id"`
	EvaluatorVersion string   `json:"evaluator_version"`
	Status           string   `json:"status"`
	Score            *float64 `json:"score"`
	ScoreMin         float64  `json:"score_min"`
	ScoreMax         float64  `json:"score_max"`
	Explanation      string   `json:"explanation"`
	Attribution      string   `json:"attribution"`
	RawRef           string   `json:"raw_ref"`
	ConfigRef        string   `json:"config_ref"`
	Provenance       string   `json:"provenance"`
}

func (s *Server) importPlatformRun(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Schema    string          `json:"schema"`
		Result    PlatformRun     `json:"result"`
		Raw       json.RawMessage `json:"raw"`
		Evaluator json.RawMessage `json:"evaluator"`
	}
	if !readJSON(w, r, &p) {
		return
	}
	row := p.Result
	if p.Schema != "repomesh.platform-import/1" || row.Workspace == "" || row.TaskID == "" || row.RunID == "" || row.ResultID == "" || row.EvaluatorID == "" || row.EvaluatorVersion == "" || !oneOfStrings(row.Status, "success", "failed", "pending", "cancelled") || len(p.Raw) == 0 || string(p.Raw) == "null" || len(p.Evaluator) == 0 || string(p.Evaluator) == "null" {
		failHTTP(w, 400, "platform result identity, raw export and evaluator configuration required")
		return
	}
	if row.Score != nil && (row.Status != "success" || row.ScoreMax <= row.ScoreMin || *row.Score < row.ScoreMin || *row.Score > row.ScoreMax) {
		failHTTP(w, 400, "invalid score or scale")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sample, _, err := s.sampleByID(row.SampleID)
	if err != nil || sample.SubjectRevision != row.SubjectRevision {
		failHTTP(w, 409, "platform result does not match frozen sample")
		return
	}
	row.ID = observepipe.Digest([]byte(strings.Join([]string{row.Workspace, row.TaskID, row.RunID, row.ResultID, row.EvaluatorID}, "\x00")))
	row.Provenance = "operator_mapped_export_not_api_verified"
	row.RawRef, err = s.archives[0].Journal.PutRawJSON(p.Raw)
	if err == nil {
		row.ConfigRef, err = s.archives[0].Journal.PutRawJSON(p.Evaluator)
	}
	if err != nil {
		failHTTP(w, 400, "invalid platform evidence")
		return
	}
	if err = s.archives[0].Journal.PutRecord("platform_runs", row.ID, row); err != nil {
		failHTTP(w, 409, "platform result conflict or storage failure")
		return
	}
	writeJSON(w, 201, row)
}

func (s *Server) evaluationList(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	judgments := []Judgment{}
	platform := []PlatformRun{}
	legacy := []json.RawMessage{}
	faults := []map[string]string{}
	for _, a := range s.archives {
		rows, err := readJudgments(a.Journal)
		if err != nil {
			faults = append(faults, map[string]string{"archive": a.ID, "error": "judgment evidence unavailable"})
		} else {
			judgments = append(judgments, rows...)
		}
		old, err := a.Journal.Records("platform")
		if err != nil {
			faults = append(faults, map[string]string{"archive": a.ID, "error": "legacy platform archive invalid"})
		} else {
			legacy = append(legacy, old...)
		}
		records, err := a.Journal.Records("platform_runs")
		if err != nil {
			faults = append(faults, map[string]string{"archive": a.ID, "error": "platform archive invalid"})
			continue
		}
		for _, raw := range records {
			var row, verified PlatformRun
			if json.Unmarshal(raw, &row) != nil || a.Journal.ReadRecord("platform_runs", row.ID, &verified) != nil || !jsonEqual(row, verified) {
				faults = append(faults, map[string]string{"archive": a.ID, "error": "platform identity conflict"})
				continue
			}
			var value json.RawMessage
			if a.Journal.ReadEvidence(row.RawRef, &value) != nil || a.Journal.ReadEvidence(row.ConfigRef, &value) != nil {
				faults = append(faults, map[string]string{"archive": a.ID, "error": "platform evidence unavailable"})
				continue
			}
			platform = append(platform, row)
		}
	}
	sort.Slice(judgments, func(i, j int) bool { return judgments[i].CreatedAt.After(judgments[j].CreatedAt) })
	writeJSON(w, 200, map[string]any{"judgments": judgments, "platform_results": platform, "legacy_platform_results": legacy, "errors": faults, "business_verdict": "see_independent_verifier"})
}
