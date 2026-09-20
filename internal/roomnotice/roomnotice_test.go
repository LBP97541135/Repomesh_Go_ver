package roomnotice

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"repomesh.local/repomesh/internal/agentteams"
)

// 房间通知是观察面，不是链路的必经环节：缺配置时 NewFromEnv 必须返回 nil，
// 而不是一个"配了但投不出去"的半成品 —— 后者会让调用方每次事件都去打一个空地址。
// 这条也是它敢无条件装在 web / coordinator 里的前提。
func TestNewFromEnvIsNilWithoutConfiguration(t *testing.T) {
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

			notifier := NewFromEnv(nil)
			if got := notifier == nil; got != testCase.wantNil {
				t.Fatalf("nil=%v; want %v", got, testCase.wantNil)
			}
		})
	}
}

// Notify 的签名没有返回值，就是为了让"投不出去"不可能打断调用方。这里钉住：
// 各种残缺状态下调用都不能 panic，也不能真的发出去。
func TestNotifyIsSafeWhenIncomplete(t *testing.T) {
	var nilNotifier *Notifier
	nilNotifier.Notify(context.Background(), "iss_1", "txn", "body")

	noMatrix := &Notifier{pool: &pgxpool.Pool{}}
	noMatrix.Notify(context.Background(), "iss_1", "txn", "body")

	noPool := &Notifier{matrix: &agentteams.MatrixSession{Controller: &agentteams.Client{}}}
	noPool.Notify(context.Background(), "iss_1", "txn", "body")
}
