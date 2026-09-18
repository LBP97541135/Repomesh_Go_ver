package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// GitHubPROpener opens pull requests through the GitHub API with a token
// supplied by the caller (the platform resolves the App installation token
// per repository; it is used per request and never stored here).
type GitHubPROpener struct {
	HTTP *http.Client
}

// prRequest is the GitHub pull-request creation payload.
type prRequest struct {
	Title string `json:"title"`
	Head  string `json:"head"`
	Base  string `json:"base"`
	Body  string `json:"body"`
}

// prResponse keeps only the fields RepoMesh surfaces.
type prResponse struct {
	HTMLURL string `json:"html_url"`
	Number  int    `json:"number"`
}

// OpenPullRequest posts to the repository pulls endpoint and returns the PR
// URL. A 422 with an existing PR message is surfaced as the existing URL so a
// retry after a lost response does not duplicate pull requests.
func (g *GitHubPROpener) OpenPullRequest(ctx context.Context, token, owner, repo, branch, base, title, body string) (string, error) {
	httpClient := g.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	endpoint := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls", owner, repo)
	encoded, err := json.Marshal(prRequest{Title: title, Head: branch, Base: base, Body: body})
	if err != nil {
		return "", fmt.Errorf("delivery: pr payload failed: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("delivery: github unreachable: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	switch {
	case resp.StatusCode == 201:
		var parsed prResponse
		if json.Unmarshal(data, &parsed) != nil || parsed.HTMLURL == "" {
			return "", fmt.Errorf("delivery: pr response unreadable")
		}
		return parsed.HTMLURL, nil
	case resp.StatusCode == 422 && strings.Contains(string(data), "already exists"):
		existing := existingPRURL(data, owner, repo, branch)
		if existing != "" {
			return existing, nil
		}
		return "", fmt.Errorf("delivery: pull request already exists")
	default:
		return "", fmt.Errorf("delivery: pr creation refused (status %d)", resp.StatusCode)
	}
}

func existingPRURL(data []byte, owner, repo, branch string) string {
	var parsed struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if json.Unmarshal(data, &parsed) != nil {
		return ""
	}
	for _, item := range parsed.Errors {
		var number int
		if _, err := fmt.Sscanf(item.Message, "A pull request already exists for %*s/%s.", &number); err == nil && number > 0 {
			return fmt.Sprintf("https://github.com/%s/%s/pull/%d", owner, repo, number)
		}
	}
	return ""
}

var _ PullRequestOpener = (*GitHubPROpener)(nil)
