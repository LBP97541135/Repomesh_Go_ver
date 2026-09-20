package web

import "testing"

// 进房那句"收到新需求"的三条边界：空正文如实说空、超长截断带省略号、
// 只取第一行（需求正文常是整份文档，往房间里倒一面墙没有意义）。
func TestRequirementOpeningNotice(t *testing.T) {
	if got := requirementOpeningNotice(nil); got != "【RepoMesh】收到新需求（正文为空，见 issue 页）。" {
		t.Fatalf("empty: %q", got)
	}
	long := requirementOpeningNotice([]byte(`{"description":"` + repeat("需", 300) + `"}`))
	if want := "【RepoMesh】收到新需求：" + repeat("需", 80) + "…"; long != want {
		t.Fatalf("long: got %d runes, want %d", len([]rune(long)), len([]rune(want)))
	}
	multi := requirementOpeningNotice([]byte(`{"description":"第一行\n第二行\n第三行"}`))
	if multi != "【RepoMesh】收到新需求：第一行" {
		t.Fatalf("first line only: %q", multi)
	}
}

func repeat(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}
