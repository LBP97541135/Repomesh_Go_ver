// Package agentteams 是 AgentTeams Controller 的锁版本服务端适配器
// (调研报告 docs/agentteams-survey-2026-09-07 与 graph-loop-design §2 的既定
// 结论):浏览器不直连 Controller,由 RepoMesh 后端持有凭据,只代理其窄 REST
// 子集中本产品需要的只读端点。当前仅封装 workflow 只读查询。
package agentteams

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client is the read-only Adapter entry. 零值不可用;由装配层按环境配置构造。
type Client struct {
	// BaseURL 形如 http://agentteams-controller:8090(不带尾斜杠)。
	BaseURL string
	// Token 是访问 Controller REST 的 ServiceAccount bearer token。
	Token string
	// HTTP 缺省为 15s 超时客户端;测试可注入。
	HTTP *http.Client
}

// Workflow 拉取整张项目工作流图(含每任务状态),上游响应原文原样返回。
// team 必填于多团队同名场景(Controller 对歧义 id 返回 409)。
// 返回值:响应体字节、上游 HTTP 状态码(透传给调用方的客户端)。
func (c *Client) Workflow(ctx context.Context, projectID, team string) ([]byte, int, error) {
	endpoint := strings.TrimRight(c.BaseURL, "/") + "/api/v1/projects/" +
		url.PathEscape(projectID) + "/workflow?includeTasks=true"
	if team != "" {
		endpoint += "&team=" + url.QueryEscape(team)
	}
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, http.StatusBadGateway, err
	}
	req.Header.Set("Accept", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, http.StatusBadGateway, fmt.Errorf("agentteams controller unreachable: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, http.StatusBadGateway, err
	}
	return body, resp.StatusCode, nil
}

// MatrixToken 取一枚用于 Matrix 的 access token
// （POST /api/v1/credentials/matrix-token）。
//
// 上游按**调用者身份**签发，不是任意用户：RepoMesh 用服务 token 调，拿到的就是
// 服务身份自己的 token，而它正是这些房间的创建者。这不是"偷控制器凭据"，
// 是控制器自己给的取凭据接口（上游注释：Workers/Managers 收到 401 时用它换新）。
//
// 房间消息只能直接打 homeserver —— 控制器的 REST 里没有"按 roomID 读消息"这条。
func (c *Client) MatrixToken(ctx context.Context) (string, int, error) {
	body, status, err := c.write(ctx, http.MethodPost, "/api/v1/credentials/matrix-token", map[string]any{})
	if err != nil || status != http.StatusOK {
		return "", status, err
	}
	var issued struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &issued); err != nil {
		return "", status, fmt.Errorf("agentteams matrix token: decode response: %w", err)
	}
	if issued.AccessToken == "" {
		return "", status, fmt.Errorf("agentteams matrix token: response carried no access_token")
	}
	return issued.AccessToken, status, nil
}
