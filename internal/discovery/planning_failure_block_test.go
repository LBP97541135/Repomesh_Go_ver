package discovery

import (
	"strings"
	"testing"
)

// 2026-09-22 线上白屏的回归：① 分析失败时，失败状态块**必须仍然满足契约声明的
// 形状**。
//
// 事故链：模型额度耗尽 → 分析 run exit=1 → FailPlanningRun 只写
// {error, ran_at, producer} → 前端按契约读 `analysis.questions.length` →
// undefined.length 抛 TypeError → 整个控制台白屏。用户报的是"网站用不了了"，
// 真正的原因却只在数据库里三个键上 —— 这条用例就是钉住"失败块也得是完整形状"。
//
// 缺字段不等于"少显示一行"：契约里这些是必填，前端无守卫地读它们。
func TestPlanningFailureBlocksKeepContractShape(t *testing.T) {
	// 前端 DiscoveryAnalysisBlock 声明的必填字段（frontend/src/api/contract.ts）。
	analysisRequired := []string{
		"sufficient", "confidence", "missing_dimensions", "questions",
		"extracted_keywords", "answers", "analyzed_requirement", "forced_continue",
		"ran_at", "error",
	}
	// 前端 DiscoveryCandidatesBlock 声明的必填字段。
	candidatesRequired := []string{
		"items", "llm_used", "limit", "entry_point", "ran_at", "error",
	}

	const reason = "agent 进程未正常结束（state=exited exit=1）：429 Too Many Requests"

	analysis := analysisFailureBlock(reason)
	for _, key := range analysisRequired {
		if _, ok := analysis[key]; !ok {
			t.Errorf("analysis 失败块缺必填字段 %q —— 前端会 render 期 TypeError 白屏", key)
		}
	}
	// 数组字段必须是真数组（不是 nil）：前端直接 .length / .map，nil 会 panic 或
	// 让 JSON 里出现 null。
	for _, key := range []string{"questions", "extracted_keywords", "missing_dimensions", "dimensions"} {
		if _, ok := analysis[key].([]any); !ok {
			t.Errorf("analysis[%q] 必须是数组，got %T", key, analysis[key])
		}
	}
	// 失败必须如实：不够充分、没有结论、原因在 error 里。
	if sufficient, _ := analysis["sufficient"].(bool); sufficient {
		t.Error("失败块不能声称 sufficient=true")
	}
	if analyzed, _ := analysis["analyzed_requirement"].(string); analyzed != "" {
		t.Errorf("失败块不能编造 analyzed_requirement，got %q", analyzed)
	}
	if got, _ := analysis["error"].(string); !strings.Contains(got, "429") {
		t.Errorf("error 必须带真实原因，got %q", got)
	}

	candidates := candidatesFailureBlock(reason)
	for _, key := range candidatesRequired {
		if _, ok := candidates[key]; !ok {
			t.Errorf("candidates 失败块缺必填字段 %q", key)
		}
	}
	if _, ok := candidates["items"].([]any); !ok {
		t.Errorf("candidates[items] 必须是数组，got %T", candidates["items"])
	}
}
