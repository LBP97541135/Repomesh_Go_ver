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
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
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
	// MasqueradeAs 非空时发送带 ?user_id=：appservice 凭据以此代指定身份发言
	//（appservice 自己的身份不在房里，裸发 403）。只影响发送，读取不需要。
	MasqueradeAs string
}

func (c *MatrixClient) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// matrixHTTPError 保留上游状态码：401（凭据过期）和 404（房间不存在）是完全两种
// 故障，调用方要能分辨——前者要换凭据重试，后者重试多少次都一样。
type matrixHTTPError struct {
	Status int
	Method string
	Path   string
	Detail string
}

func (e *matrixHTTPError) Error() string {
	return fmt.Sprintf("matrix %s %s: status %d: %s", e.Method, e.Path, e.Status, e.Detail)
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
		// 上游错误原文截断后带上：笼统一句话没法查。
		detail := string(data)
		if len(detail) > 300 {
			detail = detail[:300]
		}
		return resp.StatusCode, &matrixHTTPError{Status: resp.StatusCode, Method: method, Path: path, Detail: detail}
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return resp.StatusCode, fmt.Errorf("matrix %s %s: decode response: %w", method, path, err)
		}
	}
	return resp.StatusCode, nil
}

// RoomMessages 读一个房间的近期对话（GET /sync 带 room filter）。
//
// 为什么不走 /rooms/{roomId}/messages：这台 embedded homeserver（Tuwunel）的
// /messages 对这些房间只回 m.room.create 一条 —— 实测发送成功、/sync 能看到
// 消息，/messages 仍只回一条，机制没深究，结论是它在这不可信。/sync 带
// {"room":{"rooms":[id]}} 过滤一次只拿这间房的 timeline，对本场景（看当前
// 对话）足够；它只给最近一个窗口，不是全量历史 —— 需要全量时再换路。
//
// 只保留 `m.room.message`：状态事件不是"对话"，混进来会让空房间看起来像有人
// 在说话。sync 的 timeline 本就从旧到新，不必倒序。
func (c *MatrixClient) RoomMessages(ctx context.Context, roomID string, limit int) ([]MatrixMessage, error) {
	if strings.TrimSpace(roomID) == "" {
		return nil, fmt.Errorf("matrix room id is empty")
	}
	if limit <= 0 {
		limit = 100
	}
	roomFilter, err := json.Marshal(map[string]any{"room": map[string]any{"rooms": []string{roomID}}})
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	query.Set("timeout", "0")
	query.Set("filter", string(roomFilter))
	// 代发身份必须带：appservice 自己的身份不在房里，不带 user_id 的 sync 拿不到房。
	if c.MasqueradeAs != "" {
		query.Set("user_id", c.MasqueradeAs)
	}

	var page struct {
		Rooms struct {
			Join map[string]struct {
				Timeline struct {
					Events []struct {
						Type    string `json:"type"`
						EventID string `json:"event_id"`
						Sender  string `json:"sender"`
						TS      int64  `json:"origin_server_ts"`
						Content struct {
							Body string `json:"body"`
						} `json:"content"`
					} `json:"events"`
				} `json:"timeline"`
			} `json:"join"`
		} `json:"rooms"`
	}
	if _, err := c.do(ctx, http.MethodGet, "/_matrix/client/v3/sync?"+query.Encode(), nil, &page); err != nil {
		return nil, err
	}
	events := page.Rooms.Join[roomID].Timeline.Events
	messages := make([]MatrixMessage, 0, len(events))
	for _, event := range events {
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
	// sync 窗口可能超过要的条数，只留最新的 limit 条。
	if len(messages) > limit {
		messages = messages[len(messages)-limit:]
	}
	return messages, nil
}

// masqueradeQuery 把代发身份拼成查询串。空身份返回空串。
func masqueradeQuery(userID string) string {
	if userID == "" {
		return ""
	}
	return "?user_id=" + url.QueryEscape(userID)
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
		"/_matrix/client/v3/rooms/"+url.PathEscape(roomID)+"/send/m.room.message/"+url.PathEscape(txnID)+masqueradeQuery(c.MasqueradeAs),
		payload, &sent); err != nil {
		return "", err
	}
	return sent.EventID, nil
}

// MatrixSession 把"换凭据"和"用凭据"合成一件事：进程内持一枚 access token，
// 撞上 401 就用控制器换一枚新的、重试一次。
//
// 为什么缓存而不是每次现换：换 token 是**轮换**语义（上游 ForceRefreshMatrixToken），
// 每读一次房间就换一枚会让别处正在用的旧 token 失效。所以只在 401 时换。
// 也不落库：它是可再生的短期凭据，重启后现换一枚即可，存起来只是多一处泄露面。
type MatrixSession struct {
	// Controller 用来换凭据（POST /api/v1/credentials/matrix-token）。
	Controller *Client
	// Homeserver 形如 http://127.0.0.1:18080，来自 MATRIX_HOMESERVER_URL。
	Homeserver string
	// HTTP 透传给底下的 MatrixClient；测试可注入。
	HTTP *http.Client

	// 直配凭据（可选）：MATRIX_ACCESS_TOKEN，可选 REPOMESH_MATRIX_ACT_AS 代发身份。
	// 控制器的换凭据接口只给 Worker/Manager 身份发 —— 服务账号（admin）在那边
	// 没有凭据记录，POST credentials/matrix-token 恒 500「no credentials found」。
	// 嵌入式部署里正路是直配 appservice token 并以房间创建者身份代发。
	// 配了就完全不走控制器（Controller 可为 nil）。
	Token string
	ActAs string

	mu          sync.Mutex
	cachedToken string
}

