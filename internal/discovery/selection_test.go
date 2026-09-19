package discovery

import (
	"testing"

	"repomesh.local/repomesh/internal/scan"
)

// 「人选 → 图查漏」的核心判定：勾选的仓 forward 依赖谁而没被勾上，谁就该进
// 待确认的漏选清单；池外的依赖（外部库）不进清单；全选时清单为空。
func TestGraphSupplementsFindsForwardDependenciesOnly(t *testing.T) {
	cards := []repoCard{
		{
			ID: "r-order", Name: "org/ts-order-service",
			AutoCard: &scan.AutoCard{DepEvidence: []scan.DepEvidence{
				{Name: "ts-notification-service", Mechanism: scan.MechanismSource},
			}},
		},
		// 依赖证据写短名（真实扫描里靠声明的 identity 解析到本仓）
		{ID: "r-notify", Name: "org/ts-notification-service", AutoCard: &scan.AutoCard{
			Identities: []string{"ts-notification-service"},
		}},
		{ID: "r-other", Name: "org/ts-station-service", AutoCard: &scan.AutoCard{}},
	}
	chosen := map[string]bool{"r-order": true}
	names := map[string]string{"r-order": "org/ts-order-service"}

	got := graphSupplements(cards, chosen, names)
	if len(got) != 1 {
		t.Fatalf("want exactly the forward dependency as supplement, got %v", got)
	}
	entry := got[0].(map[string]any)
	if entry["repository_id"] != "r-notify" {
		t.Fatalf("wrong supplement: %v", entry)
	}
	if entry["via"] != "org/ts-order-service" {
		t.Fatalf("supplement must name the repository that pulled it in: %v", entry)
	}

	// 依赖已在勾选内 → 无漏选
	if got := graphSupplements(cards, map[string]bool{"r-order": true, "r-notify": true}, names); len(got) != 0 {
		t.Fatalf("nothing is missing when the dependency is already chosen, got %v", got)
	}
	// 无关仓不会被凭空拉进清单
	if got := graphSupplements(cards, map[string]bool{"r-other": true}, names); len(got) != 0 {
		t.Fatalf("unrelated repository must not produce supplements, got %v", got)
	}
}
