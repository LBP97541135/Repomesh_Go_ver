package discovery

import (
	"context"
	"fmt"
	"time"

	"repomesh.local/repomesh/internal/scan"
)

// 候选来源分流（对齐主线 2026-09-18 的用户裁定）：第 2 步在聊天室里让人选——
// 自己勾选仓库（人在前），或让模型推断（模型在前，本线的语义召回 + 图推理）。
//   人工勾选 → 第 3 步用依赖图查漏，漏选清单回聊天室让人确认；
//   模型推断 → 第 3 步走既有分档审批门。
// 图与语义召回同源：本线的依赖图是从扫描档案推断出的 scan.Graph（BuildGraph），
// 不引入 repository_dependencies 表——两条线各自成体系，这里接本线那一套。

// SelectCandidates records the human's own repository picks as the step-2
// candidates block (selection_mode=manual). Repositories must belong to the
// issue's repository pool (project directory ∩ confirmed issue scope); unknown
// ids are rejected rather than silently dropped — a typo'd scope is a fact the
// human wants to know about.
func (s *Service) SelectCandidates(ctx context.Context, issueID, agentID, idempotencyKey string, repositoryIDs []string) (map[string]any, error) {
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
	if receipt, ok := replay(st, idempotencyKey); ok {
		tx.Rollback(ctx)
		return receipt, nil
	}
	if st.Candidates != nil {
		return nil, fmt.Errorf("%w: candidates already decided", ErrConflict)
	}
	cards, err := s.loadRepoPool(ctx, tx, st.ProjectID, issueID)
	if err != nil {
		return nil, err
	}
	byID := map[string]repoCard{}
	for _, card := range cards {
		byID[card.ID] = card
	}
	items := []any{}
	for _, id := range repositoryIDs {
		card, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("%w: repository %s is not in this issue's repository scope", ErrConflict, id)
		}
		items = append(items, map[string]any{
			"repository_id": card.ID, "repository_name": card.Name,
			"score": 1.0, "matched_terms": []string{"人工勾选"},
			"rationale": "人工勾选进入交付范围", "is_entry_point": false,
			"low_signal": card.AutoCard == nil || card.AutoCard.LowSignal,
			"in_project": card.InProject, "auto_card": card.AutoCard != nil,
			"from_graph": false, "excluded_by_graph": false, "graph_conflict": false,
		})
	}
	block := map[string]any{
		"items": items, "llm_used": false, "selection_mode": "manual",
		"limit": 50, "entry_point": nil, "pool_size": len(cards),
		"supplements": []any{}, "conflicts": []any{},
		"ran_at": time.Now().UTC(), "by_agent_id": agentID, "error": nil,
	}
	st.Candidates = block
	receipt := map[string]any{"task_id": nil, "step": 2, "status": "selected", "repository_count": len(items)}
	recordReceipt(st, idempotencyKey, receipt)
	if err := s.save(ctx, tx, st); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return receipt, nil
}

