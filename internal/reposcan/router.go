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
