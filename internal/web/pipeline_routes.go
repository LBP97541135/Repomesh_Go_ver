package web

import (
	"net/http"
	"strings"

	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/assembly"
	"repomesh.local/repomesh/internal/tasks"
)

// registerPipelineRoutes exposes the M1-M9 services over HTTP. Every route is
// inside the authenticated project-route family; none are anonymous.
func registerPipelineRoutes(mux *http.ServeMux, auth Auth, pipeline Pipeline) {
	if pipeline.Tasks == nil {
		return
	}

	// ---- M4: organization assembly ----
	mux.HandleFunc("POST /api/organizations/{orgId}/assembly", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if pipeline.Assembly == nil {
			writePipelineError(w, 503, "SERVICE_NOT_CONFIGURED")
			return
		}
		if !authorizePipeline(w, r, auth) {
			return
		}
		var command struct {
			Repositories   []string `json:"repositories"`
			WorkersPerRepo int      `json:"workersPerRepo"`
			LeaderName     string   `json:"leaderName"`
		}
		if err := decodeBody(w, r, &command); err != nil {
			return
		}
		result, err := pipeline.Assembly.Assemble(r.Context(), assembly.AssemblyCommand{
			OrganizationID: r.PathValue("orgId"),
			Repositories:   command.Repositories,
			WorkersPerRepo: command.WorkersPerRepo,
			LeaderName:     command.LeaderName,
		})
		if err != nil {
			writePipelineError(w, 500, "ASSEMBLY_FAILED")
			return
		}
		writeJSON(w, http.StatusCreated, result)
	})

	// ---- M1: DAG plan control ----
	registerProjectRoute(mux, "POST /api/projects/{projectId}/plans", auth, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
		var command struct {
			RequirementText string              `json:"requirementText"`
			RequirementKey  string              `json:"requirementKey"`
			Batches         [][]string          `json:"batches"`
			DAG             map[string][]string `json:"dag"`
		}
		if err := decodeBody(w, r, &command); err != nil {
			return err
		}
		plan, err := pipeline.Tasks.CreatePlan(r.Context(), tasks.PlanWrite{
			ProjectID:       r.PathValue("projectId"),
			RequirementText: command.RequirementText,
			RequirementKey:  command.RequirementKey,
			Batches:         command.Batches,
			DAG:             command.DAG,
		})
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusCreated, plan)
		return nil
	})
	registerProjectRoute(mux, "GET /api/projects/{projectId}/plans/{planId}", auth, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
		plan, err := pipeline.Tasks.GetPlan(r.Context(), r.PathValue("planId"))
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, plan)
		return nil
	})
	// 任务树读面:该计划的在册任务(下发任务树的行数据)。计划归属先校验,
	// 跨项目的 planId 一律 404,不泄漏他库是否存在。
	registerProjectRoute(mux, "GET /api/projects/{projectId}/plans/{planId}/tasks", auth, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
		plan, err := pipeline.Tasks.GetPlan(r.Context(), r.PathValue("planId"))
		if err != nil {
			return err
		}
		if plan == nil || plan.ProjectID != r.PathValue("projectId") {
			return &access.Failure{Status: 404, Code: "RESOURCE_NOT_FOUND"}
		}
		items, err := pipeline.Tasks.ListPlanTasks(r.Context(), r.PathValue("planId"))
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
		return nil
	})
	registerProjectRoute(mux, "POST /api/projects/{projectId}/tasks/{taskId}/approve", auth, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
		var body struct {
			Summary string `json:"summary"`
		}
		if err := decodeBody(w, r, &body); err != nil {
			return err
		}
		// 2026-09-20：经理门通过时若没填小结，此前 result_summary 就留空 ——
		// 于是阶段历史里的「审核」永远是空的，而"谁在什么时候批了这条任务"这个
		// **事实**是真实发生过的，不该丢。这里如实补一句决策留痕（不编内容，
		// 只记事实本身）。
		if strings.TrimSpace(body.Summary) == "" {
			body.Summary = "经理通过（未填写小结）"
		}
		if err := pipeline.Tasks.ApproveStep(r.Context(), r.PathValue("taskId"), claims.ActorID(), body.Summary); err != nil {
			return err
		}
		// 经理批准 = 交付闸门的 review 那一项（闸门四项里唯一由人产生的一项）。
		// 记不上就记不上（fail-open）：审批本身已经落库，不能因为记账失败回滚它。
		if scmSvc := pipeline.SCMRoutes.SCM; scmSvc != nil {
			if changeSetID, csErr := scmSvc.ChangeSetForTask(r.Context(), r.PathValue("taskId")); csErr == nil && changeSetID != "" {
				_ = scmSvc.RecordEvent(r.Context(), changeSetID, "review",
					`{"actor":"`+claims.ActorID()+`","decision":"approved"}`)
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "done"})
		return nil
	})
	registerProjectRoute(mux, "POST /api/projects/{projectId}/tasks/{taskId}/reject", auth, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
		var body struct {
			Reason string `json:"reason"`
		}
		if err := decodeBody(w, r, &body); err != nil {
			return err
		}
		if err := pipeline.Tasks.RejectStep(r.Context(), r.PathValue("taskId"), claims.ActorID(), body.Reason); err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "rejected"})
		return nil
	})
	registerProjectRoute(mux, "POST /api/projects/{projectId}/plans/{planId}/interrupt", auth, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
		var body struct {
			Repository string `json:"repository"`
			Note       string `json:"note"`
		}
		if err := decodeBody(w, r, &body); err != nil {
			return err
		}
		// 2026-09-20：这条端点此前**什么都没做**就返回 {"status":"interrupt_accepted"}
		// —— 解出 body 随即 `_ = body` 丢掉，而它背后那套
		// tasks.EscalationService.InterruptPlanRepo（打断决策单 → onboarding →
		// 等待就绪 → 判定是否影响计划 → 有改动则开收集窗供重排 v2）除测试外
		// 没有任何调用方。这种"报成功但无动作"比能力缺失更坏：人会以为已经生效。
		//
		// 现在真接：真落决策单、真判就绪、真判是否影响计划，把 InterruptOutcome
		// 原样回给前端（含 ready / affectsPlan / affectedSet），**不粉饰**。
		if pipeline.Escalation == nil {
			return &access.Failure{Status: http.StatusNotImplemented, Code: "INTERRUPT_NOT_WIRED"}
		}
		if strings.TrimSpace(body.Repository) == "" {
			return &access.Failure{Status: http.StatusUnprocessableEntity, Code: "REPOSITORY_REQUIRED"}
		}
		key, err := projectIdempotencyKey(r)
		if err != nil {
			return err
		}
		outcome, err := pipeline.Escalation.InterruptPlanRepo(r.Context(), tasks.HumanInterrupt{
			PlanID:         r.PathValue("planId"),
			UserID:         claims.ActorID(),
			RepoName:       strings.TrimSpace(body.Repository),
			Note:           body.Note,
			IdempotencyKey: key,
		})
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, outcome)
		return nil
	})
}
