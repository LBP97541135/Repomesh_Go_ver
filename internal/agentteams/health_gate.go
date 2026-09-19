// health_gate.go 实现派发前健康闸(Phase 2,2026-09-18):派任务给某个
// worker 之前先确认它处于 Running;Sleeping/Stopped 自动唤醒(ensure-ready),
// 唤不醒就报 blocked。这样编排层不会把任务派给一个已死或正在停机的 worker。
//
// 闸门只回答"现在能不能派",不替编排层决定后续动作(标记任务、告警、重建
// 等由调用方按 blocked + Error 自行处理)。
package agentteams

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// WorkerPhase 是 Controller reconcile 上报的 worker 运行相位。
type WorkerPhase string

const (
	PhasePending  WorkerPhase = "Pending"
	PhaseStarting WorkerPhase = "Starting"
	PhaseRunning  WorkerPhase = "Running"
	PhaseStopping WorkerPhase = "Stopping"
	PhaseSleeping WorkerPhase = "Sleeping"
	PhaseStopped  WorkerPhase = "Stopped"
	PhaseFailed   WorkerPhase = "Failed"
)

// HealthGateResult 汇报一次派发前检查看到什么、做了什么。
// Action 取值:"none" | "ensure-ready" | "blocked"。
type HealthGateResult struct {
	Worker     string      `json:"worker"`
	Phase      WorkerPhase `json:"phase"`
	Action     string      `json:"action"`
	Recovered  bool        `json:"recovered"`
	DurationMs int64       `json:"durationMs"`
	Error      string      `json:"error,omitempty"`
}

// workerStatusBody 是上游响应里我们只关心的最小形状:status.phase。
type workerStatusBody struct {
	Status struct {
		Phase string `json:"phase"`
	} `json:"status"`
}

// fetchPhase 从 Controller 读 worker 当前相位。
func (c *Client) fetchPhase(ctx context.Context, name string) (WorkerPhase, error) {
	body, status, err := c.WorkerStatus(ctx, name)
	if err != nil {
		return "", fmt.Errorf("worker status query failed: %w", err)
	}
	if status != 200 {
		return "", fmt.Errorf("worker status returned %d", status)
	}
	var parsed workerStatusBody
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("worker status parse failed: %w", err)
	}
	if parsed.Status.Phase == "" {
		return "", fmt.Errorf("worker status has no phase field")
	}
	return WorkerPhase(parsed.Status.Phase), nil
}

// PreDispatchCheck 判断一个 worker 是否可派发:
//   - Running               → OK,无需动作
//   - Pending / Starting    → 轮询等它到 Running(上限 60s)
//   - Sleeping / Stopped    → POST ensure-ready,再轮询(上限 30s)
//   - Failed / Stopping     → 不做自动修复,直接 blocked(需人工介入/重建)
//
// 调用方自行决定 blocked 之后怎么办(标记任务、告警……)。
func (c *Client) PreDispatchCheck(ctx context.Context, workerName string) HealthGateResult {
	start := time.Now()
	result := HealthGateResult{Worker: workerName, Action: "none"}

	phase, err := c.fetchPhase(ctx, workerName)
	if err != nil {
		result.Phase = ""
		result.Error = err.Error()
		result.Action = "blocked"
		result.DurationMs = time.Since(start).Milliseconds()
		return result
	}
	result.Phase = phase

	switch phase {
	case PhaseRunning:
		// 已经健康 —— 无需任何动作
		result.DurationMs = time.Since(start).Milliseconds()
		return result

	case PhaseSleeping, PhaseStopped:
		// 尝试唤醒
		result.Action = "ensure-ready"
		if _, status, err := c.EnsureReady(ctx, workerName); err != nil || status >= 300 {
			if err != nil {
				result.Error = fmt.Sprintf("ensure-ready failed: %v", err)
			} else {
				result.Error = fmt.Sprintf("ensure-ready returned %d", status)
			}
			result.Action = "blocked"
			result.DurationMs = time.Since(start).Milliseconds()
			return result
		}
		// 轮询到 Running(唤醒预算 30s)
		if c.pollUntilRunning(ctx, workerName, 30*time.Second) {
			result.Phase = PhaseRunning
			result.Recovered = true
			result.DurationMs = time.Since(start).Milliseconds()
			return result
		}
		// 超时未恢复
		result.Phase, _ = c.fetchPhase(ctx, workerName)
		result.Action = "blocked"
		result.Error = "ensure-ready sent but worker did not reach Running within 30s"
		result.DurationMs = time.Since(start).Milliseconds()
		return result

	case PhasePending, PhaseStarting:
		// 还在启动 —— 等它(上限 60s)
		if c.pollUntilRunning(ctx, workerName, 60*time.Second) {
			result.Phase = PhaseRunning
			result.Recovered = true
			result.DurationMs = time.Since(start).Milliseconds()
			return result
		}
		result.Phase, _ = c.fetchPhase(ctx, workerName)
		result.Action = "blocked"
		result.Error = "worker did not reach Running within 60s (still " + string(result.Phase) + ")"
		result.DurationMs = time.Since(start).Milliseconds()
		return result

	case PhaseFailed:
		// Failed 需要人工介入或重建 —— 不尝试自动修复
		result.Action = "blocked"
		result.Error = "worker is in Failed phase; needs manual intervention or recreation"
		result.DurationMs = time.Since(start).Milliseconds()
		return result

	case PhaseStopping:
		// 正在优雅停机 —— 稍等一下再看
		time.Sleep(5 * time.Second)
		newPhase, _ := c.fetchPhase(ctx, workerName)
		result.Phase = newPhase
		if newPhase == PhaseRunning {
			result.DurationMs = time.Since(start).Milliseconds()
			return result
		}
		result.Action = "blocked"
		result.Error = "worker is Stopping (now " + string(newPhase) + ")"
		result.DurationMs = time.Since(start).Milliseconds()
		return result

	default:
		result.Action = "blocked"
		result.Error = "unknown phase: " + string(phase)
		result.DurationMs = time.Since(start).Milliseconds()
		return result
	}
}

// pollUntilRunning 每 3s 读一次相位,直到 Running 或超时。到达 Running 返回 true。
func (c *Client) pollUntilRunning(ctx context.Context, name string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(3 * time.Second):
			phase, err := c.fetchPhase(ctx, name)
			if err == nil && phase == PhaseRunning {
				return true
			}
		}
	}
	return false
}