func (s *MatrixSession) client(token string) *MatrixClient {
	return &MatrixClient{BaseURL: s.Homeserver, Token: token, HTTP: s.HTTP, MasqueradeAs: s.ActAs}
}

// accessToken 取当前凭据；没有就换一枚。**持锁换**：并发调用只有一个真去打控制器，
// 其余等它换完直接复用。
func (s *MatrixSession) accessToken(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Token != "" {
		return s.Token, nil
	}
	if s.cachedToken != "" {
		return s.cachedToken, nil
	}
	return s.refreshLocked(ctx, "")
}

// refreshLocked 换一枚凭据。**调用方必须已持有 s.mu。**
//
// 为什么必须串行：换 token 是**轮换**语义（上游 ForceRefreshMatrixToken）。两个并发
// 401 各自去换，第二枚会把第一枚作废，两个调用就来回打成 401 —— 越换越坏。
//
// stale 是调用方刚用过的那一枚：若它已不等于缓存里的值，说明别人刚换过，直接用新的，
// 不要再换一次（这正是"并发 401 互相作废"的解法）。
func (s *MatrixSession) refreshLocked(ctx context.Context, stale string) (string, error) {
	if stale != "" && s.cachedToken != "" && s.cachedToken != stale {
		return s.cachedToken, nil
	}
	if s.Controller == nil {
		return "", fmt.Errorf("agentteams controller client is nil; cannot obtain a matrix token")
	}
	token, _, err := s.Controller.MatrixToken(ctx)
	if err != nil {
		return "", err
	}
	// 兜一道底：空串绝不能当成"换到了"，否则会拿它去打 homeserver。
	if token == "" {
		return "", fmt.Errorf("agentteams controller returned an empty matrix token")
	}
	s.cachedToken = token
	return token, nil
}

// withToken 跑一次需要凭据的操作，401 时换一枚重试。
//
// 只重试一次，而且只认 401：403（确实无权）和 404（房间不存在）换多少枚凭据都一样，
// 重试只是把故障拖长。
func (s *MatrixSession) withToken(ctx context.Context, run func(*MatrixClient) error) error {
	token, err := s.accessToken(ctx)
	if err != nil {
		return err
	}
	err = run(s.client(token))
	var httpErr *matrixHTTPError
	if err == nil || !errors.As(err, &httpErr) || httpErr.Status != http.StatusUnauthorized {
		return err
	}
	// 换凭据这一段持锁（必须串行，见 refreshLocked），但**重试本身不持锁** ——
	// 不然一次网络往返就把所有并发调用堵在锁上。
	s.mu.Lock()
	refreshed, err := s.refreshLocked(ctx, token)
	s.mu.Unlock()
	if err != nil {
		return err
	}
	return run(s.client(refreshed))
}

// RoomMessages 读房间时间线，凭据过期会自动换一枚重试。
func (s *MatrixSession) RoomMessages(ctx context.Context, roomID string, limit int) ([]MatrixMessage, error) {
	var messages []MatrixMessage
	err := s.withToken(ctx, func(client *MatrixClient) error {
		var inner error
		messages, inner = client.RoomMessages(ctx, roomID, limit)
		return inner
	})
	return messages, err
}

// SendMessage 往房间发一条消息，凭据过期会自动换一枚重试。
func (s *MatrixSession) SendMessage(ctx context.Context, roomID, txnID, body string) (string, error) {
	var eventID string
	err := s.withToken(ctx, func(client *MatrixClient) error {
		var inner error
		eventID, inner = client.SendMessage(ctx, roomID, txnID, body)
		return inner
	})
	return eventID, err
}

// NewSessionFromEnv 按环境组装 MatrixSession，两条路二选一：
//
//  1. MATRIX_ACCESS_TOKEN（可选加 REPOMESH_MATRIX_ACT_AS 代发身份）—— 直配。
//     控制器的换凭据接口只给 Worker/Manager 发，服务账号（admin）在那边没有
//     凭据记录，POST 恒 500「no credentials found for admin」；嵌入式部署里
//     正路是直配 appservice token、并以房间创建者身份代发（appservice 自己
//     的身份不在房里，裸发 403 —— 实测过）。
//  2. 控制器换凭据 —— AGENTTEAMS_CONTROLLER_URL + MATRIX_HOMESERVER_URL，
//     适用于服务账号真有凭据记录的部署。
//
// 两条都配不上返回 nil：调用方如实报"没配"，而不是拿空凭据去打 homeserver。
func NewSessionFromEnv(controller *Client) *MatrixSession {
	homeserver := strings.TrimRight(os.Getenv("MATRIX_HOMESERVER_URL"), "/")
	if homeserver == "" {
		return nil
	}
	if token := strings.TrimSpace(os.Getenv("MATRIX_ACCESS_TOKEN")); token != "" {
		return &MatrixSession{
			Homeserver: homeserver,
			Token:      token,
			ActAs:      strings.TrimSpace(os.Getenv("REPOMESH_MATRIX_ACT_AS")),
		}
	}
	if controller == nil {
		return nil
	}
	return &MatrixSession{Controller: controller, Homeserver: homeserver}
}
