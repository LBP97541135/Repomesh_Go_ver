package issues

import (
	"context"
	"strings"

	"repomesh.local/repomesh/internal/access"
)

// AppendRepository 把一个仓库追加进 issue 的仓库范围。
//
// 这是 ③「执行中人工打断 / 动态引入新仓库」的落点之一：人确认之后，范围才真的扩张。
// 2026-09-20 之前 issue_repository_scope **只在建 issue 时写入**（insertWorkScope），
// 没有任何追加路径 —— 范围是钉死的，这也是"动态引入新仓库"整条能力缺失的一环。
//
// 两条前置条件都**显式检查并给出可自救的错误**，不把库层的外键报错直接甩给用户：
//   · 仓库必须已挂在本项目上（project_repositories）。挂仓库是另一个动作
//     （项目接仓库），这里不替调用方偷偷挂 —— 混在一起会让"范围扩张"与
//     "项目成员变化"两件事分不清；
//   · issue 必须存在且未删除。
//
// 幂等：同一 (issue, repository) 已在范围内时不做任何事（PK 挡住），返回 nil。
// 每次真正插入都带一把新的 scope_revision —— 范围变更有据可查。
func (s *Service) AppendRepository(ctx context.Context, principal access.ProjectPrincipal, projectID, issueID, repositoryID string) error {
	repo := strings.TrimSpace(repositoryID)
	if repo == "" {
		return failure(422, "REPOSITORY_REQUIRED")
	}
	tx, err := s.beginCreate(ctx)
	if err != nil {
		return err
	}
	defer rollbackTx(tx)
	if err := s.authorization.LockProjectPrincipal(ctx, tx, principal); err != nil {
		return err
	}
	if _, err := readProjectRow(ctx, tx, projectID, principal.ActorID()); err != nil {
		return err
	}
	var issueExists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM repomesh_issues.issues
		WHERE project_id=$1 AND id=$2 AND removed_at IS NULL)`, projectID, issueID).Scan(&issueExists); err != nil {
		return unavailable()
	}
	if !issueExists {
		return failure(404, "RESOURCE_NOT_FOUND")
	}
	var attached bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM repomesh_projects.project_repositories
		WHERE project_id=$1 AND repository_id=$2)`, projectID, repo).Scan(&attached); err != nil {
		return unavailable()
	}
	if !attached {
		return failure(409, "REPOSITORY_NOT_IN_PROJECT")
	}
	scopeRevision, err := newID("srev_")
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_issues.issue_repository_scope
		(issue_id, repository_id, project_id, scope_revision) VALUES ($1,$2,$3,$4)
		ON CONFLICT (issue_id, repository_id) DO NOTHING`,
		issueID, repo, projectID, scopeRevision); err != nil {
		return unavailable()
	}
	if err := tx.Commit(ctx); err != nil {
		return unavailable()
	}
	return nil
}