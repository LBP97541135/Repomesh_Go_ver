package github

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	requestTimeout   = 5 * time.Second
	maxResponseBytes = 1 << 20
	apiVersion       = "2026-03-10"
)

type Config struct {
	ClientID     string
	AppID        string
	CallbackURL  string
	ClientSecret func(context.Context) ([]byte, error)
	PrivateKey   func(context.Context) ([]byte, error)
}

type Client struct {
	config    Config
	transport http.RoundTripper
	mu        sync.Mutex
	appJWT    string
	appJWTExp time.Time
}

type TokenSet struct {
	AccessToken      string     `json:"accessToken"`
	RefreshToken     string     `json:"refreshToken"`
	AccessExpiresAt  *time.Time `json:"accessExpiresAt"`
	RefreshExpiresAt *time.Time `json:"refreshExpiresAt"`
}

type Identity struct {
	ID          int64  `json:"id"`
	DisplayName string `json:"displayName"`
}

type Repository struct {
	ID       int64  `json:"id"`
	Owner    string `json:"owner"`
	Name     string `json:"name"`
	FullName string `json:"fullName"`
}

type RepositoryPage struct {
	Items    []Repository `json:"items"`
	NextPage int          `json:"nextPage"`
}

type Capability struct {
	Status      string     `json:"status"`
	ReasonCodes []string   `json:"reasonCodes"`
	ObservedAt  *time.Time `json:"observedAt"`
}

type Error struct {
	Kind       string        `json:"kind"`
	RetryAfter time.Duration `json:"retryAfter"`
}

func (e *Error) Error() string {
	switch e.Kind {
	case "unauthorized", "denied", "rate_limited", "rejected", "unavailable":
		return "github: " + e.Kind
	default:
		return "github: unavailable"
	}
}

func New(config Config) (*Client, error) {
	callback, err := url.Parse(config.CallbackURL)
	appID, idErr := strconv.ParseInt(config.AppID, 10, 64)
	if err != nil || callback.Scheme != "https" || callback.Hostname() == "" || callback.User != nil || callback.RawQuery != "" || callback.ForceQuery || callback.Fragment != "" || callback.Path != "/api/auth/github/callback" || callback.RawPath != "" || !validCredential(config.ClientID) || idErr != nil || appID <= 0 || config.ClientSecret == nil || config.PrivateKey == nil {
		return nil, &Error{Kind: "rejected"}
	}
	return &Client{
		config: config,
		transport: &http.Transport{
			Proxy:                  nil,
			DialContext:            (&net.Dialer{Timeout: requestTimeout, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2:      false,
			TLSNextProto:           map[string]func(string, *tls.Conn) http.RoundTripper{},
			MaxIdleConns:           32,
			MaxIdleConnsPerHost:    16,
			IdleConnTimeout:        90 * time.Second,
			TLSHandshakeTimeout:    requestTimeout,
			ResponseHeaderTimeout:  requestTimeout,
			MaxResponseHeaderBytes: 64 << 10,
		},
	}, nil
}

func (c *Client) request(ctx context.Context, method, target, token string, body io.Reader) ([]byte, http.Header, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, nil, 0, &Error{Kind: "rejected"}
	}
	req.Header.Set("User-Agent", "RepoMesh")
	req.Header.Set("Accept", "application/vnd.github+json")
	if req.URL.Host == "api.github.com" {
		req.Header.Set("X-GitHub-Api-Version", apiVersion)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if method == http.MethodPost {
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	httpClient := &http.Client{
		Transport:     c.transport,
		Timeout:       requestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	response, err := httpClient.Do(req)
	if err != nil {
		return nil, nil, 0, &Error{Kind: "unavailable"}
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(payload) > maxResponseBytes {
		return nil, nil, response.StatusCode, &Error{Kind: "unavailable"}
	}
	if response.StatusCode != http.StatusOK {
		return nil, nil, response.StatusCode, responseError(response.StatusCode, response.Header, payload)
	}
	return payload, response.Header, response.StatusCode, nil
}

func responseError(status int, header http.Header, payload []byte) error {
	if status == http.StatusTooManyRequests || status == http.StatusForbidden && (header.Get("Retry-After") != "" || header.Get("X-RateLimit-Remaining") == "0") {
		return &Error{Kind: "rate_limited", RetryAfter: retryAfter(header, time.Now())}
	}
	if status == http.StatusForbidden {
		var response struct {
			Message string `json:"message"`
		}
		if decode(payload, &response) == nil && (strings.Contains(response.Message, "secondary rate limit") || strings.HasPrefix(response.Message, "API rate limit exceeded")) {
			return &Error{Kind: "rate_limited", RetryAfter: retryAfter(header, time.Now())}
		}
	}
	switch status {
	case http.StatusUnauthorized:
		return &Error{Kind: "unauthorized"}
	case http.StatusForbidden, http.StatusNotFound:
		return &Error{Kind: "denied"}
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return &Error{Kind: "rejected"}
	default:
		return &Error{Kind: "unavailable"}
	}
}

func retryAfter(header http.Header, now time.Time) time.Duration {
	if seconds, err := strconv.ParseInt(header.Get("Retry-After"), 10, 64); err == nil && seconds > 0 && seconds <= int64((1<<63-1)/time.Second) {
		return time.Duration(seconds) * time.Second
	}
	if deadline, err := http.ParseTime(header.Get("Retry-After")); err == nil && deadline.After(now) {
		return deadline.Sub(now)
	}
	if seconds, err := strconv.ParseInt(header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
		if deadline := time.Unix(seconds, 0); deadline.After(now) {
			return deadline.Sub(now)
		}
	}
	return time.Minute
}

func decode(payload []byte, value any) error {
	if !utf8.Valid(payload) || json.Unmarshal(payload, value) != nil {
		return &Error{Kind: "unavailable"}
	}
	return nil
}

func validCredential(value string) bool {
	if len(value) == 0 || len(value) > 16<<10 {
		return false
	}
	for _, char := range value {
		if char < 0x21 || char > 0x7e {
			return false
		}
	}
	return true
}

func pathSegment(value string) bool {
	if value == "" || len(value) > 100 || value == "." || value == ".." {
		return false
	}
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '-' || char == '_' || char == '.' {
			continue
		}
		return false
	}
	return true
}
