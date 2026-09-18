package discovery

import (
	"context"
	"fmt"
	"time"
)

// ---- step 2: three-tier classification ----

// Classification partitions the candidates into required / maybe / excluded
// tiers deterministically: score above the required bar and non-empty
// signals -> REQUIRED, any positive score -> MAYBE, zero or absent -> EXCLUDED.
func (s *Service) Classification(ctx context.Context, issueID, agentID, idempotencyKey string) (map[string]any, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	st, err := s.load(ctx, tx, issueID)
	if err != nil {
		return nil, err
	}
	if st == nil || st.Candidates == nil {
		return nil, fmt.Errorf("%w: candidates have not run", ErrConflict)
	}
	if receipt, ok := replay(st, idempotencyKey); ok {
		tx.Rollback(ctx)
		return receipt, nil
	}
	itemsAny, _ := st.Candidates["items"].([]any)
	required := []map[string]any{}
	maybe := []map[string]any{}
	excluded := []map[string]any{}
	for _, itemAny := range itemsAny {
		item, ok := itemAny.(map[string]any)
		if !ok {
			continue
		}
		score, _ := item["score"].(float64)
		lowSignal, _ := item["low_signal"].(bool)
		name, _ := item["repository_name"].(string)
		id, _ := item["repository_id"].(string)
		terms, _ := item["matched_terms"].([]any)
		termStrings := []string{}
		for _, termAny := range terms {
			if term, ok := termAny.(string); ok {
				termStrings = append(termStrings, term)
			}
		}
		reason := "关键词信号不足以纳入"
		status := "EXCLUDED"
		confidence := 0.2
		if score >= 2 && !lowSignal {
			status = "REQUIRED"
			confidence = 0.9
			reason = fmt.Sprintf("命中 %d 个关键词且信号充分", len(termStrings))
		} else if score >= 1 {
			status = "MAYBE"
			confidence = 0.5
			reason = fmt.Sprintf("命中 %d 个关键词", len(termStrings))
		}
		entry := map[string]any{
			"repository": name, "status": status, "confidence": confidence, "reason": reason,
			"plan_summary": "", "plan": nil, "missing_dependencies": []string{},
			"is_supplemented": false, "graph_conflict": false,
		}
		_ = id
		switch status {
		case "REQUIRED":
			required = append(required, entry)
		case "MAYBE":
			maybe = append(maybe, entry)
		default:
			excluded = append(excluded, entry)
		}
	}
	block := map[string]any{
		"required": required, "maybe": maybe, "excluded": excluded,
		"supplements": []any{}, "conflicts": []any{}, "observations": []any{},
		"adjustments": []any{}, "ran_at": time.Now().UTC(), "by_agent_id": agentID, "error": nil,
	}
	st.Classification = block
	version := newEvidenceVersion(issueID, "classification", time.Now().UTC().Format(time.RFC3339Nano))
	st.EvidenceVersion = &version
	tiers := []any{}
	for _, entry := range required {
		entry["adjusted"] = false
		entry["original_tier"] = nil
		tiers = append(tiers, map[string]any{"repository": entry["repository"], "tier": "required", "adjusted": false, "original_tier": nil})
	}
	for _, entry := range maybe {
		tiers = append(tiers, map[string]any{"repository": entry["repository"], "tier": "maybe", "adjusted": false, "original_tier": nil})
	}
	for _, entry := range excluded {
		tiers = append(tiers, map[string]any{"repository": entry["repository"], "tier": "excluded", "adjusted": false, "original_tier": nil})
	}
	st.EffectiveTiers = tiers
	receipt := map[string]any{"task_id": nil, "step": 3, "status": "accepted"}
	recordReceipt(st, idempotencyKey, receipt)
	if err := s.save(ctx, tx, st); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return receipt, nil
}
