package reposcan

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// ---------------------------------------------------------------------------
// GitHub — head, name resolution, org listing
// ---------------------------------------------------------------------------

type githubHead struct {
	SHA string `json:"sha"`
}

// FetchHead returns the default branch's latest commit SHA.
func (f *GitHubFetcher) FetchHead(ctx context.Context, repoURL string) (string, error) {
	repo, err := repoCoordinates(repoURL)
	if err != nil {
		return "", err
	}
	body, err := f.get(ctx, "/repos/"+repo+"/commits?per_page=1", 1<<20)
	if err != nil {
		return "", err
	}
	var heads []githubHead
	if err := json.Unmarshal(body, &heads); err != nil {
		return "", fmt.Errorf("%w: head payload: %v", ErrUnavailable, err)
	}
	if len(heads) == 0 {
		return "", fmt.Errorf("%w: no commits", ErrUnavailable)
	}
	return heads[0].SHA, nil
}

// ResolveName returns the repository's platform name (authoritative — a
// GitLab/GitHub project can be renamed in the UI while its URL path stays).
func (f *GitHubFetcher) ResolveName(ctx context.Context, repoURL string) (string, error) {
	repo, err := repoCoordinates(repoURL)
	if err != nil {
		return "", err
	}
	body, err := f.get(ctx, "/repos/"+repo, 1<<20)
	if err != nil {
		return "", err
	}
	var payload struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("%w: name payload: %v", ErrUnavailable, err)
	}
	return payload.Name, nil
}

type githubRepoListing struct {
	Name        string `json:"name"`
	HTMLURL     string `json:"html_url"`
	Description string `json:"description"`
	Archived    bool   `json:"archived"`
	Disabled    bool   `json:"disabled"`
	Fork        bool   `json:"fork"`
	Size        int    `json:"size"`
}

// ListRepos walks an organization's repositories, 100 per page, up to 20
// pages (2000 repositories — beyond that the org should be split).
func (f *GitHubFetcher) ListRepos(ctx context.Context, groupURL string) ([]RepoInfo, error) {
	segments, ok := SplitRepoPath(NormalizeGroupURL(groupURL))
	if !ok {
		return nil, fmt.Errorf("不是可识别的组织地址：%s", groupURL)
	}
	org := segments[0]

	var repos []RepoInfo
	for page := 1; page <= 20; page++ {
		body, err := f.get(ctx, fmt.Sprintf("/orgs/%s/repos?per_page=100&page=%d", org, page), listMaxBody)
		if err != nil {
			return nil, err
		}
		var payload []githubRepoListing
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, fmt.Errorf("%w: repos payload: %v", ErrUnavailable, err)
		}
		for _, item := range payload {
			repos = append(repos, RepoInfo{
				Name:        item.Name,
				URL:         item.HTMLURL,
				Description: item.Description,
				Archived:    item.Archived,
				Empty:       item.Size == 0,
				Fork:        item.Fork,
			})
		}
		if len(payload) < 100 {
			break
		}
	}
	return repos, nil
}

// listMaxBody caps listing payloads; large organizations page instead of
// growing this cap.
const listMaxBody = 16 << 20

// ---------------------------------------------------------------------------
// GitLab — head, name resolution, group listing
// ---------------------------------------------------------------------------

// FetchHead returns the default branch's latest commit SHA.
func (f *GitLabFetcher) FetchHead(ctx context.Context, repoURL string) (string, error) {
	project, err := projectID(repoURL)
	if err != nil {
		return "", err
	}
	body, _, err := f.get(ctx,
		fmt.Sprintf("/api/v4/projects/%s/repository/commits?per_page=1", project), 1<<20)
	if err != nil {
		return "", err
	}
	var commits []gitLabCommit
	if err := json.Unmarshal(body, &commits); err != nil {
		return "", fmt.Errorf("%w: head payload: %v", ErrUnavailable, err)
	}
	if len(commits) == 0 {
		return "", fmt.Errorf("%w: no commits", ErrUnavailable)
	}
	return commits[0].ID, nil
}

// ResolveName returns the project's platform name.
func (f *GitLabFetcher) ResolveName(ctx context.Context, repoURL string) (string, error) {
	project, err := projectID(repoURL)
	if err != nil {
		return "", err
	}
	body, _, err := f.get(ctx, "/api/v4/projects/"+project, 1<<20)
	if err != nil {
		return "", err
	}
	var payload struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("%w: name payload: %v", ErrUnavailable, err)
	}
	return payload.Name, nil
}

type gitLabProject struct {
	Name        string `json:"name"`
	WebURL      string `json:"web_url"`
	Description string `json:"description"`
	Archived    bool   `json:"archived"`
	EmptyRepo   bool   `json:"empty_repo"`
	ForkedFrom  any    `json:"forked_from_project"`
}

// ListRepos walks a group's projects (subgroups included), 100 per page,
// up to 20 pages.
func (f *GitLabFetcher) ListRepos(ctx context.Context, groupURL string) ([]RepoInfo, error) {
	segments, ok := SplitRepoPath(NormalizeGroupURL(groupURL))
	if !ok {
		return nil, fmt.Errorf("不是可识别的组织地址：%s", groupURL)
	}
	group := url.PathEscape(strings.Join(segments, "/"))

	var repos []RepoInfo
	for page := 1; page <= 20; page++ {
		apiPath := fmt.Sprintf("/api/v4/groups/%s/projects?include_subgroups=true&per_page=100&page=%d",
			group, page)
		body, _, err := f.get(ctx, apiPath, listMaxBody)
		if err != nil {
			return nil, err
		}
		var payload []gitLabProject
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, fmt.Errorf("%w: projects payload: %v", ErrUnavailable, err)
		}
		for _, item := range payload {
			repos = append(repos, RepoInfo{
				Name:        item.Name,
				URL:         item.WebURL,
				Description: item.Description,
				Archived:    item.Archived,
				Empty:       item.EmptyRepo,
				Fork:        item.ForkedFrom != nil,
			})
		}
		if len(payload) < 100 {
			break
		}
	}
	return repos, nil
}
