package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"repomesh.local/repomesh/internal/discovery"
)

// 事件流节奏:库内每 2s 快拍一次推差量(无 WAL/LISTEN-NOTIFY 的简单做法),
// 30s 心跳防中间层掐空闲连接。差量判定靠快拍指纹,指纹不变就不发。
const (
	eventPollEvery = 2 * time.Second
	eventHeartbeat = 30 * time.Second
	eventStallWind = 15 * time.Minute // 与 discovery.Stalls 同一窗口(stalls.go)
)

// registerEventsRoutes wires the SSE stream (Phase 4):
// GET /api/events/stream?issueId=…&taskId=…
// issueId is the stream scope; taskId optionally focuses task_status events on
// one idempotency-ledger receipt. Authenticates with the session cookie like
// every other read route. Unconfigured discovery skips registration.
func registerEventsRoutes(mux *http.ServeMux, auth Auth, discoveryAPI Discovery) {
	if discoveryAPI.Service == nil {
		return
	}
	guard := func(w http.ResponseWriter, r *http.Request) error {
		if auth.Service == nil {
			return &accessFailure{status: 503, code: "auth_not_configured"}
		}
		_, err := auth.Service.AuthenticateProjectRequest(r.Context(), cookie(r, sessionCookie), r.Header.Get("X-CSRF-Token"), false)
		return err
	}
	mux.HandleFunc("GET /api/events/stream", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if err := guard(w, r); err != nil {
			writeHumanControlError(w, err)
			return
		}
		serveIssueEventStream(w, r, discoveryAPI.Service)
	})
}

// serveIssueEventStream runs one SSE connection to its end: hello on connect,
// then a 2s DB poll that pushes diffs (discovery_step / task_status /
// worker_health), a 30s :keepalive comment, and a clean exit once the client
// hangs up (request context cancellation) or a write fails.
func serveIssueEventStream(w http.ResponseWriter, r *http.Request, service *discovery.Service) {
	issueID, taskID := r.URL.Query().Get("issueId"), r.URL.Query().Get("taskId")
	if issueID == "" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "validation_failed", "message": "issueId query parameter is required"})
		return
	}
	// 流的寿命长于 server.go 的 90s WriteTimeout:清掉本连接的写截止,
	// 否则每 90 秒被强制断一次(客户端虽会重连,但那是噪音不是设计)。
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{}) // 包裹过的 ResponseWriter(测试桩)可能不支持:按默认超时继续
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("X-Accel-Buffering", "no") // 反代(Vite dev proxy/nginx)禁缓冲,逐事件下发
	w.WriteHeader(http.StatusOK)

	var id int64
	send := func(event string, data any) bool {
		id++
		raw, err := json.Marshal(data)
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", id, event, raw); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	if !send("hello", map[string]any{"issue_id": issueID, "task_id": taskID, "poll_ms": eventPollEvery.Milliseconds()}) {
		return
	}

	ctx := r.Context()
	pollTick := time.NewTicker(eventPollEvery)
	defer pollTick.Stop()
	heartbeat := time.NewTicker(eventHeartbeat)
	defer heartbeat.Stop()

	var prev *issueEventSnapshot
	poll := func() bool {
		snap, err := pollIssueEvents(ctx, service, issueID, taskID)
		if err != nil {
			return true // 单次读失败不拆流:下一拍再试,断流交给客户端重连
		}
		for _, event := range snap.eventsFrom(prev) {
			if !send(event.name, event.data) {
				return false
			}
		}
		prev = snap
		return true
	}
	if !poll() { // 连上先给一拍首状,别让客户端干等 2s
		return
	}
	for {
		select {
		case <-ctx.Done(): // 客户端断开:就地返回,连接随 handler 结束关闭
			return
		case <-shuttingDown():
			// 进程在关停（部署重启）：主动收摊。
			// 不返回的话，`http.Server.Shutdown` 会一直等这条永不结束的流，
			// 耗满 5 秒期限再强杀 —— 那 5 秒就是站点 502 的窗口。
			// 客户端会看到流结束并自动重连（EventSource 内建行为），重连时
			// 新进程通常已经起来了。
			return
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ":keepalive\n\n"); err != nil {
				return
			}
			if rc.Flush() != nil {
				return
			}
		case <-pollTick.C:
			if !poll() {
				return
			}
		}
	}
}

// streamEvent is one SSE frame ready to send.
type streamEvent struct {
	name string
	data any
}

