package agentteams

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// ManagerView is the subset of one AgentTeams Manager we read back.
//
// Manager 是**实例级**协调 agent（一个 AgentTeams 实例一个，不是每队/每 issue
// 一个）——多 issue 并行时人的消息不能都涌进它的房间（会串台，2026-09-20 双向
// 桥对抗性审查的 ③）。桥的默认收件人是**队房**（TeamRoomID，按 team 隔离），
// ManagerView.Room 只在无队兜底时使用。
//
// ponytail: Room 的 json 名"room"按调研文档的 status 字段表
// （room/version/welcomeSent）先写；首次真实负载到手后核对，不对就改这一处。
type ManagerView struct {
	Name  string `json:"name"`
	Phase string `json:"phase"`
	// Room 是 Manager 的 Matrix 房间号（status 里，controller 协调完成后才有值；
	// 为空 = 还没就绪，别拿它发消息）。
	Room string `json:"room"`
}

// GetManager reads one manager back (GET /api/v1/managers/{name}).
func (c *Client) GetManager(ctx context.Context, name string) (ManagerView, int, error) {
	data, status, err := c.read(ctx, http.MethodGet, "/api/v1/managers/"+url.PathEscape(name))
	if err != nil || status != http.StatusOK {
		return ManagerView{}, status, err
	}
	var view ManagerView
	if err := json.Unmarshal(data, &view); err != nil {
		return ManagerView{}, status, fmt.Errorf("agentteams manager %s: decode response: %w", name, err)
	}
	return view, status, nil
}
