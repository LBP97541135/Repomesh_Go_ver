package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
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
// （组织的 `type` 是 "Organization"），一次调用够用。
func (c *Client) AccountProfile(ctx context.Context, login string) (int64, string, error) {
	if !pathSegment(login) {
		return 0, "", &Error{Kind: "rejected"}
	}
	payload, err := c.appJSON(ctx, "https://api.github.com/users/"+url.PathEscape(login))
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

// OrgRole 取这个用户在某个组织里的角色（admin / member）。
//
// 用**用户令牌**调（App JWT 读不到"某个人的组织角色"）。拿不到就返回空串——
// 界面据此显示"不确定"，绝不冒充"你有权限"：那会让人点进安装页才发现装不了，
// 比一开始就说清更糟。
func (c *Client) OrgRole(ctx context.Context, token, org string) (string, error) {
	if token == "" || !pathSegment(org) {
		return "", &Error{Kind: "rejected"}
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	payload, _, _, err := c.request(ctx, http.MethodGet,
		"https://api.github.com/user/memberships/orgs/"+url.PathEscape(org), token, nil)
	if err != nil {
		return "", err
	}
	var response struct {
		Role  string `json:"role"`
		State string `json:"state"`
	}
	if err := decode(payload, &response); err != nil {
		return "", err
	}
	// pending 的邀请不算能装——人还没真正进这个组织。
	if response.State != "" && response.State != "active" {
		return "", nil
	}
	return strings.ToLower(response.Role), nil
}
