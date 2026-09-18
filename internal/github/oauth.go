package github

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (c *Client) AuthorizationURL(state, challenge string) string {
	query := url.Values{
		"client_id":             {c.config.ClientID},
		"redirect_uri":          {c.config.CallbackURL},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	return "https://github.com/login/oauth/authorize?" + query.Encode()
}

func (c *Client) Exchange(ctx context.Context, code, verifier string) (TokenSet, error) {
	if !validCredential(code) || !validVerifier(verifier) {
		return TokenSet{}, &Error{Kind: "rejected"}
	}
	return c.token(ctx, url.Values{
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {c.config.CallbackURL},
	}, false)
}

func (c *Client) Refresh(ctx context.Context, refreshToken string) (TokenSet, error) {
	if !validCredential(refreshToken) {
		return TokenSet{}, &Error{Kind: "rejected"}
	}
	return c.token(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}, true)
}

func (c *Client) token(ctx context.Context, form url.Values, refreshing bool) (TokenSet, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	secret, err := c.config.ClientSecret(ctx)
	if err != nil || !validCredential(string(secret)) {
		return TokenSet{}, &Error{Kind: "unavailable"}
	}
	form.Set("client_id", c.config.ClientID)
	form.Set("client_secret", string(secret))
	requestedAt := time.Now().UTC()
	payload, _, _, err := c.request(ctx, http.MethodPost, "https://github.com/login/oauth/access_token", "", strings.NewReader(form.Encode()))
	if err != nil {
		return TokenSet{}, err
	}
	var response struct {
		AccessToken           string `json:"access_token"`
		TokenType             string `json:"token_type"`
		ExpiresIn             *int64 `json:"expires_in"`
		RefreshToken          string `json:"refresh_token"`
		RefreshTokenExpiresIn *int64 `json:"refresh_token_expires_in"`
		Error                 string `json:"error"`
	}
	if err := decode(payload, &response); err != nil {
		return TokenSet{}, err
	}
	if response.Error != "" {
		switch response.Error {
		case "bad_verification_code", "incorrect_code_verifier", "redirect_uri_mismatch":
			return TokenSet{}, &Error{Kind: "rejected"}
		case "bad_refresh_token", "expired_refresh_token", "invalid_grant":
			return TokenSet{}, &Error{Kind: "unauthorized"}
		case "access_denied":
			return TokenSet{}, &Error{Kind: "denied"}
		default:
			return TokenSet{}, &Error{Kind: "unavailable"}
		}
	}
	if !validCredential(response.AccessToken) || !strings.EqualFold(response.TokenType, "bearer") || response.RefreshToken != "" && !validCredential(response.RefreshToken) || refreshing && response.RefreshToken == "" || response.RefreshTokenExpiresIn != nil && response.RefreshToken == "" {
		return TokenSet{}, &Error{Kind: "unavailable"}
	}
	accessExpiry, err := expiresAt(requestedAt, response.ExpiresIn)
	if err != nil {
		return TokenSet{}, err
	}
	refreshExpiry, err := expiresAt(requestedAt, response.RefreshTokenExpiresIn)
	if err != nil {
		return TokenSet{}, err
	}
	return TokenSet{AccessToken: response.AccessToken, RefreshToken: response.RefreshToken, AccessExpiresAt: accessExpiry, RefreshExpiresAt: refreshExpiry}, nil
}

func expiresAt(now time.Time, seconds *int64) (*time.Time, error) {
	if seconds == nil {
		return nil, nil
	}
	if *seconds <= 0 || *seconds > int64((1<<63-1)/time.Second) {
		return nil, &Error{Kind: "unavailable"}
	}
	deadline := now.Add(time.Duration(*seconds) * time.Second)
	return &deadline, nil
}

func validVerifier(value string) bool {
	if len(value) < 43 || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || strings.ContainsRune("-._~", char) {
			continue
		}
		return false
	}
	return true
}

func (c *Client) Identity(ctx context.Context, accessToken string) (Identity, error) {
	if !validCredential(accessToken) {
		return Identity{}, &Error{Kind: "rejected"}
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	payload, _, _, err := c.request(ctx, http.MethodGet, "https://api.github.com/user", accessToken, nil)
	if err != nil {
		return Identity{}, err
	}
	var response struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
		Name  string `json:"name"`
	}
	if err := decode(payload, &response); err != nil {
		return Identity{}, err
	}
	if response.ID <= 0 || !pathSegment(response.Login) || len(response.Name) > 1024 {
		return Identity{}, &Error{Kind: "unavailable"}
	}
	displayName := response.Name
	if displayName == "" {
		displayName = response.Login
	}
	return Identity{ID: response.ID, DisplayName: displayName}, nil
}
