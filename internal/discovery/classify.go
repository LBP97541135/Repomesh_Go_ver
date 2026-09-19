package discovery

import (
	"context"
	"fmt"
	"strings"
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
		// score 的语义已从"关键词命中个数"改成**置信度 0..1**（模型语义分，
		// 或模型不可用时的关键词回退分）。阈值与 GOAI-infra-repomesh 的最终
		// 设计一致：≥0.7 必改、≥0.4 可能、其余排除。
		score, _ := item["score"].(float64)
		lowSignal, _ := item["low_signal"].(bool)
		fromGraph, _ := item["from_graph"].(bool)
		agentTier, _ := item["agent_tier"].(string)
		excludedByGraph, _ := item["excluded_by_graph"].(bool)
		conflictsWithGraph, _ := item["graph_conflict"].(bool)
		name, _ := item["repository_name"].(string)
		rationale, _ := item["rationale"].(string)
		if rationale == "" {
			rationale = "无判断依据"
		}
		// 2026-09-20：候选分档现在**由 agent 给出**（artifact 的 tier →
		// item.agent_tier）。这里直接采用它的判断，不再用分数阈值二次推断 ——
		// 二次推断等于把 agent 的结论又算一遍，还会悄悄把它改掉。
		// 阈值只在 agent 没给档位时作为回退（老数据 / 回退路径）。
		status := ""
		switch strings.ToUpper(strings.TrimSpace(agentTier)) {
		case "REQUIRED", "MAYBE", "EXCLUDED":
			status = strings.ToUpper(strings.TrimSpace(agentTier))
		}
		reason := rationale
		if status == "" {
			status = "EXCLUDED"
			switch {
			case excludedByGraph:
				reason = rationale + "；依赖图上与任何必改仓库都不相邻，且置信度不足"
			case fromGraph:
				// 图推理补进来的（依赖/被依赖），按"可能"档纳入。
				status = "MAYBE"
			case score >= requiredBar:
				status = "REQUIRED"
			case score >= maybeBar:
				status = "MAYBE"
			default:
				reason = rationale + "；置信度低于纳入门槛"
			}
		} else if reason == "" {
			reason = "由组织 Leader 分档"
		}
		if conflictsWithGraph {
			reason += "（注意：依赖图上孤立，与其它必改仓库没有依赖关系，建议复核）"
		}
		if lowSignal {
			reason += "（扫描信号不足）"
		}
		entry := map[string]any{
			"repository": name, "status": status, "confidence": score, "reason": reason,
			"plan_summary": "", "plan": nil, "missing_dependencies": []string{},
			"is_supplemented": fromGraph, "graph_conflict": conflictsWithGraph,
		}
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
	names := []string{}
	// 2026-09-20 线上实测：这里此前把**排除档也算进范围校验**，于是一个越界的
	// 候选（agent 顺手给"本项目里但不在本 issue 范围"的仓库打了个 excluded）
	// 会让整步 ③ 以「仓库不在此 Issue 已确认的项目范围内」失败 —— 而"排除它"
	// 恰恰就是正确的结论。范围校验只对**真会被改动的那两档**有意义；排除档
	// 不进计划、不进任务，越界也无害。required/maybe 越界仍然照旧拦下。
	for _, group := range [][]map[string]any{required, maybe} {
		for _, entry := range group {
			names = append(names, entry["repository"].(string))
		}
	}
	if err := validateRepositories(ctx, tx, st, names); err != nil {
		return nil, err
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
