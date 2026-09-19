package observepipe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestTrialCSVRoundTripChineseNewlinesAndNestedEvidence(t *testing.T) {
	trial, err := RunDiscountFixture(context.Background(), "baseline")
	if err != nil {
		t.Fatal(err)
	}
	row, err := NewTrialDatasetRow(trial)
	if err != nil {
		t.Fatal(err)
	}
	row.PublicTask = "折扣契约，含逗号\n第二行包含\"引号\"和换行"
	row.RelevantTrajectory = json.RawMessage(`{"message":"中文\n第二行","nested":{"before":[1,"x"],"after":null}}`)
	row.TraceIDs = []string{"11111111111111111111111111111111", "22222222222222222222222222222222"}
	row.EvidenceRefs = []string{"evidence/折扣.json"}
	var buf bytes.Buffer
	if err := WriteTrialCSV(&buf, []TrialDatasetRow{row}); err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeTrialCSV(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 1 || decoded[0]["public_task"] != row.PublicTask || decoded[0]["relevant_trajectory"] != string(row.RelevantTrajectory) || decoded[0]["verifier_reports"] != string(row.VerifierReports) {
		t.Fatalf("CSV lost values: %+v", decoded)
	}
	var traceIDs []string
	if err := json.Unmarshal([]byte(decoded[0]["trace_ids"]), &traceIDs); err != nil || len(traceIDs) != 2 {
		t.Fatal("trace list lost")
	}
	var duplicate bytes.Buffer
	if err := WriteTrialCSV(&duplicate, []TrialDatasetRow{row, row}); err == nil || duplicate.Len() != 0 {
		t.Fatal("duplicate trial allowed or partially exported")
	}
	row.VerifierReports = json.RawMessage(`{"broken"`)
	if err := WriteTrialCSV(&duplicate, []TrialDatasetRow{row}); err == nil {
		t.Fatal("invalid JSON exported")
	}
}

type rejectCSVWriter struct{}

func (rejectCSVWriter) Write([]byte) (int, error) { return 0, errors.New("disk unavailable") }
func TestTrialCSVReportsWriteFailure(t *testing.T) {
	if err := WriteTrialCSV(rejectCSVWriter{}, nil); err == nil {
		t.Fatal("CSV write error ignored")
	}
}

func platformTestConfig() (PlatformBinding, GraderConfig) {
	return PlatformBinding{TrialID: "local-trial", GradingAttemptID: "grading-1", PlatformTaskID: "platform-task", PlatformRunID: "platform-run", DataScope: "dataset", DatasetID: "dataset-1", DatasetItemID: "item-1"},
		GraderConfig{ID: "delivery_acceptance", Version: "1", EvaluatorName: "known-outcome", RequiredForSuccess: true, ResultType: "binary", ScoreName: "acceptance", ScoreField: "score_value", Unit: "binary", Minimum: 0, Maximum: 1, Direction: "higher_is_better", PassThreshold: 1}
}

func platformTestRecord() map[string]any {
	return map[string]any{
		"task_id": "platform-task", "run_id": "platform-run", "eval_id": "eval-1", "eval_base_id": "base-1", "status": "success",
		"evaluator_name": "known-outcome", "result_type": "binary", "score_name": "acceptance", "score_value": 0,
		"data_link": map[string]any{"data_scope": "dataset", "dataset_id": "dataset-1", "dataset_item_id": "item-1", "trace_id": "subject-trace"},
		"eval_meta": map[string]any{"evaluator_trace_id": "judge-trace"}, "explanation": "已执行评估，但金额错误。",
	}
}

func TestPlatformSuccessIsNotBusinessPassAndIdentityRemainsSeparate(t *testing.T) {
	binding, grader := platformTestConfig()
	raw, _ := json.Marshal(platformTestRecord())
	result, err := NormalizePlatformResult(raw, binding, grader)
	if err != nil {
		t.Fatal(err)
	}
	if result.PlatformStatus != "success" || result.Verdict != "fail" || result.Status != "scored" || result.Value == nil || *result.Value != 0 {
		t.Fatalf("execution success became business pass: %+v", result)
	}
	if result.TrialID != "local-trial" || result.PlatformTaskID == result.TrialID || result.SubjectTraceID != "subject-trace" || result.EvaluatorTraceID != "judge-trace" {
		t.Fatal("identity mapping lost")
	}
	if !bytes.Equal(result.Raw, raw) || result.ResultKey() == "" {
		t.Fatal("raw result or repeat key missing")
	}
}

func TestPlatformMissingUnknownErrorAndChangedFields(t *testing.T) {
	for _, tc := range []struct {
		name            string
		change          func(map[string]any, *GraderConfig)
		status, verdict string
		wantErr         bool
	}{
		{"missing_score", func(p map[string]any, _ *GraderConfig) { delete(p, "score_value") }, "unknown", "unknown", false},
		{"normalized_missing_no_fallback", func(_ map[string]any, g *GraderConfig) { g.ScoreField = "normalized_score_value" }, "unknown", "unknown", false},
		{"failed_with_score", func(p map[string]any, _ *GraderConfig) {
			p["status"] = "failed"
			p["score_value"] = 1
			p["error_code"] = "evidence_unavailable"
		}, "error", "unknown", false},
		{"unknown_with_score", func(p map[string]any, _ *GraderConfig) { p["status"] = "unknown"; p["score_value"] = 1 }, "unknown", "unknown", false},
		{"new_status", func(p map[string]any, _ *GraderConfig) { p["status"] = "succeeded" }, "error", "unknown", true},
		{"renamed_score", func(p map[string]any, _ *GraderConfig) { p["score_name"] = "another_metric" }, "error", "unknown", true},
		{"invalid_binary", func(p map[string]any, _ *GraderConfig) { p["score_value"] = 0.8 }, "error", "unknown", true},
		{"out_of_range", func(p map[string]any, _ *GraderConfig) { p["score_value"] = 2 }, "error", "unknown", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			binding, grader := platformTestConfig()
			record := platformTestRecord()
			tc.change(record, &grader)
			raw, _ := json.Marshal(record)
			result, err := NormalizePlatformResult(raw, binding, grader)
			if (err != nil) != tc.wantErr || result.Status != tc.status || result.Verdict != tc.verdict || result.Value != nil {
				t.Fatalf("got %+v err %v", result, err)
			}
			if !bytes.Equal(result.Raw, raw) {
				t.Fatal("raw record lost")
			}
		})
	}
}

