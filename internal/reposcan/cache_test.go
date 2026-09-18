package reposcan

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

type countingFetcher struct {
	trees    atomic.Int64
	commits  atomic.Int64
	contents atomic.Int64

	contentByPath map[string]string
	missingPath   string
}

func (f *countingFetcher) FetchTree(ctx context.Context, repoURL string) ([]TreeEntry, error) {
	f.trees.Add(1)
	return []TreeEntry{{Path: "pom.xml"}}, nil
}

func (f *countingFetcher) FetchCommits(ctx context.Context, repoURL string, limit int) ([]string, error) {
	f.commits.Add(1)
	return []string{"one", "two", "three"}, nil
}

func (f *countingFetcher) FetchHead(ctx context.Context, repoURL string) (string, error) {
	return "head-sha", nil
}

func (f *countingFetcher) ResolveName(ctx context.Context, repoURL string) (string, error) {
	return "orders", nil
}

func (f *countingFetcher) ListRepos(ctx context.Context, groupURL string) ([]RepoInfo, error) {
	return nil, nil
}

func (f *countingFetcher) FetchFileContent(ctx context.Context, repoURL string, path string) (string, error) {
	f.contents.Add(1)
	if path == f.missingPath {
		return "", errors.New("fetch failed")
	}
	return f.contentByPath[path], nil
}

func TestCacheFetchesEachMaterialOnce(t *testing.T) {
	inner := &countingFetcher{contentByPath: map[string]string{"pom.xml": "x"}}
	cache := NewCache(inner)
	ctx := context.Background()

	for range 3 {
		if _, err := cache.FetchTree(ctx, "https://github.com/acme/orders"); err != nil {
			t.Fatal(err)
		}
		if _, err := cache.FetchCommits(ctx, "https://github.com/acme/orders", 5); err != nil {
			t.Fatal(err)
		}
		if _, err := cache.FetchFileContent(ctx, "https://github.com/acme/orders", "pom.xml"); err != nil {
			t.Fatal(err)
		}
	}

	if got := inner.trees.Load(); got != 1 {
		t.Fatalf("tree upstream calls = %d, want 1", got)
	}
	if got := inner.commits.Load(); got != 1 {
		t.Fatalf("commit upstream calls = %d, want 1", got)
	}
	if got := inner.contents.Load(); got != 1 {
		t.Fatalf("content upstream calls = %d, want 1", got)
	}
}

func TestCacheCachesFailuresToo(t *testing.T) {
	inner := &countingFetcher{missingPath: "broken.yml", contentByPath: map[string]string{}}
	cache := NewCache(inner)
	ctx := context.Background()

	for range 2 {
		if _, err := cache.FetchFileContent(ctx, "https://github.com/acme/orders", "broken.yml"); err == nil {
			t.Fatal("missing path must keep reporting the fetch error")
		}
	}
	if got := inner.contents.Load(); got != 1 {
		t.Fatalf("a failed fetch must not be retried within one scan, upstream calls = %d", got)
	}
}

func TestCacheCommitLimitSlices(t *testing.T) {
	cache := NewCache(&countingFetcher{})
	ctx := context.Background()

	commits, err := cache.FetchCommits(ctx, "https://github.com/acme/orders", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 {
		t.Fatalf("limit slicing = %d commits, want 2", len(commits))
	}
}
