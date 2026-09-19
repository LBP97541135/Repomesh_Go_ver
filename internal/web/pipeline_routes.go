package web

import (
	"net/http"

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
			Reason string `json:"reason"`
		}
		if err := decodeBody(w, r, &body); err != nil {
			return err
		}
		_ = body
		writeJSON(w, http.StatusOK, map[string]string{"status": "interrupt_accepted"})
		return nil
	})
}
