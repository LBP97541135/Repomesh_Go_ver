package web

import (
	"net/http"
	"os/exec"
	"strings"
)

// setup_coding_agents.go 补上「Coding Agent 探测」这条读面（迁移 3 的漏项）。
//
// 2026-09-20：前端 `client.ts` 的 `getCodingAgents()` 一直在打
// `GET /api/setup/coding-agents`，而 Go 侧**从来没实现过这条路由** —— 404，然后
// `display.ts` 把 404 统一渲染成"该功能在当前服务端版本尚未就绪（404）"。于是设置页
// 的「Coding Agent 适配器」与「运行时种类」永远是空的，还挂着一句误导人的提示。
//
// 探测口径（照契约里那句 note）：**探的是 API 进程所在环境**，不是 Runner 容器。
// 只报探得到的事实：
//   - installed / executable：`exec.LookPath` 的真实结果；
//   - auth_status：不实际跑一次就判不出来 → 一律 "unknown"（不猜 authorized）；
//   - runnable_by_verified_driver：没有已验证驱动 → false；
//   - detail：原样写探到的原因（`binary_not_found` / `binary_found`），不翻译。

type codingAgentAdapterView struct {
	AdapterID                string  `json:"adapter_id"`
	DisplayName              string  `json:"display_name"`
	Installed                bool    `json:"installed"`
	Executable               *string `json:"executable"`
	AuthStatus               string  `json:"auth_status"`
	Detail                   *string `json:"detail"`
	ExecutionStatus          string  `json:"execution_status"`
	RunnableByVerifiedDriver bool    `json:"runnable_by_verified_driver"`
}

type codingAgentsProbe struct {
	Environment string                   `json:"environment"`
	Note        string                   `json:"note"`
	Adapters    []codingAgentAdapterView `json:"adapters"`
}

func probeCodingAgents() codingAgentsProbe {
	candidates := []struct{ id, display, binary string }{
		{"codex_cli", "Codex CLI", "codex"},
		{"claude_cli", "Claude Code CLI", "claude"},
	}
	adapters := make([]codingAgentAdapterView, 0, len(candidates))
	for _, candidate := range candidates {
		view := codingAgentAdapterView{
			AdapterID:   candidate.id,
			DisplayName: candidate.display,
			// 不实际运行一次就判不出授权状态：如实 unknown，不猜 authorized。
			AuthStatus: "unknown",
			// 没有"已验证驱动"这件事的登记面：如实 false。
			RunnableByVerifiedDriver: false,
			ExecutionStatus:          "unverified",
		}
		if path, err := exec.LookPath(candidate.binary); err == nil {
			view.Installed = true
			view.Executable = &path
			detail := "binary_found"
			view.Detail = &detail
		} else {
			detail := "binary_not_found"
			view.Detail = &detail
		}
		adapters = append(adapters, view)
	}
	return codingAgentsProbe{
		Environment: "repomesh-api",
		Note: strings.TrimSpace("探的是 API 进程所在环境，不是 Runner 容器；" +
			"授权状态需要实际运行一次才能判定，这里如实为 unknown；" +
			"没有已验证驱动时 runnable_by_verified_driver 为 false。"),
		Adapters: adapters,
	}
}

// registerCodingAgentProbe 挂在 console 的会话守卫下（与 /api/setup/status 同一族）。
func registerCodingAgentProbe(register func(pattern string, handler func(http.ResponseWriter, *http.Request, string))) {
	register("GET /api/setup/coding-agents", func(w http.ResponseWriter, r *http.Request, actor string) {
		_ = actor
		writeJSON(w, http.StatusOK, probeCodingAgents())
	})
}