package skill

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// abeval.go 是技能版本的 **A/B 评估执行器**（真盲评版，2026-09-21 重写）。
//
// 演进史（写在这里免得再走回头路）：
//   · 第一版：执行器根本不存在 —— 版本能进 evaluating、结果能记录、前端能读，
//     但**没有任何东西去产生那些结果**，线上 `skill_evaluation_runs` 长期 0 行。
//   · 第二版：加了执行器，但判定是"拿技能正文当带技能臂的答案，再在正文里找
//     测试题要求的关键词"。对照组恒为空串 → 恒 fail，带技能臂恒 pass，结论恒 win。
//     那是在量**文档里有没有写这个词**，不是量**agent 拿到这个技能会不会做得更好**。
//     用户 2026-09-21 指出方向错了："ab测试不应该是agent盲选吗"。
//   · 第三版（本文件）：真盲评。
//
// 真盲评的三条硬约束，缺一条这份记录就没有意义：
//  1. **同模型、同系统提示**：两臂唯一差别是用户消息里有没有技能正文；
//  2. **盲标**：两份答案按随机槽位（甲/乙）交给裁判，裁判不知道哪份用了技能；
//  3. **先判后反盲**：分数先落在槽位上，判完才归到 with / without 臂。
//
// 如实说明它不是什么：裁判是模型不是人，分数是**主观序**不是客观正确率。
// 它能证明"这个技能对这批题带来了增量"，不能证明"这个技能写得好"。

// ABQuestionResult 是一道题在两臂上的结果（已反盲）。
type ABQuestionResult struct {
	QuestionID string `json:"question_id"`
	Question   string `json:"question"`

	WithLabel    string `json:"with_label"`
	WithoutLabel string `json:"without_label"`

	WithAnswer    string  `json:"with_answer"`
	WithoutAnswer string  `json:"without_answer"`
	WithScore     float64 `json:"with_score"`
	WithoutScore  float64 `json:"without_score"`
	WithResult    string  `json:"with_result"`
	WithoutResult string  `json:"without_result"`

	// Winner 是**反盲后**的胜方臂：with | without | tie。
	Winner    string `json:"winner"`
	Rationale string `json:"rationale"`
}

// ABEvaluationSummary 是一次 A/B 评估的结论。
type ABEvaluationSummary struct {
	VersionID string             `json:"version_id"`
	SkillID   string             `json:"skill_id"`
	Judge     string             `json:"judge"`
	Questions []ABQuestionResult `json:"questions"`

	WithPass    int `json:"with_pass"`
	WithoutPass int `json:"without_pass"`

	WithMeanScore    float64 `json:"with_mean_score"`
	WithoutMeanScore float64 `json:"without_mean_score"`
	// WinMargin 是带技能臂的平均分优势（可为负）。
	WinMargin float64 `json:"win_margin"`

	// Verdict 是**如实结论**：win / lose / tie。只有"带技能臂明显更好"才是 win；
	// 分不开就是 tie，不硬凑一个通过。
	Verdict string `json:"verdict"`
}

const (
	// abPassScore 是单臂"这条通过"的分数线（10 分制）。6 分 ≈ 答案直接切题、
	// 有可执行步骤。它不是"优秀线"，只是"及格线"。
	abPassScore = 6.0
	// abWinMargin 是判定 win 的最小平均分差。小于它就算分不开（tie）——
	// 模型打分有噪声，0.2 分差不值得当成"技能有效"。
	abWinMargin = 0.5
)

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

// RunABEvaluation 对一个处于 evaluating / canary 的版本跑完整 A/B 盲评并落库。
//
// judgedBy 只用于审计（谁触发的）；`judged_by` 列写的是**裁判身份**
// （例如 `blind_llm_judge:tokendance.space:deepseek-v4.1-flash`），
// 这样"模型盲评"与"本地覆盖度检查"在库里一眼可分。
func (svc *Service) RunABEvaluation(ctx context.Context, versionID, judgedBy string) (ABEvaluationSummary, error) {
	if svc.Judge == nil {
		// 没有裁判就不写任何记录 —— **宁可没有记录，也不要假记录**。
		return ABEvaluationSummary{}, Refused("skill_evaluation_refused",
			"未接入模型裁判：请先在「模型」页保存一个可用的中转站（openai_chat_completions），"+
				"或设置 REPOMESH_AB_BASE_URL / REPOMESH_AB_API_KEY / REPOMESH_AB_MODEL")
	}
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
	judgeName := svc.Judge.Name()
	summary := ABEvaluationSummary{
		VersionID: versionID, SkillID: sk.ID, Judge: judgeName, Questions: []ABQuestionResult{},
	}
	for _, q := range questions {
		result, err := svc.evaluateOneQuestion(ctx, version, q, judgeName)
		if err != nil {
			return ABEvaluationSummary{}, err
		}
		if result.WithResult == ResultPass {
			summary.WithPass++
		}
		if result.WithoutResult == ResultPass {
			summary.WithoutPass++
		}
		summary.Questions = append(summary.Questions, result)
	}
	if n := len(summary.Questions); n > 0 {
		var withSum, withoutSum float64
		for _, q := range summary.Questions {
			withSum += q.WithScore
			withoutSum += q.WithoutScore
		}
		summary.WithMeanScore = withSum / float64(n)
		summary.WithoutMeanScore = withoutSum / float64(n)
		summary.WinMargin = summary.WithMeanScore - summary.WithoutMeanScore
	}
	switch {
	case summary.WinMargin >= abWinMargin && summary.WithMeanScore >= abPassScore:
		summary.Verdict = "win"
	case summary.WinMargin <= -abWinMargin:
		summary.Verdict = "lose"
	default:
		summary.Verdict = "tie"
	}
	_ = judgedBy // 触发者由调用方记审计；judged_by 列写裁判身份，两件事别混。
	return summary, nil
}

