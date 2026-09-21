package skill

import (
	"strings"
	"testing"
)

// TestCleanGateMessageSaysWhatToDoNext 钉住用户 2026-09-21 报的第二件事：
// 「信息显示的也不明确」。
//
// 此前只有一句英文：
//
//	clean gate not satisfied for skill <uuid>: need at least one pass and zero fails in history
//
// 既没说**是哪个版本**（给的是 skill 的 uuid，而用户点的是某个版本），也没说
// **卡在哪几条证据**上，人拿到它不知道该改什么。现在三件事都要写清楚。
func TestCleanGateMessageSaysWhatToDoNext(t *testing.T) {
	// 有失败：必须点名版本号、失败条数，并给出可执行的下一步（登记新版本）。
	withFail := cleanGateMessage("1.2.0", 1, 1)
	for _, want := range []string{"1.2.0", "失败", "登记新版本"} {
		if !strings.Contains(withFail, want) {
			t.Errorf("有失败时的话里缺少 %q：%s", want, withFail)
		}
	}
	if strings.Contains(withFail, "skill_gate_failed") {
		t.Errorf("不该把内部错误码甩给用户：%s", withFail)
	}

	// 完全没有证据：说清"先跑一次 A/B"，而不是含糊地说"条件不满足"。
	empty := cleanGateMessage("1.3.0", 0, 0)
	for _, want := range []string{"1.3.0", "A/B"} {
		if !strings.Contains(empty, want) {
			t.Errorf("没有证据时的话里缺少 %q：%s", want, empty)
		}
	}

	// 版本号不能为空 —— 报错里只给 uuid 正是"看不出是哪个版本"的根源。
	if strings.Contains(cleanGateMessage("1.2.0", 1, 1), "<uuid>") {
		t.Error("不该出现占位符")
	}
}

// canaryGateMessage 与 cleanGateMessage 同一取向：说清哪个版本、卡在哪、下一步。
func TestCanaryGateMessageSaysWhatToDoNext(t *testing.T) {
	empty := canaryGateMessage("2.0.0", 0, 0)
	for _, want := range []string{"2.0.0", "A/B"} {
		if !strings.Contains(empty, want) {
			t.Errorf("灰度窗口无证据时缺少 %q：%s", want, empty)
		}
	}
	// 灰度窗口内出现失败：按设计应当已自动回滚，还能点到晋升就是状态不一致 ——
	// 如实说不一致，不要编一句"缺 X"糊过去。
	withFail := canaryGateMessage("2.0.0", 0, 1)
	if !strings.Contains(withFail, "不一致") {
		t.Errorf("灰度窗口内有失败时应当如实说状态不一致：%s", withFail)
	}
}
