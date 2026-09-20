package agentteams

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// GetManager 只为拿 Manager 的房间号（实例级 agent，桥的无队兜底收件人）。
func TestGetManagerReadsRoomFromStatus(t *testing.T) {
	var gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"agt-manager","phase":"Active",
			"room":"!mGrRoom01:matrix-local.agentteams.io:18080"}`))
	}))
	defer upstream.Close()

	view, status, err := (&Client{BaseURL: upstream.URL, Token: "sa-token"}).
		GetManager(context.Background(), "agt-manager")
	if err != nil {
		t.Fatalf("GetManager: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status=%d", status)
	}
	if gotPath != "/api/v1/managers/agt-manager" {
		t.Fatalf("path=%q", gotPath)
	}
	if view.Room != "!mGrRoom01:matrix-local.agentteams.io:18080" {
		t.Fatalf("room=%q", view.Room)
	}
}