// evaluateOneQuestion 跑一道题的完整盲评：两臂作答 → 盲裁判 → 反盲 → 落库。
func (svc *Service) evaluateOneQuestion(ctx context.Context, version *SkillVersion, q TestQuestion, judgeName string) (ABQuestionResult, error) {
	withLabel, withoutLabel, labelA, labelB, err := svc.Store.BuildArms(ctx, q.ID)
	if err != nil {
		return ABQuestionResult{}, err
	}
	// ① 两臂各自作答。两臂互不可见，且**并行**跑（各自独立、无共享状态）。
	var (
		withAnswer, withoutAnswer string
		withErr, withoutErr       error
		wg                        sync.WaitGroup
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		withAnswer, withErr = svc.Judge.Answer(ctx, version.Content, q.Question)
	}()
	go func() {
		defer wg.Done()
		withoutAnswer, withoutErr = svc.Judge.Answer(ctx, "", q.Question)
	}()
	wg.Wait()
	if withErr != nil {
		return ABQuestionResult{}, fmt.Errorf("skills: 带技能臂作答失败（题 %s）: %w", q.ID, withErr)
	}
	if withoutErr != nil {
		return ABQuestionResult{}, fmt.Errorf("skills: 不带技能臂作答失败（题 %s）: %w", q.ID, withoutErr)
	}

	// ② 按盲标槽位摆好，交给裁判 —— 裁判只看得到"甲/乙"。
	slotA, slotB := withAnswer, withoutAnswer
	if labelA == withoutLabel {
		slotA, slotB = withoutAnswer, withAnswer
	}
	verdict, err := svc.Judge.Judge(ctx, q.Question, slotA, slotB)
	if err != nil {
		return ABQuestionResult{}, fmt.Errorf("skills: 盲裁判失败（题 %s）: %w", q.ID, err)
	}

	// ③ 反盲：分数先落在槽位上，判完才归到臂上。
	withScore, withoutScore := verdict.ScoreA, verdict.ScoreB
	if labelA == withoutLabel {
		withScore, withoutScore = verdict.ScoreB, verdict.ScoreA
	}
	winner := "tie"
	switch verdict.Winner {
	case "A":
		if labelA == withLabel {
			winner = ArmWith
		} else {
			winner = ArmWithout
		}
	case "B":
		if labelB == withLabel {
			winner = ArmWith
		} else {
			winner = ArmWithout
		}
	}
	withResult, withoutResult := ResultFail, ResultFail
	if withScore >= abPassScore {
		withResult = ResultPass
	}
	if withoutScore >= abPassScore {
		withoutResult = ResultPass
	}
	rationale := strings.TrimSpace(verdict.Rationale)
	base := map[string]any{
		"question":  q.Question,
		"judge":     judgeName,
		"winner":    winner,
		"rationale": rationale,
		"blinded":   true,
	}
	if _, err := svc.Store.RecordRun(ctx, version.ID, q.ID, ArmWith, withLabel,
		mergeAnswer(base, map[string]any{"text": withAnswer, "score": withScore}), &judgeName, withResult); err != nil {
		return ABQuestionResult{}, err
	}
	if _, err := svc.Store.RecordRun(ctx, version.ID, q.ID, ArmWithout, withoutLabel,
		mergeAnswer(base, map[string]any{"text": withoutAnswer, "score": withoutScore}), &judgeName, withoutResult); err != nil {
		return ABQuestionResult{}, err
	}
	return ABQuestionResult{
		QuestionID: q.ID, Question: q.Question,
		WithLabel: withLabel, WithoutLabel: withoutLabel,
		WithAnswer: withAnswer, WithoutAnswer: withoutAnswer,
		WithScore: withScore, WithoutScore: withoutScore,
		WithResult: withResult, WithoutResult: withoutResult,
		Winner: winner, Rationale: rationale,
	}, nil
}

func mergeAnswer(base, extra map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(extra))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}
