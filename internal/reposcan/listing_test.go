package reposcan

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

// 2026-09-19 线上实测：扫 `https://github.com/LBP97541135`（**个人账号**，不是组织）
// 秒失败 `reposcan: platform unavailable: HTTP 404` —— 因为列仓库只打了组织端点。
// 组织端点 404 时必须退回用户端点，而不是把"这是个人账号"报成"平台不可用"。
func TestGitHubListReposFallsBackToUserEndpoint(t *testing.T) {
	var orgCalls, userCalls int
	fetcher := newGitHubServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/orgs/LBP97541135/repos":
			orgCalls++
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
		case "/users/LBP97541135/repos":
			userCalls++
			_, _ = w.Write([]byte(`[{"name":"repomesh-e2e-api","html_url":"https://github.com/LBP97541135/repomesh-e2e-api","description":"报价服务","archived":false,"disabled":false,"fork":false,"size":128}]`))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})

	repos, err := fetcher.ListRepos(context.Background(), "https://github.com/LBP97541135")
	if err != nil {
		t.Fatalf("个人账号应能列出仓库，得到 %v", err)
	}
	if orgCalls != 1 || userCalls != 1 {
		t.Fatalf("应先试组织端点再退用户端点，得到 org=%d user=%d", orgCalls, userCalls)
	}
	if len(repos) != 1 || repos[0].Name != "repomesh-e2e-api" {
		t.Fatalf("仓库列表不符: %+v", repos)
	}
}

// 组织端点正常时**不该**多打一次用户端点（个人账号是少数情况）。
func TestGitHubListReposPrefersOrgEndpoint(t *testing.T) {
	var userCalls int
	fetcher := newGitHubServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/users/acme/repos" {
			userCalls++
		}
		_, _ = w.Write([]byte(`[{"name":"orders","html_url":"https://github.com/acme/orders","archived":false,"fork":false,"size":64}]`))
	})

	repos, err := fetcher.ListRepos(context.Background(), "https://github.com/acme")
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if len(repos) != 1 {
		t.Fatalf("仓库列表不符: %+v", repos)
	}
	if userCalls != 0 {
		t.Fatalf("组织端点成功时不该再打用户端点，得到 user=%d", userCalls)
	}
}

// 限流不能伪装成"换个端点就好了"：非 404 的错误必须原样上抛。
func TestGitHubListReposDoesNotFallBackOnRateLimit(t *testing.T) {
	var calls int
	fetcher := newGitHubServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.WriteHeader(http.StatusForbidden)
	})

	_, err := fetcher.ListRepos(context.Background(), "https://github.com/acme")
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("限流必须原样上抛（ErrRateLimited），得到 %v", err)
	}
	if calls != 1 {
		t.Fatalf("限流时不该重试别的端点，得到 %d 次调用", calls)
	}
}

// 404 现在有自己的错误类别，不再混进"平台不可用"。
func TestGitHubNotFoundIsDistinctFromUnavailable(t *testing.T) {
	fetcher := newGitHubServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	})

	_, err := fetcher.FetchHead(context.Background(), "https://github.com/acme/ghost")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("404 应映射成 ErrNotFound，得到 %v", err)
	}
	if errors.Is(err, ErrUnavailable) {
		t.Fatalf("404 不该再说成「平台不可用」")
	}
}
