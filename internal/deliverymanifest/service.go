// Package deliverymanifest 组装**跨仓交付的一致版本清单**（评委建议②）。
//
// 评委原话：企业需要明确一次交付包含哪些仓库提交、使用什么 Schema、测试结果对应哪套
// 环境；建议关联需求、仓库版本集合、迁移版本、数据基线、分支与验证记录，**保留失败
// 尝试**（重跑不得覆盖原始证据），对重复事件与并发任务使用幂等标识与状态校验；展示时
// 能从一次交付结果展开完整版本清单，定位失败发生在哪个仓库与数据接口。
//
// 这里做的是"**快照**"：一次交付落一份 manifest + 每个仓库一行 entry。同一把幂等键
// 重放返回原快照；换键重跑落**新的一份**，旧的那份原样留着（失败尝试不丢）。
// 拿不到的事实留空（没跑过分支验证就是 not_run、没有数据库改动就没有迁移版本），不编。
package deliverymanifest

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Service 组装并读取交付清单。
type Service struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

// BuildCommand 建一份清单。IdempotencyKey 必填：同一把键只落一份。
type BuildCommand struct {
	ProjectID      string
	IssueID        string
	PlanID         string
	CreatedBy      string
	IdempotencyKey string
}

// EvidenceView 是清单里的一条测试证据摘录。
type EvidenceView struct {
	Kind    string `json:"kind"`
	Passed  bool   `json:"passed"`
	Summary string `json:"summary"`
}

// EntryView 是清单里一个仓库那一行。
type EntryView struct {
	RepositoryID     string         `json:"repositoryId"`
	RepositoryName   string         `json:"repositoryName"`
	CommitSHA        string         `json:"commitSha,omitempty"`
	BranchRef        string         `json:"branchRef,omitempty"`
	PullRequestURL   string         `json:"pullRequestUrl,omitempty"`
	Migrations       []string       `json:"migrations"`
	DatabaseBaseline string         `json:"databaseBaseline,omitempty"`
	DatabaseBranch   string         `json:"databaseBranch,omitempty"`
	DatabaseProvider string         `json:"databaseProvider,omitempty"`
	ValidationStatus string         `json:"validationStatus"`
	FailureStage     string         `json:"failureStage,omitempty"`
	FailureDetail    string         `json:"failureDetail,omitempty"`
	TestEvidence     []EvidenceView `json:"testEvidence"`
}

// ManifestView 是清单的读投影。
type ManifestView struct {
	ID              string      `json:"id"`
	ProjectID       string      `json:"projectId"`
	IssueID         string      `json:"issueId"`
	PlanID          string      `json:"planId,omitempty"`
	PlanVersion     string      `json:"planVersion"`
	RequirementText string      `json:"requirementText"`
	Status          string      `json:"status"`
	FailureSummary  string      `json:"failureSummary,omitempty"`
	CreatedAt       time.Time   `json:"createdAt"`
	Entries         []EntryView `json:"entries"`
}

