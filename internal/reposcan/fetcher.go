package reposcan

import (
	"context"
	"errors"
	"sync"
)

// TreeEntry is one line of a repository file tree.
type TreeEntry struct {
	Path  string `json:"path"`
	IsDir bool   `json:"is_dir"`
}

// RepoInfo is one repository as listed from a group/org. Skippable reports
// whether the scanner should pass it by (archived or empty repositories
// always; forks unless the run explicitly includes them).
type RepoInfo struct {
	Name        string
	URL         string
	Description string
	Archived    bool
	Empty       bool
	Fork        bool
}

// Skippable reports whether the scanner should pass this repository by.
func (r RepoInfo) Skippable(includeForks bool) bool {
	return r.Archived || r.Empty || (!includeForks && r.Fork)
}

// Fetcher supplies the raw materials the scan pipeline consumes. FetchHead
// feeds the incremental fingerprint gate; ListRepos and ResolveName serve
// the organization walk and single-repository entry points.
type Fetcher interface {
	// FetchTree lists every file and directory entry of the default branch.
	FetchTree(ctx context.Context, repoURL string) ([]TreeEntry, error)
	// FetchCommits returns at most limit recent commit subjects
	// (first line of the message), newest first.
	FetchCommits(ctx context.Context, repoURL string, limit int) ([]string, error)
	// FetchFileContent returns one file's text content. Implementations
	// return an error for missing or unreadable files; the pipeline
	// treats any error as "this channel sees nothing here".
	FetchFileContent(ctx context.Context, repoURL string, path string) (string, error)
	// FetchHead returns the repository's latest commit SHA — the
	// incremental fingerprint. One lightweight call.
	FetchHead(ctx context.Context, repoURL string) (string, error)
	// ResolveName returns the platform's authoritative repository name,
	// or "" when the platform cannot confirm the URL names a repository.
	ResolveName(ctx context.Context, repoURL string) (string, error)
	// ListRepos lists the repositories under a group/org.
	ListRepos(ctx context.Context, groupURL string) ([]RepoInfo, error)
}

// ErrUnauthorized reports the configured credential was rejected.
var ErrUnauthorized = errors.New("reposcan: credential rejected")

// ErrUnavailable reports the platform could not be reached or answered
// unusably. The pipeline degrades per item; only the entry points decide
// whether that becomes a whole-scan failure.
var ErrUnavailable = errors.New("reposcan: platform unavailable")

// ErrRateLimited reports the platform refused the call because the caller
// exhausted its quota — GitHub answers 403 (or 429) with
// `X-RateLimit-Remaining: 0` in that case.
//
// 2026-09-19 事故复盘：此前 403 与 401 一起被判成 ErrUnauthorized，于是
// "配额用尽"被说成"没权限"，46 个仓库里 40 个被记成扫描失败、任务级却仍报
// succeeded。限流必须与鉴权失败分开——前者可退避重试，后者重试无用。
var ErrRateLimited = errors.New("reposcan: rate limited")

// ErrNotFound reports the platform answered "there is no such thing" (GitHub 404).
//
// 与 ErrUnavailable 分开的理由：404 是**确定性**的（不存在 / 看不见），重试无用；
// 而"组织端点 404 → 换用户端点"这种降级恰恰需要把它和"服务不可用"区分开。
//
// 2026-09-19 线上实测：扫 `https://github.com/LBP97541135`（**个人账号**）秒失败
// `reposcan: platform unavailable: HTTP 404` —— 因为列仓库只打了组织端点。
var ErrNotFound = errors.New("reposcan: not found")

// Cache wraps a Fetcher with per-scan memoization. Several channels select
// overlapping files, so each distinct path must cost exactly one upstream
// call; a fetch that failed (or found nothing) is cached too — a later
// channel must not retry it.
type Cache struct {
	inner Fetcher

	mu          sync.Mutex
	tree        []TreeEntry
	treeDone    bool
	commits     []string
	commitsDone bool
	contents    map[string]cachedContent
	head        string
	headDone    bool
	name        string
	nameDone    bool
	repos       []RepoInfo
	reposDone   bool
}

type cachedContent struct {
	value string
	err   error
}

// NewCache memoizes every call to inner for the lifetime of the Cache.
// One Cache per scan.
func NewCache(inner Fetcher) *Cache {
	return &Cache{inner: inner, contents: make(map[string]cachedContent)}
}

// FetchTree implements Fetcher.
func (c *Cache) FetchTree(ctx context.Context, repoURL string) ([]TreeEntry, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.treeDone {
		tree, err := c.inner.FetchTree(ctx, repoURL)
		if err != nil {
			return nil, err
		}
		c.tree = tree
		c.treeDone = true
	}
	return c.tree, nil
}

// FetchCommits implements Fetcher.
func (c *Cache) FetchCommits(ctx context.Context, repoURL string, limit int) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.commitsDone {
		commits, err := c.inner.FetchCommits(ctx, repoURL, limit)
		if err != nil {
			return nil, err
		}
		c.commits = commits
		c.commitsDone = true
	}
	if limit >= len(c.commits) {
		return c.commits, nil
	}
	return c.commits[:limit], nil
}

// FetchFileContent implements Fetcher. Errors are cached alongside values:
// a path that missed once stays a miss for the whole scan.
func (c *Cache) FetchFileContent(ctx context.Context, repoURL string, path string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cached, seen := c.contents[path]; seen {
		return cached.value, cached.err
	}
	value, err := c.inner.FetchFileContent(ctx, repoURL, path)
	c.contents[path] = cachedContent{value: value, err: err}
	return value, err
}

// FetchHead implements Fetcher (memoized: the fingerprint gate and the
// card both use it, one upstream call per scan).
func (c *Cache) FetchHead(ctx context.Context, repoURL string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.headDone {
		head, err := c.inner.FetchHead(ctx, repoURL)
		if err != nil {
			return "", err
		}
		c.head = head
		c.headDone = true
	}
	return c.head, nil
}

// ResolveName implements Fetcher (memoized).
func (c *Cache) ResolveName(ctx context.Context, repoURL string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.nameDone {
		name, err := c.inner.ResolveName(ctx, repoURL)
		if err != nil {
			return "", err
		}
		c.name = name
		c.nameDone = true
	}
	return c.name, nil
}

// ListRepos implements Fetcher (memoized).
func (c *Cache) ListRepos(ctx context.Context, groupURL string) ([]RepoInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.reposDone {
		repos, err := c.inner.ListRepos(ctx, groupURL)
		if err != nil {
			return nil, err
		}
		c.repos = repos
		c.reposDone = true
	}
	return c.repos, nil
}
