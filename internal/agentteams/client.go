// Package agentteams 是 AgentTeams Controller 的锁版本服务端适配器
// (调研报告 docs/agentteams-survey-2026-09-07 与 graph-loop-design §2 的既定
// 结论):浏览器不直连 Controller,由 RepoMesh 后端持有凭据,只代理其窄 REST
// 子集中本产品需要的只读端点。当前仅封装 workflow 只读查询。
package agentteams

import (
	"context"
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
