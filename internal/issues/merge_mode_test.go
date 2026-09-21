package issues

import (
	"strings"
	"testing"
)

// TestMergeModeDefaultsToManualAndRejectsUnknown 钉住「合并方式」这条新输入的三件事：
//  1. **缺省 manual** —— 合并是整条链上唯一的外部副作用（真动用户仓库、真进主分支），
//     没明说要自动合并的，就不替任何人合；
//  2. 显式 auto / manual 原样收下；
//  3. 不认识的取值一律 422，不静默降级成某个默认值（那样人会以为自己选上了）。
func TestMergeModeDefaultsToManualAndRejectsUnknown(t *testing.T) {
	base := `"expectedCreationContextRevision":"ctx-1","title":"t","description":"d","conversation":{"mode":"new"}`
	cases := []struct {
		name    string
		body    string
		want    string
		wantErr bool
	}{
		{"缺省（不再兜底，留空交给卡点派生）", `{` + base + `}`, "", false},
		{"显式自动", `{` + base + `,"mergeMode":"auto"}`, "auto", false},
		{"显式人工", `{` + base + `,"mergeMode":"manual"}`, "manual", false},
		{"非法取值", `{` + base + `,"mergeMode":"yes"}`, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			input, err := parseNewInput([]byte(c.body))
			if c.wantErr {
				if err == nil {
					t.Fatalf("非法 mergeMode 必须被拒，却收下了 %q", input.mergeMode)
				}
				if !strings.Contains(err.Error(), "mergeMode") {
					t.Fatalf("错误里必须点名是哪个字段，got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if input.mergeMode != c.want {
				t.Fatalf("mergeMode = %q, want %q", input.mergeMode, c.want)
			}
		})
	}
}

// TestMergeModeOrDefaultFallsBackToManual 钉住落库前的兜底：任何非 auto/manual
// 的值都收敛到 manual，绝不悄悄变成 auto。
func TestMergeModeOrDefaultFallsBackToManual(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"auto", "auto"},
		{"manual", "manual"},
		{"", "manual"},
		{"AUTO", "manual"},
		{"automatic", "manual"},
	} {
		if got := mergeModeOrDefault(c.in); got != c.want {
			t.Fatalf("mergeModeOrDefault(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
