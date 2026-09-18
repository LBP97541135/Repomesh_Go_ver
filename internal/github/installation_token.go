package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// InstallationAccessToken mints one installation access token for the App
// installation that governs owner/name. This is RepoMesh's own GitHub App
// identity: the token carries exactly the permissions granted to the App on
// that repository (contents/pull_requests write for delivery) and expires
// server-side in about one hour.
func (c *Client) InstallationAccessToken(ctx context.Context, owner, name string) (string, time.Time, error) {
	observation, err := c.ObserveAppInstallation(ctx, owner, name)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("observe installation: %w", err)
	}
	// A denied capability only means the local permission fingerprint does
	// not match the delivery baseline; GitHub remains the authority. As long
	// as the installation exists (a missing installation returns ID 0) we
	// mint the token and let the actual API scope decide.
	if observation.InstallationID() <= 0 {
		return "", time.Time{}, &Error{Kind: "denied"}
	}
	appJWT, err := c.appToken(ctx)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("app jwt: %w", err)
	}
	target := "https://api.github.com/app/installations/" + fmt.Sprintf("%d", observation.InstallationID()) + "/access_tokens"
	// The generic c.request treats any non-200 as an error; the access_tokens
	// endpoint answers 201 Created on success, so POST directly here.
	req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, target, nil)
	if reqErr != nil {
		return "", time.Time{}, fmt.Errorf("build request: %w", reqErr)
	}
	req.Header.Set("User-Agent", "RepoMesh")
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	req.Header.Set("Authorization", "Bearer "+appJWT)
	httpClient := &http.Client{Transport: c.transport, Timeout: 10 * time.Second}
	response, doErr := httpClient.Do(req)
	if doErr != nil {
		return "", time.Time{}, fmt.Errorf("POST access_tokens: %w", doErr)
	}
	defer response.Body.Close()
	payload, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if readErr != nil {
		return "", time.Time{}, fmt.Errorf("read access_tokens: %w", readErr)
	}
	if response.StatusCode != http.StatusCreated && response.StatusCode != http.StatusOK {
		return "", time.Time{}, fmt.Errorf("POST access_tokens: status %d body %.300s", response.StatusCode, string(payload))
	}
	var decoded struct {
		Token     string `json:"token"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil || decoded.Token == "" {
		return "", time.Time{}, fmt.Errorf("decode access_tokens: status %d body %.200s", response.StatusCode, string(payload))
	}
	expires, parseErr := time.Parse(time.RFC3339, decoded.ExpiresAt)
	if parseErr != nil {
		expires = time.Now().UTC().Add(time.Hour)
	}
	return decoded.Token, expires, nil
}
