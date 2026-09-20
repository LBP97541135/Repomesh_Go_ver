package agentteams

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// GetTeam 是唯一能拿到房间号的读法：TeamRoomID / LeaderDMRoomID 在 Team CR 的
// status 里，由控制器异步建完 Matrix 房之后回填，POST /api/v1/teams 的响应里没有。
func TestGetTeamReadsRoomIDsFromStatus(t *testing.T) {
	var gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"repomesh-r-d0b23e2167cd1065","phase":"Active",
			"teamRoomID":"!iTazDQ5FurAaYP0w5o:matrix-local.agentteams.io:18080",
			"leaderDMRoomID":"!oSx8eM74D8XiMVxpVV:matrix-local.agentteams.io:18080"}`))
	}))
	defer upstream.Close()

	view, status, err := (&Client{BaseURL: upstream.URL, Token: "sa-token"}).
		GetTeam(context.Background(), "repomesh-r-d0b23e2167cd1065")
	if err != nil {
		t.Fatalf("GetTeam: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status=%d", status)
	}
	if gotPath != "/api/v1/teams/repomesh-r-d0b23e2167cd1065" {
		t.Fatalf("path=%q", gotPath)
	}
	if view.TeamRoomID != "!iTazDQ5FurAaYP0w5o:matrix-local.agentteams.io:18080" {
		t.Fatalf("teamRoomID=%q", view.TeamRoomID)
	}
	if view.LeaderDMRoomID != "!oSx8eM74D8XiMVxpVV:matrix-local.agentteams.io:18080" {
		t.Fatalf("leaderDMRoomID=%q", view.LeaderDMRoomID)
	}
}

// 房间还没建好时上游回的是没有 status 的 team —— 这不是错误，是常态。
// 调用方（repositoryteams.persistRooms）靠"读回空就不覆盖"来容忍它，所以这里
// 必须给出干净的空值 + 200，而不是让 decode 炸掉。
func TestGetTeamToleratesTeamWithoutRoomsYet(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"repomesh-r-fresh","phase":"Pending"}`))
	}))
	defer upstream.Close()

	view, status, err := (&Client{BaseURL: upstream.URL}).GetTeam(context.Background(), "repomesh-r-fresh")
	if err != nil {
		t.Fatalf("GetTeam: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status=%d", status)
	}
	if view.TeamRoomID != "" || view.LeaderDMRoomID != "" {
		t.Fatalf("want empty rooms, got %q / %q", view.TeamRoomID, view.LeaderDMRoomID)
	}
}

// 上游 404/503 时不能装作读到了房间：状态码原样回，调用方据此放弃本次回读。
func TestGetTeamSurfacesUpstreamStatus(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"controller not ready"}`))
	}))
	defer upstream.Close()

	view, status, err := (&Client{BaseURL: upstream.URL}).GetTeam(context.Background(), "repomesh-r-x")
	if err != nil {
		t.Fatalf("GetTeam: %v", err)
	}
	if status != http.StatusServiceUnavailable {
		t.Fatalf("status=%d; want 503", status)
	}
	if view != (TeamView{}) {
		t.Fatalf("view=%+v; want zero on non-200", view)
	}
}
