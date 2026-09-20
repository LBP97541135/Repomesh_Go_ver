// matrix.go 是 AgentTeams 自带 homeserver 的客户端。
//
// 为什么需要它：RepoMesh 想把「房间里到底说了什么」读回来给工作台看，而
// AgentTeams 控制器**没有**「按 roomID 读消息」的 REST（它只有
// `GET /api/v1/projects/{id}/spawns/{sessionId}/messages`，要求先有项目），
// 所以房间这一路只能直接打 Matrix。
//
// 凭据走控制器的 `POST /api/v1/credentials/matrix-token`：它按**调用者身份**
// 签发，不是任意用户。RepoMesh 用服务 token 调用，拿到的就是服务身份自己的
// Matrix token —— 而它正是这些房间的创建者，本来就在房里。
package agentteams

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// MatrixMessage 是房间里的一条 `m.room.message`。
type MatrixMessage struct {
	EventID   string
	Sender    string
	Body      string
	Timestamp time.Time
}

// MatrixClient 是 homeserver 上的读写入口。零值不可用。
type MatrixClient struct {
	// BaseURL 形如 http://127.0.0.1:18080（不带尾斜杠）。来自 MATRIX_HOMESERVER_URL。
	BaseURL string
	// Token 是 access token；空串时只读调用会失败并返回 401。
	Token string
	// HTTP 缺省 15s 超时；测试可注入。
	HTTP *http.Client
}

func (c *MatrixClient) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func (c *MatrixClient) do(ctx context.Context, method, path string, payload any, out any) (int, error) {
	endpoint := strings.TrimRight(c.BaseURL, "/") + path
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return http.StatusBadRequest, err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return http.StatusBadGateway, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return http.StatusBadGateway, fmt.Errorf("matrix homeserver unreachable: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return http.StatusBadGateway, err
	}
	if resp.StatusCode != http.StatusOK {
		// 上游错误原文截断后带上：401/403/404 的成因差别很大，笼统一句话没法查。
		detail := string(data)
		if len(detail) > 300 {
			detail = detail[:300]
		}
		return resp.StatusCode, fmt.Errorf("matrix %s %s: status %d: %s", method, path, resp.StatusCode, detail)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return resp.StatusCode, fmt.Errorf("matrix %s %s: decode response: %w", method, path, err)
		}
	}
	return resp.StatusCode, nil
}

// RoomMessages 读一个房间的时间线（GET /_matrix/client/v3/rooms/{roomId}/messages）。
//
// Matrix 的 `dir=b` 从新往旧回，这里翻成**从旧到新**再返回——时间线是按聊天读的，
// 调用方不该自己记得倒一遍。`limit` <= 0 时取 100（上游上限）。
//
// 只保留 `m.room.message`：房间创建、成员变更那些状态事件不是"对话"，
// 混进来会让空房间看起来像有人在说话。
func (c *MatrixClient) RoomMessages(ctx context.Context, roomID string, limit int) ([]MatrixMessage, error) {
	if strings.TrimSpace(roomID) == "" {
		return nil, fmt.Errorf("matrix room id is empty")
	}
	if limit <= 0 {
		limit = 100
	}
	query := url.Values{}
	query.Set("dir", "b")
	query.Set("limit", strconv.Itoa(limit))

	var page struct {
		Chunk []struct {
			Type    string `json:"type"`
			EventID string `json:"event_id"`
			Sender  string `json:"sender"`
			TS      int64  `json:"origin_server_ts"`
			Content struct {
				Body string `json:"body"`
			} `json:"content"`
		} `json:"chunk"`
	}
	if _, err := c.do(ctx, http.MethodGet,
		"/_matrix/client/v3/rooms/"+url.PathEscape(roomID)+"/messages?"+query.Encode(), nil, &page); err != nil {
		return nil, err
	}
	messages := make([]MatrixMessage, 0, len(page.Chunk))
	for _, event := range page.Chunk {
		if event.Type != "m.room.message" {
			continue
		}
		messages = append(messages, MatrixMessage{
			EventID:   event.EventID,
			Sender:    event.Sender,
			Body:      event.Content.Body,
			Timestamp: time.UnixMilli(event.TS).UTC(),
		})
	}
	sort.SliceStable(messages, func(i, j int) bool {
		return messages[i].Timestamp.Before(messages[j].Timestamp)
	})
	return messages, nil
}

// SendMessage 往房间发一条文本消息（PUT /_matrix/client/v3/rooms/{roomId}/send/m.room.message/{txnID}）。
// txnID 是幂等键：同一个 txnID 重发上游只认第一条，所以重试不会刷屏。
func (c *MatrixClient) SendMessage(ctx context.Context, roomID, txnID, body string) (string, error) {
	if strings.TrimSpace(roomID) == "" {
		return "", fmt.Errorf("matrix room id is empty")
	}
	if strings.TrimSpace(txnID) == "" {
		return "", fmt.Errorf("matrix transaction id is empty")
	}
	payload := map[string]string{"msgtype": "m.text", "body": body}
	var sent struct {
		EventID string `json:"event_id"`
	}
	if _, err := c.do(ctx, http.MethodPut,
		"/_matrix/client/v3/rooms/"+url.PathEscape(roomID)+"/send/m.room.message/"+url.PathEscape(txnID),
		payload, &sent); err != nil {
		return "", err
	}
	return sent.EventID, nil
}
