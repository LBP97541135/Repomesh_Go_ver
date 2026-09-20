package skill

import (
	"context"
	"fmt"
)

// abeval.go 是技能版本的 **A/B 评估执行器**。
//
// 为什么要有它：整条路早就铺好了 —— 版本能进 evaluating（`/evaluate`）、
// 盲测两臂能构建（`BuildArms`）、结果能记录（`RecordRun`）、前端能读历史
// （`GET /versions/{id}/evaluations`）—— 但**没有任何东西去产生那些结果**：
// 线上实测 `skill_evaluation_runs` 至今 0 行。没有执行器，"送评估"就只是把状态
// 从 draft 改成 evaluating，然后永远停在那儿。
//
// 判定方式：**本地确定性覆盖度检查**，不依赖任何外部服务。
//   · 带技能臂（with skill）：以**待评版本的内容**作为"agent 手上的上下文"，
//     看它是否覆盖了测试题 `expected` 里要求的东西；
//   · 不带技能臂（without skill）：同样的题，但上下文为空。
// 两条臂用 `BuildArms` 给的**盲标**记录，判定完成前不暴露哪条用了技能。
//
// **如实说明它不是什么**：这不是 LLM 质量评判，是"技能正文里到底有没有写着
// 该写的东西"这一层下限检查。它能挡住"内容空洞却想过审"，挡不住"写得对但写得差"。
// 需要真实质量分时，接 LLM 评判（观测台那侧）即可，本执行器的记录形状不用改。

// ABQuestionResult 是一道题在两臂上的结果。
type ABQuestionResult struct {
	QuestionID   string  `json:"question_id"`
	Question     string  `json:"question"`
	WithLabel    string  `json:"with_label"`
	WithoutLabel string  `json:"without_label"`
	WithScore    float64 `json:"with_score"`
	WithoutScore float64 `json:"without_score"`
	WithResult   string  `json:"with_result"`
	WithoutResult string `json:"without_result"`
}

// ABEvaluationSummary 是一次 A/B 评估的结论。
type ABEvaluationSummary struct {
	VersionID string             `json:"version_id"`
	SkillID   string             `json:"skill_id"`
	Judge     string             `json:"judge"`
	Questions []ABQuestionResult `json:"questions"`
	WithPass  int                `json:"with_pass"`
	WithoutPass int              `json:"without_pass"`
	// Verdict 是**如实结论**：只有"带技能明显好于不带"才算 win。
	Verdict string `json:"verdict"`
}

// abPassThreshold 是覆盖度判定的下限。0.34 ≈ 期望答案里的关键词被覆盖到三分之一，
// 对"技能正文里写了这件事"来说是个宽松但不至于形同虚设的门槛。
const abPassThreshold = 0.34

// ListQuestions 取某个技能的全部测试题。
func (s *Store) ListQuestions(ctx context.Context, skillID string) ([]TestQuestion, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT id, skill_id, kind::text, question, expected::text, provided_by, created_at
		FROM public.skill_test_questions WHERE skill_id = $1 ORDER BY created_at`, skillID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TestQuestion{}
	for rows.Next() {
		q := TestQuestion{}
		var expectedText string
		if err := rows.Scan(&q.ID, &q.SkillID, (*string)(&q.Kind), &q.Question, &expectedText, &q.ProvidedBy, &q.CreatedAt); err != nil {
			return nil, err
		}
		q.Expected = parseJSON(expectedText)
		out = append(out, q)
	}
	return out, rows.Err()
}

// expectedText 把 expected 块压成一段可比较的文本。
// 形状是自由的 jsonb（种子题里是 {"must_mention":[…]}/{"must_not":[…]}/文本），
// 所以这里只做一件事：把里面所有字符串取出来拼起来。**不解释语义**。
func expectedText(expected map[string]any) string {
	out := ""
	for _, value := range expected {
		switch v := value.(type) {
		case string:
			out += " " + v
		case []any:
			for _, item := range v {
				if s, ok := item.(string); ok {
					out += " " + s
				}
			}
		}
	}
	return out
}

// RunABEvaluation 对一个处于 evaluating / canary 的版本跑完整 A/B 评估并记录结果。
func (svc *Service) RunABEvaluation(ctx context.Context, versionID, judgedBy string) (ABEvaluationSummary, error) {
	version, err := svc.Store.GetVersion(ctx, versionID)
	if err != nil {
		return ABEvaluationSummary{}, err
	}
	if version.Status != StatusEvaluating && version.Status != StatusCanary {
		return ABEvaluationSummary{}, Refused("skill_evaluation_refused",
			"evaluations are only accepted in evaluating or canary state, got %s", version.Status)
	}
	sk, err := svc.Store.getSkillByID(ctx, version.SkillID)
	if err != nil {
		return ABEvaluationSummary{}, err
	}
	questions, err := svc.Store.ListQuestions(ctx, sk.ID)
	if err != nil {
		return ABEvaluationSummary{}, err
	}
	if len(questions) == 0 {
		return ABEvaluationSummary{}, Refused("skill_evaluation_refused",
			"skill %q has no test questions to evaluate", sk.Name)
	}
	judge := "local_coverage_check"
	summary := ABEvaluationSummary{
		VersionID: versionID, SkillID: sk.ID, Judge: judge, Questions: []ABQuestionResult{},
	}
	for _, q := range questions {
		withLabel, withoutLabel, _, _, err := svc.Store.BuildArms(ctx, q.ID)
		if err != nil {
			return ABEvaluationSummary{}, err
		}
		want := expectedText(q.Expected)
		// 带技能臂：以**待评版本的内容**作为上下文；不带技能臂：上下文为空。
		withScore := TokenSimilarity(version.Content, want)
		withoutScore := TokenSimilarity("", want)
		withResult := "fail"
		if withScore >= abPassThreshold {
			withResult = "pass"
			summary.WithPass++
		}
		withoutResult := "fail"
		if withoutScore >= abPassThreshold {
			withoutResult = "pass"
			summary.WithoutPass++
		}
		if _, err := svc.Store.RecordRun(ctx, versionID, q.ID, "with_skill", withLabel,
			map[string]any{"text": version.Content, "score": withScore, "expected": want}, &judge, withResult); err != nil {
			return ABEvaluationSummary{}, err
		}
		if _, err := svc.Store.RecordRun(ctx, versionID, q.ID, "without_skill", withoutLabel,
			map[string]any{"text": "", "score": withoutScore, "expected": want}, &judge, withoutResult); err != nil {
			return ABEvaluationSummary{}, err
		}
		summary.Questions = append(summary.Questions, ABQuestionResult{
			QuestionID: q.ID, Question: q.Question,
			WithLabel: withLabel, WithoutLabel: withoutLabel,
			WithScore: withScore, WithoutScore: withoutScore,
			WithResult: withResult, WithoutResult: withoutResult,
		})
	}
	// 结论按**事实**给：带技能全过才算 win；带技能没过就是 lose；
	// 其余（部分过）是 inconclusive —— 不硬凑一个"通过"。
	switch {
	case summary.WithPass == len(summary.Questions) && summary.WithPass > summary.WithoutPass:
		summary.Verdict = "win"
	case summary.WithPass == 0:
		summary.Verdict = "lose"
	default:
		summary.Verdict = "inconclusive"
	}
	_ = fmt.Sprintf // 保留 fmt 以便后续扩展日志
	return summary, nil
}