// SupplementCheck runs the step-3 graph pass for a manual selection: which
// repositories does the choice depend on (forward edges of the scanned
// dependency graph) that the human did NOT pick, and that are inside the
// issue's repository pool? The missing list is persisted on the classification
// block as supplements (state pending) and echoed back for the chat card.
// No missing repos → the classification is complete and the approval is
// recorded on the spot (the human's gate was the selection).
func (s *Service) SupplementCheck(ctx context.Context, issueID, agentID, idempotencyKey string) (map[string]any, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	st, err := s.load(ctx, tx, issueID)
	if err != nil {
		return nil, err
	}
	if receipt, ok := replay(st, idempotencyKey); ok {
		tx.Rollback(ctx)
		return receipt, nil
	}
	if st == nil || st.Candidates == nil {
		return nil, fmt.Errorf("%w: no selection to check", ErrConflict)
	}
	if mode, _ := st.Candidates["selection_mode"].(string); mode != "manual" {
		return nil, fmt.Errorf("%w: supplement check only applies to manual selections", ErrConflict)
	}
	if st.Classification != nil {
		return nil, fmt.Errorf("%w: classification already decided", ErrConflict)
	}
	chosen := []any{}
	chosenIDs := map[string]bool{}
	chosenNames := map[string]string{}
	if items, ok := st.Candidates["items"].([]any); ok {
		for _, itemAny := range items {
			item, _ := itemAny.(map[string]any)
			id, _ := item["repository_id"].(string)
			if id == "" {
				continue
			}
			chosenIDs[id] = true
			name, _ := item["repository_name"].(string)
			chosenNames[id] = name
			chosen = append(chosen, requiredEntry(name, "人工勾选"))
		}
	}
	cards, err := s.loadRepoPool(ctx, tx, st.ProjectID, issueID)
	if err != nil {
		return nil, err
	}
	supplements := graphSupplements(cards, chosenIDs, chosenNames)

	block := baseClassification(chosen, agentID)
	result := map[string]any{"supplement_state": "none", "supplements": []any{}}
	if len(supplements) > 0 {
		block["supplements"] = supplements
		block["supplement_state"] = "pending"
		st.Classification = block
		st.EvidenceVersion = strPtr(newEvidenceVersion(issueID, "classification", time.Now().UTC().Format(time.RFC3339Nano)))
		receipt := map[string]any{"task_id": nil, "step": 3, "status": "supplement_pending", "missing_count": len(supplements)}
		recordReceipt(st, idempotencyKey, receipt)
		if err := s.save(ctx, tx, st); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		result["supplement_state"] = "pending"
		result["supplements"] = supplements
		return result, nil
	}
	// 没有漏选：分类即定，人选这道闸就是审批
	approveBlock(st, agentID, "人工勾选范围经依赖图核对无漏选")
	receipt := map[string]any{"task_id": nil, "step": 3, "status": "complete", "missing_count": 0}
	recordReceipt(st, idempotencyKey, receipt)
	if err := s.save(ctx, tx, st); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

// ConfirmSupplements applies the human's verdict on the missing list: the
// confirmed repositories join the required tier (marked supplemented), the
// classification completes and the approval is recorded — the confirmation
// itself was this path's human gate.
func (s *Service) ConfirmSupplements(ctx context.Context, issueID, agentID, idempotencyKey string, repositories []string) (map[string]any, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	st, err := s.load(ctx, tx, issueID)
	if err != nil {
		return nil, err
	}
	if receipt, ok := replay(st, idempotencyKey); ok {
		tx.Rollback(ctx)
		return receipt, nil
	}
	if st == nil || st.Classification == nil {
		return nil, fmt.Errorf("%w: no supplement list to confirm", ErrConflict)
	}
	if state, _ := st.Classification["supplement_state"].(string); state != "pending" {
		return nil, fmt.Errorf("%w: supplements are not pending", ErrConflict)
	}
	confirmed := map[string]bool{}
	for _, name := range repositories {
		confirmed[name] = true
	}
	sups, _ := st.Classification["supplements"].([]any)
	kept := []any{}
	for _, supAny := range sups {
		sup, _ := supAny.(map[string]any)
		name, _ := sup["repository"].(string)
		if confirmed[name] {
			kept = append(kept, supAny)
			appendRequired(st.Classification, requiredEntry(name, "依赖图补选"))
		}
	}
	st.Classification["supplements"] = kept
	st.Classification["supplement_state"] = "confirmed"
	approveBlock(st, agentID, "人工确认依赖图补选清单")
	receipt := map[string]any{"task_id": nil, "step": 3, "status": "confirmed", "confirmed_count": len(kept)}
	recordReceipt(st, idempotencyKey, receipt)
	if err := s.save(ctx, tx, st); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return receipt, nil
}

/* ── helpers ── */

// graphSupplements：勾选的仓在依赖图上 forward 依赖谁（接口要改，依赖仓多半也得
// 跟着改）而没被勾上——这些就是「漏选清单」，待人确认。池外的依赖（外部库/未扫描
// 的仓）不进清单：陌生名字进清单只会教人瞎点，缺席是诚实的数据。
//
// 抽成纯函数是为了能单测：这条判定是「人选 → 图查漏」的产品核心。
func graphSupplements(cards []repoCard, chosenIDs map[string]bool, chosenNames map[string]string) []any {
	scanCards := make([]scan.RepositoryCard, 0, len(cards))
	byID := map[string]repoCard{}
	for _, card := range cards {
		byID[card.ID] = card
		scanCards = append(scanCards, scan.RepositoryCard{
			ID: card.ID, Name: card.Name, Description: card.Description,
			Topics: card.Topics, Languages: card.Languages, AutoCard: card.AutoCard,
		})
	}
	registry := scan.BuildAliasRegistry(scanCards)
	graph := scan.BuildGraph(scanCards, registry)
	supplements := []any{}
	seen := map[string]bool{}
	for _, edge := range graph.Edges {
		if !chosenIDs[edge.FromID] || chosenIDs[edge.ToID] || seen[edge.ToID] {
			continue
		}
		card, ok := byID[edge.ToID]
		if !ok {
			continue
		}
		seen[edge.ToID] = true
		supplements = append(supplements, map[string]any{
			"repository": card.Name, "repository_id": card.ID,
			"via": chosenNames[edge.FromID], "confidence": 0.6,
			"mechanism": string(edge.Mechanism),
		})
	}
	return supplements
}

func requiredEntry(name, reason string) map[string]any {
	return map[string]any{
		"repository": name, "status": "REQUIRED", "confidence": 1.0,
		"reason": reason, "plan_summary": "", "plan": nil,
		"missing_dependencies": []string{}, "is_supplemented": false, "graph_conflict": false,
	}
}

func appendRequired(classification map[string]any, entry map[string]any) {
	entry["is_supplemented"] = true
	if req, ok := classification["required"].([]any); ok {
		classification["required"] = append(req, entry)
	}
}

func baseClassification(chosen []any, agentID string) map[string]any {
	return map[string]any{
		"required": chosen, "maybe": []any{}, "excluded": []any{},
		"supplements": []any{}, "conflicts": []any{}, "observations": []any{},
		"adjustments": []any{}, "supplement_state": "none",
		"ran_at": time.Now().UTC(), "by_agent_id": agentID, "error": nil,
	}
}

func approveBlock(st *State, agentID, reason string) {
	if st.Approval == nil {
		st.Approval = map[string]any{}
	}
	st.Approval["state"] = "approved"
	st.Approval["decided_by_agent_id"] = agentID
	st.Approval["reason"] = reason
	st.Approval["decided_at"] = time.Now().UTC()
	st.Approval["evidence_version"] = nil
	recomputeTiers(st)
}

// recomputeTiers rebuilds effective_tiers from the required tier.
func recomputeTiers(st *State) {
	tiers := []any{}
	if st.Classification != nil {
		if req, ok := st.Classification["required"].([]any); ok {
			for _, entryAny := range req {
				entry, _ := entryAny.(map[string]any)
				name, _ := entry["repository"].(string)
				tiers = append(tiers, map[string]any{
					"repository": name, "tier": "required", "adjusted": false, "original_tier": nil,
				})
			}
		}
	}
	st.EffectiveTiers = tiers
	if st.EvidenceVersion != nil {
		st.EvidenceVersion = strPtr(newEvidenceVersion(st.IssueID, "classification", time.Now().UTC().Format(time.RFC3339Nano)))
	}
}

func strPtr(s string) *string { return &s }
