package discovery

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ---- step 0: requirement analysis ----

var dimensionNames = []string{"业务场景", "行为描述", "变更类型", "技术约束"}

// Analysis runs step 0. Without a live model the deterministic analyzer
// extracts keywords and reports honestly which dimensions the requirement
// text covers; insufficient analysis blocks step 1 unless force_continue.
func (s *Service) Analysis(ctx context.Context, issueID, agentID, idempotencyKey string, answers []Answer, forceContinue bool) (map[string]any, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	title, description, err := s.ensureIssue(ctx, tx, issueID)
	if err != nil {
		return nil, err
	}
	st, err := s.load(ctx, tx, issueID)
	if err != nil {
		return nil, err
	}
	if st == nil {
		combined := title + "\n" + description
		st = &State{IssueID: issueID, RequirementText: strings.TrimSpace(combined)}
	}
	if st.ProjectID == "" {
		var projectID string
		if err := tx.QueryRow(ctx, "SELECT project_id FROM repomesh_issues.issues WHERE id=$1", issueID).Scan(&projectID); err != nil {
			return nil, err
		}
		st.ProjectID = projectID
	}
	if receipt, ok := replay(st, idempotencyKey); ok {
		tx.Rollback(ctx)
		return receipt, nil
	}
	if forceContinue {
		if st.Analysis == nil {
			tx.Rollback(ctx)
			return nil, fmt.Errorf("%w: no analysis to force-continue", ErrConflict)
		}
		ignored := 0
		if questions, ok := st.Analysis["questions"].([]any); ok {
			ignored = len(questions)
		}
		st.Analysis["forced_continue"] = map[string]any{
			"ignored_question_count": ignored, "by_agent_id": agentID, "at": time.Now().UTC(),
		}
		receipt := map[string]any{"task_id": nil, "step": 1, "status": "accepted"}
		recordReceipt(st, idempotencyKey, receipt)
		if err := s.save(ctx, tx, st); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return receipt, nil
	}
	text := st.RequirementText
	for _, answer := range answers {
		text += "\n" + answer.Question + ": " + answer.Answer
	}
	lower := strings.ToLower(text)
	keywords := extractKeywords(lower)
	questions := []string{}
	dimensions := make([]map[string]any, 0, len(dimensionNames))
	missing := []string{}
	coverage := map[string][]string{
		"业务场景": {"用户", "场景", "业务", "需求", "客户"},
		"行为描述": {"应该", "需要", "支持", "实现", "行为", "功能", "接口"},
		"变更类型": {"新增", "修改", "重构", "修复", "迁移", "删除", "升级"},
		"技术约束": {"性能", "安全", "兼容", "依赖", "协议", "约束", "限制"},
	}
	for _, name := range dimensionNames {
		covered := false
		for _, marker := range coverage[name] {
			if strings.Contains(lower, marker) {
				covered = true
				break
			}
		}
		note := "未在需求文本中找到明确表述"
		if covered {
			note = "需求文本包含相关表述"
		} else {
			missing = append(missing, name)
			questions = append(questions, "请补充该需求的"+name+"：具体期望是什么？")
		}
		dimensions = append(dimensions, map[string]any{"name": name, "covered": covered, "note": note})
	}
	sufficient := len(missing) == 0
	confidence := float64(len(dimensionNames)-len(missing)) / float64(len(dimensionNames))
	block := map[string]any{
		"sufficient":           sufficient,
		"confidence":           confidence,
		"missing_dimensions":   missing,
		"dimensions":           dimensions,
		"questions":            questions,
		"extracted_keywords":   keywords,
		"answers":              answers,
		"analyzed_requirement": text,
		"forced_continue":      nil,
		"ran_at":               time.Now().UTC(),
		"by_agent_id":          agentID,
		"error":                nil,
	}
	st.Analysis = block
	st.AnalyzedText = &text
	receipt := map[string]any{"task_id": nil, "step": 1, "status": "accepted"}
	recordReceipt(st, idempotencyKey, receipt)
	if err := s.save(ctx, tx, st); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return receipt, nil
}

