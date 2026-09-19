package discovery

import (
	"strings"
	"testing"

	"repomesh.local/repomesh/internal/scan"
)

// ---- 语义召回的容错解析 ----

// 模型可能把 JSON 包在 markdown fence 里、前后带解释文字，或直接给裸数组。
// 三种都要能解析出来——解析不了就回退关键词，而不是把整步判死。
func TestParseVerdictsToleratesModelFormatting(t *testing.T) {
	cases := map[string]string{
		"标准包装":     `{"candidates":[{"repository":"o/r","confidence":0.9,"rationale":"共享依赖"}]}`,
		"裸数组":      `[{"repository":"o/r","confidence":0.8,"rationale":"接口对应"}]`,
		"markdown": "```json\n{\"candidates\":[{\"repository\":\"o/r\",\"confidence\":0.7,\"rationale\":\"目录职责\"}]}\n```",
		"前后有解释":    "好的，这是我的判断：\n{\"candidates\":[{\"repository\":\"o/r\",\"confidence\":0.6,\"rationale\":\"可能涉及\"}]}\n以上。",
	}
	for name, content := range cases {
		verdicts, err := parseVerdicts(content)
		if err != nil {
			t.Fatalf("%s: 解析失败: %v", name, err)
		}
		if len(verdicts) != 1 || verdicts[0].Repository != "o/r" {
			t.Fatalf("%s: 解析结果不符: %+v", name, verdicts)
		}
	}
}

func TestParseVerdictsRejectsGarbage(t *testing.T) {
	if _, err := parseVerdicts("我无法判断"); err == nil {
		t.Fatal("非 JSON 输出必须报错，交给调用方回退关键词路径")
	}
}

// ---- 关键词回退路径 ----

// 模型不可用时的回退：在**名片全文**（含 AutoCard）上做关键词命中，
// 并且把"未使用模型"写进 rationale，绝不冒充模型打分。
func TestKeywordVerdictsUseAutoCardAndLabelFallback(t *testing.T) {
	cards := []repoCard{
		{
			ID: "1", Name: "org/order-service",
			AutoCard: &scan.AutoCard{
				TopDirs: []string{"src/main/java/order"},
				Deps:    []string{"org.services:ts-common"},
			},
		},
		{ID: "2", Name: "org/unrelated", AutoCard: &scan.AutoCard{TopDirs: []string{"docs"}}},
	}
	verdicts := keywordVerdicts(cards, []string{"order", "ts-common"}, "给 order 服务加满减活动")
	if len(verdicts) != 1 {
		t.Fatalf("只应命中一个仓库，得到 %+v", verdicts)
	}
	if verdicts[0].Repository != "org/order-service" {
		t.Fatalf("命中仓库不符: %s", verdicts[0].Repository)
	}
	if verdicts[0].Confidence < requiredBar {
		t.Fatalf("两个关键词命中应达到必改档，得到 %v", verdicts[0].Confidence)
	}
	if !strings.Contains(verdicts[0].Rationale, "未使用模型") {
		t.Fatalf("回退路径必须自报家门，得到 %q", verdicts[0].Rationale)
	}
}

// ---- 图推理（第二层） ----

func graphFixture() []repoCard {
	// checkout 依赖 shared-lib（构建期证据）→ 图上有一条 checkout → shared-lib 的边。
	return []repoCard{
		{
			ID: "checkout", Name: "org/checkout",
			AutoCard: &scan.AutoCard{
				Identities:  []string{"org.checkout:checkout"},
				DepEvidence: []scan.DepEvidence{{Name: "org.shared:shared-lib", Mechanism: scan.MechanismBuild, Confidence: scan.ConfidenceConfirmed}},
			},
		},
		{
			ID: "shared", Name: "org/shared-lib",
			AutoCard: &scan.AutoCard{Identities: []string{"org.shared:shared-lib"}},
		},
		{ID: "island", Name: "org/island", AutoCard: &scan.AutoCard{TopDirs: []string{"docs"}}},
	}
}

