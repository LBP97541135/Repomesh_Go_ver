package web

import (
	"context"
	"net/http"

	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/issues"
	"repomesh.local/repomesh/internal/roomnotice"
)

// confirmScopeSelection 是选仓门的批量确认端点(spec 2026-09-20 §3.2):
// POST /api/projects/{pid}/issues/{iid}/scope/selection。
// 服务层在一个事务里完成校验 → 双表写(issue_repository_scope 与
// issue_content_scope,整组同一把 scope_revision)→ 门 CAS 置 resolved;
// 这里只做解码与回执。空仓库数组 422;revision 不匹配或仓不在项目 409;
// 幂等键重放 200(status=replayed)。
func confirmScopeSelection(w http.ResponseWriter, r *http.Request, service *issues.Service, claims access.ProjectPrincipal,
	rooms *roomnotice.Notifier, onScopeConfirmed func(ctx context.Context, projectID string, repositoryIDs []string)) error {
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
	// 门确认的两件后事(都在回执之后,失败不改判定):
	//   1) 房间留一条——谁选的、选了几个,人回看时知道范围是怎么定的;
	//   2) 唤醒入选仓库的团队——建队时 worker 是 Sleeping 的(spec §3.3),
	//      不唤醒就没有可接活的 runtime。唤醒是幂等+fire-and-forget。
	projectID := r.PathValue("projectId")
	if rooms != nil {
		rooms.Notify(r.Context(), r.PathValue("issueId"), "gate:"+r.PathValue("issueId")+":confirmed",
			scopeConfirmedNotice(body.DecidedBy, len(body.RepositoryIDs)))
	}
	if onScopeConfirmed != nil {
		onScopeConfirmed(r.Context(), projectID, body.RepositoryIDs)
	}
	return nil
}

// scopeConfirmedNotice 是选仓门确认进房的文案。
// 谁选的必须写清楚——"人勾的"与"AI 定的"在审计上不是一回事。
func scopeConfirmedNotice(decidedBy string, repositoryCount int) string {
	who := "人勾选"
	switch decidedBy {
	case "ai":
		who = "AI 定"
	case "timeout":
		who = "超时代选"
	}
	return "【RepoMesh】选仓门已确认：" + who + "，" + itoa(repositoryCount) + " 个仓库进入本次范围。"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := [20]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
