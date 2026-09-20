package observeui

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"repomesh.local/repomesh/internal/observepipe"
)

type modelConfig struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model"`
	Key      string `json:"api_key"`
}
type Judgment struct {
	ClaimID        string `json:"claim_id,omitempty"`
	RequestedModel string `json:"requested_model,omitempty"`

	Provider         string                           `json:"provider,omitempty"`
	TraceID          string                           `json:"trace_id,omitempty"`
	SubjectRevision  string                           `json:"subject_revision,omitempty"`
	ExecutionPurpose string                           `json:"execution_purpose,omitempty"`
	DurationMS       float64                          `json:"duration_ms,omitempty"`
	RubricSnapshot   *observepipe.Rubric              `json:"rubric_snapshot,omitempty"`
	Dimensions       []observepipe.DimensionGrade     `json:"dimensions,omitempty"`
	TypedAnswers     map[string]observepipe.JevAnswer `json:"typed_answers,omitempty"`
	ID               string                           `json:"id"`
	TrialID          string                           `json:"trial_id"`
	ArchiveID        string                           `json:"source_archive_id"`
	Model            string                           `json:"model"`
	Rubric           string                           `json:"rubric"`
	Status           string                           `json:"status"`
	Verdict          string                           `json:"verdict"`
	Explanation      string                           `json:"explanation"`
	EvidenceRefs     []string                         `json:"evidence_refs"`
	MissingEvidence  []string                         `json:"missing_evidence"`
	RawRef           string                           `json:"raw_ref,omitempty"`
	InputRef         string                           `json:"input_ref"`
	Usage            json.RawMessage                  `json:"usage,omitempty"`
	CreatedAt        time.Time                        `json:"created_at"`
}

func (s *Server) modelPath() string { return filepath.Join(s.archives[0].Journal.Dir, "model.json") }
func (s *Server) loadModel() (modelConfig, error) {
	s.modelMu.Lock()
	defer s.modelMu.Unlock()
	c := modelConfig{Provider: "typesafe", Model: "jev-1.13.0"}
	info, err := os.Lstat(s.modelPath())
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 16384 {
		return c, errors.New("model config must be a private regular file")
	}
	b, err := os.ReadFile(s.modelPath())
	if err != nil {
		return c, errors.New("cannot read model config")
	}
	c = modelConfig{}
	if json.Unmarshal(b, &c) != nil {
		return modelConfig{}, errors.New("invalid model config")
	}
	if c.Provider == "" {
		c.Provider = modelProvider(c.Model)
	}
	return c, nil
}

func modelProvider(model string) string {
	if strings.HasPrefix(model, "jev-") {
		return "typesafe"
	}
	return "deepseek"
}

func (s *Server) settings(w http.ResponseWriter, _ *http.Request) {
	c, err := s.loadModel()
	if err != nil {
		failHTTP(w, 500, err.Error())
		return
	}
	archives := []map[string]any{}
	for _, a := range s.archives {
		archives = append(archives, map[string]any{"id": a.ID, "path": a.Journal.Dir, "writable": a.Writable})
	}
	endpoint := s.modelEndpoint
	if c.Provider == "typesafe" {
		endpoint = observepipe.JevEndpoint
	}
	writeJSON(w, 200, map[string]any{"provider": c.Provider, "rubric": observepipe.ObservationRubric(), "mode": "local", "cloud_required": false, "archives": archives, "model": c.Model, "model_configured": c.Key != "", "model_endpoint": endpoint, "dsh": "not_connected", "evaluation": "local_deterministic_and_typed_rubric", "data_policy": "Only the selected frozen evidence is sent to the configured grading provider. Storage and rule verification remain local."})
}