// Build 组装并落一份清单（幂等）。
func (s *Service) Build(ctx context.Context, command BuildCommand) (ManifestView, error) {
	if command.ProjectID == "" || command.IssueID == "" || command.IdempotencyKey == "" {
		return ManifestView{}, fmt.Errorf("deliverymanifest: project, issue and idempotency key are required")
	}
	if existing, err := s.byKey(ctx, command.IdempotencyKey); err == nil {
		return existing, nil
	} else if err != pgx.ErrNoRows {
		return ManifestView{}, err
	}

	planID := command.PlanID
	var planVersion, requirementText string
	var batches []byte
	err := s.pool.QueryRow(ctx, `SELECT id::text, plan_version, requirement_text, execution_batches
		FROM public.plans WHERE ($1 = '' OR id::text = $1) AND issue_id = $2
		ORDER BY plan_version DESC LIMIT 1`, planID, command.IssueID).
		Scan(&planID, &planVersion, &requirementText, &batches)
	if err != nil {
		return ManifestView{}, fmt.Errorf("deliverymanifest: 该 issue 还没有计划，无法建交付清单: %w", err)
	}

	names, err := s.repositoryNames(ctx, planID, batches)
	if err != nil {
		return ManifestView{}, err
	}
	entries := make([]EntryView, 0, len(names))
	for _, name := range names {
		entry, err := s.buildEntry(ctx, command.ProjectID, planID, name)
		if err != nil {
			return ManifestView{}, err
		}
		entries = append(entries, entry)
	}

	status, summary := summarize(entries)
	manifestID, err := newID()
	if err != nil {
		return ManifestView{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ManifestView{}, fmt.Errorf("deliverymanifest: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `INSERT INTO public.delivery_manifests
		(id, project_id, issue_id, plan_id, plan_version, requirement_text, status,
		 failure_summary, idempotency_key, request_hash, created_by)
		VALUES ($1,$2::uuid,$3,$4::uuid,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (idempotency_key) DO NOTHING`,
		manifestID, command.ProjectID, command.IssueID, planID, planVersion, requirementText,
		status, summary, command.IdempotencyKey, hashOf(command), command.CreatedBy); err != nil {
		return ManifestView{}, fmt.Errorf("deliverymanifest: insert manifest: %w", err)
	}
	for _, entry := range entries {
		evidence, err := json.Marshal(entry.TestEvidence)
		if err != nil {
			return ManifestView{}, err
		}
		migrations, err := json.Marshal(entry.Migrations)
		if err != nil {
			return ManifestView{}, err
		}
		entryID, err := newID()
		if err != nil {
			return ManifestView{}, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO public.delivery_manifest_entries
			(id, manifest_id, repository_id, repository_name, commit_sha, branch_ref, pull_request_url,
			 migrations, database_baseline, database_branch, database_provider, validation_status,
			 failure_stage, failure_detail, test_evidence)
			VALUES ($1,$2::uuid,$3,$4,$5,$6,$7,$8::jsonb,$9,$10,$11,$12,$13,$14,$15::jsonb)`,
			entryID, manifestID, entry.RepositoryID, entry.RepositoryName, entry.CommitSHA,
			entry.BranchRef, entry.PullRequestURL, string(migrations), entry.DatabaseBaseline,
			entry.DatabaseBranch, entry.DatabaseProvider, entry.ValidationStatus,
			entry.FailureStage, entry.FailureDetail, string(evidence)); err != nil {
			return ManifestView{}, fmt.Errorf("deliverymanifest: insert entry: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return ManifestView{}, fmt.Errorf("deliverymanifest: commit: %w", err)
	}
	return s.Get(ctx, manifestID)
}

// Latest 返回该 issue 最近一份清单（没有就 pgx.ErrNoRows）。
func (s *Service) Latest(ctx context.Context, projectID, issueID string) (ManifestView, error) {
	var id string
	err := s.pool.QueryRow(ctx, `SELECT id::text FROM public.delivery_manifests
		WHERE project_id=$1::uuid AND issue_id=$2 ORDER BY created_at DESC LIMIT 1`,
		projectID, issueID).Scan(&id)
	if err != nil {
		return ManifestView{}, err
	}
	return s.Get(ctx, id)
}

// Get 读一份清单（含每个仓库那一行）。
func (s *Service) Get(ctx context.Context, manifestID string) (ManifestView, error) {
	view := ManifestView{}
	var planID *string
	err := s.pool.QueryRow(ctx, `SELECT id::text, project_id::text, issue_id, plan_id::text,
		   plan_version, requirement_text, status, failure_summary, created_at
		FROM public.delivery_manifests WHERE id=$1::uuid`, manifestID).Scan(
		&view.ID, &view.ProjectID, &view.IssueID, &planID, &view.PlanVersion,
		&view.RequirementText, &view.Status, &view.FailureSummary, &view.CreatedAt)
	if err != nil {
		return ManifestView{}, err
	}
	if planID != nil {
		view.PlanID = *planID
	}
	rows, err := s.pool.Query(ctx, `SELECT repository_id, repository_name, commit_sha, branch_ref,
		   pull_request_url, migrations, database_baseline, database_branch, database_provider,
		   validation_status, failure_stage, failure_detail, test_evidence
		FROM public.delivery_manifest_entries WHERE manifest_id=$1::uuid ORDER BY repository_name`,
		manifestID)
	if err != nil {
		return ManifestView{}, fmt.Errorf("deliverymanifest: read entries: %w", err)
	}
	defer rows.Close()
	view.Entries = []EntryView{}
	for rows.Next() {
		entry := EntryView{}
		var migrations, evidence []byte
		if err := rows.Scan(&entry.RepositoryID, &entry.RepositoryName, &entry.CommitSHA,
			&entry.BranchRef, &entry.PullRequestURL, &migrations, &entry.DatabaseBaseline,
			&entry.DatabaseBranch, &entry.DatabaseProvider, &entry.ValidationStatus,
			&entry.FailureStage, &entry.FailureDetail, &evidence); err != nil {
			return ManifestView{}, fmt.Errorf("deliverymanifest: scan entry: %w", err)
		}
		entry.Migrations = []string{}
		_ = json.Unmarshal(migrations, &entry.Migrations)
		entry.TestEvidence = []EvidenceView{}
		_ = json.Unmarshal(evidence, &entry.TestEvidence)
		view.Entries = append(view.Entries, entry)
	}
	return view, rows.Err()
}

func (s *Service) byKey(ctx context.Context, key string) (ManifestView, error) {
	var id string
	if err := s.pool.QueryRow(ctx,
		`SELECT id::text FROM public.delivery_manifests WHERE idempotency_key=$1`, key).Scan(&id); err != nil {
		return ManifestView{}, err
	}
	return s.Get(ctx, id)
}

// repositoryNames 取这次交付涉及的仓库名：优先用计划的批次快照（那就是"这次交付"的
// 仓库集合），批次为空时退回任务行。
func (s *Service) repositoryNames(ctx context.Context, planID string, batches []byte) ([]string, error) {
	names := []string{}
	seen := map[string]bool{}
	var decoded [][]string
	if len(batches) > 0 {
		_ = json.Unmarshal(batches, &decoded)
	}
	for _, batch := range decoded {
		for _, name := range batch {
			if name != "" && !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	if len(names) > 0 {
		return names, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT repository_id FROM public.tasks
		WHERE plan_id=$1::uuid AND status <> 'superseded' AND repository_id <> ''`, planID)
	if err != nil {
		return nil, fmt.Errorf("deliverymanifest: read tasks: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	return names, rows.Err()
}

// buildEntry 汇总一个仓库那一行：代码侧（PR/分支/提交）、数据库侧（迁移/基线/分支/
// provider/结论）、测试证据，以及失败定位。
func (s *Service) buildEntry(ctx context.Context, projectID, planID, name string) (EntryView, error) {
	entry := EntryView{RepositoryID: name, RepositoryName: name, Migrations: []string{}, TestEvidence: []EvidenceView{}}
	if err := s.pool.QueryRow(ctx, `SELECT r.id FROM repomesh_projects.repositories r
		JOIN repomesh_projects.project_repositories pr ON pr.repository_id = r.id
		WHERE pr.project_id=$1 AND r.owner || '/' || r.name = $2 LIMIT 1`,
		projectID, name).Scan(&entry.RepositoryID); err != nil && err != pgx.ErrNoRows {
		return entry, fmt.Errorf("deliverymanifest: resolve repository: %w", err)
	}

	rows, err := s.pool.Query(ctx, `SELECT COALESCE(cs.pr_url,''), COALESCE(sc.params->>'sha',''),
		   COALESCE(sc.params->>'branch','')
		FROM public.tasks t
		JOIN public.change_sets cs ON cs.task_id = t.id
		LEFT JOIN LATERAL (
		  SELECT params FROM public.scm_commands
		  WHERE change_set_id = cs.id AND command_type IN ('push','pull_request')
		  ORDER BY created_at DESC LIMIT 1
		) sc ON true
		WHERE t.plan_id=$1::uuid AND t.repository_id=$2 AND t.status <> 'superseded'`,
		planID, name)
	if err != nil {
		return entry, fmt.Errorf("deliverymanifest: read change sets: %w", err)
	}
	for rows.Next() {
		var prURL, sha, branch string
		if err := rows.Scan(&prURL, &sha, &branch); err != nil {
			rows.Close()
			return entry, err
		}
		if entry.PullRequestURL == "" && prURL != "" {
			entry.PullRequestURL = prURL
		}
		if entry.CommitSHA == "" && sha != "" {
			entry.CommitSHA = sha
		}
		if entry.BranchRef == "" && branch != "" {
			entry.BranchRef = branch
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return entry, err
	}

	var provider, branchRef, baseline, status, failureCode string
	var results []byte
	err = s.pool.QueryRow(ctx, `SELECT provider, COALESCE(provider_branch_ref,''),
		   COALESCE(source_database_ref,''), status, COALESCE(failure_code,''), COALESCE(results,'[]'::jsonb)
		FROM public.database_branch_validations
		WHERE project_id=$1::uuid AND repository_id=$2
		ORDER BY created_at DESC LIMIT 1`, projectID, entry.RepositoryID).
		Scan(&provider, &branchRef, &baseline, &status, &failureCode, &results)
	switch {
	case err == pgx.ErrNoRows:
		entry.ValidationStatus = "not_run"
	case err != nil:
		return entry, fmt.Errorf("deliverymanifest: read branch validation: %w", err)
	default:
		entry.ValidationStatus = status
		entry.DatabaseProvider = provider
		entry.DatabaseBranch = branchRef
		entry.DatabaseBaseline = baseline
		entry.Migrations = migrationStatements(results)
		if status != "passed" {
			entry.FailureStage = "database"
			entry.FailureDetail = failureCode
			if detail := firstMigrationError(results); detail != "" {
				entry.FailureDetail = detail
			}
		}
	}

	evidenceRows, err := s.pool.Query(ctx, `SELECT kind, passed, COALESCE(summary,'')
		FROM public.test_evidence WHERE plan_id=$1::uuid AND repository_id=$2
		ORDER BY created_at`, planID, name)
	if err != nil {
		return entry, fmt.Errorf("deliverymanifest: read test evidence: %w", err)
	}
	defer evidenceRows.Close()
	for evidenceRows.Next() {
		item := EvidenceView{}
		if err := evidenceRows.Scan(&item.Kind, &item.Passed, &item.Summary); err != nil {
			return entry, err
		}
		entry.TestEvidence = append(entry.TestEvidence, item)
		if !item.Passed && entry.FailureStage == "" {
			entry.FailureStage = "test"
			entry.FailureDetail = item.Kind + "：" + item.Summary
		}
	}
	if err := evidenceRows.Err(); err != nil {
		return entry, err
	}
	if entry.FailureStage == "" && entry.PullRequestURL == "" {
		entry.FailureStage = "delivery"
		entry.FailureDetail = "没有观测到该仓库的 PR（交付面没走到开 PR 那一步）"
	}
	return entry, nil
}

// summarize 汇总结论：只要有一个仓库有失败定位，整份清单就是 inconsistent。
func summarize(entries []EntryView) (string, string) {
	failed := []string{}
	for _, entry := range entries {
		if entry.FailureStage != "" {
			failed = append(failed, entry.RepositoryName+"("+entry.FailureStage+")")
		}
	}
	if len(failed) == 0 {
		return "consistent", ""
	}
	return "inconsistent", "以下仓库未达成一致：" + joinStrings(failed, "、")
}

func migrationStatements(results []byte) []string {
	statements := []string{}
	items := []map[string]any{}
	if len(results) == 0 || json.Unmarshal(results, &items) != nil {
		return statements
	}
	for _, item := range items {
		if statement, ok := item["statement"].(string); ok && statement != "" {
			statements = append(statements, statement)
		}
	}
	return statements
}

func firstMigrationError(results []byte) string {
	items := []map[string]any{}
	if len(results) == 0 || json.Unmarshal(results, &items) != nil {
		return ""
	}
	for _, item := range items {
		if message, _ := item["error"].(string); message != "" {
			statement, _ := item["statement"].(string)
			return "迁移失败：" + statement + " → " + message
		}
	}
	return ""
}
