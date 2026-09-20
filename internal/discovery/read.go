package discovery

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// BeginRead opens a read transaction over the issue schema.
func (s *Service) BeginRead(ctx context.Context) (pgx.Tx, error) {
	return s.pool.Begin(ctx)
}

// EnsureIssueRead verifies the issue exists inside a read transaction.
func (s *Service) EnsureIssueRead(ctx context.Context, tx pgx.Tx, issueID string) (bool, error) {
	var exists bool
	err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM repomesh_issues.issues WHERE id=$1)", issueID).Scan(&exists)
	return exists, err
}

// LoadRead loads the discovery state inside a read transaction; a missing
// row yields an empty state (200 with idle step per contract 7.2).
func (s *Service) LoadRead(ctx context.Context, tx pgx.Tx, issueID string) (*State, error) {
	st, err := s.load(ctx, tx, issueID)
	if err != nil {
		return nil, err
	}
	if st == nil {
		st = &State{IssueID: issueID}
	}
	if err := s.fillRunning(ctx, tx, st); err != nil {
		return nil, err
	}
	return st, nil
}

// fillRunning 把「在途的规划步」读进状态里（**读面专用**，写路径不调用）。
//
// 为什么单独一个查询、而不是并进 stateColumns：`planning_runs` 与
// `issue_discoveries` 是两张表、两条时间线。join 进去会让「发现链一行都还没有」
// 的 issue（刚建好、① 正在跑）被整行过滤掉 —— 而那恰恰是最需要显示「进行中」的
// 时刻。读不到（还没派发过）不算错误：如实留 0 / nil。
func (s *Service) fillRunning(ctx context.Context, tx pgx.Tx, st *State) error {
	var step int
	var runID *string
	err := tx.QueryRow(ctx, `SELECT step, run_id::text
		FROM repomesh_issues.planning_runs
		WHERE issue_id=$1 AND state='pending'
		ORDER BY created_at DESC LIMIT 1`, st.IssueID).Scan(&step, &runID)
	if err != nil {
		// 没有在途（pgx.ErrNoRows）、或这张表在更早的库上还不存在 —— 两种都
		// 只意味着「进行中」这项增量信息为空，不该让整个读面 500。
		return nil
	}
	st.RunningStep, st.RunningRunID = step, runID
	return nil
}

// IssueContext 返回该 issue 的项目、标题，以及项目的属主账号。
//
// 用途：审核单必须挂在**项目**上、标题要能给人看，而发现链的路由入参只有 issue id
// （契约里"project_id 就是 issue_id"那套等式在 Go 侧并不成立）。
//
// 属主是必须一起带回来的：审核台对**非管理员**只显示 `assignee = 自己` 的待审项
// （humancontrol.List），落单时不写 assignee 的话，人工卡点在审核台照样是空的。
func (s *Service) IssueContext(ctx context.Context, issueID string) (projectID, title, owner string, err error) {
	err = s.pool.QueryRow(ctx, `SELECT i.project_id, COALESCE(i.title,''), COALESCE(p.owner,'')
		FROM repomesh_issues.issues i
		LEFT JOIN repomesh_projects.projects p ON p.id = i.project_id
		WHERE i.id=$1`, issueID).Scan(&projectID, &title, &owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", "", nil
	}
	if err != nil {
		return "", "", "", fmt.Errorf("discovery: issue context: %w", err)
	}
	return projectID, title, owner, nil
}
