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
		return err
	}
	if status < 200 || status > 299 {
		return fmt.Errorf("github: merge rejected with HTTP %d: %s", status, string(body))
	}
	return nil
}
