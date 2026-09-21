package main

import (
	"strings"
	"testing"
)

// 失败原因里可能带 agent 的 stderr 尾巴。房间里一条消息不该变成一面墙 ——
// 判据是"不随输入增长"，不是某个具体字数。
func TestPlanningFailedNoticeTruncatesReason(t *testing.T) {
	long := planningFailedNotice(1, strings.Repeat("错", 900))
	longer := planningFailedNotice(1, strings.Repeat("错", 9000))
	if len([]rune(long)) != len([]rune(longer)) {
		t.Fatalf("notice grows with the reason: %d runes vs %d runes",
			len([]rune(long)), len([]rune(longer)))
	}
	if !strings.Contains(long, "…") {
		t.Fatal("truncated notice should say it was cut")
	}
	short := planningFailedNotice(3, "agent 进程未正常结束（exit=1）")
	if !strings.Contains(short, "exit=1") {
		t.Fatalf("short reason lost: %q", short)
	}
	if strings.Contains(short, "…") {
		t.Fatalf("short reason should not be truncated: %q", short)
	}
}

// 三条通知都要能被读成一句话：带步骤号、单行、有明显的来源前缀（这些消息的发送者
// 是 RepoMesh 自己的身份，不是某个 agent，所以正文必须自报家门）。
func TestPlanningNoticesCarryStepAndSource(t *testing.T) {
	notices := map[string]string{
		"派发": planningDispatchedNotice(2, "organization_leader"),
		"完成": planningCompletedNotice(2, "organization_leader"),
		"失败": planningFailedNotice(2, "产物不合格"),
	}
	for name, notice := range notices {
		if !strings.Contains(notice, "第 2 步") {
			t.Errorf("%s 通知缺少步骤号：%q", name, notice)
		}
		if !strings.HasPrefix(notice, "【RepoMesh】") {
			t.Errorf("%s 通知没有来源前缀：%q", name, notice)
		}
		if strings.Contains(notice, "\n") {
			t.Errorf("%s 通知不该是多行：%q", name, notice)
		}
	}
}

// 门事件文案：谁选的必须说清楚（人勾的与 AI 定的在审计上不是一回事），
// 查漏只说数量+首个仓名，不擅自改范围。
func TestGateNotices(t *testing.T) {
	opened := gateOpenedNotice(5)
	if !strings.Contains(opened, "选仓门已开") || !strings.Contains(opened, "5") {
		t.Fatalf("opened notice: %q", opened)
	}
	audit := gateAuditNotice([]string{"acme/billing", "acme/notify", "acme/pay"})
	if !strings.Contains(audit, "acme/billing") || !strings.Contains(audit, "3") {
		t.Fatalf("audit notice should name the first repo and the count: %q", audit)
	}
	if !strings.Contains(audit, "请确认补不补") {
		t.Fatalf("audit must stay a prompt, not a decision: %q", audit)
	}
}

// gateSuggested 对两种产物形状各取各的字段；取不到返回空（宁缺勿编）。
func TestGateSuggested(t *testing.T) {
	candidates := map[string]any{"items": []any{
		map[string]any{"repository_name": "acme/checkout"},
		map[string]any{"repository_name": "acme/shared-lib"},
	}}
	if got := gateSuggested(candidates); len(got) != 2 || got[0] != "acme/checkout" {
		t.Fatalf("candidates: %v", got)
	}
	audit := map[string]any{"missing": []any{map[string]any{"repository": "acme/billing"}}}
	if got := gateSuggested(audit); len(got) != 1 || got[0] != "acme/billing" {
		t.Fatalf("audit: %v", got)
	}
	if got := gateSuggested(map[string]any{}); got != nil {
		t.Fatalf("empty artifact must yield nil, got %v", got)
	}
}
