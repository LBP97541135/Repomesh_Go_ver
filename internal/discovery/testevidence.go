package discovery

import (
	"context"
	"fmt"
)

// TestEvidenceRow 是一行测试记录（public.test_evidence）。
type TestEvidenceRow struct {
	Kind         string  `json:"kind"`
	TaskID       string  `json:"task_id,omitempty"`
	RepositoryID string  `json:"repository_id,omitempty"`
	Script       string  `json:"script,omitempty"`
	Command      string  `json:"command,omitempty"`
	ExitCode     *int    `json:"exit_code,omitempty"`
	Passed       bool    `json:"passed"`
	Summary      string  `json:"summary,omitempty"`
	Producer     string  `json:"producer,omitempty"`
	CreatedAt    string  `json:"created_at"`
}

// TestEvidence 返回一个 issue 名下**全部**测试记录：每条任务的单点验收、每个
// DAG 节点（仓库）的集成验证、以及跨仓库联调与回归。
//
// 2026-09-20：这些事实此前只以一个退出码的形式存在（scm_commands 的 ci 行），
// 界面上"测试组"永远是写死的一行文案。这里把它们如实读出来 —— 没有记录就返回
// 空列表，界面显示"还没有记录"，而不是编一个通过。
func (s *Service) TestEvidence(ctx context.Context, issueID string) (map[string]any, error) {
	rows, err := s.pool.Query(ctx, `SELECT kind, COALESCE(task_id::text,''), COALESCE(repository_id,''),
		       COALESCE(script,''), COALESCE(command,''), exit_code, passed,
		       COALESCE(summary,''), COALESCE(producer,''),
		       to_char(created_at, 'YYYY-MM-DD"T"HH24:MI:SSOF')
		FROM public.test_evidence
		WHERE issue_id=$1
		ORDER BY created_at`, issueID)
	if err != nil {
		return nil, fmt.Errorf("discovery: test evidence: %w", err)
	}
	defer rows.Close()
	items := []TestEvidenceRow{}
	passed, failed := 0, 0
	for rows.Next() {
		var row TestEvidenceRow
		if err := rows.Scan(&row.Kind, &row.TaskID, &row.RepositoryID,
			&row.Script, &row.Command, &row.ExitCode, &row.Passed,
			&row.Summary, &row.Producer, &row.CreatedAt); err != nil {
			return nil, fmt.Errorf("discovery: test evidence scan: %w", err)
		}
		if row.Passed {
			passed++
		} else {
			failed++
		}
		items = append(items, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("discovery: test evidence rows: %w", err)
	}
	return map[string]any{
		"issue_id": issueID,
		"items":    items,
		"passed":   passed,
		"failed":   failed,
	}, nil
}
