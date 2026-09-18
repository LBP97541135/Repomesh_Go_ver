package github

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestAuthorizationURLUsesPKCEAndFixedApp(t *testing.T) {
	client := testClient(t, nil)
	parsed, err := url.Parse(client.AuthorizationURL("state&value", "challenge+value"))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme != "https" || parsed.Host != "github.com" || parsed.Path != "/login/oauth/authorize" {
		t.Fatal("incorrect authorization target")
	}
	query := parsed.Query()
	for key, want := range map[string]string{"client_id": "Iv1.client", "redirect_uri": "https://repomesh.example/api/auth/github/callback", "state": "state&value", "code_challenge": "challenge+value", "code_challenge_method": "S256"} {
		if query.Get(key) != want {
			t.Fatalf("%s = %q", key, query.Get(key))
		}
	}
	if len(query) != 5 {
		t.Fatalf("unexpected authorization parameters: %v", query)
	}
}

func TestExchangeAndRefreshProtocol(t *testing.T) {
	for _, refreshing := range []bool{false, true} {
		t.Run(map[bool]string{false: "exchange", true: "refresh"}[refreshing], func(t *testing.T) {
			calls := 0
			client := testClient(t, func(req *http.Request) (*http.Response, error) {
				calls++
				if req.Method != http.MethodPost || req.URL.String() != "https://github.com/login/oauth/access_token" {
					t.Fatalf("incorrect token target: %s %s", req.Method, req.URL)
				}
				if req.Header.Get("Authorization") != "" || req.Header.Get("Accept") != "application/json" || req.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
					t.Fatal("incorrect token headers")
				}
				if err := req.ParseForm(); err != nil {
					t.Fatal(err)
				}
				if req.Form.Get("client_id") != "Iv1.client" || req.Form.Get("client_secret") != "client-secret" {
					t.Fatal("client credentials missing")
				}
				if refreshing {
					if req.Form.Get("grant_type") != "refresh_token" || req.Form.Get("refresh_token") != "old-refresh" || req.Form.Has("code") {
						t.Fatal("incorrect refresh body")
					}
				} else if req.Form.Get("code") != "test-code" || req.Form.Get("code_verifier") != strings.Repeat("v", 43) || req.Form.Get("redirect_uri") != "https://repomesh.example/api/auth/github/callback" || req.Form.Has("refresh_token") {
					t.Fatal("incorrect exchange body")
				}
				deadline, ok := req.Context().Deadline()
				if !ok || time.Until(deadline) > 5*time.Second {
					t.Fatal("missing request budget")
				}
				return reply(200, `{"access_token":"new-access","token_type":"bearer","expires_in":123,"refresh_token":"new-refresh","refresh_token_expires_in":456}`, nil), nil
			})
			started := time.Now()
			var tokens TokenSet
			var err error
			if refreshing {
				tokens, err = client.Refresh(context.Background(), "old-refresh")
			} else {
				tokens, err = client.Exchange(context.Background(), "test-code", strings.Repeat("v", 43))
			}
			if err != nil {
				t.Fatal(err)
			}
			if tokens.AccessToken != "new-access" || tokens.RefreshToken != "new-refresh" || tokens.AccessExpiresAt == nil || tokens.RefreshExpiresAt == nil {
				t.Fatalf("tokens = %+v", tokens)
			}
			if delta := tokens.AccessExpiresAt.Sub(started); delta < 122*time.Second || delta > 124*time.Second {
				t.Fatalf("access expiry delta = %v", delta)
			}
			if delta := tokens.RefreshExpiresAt.Sub(started); delta < 455*time.Second || delta > 457*time.Second {
				t.Fatalf("refresh expiry delta = %v", delta)
			}
			if calls != 1 {
				t.Fatalf("calls = %d", calls)
			}
		})
	}
}

