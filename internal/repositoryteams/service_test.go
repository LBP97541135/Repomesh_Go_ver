package repositoryteams

import (
	"context"
	"testing"

	"repomesh.local/repomesh/internal/agentteams"
)

// 缺配置时一个远端请求都不该发出去。BackfillRooms 由组合根的低频循环周期调用，
// 所以"没配就打远端"会被放大成周期性噪声。
func TestBackfillRoomsIsNoOpWithoutPoolOrClient(t *testing.T) {
	cases := []struct {
		name    string
		service *Service
	}{
		{"没有池", &Service{client: &agentteams.Client{BaseURL: "http://controller:8090"}}},
		{"没有控制器客户端", &Service{}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			filled, err := testCase.service.BackfillRooms(context.Background())
			if err != nil {
				t.Fatalf("BackfillRooms: %v", err)
			}
			if filled != 0 {
				t.Fatalf("filled=%d; want 0", filled)
			}
		})
	}
}

// persistRooms 同理：没有控制器客户端时不能假装"回读过了"，更不能去碰事务。
// 这里传 nil 事务正是为了钉住"客户端为空时提前返回"这条顺序。
// 0057 起归属是 (项目, 仓库)，所以第一个参数是项目。
func TestPersistRoomsIsNoOpWithoutClient(t *testing.T) {
	if err := (&Service{}).persistRooms(context.Background(), nil, "proj-1", "repo-1", "team-1"); err != nil {
		t.Fatalf("persistRooms: %v", err)
	}
}
