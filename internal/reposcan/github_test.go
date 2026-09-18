package reposcan

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

const fixtureTreePayload = `{
  "truncated": false,
  "tree": [
    {"path": "src", "type": "tree"},
    {"path": "src/main/java/com/demo/OrderClient.java", "type": "blob"},
    {"path": "pom.xml", "type": "blob"}
  ]
}`

func newGitHubServer(t *testing.T, handler http.HandlerFunc) *GitHubFetcher {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &GitHubFetcher{BaseURL: server.URL}
}

func TestGitHubFetchTree(t *testing.T) {
	fetcher := newGitHubServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme/orders/git/trees/HEAD" {
			t.Errorf("tree path = %q", r.URL.Path)
		}
		if r.URL.Query().Get("recursive") != "1" {
			t.Errorf("recursive param = %q", r.URL.Query().Get("recursive"))
		}
		_, _ = w.Write([]byte(fixtureTreePayload))
	})

	entries, err := fetcher.FetchTree(context.Background(), "https://github.com/acme/orders")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(entries))
	}
	if !entries[0].IsDir || entries[1].IsDir {
		t.Fatalf("dir flags wrong: %+v", entries)
	}
	if entries[2].Path != "pom.xml" {
		t.Fatalf("path = %q", entries[2].Path)
	}
}

func TestGitHubFetchCommitsTakesSubjects(t *testing.T) {
	fetcher := newGitHubServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("per_page"); got != "5" {
			t.Errorf("per_page = %q", got)
		}
		_, _ = w.Write([]byte(`[
			{"commit": {"message": "feat: booking flow\n\ncarries the details"}},
			{"commit": {"message": "fix: timeout"}}
		]`))
	})

	commits, err := fetcher.FetchCommits(context.Background(), "https://github.com/acme/orders", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 || commits[0] != "feat: booking flow" || commits[1] != "fix: timeout" {
		t.Fatalf("subjects = %v", commits)
	}
}

func TestGitHubFetchFileContentDecodesBase64(t *testing.T) {
	content := "spring:\n  datasource:\n    url: jdbc:mysql://orders-db:3306/orders\n"
	encoded := base64.StdEncoding.EncodeToString([]byte(content))
	// GitHub wraps long base64 in escaped newlines inside the JSON string;
	// the decoder must tolerate them.
	wrapped := ""
	for len(encoded) > 60 {
		wrapped += encoded[:60] + "\\n"
		encoded = encoded[60:]
	}
	wrapped += encoded

	fetcher := newGitHubServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme/orders/contents/src/main/resources/application.yml" {
			t.Errorf("content path = %q", r.URL.Path)
		}
		if r.URL.Query().Get("ref") != "HEAD" {
			t.Errorf("ref = %q", r.URL.Query().Get("ref"))
		}
		_, _ = w.Write([]byte(`{"content": "` + wrapped + `", "encoding": "base64"}`))
	})

	got, err := fetcher.FetchFileContent(context.Background(),
		"https://github.com/acme/orders", "src/main/resources/application.yml")
	if err != nil {
		t.Fatal(err)
	}
	if got != content {
		t.Fatalf("content = %q", got)
	}
}

func TestGitHubCredentialRejectionMapsToErrUnauthorized(t *testing.T) {
	fetcher := newGitHubServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})

	_, err := fetcher.FetchTree(context.Background(), "https://github.com/acme/orders")
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("401 must map to ErrUnauthorized, got %v", err)
	}
}

func TestGitHubServerErrorMapsToErrUnavailable(t *testing.T) {
	fetcher := newGitHubServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})

	_, err := fetcher.FetchCommits(context.Background(), "https://github.com/acme/orders", 5)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("5xx must map to ErrUnavailable, got %v", err)
	}
}

func TestGitHubFetcherRejectsNonRepositoryURL(t *testing.T) {
	fetcher := newGitHubServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("no upstream call expected for a URL without a repository path")
	})
	if _, err := fetcher.FetchTree(context.Background(), "https://github.com/"); err == nil {
		t.Fatal("a bare host URL must be refused before egress")
	}
}
