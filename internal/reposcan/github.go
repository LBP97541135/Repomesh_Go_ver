package reposcan

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// GitHubFetcher implements Fetcher against the GitHub REST API.
// Zero-value configuration defaults to github.com with a shared client;
// BaseURL is overridden in tests.
type GitHubFetcher struct {
	// Token is a personal access or installation token. Empty means
	// anonymous access (public repositories only, tight rate limits).
	Token string
	// BaseURL defaults to https://api.github.com.
	BaseURL string
	// Client defaults to a client with a per-request timeout.
	Client *http.Client
}

const (
	githubDefaultBase = "https://api.github.com"
	githubMaxBody     = 10 << 20 // tree payloads of large monorepos
	githubMaxFile     = 2 << 20  // selected files are manifests and sources
	githubTimeout     = 15 * time.Second
)

func (f *GitHubFetcher) base() string {
	if f.BaseURL != "" {
		return strings.TrimRight(f.BaseURL, "/")
	}
	return githubDefaultBase
}

func (f *GitHubFetcher) client() *http.Client {
	if f.Client != nil {
		return f.Client
	}
	return &http.Client{Timeout: githubTimeout}
}

// repoCoordinates extracts "owner/repo" from a normalized repository URL.
func repoCoordinates(normalizedURL string) (string, error) {
	segments, ok := SplitRepoPath(normalizedURL)
	if !ok || len(segments) < 2 {
		return "", fmt.Errorf("不是可识别的仓库地址：%s", normalizedURL)
	}
	return segments[0] + "/" + segments[1], nil
}

// get performs one authenticated GET and returns the body up to cap bytes.
func (f *GitHubFetcher) get(ctx context.Context, apiPath string, capBytes int64) ([]byte, error) {
	target := f.base() + apiPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if f.Token != "" {
		req.Header.Set("Authorization", "Bearer "+f.Token)
	}
	response, err := f.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, capBytes))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	switch {
	case response.StatusCode == http.StatusTooManyRequests,
		response.StatusCode == http.StatusForbidden && response.Header.Get("X-RateLimit-Remaining") == "0":
		// 配额用尽（GitHub 用 403 + X-RateLimit-Remaining: 0 表达，偶尔 429）。
		// 与 401/403 的"凭据被拒"必须分开：前者等一会儿还能成，后者重试无用。
		if reset := rateLimitReset(response.Header); reset != "" {
			return nil, fmt.Errorf("%w（配额重置时间 %s）", ErrRateLimited, reset)
		}
		return nil, ErrRateLimited
	case response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden:
		return nil, ErrUnauthorized
	case response.StatusCode >= 400:
		return nil, fmt.Errorf("%w: HTTP %d", ErrUnavailable, response.StatusCode)
	}
	return body, nil
}

// rateLimitReset 把 X-RateLimit-Reset（unix 秒）格式化成 UTC 时间串，供
// 上层如实告诉用户"什么时候能再试"。取不到就返回空串。
func rateLimitReset(header http.Header) string {
	seconds, err := strconv.ParseInt(header.Get("X-RateLimit-Reset"), 10, 64)
	if err != nil || seconds <= 0 {
		return ""
	}
	return time.Unix(seconds, 0).UTC().Format(time.RFC3339)
}

type githubTreeResponse struct {
	Truncated bool `json:"truncated"`
	Tree      []struct {
		Path string `json:"path"`
		Type string `json:"type"`
	} `json:"tree"`
}

// FetchTree implements Fetcher. One recursive tree call; GitHub truncates
// trees beyond its own internal limits and the channels cap their
// selections anyway, so a truncated tree is accepted as-is.
func (f *GitHubFetcher) FetchTree(ctx context.Context, repoURL string) ([]TreeEntry, error) {
	repo, err := repoCoordinates(repoURL)
	if err != nil {
		return nil, err
	}
	body, err := f.get(ctx, "/repos/"+repo+"/git/trees/HEAD?recursive=1", githubMaxBody)
	if err != nil {
		return nil, err
	}
	var payload githubTreeResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("%w: tree payload: %v", ErrUnavailable, err)
	}
	entries := make([]TreeEntry, 0, len(payload.Tree))
	for _, item := range payload.Tree {
		entries = append(entries, TreeEntry{Path: item.Path, IsDir: item.Type == "tree"})
	}
	return entries, nil
}

type githubCommitResponse struct {
	Commit struct {
		Message string `json:"message"`
	} `json:"commit"`
}

// FetchCommits implements Fetcher: commit subjects (message first line),
// newest first.
func (f *GitHubFetcher) FetchCommits(ctx context.Context, repoURL string, limit int) ([]string, error) {
	repo, err := repoCoordinates(repoURL)
	if err != nil {
		return nil, err
	}
	if limit < 1 {
		limit = 1
	}
	body, err := f.get(ctx, fmt.Sprintf("/repos/%s/commits?per_page=%d", repo, limit), 1<<20)
	if err != nil {
		return nil, err
	}
	var payload []githubCommitResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("%w: commits payload: %v", ErrUnavailable, err)
	}
	subjects := make([]string, 0, len(payload))
	for _, item := range payload {
		subjects = append(subjects, firstLine(item.Commit.Message))
	}
	return subjects, nil
}

type githubContentResponse struct {
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}

// FetchFileContent implements Fetcher via the contents API (base64 JSON
// form, so binary content fails the UTF-8 check instead of leaking bytes
// into text channels).
func (f *GitHubFetcher) FetchFileContent(ctx context.Context, repoURL string, path string) (string, error) {
	repo, err := repoCoordinates(repoURL)
	if err != nil {
		return "", err
	}
	body, err := f.get(ctx,
		"/repos/"+repo+"/contents/"+pathEscapeSegments(path)+"?ref=HEAD", githubMaxFile)
	if err != nil {
		return "", err
	}
	var payload githubContentResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("%w: content payload: %v", ErrUnavailable, err)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(payload.Content, "\n", ""))
	if err != nil {
		return "", fmt.Errorf("%w: content encoding: %v", ErrUnavailable, err)
	}
	if !utf8.Valid(decoded) {
		return "", fmt.Errorf("%w: %s is not text", ErrUnavailable, path)
	}
	return string(decoded), nil
}

// pathEscapeSegments escapes each path segment so "src/main/go.mod" becomes
// "src/main/go.mod" (dots survive) while special characters never cross a
// segment boundary.
func pathEscapeSegments(path string) string {
	parts := strings.Split(path, "/")
	escaped := make([]string, len(parts))
	for index, part := range parts {
		escaped[index] = url.PathEscape(part)
	}
	return strings.Join(escaped, "/")
}

func firstLine(message string) string {
	if index := strings.IndexByte(message, '\n'); index >= 0 {
		return strings.TrimSpace(message[:index])
	}
	return strings.TrimSpace(message)
}