func extractKeywords(lower string) []string {
	stop := map[string]bool{"的": true, "了": true, "和": true, "是": true, "在": true, "有": true,
		"the": true, "and": true, "for": true, "with": true, "this": true, "that": true}
	words := strings.FieldsFunc(lower, func(r rune) bool {
		return !(r > 127 || (r >= 97 && r <= 122) || (r >= 48 && r <= 57))
	})
	seen := map[string]bool{}
	result := []string{}
	for _, word := range words {
		if len([]rune(word)) < 2 || stop[word] || seen[word] {
			continue
		}
		seen[word] = true
		result = append(result, word)
		if len(result) >= 12 {
			break
		}
	}
	return result
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ---- step 1: candidate scoring ----

// Candidates scores repositories from the project catalog and scan signals
// against the analyzed requirement keywords. The deterministic path always
// reports llm_used=false (contract Q11 honesty clause).
func (s *Service) Candidates(ctx context.Context, issueID, agentID, idempotencyKey string, limit int, entryPoint *string) (map[string]any, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	st, err := s.load(ctx, tx, issueID)
	if err != nil {
		return nil, err
	}
	if st == nil || st.Analysis == nil {
		return nil, fmt.Errorf("%w: analysis has not run", ErrConflict)
	}
	if forced, _ := st.Analysis["forced_continue"].(map[string]any); forced == nil {
		if sufficient, _ := st.Analysis["sufficient"].(bool); !sufficient {
			return nil, fmt.Errorf("%w: analysis insufficient and not force-continued", ErrConflict)
		}
	}
	if receipt, ok := replay(st, idempotencyKey); ok {
		tx.Rollback(ctx)
		return receipt, nil
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}

	// ① 候选池：**本空间已登记的全部仓库**（含扫描生成的 AutoCard），
	//    不再只是"本项目已挂的那几个"——那正是死胡同的来源。
	cards, err := s.loadRepoPool(ctx, tx, st.ProjectID)
	if err != nil {
		return nil, err
	}

	// ② 语义召回：优先用部署配置的模型做"需求 → 仓库"的语义判断
	//    （GOAI-infra-repomesh 设计里的"总 Manager 全局扫描"）。
	//    模型不可用时回退关键词，并**如实标注 llm_used=false**，不冒充模型打分。
	requirement := st.RequirementText
	if st.AnalyzedText != nil && strings.TrimSpace(*st.AnalyzedText) != "" {
		requirement = *st.AnalyzedText
	}
	rawKeywords, _ := st.Analysis["extracted_keywords"].([]any)
	keywords := toStrings(rawKeywords)
	verdicts, llmErr := s.semanticRecall(ctx, tx, requirement, cards)
	llmUsed := llmErr == nil
	llmError := ""
	if !llmUsed {
		llmError = llmErr.Error()
		verdicts = keywordVerdicts(cards, keywords, requirement)
	}

	// ③ 图推理（第二层）：用确定性依赖证据补漏报、压误报。
	verdicts, supplements, conflicts := graphAdjust(cards, verdicts)

	// ④ 组装读面。score 的语义从"关键词命中个数"改成**置信度 0..1**，
	//    分档阈值（0.7 / 0.4）与设计文档一致。
	byName := map[string]repoCard{}
	for _, card := range cards {
		byName[card.Name] = card
	}
	sort.SliceStable(verdicts, func(i, j int) bool {
		if verdicts[i].Confidence != verdicts[j].Confidence {
			return verdicts[i].Confidence > verdicts[j].Confidence
		}
		return byName[verdicts[i].Repository].InProject && !byName[verdicts[j].Repository].InProject
	})
	if len(verdicts) > limit {
		verdicts = verdicts[:limit]
	}
	blocks := []map[string]any{}
	for _, verdict := range verdicts {
		card := byName[verdict.Repository]
		isEntry := entryPoint != nil && *entryPoint == card.Name
		blocks = append(blocks, map[string]any{
			"repository_id": card.ID, "repository_name": card.Name,
			"score": verdict.Confidence, "matched_terms": verdict.Matched,
			"rationale":      verdict.Rationale,
			"is_entry_point": isEntry,
			"low_signal":     card.AutoCard == nil || card.AutoCard.LowSignal,
			"in_project":     card.InProject,
			"auto_card":      card.AutoCard != nil,
			"from_graph":     verdict.FromGraph,
			"excluded_by_graph": verdict.ExcludedByGraph,
			"graph_conflict":    verdict.ConflictsWithGraph,
		})
	}
	block := map[string]any{
		"items": blocks, "llm_used": llmUsed, "limit": limit, "entry_point": entryPoint,
		"pool_size": len(cards), "supplements": supplements, "conflicts": conflicts,
		"ran_at": time.Now().UTC(), "by_agent_id": agentID, "error": errorOrNil(llmError),
	}
	st.Candidates = block
	receipt := map[string]any{"task_id": nil, "step": 2, "status": "accepted"}
	recordReceipt(st, idempotencyKey, receipt)
	if err := s.save(ctx, tx, st); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return receipt, nil
}
