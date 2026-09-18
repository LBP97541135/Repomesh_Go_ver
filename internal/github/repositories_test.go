package github

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

const repositoryJSON = `{"id":42,"name":"project","full_name":"owner/project","owner":{"login":"owner"}}`

func TestRepositoryPagesPreserveEmptyContinuation(t *testing.T) {
	calls := 0
	client := testClient(t, func(req *http.Request) (*http.Response, error) {
		calls++
		if req.Method != http.MethodGet || req.URL.Scheme != "https" || req.URL.Host != "api.github.com" || req.URL.Path != "/user/repos" || req.URL.Query().Get("page") != fmt.Sprint(calls) || req.URL.Query().Get("per_page") != "100" || req.Header.Get("Authorization") != "Bearer access" {
			t.Fatal("incorrect repositories request")
		}
		switch calls {
		case 1:
			return reply(200, `[]`, http.Header{"Link": {`<https://api.github.com/user/repos?page=2&per_page=100>; rel="next", <https://api.github.com/user/repos?page=3&per_page=100>; rel="last"`}}), nil
		case 2:
			return reply(200, `[`+repositoryJSON+`]`, http.Header{"Link": {`<https://api.github.com/user/repos?page=3&per_page=100>; rel="next"`}}), nil
		case 3:
			return reply(200, `[]`, http.Header{"Link": {`<https://api.github.com/user/repos?page=2&per_page=100>; rel="prev"`}}), nil
		default:
			t.Fatal("unexpected request")
			return nil, nil
		}
	})
	first, err := client.Repositories(context.Background(), "access", 1)
	if err != nil {
		t.Fatal(err)
	}
	if first.Items == nil || len(first.Items) != 0 || first.NextPage != 2 || calls != 1 {
		t.Fatalf("first = %+v, calls = %d", first, calls)
	}
	second, err := client.Repositories(context.Background(), "access", first.NextPage)
	if err != nil {
		t.Fatal(err)
	}
	if second.NextPage != 3 || !reflect.DeepEqual(second.Items, []Repository{{ID: 42, Owner: "owner", Name: "project", FullName: "owner/project"}}) {
		t.Fatalf("second = %+v", second)
	}
	last, err := client.Repositories(context.Background(), "access", second.NextPage)
	if err != nil {
		t.Fatal(err)
	}
	if last.NextPage != 0 || len(last.Items) != 0 || calls != 3 {
		t.Fatalf("last = %+v, calls = %d", last, calls)
	}
}

func TestRepositoryPaginationRejectsUnsafeAndAmbiguousLinks(t *testing.T) {
	for _, link := range []string{
		`<https://attacker.example/user/repos?page=2&per_page=100>; rel="next"`,
		`<http://api.github.com/user/repos?page=2&per_page=100>; rel="next"`,
		`<https://secret@api.github.com/user/repos?page=2&per_page=100>; rel="next"`,
		`<https://api.github.com:443/user/repos?page=2&per_page=100>; rel="next"`,
		`<https://api.github.com/repos/private/hidden?page=2&per_page=100>; rel="next"`,
		`<https://api.github.com/user/%72epos?page=2&per_page=100>; rel="next"`,
		`<https://api.github.com/user/repos?page=2&per_page=100#hidden>; rel="next"`,
		`<https://api.github.com/user/repos?page=2&page=3&per_page=100>; rel="next"`,
		`<https://api.github.com/user/repos?page=2&per_page=100&token=secret>; rel="next"`,
		`<https://api.github.com/user/repos?page=2&per_page=50>; rel="next"`,
		`<https://api.github.com/user/repos?page=0&per_page=100>; rel="next"`,
		`<https://api.github.com/user/repos?page=1&per_page=100>; rel="next"`,
		`<https://api.github.com/user/repos?page=abc&per_page=100>; rel="next"`,
		`<https://api.github.com/user/repos?page=9223372036854775808&per_page=100>; rel="next"`,
		`https://api.github.com/user/repos?page=2&per_page=100; rel="next"`,
		`<https://api.github.com/user/repos?page=2&per_page=100>; rel="next", <https://api.github.com/user/repos?page=3&per_page=100>; rel="next"`,
	} {
		calls := 0
		client := testClient(t, func(*http.Request) (*http.Response, error) {
			calls++
			return reply(200, `[`+repositoryJSON+`]`, http.Header{"Link": {link}}), nil
		})
		page, err := client.Repositories(context.Background(), "access", 1)
		requireKind(t, err, "unavailable")
		if len(page.Items) != 0 || calls != 1 {
			t.Fatal("unsafe link released partial page or followed URL")
		}
	}
}

func TestRepositoryPageRejectsInvalidRecords(t *testing.T) {
	for _, payload := range []string{
		`null`, `{}`, `[{"id":0,"name":"project","full_name":"owner/project","owner":{"login":"owner"}}]`,
		`[{"id":42,"name":"project","full_name":"other/project","owner":{"login":"owner"}}]`,
		`[{"id":42,"name":"project/hidden","full_name":"owner/project/hidden","owner":{"login":"owner"}}]`,
		`[` + strings.TrimSuffix(strings.Repeat(repositoryJSON+",", 101), ",") + `]`,
	} {
		client := testClient(t, func(*http.Request) (*http.Response, error) { return reply(200, payload, nil), nil })
		_, err := client.Repositories(context.Background(), "access", 1)
		requireKind(t, err, "unavailable")
	}
}

func TestRepositoryUsesDocumentedNameEndpoint(t *testing.T) {
	client := testClient(t, func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet || req.URL.String() != "https://api.github.com/repos/Owner/Project" {
			t.Fatalf("request = %s %s", req.Method, req.URL)
		}
		return reply(200, repositoryJSON, nil), nil
	})
	repository, err := client.Repository(context.Background(), "access", "Owner", "Project")
	if err != nil {
		t.Fatal(err)
	}
	if repository.ID != 42 || repository.FullName != "owner/project" {
		t.Fatalf("repository = %+v", repository)
	}
	client.transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return reply(200, `{"id":42,"name":"renamed","full_name":"owner/renamed","owner":{"login":"owner"}}`, nil), nil
	})
	_, err = client.Repository(context.Background(), "access", "owner", "project")
	requireKind(t, err, "unavailable")
}

func TestRepositoryRejectsPathEscapeBeforeHTTP(t *testing.T) {
	client := testClient(t, func(*http.Request) (*http.Response, error) { t.Fatal("invalid path reached provider"); return nil, nil })
	for _, segment := range []string{"", ".", "..", "owner/repo", `owner\repo`, "%2f", "a?token=secret", "a#secret", "a\nsecret"} {
		_, err := client.Repository(context.Background(), "access", segment, "project")
		requireKind(t, err, "rejected")
		_, err = client.Repository(context.Background(), "access", "owner", segment)
		requireKind(t, err, "rejected")
		_, err = client.AppCapability(context.Background(), segment, "project")
		requireKind(t, err, "rejected")
	}
	_, err := client.Repositories(context.Background(), "access", 0)
	requireKind(t, err, "rejected")
}
