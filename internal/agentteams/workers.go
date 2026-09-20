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

// workerSleepingSpec 是"出生即休眠"的建 worker 载荷(选仓门 §3.3)。
//
// 上游 CreateWorkerRequest 原生带 `state *string`(合法值 Running/Sleeping/
// Stopped),所以休眠建队就是**一步** POST —— 不是"先建 Running 再补一发
// /sleep":那会先真的起一个 runtime 再停它,正是懒启动要避免的浪费。
type workerSleepingSpec struct {
	Name  string `json:"name"`
	State string `json:"state"`
}

// CreateWorkerSleeping 注册一个出生即休眠的 worker(POST /api/v1/workers,
// body {"name":..,"state":"Sleeping"})。团队在仓库接入项目时预建,选进
// issue 范围时再由 EnsureReadyOrWake 唤醒。
func (c *Client) CreateWorkerSleeping(ctx context.Context, name string) ([]byte, int, error) {
	return c.write(ctx, http.MethodPost, "/api/v1/workers", workerSleepingSpec{Name: name, State: string(PhaseSleeping)})
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
//
// ⚠️ 上游语义(锁版本 lifecycle_handler.go):ensure-ready **只对
// Sleeping/Stopped 生效**;worker 处于其它相位(Pending/Failed/…)时它是一个
// 空操作,原样返回当前相位。要走"先试、不行就强制唤醒"的完整语义,用
// EnsureReadyOrWake —— 这条裸接口保留给 health_gate(它先读相位再行动)。
func (c *Client) EnsureReady(ctx context.Context, name string) ([]byte, int, error) {
	return c.write(ctx, http.MethodPost, "/api/v1/workers/"+url.PathEscape(name)+"/ensure-ready", nil)
}

// workerLifecycleBody 是上游 lifecycle 端点响应的最小形状:{name, phase}。
type workerLifecycleBody struct {
	Phase string `json:"phase"`
}

// EnsureReadyOrWake 把一个 worker 推回可用:先 POST ensure-ready,判定没生效就
// 回退 POST wake(选仓门 §3.3 的唤醒双保险就走这条)。
//
// 为什么要回退:上游 ensure-ready 只对 Sleeping/Stopped 生效,其余相位是空操作;
// 后端冲突时还会回 409。wake 则是**无条件**把 spec.state 置 Running 的强动作,
// 拿它兜底。返回最终观测到的相位;ensure-ready 与 wake 两条路都失败才返回错误
// (调用方要把失败如实写进 run 的失败原因,不许假装成功)。
func (c *Client) EnsureReadyOrWake(ctx context.Context, name string) (string, error) {
	data, status, err := c.EnsureReady(ctx, name)
	if err == nil && status >= http.StatusOK && status < http.StatusMultipleChoices {
		var response workerLifecycleBody
		if json.Unmarshal(data, &response) == nil && isReadyPhase(response.Phase) {
			return response.Phase, nil
		}
	}
	// ensure-ready 没生效(空操作 / 409 / 非 2xx / 传输失败)→ 回退 wake。
	wakeData, wakeStatus, wakeErr := c.Wake(ctx, name)
	if wakeErr != nil {
		return "", fmt.Errorf("wake worker %s: %w", name, wakeErr)
	}
	if wakeStatus < http.StatusOK || wakeStatus >= http.StatusMultipleChoices {
		return "", fmt.Errorf("wake worker %s returned %d", name, wakeStatus)
	}
	var woken workerLifecycleBody
	if err := json.Unmarshal(wakeData, &woken); err != nil || woken.Phase == "" {
		// 相位读不出来不算成功:上游 wake 总是回 {name, phase:"Running"}。
		return "", fmt.Errorf("wake worker %s: decode response: %v", name, err)
	}
	return woken.Phase, nil
}

// isReadyPhase 判定 ensure-ready 的响应是否表明"已经可用"(Running,或控制器
// 在 running+ready 时回的 Ready)。其余相位说明 ensure-ready 是空操作。
func isReadyPhase(phase string) bool {
	return phase == string(PhaseRunning) || phase == "Ready"
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
