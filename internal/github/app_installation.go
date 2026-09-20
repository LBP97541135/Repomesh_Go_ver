package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// ── App 级只读读面（2026-09-20）─────────────────────────────────────────────
//
// 给「把 App 装到哪里」这件事做指引用。用户要在**每个仓所属的账号**上各装一次
// ——个人号与组织是**各自独立**的安装目标，装在个人号上覆盖不到组织名下的仓。
// 界面得如实告诉他：还差哪几个账号、点哪个链接、你有没有权限装。
//
// GitHub **没有**创建安装的 API（只有列出 / 读 / 铸令牌 / 删）。这是刻意的同意
// 步骤，不是缺口。所以这里只做只读探测 + 拼安装链接；点那一下永远得人来。

// AppSlug 取这个 App 的 slug，用于拼 /apps/{slug}/installations/new。
func (c *Client) AppSlug(ctx context.Context) (string, error) {
	payload, err := c.appJSON(ctx, "https://api.github.com/app")
	if err != nil {
		return "", err
	}
	var response struct {
		Slug string `json:"slug"`
	}
	if err := decode(payload, &response); err != nil {
		return "", err
	}
	if response.Slug == "" {
		return "", &Error{Kind: "unavailable"}
	}
	return response.Slug, nil
}

// appJSON 用 App JWT 发一个只读 GET。三个方法共用，省得各自重复取令牌与超时。
func (c *Client) appJSON(ctx context.Context, target string) ([]byte, error) {
	token, err := c.appToken(ctx)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	payload, _, _, err := c.request(ctx, http.MethodGet, target, token, nil)
	return payload, err
}

// AppInstallationSummary 一次安装的要点（只留界面要用的字段）。
type AppInstallationSummary struct {
	ID                  int64
	AccountLogin        string
	AccountID           int64
	AccountType         string // "User" | "Organization"
	RepositorySelection string // "all" | "selected"
	Suspended           bool
}

// AppInstallations 列出这个 App 的全部安装（App JWT）。
// 返回体是**顶层数组**（不是 {installations:[…]} 包装），字段以官方 schema 为准。
func (c *Client) AppInstallations(ctx context.Context) ([]AppInstallationSummary, error) {
	payload, err := c.appJSON(ctx, "https://api.github.com/app/installations?per_page=100")
	if err != nil {
		return nil, err
	}
	var raw []struct {
		ID                  int64           `json:"id"`
		RepositorySelection string          `json:"repository_selection"`
		SuspendedAt         json.RawMessage `json:"suspended_at"`
		Account             struct {
			Login string `json:"login"`
			ID    int64  `json:"id"`
			Type  string `json:"type"`
		} `json:"account"`
	}
	if err := decode(payload, &raw); err != nil {
		return nil, err
	}
	out := make([]AppInstallationSummary, 0, len(raw))
	for _, item := range raw {
		out = append(out, AppInstallationSummary{
			ID:                  item.ID,
			AccountLogin:        item.Account.Login,
			AccountID:           item.Account.ID,
			AccountType:         item.Account.Type,
			RepositorySelection: item.RepositorySelection,
			Suspended:           len(item.SuspendedAt) > 0 && string(item.SuspendedAt) != "null",
		})
	}
	return out, nil
}

// AccountProfile 取一个账号（个人号或组织）的 id 与类型。
//
// `suggested_target_id` 要的是**数字 id**，少了它用户还得在安装页自己从账号列表里挑
// ——那正是我们想省掉的那一步。`/users/{login}` 对个人号和组织都返回这两个字段
// （组织的 `type` 是 "Organization"）。
//
// **必须用用户令牌或匿名调，不能用 App JWT**：JWT 只对 `/app`、`/app/installations`
// 这类 App 级端点有效，打到 `/users/...` 会被 GitHub 判 unauthorized（线上探针实测
// 就是这个错）。token 为空就匿名调（公开数据，60 次/小时够这一次引导用）。
func (c *Client) AccountProfile(ctx context.Context, token, login string) (int64, string, error) {
	if !pathSegment(login) {
		return 0, "", &Error{Kind: "rejected"}
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	payload, _, _, err := c.request(ctx, http.MethodGet,
		"https://api.github.com/users/"+url.PathEscape(login), token, nil)
	if err != nil {
		return 0, "", err
	}
	var response struct {
		ID   int64  `json:"id"`
		Type string `json:"type"`
	}
	if err := decode(payload, &response); err != nil {
		return 0, "", err
	}
	if response.ID <= 0 || response.Type == "" {
		return 0, "", &Error{Kind: "unavailable"}
	}
	return response.ID, response.Type, nil
}

// 说明：这里**没有**「读某个用户在组织里的角色」的方法。
// `GET /user/memberships/orgs/{org}` 需要 `read:org` scope，而本部署的登录流程
// 刻意不申请任何 scope（AuthorizationURL 不带 scope 参数）。加一个必然失败的方法
// 等于埋一节死代码，所以"你有没有权限装"这件事交给**安装页自己**回答：
// GitHub 的 installations/new 只列出你能装的账号，点进去自然就知道。
// 真要精确区分，得给 OAuth 加 read:org 并让所有人重登一次——那是产品决策。
