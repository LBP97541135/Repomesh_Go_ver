package web

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/assembly"
	"repomesh.local/repomesh/internal/humancontrol"
)

// registerTopologyRoutes exposes the M4 assembly as the project topology API
// (Py: human_control.py /projects/topologies + automatic-topologies + {id}/topology).
func registerTopologyRoutes(mux *http.ServeMux, auth Auth, assemblySvc *assembly.Service, humanControlSvc *humancontrol.Service) {
	if assemblySvc == nil {
		return
	}
	// GET /api/agents —— 智能体花名册（org 作用域）。
	// 2026-09-19 补：前端 `api/agents.ts` 一直在打这条路径，而后端**从未注册**它
	// ——查询（internal/assembly/roster.go 的 Roster）与投影类型（AgentRosterRow，
	// 字段与前端逐字一致）早就写好了，只是没接出来，于是智能体页的花名册恒 404。
	// 这里用与 console 路由同一套会话鉴权接上（读操作，CSRF 传 false）。
	mux.HandleFunc("GET /api/agents", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if auth.Service == nil {
			writeHumanControlError(w, &accessFailure{status: 503, code: "auth_not_configured"})
			return
		}
		principal, err := auth.Service.AuthenticateProjectRequest(
			r.Context(), cookie(r, sessionCookie), r.Header.Get("X-CSRF-Token"), false)
		if err != nil {
			writeHumanControlError(w, err)
			return
		}
		rows, err := assemblySvc.Roster(r.Context(), principal.ActorID(),
			r.URL.Query().Get("role"), r.URL.Query().Get("repositoryId"), r.URL.Query().Get("status"))
		if err != nil {
			writeHumanControlError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, rows)
	})
	// 智能体写面（2026-09-19 补）：新建 / 删除。
	// 约定与其它写面一致——会话 + CSRF + Origin 严格相等。注意 registerProjectRoute
	// 只把 POST/PATCH 当写操作，**DELETE 不在其列**；这里显式把 DELETE 也按写处理，
	// 否则删除会绕过 CSRF。
	// agentWrite 现在把**调用者身份**一并交出来：智能体写面必须按调用者自己的
	// 空间收口（2026-09-19 账号隔离）。
	agentWrite := func(w http.ResponseWriter, r *http.Request) (string, bool) {
		if auth.Service == nil {
			writeHumanControlError(w, &accessFailure{status: 503, code: "auth_not_configured"})
			return "", false
		}
		if auth.Origin == "" || len(r.Header.Values("Origin")) != 1 || r.Header.Get("Origin") != auth.Origin {
			writeHumanControlError(w, &accessFailure{status: 403, code: "origin_rejected"})
			return "", false
		}
		principal, err := auth.Service.AuthenticateProjectRequest(
			r.Context(), cookie(r, sessionCookie), r.Header.Get("X-CSRF-Token"), true)
		if err != nil {
			writeHumanControlError(w, err)
			return "", false
		}
		return principal.ActorID(), true
	}
	// agentInSpace 校验该智能体落在调用者的空间里；不在就 404（不泄露"存在但不是你的"）。
	agentInSpace := func(w http.ResponseWriter, r *http.Request, actor, agentID string) bool {
		ok, err := auth.Service.AgentInOrganization(r.Context(), actor, agentID)
		if err != nil {
			writeHumanControlError(w, err)
			return false
		}
		if !ok {
			writeHumanControlError(w, &accessFailure{status: 404, code: "NOT_FOUND"})
			return false
		}
		return true
	}
	mux.HandleFunc("POST /api/agents", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		actor, ok := agentWrite(w, r)
		if !ok {
			return
		}
		var body struct {
			ProjectID    string `json:"projectId"`
			Role         string `json:"role"`
			RepositoryID string `json:"repositoryId"`
			Name         string `json:"name"`
		}
		if err := decodeBody(w, r, &body); err != nil {
			return
		}
		// 2026-09-20（迁移 0053）：编制的归属是**项目**，不是组织。
		// 2026-09-19 那次改的是「空间只认调用者自己的」（此前 organizationId 直接取自
		// 请求体）；但组织与账号 1:1，按组织建人会让同一账号下的多个项目共用编制。
		// 现在项目必须显式给出、且必须是调用者自己的——**不给兜底**：兜底正是脏数据的来源。
		if humanControlSvc == nil {
			writeHumanControlError(w, &accessFailure{status: 503, code: "HUMANCONTROL_NOT_CONFIGURED"})
			return
		}
		projectID, err := humanControlSvc.ResolveProjectScope(r.Context(), actor, body.ProjectID)
		if errors.Is(err, pgx.ErrNoRows) {
			writeHumanControlError(w, &accessFailure{status: 404, code: "NOT_FOUND"})
			return
		}
		if err != nil {
			writeHumanControlError(w, err)
			return
		}
		agentID, err := assemblySvc.CreateAgent(r.Context(), projectID, body.Role, body.RepositoryID, body.Name)
		if err != nil {
			writeHumanControlError(w, &accessFailure{status: 422, code: "VALIDATION_FAILED"})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"id": agentID})
	})
	mux.HandleFunc("DELETE /api/agents/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		actor, ok := agentWrite(w, r)
		if !ok {
			return
		}
		if !agentInSpace(w, r, actor, r.PathValue("id")) {
			return
		}
		if err := assemblySvc.DeleteAgent(r.Context(), r.PathValue("id")); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeHumanControlError(w, &accessFailure{status: 404, code: "NOT_FOUND"})
				return
			}
			writeHumanControlError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	// PATCH /api/agents/{id} —— 设置智能体的预设提示词与 CLI 工具（迁移 0035）。
	// 同一套写守卫（会话 + CSRF + Origin）；PATCH 本就在 registerProjectRoute 的
	// 写集合里，这里沿用显式守卫以保持一致。
	mux.HandleFunc("PATCH /api/agents/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		actor, ok := agentWrite(w, r)
		if !ok {
			return
		}
		if !agentInSpace(w, r, actor, r.PathValue("id")) {
			return
		}
		var body struct {
			Prompt  string `json:"prompt"`
			CLIKind string `json:"cliKind"`
		}
		if err := decodeBody(w, r, &body); err != nil {
			return
		}
		if err := assemblySvc.UpdateAgentProfile(r.Context(), r.PathValue("id"), body.Prompt, body.CLIKind); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeHumanControlError(w, &accessFailure{status: 404, code: "NOT_FOUND"})
				return
			}
			writeHumanControlError(w, &accessFailure{status: 422, code: "VALIDATION_FAILED"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	// POST /api/projects/{projectId}/topologies —— 建团（编制落地）。
	//
	// 2026-09-20（迁移 0053）：**组织退出业务**。此前这里吃请求体的 organizationId，
	// 留空时还兜底成 projectId（而 projectId 不是组织 id，装配必然失败）——隔壁的
	// /api/agents 早已改成"只认调用者自己的空间"，这里却仍信客户端。
	// 现在作用域一律取路径里的 projectId：组织由服务端从项目反查（assembly.resolveOrganization），
	// 总领导名字也由项目派生，客户端给不了第二个总领导。
	// 归属校验补上：写操作必须核实项目属于调用者（与 GET topology 同一把尺子）。
	registerProjectRoute(mux, "POST /api/projects/{projectId}/topologies", auth, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
		if humanControlSvc == nil {
			return &access.Failure{Status: 503, Code: "HUMANCONTROL_NOT_CONFIGURED"}
		}
		var command struct {
			Repositories   []string `json:"repositories"`
			WorkersPerRepo int      `json:"workersPerRepo"`
		}
		if err := decodeBody(w, r, &command); err != nil {
			return err
		}
		projectID, err := humanControlSvc.ResolveProjectScope(r.Context(), claims.ActorID(), r.PathValue("projectId"))
		if errors.Is(err, pgx.ErrNoRows) {
			return &access.Failure{Status: 404, Code: "NOT_FOUND"}
		}
		if err != nil {
			return err
		}
		result, err := assemblySvc.Assemble(r.Context(), assembly.AssemblyCommand{
			ProjectID:      projectID,
			Repositories:   command.Repositories,
			WorkersPerRepo: command.WorkersPerRepo,
		})
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusCreated, result)
		return nil
	})
	// GET /api/projects/{projectId}/topology —— 项目拓扑读面（前端
	// ProjectAgentTopologyView 逐字对应）。
	//
	// 2026-09-19 修：此前这里回的是 `{"items":[agents]}`（assembly.ListTopology 的
	// agent 行），**恒 200 且字段全对不上**。而前端拿它的有无判定监管策略草稿窗口
	// 开不开（§3.4：拓扑一落地，草稿就定死了）—— 于是永远判成「档案已锁死」，
	// 策略卡片只剩一个标题、连「配置」按钮都不出现：用户根本没有地方设卡点。
	//
	// 现在的语义与契约一致：
	//   404 = 还没有拓扑（监管策略尚未设定，不是错误，正是"来得及设"的窗口）；
	//   200 = 拓扑在场，execution_mode / required_checkpoints / human_grants 取自
	//         监管策略草稿（那三件事在这套实现里的唯一来源）。
	// 作用域收 issue id 与项目 id 两种（契约 §0 说 project_id 就是 issue_id），
	// 且只认调用者名下的项目，其余 404。
	registerProjectRoute(mux, "GET /api/projects/{projectId}/topology", auth, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
		if humanControlSvc == nil {
			return &access.Failure{Status: 503, Code: "HUMANCONTROL_NOT_CONFIGURED"}
		}
		projectID, err := humanControlSvc.ResolveProjectScope(r.Context(), claims.ActorID(), r.PathValue("projectId"))
		if errors.Is(err, pgx.ErrNoRows) {
			return &access.Failure{Status: 404, Code: "NOT_FOUND"}
		}
		if err != nil {
			return err
		}
		view, err := assemblySvc.ProjectTopology(r.Context(), projectID)
		if errors.Is(err, pgx.ErrNoRows) {
			return &access.Failure{Status: 404, Code: "NOT_FOUND"}
		}
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, view)
		return nil
	})
}
