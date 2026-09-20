package scm

import "testing"

// ciPassed 是合并闸门里**唯一**决定 CI 那一门开不开的函数。
//
// 2026-09-21：它替换掉了一段死代码（旧判定在只取 status='recorded' 的结果集上
// 找 "fail"，永不成立），所以这里把语义钉死：**只认通过，其余一律不过**。
// 尤其是"从未有过 ci 事件"（空串）必须是 false —— 那正是线上 saleor 那条
// 冻结变更集的处境，闸门当时正确地关着，修完之后必须仍然关着。
func TestCIPassedIsFailClosed(t *testing.T) {
	cases := []struct {
		status string
		want   bool
	}{
		{"recorded", true},
		{"passed", true},
		{"success", true},
		{"SUCCESS", true},
		{" succeeded ", true},
		{"failed", false},
		{"FAILED", false},
		{"", false},        // 从未有过 ci 事件
		{"queued", false},  // 跑着不算过
		{"weird", false},   // 不认识的态 → 不过（fail-closed）
	}
	for _, c := range cases {
		if got := ciPassed(c.status); got != c.want {
			t.Errorf("ciPassed(%q) = %v, want %v", c.status, got, c.want)
		}
	}
}
