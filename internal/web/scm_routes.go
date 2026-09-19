package web

import (
	"io"
	"net/http"
	"strings"

	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/observability"
	"repomesh.local/repomesh/internal/scm"
)

// PipelineSCM bundles the P1 SCM services and usage queries.
type PipelineSCM struct {
	SCM *scm.Service
	// Merger 是"能合并的手"（GitHub App 客户端）。nil = 部署没配 App 凭据，
	// 合并端点会如实回"服务端没有可用的 GitHub 凭据"，不假装成功。
	Merger      scm.PullMerger
	Observation *observability.Service
}

// registerSCMRoutes wires the webhook (anonymous but signature-verified) and
// the authenticated change-set lifecycle routes.
func registerSCMRoutes(mux *http.ServeMux, auth Auth, scmAPI PipelineSCM) {
	if scmAPI.SCM == nil {
		return
	}
	// Webhook: GitHub posts here; the signature header is the authentication.
	mux.HandleFunc("POST /api/delivery/github-webhook", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1024*1024))
		if err != nil {
			writePipelineError(w, 400, "INVALID_PAYLOAD")
			return
		}
		if !scmAPI.SCM.VerifySignature(body, r.Header.Get("X-Hub-Signature-256")) {
			writePipelineError(w, 403, "SIGNATURE_REJECTED")
			return
		}
		deliveryID := r.Header.Get("X-GitHub-Delivery")
		if deliveryID == "" {
			writePipelineError(w, 400, "MISSING_DELIVERY_ID")
			return
		}
		if err := scmAPI.SCM.Ingest(r.Context(), []byte(deliveryID), []byte(r.Header.Get("X-GitHub-Event")), body); err != nil {
			writePipelineError(w, 500, "INGEST_FAILED")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ingested"})
	})

	registerProjectRoute(mux, "POST /api/projects/{projectId}/change-sets", auth, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
		var command scm.FreezeCommand
		command.ProjectID = r.PathValue("projectId")
		if err := decodeBody(w, r, &command); err != nil {
			return err
		}
		csID, err := scmAPI.SCM.Freeze(r.Context(), command)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusCreated, map[string]string{"changeSetId": csID})
		return nil
	})
	registerProjectRoute(mux, "POST /api/projects/{projectId}/change-sets/{changeSetId}/events", auth, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
		var body struct {
			Kind    string `json:"kind"`
			Payload string `json:"payload"`
		}
		if err := decodeBody(w, r, &body); err != nil {
			return err
		}
		if err := scmAPI.SCM.RecordEvent(r.Context(), r.PathValue("changeSetId"), body.Kind, body.Payload); err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "recorded"})
		return nil
	})
	// 交付段收口：真的把 PR 合掉（闸门 fail-closed，见 scm.Merge 的三道门）。
	// 这是整条链上唯一的**外部副作用**，所以错误一律如实上抛、不吞不重试。
	registerProjectRoute(mux, "POST /api/projects/{projectId}/change-sets/{changeSetId}/merge", auth, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
		outcome, err := scmAPI.SCM.Merge(r.Context(), r.PathValue("projectId"), r.PathValue("changeSetId"),
			claims.ActorID(), scmAPI.Merger)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, outcome)
		return nil
	})
	registerProjectRoute(mux, "GET /api/projects/{projectId}/change-sets/{changeSetId}/merge-gate", auth, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
		gate, err := scmAPI.SCM.Gate(r.Context(), r.PathValue("changeSetId"))
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, gate)
		return nil
	})
	// 交付列车读面:按任务 id 列 change-sets(逗号分隔)。经 tasks.project_id
	// 收敛项目域,跨项目的任务 id 返回空而非泄漏。
	registerProjectRoute(mux, "GET /api/projects/{projectId}/change-sets", auth, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
		raw := strings.Split(r.URL.Query().Get("taskIds"), ",")
		taskIDs := make([]string, 0, len(raw))
		for _, id := range raw {
			if id = strings.TrimSpace(id); id != "" {
				taskIDs = append(taskIDs, id)
			}
		}
		items, err := scmAPI.SCM.ListByTasks(r.Context(), r.PathValue("projectId"), taskIDs)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
		return nil
	})

	// LLM usage cost summary (Py observability usage_query 对等).
	if scmAPI.Observation != nil {
		registerProjectRoute(mux, "GET /api/observability/usage", auth, func(w http.ResponseWriter, r *http.Request, claims access.ProjectPrincipal) error {
			summary, err := scmAPI.Observation.UsageSummary(r.Context(), r.URL.Query().Get("orgId"))
			if err != nil {
				return err
			}
			writeJSON(w, http.StatusOK, summary)
			return nil
		})
	}
}
