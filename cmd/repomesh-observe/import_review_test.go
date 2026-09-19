package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"repomesh.local/repomesh/internal/observepipe"
)

func TestImportResultRequiresTraceBelongingToSelectedTrial(t *testing.T) {
	j, err := observepipe.OpenJournal(filepath.Join(t.TempDir(), "archive"))
	if err != nil {
		t.Fatal(err)
	}
	local, err := observepipe.RunDiscountFixture(t.Context(), "baseline")
	if err != nil {
		t.Fatal(err)
	}
	selected, err := observepipe.ArchiveTrial(j, local)
	if err != nil {
		t.Fatal(err)
	}
	otherResult := local
	otherResult.TrialID += "-different-trial"
	other, err := observepipe.ArchiveTrial(j, otherResult)
	if err != nil {
		t.Fatal(err)
	}
	binding := observepipe.PlatformBinding{TrialID: selected.TrialID, GradingAttemptID: "review-grading-attempt", PlatformTaskID: "review-task", PlatformRunID: "review-run", DataScope: "trace", SubjectTraceID: other.TraceIDs[0]}
	grader := observepipe.GraderConfig{ID: "review-grader", Version: "1", EvaluatorName: "review-grader", ResultType: "binary", ScoreName: "acceptance", ScoreField: "score_value", Unit: "binary", Minimum: 0, Maximum: 1, Direction: "higher_is_better", PassThreshold: 1}
	record := map[string]any{"task_id": binding.PlatformTaskID, "run_id": binding.PlatformRunID, "eval_id": "review-eval", "eval_base_id": "review-base", "status": "success", "evaluator_name": grader.EvaluatorName, "result_type": "binary", "score_name": "acceptance", "score_value": 1, "data_link": map[string]any{"data_scope": "trace", "trace_id": binding.SubjectTraceID}}
	inputs := t.TempDir()
	write := func(name string, value any) string {
		t.Helper()
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(inputs, name+".json")
		if err = os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	bindingPath, graderPath, rawPath := write("binding", binding), write("grader", grader), write("raw", record)
	args := []string{"import-result", "--archive", j.Dir, "--binding", bindingPath, "--grader", graderPath, "--raw", rawPath}
	var out, errOut bytes.Buffer
	if code := run(t.Context(), args, &out, &errOut); code != 2 || out.Len() != 0 || !strings.Contains(errOut.String(), "does not belong") {
		t.Fatalf("other trial's trace accepted: exit=%d, stdout=%s, stderr=%s", code, &out, &errOut)
	}
	if records, err := filepath.Glob(filepath.Join(j.Dir, "platform", "*.json")); err != nil || len(records) != 0 {
		t.Fatalf("rejected binding left a score: records=%d, error=%v", len(records), err)
	}

	binding.SubjectTraceID = selected.TraceIDs[0]
	record["data_link"] = map[string]any{"data_scope": "trace", "trace_id": binding.SubjectTraceID}
	write("binding", binding)
	write("raw", record)
	for range 2 {
		out.Reset()
		errOut.Reset()
		if code := run(t.Context(), args, &out, &errOut); code != 0 {
			t.Fatalf("correct binding rejected: exit=%d, stderr=%s", code, &errOut)
		}
		var summary struct {
			Verdict              string `json:"verdict"`
			DeterministicVerdict string `json:"deterministic_verdict"`
		}
		if err = json.Unmarshal(out.Bytes(), &summary); err != nil || summary.Verdict != "pass" || summary.DeterministicVerdict != "fail" {
			t.Fatalf("platform grade overwrote deterministic failure: %s, error=%v", &out, err)
		}
	}
	if records, err := filepath.Glob(filepath.Join(j.Dir, "platform", "*.json")); err != nil || len(records) != 1 {
		t.Fatalf("repeat result was counted twice: records=%d, error=%v", len(records), err)
	}
}
