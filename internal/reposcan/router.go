package reposcan

import (
	"context"
	"fmt"
)

// Router dispatches fetch calls to the platform fetcher the target URL
// belongs to. One Router serves a whole scan run even when repositories
// span github.com, gitlab.com and declared self-hosted instances.
type Router struct {
	GitHub Fetcher
	GitLab Fetcher
	// Extra maps declared hosts (lowercase) to "github"/"gitlab".
	Extra map[string]string
}

func (r *Router) fetcherFor(repoURL string) (Fetcher, error) {
	platform, _, err := DetectPlatform(repoURL, r.Extra)
	if err != nil {
		return nil, err
	}
	switch platform {
	case PlatformGitHub:
		if r.GitHub == nil {
			return nil, fmt.Errorf("%w: github fetcher not configured", ErrUnavailable)
		}
		return r.GitHub, nil
	case PlatformGitLab:
		if r.GitLab == nil {
			return nil, fmt.Errorf("%w: gitlab fetcher not configured", ErrUnavailable)
		}
		return r.GitLab, nil
	default:
		return nil, fmt.Errorf("%w: platform %v", ErrUnavailable, platform)
	}
}

// WithToken returns a router whose platform fetchers use the given token.
//
// 2026-09-19：仓库扫描要"**以发起人本人的身份**"去读——配额 5000 次/小时
// （匿名只有 60），且天然按账号隔离。所以每次扫描按需克隆一个带用户令牌的
// Router：Router 是值类型，浅拷贝后替换 GitHub/GitLab 即可，**不需要改
// Runner 的签名**。token 为空时原样返回（由调用方决定兜底策略）。
func (r *Router) WithToken(token string) Fetcher {
	if r == nil || token == "" {
		return r
	}
	clone := *r
	if github, ok := r.GitHub.(*GitHubFetcher); ok {
		copied := *github
		copied.Token = token
		clone.GitHub = &copied
	}
	if gitlab, ok := r.GitLab.(*GitLabFetcher); ok {
		copied := *gitlab
		copied.Token = token
		clone.GitLab = &copied
	}
	return &clone
}

// HasToken reports whether any platform fetcher already carries a credential.
// Used to label a scan honestly ("deployment" vs "anonymous") — a scan that
// ran without a token must never be described as if it had one.
func (r *Router) HasToken() bool {
	if r == nil {
		return false
	}
	if github, ok := r.GitHub.(*GitHubFetcher); ok && github.Token != "" {
		return true
	}
	if gitlab, ok := r.GitLab.(*GitLabFetcher); ok && gitlab.Token != "" {
		return true
	}
	return false
}

// FetchTree implements Fetcher.
func (r *Router) FetchTree(ctx context.Context, repoURL string) ([]TreeEntry, error) {
	fetcher, err := r.fetcherFor(repoURL)
	if err != nil {
		return nil, err
	}
	return fetcher.FetchTree(ctx, repoURL)
}

// FetchCommits implements Fetcher.
func (r *Router) FetchCommits(ctx context.Context, repoURL string, limit int) ([]string, error) {
	fetcher, err := r.fetcherFor(repoURL)
	if err != nil {
		return nil, err
	}
	return fetcher.FetchCommits(ctx, repoURL, limit)
}

// FetchFileContent implements Fetcher.
func (r *Router) FetchFileContent(ctx context.Context, repoURL string, path string) (string, error) {
	fetcher, err := r.fetcherFor(repoURL)
	if err != nil {
		return "", err
	}
	return fetcher.FetchFileContent(ctx, repoURL, path)
}

// FetchHead implements Fetcher.
func (r *Router) FetchHead(ctx context.Context, repoURL string) (string, error) {
	fetcher, err := r.fetcherFor(repoURL)
	if err != nil {
		return "", err
	}
	return fetcher.FetchHead(ctx, repoURL)
}

// ResolveName implements Fetcher.
func (r *Router) ResolveName(ctx context.Context, repoURL string) (string, error) {
	fetcher, err := r.fetcherFor(repoURL)
	if err != nil {
		return "", err
	}
	return fetcher.ResolveName(ctx, repoURL)
}

// ListRepos implements Fetcher.
func (r *Router) ListRepos(ctx context.Context, groupURL string) ([]RepoInfo, error) {
	fetcher, err := r.fetcherFor(groupURL)
	if err != nil {
		return nil, err
	}
	return fetcher.ListRepos(ctx, groupURL)
}
