// workers.go extends the AgentTeams adapter with the lifecycle subset the
// pipeline needs: worker/team creation and wake/sleep/status. All endpoints
// are the locked-upstream REST surface (核查报告: internal/server/
// resource_handler.go + lifecycle_handler.go). RepoMesh holds the admin/
// manager token; the browser never talks to the Controller directly.
package agentteams

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// WorkerSpec is the creation payload for one worker under a team.
type WorkerSpec struct {
	Name     string            `json:"name"`
	Team     string            `json:"team,omitempty"`
	Runtime  string            `json:"runtime,omitempty"` // e.g. repomesh-runner
	Identity map[string]string `json:"identity,omitempty"`
	Labels   []string          `json:"labels,omitempty"`
}

// CreateWorker registers one worker (POST /api/v1/workers). A team leader
// token cannot create workers (upstream 409); the configured token must be
// admin/manager level.
func (c *Client) CreateWorker(ctx context.Context, spec WorkerSpec) ([]byte, int, error) {
	return c.write(ctx, http.MethodPost, "/api/v1/workers", spec)
}

// WorkerStatus returns the live status of one worker (GET /api/v1/workers/{name}/status).
func (c *Client) WorkerStatus(ctx context.Context, name string) ([]byte, int, error) {
	return c.read(ctx, http.MethodGet, "/api/v1/workers/"+url.PathEscape(name)+"/status")
}

// Wake transitions a sleeping worker back to active.
func (c *Client) Wake(ctx context.Context, name string) ([]byte, int, error) {
	return c.write(ctx, http.MethodPost, "/api/v1/workers/"+url.PathEscape(name)+"/wake", nil)
}

// Sleep puts an idle worker to sleep.
func (c *Client) Sleep(ctx context.Context, name string) ([]byte, int, error) {
	return c.write(ctx, http.MethodPost, "/api/v1/workers/"+url.PathEscape(name)+"/sleep", nil)
}

// EnsureReady drives a worker to ready state (idempotent upstream).
func (c *Client) EnsureReady(ctx context.Context, name string) ([]byte, int, error) {
	return c.write(ctx, http.MethodPost, "/api/v1/workers/"+url.PathEscape(name)+"/ensure-ready", nil)
}

func (c *Client) write(ctx context.Context, method, path string, payload any) ([]byte, int, error) {
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	endpoint := strings.TrimRight(c.BaseURL, "/") + path
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, http.StatusBadRequest, err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, http.StatusBadGateway, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, http.StatusBadGateway, fmt.Errorf("agentteams controller unreachable: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, http.StatusBadGateway, err
	}
	return data, resp.StatusCode, nil
}

func (c *Client) read(ctx context.Context, method, path string) ([]byte, int, error) {
	return c.write(ctx, method, path, nil)
}

// ---- 团队管理（仓库作用域 Team，2026-09-20 从计划分支并入）----

// TeamMember is one worker's role in an AgentTeams team.
type TeamMember struct {
	Name string `json:"name"`
	Role string `json:"role"`
}

// TeamSpec is the creation payload for one AgentTeams team.
type TeamSpec struct {
	Name          string       `json:"name"`
	TeamName      string       `json:"teamName,omitempty"`
	WorkerMembers []TeamMember `json:"workerMembers"`
}

// DeleteWorker removes one worker (DELETE /api/v1/workers/{name}).
func (c *Client) DeleteWorker(ctx context.Context, name string) ([]byte, int, error) {
	return c.write(ctx, http.MethodDelete, "/api/v1/workers/"+url.PathEscape(name), nil)
}

// CreateTeam creates one team (POST /api/v1/teams).
func (c *Client) CreateTeam(ctx context.Context, spec TeamSpec) ([]byte, int, error) {
	return c.write(ctx, http.MethodPost, "/api/v1/teams", spec)
}

// UpdateTeam replaces only a team's membership (PUT /api/v1/teams/{name}).
func (c *Client) UpdateTeam(ctx context.Context, name string, members []TeamMember) ([]byte, int, error) {
	payload := struct {
		WorkerMembers []TeamMember `json:"workerMembers"`
	}{WorkerMembers: members}
	return c.write(ctx, http.MethodPut, "/api/v1/teams/"+url.PathEscape(name), payload)
}

// TeamView is the subset of one AgentTeams Team we read back.
//
// TeamRoomID / LeaderDMRoomID live in the Team CR's **status**, filled in by the
// controller after it has created the Matrix rooms. They are therefore absent
// from the POST/PUT /api/v1/teams response — the only way to learn a room is to
// read the team back once reconciliation has run.
type TeamView struct {
	Name           string `json:"name"`
	Phase          string `json:"phase"`
	TeamRoomID     string `json:"teamRoomID"`
	LeaderDMRoomID string `json:"leaderDMRoomID"`
}

// GetTeam reads one team back (GET /api/v1/teams/{name}).
func (c *Client) GetTeam(ctx context.Context, name string) (TeamView, int, error) {
	data, status, err := c.read(ctx, http.MethodGet, "/api/v1/teams/"+url.PathEscape(name))
	if err != nil || status != http.StatusOK {
		return TeamView{}, status, err
	}
	var view TeamView
	if err := json.Unmarshal(data, &view); err != nil {
		return TeamView{}, status, fmt.Errorf("agentteams team %s: decode response: %w", name, err)
	}
	return view, status, nil
}
