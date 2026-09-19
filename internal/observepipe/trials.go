package observepipe

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type ArchivedTrial struct {
	TrialID     string   `json:"trial_id"`
	ReportRef   string   `json:"report_ref"`
	ManifestRef string   `json:"manifest_ref"`
	EventIDs    []string `json:"event_ids"`
	TraceIDs    []string `json:"trace_ids"`
	Verdict     string   `json:"verdict"`
}

func ArchiveTrial(j *Journal, r TrialResult) (ArchivedTrial, error) {
	a := ArchivedTrial{TrialID: r.TrialID, Verdict: r.Verdict, EventIDs: []string{}, TraceIDs: []string{}}
	if r.TrialID == "" || r.GradingAttemptID == "" {
		return a, errors.New("trial and grading attempt identity required")
	}
	v, g := AggregateChecks(r.RequiredChecks, r.Checks)
	if v != r.Verdict || g != r.GradingStatus {
		return a, errors.New("trial summary does not match required checks")
	}
	var err error
	a.ReportRef, err = j.PutJSON(r)
	if err != nil {
		return a, err
	}
	a.ManifestRef, err = j.PutJSON(map[string]any{"schema_version": "repomesh-context/0.1", "subject_kind": r.SubjectKind, "manifest": r.Manifest, "collector_build": CollectorBuild(), "limitations": r.Limitations})
	if err != nil {
		return a, err
	}
	for _, check := range r.Checks {
		e := trialEvent(r, a.ManifestRef, a.ReportRef, "validation.check.finished", r.GradingAttemptID+"/"+check.ID)
		e.CheckID = check.ID
		e.OutcomeVerdict = check.Verdict
		if check.ReasonCode != "" {
			e.ReasonCode = StringPtr(check.ReasonCode)
		}
		if check.Verdict == "fail" {
			e.ErrorClass = StringPtr("business_defect")
		}
		saved, _, err := j.Append(e)
		if err != nil {
			return a, err
		}
		a.EventIDs = append(a.EventIDs, saved.EventID)
		a.TraceIDs = append(a.TraceIDs, saved.TraceID)
	}
	e := trialEvent(r, a.ManifestRef, a.ReportRef, "validation.trial.finished", r.GradingAttemptID+"/summary")
	e.OutcomeVerdict = r.Verdict
	e.CausedByEventIDs = append([]string{}, a.EventIDs...)
	saved, _, err := j.Append(e)
	if err != nil {
		return a, err
	}
	a.EventIDs = append(a.EventIDs, saved.EventID)
	a.TraceIDs = append(a.TraceIDs, saved.TraceID)
	if err = j.PutRecord("trials", r.TrialID, a); err != nil {
		return a, err
	}
	return a, nil
}

func trialEvent(r TrialResult, manifest, report, name, key string) Event {
	e := NewEvent("trusted-verifier", r.GraderVersion, r.TrialID+"/"+key, name, "trial:"+r.TrialID, r.FinishedAt)
	e.ActorType = "verifier"
	e.ActorID = r.GraderID
	e.TrialID = r.TrialID
	e.CaseID = r.CaseID
	e.VariantID = r.VariantID
	e.ContextManifestRef = manifest
	e.InputArtifactRefs = []string{manifest}
	e.EvidenceRefs = []string{report}
	e.ExecutionStatus = r.ExecutionStatus
	e.CollectionStatus = "partial"
	e.MissingFields = []string{"repository_selection", "planning", "agent_execution", "human_review", "model_usage"}
	e.MissingReason = StringPtr("Independent HTTP verification only; no Agent capability or model-cost claim.")
	return e
}

func DatasetRows(j *Journal) ([]TrialDatasetRow, error) {
	paths, err := filepath.Glob(filepath.Join(j.Dir, "trials", "*.json"))
	if err != nil {
		return nil, err
	}
	rows := []TrialDatasetRow{}
	for _, p := range paths {
		info, err := os.Lstat(p)
		if err != nil || !info.Mode().IsRegular() {
			return nil, errors.New("invalid trial archive file")
		}
		b, err := j.read("trials/" + filepath.Base(p))
		if err != nil {
			return nil, err
		}
		var wrapper diskRecord
		if json.Unmarshal(b, &wrapper) != nil || Digest(wrapper.Data) != wrapper.Checksum {
			return nil, errors.New("trial archive checksum mismatch")
		}
		var a ArchivedTrial
		if json.Unmarshal(wrapper.Data, &a) != nil || a.TrialID == "" || filepath.Base(p) != Digest([]byte(a.TrialID))+".json" {
			return nil, errors.New("invalid trial archive identity")
		}
		var r TrialResult
		if err = j.ReadEvidence(a.ReportRef, &r); err != nil {
			return nil, err
		}
		var manifest json.RawMessage
		if err = j.ReadEvidence(a.ManifestRef, &manifest); err != nil {
			return nil, err
		}
		if r.TrialID != a.TrialID {
			return nil, errors.New("trial report identity mismatch")
		}
		row, err := NewTrialDatasetRow(r)
		if err != nil {
			return nil, err
		}
		row.TraceIDs = a.TraceIDs
		row.EvidenceRefs = []string{a.ManifestRef, a.ReportRef}
		if len(a.EventIDs) != len(a.TraceIDs) {
			return nil, errors.New("trial trace association is incomplete")
		}
		for n, id := range a.EventIDs {
			e, readErr := j.ReadEvent(id)
			if readErr != nil {
				return nil, readErr
			}
			if e.TrialID != a.TrialID || e.TraceID != a.TraceIDs[n] || e.ContextManifestRef != a.ManifestRef {
				return nil, errors.New("trial event identity mismatch")
			}
			for _, ref := range append(e.EvidenceRefs, e.InputArtifactRefs...) {
				var data json.RawMessage
				if err = j.ReadEvidence(ref, &data); err != nil {
					return nil, err
				}
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}
