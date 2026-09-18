package reposcan

import (
	"context"
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

// GitLabFetcher implements Fetcher against the GitLab REST API (gitlab.com
// or a self-hosted instance declared in the platform map).
type GitLabFetcher struct {
	// Token is a personal access token sent as PRIVATE-TOKEN. Empty means
	// anonymous access (public projects only).
	Token string
	// BaseURL defaults to https://gitlab.com.
	BaseURL string
	// Client defaults to a client with a per-request timeout.
	Client *http.Client
}

const (
	gitlabDefaultBase = "https://gitlab.com"
	gitlabMaxBody     = 10 << 20
	gitlabMaxFile     = 2 << 20
	gitlabTimeout     = 15 * time.Second
	gitlabMaxPages    = 100 // tree pagination ceiling: 100 pages × 100 entries
)

func (f *GitLabFetcher) base() string {
	if f.BaseURL != "" {
		return strings.TrimRight(f.BaseURL, "/")
	}
	return gitlabDefaultBase
}

func (f *GitLabFetcher) client() *http.Client {
	if f.Client != nil {
		return f.Client
	}
	return &http.Client{Timeout: gitlabTimeout}
}

// projectID turns a normalized project URL into GitLab's URL-escaped
// namespace/project identifier: "group/sub/app" → "group%2Fsub%2Fapp".
func projectID(normalizedURL string) (string, error) {
	segments, ok := SplitRepoPath(normalizedURL)
	if !ok {
		return "", fmt.Errorf("不是可识别的仓库地址：%s", normalizedURL)
	}
	return url.PathEscape(strings.Join(segments, "/")), nil
}

// get performs one authenticated GET and returns body plus headers.
func (f *GitLabFetcher) get(ctx context.Context, apiPath string, capBytes int64) ([]byte, http.Header, error) {
	target := f.base() + apiPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if f.Token != "" {
		req.Header.Set("PRIVATE-TOKEN", f.Token)
	}
	response, err := f.client().Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, capBytes))
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	switch {
	case response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden:
		return nil, nil, ErrUnauthorized
	case response.StatusCode >= 400:
		return nil, nil, fmt.Errorf("%w: HTTP %d", ErrUnavailable, response.StatusCode)
	}
	return body, response.Header, nil
}

type gitLabTreeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"` // "tree" or "blob"
}

// FetchTree implements Fetcher: the recursive tree, following GitLab's
// page headers up to gitlabMaxPages. Selection caps downstream bound what
// a huge tree costs the scan.
func (f *GitLabFetcher) FetchTree(ctx context.Context, repoURL string) ([]TreeEntry, error) {
	project, err := projectID(repoURL)
	if err != nil {
		return nil, err
	}
	var entries []TreeEntry
	page := 1
	for {
		apiPath := fmt.Sprintf("/api/v4/projects/%s/repository/tree?recursive=true&per_page=100&page=%d",
			project, page)
		body, header, err := f.get(ctx, apiPath, gitlabMaxBody)
		if err != nil {
			return nil, err
		}
		var payload []gitLabTreeEntry
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, fmt.Errorf("%w: tree payload: %v", ErrUnavailable, err)
		}
		for _, item := range payload {
			entries = append(entries, TreeEntry{Path: item.Path, IsDir: item.Type == "tree"})
		}
		next := strings.TrimSpace(header.Get("X-Next-Page"))
		if next == "" || page >= gitlabMaxPages {
			break
		}
		page, err = strconv.Atoi(next)
		if err != nil {
			break
		}
	}
	return entries, nil
}

type gitLabCommit struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// FetchCommits implements Fetcher: commit subjects, newest first.
func (f *GitLabFetcher) FetchCommits(ctx context.Context, repoURL string, limit int) ([]string, error) {
	project, err := projectID(repoURL)
	if err != nil {
		return nil, err
	}
	if limit < 1 {
		limit = 1
	}
	body, _, err := f.get(ctx,
		fmt.Sprintf("/api/v4/projects/%s/repository/commits?per_page=%d", project, limit), 1<<20)
	if err != nil {
		return nil, err
	}
	var payload []gitLabCommit
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("%w: commits payload: %v", ErrUnavailable, err)
	}
	subjects := make([]string, 0, len(payload))
	for _, item := range payload {
		subjects = append(subjects, strings.TrimSpace(item.Title))
	}
	return subjects, nil
}

// FetchFileContent implements Fetcher via the raw file endpoint (ref HEAD).
func (f *GitLabFetcher) FetchFileContent(ctx context.Context, repoURL string, path string) (string, error) {
	project, err := projectID(repoURL)
	if err != nil {
		return "", err
	}
	apiPath := "/api/v4/projects/" + project + "/repository/files/" +
		url.PathEscape(path) + "/raw?ref=HEAD"
	body, _, err := f.get(ctx, apiPath, gitlabMaxFile)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(body) {
		return "", fmt.Errorf("%w: %s is not text", ErrUnavailable, path)
	}
	return string(body), nil
}
