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
	keywords, _ := st.Analysis["extracted_keywords"].([]any)
	query := "SELECT r.id, r.owner || '/' || r.name," +
		" COALESCE(s.description, ''), COALESCE(s.topics::text, ''), COALESCE(s.languages::text, '')" +
		" FROM repomesh_projects.repositories r" +
		" LEFT JOIN repomesh_scan.repositories s ON s.url LIKE '%' || r.owner || '/' || r.name || '%'" +
		" WHERE r.id IN (SELECT repository_id FROM repomesh_projects.project_repositories WHERE project_id=$1)"
	rows, err := tx.Query(ctx, query, st.ProjectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type scored struct {
		id, name, rationale string
		score               float64
		terms               []string
		signals             bool
	}
	items := []scored{}
	prefix := ""
	if len(st.RequirementText) > 0 {
		prefix = strings.ToLower(st.RequirementText[:minInt(40, len(st.RequirementText))])
	}
	for rows.Next() {
		var id, full, description, topics, languages string
		if err := rows.Scan(&id, &full, &description, &topics, &languages); err != nil {
			return nil, err
		}
		haystack := strings.ToLower(full + " " + description + " " + topics + " " + languages)
		signals := strings.TrimSpace(description+topics+languages) != ""
		sc := scored{id: id, name: full, signals: signals}
		matched := map[string]bool{}
		for _, keywordAny := range keywords {
			keyword, _ := keywordAny.(string)
			if keyword != "" && strings.Contains(haystack, strings.ToLower(keyword)) {
				sc.score += 1
				matched[keyword] = true
			}
		}
		for term := range matched {
			sc.terms = append(sc.terms, term)
		}
		if prefix != "" && strings.Contains(haystack, prefix) {
			sc.score += 0.5
		}
		parts := strings.Join(sc.terms, ", ")
		signalWord := "来自扫描档案"
		if !signals {
			signalWord = "缺失，分数为低信任猜测"
		}
		sc.rationale = fmt.Sprintf("关键词命中 [%s]，信号%s", parts, signalWord)
		items = append(items, sc)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].score > items[j].score })
	if len(items) > limit {
		items = items[:limit]
	}
	blocks := []map[string]any{}
	for _, item := range items {
		isEntry := entryPoint != nil && *entryPoint == item.name
		blocks = append(blocks, map[string]any{
			"repository_id": item.id, "repository_name": item.name,
			"score": item.score, "matched_terms": item.terms, "rationale": item.rationale,
			"is_entry_point": isEntry, "low_signal": !item.signals,
		})
	}
	block := map[string]any{
		"items": blocks, "llm_used": false, "limit": limit, "entry_point": entryPoint,
		"ran_at": time.Now().UTC(), "by_agent_id": agentID, "error": nil,
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