func TestNonExpiringTokenDoesNotInventExpiry(t *testing.T) {
	client := testClient(t, func(*http.Request) (*http.Response, error) {
		return reply(200, `{"access_token":"access","token_type":"bearer"}`, nil), nil
	})
	tokens, err := client.Exchange(context.Background(), "code", strings.Repeat("v", 43))
	if err != nil {
		t.Fatal(err)
	}
	if tokens.AccessExpiresAt != nil || tokens.RefreshExpiresAt != nil || tokens.RefreshToken != "" {
		t.Fatalf("invented token fields: %+v", tokens)
	}
}

func TestTokenErrorsAndMalformedResponses(t *testing.T) {
	for _, test := range []struct{ payload, kind string }{
		{`{"error":"bad_verification_code","error_description":"code-secret"}`, "rejected"},
		{`{"error":"incorrect_code_verifier"}`, "rejected"},
		{`{"error":"bad_refresh_token"}`, "unauthorized"},
		{`{"error":"expired_refresh_token"}`, "unauthorized"},
		{`{"error":"access_denied"}`, "denied"},
		{`{"error":"future_error","error_description":"private-repo"}`, "unavailable"},
		{`{"error":"incorrect_client_credentials"}`, "unavailable"},
		{`{"access_token":"access","token_type":"mac"}`, "unavailable"},
		{`{"token_type":"bearer"}`, "unavailable"},
		{`{"access_token":"access\nsecret","token_type":"bearer"}`, "unavailable"},
		{`{"access_token":"access","token_type":"bearer","expires_in":-1}`, "unavailable"},
		{`{"access_token":"access","token_type":"bearer","expires_in":9223372036854775807}`, "unavailable"},
		{`{"access_token":"access","token_type":"bearer","expires_in":"123"}`, "unavailable"},
		{`{"access_token":"access","token_type":"bearer","refresh_token_expires_in":123}`, "unavailable"},
	} {
		client := testClient(t, func(*http.Request) (*http.Response, error) { return reply(200, test.payload, nil), nil })
		_, err := client.Exchange(context.Background(), "code", strings.Repeat("v", 43))
		requireKind(t, err, test.kind)
	}
}

func TestInvalidOAuthInputDoesNotContactProvider(t *testing.T) {
	client := testClient(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid input contacted provider")
		return nil, nil
	})
	for _, verifier := range []string{"", strings.Repeat("v", 42), strings.Repeat("v", 129), strings.Repeat("v", 42) + "/"} {
		_, err := client.Exchange(context.Background(), "code", verifier)
		requireKind(t, err, "rejected")
	}
	_, err := client.Refresh(context.Background(), "token\nsecret")
	requireKind(t, err, "rejected")
	client.config.ClientSecret = func(context.Context) ([]byte, error) { return nil, errors.New("secret-root-path") }
	_, err = client.Refresh(context.Background(), "token")
	requireKind(t, err, "unavailable")
}

func TestIdentityUsesStableID(t *testing.T) {
	for _, name := range []string{`"Friendly Name"`, `null`} {
		client := testClient(t, func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodGet || req.URL.String() != "https://api.github.com/user" || req.Header.Get("Authorization") != "Bearer access" || req.Header.Get("X-GitHub-Api-Version") != "2026-03-10" || req.Header.Get("User-Agent") != "RepoMesh" {
				t.Fatal("incorrect identity request")
			}
			return reply(200, `{"id":9007199254740993,"login":"renamed-account","name":`+name+`}`, nil), nil
		})
		identity, err := client.Identity(context.Background(), "access")
		if err != nil {
			t.Fatal(err)
		}
		want := "Friendly Name"
		if name == "null" {
			want = "renamed-account"
		}
		if identity.ID != 9007199254740993 || identity.DisplayName != want {
			t.Fatalf("identity = %+v", identity)
		}
	}
	client := testClient(t, func(*http.Request) (*http.Response, error) { return reply(200, `{"id":0,"login":"name"}`, nil), nil })
	_, err := client.Identity(context.Background(), "access")
	requireKind(t, err, "unavailable")
}
