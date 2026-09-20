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
