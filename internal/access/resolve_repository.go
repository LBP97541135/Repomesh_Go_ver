package access

import (
	"context"
	"errors"
	"fmt"
	"log"

	"repomesh.local/repomesh/internal/github"
)

// ResolveRepositoryByName 用 owner/name 去 GitHub 认领一个仓库，给出它的定位
// （含 `repo_<20 位 GitHub 数字 id>` 形式的 ID）。
//
// 2026-09-20 加：接入接口此前**只吃 id**，而那个 id 只有发现面给得出来；仓库页
// （扫描目录）只有 URL。线上实测的后果是用户点「接入本项目」必得 422 —— 拿目录的
// 32 位随机 hex 去撞 `repo_` 前缀校验。这里把"URL → 定位"这一跳补上。
//
// 数字 id 是 GitHub 的权威值，用**发起人的令牌**查：查不到按 404（这个仓对他不可见
// 或不存在），令牌失效按 503 并作废凭据。**不猜、不编一个 id** —— 编出来的 id 会
// 一路流到接入与交付，失败点在很远的地方。
func (s *Service) ResolveRepositoryByName(ctx context.Context, principal ProjectPrincipal, host, owner, name string) (RepositoryLocator, error) {
	credential, err := s.credential(ctx, principal.actor, false)
	if err != nil {
		return RepositoryLocator{}, err
	}
	repository, err := s.provider.Repository(ctx, credential.token, owner, name)
	if err != nil {
		var providerErr *github.Error
		if errors.As(err, &providerErr) {
			switch providerErr.Kind {
			case "denied":
				return RepositoryLocator{}, failure(404, "RESOURCE_NOT_FOUND")
			case "unauthorized":
				s.rejectCredential(ctx, credential)
				return RepositoryLocator{}, failure(503, "AUTHORIZATION_UNCONFIRMED")
			}
		}
		// **这条路必须落日志**：它返回的是裸 503 RESULT_UNCONFIRMED，不含原因，
		// 而调用方（含批量接入）只把错误码往界面上抛。
		// 2026-09-20 线上查这个 503 花了好几轮：PostgreSQL 里一条错误都没有（失败
		// 不在 SQL 层），GitHub 客户端自己也不打日志，于是"哪个仓、哪种 Kind"全靠猜
		// —— 批次越大越猜不出来。这里把仓名和 Kind 记下来。
		log.Printf("access: resolve repository failed owner=%s/%s kind=%s err=%v", owner, name, providerKind(err), err)
		return RepositoryLocator{}, unavailable()
	}
	if repository.ID <= 0 || repository.Owner == "" || repository.Name == "" {
		// 同上：payload 不可用也是一种"说不清"的失败，必须留下现场。
		log.Printf("access: resolve repository returned unusable payload ask=%s/%s id=%d canonical=%q/%q", owner, name, repository.ID, repository.Owner, repository.Name)
		return RepositoryLocator{}, unavailable()
	}
	return RepositoryLocator{
		ID:         fmt.Sprintf("repo_%020d", repository.ID),
		Host:       host,
		ExternalID: repository.ID,
		Owner:      repository.Owner,
		Name:       repository.Name,
	}, nil
}

// providerKind 取出 GitHub 客户端的错误分类，给日志用。
// 没有分类（网络错误、超时、解码失败）就记 "none" —— 那本身也是重要信息：
// 说明错在传输层而不是 GitHub 给的回答。
func providerKind(err error) string {
	var providerErr *github.Error
	if errors.As(err, &providerErr) && providerErr.Kind != "" {
		return providerErr.Kind
	}
	return "none"
}
