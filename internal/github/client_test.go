package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func testClient(t *testing.T, fn roundTripFunc) *Client {
	t.Helper()
	client, err := New(Config{
		ClientID: "Iv1.client", AppID: "123", CallbackURL: "https://repomesh.example/api/auth/github/callback",
		ClientSecret: func(context.Context) ([]byte, error) { return []byte("client-secret"), nil },
		PrivateKey:   func(context.Context) ([]byte, error) { return testKeyPEM(t), nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if fn != nil {
		client.transport = fn
	}
	return client
}

func reply(status int, payload string, header http.Header) *http.Response {
	if header == nil {
		header = make(http.Header)
	}
	return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(payload))}
}

func requireKind(t *testing.T, err error, kind string) *Error {
	t.Helper()
	var failure *Error
	if !errors.As(err, &failure) || failure.Kind != kind || err.Error() != "github: "+kind {
		t.Fatalf("error = %v, want safe %s", err, kind)
	}
	return failure
}

func TestNewRejectsInvalidConfiguration(t *testing.T) {
	base := testClient(t, nil).config
	tests := []struct {
		name   string
		change func(*Config)
	}{
		{"http callback", func(c *Config) { c.CallbackURL = "http://repomesh.example/api/auth/github/callback" }},
		{"callback credentials", func(c *Config) { c.CallbackURL = "https://secret@repomesh.example/api/auth/github/callback" }},
		{"callback query", func(c *Config) { c.CallbackURL += "?secret=1" }},
		{"callback fragment", func(c *Config) { c.CallbackURL += "#secret" }},
		{"callback path", func(c *Config) { c.CallbackURL = "https://repomesh.example/other" }},
		{"callback encoded path", func(c *Config) { c.CallbackURL = "https://repomesh.example/%61pi/auth/github/callback" }},
		{"client identifier", func(c *Config) { c.ClientID = "\nsecret" }},
		{"app identifier", func(c *Config) { c.AppID = "not-an-id" }},
		{"missing secret", func(c *Config) { c.ClientSecret = nil }},
		{"missing key", func(c *Config) { c.PrivateKey = nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := base
			test.change(&config)
			_, err := New(config)
			requireKind(t, err, "rejected")
		})
	}
}

func TestTransportIgnoresEnvironmentProxy(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	transport := testClient(t, nil).transport.(*http.Transport)
	if transport.Proxy != nil {
		t.Fatal("production transport reads a proxy")
	}
	if transport.ResponseHeaderTimeout != 5*time.Second || transport.TLSHandshakeTimeout != 5*time.Second {
		t.Fatal("transport timeout missing")
	}
	if transport.ForceAttemptHTTP2 || transport.TLSNextProto == nil {
		t.Fatal("github transport should not use HTTP/2")
	}
}

func TestRedirectNeverForwardsCredential(t *testing.T) {
	targetCalls := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls++; w.WriteHeader(http.StatusOK) }))
	defer target.Close()
	originCalls := 0
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originCalls++
		w.Header().Set("Location", target.URL+"?credential=secret")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	originURL, err := url.Parse(origin.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := testClient(t, func(req *http.Request) (*http.Response, error) {
		copy := req.Clone(req.Context())
		copy.URL.Scheme, copy.URL.Host = originURL.Scheme, originURL.Host
		return origin.Client().Transport.RoundTrip(copy)
	})
	_, err = client.Exchange(context.Background(), "code-secret", strings.Repeat("v", 43))
	requireKind(t, err, "unavailable")
	if originCalls != 1 || targetCalls != 0 {
		t.Fatalf("origin calls = %d, target calls = %d", originCalls, targetCalls)
	}
}

func TestHTTPFailuresStaySafeAndDoNotRetry(t *testing.T) {
	tests := []struct {
		name          string
		status        int
		header        http.Header
		payload, kind string
	}{
		{"unauthorized", 401, nil, `{"message":"access-token-secret"}`, "unauthorized"},
		{"forbidden", 403, nil, `{"message":"private/repo-secret"}`, "denied"},
		{"not found", 404, nil, `{"message":"private/repo-secret"}`, "denied"},
		{"bad request", 400, nil, `{"message":"code-secret"}`, "rejected"},
		{"unprocessable", 422, nil, "verifier-secret", "rejected"},
		{"rate limit", 429, http.Header{"Retry-After": {"12"}}, "response-secret", "rate_limited"},
		{"primary limit", 403, http.Header{"X-Ratelimit-Remaining": {"0"}}, "response-secret", "rate_limited"},
		{"secondary limit", 403, nil, `{"message":"You have exceeded a secondary rate limit."}`, "rate_limited"},
		{"unknown status", 418, nil, "response-secret", "unavailable"},
		{"server error", 503, nil, "response-secret", "unavailable"},
		{"redirect", 302, http.Header{"Location": {"https://github.com/secret?token=secret"}}, "response-secret", "unavailable"},
		{"large response", 200, nil, strings.Repeat("x", maxResponseBytes+1), "unavailable"},
		{"invalid JSON", 200, nil, "response-secret", "unavailable"},
		{"extra JSON", 200, nil, `{"id":1,"login":"name"} {"id":2}`, "unavailable"},
		{"invalid utf8", 200, nil, "{\"id\":1,\"login\":\"name\",\"name\":\"\xff\"}", "unavailable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			client := testClient(t, func(req *http.Request) (*http.Response, error) {
				calls++
				return reply(test.status, test.payload, test.header), nil
			})
			_, err := client.Refresh(context.Background(), "refresh-token-secret")
			failure := requireKind(t, err, test.kind)
			if calls != 1 {
				t.Fatalf("calls = %d", calls)
			}
			if test.name == "rate limit" && failure.RetryAfter != 12*time.Second {
				t.Fatalf("retry after = %v", failure.RetryAfter)
			}
		})
	}
}

func TestTransportErrorDoesNotExposeRequest(t *testing.T) {
	calls := 0
	client := testClient(t, func(req *http.Request) (*http.Response, error) {
		calls++
		return nil, fmt.Errorf("https://github.com/?code=secret token=private")
	})
	_, err := client.Exchange(context.Background(), "secret-code", strings.Repeat("v", 43))
	requireKind(t, err, "unavailable")
	if calls != 1 {
		t.Fatalf("calls = %d", calls)
	}
}

func TestRequestUsesShorterCallerDeadline(t *testing.T) {
	client := testClient(t, func(req *http.Request) (*http.Response, error) {
		deadline, ok := req.Context().Deadline()
		if !ok || time.Until(deadline) > 25*time.Millisecond {
			t.Fatal("caller deadline was lost")
		}
		<-req.Context().Done()
		return nil, req.Context().Err()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := client.Identity(ctx, "access-token")
	requireKind(t, err, "unavailable")
	if time.Since(started) > time.Second {
		t.Fatal("short timeout not honored")
	}
}

func TestRetryAfterParsesDateAndReset(t *testing.T) {
	now := time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		header http.Header
		want   time.Duration
	}{
		{http.Header{"Retry-After": {"12"}}, 12 * time.Second},
		{http.Header{"Retry-After": {now.Add(30 * time.Second).Format(http.TimeFormat)}}, 30 * time.Second},
		{http.Header{"X-Ratelimit-Reset": {fmt.Sprint(now.Add(45 * time.Second).Unix())}}, 45 * time.Second},
		{http.Header{"Retry-After": {"9223372036854775807"}}, time.Minute},
		{http.Header{"Retry-After": {"-1"}}, time.Minute},
	} {
		if got := retryAfter(test.header, now); got != test.want {
			t.Fatalf("retryAfter = %v, want %v", got, test.want)
		}
	}
}
