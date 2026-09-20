package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"repomesh.local/repomesh/internal/access"
)

// registerPlanSnapshotRoutes 暴露"某一版计划快照"的读面。
//
// 2026-09-20：前端 `fetchPlanGraphEdges` 一直在打 `GET /api/plans/{planId}/versions/{n}`
// —— 那是 Python 时代的接口，**Go 后端从未注册过**，于是工作台每 2.5 秒收一个 404
// （nginx 实测 90 秒 49 条）。数据其实就在 `public.plans.task_dag->'dag'` 里
// （nodes/edges 都有，边还带 interface/agreement 语义），所以按 as-built 形状补上：
// **不做新表、不编数据**，读不到就 404。
//
// 鉴权与其它项目域读面一致（会话 + 项目成员），路径沿用前端已有的形状。
func registerPlanSnapshotRoutes(mux *http.ServeMux, auth Auth, pipeline Pipeline) {
	if pipeline.Pool == nil {
		return
	}
	pool := pipeline.Pool
	mux.HandleFunc("GET /api/plans/{planId}/versions/{version}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if auth.Service == nil {
			writeProjectError(w, &access.Failure{Status: http.StatusServiceUnavailable, Code: "AUTH_NOT_CONFIGURED"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		if _, err := auth.Service.AuthenticateProjectRequest(ctx, cookie(r, sessionCookie), r.Header.Get("X-CSRF-Token"), false); err != nil {
			writeProjectError(w, err)
			return
		}
		planID := r.PathValue("planId")
		version, err := strconv.Atoi(strings.TrimPrefix(r.PathValue("version"), "v"))
		if err != nil || version < 1 {
			writeProjectError(w, &access.Failure{Status: http.StatusNotFound, Code: "PLAN_SNAPSHOT_NOT_FOUND"})
			return
		}
		view, err := readPlanSnapshot(ctx, pool, planID, version)
		if errors.Is(err, pgx.ErrNoRows) {
			writeProjectError(w, &access.Failure{Status: http.StatusNotFound, Code: "PLAN_SNAPSHOT_NOT_FOUND"})
			return
		}
		if err != nil {
			writeProjectError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	})
}

// planSnapshotView 与前端 `PlanSnapshotView` 对齐（前端只消费 graph_edges，
// 其余字段照实给，免得以后再补一次）。
type planSnapshotView struct {
	ID              string           `json:"id"`
	ProjectID       string           `json:"project_id"`
	PlanVersion     int              `json:"plan_version"`
	RequirementText string           `json:"requirement_text"`
	Integration     string           `json:"integration_method"`
	TaskDAG         []map[string]any `json:"task_dag"`
	ExecutionPlanID string           `json:"execution_plan_id"`
	GraphEdges      []map[string]any `json:"graph_edges"`
}

type planSnapshotQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func readPlanSnapshot(ctx context.Context, pool planSnapshotQuerier, planID string, version int) (planSnapshotView, error) {
	var (
		view    planSnapshotView
		dagRaw  []byte
		batches []byte
		planVer string
		planIDOut *string
		issueID *string
	)
	err := pool.QueryRow(ctx, `SELECT id::text, project_id::text, plan_version, COALESCE(requirement_text,''),
		   COALESCE(integration_method,''), COALESCE(task_dag,'{}'::jsonb), COALESCE(execution_plan_id::text,'')
		FROM public.plans WHERE id=$1::uuid`, planID).Scan(
		&view.ID, &view.ProjectID, &planVer, &view.RequirementText, &view.Integration, &dagRaw, &view.ExecutionPlanID)
	if err != nil {
		return planSnapshotView{}, err
	}
	_ = batches
	_ = planIDOut
	_ = issueID
	// 版本号在库里是文本（'v1'），对不上就当这一版不存在 —— 不猜。
	got, convErr := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(planVer), "v"))
	if convErr != nil || got != version {
		return planSnapshotView{}, pgx.ErrNoRows
	}
	view.PlanVersion = got
	var dag map[string]any
	if json.Unmarshal(dagRaw, &dag) != nil {
		dag = map[string]any{}
	}
	view.TaskDAG = []map[string]any{}
	if nodes, ok := dag["nodes"].([]any); ok {
		for _, n := range nodes {
			if m, ok := n.(map[string]any); ok {
				view.TaskDAG = append(view.TaskDAG, m)
			}
		}
	}
	view.GraphEdges = []map[string]any{}
	if edges, ok := dag["edges"].([]any); ok {
		for _, e := range edges {
			if m, ok := e.(map[string]any); ok {
				view.GraphEdges = append(view.GraphEdges, m)
			}
		}
	}
	return view, nil
}