// issueEventSnapshot is one 2s poll of the repomesh_issues schema for a single
// issue. Signatures drive the diff: unchanged signature → no event.
type issueEventSnapshot struct {
	view      map[string]any // 与 GET /issues/{id}/discovery 同一 View 投影
	viewSig   string         // 整个读投影的指纹:任何块变了都算 discovery_step
	task      map[string]any // 无可报任务时为 nil
	taskSig   string
	health    []map[string]any // worker_health 信号(失败步/疑似中断)
	healthSig string
}

// pollIssueEvents reads the discovery state once (own schema, same read path
// as GET /discovery) and folds it into an event snapshot.
func pollIssueEvents(ctx context.Context, service *discovery.Service, issueID, taskID string) (*issueEventSnapshot, error) {
	tx, err := service.BeginRead(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	state, err := service.LoadRead(ctx, tx, issueID)
	if err != nil {
		return nil, err
	}
	snap := &issueEventSnapshot{view: state.View()}
	if raw, err := json.Marshal(snap.view); err == nil {
		sum := sha256.Sum256(raw)
		snap.viewSig = hex.EncodeToString(sum[:])
	}
	snap.task, snap.taskSig = taskStatusOf(state, snap.view, taskID)
	snap.health = healthSignalsOf(state, snap.view)
	if raw, err := json.Marshal(snap.health); err == nil {
		sum := sha256.Sum256(raw)
		snap.healthSig = hex.EncodeToString(sum[:])
	}
	return snap, nil
}

// eventsFrom diffs this snapshot against the previous one (nil = first poll)
// and returns the SSE frames to send. The first poll reports current state so
// a fresh subscriber is immediately up to date; afterwards only changes go out.
func (snap *issueEventSnapshot) eventsFrom(prev *issueEventSnapshot) []streamEvent {
	out := []streamEvent{}
	if prev == nil || snap.viewSig != prev.viewSig {
		out = append(out, streamEvent{name: "discovery_step", data: snap.view})
	}
	if snap.task != nil && (prev == nil || snap.taskSig != prev.taskSig) {
		out = append(out, streamEvent{name: "task_status", data: snap.task})
	}
	// 信号清空也发一帧空表:前端才能把旧告警撤下来。
	if prev == nil || snap.healthSig != prev.healthSig {
		if len(snap.health) > 0 || prev != nil && len(prev.health) > 0 {
			out = append(out, streamEvent{name: "worker_health", data: map[string]any{
				"issue_id": snap.view["issue_id"], "signals": snap.health,
			}})
		}
	}
	return out
}

// taskStatusOf derives the task_status payload. Prefer the taskId's
// idempotency-ledger receipt (same source TaskView reads), else the read
// projection's running task. Empty signature = nothing to report.
func taskStatusOf(state *discovery.State, view map[string]any, taskID string) (map[string]any, string) {
	reportID, report := "", ""
	if taskID != "" {
		if receipt, ok := state.Idempotency[taskID].(map[string]any); ok {
			reportID = taskID
			report, _ = receipt["status"].(string)
			if report == "replayed" { // 与 TaskView 同一口径:重放即成功
				report = "succeeded"
			}
		}
	}
	if reportID == "" {
		if running, ok := view["running_task_id"].(*string); ok && running != nil {
			reportID, report = *running, "running"
		}
	}
	if reportID == "" {
		return nil, ""
	}
	return map[string]any{
		"issue_id": state.IssueID, "task_id": reportID, "status": report, "step": view["step"],
	}, reportID + "|" + report
}

// healthSignalsOf mirrors stalls.go per issue: error blocks are "failed"
// steps; step 4 approved but plan-less past the stall window is "stalled".
// 与观测告警面同一语义,事件流不发明第二套健康判定。
func healthSignalsOf(state *discovery.State, view map[string]any) []map[string]any {
	signals := []map[string]any{}
	blocks := map[int]map[string]any{1: state.Analysis, 2: state.Candidates, 3: state.Classification, 4: state.Plan}
	for step := 1; step <= 4; step++ {
		block := blocks[step]
		if block == nil {
			continue
		}
		failure, _ := block["error"].(map[string]any)
		if failure == nil {
			continue
		}
		message, _ := failure["message"].(string)
		if message == "" {
			message = "步骤执行失败(未给出原因)"
		}
		signals = append(signals, map[string]any{"step": step, "kind": "failed", "message": message})
	}
	if step, _ := view["step"].(int); step == 4 && time.Since(state.UpdatedAt) > eventStallWind {
		if approval, _ := state.Approval["state"].(string); approval == "approved" {
			signals = append(signals, map[string]any{
				"step": 4, "kind": "stalled", "message": "分档已批准但计划超过 15 分钟未生成,执行者疑似中断",
			})
		}
	}
	return signals
}
