package web

import (
	"testing"

	"repomesh.local/repomesh/internal/issues"
)

func roomPointer(roomID string) *string { return &roomID }

// 房间号是上游的不透明 id：凭一个 id 就能读任意房间等于绕过项目边界，
// 所以这道闸必须挡住"这个 issue 关联不到的房间"。
func TestRoomBelongsToIssueAllowsOnlyAssociatedRooms(t *testing.T) {
	view := issues.RoomsView{
		IssueID: "iss_1",
		Main:    issues.RoomObservation{RoomID: roomPointer("!main:hs")},
		Leaders: []any{
			issues.RepositoryRoom{RepositoryID: "owner/other", RoomID: roomPointer("!leader:hs")},
		},
	}

	cases := []struct {
		name   string
		roomID string
		want   bool
	}{
		{"主房", "!main:hs", true},
		{"leader 房", "!leader:hs", true},
		{"别的房间", "!someone-else:hs", false},
		{"空串", "", false},
	}
	for _, testCase := range cases {
		if got := roomBelongsToIssue(view, testCase.roomID); got != testCase.want {
			t.Errorf("%s: roomBelongsToIssue(%q)=%v; want %v", testCase.name, testCase.roomID, got, testCase.want)
		}
	}
}

// 没有房间时（团队还没建 / 还没回读到）不能放行任何房间号 —— 尤其不能因为
// main.RoomID 是 nil 就把 nil 和空串当成"匹配"。
func TestRoomBelongsToIssueRejectsWhenNoRoomsAssociated(t *testing.T) {
	view := issues.RoomsView{
		IssueID: "iss_1",
		Main:    issues.RoomObservation{Availability: "unavailable", Reason: "NOT_ASSOCIATED"},
		Leaders: []any{},
	}
	if roomBelongsToIssue(view, "!any:hs") {
		t.Fatal("no associated room must not authorize any room id")
	}
}

// leaders[] 是 []any，实现可能是别的形状（历史数据/未来扩展）。认不出来的条目
// 只能当作"不放行"，不能 panic。
func TestRoomBelongsToIssueToleratesUnknownLeaderShape(t *testing.T) {
	view := issues.RoomsView{
		IssueID: "iss_1",
		Leaders: []any{map[string]string{"roomId": "!some:hs"}},
	}
	if roomBelongsToIssue(view, "!some:hs") {
		t.Fatal("an unrecognised leader entry must not authorize a room")
	}
}
