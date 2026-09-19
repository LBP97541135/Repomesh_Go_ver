package web

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"
	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/humancontrol"
)

// registerPolicyDraftRoutes 接出「监管策略草稿」的三个端点（迁移 5-1b / 0040）。
//
// 为什么必须有这三个端点：前端的「配置监管策略」弹窗（SupervisionPolicyDialog）
// 一直在打 /projects/{id}/policy-draft，而 Go 后端**从来没有注册过它** —— 全仓
// grep 为空。于是三档与卡点永远存不下来，物化只能按默认值建档案
// （线上实测：agent_teams 唯一一行 required_checkpoints=[]、review_requests 0 行），
// 而人工审核台读的正是那些卡点。用户看到的是「明明 issue 进了人工步骤，审核台却
// 永远没有待审事项」。
//
// 守卫与其它会话写面一致：会话 cookie + 写操作 CSRF + Origin 严格相等。
// 路径放在 /api（无 v1 段）与 topology 一族同前缀 —— 前端这两处都走 apiRequest
// （它会带 X-CSRF-Token），而旧的 sessionRequest 不带，写操作必被 CSRF 拒。
//
// projectId 接受**两种 id**：契约 §0 说「project_id 就是 issue_id」，工作台确实是
// 拿 issue id 在调；这里用 ResolveProjectScope 认，认不到按项目 id 再认一次，
// 都不在调用者名下就 404（不泄露「存在但不是你的」）。
func registerPolicyDraftRoutes(mux *http.ServeMux, auth Auth, control HumanControl) {
	if control.Service == nil {
		return
	}
	// guard 解析会话 + 解析作用域，一次给全两个调用方需要的值。
	guard := func(w http.ResponseWriter, r *http.Request) (string, string, bool) {
		if auth.Service == nil {
			writePolicyError(w, &accessFailure{status: 503, code: "auth_not_configured"})
			return "", "", false
		}
		write := r.Method != http.MethodGet && r.Method != http.MethodHead
		if write && (auth.Origin == "" || len(r.Header.Values("Origin")) != 1 || r.Header.Get("Origin") != auth.Origin) {
			writePolicyError(w, &accessFailure{status: 403, code: "origin_rejected"})
			return "", "", false
		}
		principal, err := auth.Service.AuthenticateProjectRequest(
			r.Context(), cookie(r, sessionCookie), r.Header.Get("X-CSRF-Token"), write)
		if err != nil {
			writePolicyError(w, err)
			return "", "", false
		}
		actor := principal.ActorID()
		projectID, err := control.Service.ResolveProjectScope(r.Context(), actor, r.PathValue("projectId"))
		if err != nil {
			writePolicyError(w, err)
			return "", "", false
		}
		return actor, projectID, true
	}

	mux.HandleFunc("GET /api/projects/{projectId}/policy-draft", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		_, projectID, ok := guard(w, r)
		if !ok {
			return
		}
		view, err := control.Service.PolicyDraft(r.Context(), projectID)
		if err != nil {
			writePolicyError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	})

	mux.HandleFunc("PUT /api/projects/{projectId}/policy-draft", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		actor, projectID, ok := guard(w, r)
		if !ok {
			return
		}
		var body struct {
			ExecutionMode       string                     `json:"execution_mode"`
			RequiredCheckpoints []string                   `json:"required_checkpoints"`
			HumanGrants         []humancontrol.PolicyGrant `json:"human_grants"`
		}
		if err := decodeBody(w, r, &body); err != nil {
			return
		}
		// 三个字段一律全传是前端的规矩；后端这里不替缺省值做主 ——
		// 省略字段等于让默认值悄悄参与决定监管强度。
		view, err := control.Service.PutPolicyDraft(r.Context(), actor, projectID, humancontrol.PolicyDraftCommand{
			ExecutionMode:       body.ExecutionMode,
			RequiredCheckpoints: body.RequiredCheckpoints,
			HumanGrants:         body.HumanGrants,
		})
		if err != nil {
			writePolicyError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	})

	mux.HandleFunc("DELETE /api/projects/{projectId}/policy-draft", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		_, projectID, ok := guard(w, r)
		if !ok {
			return
		}
		if err := control.Service.DeletePolicyDraft(r.Context(), projectID); err != nil {
			writePolicyError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// writePolicyError 在共享错误写出器之前拦两类策略专属错误。
//
//   - PolicyViolation → **422 + 域不变量原文**。这些原文一条都不该被用户看见
//     （前端的三档映射与它们一一对应，看见说明界面漏了一条约束），所以不翻译、
//     不归并 —— 原文正是修界面时要照着改的那句话；
//   - ErrPolicyFrozen → **409 + 人能读懂的那句**。它确实会发生在用户身上
//     （物化之后回来改策略），所以这一条要给一句有用的话，而不是域原文。
func writePolicyError(w http.ResponseWriter, err error) {
	var violation *humancontrol.PolicyViolation
	if errors.As(err, &violation) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
			"error": "policy_violation", "message": violation.Message,
		})
		return
	}
	if errors.Is(err, humancontrol.ErrPolicyFrozen) {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error":   "policy_frozen",
			"message": "监管策略已随首次物化定死：这个需求的卡点与审核人不能再改。要换强度就新建一个需求。",
		})
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		// 读：草稿没设过（前端把 404 当「未设定」这条事实，不是错误）；
		// 删：没得撤（后端选择说出来，而不是回一句轻快的 204）。
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "resource_not_found"})
		return
	}
	var failure *access.Failure
	if errors.As(err, &failure) {
		slog.Warn("policy draft request failed", "error", err.Error())
		writeJSON(w, failure.Status, map[string]string{"error": failure.Code})
		return
	}
	writeHumanControlError(w, err)
}

// registerAccountDirectory 接出 GET /api/auth/accounts —— 账号目录。
//
// 为什么是策略面的一部分：监管策略的每条授权都要指向一个**真实存在、能登录**的
// 账号（弹窗的「选人」下拉）。前端 authApi.accounts() 一直在打这条路径，而 Go
// 后端从未注册过它（全仓 grep 为空）→ 404 → 弹窗的选人下拉恒空 → 即便端点补齐了，
// 用户也选不出人来配卡点。
//
// 按调用者自己的空间裁剪（与 console 目录同一套隔离规则）：公有部署下，
// 任何登录账号都不该看到别人空间里的账号 —— 那是别人的身份。
func registerAccountDirectory(mux *http.ServeMux, auth Auth) {
	mux.HandleFunc("GET /api/auth/accounts", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if auth.Service == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "auth_not_configured"})
			return
		}
		principal, err := auth.Service.AuthenticateProjectRequest(
			r.Context(), cookie(r, sessionCookie), r.Header.Get("X-CSRF-Token"), false)
		if err != nil {
			writeHumanControlError(w, err)
			return
		}
		organization, err := auth.Service.OrganizationOf(r.Context(), principal.ActorID())
		if err != nil {
			writeHumanControlError(w, err)
			return
		}
		// 没有空间的账号只看到自己：空组织不能退化成「看到全部」。
		rows, err := auth.Service.AccountDirectory(r.Context(), principal.ActorID(), organization)
		if err != nil {
			writeHumanControlError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, rows)
	})
}
