package discovery

import (
	"sort"

	"repomesh.local/repomesh/internal/scan"
)

// graphAdjust 用**扫描产出的依赖图**精化召回结果。这是 GOAI-infra-repomesh
// 设计里的"第二层图推理"：模型负责高 Recall 的语义召回，图负责用确定性证据
// 补漏报与压误报——因为"某仓库是否与需求相关"需要全局视角，单靠 prompt 调优
// 做不好（设计文档 V7 的结论）。
//
// 三条规则：
//
//	补漏报：被判 REQUIRED 的仓库，它的**直接依赖**（接口要改）与**直接被依赖者**
//	        （调用方要跟着改）补进 MAYBE，并标注 supplemented_by_graph。
//	压误报：与任何 REQUIRED 仓库之间**都没有图边**、且置信度 < 0.7 的候选降为排除。
//	记冲突：模型给 ≥0.7 却在图上完全孤立 → 保留（不擅自推翻模型），但打上
//	        graph_conflict=true 供人复核——这正是设计里"图上冲突"该有的语义。
func graphAdjust(cards []repoCard, verdicts []llmVerdict) (refined []llmVerdict, supplements []string, conflicts []string) {
	if len(verdicts) == 0 {
		return verdicts, nil, nil
	}
	scanCards := make([]scan.RepositoryCard, 0, len(cards))
	byName := map[string]repoCard{}
	for _, card := range cards {
		byName[card.Name] = card
		scanCards = append(scanCards, scan.RepositoryCard{
			ID: card.ID, Name: card.Name, Description: card.Description,
			Topics: card.Topics, Languages: card.Languages, AutoCard: card.AutoCard,
		})
	}
	registry := scan.BuildAliasRegistry(scanCards)
	graph := scan.BuildGraph(scanCards, registry)

	requiredIDs := map[string]bool{}
	idByName := map[string]string{}
	for _, card := range cards {
		idByName[card.Name] = card.ID
	}
	for _, verdict := range verdicts {
		if verdict.Confidence >= requiredBar {
			if id, ok := idByName[verdict.Repository]; ok {
				requiredIDs[id] = true
			}
		}
	}

	// 与任一 required 仓库相邻的节点（正反两个方向）。
	adjacent := map[string]bool{}
	neighbours := map[string]map[string]bool{}
	for _, edge := range graph.Edges {
		if neighbours[edge.FromID] == nil {
			neighbours[edge.FromID] = map[string]bool{}
		}
		if neighbours[edge.ToID] == nil {
			neighbours[edge.ToID] = map[string]bool{}
		}
		neighbours[edge.FromID][edge.ToID] = true
		neighbours[edge.ToID][edge.FromID] = true
	}
	for requiredID := range requiredIDs {
		for other := range neighbours[requiredID] {
			adjacent[other] = true
		}
	}

	seen := map[string]bool{}
	for _, verdict := range verdicts {
		id := idByName[verdict.Repository]
		switch {
		case requiredIDs[id] && len(neighbours[id]) == 0 && len(requiredIDs) > 1:
			// 多个 required 里有一个在图上**完全孤立**（一个邻居都没有）：
			// 保留（不擅自推翻模型）但如实标注冲突，供人复核。
			// 注意判据是"自己没有邻居"，不是"自己不在别人的邻居表里"——
			// 后者会把"有依赖但没人依赖它"的正常仓库误判成冲突。
			verdict.ConflictsWithGraph = true
			conflicts = append(conflicts, verdict.Repository)
		case !requiredIDs[id] && !adjacent[id] && verdict.Confidence < requiredBar:
			// 既不与任何 required 相邻，置信度也不够 → 图上无依据，压掉。
			verdict.ExcludedByGraph = true
		}
		seen[verdict.Repository] = true
		refined = append(refined, verdict)
	}

	// 补漏报：required 的邻居补进 MAYBE（模型没提也算，但要标出来源）。
	for id := range adjacent {
		name := ""
		for _, card := range cards {
			if card.ID == id {
				name = card.Name
				break
			}
		}
		if name == "" || seen[name] {
			continue
		}
		refined = append(refined, llmVerdict{
			Repository: name,
			Confidence: maybeBar,
			Rationale:  "依赖图补充：与已判定必须改动的仓库存在直接依赖/被依赖关系",
			FromGraph:  true,
		})
		supplements = append(supplements, name)
		seen[name] = true
	}
	sort.Strings(supplements)
	sort.Strings(conflicts)
	return refined, supplements, conflicts
}

// 与 GOAI-infra-repomesh 设计一致的分档阈值：≥0.7 必改、≥0.4 可能、其余排除。
const (
	requiredBar = 0.7
	maybeBar    = 0.4
)

// keywordVerdicts 是模型不可用时的确定性回退路径：在**名片全文**（含 AutoCard）
// 上做关键词命中。回退路径会被如实标注 llm_used=false，绝不冒充模型打分。
func keywordVerdicts(cards []repoCard, keywords []string, requirement string) []llmVerdict {
	prefix := ""
	lowered := []rune(requirement)
	if len(lowered) > 0 {
		n := 40
		if len(lowered) < n {
			n = len(lowered)
		}
		prefix = string(lowered[:n])
	}
	verdicts := []llmVerdict{}
	for _, card := range cards {
		text := haystack(card)
		matched := []string{}
		for _, keyword := range keywords {
			if keyword == "" {
				continue
			}
			if containsFold(text, keyword) {
				matched = append(matched, keyword)
			}
		}
		confidence := 0.0
		switch {
		case len(matched) >= 2:
			confidence = 0.8
		case len(matched) == 1:
			confidence = 0.5
		}
		if prefix != "" && containsFold(text, prefix) && confidence < 0.5 {
			confidence = 0.5
		}
		if confidence <= 0 {
			continue
		}
		verdicts = append(verdicts, llmVerdict{
			Repository: card.Name,
			Confidence: confidence,
			Rationale:  "关键词回退：命中 [" + joinStrings(matched, ", ") + "]（未使用模型）",
			Matched:    matched,
		})
	}
	return verdicts
}
