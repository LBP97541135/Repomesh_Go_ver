package access

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"

	"repomesh.local/repomesh/internal/github"
)

// RepositoryRef 是「按名字认领一个仓」的输入：URL 已经在调用方解析成三段。
type RepositoryRef struct {
	Host  string
	Owner string
	Name  string
}

// repositoryResolveConcurrency 是批量认领的最大并发。与参与权现探同一个档位
// （participationProbeConcurrency）—— 两者都是在打 GitHub，量级与限流风险一致。
const repositoryResolveConcurrency = 8

// ResolveRepositoriesByName 用 owner/name 批量认领仓库，给出它们的定位
// （含 `repo_<20 位 GitHub 数字 id>` 形式的 ID）。
//
// 2026-09-20 加：接入接口此前**只吃 id**，而那个 id 只有发现面给得出来；仓库页
// （扫描目录）只有 URL。线上实测的后果是用户点「接入本项目」必得 422 —— 拿目录的
// 32 位随机 hex 去撞 `repo_` 前缀校验。这里把"URL → 定位"这一跳补上。
//
// 数字 id 是 GitHub 的权威值，用**发起人的令牌**查：查不到按 404（这个仓对他不可见
// 或不存在），令牌失效按 503 并作废凭据。**不猜、不编一个 id** —— 编出来的 id 会
// 一路流到接入与交付，失败点在很远的地方。
//
// 2026-09-20 从「一仓一调用」改成批量，是被线上实测逼出来的：仓库页的「全部接入本
// 项目」一次交上来 43 个 URL，逐仓调用时第 35 个正好撞上 web 层的 15s 请求预算
// （日志：`resolve repository url failed at 35/43 elapsed=14.997s`）。代价的大头
// 不是"串行"，而是**每个仓都要重做一遍的固定开销**：credential() 每次都会开一个
// 事务去密钥库解封用户令牌（secrets.Open 自带事务 + FOR SHARE），逐仓调用等于把这
// 一步重复 N 次；叠上 GitHub 单次 ~114ms 的真实往返，实测每仓被抬到 ~430ms。
// 所以这里凭据只取一次，认领本身走有界并发。
//
// 错误语义与逐仓调用一致：**按输入顺序取第一个错误** —— 回执必须可复现，不能取决
// 于哪个 goroutine 先跑完。
func (s *Service) ResolveRepositoriesByName(ctx context.Context, principal ProjectPrincipal, refs []RepositoryRef) ([]RepositoryLocator, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	cred, err := s.credential(ctx, principal.actor, false)
	if err != nil {
		return nil, err
	}
	locators := make([]RepositoryLocator, len(refs))
	errs := make([]error, len(refs))
	sem := make(chan struct{}, repositoryResolveConcurrency)
	var wg sync.WaitGroup
	for index := range refs {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			locators[index], errs[index] = s.resolveRepository(ctx, cred, refs[index])
		}(index)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return locators, nil
}

func (s *Service) resolveRepository(ctx context.Context, cred credential, ref RepositoryRef) (RepositoryLocator, error) {
	repository, err := s.provider.Repository(ctx, cred.token, ref.Owner, ref.Name)
	if err != nil {
		var providerErr *github.Error
		if errors.As(err, &providerErr) {
			switch providerErr.Kind {
			case "denied":
				return RepositoryLocator{}, failure(404, "RESOURCE_NOT_FOUND")
			case "unauthorized":
				s.rejectCredential(ctx, cred)
				return RepositoryLocator{}, failure(503, "AUTHORIZATION_UNCONFIRMED")
			}
		}
		// **这条路必须落日志**：它返回的是裸 503 RESULT_UNCONFIRMED，不含原因，
		// 而调用方（含批量接入）只把错误码往界面上抛。
		// 2026-09-20 线上查这个 503 花了好几轮：PostgreSQL 里一条错误都没有（失败
		// 不在 SQL 层），GitHub 客户端自己也不打日志，于是"哪个仓、哪种 Kind"全靠猜
		// —— 批次越大越猜不出来。这里把仓名和 Kind 记下来。
		log.Printf("access: resolve repository failed owner=%s/%s kind=%s err=%v", ref.Owner, ref.Name, providerKind(err), err)
		return RepositoryLocator{}, unavailable()
	}
	if repository.ID <= 0 || repository.Owner == "" || repository.Name == "" {
		// 同上：payload 不可用也是一种"说不清"的失败，必须留下现场。
		log.Printf("access: resolve repository returned unusable payload ask=%s/%s id=%d canonical=%q/%q", ref.Owner, ref.Name, repository.ID, repository.Owner, repository.Name)
		return RepositoryLocator{}, unavailable()
	}
	return RepositoryLocator{
		ID:         fmt.Sprintf("repo_%020d", repository.ID),
		Host:       ref.Host,
		ExternalID: repository.ID,
		Owner:      repository.Owner,
		Name:       repository.Name,
	}, nil
}

// providerKind 取出 GitHub 客户端的错误分类，给日志用。
// 没有分类（网络错误、超时、ctx 到期）就记 "none" —— 那本身也是重要信息：
// 说明错在传输层或调用方的期限上，而不是 GitHub 给出的回答。
func providerKind(err error) string {
	var providerErr *github.Error
	if errors.As(err, &providerErr) && providerErr.Kind != "" {
		return providerErr.Kind
	}
	return "none"
}
