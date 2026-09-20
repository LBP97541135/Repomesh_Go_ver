package web

import (
	"net/http"
	"strings"

	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/tasks"
)

// registerPipelineRoutes exposes the M1-M9 services over HTTP. Every route is
// inside the authenticated project-route family; none are anonymous.
func registerPipelineRoutes(mux *http.ServeMux, auth Auth, pipeline Pipeline) {
	if pipeline.Tasks == nil {
		return
	}

	// 跨仓交付的一致版本清单（评委建议②）：读最近一份 / 建一份快照。
	registerDeliveryManifestRoutes(mux, auth, pipeline)
	registerPlanSnapshotRoutes(mux, auth, pipeline)
	// 「这条任务到底干了什么」：把工作区里 agent 的真实输出读出来（用户反复提的
	// "看不到 worker 内部工作记录"）。
	registerTaskOutputRoutes(mux, auth, pipeline)

	// ---- M4: 编制组装 ----
	//
	// 2026-09-20（迁移 0053）：原 `POST /api/organizations/{orgId}/assembly` **已删除**。
	// 它是**组织根**的路由：编制的作用域已经改成项目（组织只回答"这是哪个账号的数据"），
	// 组织不再参与业务分组。真身在 `POST /api/projects/{projectId}/topologies`
	// （topology_routes.go）——它有归属校验，而且总领导名字由项目派生。
	// 前端 `api/plans.ts` 的同名封装 assembleOrganization 没有任何调用方，一并删除。

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
	// 计划换代历史（A3）：每一次全量快照替换都留在 public.plans.revisions 里
	// （含触发它的那一跳 UpstreamRef、增删的仓库、创建/取代的任务数）。
	// 此前这条历史**只有落库没有读面** —— 计划换过几版、每版为什么换，界面上看不到。
	registerProjectRoute(mux, "GET /api/projects/{projectId}/plans/{planId}/revisions", auth, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
		plan, err := pipeline.Tasks.GetPlan(r.Context(), r.PathValue("planId"))
		if err != nil {
			return err
		}
		if plan == nil || plan.ProjectID != r.PathValue("projectId") {
			return &access.Failure{Status: 404, Code: "RESOURCE_NOT_FOUND"}
		}
		items, err := pipeline.Tasks.PlanRevisions(r.Context(), r.PathValue("planId"))
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
		// 判定"影响当前计划"= 收集窗已开、需要重排 v2。这里把**重排派发意图**
		// 登记进发现链（第 6 步），由 coordinator 派给 Leader agent 产出 v2。
		// 登记失败**不能吞**：判定了要重排却没人产 v2，返回 200 会让人以为计划已经动了。
		if outcome.AffectsPlan && pipeline.ReplanHook != nil {
			if err := pipeline.ReplanHook(r.Context(), r.PathValue("planId"), outcome.NodeID, outcome.AffectedSet); err != nil {
				return err
			}
			outcome.ReplanQueued = true
		}
		writeJSON(w, http.StatusOK, outcome)
		return nil
	})
}
