package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

// MergePullRequest 调 GitHub 的合并接口（PUT /repos/{owner}/{repo}/pulls/{number}/merge）。
//
// 这是交付段唯一的**外部副作用**：合并真的会动用户的仓库，所以状态码一律如实
// 上抛（405 不可合并 / 409 有冲突 / 403 无权限），不吞、不重试 —— 重试一个
// 冲突的合并不会有别的结果，只会多打几次 API。
func (c *Client) MergePullRequest(ctx context.Context, token, owner, name string, number int, commitTitle string) error {
	payload, err := json.Marshal(map[string]string{
		"merge_method": "squash",
		"commit_title": commitTitle,
	})
	if err != nil {
		return err
	}
	target := "/repos/" + owner + "/" + name + "/pulls/" + strconv.Itoa(number) + "/merge"
	body, _, status, err := c.request(ctx, http.MethodPut, target, token, bytes.NewReader(payload))
	if err != nil {
		// 2026-09-20 线上实测：这里此前把 err 原样上抛，而 github.Error 对
		// 405/409/415/5xx 这类状态一律只吐一句 "github: unavailable" —— 界面上
		// 看到「合并被 GitHub 拒绝：github: unavailable」，查不出到底是哪一类。
		// 状态码带上（不带响应体，避免把响应内容里的敏感串带进界面/日志）。
		if status != 0 {
			return fmt.Errorf("%w (HTTP %d)", err, status)
		}
		return err
	}
	if status < 200 || status > 299 {
		return fmt.Errorf("github: merge rejected with HTTP %d: %s", status, string(body))
	}
	// 2xx 也不等于"合掉了"：GitHub 会在 200 里回 {"merged": false, "message": …}
	// （例如分支保护拦下）。只有明确看到 merged=false 才算失败，如实把 message 带出去。
	var result struct {
		Merged  *bool  `json:"merged"`
		Message string `json:"message"`
	}
	if decode(body, &result) == nil && result.Merged != nil && !*result.Merged {
		if result.Message == "" {
			result.Message = "GitHub 没有给出原因"
		}
		return fmt.Errorf("github: merge not applied: %s", result.Message)
	}
	return nil
}