func TestPlatformRequiresExactTrustedTrialBinding(t *testing.T) {
	for _, change := range []func(map[string]any, *PlatformBinding){
		func(p map[string]any, _ *PlatformBinding) { p["run_id"] = "other-run" },
		func(p map[string]any, _ *PlatformBinding) {
			p["data_link"].(map[string]any)["dataset_item_id"] = "other-item"
		},
		func(_ map[string]any, b *PlatformBinding) { b.TrialID = "" },
		func(_ map[string]any, b *PlatformBinding) { b.DataScope = "session" },
		func(p map[string]any, _ *PlatformBinding) { p["evaluator_name"] = "other-evaluator" },
	} {
		binding, grader := platformTestConfig()
		record := platformTestRecord()
		change(record, &binding)
		record["trial_id"] = "agent-claimed-trial"
		raw, _ := json.Marshal(record)
		result, err := NormalizePlatformResult(raw, binding, grader)
		if err == nil || result.Value != nil || result.Verdict != "unknown" {
			t.Fatal("untrusted mapping accepted")
		}
	}
}

func TestPlatformScoreConfigurationAndEmbeddedJSON(t *testing.T) {
	binding, grader := platformTestConfig()
	grader.ResultType, grader.Unit, grader.Maximum, grader.Direction, grader.PassThreshold = "score", "defect_count", 10, "lower_is_better", 2
	record := platformTestRecord()
	record["result_type"] = "score"
	record["score_value"] = 2
	record["score_range"] = "[0,100]"
	link, _ := json.Marshal(record["data_link"])
	record["data_link"] = string(link)
	meta, _ := json.Marshal(record["eval_meta"])
	record["eval_meta"] = string(meta)
	raw, _ := json.Marshal(record)
	result, err := NormalizePlatformResult(raw, binding, grader)
	if err != nil || result.Verdict != "pass" || result.Unit != "defect_count" {
		t.Fatalf("frozen scoring rules not used: %+v %v", result, err)
	}
	grader.PassThreshold = 11
	if _, err := NormalizePlatformResult(raw, binding, grader); err == nil {
		t.Fatal("invalid frozen threshold accepted")
	}
}

