package main

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"repomesh.local/repomesh/internal/agentteams"
)

// 房间通知是观察面，不是链路的必经环节：缺配置时 newRoomNotifier 必须返回 nil，
// 而不是一个"配了但投不出去"的半成品 —— 后者会让 coordinator 每次派发都去打一个
// 空地址。这条也是它敢无条件装在 coordinator 里的前提。
func TestNewRoomNotifierIsNilWithoutConfiguration(t *testing.T) {
	cases := []struct {
		name       string
		controller string
		homeserver string
		wantNil    bool
	}{
		{"两样都缺", "", "", true},
		{"只有控制器", "http://controller:8090", "", true},
		{"只有 homeserver", "", "http://127.0.0.1:18080", true},
		{"都配了", "http://controller:8090", "http://127.0.0.1:18080", false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("AGENTTEAMS_CONTROLLER_URL", testCase.controller)
			t.Setenv("MATRIX_HOMESERVER_URL", testCase.homeserver)

			notifier := newRoomNotifier(nil)
			if got := notifier == nil; got != testCase.wantNil {
				t.Fatalf("nil=%v; want %v", got, testCase.wantNil)
			}
		})
	}
}

// Notify 的签名没有返回值，就是为了让"投不出去"不可能打断调用方。这里把它钉住：
// 各种残缺状态下调用都不能 panic，也不能打出去。
func TestRoomNotifierNotifyIsSafeWhenIncomplete(t *testing.T) {
	var nilNotifier *roomNotifier
	nilNotifier.Notify(context.Background(), "iss_1", "txn", "body")

	noMatrix := &roomNotifier{pool: &pgxpool.Pool{}}
	noMatrix.Notify(context.Background(), "iss_1", "txn", "body")

	noPool := &roomNotifier{matrix: &agentteams.MatrixSession{Controller: &agentteams.Client{}}}
	noPool.Notify(context.Background(), "iss_1", "txn", "body")
}

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
	// 短原因原样带上，不做无谓省略。
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
