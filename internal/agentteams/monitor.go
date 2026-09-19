// monitor.go 实现后台健康轮询循环(Phase 3,2026-09-18):定时拉
// GET /api/v1/workers,比对每个 worker 的相位变化并产出事件,供事件流
// (SSE)/发现链路消费。
//
// Monitor 是只读观察者 —— 它从不直接改 worker 状态。恢复动作(ensure-ready、
// 重建)一律走 Phase 2 的 PreDispatchCheck 派发闸,在下次派任务时执行。
//
// **目前没有接线**：主线(catbobyman/Repomesh_Go_ver)也从未在任何地方构造或
// 启动它 —— 全仓只有本文件提到 NewMonitor/Start。这里保持同样状态：组件完整
// 可用，但没人 `go monitor.Start(ctx)`，Changes() 也没有消费方(SSE 走的是
// issue_discoveries 的库轮询)。要用它得先决定"状态变更事件接到哪里"，
// 别默认它已经在跑。
package agentteams

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"
)

// MonitorConfig 控制后台循环。
type MonitorConfig struct {
	// Interval 是两拍轮询之间的间隔(缺省 30s)。
	Interval time.Duration
	// Logger 为 nil 时用 slog.Default()。
	Logger *slog.Logger
}

// WorkerSnapshot 是一个 worker 在某一时刻的相位快照。
type WorkerSnapshot struct {
	Name  string      `json:"name"`
	Phase WorkerPhase `json:"phase"`
	At    time.Time   `json:"at"`
}

// StateChange 在某 worker 相位与上一拍不同时产出。
type StateChange struct {
	Worker string      `json:"worker"`
	From   WorkerPhase `json:"from"`
	To     WorkerPhase `json:"to"`
	At     time.Time   `json:"at"`
}

// listWorkersResponse 是 GET /api/v1/workers 响应里我们只关心的最小形状。
type listWorkersResponse struct {
	Items []struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
		Status struct {
			Phase string `json:"phase"`
		} `json:"status"`
	} `json:"items"`
}

// Monitor 跑后台轮询循环。在 goroutine 里调 Start();ctx 取消即退出。
type Monitor struct {
	client *Client
	config MonitorConfig
	// snapshot 保存上一拍各 worker 的相位(按名字)。
	snapshot map[string]WorkerPhase
	// changes 由调用方消费(SSE 流、事件写入器)。
	changes chan StateChange
}

// NewMonitor 基于给定 client 创建 monitor。
func NewMonitor(client *Client, config MonitorConfig) *Monitor {
	if config.Interval <= 0 {
		config.Interval = 30 * time.Second
	}
	if config.Logger == nil {
		config.Logger = slog.Default()
	}
	return &Monitor{
		client:   client,
		config:   config,
		snapshot: make(map[string]WorkerPhase),
		changes:  make(chan StateChange, 32),
	}
}

// Changes 返回只读的状态变化通道。
func (m *Monitor) Changes() <-chan StateChange {
	return m.changes
}

// Snapshot 返回当前相位表的一份拷贝。
func (m *Monitor) Snapshot() map[string]WorkerPhase {
	out := make(map[string]WorkerPhase, len(m.snapshot))
	for k, v := range m.snapshot {
		out[k] = v
	}
	return out
}

// Start 运行轮询循环直到 ctx 取消。应以 goroutine 调用:`go monitor.Start(ctx)`。
func (m *Monitor) Start(ctx context.Context) {
	ticker := time.NewTicker(m.config.Interval)
	defer ticker.Stop()
	// 立即先拉一拍
	m.poll(ctx)
	for {
		select {
		case <-ctx.Done():
			m.config.Logger.Info("agentteams monitor stopped")
			return
		case <-ticker.C:
			m.poll(ctx)
		}
	}
}

// poll 拉全量 worker,与快照比差,产出状态变化。
func (m *Monitor) poll(ctx context.Context) {
	body, status, err := m.client.read(ctx, "GET", "/api/v1/workers")
	if err != nil || status != 200 {
		m.config.Logger.Warn("agentteams monitor poll failed",
			"error", err, "status", status)
		return
	}
	var list listWorkersResponse
	if err := json.Unmarshal(body, &list); err != nil {
		m.config.Logger.Warn("agentteams monitor parse failed", "error", err)
		return
	}

	// 组装当前状态
	current := make(map[string]WorkerPhase, len(list.Items))
	for _, item := range list.Items {
		name := item.Metadata.Name
		if name == "" {
			continue
		}
		current[name] = WorkerPhase(item.Status.Phase)
	}

	// 比差:产出相位迁移事件
	now := time.Now().UTC()
	for name, newPhase := range current {
		oldPhase, existed := m.snapshot[name]
		if !existed {
			// 新发现的 worker —— 发一帧初始状态
			m.emit(StateChange{Worker: name, From: "", To: newPhase, At: now})
		} else if oldPhase != newPhase {
			m.emit(StateChange{Worker: name, From: oldPhase, To: newPhase, At: now})
		}
	}

	// 检查消失的 worker(被删除)
	for name, oldPhase := range m.snapshot {
		if _, still := current[name]; !still {
			m.emit(StateChange{Worker: name, From: oldPhase, To: "", At: now})
		}
	}

	// 替换快照
	m.snapshot = current
}

// emit 把一条变化送进通道(非阻塞,通道满则丢弃)。
func (m *Monitor) emit(change StateChange) {
	select {
	case m.changes <- change:
		m.config.Logger.Info("worker phase change",
			"worker", change.Worker,
			"from", change.From,
			"to", change.To)
	default:
		// 通道满 —— 丢弃并记日志
		m.config.Logger.Warn("monitor changes channel full, dropping",
			"worker", change.Worker)
	}
}