func TestPlatformDiagnosticEvidenceAndRequiredFlag(t *testing.T) {
	binding, grader := platformTestConfig()
	grader.ResultType = "text"
	grader.RequiredForSuccess = false
	for _, tc := range []struct {
		verdict      string
		refs         []string
		status, want string
	}{
		{"fail", []string{"evidence:contract"}, "scored", "fail"},
		{"pass", []string{"evidence:contract"}, "scored", "pass"},
		{"pass", nil, "unknown", "unknown"},
		{"pass", []string{}, "unknown", "unknown"},
		{"pass", []string{""}, "unknown", "unknown"},
		{"pass", []string{"  ", "\t\n"}, "unknown", "unknown"},
		{"pass", []string{"", "  evidence:contract  "}, "scored", "pass"},
		{"fail", []string{"  "}, "unknown", "unknown"},
		{"unknown", nil, "unknown", "unknown"},
		{"not_applicable", nil, "not_applicable", "not_applicable"},
	} {
		record := platformTestRecord()
		record["result_type"] = "text"
		record["required_for_success"] = true
		record["custom_outputs"] = map[string]any{"verdict": tc.verdict, "finding_code": "contract_evidence", "evidence_refs": tc.refs, "missing_evidence": []string{"consumed_input"}}
		raw, _ := json.Marshal(record)
		result, err := NormalizePlatformResult(raw, binding, grader)
		if err != nil || result.Status != tc.status || result.Verdict != tc.want || result.RequiredForSuccess || result.Value != nil {
			t.Fatalf("diagnostic normalization: %+v %v", result, err)
		}
		if result.Status == "scored" && (len(result.EvidenceRefs) != 1 || result.EvidenceRefs[0] != "evidence:contract") {
			t.Fatalf("empty or untrimmed evidence refs retained: %+v", result.EvidenceRefs)
		}
	}
}

func TestMergeGraderResultsCannotEraseDeterministicFailure(t *testing.T) {
	config := []GraderConfig{{ID: "delivery_acceptance", Version: "1", RequiredForSuccess: true}, {ID: "contract_handoff", Version: "1", RequiredForSuccess: true}, {ID: "soft", Version: "1", RequiredForSuccess: false}}
	results := []GraderResult{{GraderID: "delivery_acceptance", GraderVersion: "1", Status: "scored", Verdict: "fail"}, {GraderID: "contract_handoff", GraderVersion: "1", Status: "error", Verdict: "unknown"}, {GraderID: "soft", GraderVersion: "1", Status: "scored", Verdict: "pass"}}
	for i := range results {
		results[i].TrialID = "same-trial"
	}
	verdict, status := MergeGraderResults(config, results)
	if verdict != "fail" || status != "error" {
		t.Fatalf("got %s/%s", verdict, status)
	}
	results[0].Verdict = "pass"
	if verdict, _ := MergeGraderResults(config, results); verdict != "unknown" {
		t.Fatal("missing required grade became pass")
	}
	results[1].Status, results[1].Verdict = "not_applicable", "not_applicable"
	if verdict, _ := MergeGraderResults(config, results); verdict != "unknown" {
		t.Fatal("required not applicable became pass")
	}
	results[1].TrialID = "different-trial"
	if verdict, status := MergeGraderResults(config, results); verdict != "unknown" || status != "error" {
		t.Fatal("grades from different trials were aggregated")
	}
}
