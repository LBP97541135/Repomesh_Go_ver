package web

import (
	"net/http"

	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/issues"
)

// confirmScopeSelection 是选仓门的批量确认端点(spec 2026-09-20 §3.2):
// POST /api/projects/{pid}/issues/{iid}/scope/selection。
// 服务层在一个事务里完成校验 → 双表写(issue_repository_scope 与
// issue_content_scope,整组同一把 scope_revision)→ 门 CAS 置 resolved;
// 这里只做解码与回执。空仓库数组 422;revision 不匹配或仓不在项目 409;
// 幂等键重放 200(status=replayed)。
func confirmScopeSelection(w http.ResponseWriter, r *http.Request, service *issues.Service, claims access.ProjectPrincipal) error {
	var body struct {
		RepositoryIDs                   []string `json:"repositoryIds"`
		DecidedBy                       string   `json:"decidedBy"`
		IdempotencyKey                  string   `json:"idempotencyKey"`
		ExpectedCreationContextRevision string   `json:"expectedCreationContextRevision"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		return err
	}
	receipt, err := service.ConfirmScopeSelection(r.Context(), claims, issues.ScopeSelectionCommand{
		ProjectID:                       r.PathValue("projectId"),
		IssueID:                         r.PathValue("issueId"),
		RepositoryIDs:                   body.RepositoryIDs,
		DecidedBy:                       body.DecidedBy,
		IdempotencyKey:                  body.IdempotencyKey,
		ExpectedCreationContextRevision: body.ExpectedCreationContextRevision,
	})
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, receipt)
	return nil
}