// 补漏报：被判必改的仓库，它的依赖要被图补进"可能"档。
func TestGraphAdjustSupplementsDependencies(t *testing.T) {
	verdicts := []llmVerdict{{Repository: "org/checkout", Confidence: 0.9, Rationale: "需求直接命中"}}
	refined, supplements, _ := graphAdjust(graphFixture(), verdicts)
	if len(supplements) != 1 || supplements[0] != "org/shared-lib" {
		t.Fatalf("依赖应被补进候选，得到 %+v", supplements)
	}
	found := false
	for _, verdict := range refined {
		if verdict.Repository == "org/shared-lib" {
			found = true
			if !verdict.FromGraph {
				t.Fatal("补充项必须标 from_graph，让界面能区分来源")
			}
		}
	}
	if !found {
		t.Fatal("补充项没有进入结果")
	}
}

// 压误报：与任何必改仓库都不相邻、置信度又不够的候选，图上没有依据，排除。
func TestGraphAdjustExcludesIsolatedLowConfidence(t *testing.T) {
	verdicts := []llmVerdict{
		{Repository: "org/checkout", Confidence: 0.9, Rationale: "需求直接命中"},
		{Repository: "org/island", Confidence: 0.5, Rationale: "可能相关"},
	}
	refined, _, _ := graphAdjust(graphFixture(), verdicts)
	for _, verdict := range refined {
		if verdict.Repository == "org/island" && !verdict.ExcludedByGraph {
			t.Fatal("孤立的低置信候选应被图标为排除")
		}
	}
}

// 记冲突：模型给高分却在图上孤立 —— 保留（不擅自推翻模型）但如实标注冲突。
func TestGraphAdjustFlagsHighConfidenceConflict(t *testing.T) {
	verdicts := []llmVerdict{
		{Repository: "org/checkout", Confidence: 0.9, Rationale: "需求直接命中"},
		{Repository: "org/island", Confidence: 0.8, Rationale: "模型认为相关"},
	}
	refined, _, conflicts := graphAdjust(graphFixture(), verdicts)
	flagged := false
	for _, verdict := range refined {
		if verdict.Repository == "org/island" {
			flagged = verdict.ConflictsWithGraph
		}
	}
	if !flagged {
		t.Fatal("高分孤立候选应被标 graph_conflict，供人复核")
	}
	if len(conflicts) != 1 || conflicts[0] != "org/island" {
		t.Fatalf("冲突清单不符: %+v", conflicts)
	}
}

// ---- 兜底：空计划的判据 ----

func TestTiersHaveSelection(t *testing.T) {
	excluded := []any{
		map[string]any{"repository": "a", "tier": "excluded"},
		map[string]any{"repository": "b", "tier": "excluded"},
	}
	if tiersHaveSelection(excluded) {
		t.Fatal("全部排除时必须判定为「没有可改动仓库」，否则会生成空计划")
	}
	withMaybe := append([]any{map[string]any{"repository": "c", "tier": "maybe"}}, excluded...)
	if !tiersHaveSelection(withMaybe) {
		t.Fatal("有「可能」档就应放行")
	}
}

// ---- 名片渲染 ----

func TestCardTextCarriesAutoCardFacts(t *testing.T) {
	card := repoCard{
		Name:        "org/checkout",
		Description: "结账服务",
		Topics:      []string{"payments"},
		AutoCard: &scan.AutoCard{
			TopDirs:       []string{"src/checkout"},
			Deps:          []string{"org.shared:shared-lib"},
			ExposedAPIs:   []string{"POST /checkout"},
			RecentCommits: []string{"feat: 满减"},
		},
	}
	text := cardText(card)
	for _, want := range []string{"org/checkout", "结账服务", "payments", "src/checkout", "shared-lib", "POST /checkout", "满减"} {
		if !strings.Contains(text, want) {
			t.Fatalf("名片文本缺少 %q：%s", want, text)
		}
	}
}

func TestAutoHasContentIgnoresEmptyCards(t *testing.T) {
	if autoHasContent(&scan.AutoCard{}) {
		t.Fatal("空卡片不应被当成有效 AutoCard")
	}
	if !autoHasContent(&scan.AutoCard{TopDirs: []string{"src"}}) {
		t.Fatal("有目录信息就是有效卡片")
	}
}
