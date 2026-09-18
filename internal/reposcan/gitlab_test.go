package reposcan

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func newGitLabServer(t *testing.T, handler http.HandlerFunc) *GitLabFetcher {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &GitLabFetcher{BaseURL: server.URL}
}

func TestGitLabFetchTreeFollowsPages(t *testing.T) {
	page := 0
	fetcher := newGitLabServer(t, func(w http.ResponseWriter, r *http.Request) {
		// The namespace/project path must travel URL-escaped (%2F), and
		// EscapedPath is the field that preserves it.
		if got := r.URL.EscapedPath(); got != "/api/v4/projects/group%2Fsub%2Forders/repository/tree" {
			t.Errorf("tree path = %q", got)
		}
		if r.URL.Query().Get("recursive") != "true" {
			t.Errorf("recursive = %q", r.URL.Query().Get("recursive"))
		}
		w.Header().Set("X-Next-Page", strconv.Itoa(page+1))
		if page == 0 {
			page++
			_, _ = w.Write([]byte(`[{"path": "src", "type": "tree"}]`))
			return
		}
		w.Header().Del("X-Next-Page")
		_, _ = w.Write([]byte(`[{"path": "pom.xml", "type": "blob"}]`))
	})

	entries, err := fetcher.FetchTree(context.Background(), "https://gitlab.com/group/sub/orders")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || !entries[0].IsDir || entries[1].IsDir {
		t.Fatalf("paged entries = %+v", entries)
	}
}

func TestGitLabFetchCommits(t *testing.T) {
	fetcher := newGitLabServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("per_page"); got != "5" {
			t.Errorf("per_page = %q", got)
		}
		_, _ = w.Write([]byte(`[{"title": "feat: pay flow"}, {"title": "init"}]`))
	})

	commits, err := fetcher.FetchCommits(context.Background(), "https://gitlab.com/group/orders", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 || commits[0] != "feat: pay flow" {
		t.Fatalf("subjects = %v", commits)
	}
}

func TestGitLabFetchFileContentRaw(t *testing.T) {
	fetcher := newGitLabServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.EscapedPath(); got != "/api/v4/projects/group%2Forders/repository/files/src%2Fmain%2Fresources%2Fapplication.yml/raw" {
			t.Errorf("raw path = %q", got)
		}
		if r.URL.Query().Get("ref") != "HEAD" {
			t.Errorf("ref = %q", r.URL.Query().Get("ref"))
		}
		_, _ = w.Write([]byte("spring:\n  datasource:\n    url: jdbc:mysql://orders-db:3306/orders\n"))
	})

	got, err := fetcher.FetchFileContent(context.Background(),
		"https://gitlab.com/group/orders", "src/main/resources/application.yml")
	if err != nil {
		t.Fatal(err)
	}
	if want := "jdbc:mysql://orders-db:3306/orders"; !strings.Contains(got, want) {
		t.Fatalf("content missing %q:\n%s", want, got)
	}
}

func TestGitLabBinaryContentRefused(t *testing.T) {
	fetcher := newGitLabServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte{0xff, 0xfe, 0x00, 0x01})
	})
	if _, err := fetcher.FetchFileContent(context.Background(),
		"https://gitlab.com/group/orders", "assets/logo.bin"); err == nil {
		t.Fatal("binary content must not enter text channels")
	}
}

func TestGitLabAuthFailureMapsToErrUnauthorized(t *testing.T) {
	fetcher := newGitLabServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	_, err := fetcher.FetchTree(context.Background(), "https://gitlab.com/group/orders")
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("403 must map to ErrUnauthorized, got %v", err)
	}
}
