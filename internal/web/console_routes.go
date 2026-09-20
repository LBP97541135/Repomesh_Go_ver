package web

import (
	"net/http"

	"repomesh.local/repomesh/internal/console"
)

// Console carries the directory read services into the web layer.
type Console struct {
	Service *console.Service
}

// registerConsoleRoutes wires the contract v0.2 console directory endpoints
// (organizations / repositories / teams / agents) with session auth.
func registerConsoleRoutes(mux *http.ServeMux, auth Auth, consoleAPI Console) {
	if consoleAPI.Service == nil {
		return
	}
	guard := func(w http.ResponseWriter, r *http.Request) (string, error) {
		if auth.Service == nil {
			return "", &accessFailure{status: 503, code: "auth_not_configured"}
		}
		principal, err := auth.Service.AuthenticateProjectRequest(r.Context(), cookie(r, sessionCookie), r.Header.Get("X-CSRF-Token"), false)
		if err != nil {
			return "", err
		}
		return principal.ActorID(), nil
	}
	// register 把**调用者身份**一并交给处理器。
	//
	// 2026-09-19 账号隔离：目录类读面（组织/智能体/团队/仓库）此前全部**全库返回**
	// ——公有部署（一账号一空间）下，任何登录账号都能看到别人的空间与智能体。
	// 身份必须传下去，裁剪才有依据。
	register := func(pattern string, handler func(http.ResponseWriter, *http.Request, string)) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			actor, err := guard(w, r)
			if err != nil {
				writeHumanControlError(w, err)
				return
			}
			handler(w, r, actor)
		})
	}
	withRuntime := func(r *http.Request) bool {
		return r.URL.Query().Get("with_runtime") != "false"
	}

	register("GET /api/console/organizations", func(w http.ResponseWriter, r *http.Request, actor string) {
		result, err := consoleAPI.Service.Organizations(r.Context(), actor)
		if err != nil {
			writeHumanControlError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	// GET /api/setup/status —— 平台就绪检查（2026-09-19 补：前端一直在打，
	// Go 从未实现 → 404 → ConsoleShell 的 setupReady 恒 false）。
	// 复用 console 的会话守卫（前端只在登录后调用）。
	register("GET /api/setup/status", func(w http.ResponseWriter, r *http.Request, actor string) {
		_ = actor
		view, err := consoleAPI.Service.SetupStatus(r.Context())
		if err != nil {
			writeHumanControlError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	})

	// GET /api/setup/coding-agents —— Coding Agent 探测（迁移 3 的漏项）。
	// 前端一直在打这条，Go 从未实现 → 404 → 设置页「Coding Agent 适配器」永远空着，
	// 还挂一句"该功能在当前服务端版本尚未就绪"。
	registerCodingAgentProbe(register)

	register("GET /api/console/agents", func(w http.ResponseWriter, r *http.Request, actor string) {
		result, err := consoleAPI.Service.Agents(r.Context(), actor, withRuntime(r))
		if err != nil {
			writeHumanControlError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	register("GET /api/console/teams", func(w http.ResponseWriter, r *http.Request, actor string) {
		result, err := consoleAPI.Service.Teams(r.Context(), actor, withRuntime(r))
		if err != nil {
			writeHumanControlError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	register("GET /api/console/repositories", func(w http.ResponseWriter, r *http.Request, actor string) {
		result, err := consoleAPI.Service.Repositories(r.Context(), actor)
		if err != nil {
			writeHumanControlError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
}
