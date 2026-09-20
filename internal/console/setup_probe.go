package console

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// probeAgentTeams 真探 AgentTeams Controller。
//
// 2026-09-20：此前 checks["agentteams"] **写死 false**，注释写着"当前部署未接" ——
// 而 Controller 现在就跑在同一台机器上、宿主机实测可达（HTTP 200）。写死等于把
// "明明跑着"报成"没接"。这里改成真探，三种结局都如实落 dependencies：
//
//	未接线（探针为 nil） / 可达（200） / 不可达（错误或非 200）
func (s *Service) probeAgentTeams(ctx context.Context, view *SetupStatusView) bool {
	if s.agentteams == nil {
		view.Dependencies = append(view.Dependencies, map[string]any{
			"name": "agentteams", "state": "unwired",
			"detail": "本部署未接线 Controller 探针",
		})
		return false
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	body, status, err := s.agentteams.ControllerHealth(probeCtx)
	switch {
	case err != nil:
		view.Dependencies = append(view.Dependencies, map[string]any{
			"name": "agentteams", "state": "unreachable", "detail": err.Error(),
		})
		return false
	case status != http.StatusOK:
		view.Dependencies = append(view.Dependencies, map[string]any{
			"name": "agentteams", "state": "unreachable",
			"detail": fmt.Sprintf("HTTP %d", status),
		})
		return false
	}
	view.Dependencies = append(view.Dependencies, map[string]any{
		"name": "agentteams", "state": "reachable", "detail": summarizeHealth(body),
	})
	return true
}

// summarizeHealth 把上游健康响应压成一行。解析不出结构化字段就原样截断 ——
// 不编字段（"status=ok" 这种结论必须有来源）。
func summarizeHealth(body []byte) string {
	parsed := map[string]any{}
	if json.Unmarshal(body, &parsed) == nil {
		if status, ok := parsed["status"].(string); ok && strings.TrimSpace(status) != "" {
			return "status=" + status
		}
	}
	text := strings.TrimSpace(string(body))
	if len(text) > 120 {
		text = text[:120] + "…"
	}
	if text == "" {
		return "空响应"
	}
	return text
}

// matrixConfigured 如实回答"本部署有没有配 Matrix 消息面"。
//
// 没有配置入口就是 false，并写明是"未配置"而不是"探测失败" —— 两者对使用者的
// 含义完全不同（一个是"要配"，一个是"配了但连不上"）。
func matrixConfigured(view *SetupStatusView) bool {
	for _, key := range []string{"MATRIX_HOMESERVER_URL", "MATRIX_ACCESS_TOKEN", "REPOMESH_MATRIX_URL"} {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			view.Dependencies = append(view.Dependencies, map[string]any{
				"name": "matrix", "state": "configured", "detail": "由 " + key + " 配置",
			})
			return true
		}
	}
	view.Dependencies = append(view.Dependencies, map[string]any{
		"name": "matrix", "state": "unconfigured",
		"detail": "本部署未配置 Matrix 消息面（没有 MATRIX_* 环境变量）",
	})
	return false
}

// runtimeFor 探一个 agent 的**运行时**（AgentTeams Controller 的 worker status）。
//
// 2026-09-20：`Agents(with_runtime=true)` 此前把 `runtime` 一律置 nil —— 于是设置页
// 的「连接健康」对 6 个 worker 只显示"可达 0 · 不可达 0 · 无事实 6"。Controller
// 明明可达（宿主机实测 200），runtime 却一个字节都没有。这里真探：
//
//	资源名缺失 → nil（问不出运行时，不编）；
//	探到 200 → reachable=true + Controller 回报的 phase（没回报就 phase=null）；
//	错误 / 非 200 → reachable=false + 具体原因。
//
// 前端 runtimeDisplay 读的就是 reachable / phase（+ 可选 kind），字段名逐字对齐。
func (s *Service) runtimeFor(ctx context.Context, name string) *map[string]any {
	if s.agentteams == nil || strings.TrimSpace(name) == "" {
		return nil
	}
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	body, status, err := s.agentteams.WorkerStatus(probeCtx, name)
	switch {
	case err != nil:
		block := map[string]any{"reachable": false, "phase": nil, "detail": err.Error()}
		return &block
	case status != http.StatusOK:
		block := map[string]any{"reachable": false, "phase": nil, "detail": fmt.Sprintf("HTTP %d", status)}
		return &block
	}
	block := map[string]any{"reachable": true, "source": "agentteams-controller", "phase": nil}
	parsed := map[string]any{}
	if json.Unmarshal(body, &parsed) == nil {
		if phase, ok := parsed["phase"].(string); ok && strings.TrimSpace(phase) != "" {
			block["phase"] = phase
		}
	}
	return &block
}