func (s *Server) saveModel(w http.ResponseWriter, r *http.Request) {
	var c modelConfig
	if !readJSON(w, r, &c) {
		return
	}
	c.Model = strings.TrimSpace(c.Model)
	c.Key = strings.TrimSpace(c.Key)
	if c.Provider == "" {
		c.Provider = modelProvider(c.Model)
	}
	if c.Provider != "typesafe" || !observepipe.ValidJevModel(c.Model) {
		failHTTP(w, 400, "invalid grading provider or model")
		return
	}
	if strings.ContainsAny(c.Key, "\r\n\t ") || len(c.Key) > 4096 {
		failHTTP(w, 400, "invalid API key")
		return
	}
	if c.Key == "" {
		old, err := s.loadModel()
		if err != nil {
			failHTTP(w, 500, err.Error())
			return
		}
		if old.Provider != c.Provider {
			failHTTP(w, 400, "a new key is required when changing provider")
			return
		}
		c.Key = old.Key
	}
	if c.Key == "" {
		failHTTP(w, 400, "API key required for first configuration")
		return
	}
	s.modelMu.Lock()
	b, _ := json.Marshal(c)
	f, err := os.CreateTemp(s.archives[0].Journal.Dir, ".model-")
	if err == nil {
		defer os.Remove(f.Name())
		_, err = f.Write(b)
		if err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
		if err == nil {
			err = os.Rename(f.Name(), s.modelPath())
		}
	}
	s.modelMu.Unlock()
	if err != nil {
		failHTTP(w, 500, "cannot save private model config")
		return
	}
	writeJSON(w, 200, map[string]any{"configured": true, "model": c.Model})
}

func (s *Server) testModel(w http.ResponseWriter, r *http.Request) {
	c, err := s.loadModel()
	if err != nil {
		failHTTP(w, 500, err.Error())
		return
	}
	if c.Provider != "typesafe" {
		failHTTP(w, 409, "legacy DeepSeek grading is read-only; configure Jev for rubric scoring")
		return
	}
	s.testJev(w, r, c)
}

func (s *Server) judge(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Archive          string `json:"archive"`
		TrialID          string `json:"trial_id"`
		TraceID          string `json:"trace_id"`
		ExpectedRevision string `json:"expected_revision"`
		AttemptKey       string `json:"attempt_key"`
	}
	if !readJSON(w, r, &p) {
		return
	}
	a := s.findArchive(p.Archive)
	if a == nil {
		failHTTP(w, 404, "archive not found")
		return
	}
	c, err := s.loadModel()
	if err != nil {
		failHTTP(w, 500, "invalid grading configuration")
		return
	}
	if c.Provider != "typesafe" {
		failHTTP(w, 409, "rubric scoring requires Jev; DeepSeek is configured separately for analysis")
		return
	}
	if c.Key == "" {
		failHTTP(w, 409, "configure a Jev grading key first")
		return
	}
	if len(p.AttemptKey) > 128 {
		failHTTP(w, 400, "attempt key too long")
		return
	}
	s.judgeJev(w, r, a, c, p.TrialID, p.TraceID, p.ExpectedRevision, p.AttemptKey)
}

func readJudgments(j *observepipe.Journal) ([]Judgment, error) {
	records, err := j.Records("judgments")
	if err != nil {
		return nil, err
	}
	results := []Judgment{}
	seen := map[string]bool{}
	for _, raw := range records {
		var r, verified Judgment
		if json.Unmarshal(raw, &r) != nil || !validHex(r.ID, 16) || seen[r.ID] {
			return nil, errors.New("invalid judgment identity")
		}
		seen[r.ID] = true
		if err = j.ReadRecord("judgments", r.ID, &verified); err != nil {
			return nil, err
		}
		b, _ := json.Marshal(verified)
		a, _ := json.Marshal(r)
		if !bytes.Equal(a, b) {
			return nil, errors.New("judgment identity mismatch")
		}
		for _, ref := range []string{r.InputRef, r.RawRef} {
			if ref != "" {
				var v json.RawMessage
				if err = j.ReadEvidence(ref, &v); err != nil {
					return nil, err
				}
			}
		}
		results = append(results, r)
	}
	return results, nil
}
