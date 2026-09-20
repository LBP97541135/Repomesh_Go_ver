package observeui

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"repomesh.local/repomesh/internal/observepipe"
)

type Sample struct {
	SubjectKind     string    `json:"subject_kind,omitempty"`
	Schema          string    `json:"schema"`
	ID              string    `json:"id"`
	SourceArchive   string    `json:"source_archive"`
	TrialID         string    `json:"trial_id,omitempty"`
	TraceID         string    `json:"trace_id,omitempty"`
	SubjectRevision string    `json:"subject_revision"`
	SnapshotRef     string    `json:"snapshot_ref"`
	Reason          string    `json:"reason"`
	JudgmentID      string    `json:"judgment_id,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

type SampleAnnotation struct {
	Schema          string    `json:"schema"`
	ID              string    `json:"id"`
	SampleID        string    `json:"sample_id"`
	SubjectRevision string    `json:"subject_revision"`
	Platform        string    `json:"platform"`
	DatasetID       string    `json:"dataset_id"`
	ItemID          string    `json:"item_id"`
	ExternalID      string    `json:"external_id"`
	TemplateVersion string    `json:"template_version"`
	ActorID         string    `json:"actor_id"`
	ActorType       string    `json:"actor_type"`
	Verdict         string    `json:"verdict"`
	FailureCategory string    `json:"failure_category"`
	Reason          string    `json:"reason"`
	Confirmed       bool      `json:"confirmed"`
	Supersedes      string    `json:"supersedes,omitempty"`
	AnnotatedAt     time.Time `json:"annotated_at"`
	RawRef          string    `json:"raw_ref"`
	Provenance      string    `json:"provenance"`
}

func (s *Server) sampleByID(id string) (Sample, *archive, error) {
	var found Sample
	var source *archive
	for i := range s.archives {
		a := &s.archives[i]
		var sample Sample
		err := a.Journal.ReadRecord("samples", id, &sample)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return sample, nil, err
		}
		var evidence any
		if sample.ID != id || a.Journal.ReadEvidence(sample.SnapshotRef, &evidence) != nil {
			return sample, nil, errors.New("invalid sample")
		}
		if source != nil && !jsonEqual(found, sample) {
			return Sample{}, nil, errors.New("sample identity conflict across archives")
		}
		found = sample
		source = a
	}
	if source == nil {
		return Sample{}, nil, os.ErrNotExist
	}
	return found, source, nil
}

func (s *Server) createSample(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Archive          string `json:"archive"`
		TrialID          string `json:"trial_id"`
		TraceID          string `json:"trace_id"`
		ExpectedRevision string `json:"expected_revision"`
		Reason           string `json:"reason"`
		JudgmentID       string `json:"judgment_id"`
	}
	if !readJSON(w, r, &p) {
		return
	}
	a := s.findArchive(p.Archive)
	if a == nil || (p.TrialID == "") == (p.TraceID == "") || p.ExpectedRevision == "" {
		failHTTP(w, 400, "select a source and exact evidence revision")
		return
	}
	if !oneOfStrings(p.Reason, "selected_trace", "low_score", "verification_failed", "disagreement", "high_score_audit", "representative_sample") {
		failHTTP(w, 400, "invalid sample reason")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var state map[string]any
	var revision string
	var err error
	if p.TrialID != "" {
		state, _, revision, _, err = s.trialMaterial(a, p.TrialID)
	} else {
		state, _, revision, err = s.traceMaterial(a, p.TraceID)
	}
	if err != nil {
		failHTTP(w, 409, "source evidence unavailable")
		return
	}
	if revision != p.ExpectedRevision {
		failHTTP(w, 409, "source evidence changed")
		return
	}
	if p.JudgmentID != "" {
		found := false
		for _, source := range s.archives {
			var j Judgment
			if source.Journal.ReadRecord("judgments", p.JudgmentID, &j) == nil && j.ID == p.JudgmentID && j.ArchiveID == a.ID && j.TrialID == p.TrialID && j.TraceID == p.TraceID && j.SubjectRevision == revision {
				found = true
			}
		}
		if !found {
			failHTTP(w, 409, "judgment does not match selected revision")
			return
		}
	}
	identity := observepipe.Digest([]byte(p.TrialID + "\x00" + p.TraceID + "\x00" + revision))
	if existing, _, err := s.sampleByID(identity); err == nil {
		if err := s.recordSelection(existing, p.Reason, p.JudgmentID); err != nil {
			failHTTP(w, 409, "selection conflict")
			return
		}
		writeJSON(w, 200, existing)
		return
	} else if !errors.Is(err, os.ErrNotExist) {
		failHTTP(w, 409, "existing sample is corrupt")
		return
	}
	ref, err := s.archives[0].Journal.PutJSON(map[string]any{"schema": "repomesh.sample-evidence/1", "source_archive": a.ID, "subject_revision": revision, "state": state})
	if err != nil {
		failHTTP(w, 500, "cannot freeze sample evidence")
		return
	}
	sample := Sample{SubjectKind: str(state, "subject_kind"), Schema: "repomesh.sample/1", ID: identity, SourceArchive: a.ID, TrialID: p.TrialID, TraceID: p.TraceID, SubjectRevision: revision, SnapshotRef: ref, Reason: p.Reason, JudgmentID: p.JudgmentID, CreatedAt: time.Now().UTC()}
	if err := s.archives[0].Journal.PutRecord("samples", identity, sample); err != nil {
		failHTTP(w, 409, "sample conflict or storage failure")
		return
	}
	if err := s.recordSelection(sample, p.Reason, p.JudgmentID); err != nil {
		failHTTP(w, 500, "sample saved; selection recovery needed")
		return
	}
	writeJSON(w, 201, sample)
}

type SampleSelection struct {
	ID         string `json:"id"`
	SampleID   string `json:"sample_id"`
	Reason     string `json:"reason"`
	JudgmentID string `json:"judgment_id,omitempty"`
}

func (s *Server) recordSelection(sample Sample, reason, judgment string) error {
	id := observepipe.Digest([]byte(sample.ID + "\x00" + reason + "\x00" + judgment))
	return s.archives[0].Journal.PutRecord("sample_selections", id, SampleSelection{ID: id, SampleID: sample.ID, Reason: reason, JudgmentID: judgment})
}

func oneOfStrings(value string, choices ...string) bool {
	for _, c := range choices {
		if value == c {
			return true
		}
	}
	return false
}

// Imports use an explicitly versioned local envelope. This is not a claim that
// AgentLoop exposes this schema as its own API or that operator input is verified.
func (s *Server) importAnnotation(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Annotation SampleAnnotation `json:"annotation"`
		Raw        json.RawMessage  `json:"raw"`
	}
	if !readJSON(w, r, &p) {
		return
	}
	a := p.Annotation
	if a.Schema != "repomesh.annotation-import/1" || a.Platform != "agentloop" || a.DatasetID == "" || a.ItemID == "" || a.ExternalID == "" || a.TemplateVersion == "" || a.ActorID == "" || a.AnnotatedAt.IsZero() || strings.TrimSpace(a.Reason) == "" || !oneOfStrings(a.ActorType, "human", "ai") || !oneOfStrings(a.Verdict, "pass", "fail", "unknown") || len(p.Raw) == 0 || string(p.Raw) == "null" {
		failHTTP(w, 400, "annotation export and provenance are required")
		return
	}
	if a.Confirmed && a.ActorType != "human" {
		failHTTP(w, 400, "AI labels cannot be human confirmed")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sample, _, err := s.sampleByID(a.SampleID)
	if err != nil || sample.SubjectRevision != a.SubjectRevision {
		failHTTP(w, 409, "annotation does not match sample revision")
		return
	}
	if a.Supersedes != "" {
		matched := false
		for _, source := range s.archives {
			var old SampleAnnotation
			if source.Journal.ReadRecord("annotations", a.Supersedes, &old) == nil && old.SampleID == a.SampleID && old.SubjectRevision == a.SubjectRevision {
				matched = true
			}
		}
		if !matched {
			failHTTP(w, 409, "prior annotation is not for this sample revision")
			return
		}
	}
	a.ID = observepipe.Digest([]byte(a.Platform + "\x00" + a.DatasetID + "\x00" + a.ItemID + "\x00" + a.ExternalID))
	a.Provenance = "operator_imported_not_api_verified"
	ref, err := s.archives[0].Journal.PutRawJSON(p.Raw)
	if err != nil {
		failHTTP(w, 400, "invalid raw annotation export")
		return
	}
	a.RawRef = ref
	if err := s.archives[0].Journal.PutRecord("annotations", a.ID, a); err != nil {
		failHTTP(w, 409, "annotation identity conflict or storage failure")
		return
	}
	writeJSON(w, 201, a)
}

func (s *Server) listSamples(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	samples := []map[string]any{}
	annotations := []SampleAnnotation{}
	faults := []map[string]string{}
	seen := map[string]bool{}
	for _, a := range s.archives {
		raw, err := a.Journal.Records("samples")
		if err != nil {
			faults = append(faults, map[string]string{"archive": a.ID, "error": "sample archive corrupt"})
			continue
		}
		for _, b := range raw {
			var row, verified Sample
			if json.Unmarshal(b, &row) != nil || a.Journal.ReadRecord("samples", row.ID, &verified) != nil || !jsonEqual(row, verified) {
				faults = append(faults, map[string]string{"archive": a.ID, "error": "invalid sample identity"})
				continue
			}
			if seen[row.ID] {
				if _, _, err := s.sampleByID(row.ID); err != nil {
					faults = append(faults, map[string]string{"archive": a.ID, "error": "sample identity conflict across archives"})
				}
				continue
			}
			seen[row.ID] = true
			var material json.RawMessage
			available := a.Journal.ReadEvidence(row.SnapshotRef, &material) == nil
			samples = append(samples, map[string]any{"sample": row, "archive": a.ID, "evidence_available": available})
		}
		raw, err = a.Journal.Records("annotations")
		if err != nil {
			faults = append(faults, map[string]string{"archive": a.ID, "error": "annotation archive corrupt"})
			continue
		}
		for _, b := range raw {
			var row, verified SampleAnnotation
			if json.Unmarshal(b, &row) != nil || a.Journal.ReadRecord("annotations", row.ID, &verified) != nil || !jsonEqual(row, verified) {
				faults = append(faults, map[string]string{"archive": a.ID, "error": "invalid annotation identity"})
				continue
			}
			var exported any
			if a.Journal.ReadEvidence(row.RawRef, &exported) != nil {
				faults = append(faults, map[string]string{"archive": a.ID, "error": "annotation export unavailable"})
				continue
			}
			annotations = append(annotations, row)
		}
	}
	sort.Slice(annotations, func(i, j int) bool { return annotations[i].AnnotatedAt.After(annotations[j].AnnotatedAt) })
	writeJSON(w, 200, map[string]any{"samples": samples, "annotations": annotations, "selections": s.sampleSelections(), "errors": faults, "platform_sync": "local_only"})
}

func jsonEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func (s *Server) sampleSelections() []SampleSelection {
	result := []SampleSelection{}
	seen := map[string]bool{}
	for _, a := range s.archives {
		rows, err := a.Journal.Records("sample_selections")
		if err != nil {
			continue
		}
		for _, raw := range rows {
			var row, verified SampleSelection
			if json.Unmarshal(raw, &row) == nil && a.Journal.ReadRecord("sample_selections", row.ID, &verified) == nil && jsonEqual(row, verified) && !seen[row.ID] {
				result = append(result, row)
				seen[row.ID] = true
			}
		}
	}
	return result
}

func (s *Server) exportSample(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sample, a, err := s.sampleByID(r.URL.Query().Get("id"))
	if err != nil {
		failHTTP(w, 404, "sample unavailable")
		return
	}
	var evidence json.RawMessage
	if err = a.Journal.ReadEvidence(sample.SnapshotRef, &evidence); err != nil {
		failHTTP(w, 409, "sample evidence unavailable")
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="repomesh-sample.json"`)
	writeJSON(w, 200, map[string]any{"schema": "repomesh.sample-export/1", "sample": sample, "evidence": redactJSON(evidence), "export_policy": "credential_fields_redacted/1", "export_target": "local_dataset", "notice": "Review text content before uploading; structured redaction cannot prove free text contains no secrets."})
}

func redactJSON(raw json.RawMessage) any {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return nil
	}
	var walk func(any) any
	walk = func(v any) any {
		switch x := v.(type) {
		case map[string]any:
			out := map[string]any{}
			for k, item := range x {
				lower := strings.ToLower(k)
				if strings.Contains(lower, "api_key") || strings.Contains(lower, "password") || strings.Contains(lower, "authorization") || strings.Contains(lower, "access_token") || strings.Contains(lower, "secret") {
					out[k] = "[redacted]"
				} else {
					out[k] = walk(item)
				}
			}
			return out
		case []any:
			out := make([]any, len(x))
			for i, item := range x {
				out[i] = walk(item)
			}
			return out
		default:
			return v
		}
	}
	return walk(value)
}
