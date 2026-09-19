// dispatch_gate.go 把派发前健康闸接到 HTTP 面(Phase 2,2026-09-18)。
// 它是独立端点(而非塞进 DispatchDual),这样前端能在真正派任务之前先拿到
// 健康检查结果,blocked 时展示恢复卡片,而不是把任务派给一个死掉的 worker。
package web

import (
	"encoding/json"
	"net/http"

	"repomesh.local/repomesh/internal/access"
)

// registerDispatchGate 注册 POST /api/agentteams/dispatch-gate。
// Body:{"worker": "agt-worker-xxx"} → HealthGateResult JSON。
// action=="blocked" 时用 409 返回:可派发但需要先处理,前端据此走恢复卡片。
func registerDispatchGate(mux *http.ServeMux, auth Auth, at AgentTeams) {
	registerProjectRoute(mux, "POST /api/agentteams/dispatch-gate", auth,
		func(w http.ResponseWriter, r *http.Request, _ access.ProjectPrincipal) error {
			if at.Client == nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{
					"detail": "AgentTeams controller not configured",
				})
				return nil
			}
			var body struct {
				Worker string `json:"worker"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Worker == "" {
				writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
					"detail": "worker name required",
				})
				return nil
			}
			result := at.Client.PreDispatchCheck(r.Context(), body.Worker)
			status := http.StatusOK
			if result.Action == "blocked" {
				status = http.StatusConflict // 409:dispatchable but needs attention
			}
			writeJSON(w, status, result)
			return nil
		})
}
