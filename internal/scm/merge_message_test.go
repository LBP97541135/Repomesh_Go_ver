package scm

import (
	"strings"
	"testing"
)

// TestMergeGateMessageNamesTheMissingGates 钉住用户 2026-09-21 报的那件事：
// 「合并失败不告诉缺哪一门」。
//
// Merge 本来就算得出闸门四项的真值，但那句话此前只进了服务端日志 ——
// web 层认不出裸 fmt.Errorf，统一降级成 503 RESULT_UNCONFIRMED +
// 写死的 "The request could not be completed."，用户拿到的是一个没有信息量的 503。
//
// 这条用例守的是**翻译本身**：缺哪一项就要点名哪一项，缺两项就要点两项。
func TestMergeGateMessageNamesTheMissingGates(t *testing.T) {
	cases := []struct {
		name  string
		gate  MergeGate
		wants []string
	}{
		{
			name:  "只缺 CI",
			gate:  MergeGate{Pushed: true, PR: true, CIPassed: false, Reviewed: true},
			wants: []string{"CI"},
		},
		{
			name:  "缺 PR 与评审",
			gate:  MergeGate{Pushed: true, PR: false, CIPassed: true, Reviewed: false},
			wants: []string{"PR", "评审"},
		},
		{
			name:  "四门全缺",
			gate:  MergeGate{},
			wants: []string{"push", "PR", "CI", "评审"},
		},
	}
	for _, tc := range cases {
		got := mergeGateMessage(tc.gate)
		for _, want := range tc.wants {
			if !strings.Contains(got, want) {
				t.Errorf("%s：缺 %s，但那句话里没点名它 —— 用户看不出该补哪一门。got=%q",
					tc.name, want, got)
			}
		}
	}
	// 反向：只缺 CI 时不该把不缺的也说成缺（那是另一种撒谎）。
	onlyCI := mergeGateMessage(MergeGate{Pushed: true, PR: true, Reviewed: true})
	if strings.Contains(onlyCI, "评审") || strings.Contains(onlyCI, "push") {
		t.Errorf("只缺 CI 却把不缺的也说成缺：%q", onlyCI)
	}
}

// 四项都真却判未开：这是**不该发生**的状态。如实说"不一致、去排查"，
// 不要为了凑一句"缺 X"而编一个出来。
func TestMergeGateMessageDoesNotInventAMissingGate(t *testing.T) {
	got := mergeGateMessage(MergeGate{Pushed: true, PR: true, CIPassed: true, Reviewed: true})
	if !strings.Contains(got, "不一致") {
		t.Errorf("四项都齐却未开时应当如实说判定不一致，而不是编一个缺失项：%q", got)
	}
	if strings.Contains(got, "缺：") {
		t.Errorf("四项都齐时不该出现「缺：」这种点名：%q", got)
	}
}

// Failure 必须能带住底层原因（进日志），但**不能**把它混进给用户看的那句话。
func TestFailureKeepsCauseOutOfMessage(t *testing.T) {
	failure := &Failure{
		Status:  502,
		Code:    "MERGE_REJECTED",
		Message: "GitHub 拒绝了这次合并（可能是冲突或分支保护）。",
		Cause:   errString("boom: internal detail"),
	}
	if failure.Error() == failure.Message {
		t.Error("Error() 应当带上 cause（供服务端日志排障），否则日志里查不到根因")
	}
	if strings.Contains(failure.Message, "internal detail") {
		t.Error("给用户看的那句话里混进了底层错误原文")
	}
}

type errString string

func (e errString) Error() string { return string(e) }
