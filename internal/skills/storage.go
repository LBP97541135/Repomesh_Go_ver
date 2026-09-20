package skill

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	Pool *pgxpool.Pool
}

type Skill struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Scenario        string    `json:"scenario"`
	TargetAgentRole string    `json:"target_agent_role"`
	CreatedBy       string    `json:"created_by"`
	CreatedAt       time.Time `json:"created_at"`
}

type SkillVersion struct {
	ID          string    `json:"id"`
	SkillID     string    `json:"skill_id"`
	Version     string    `json:"version"`
	Status      Status    `json:"status"`
	Content     string    `json:"content"`
	ContentHash string    `json:"content_hash"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type TestQuestion struct {
	ID         string         `json:"id"`
	SkillID    string         `json:"skill_id"`
	Kind       QuestionKind   `json:"kind"`
	Question   string         `json:"question"`
	Expected   map[string]any `json:"expected"`
	ProvidedBy string         `json:"provided_by"`
	CreatedAt  time.Time      `json:"created_at"`
}

type EvalRun struct {
	ID           string         `json:"id"`
	VersionID    string         `json:"version_id"`
	QuestionID   string         `json:"question_id"`
	Arm          string         `json:"arm"`
	BlindedLabel string         `json:"blinded_label"`
	Answer       map[string]any `json:"answer"`
	JudgedBy     *string        `json:"judged_by"`
	Result       string         `json:"result"`
	RunAt        time.Time      `json:"run_at"`
}

type McpPolicy struct {
	ServerName           string   `json:"server_name"`
	TimeoutSeconds       int      `json:"timeout_seconds"`
	MaxRetries           int      `json:"max_retries"`
	RetryableOnlyReads   bool     `json:"retryable_only_reads"`
	DegradedBlockWrites  bool     `json:"degraded_block_writes"`
	RequiredTaskFeatures []string `json:"required_task_features"`
}

var semverRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

func ValidSemver(v string) bool { return semverRe.MatchString(v) }

func ContentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// nullableUUID 把空串落成 SQL NULL：organization_id 为 NULL 表示**全局种子技能**。
func nullableUUID(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// SkillInSpace 判断该技能是否落在调用者的空间里（全局种子技能对所有空间可见）。
//
// 2026-09-19 账号隔离：技能的**按 id** 端点（版本列表、状态流转、绑定…）此前
// 完全不看归属 —— 只要拿到别人的 skill id 就能读它、改它。读面按名字裁剪只
// 挡住了"按名字查"，挡不住"按 id 打"。
func (s *Store) SkillInSpace(ctx context.Context, organizationID, skillID string) (bool, error) {
	var ok bool
	err := s.Pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM public.skills
			 WHERE id = $1 AND (organization_id IS NULL OR organization_id = $2::uuid))`,
		skillID, nullableUUID(organizationID)).Scan(&ok)
	return ok, err
}

// VersionInSpace 判断该版本所属技能是否落在调用者的空间里。
func (s *Store) VersionInSpace(ctx context.Context, organizationID, versionID string) (bool, error) {
	var ok bool
	err := s.Pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM public.skill_versions v
			 JOIN public.skills sk ON sk.id = v.skill_id
			 WHERE v.id = $1 AND (sk.organization_id IS NULL OR sk.organization_id = $2::uuid))`,
		versionID, nullableUUID(organizationID)).Scan(&ok)
	return ok, err
}

// VersionIDForApproval 把审批 id 解析回它评审的版本 id，供空间校验使用。
func (s *Store) VersionIDForApproval(ctx context.Context, approvalID string) (string, error) {
	var versionID string
	err := s.Pool.QueryRow(ctx, `SELECT version_id FROM public.skill_approvals WHERE id = $1`, approvalID).Scan(&versionID)
	return versionID, err
}

// SkillIDForSuggestion 把建议 id 解析回它针对的技能 id。
func (s *Store) SkillIDForSuggestion(ctx context.Context, suggestionID string) (string, error) {
	var skillID string
	err := s.Pool.QueryRow(ctx, `SELECT skill_id FROM public.skill_update_suggestions WHERE id = $1`, suggestionID).Scan(&skillID)
	return skillID, err
}

// VersionIDForBinding 把绑定 id 解析回它引用的版本 id。
func (s *Store) VersionIDForBinding(ctx context.Context, bindingID string) (string, error) {
	var versionID string
	err := s.Pool.QueryRow(ctx, `SELECT version_id FROM public.agent_skill_bindings WHERE id = $1`, bindingID).Scan(&versionID)
	return versionID, err
}

// SkillIDForQuestion 把测试题 id 解析回它所属的技能 id。
func (s *Store) SkillIDForQuestion(ctx context.Context, questionID string) (string, error) {
	var skillID string
	err := s.Pool.QueryRow(ctx, `SELECT skill_id FROM public.skill_test_questions WHERE id = $1`, questionID).Scan(&skillID)
	return skillID, err
}

// AgentInSpace 判断该智能体是否落在调用者的空间里（绑定列表按 agent 查询，
// 而 agent 是空间内对象）。
func (s *Store) AgentInSpace(ctx context.Context, organizationID, agentID string) (bool, error) {
	if agentID == "" || organizationID == "" {
		return false, nil
	}
	var ok bool
	// 用 id::text 比较而不是 $1::uuid：调用者可能传垃圾字符串，
	// 那样会得到 22P02 而不是干净的"不在你的空间里"。
	err := s.Pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM public.agents
			 WHERE id::text = $1
			   AND organization_id = (SELECT organization_id FROM repomesh_access.accounts WHERE id = $2))`,
		agentID, organizationID).Scan(&ok)
	return ok, err
}

// RegisterSkill 在指定空间里登记/更新一把技能；organizationID 为空表示
// **全局种子技能**（系统自带，所有人可见）。
//
// 2026-09-19 账号隔离：此前是全局 `ON CONFLICT (name) DO UPDATE` —— 公有部署
// （一账号一空间）下任何人都能按名字覆盖别人的技能。现在按 (空间, 名字) 定位；
// 名字唯一性改成两级部分唯一索引（见迁移 0039），所以这里不用 ON CONFLICT
// （部分索引的谓词与 NULL 比较纠缠），改成"先精确更新、没有再插入"。
func (s *Store) RegisterSkill(ctx context.Context, organizationID, name, scenario, targetRole, createdBy string) (*Skill, error) {
	org := nullableUUID(organizationID)
	tag, err := s.Pool.Exec(ctx, `
		UPDATE public.skills SET scenario=$3, target_agent_role=$4
		 WHERE name=$1 AND organization_id IS NOT DISTINCT FROM $2`,
		name, org, scenario, targetRole)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() > 0 {
		return s.GetSkillByName(ctx, organizationID, name)
	}
	row := s.Pool.QueryRow(ctx, `
		INSERT INTO public.skills (name, scenario, target_agent_role, created_by, organization_id)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, name, scenario, target_agent_role, created_by, created_at`,
		name, scenario, targetRole, createdBy, org)
	sk := &Skill{}
	if err := row.Scan(&sk.ID, &sk.Name, &sk.Scenario, &sk.TargetAgentRole, &sk.CreatedBy, &sk.CreatedAt); err != nil {
		return nil, err
	}
	return sk, nil
}

// GetSkillByName 只认「全局种子」或「调用者自己空间」里的技能，优先自己空间的同名项。
func (s *Store) GetSkillByName(ctx context.Context, organizationID, name string) (*Skill, error) {
	row := s.Pool.QueryRow(ctx,
		`SELECT id, name, scenario, target_agent_role, created_by, created_at
		 FROM public.skills
		 WHERE name = $1 AND (organization_id IS NULL OR organization_id = $2::uuid)
		 ORDER BY (organization_id IS NULL)
		 LIMIT 1`, name, nullableUUID(organizationID))
	sk := &Skill{}
	if err := row.Scan(&sk.ID, &sk.Name, &sk.Scenario, &sk.TargetAgentRole, &sk.CreatedBy, &sk.CreatedAt); err != nil {
		return nil, err
	}
	return sk, nil
}

// getSkillByIDScoped 按 id 取技能，**带组织作用域**（全局种子或调用者自己空间）。
//
// 为什么需要它：`getSkillByID` 不带作用域（它只按 id 查），拿它做写路径会绕过
// 空间隔离 —— 别处调用点自带守卫，这里不能假设。2026-09-20：控制台的
// 「登记新版本」传的是 **skill_id（uuid）**，而 `RegisterVersion` 按**名字**查
// （`GetSkillByName`），于是必 `skill_not_found` → 409，A/B 评估这条路走不到。
// 修法是两种口径都收，但按 id 收时必须同样过作用域。
func (s *Store) getSkillByIDScoped(ctx context.Context, organizationID, id string) (*Skill, error) {
	row := s.Pool.QueryRow(ctx,
		`SELECT id, name, scenario, target_agent_role, created_by, created_at
		 FROM public.skills
		 WHERE id = $1 AND (organization_id IS NULL OR organization_id = $2::uuid)`,
		id, nullableUUID(organizationID))
	sk := &Skill{}
	if err := row.Scan(&sk.ID, &sk.Name, &sk.Scenario, &sk.TargetAgentRole, &sk.CreatedBy, &sk.CreatedAt); err != nil {
		return nil, err
	}
	return sk, nil
}

// ListSkills 返回「全局种子 + 调用者自己空间」的技能（此前是全库一份）。
func (s *Store) ListSkills(ctx context.Context, organizationID string) ([]Skill, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT id, name, scenario, target_agent_role, created_by, created_at
		 FROM public.skills
		 WHERE organization_id IS NULL OR organization_id = $1::uuid
		 ORDER BY name`, nullableUUID(organizationID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Skill
	for rows.Next() {
		var sk Skill
		if err := rows.Scan(&sk.ID, &sk.Name, &sk.Scenario, &sk.TargetAgentRole, &sk.CreatedBy, &sk.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, sk)
	}
	return out, rows.Err()
}

func (s *Store) RegisterVersion(ctx context.Context, skillID, version, content, createdBy string) (*SkillVersion, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var dup string
	err = tx.QueryRow(ctx, `
		SELECT id FROM public.skill_versions
		WHERE skill_id = $1 AND version = $2 AND status IN ('draft','evaluating','canary','promoted')`,
		skillID, version).Scan(&dup)
	if err == nil {
		return nil, Refused("skill_version_conflict", "version %s already exists in an active state", version)
	}
	if err != pgx.ErrNoRows {
		return nil, err
	}

	row := tx.QueryRow(ctx, `
		INSERT INTO public.skill_versions (skill_id, version, status, content, content_hash, created_by)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, skill_id, version, status, content, content_hash, created_by, created_at, updated_at`,
		skillID, version, string(StatusDraft), content, ContentHash(content), createdBy)
	v := &SkillVersion{}
	if err := row.Scan(&v.ID, &v.SkillID, &v.Version, (*string)(&v.Status), &v.Content, &v.ContentHash, &v.CreatedBy, &v.CreatedAt, &v.UpdatedAt); err != nil {
		return nil, err
	}
	return v, tx.Commit(ctx)
}

func (s *Store) GetVersion(ctx context.Context, id string) (*SkillVersion, error) {
	row := s.Pool.QueryRow(ctx, `
		SELECT id, skill_id, version, status, content, content_hash, created_by, created_at, updated_at
		FROM public.skill_versions WHERE id = $1`, id)
	return scanVersion(row)
}

func (s *Store) ListVersions(ctx context.Context, skillID string) ([]SkillVersion, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT id, skill_id, version, status, content, content_hash, created_by, created_at, updated_at
		FROM public.skill_versions WHERE skill_id = $1 ORDER BY updated_at DESC`, skillID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SkillVersion
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

type rowScanner interface{ Scan(dest ...any) error }

func scanVersion(row rowScanner) (*SkillVersion, error) {
	v := &SkillVersion{}
	err := row.Scan(&v.ID, &v.SkillID, &v.Version, (*string)(&v.Status), &v.Content, &v.ContentHash, &v.CreatedBy, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return v, nil
}

func (s *Store) Transition(ctx context.Context, id string, to Status) (*SkillVersion, error) {
	tag, err := s.Pool.Exec(ctx, `
		UPDATE public.skill_versions
		SET status = $2, updated_at = now() WHERE id = $1`, id, string(to))
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, fmt.Errorf("skill version %s not found", id)
	}
	return s.GetVersion(ctx, id)
}

func (s *Store) RecordRun(ctx context.Context, versionID, questionID, arm, blindedLabel string,
	answer map[string]any, judgedBy *string, result string) (*EvalRun, error) {

	var status Status
	if err := s.Pool.QueryRow(ctx,
		`SELECT status FROM public.skill_versions WHERE id = $1`, versionID).Scan((*string)(&status)); err != nil {
		return nil, err
	}
	if status != StatusEvaluating && status != StatusCanary {
		return nil, Refused("skill_evaluation_refused",
			"evaluations are only accepted in evaluating or canary state, got %s", status)
	}

	row := s.Pool.QueryRow(ctx, `
		INSERT INTO public.skill_evaluation_runs
			(version_id, question_id, arm, blinded_label, answer, judged_by, result)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7)
		RETURNING id, version_id, question_id, arm, blinded_label, answer::text, judged_by, result, run_at`,
		versionID, questionID, arm, blindedLabel, mustJSON(answer), judgedBy, result)
	run := &EvalRun{}
	var answerText string
	if err := row.Scan(&run.ID, &run.VersionID, &run.QuestionID, &run.Arm, &run.BlindedLabel, &answerText, &run.JudgedBy, &run.Result, &run.RunAt); err != nil {
		return nil, err
	}
	run.Answer = parseJSON(answerText)

	if status == StatusCanary && result == ResultFail {
		if _, err := s.Transition(ctx, versionID, StatusRolledBack); err != nil {
			return nil, err
		}
	}
	return run, nil
}

func (s *Store) ListRuns(ctx context.Context, versionID string) ([]EvalRun, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT id, version_id, question_id, arm, blinded_label, answer::text, judged_by, result, run_at
		FROM public.skill_evaluation_runs WHERE version_id = $1 ORDER BY run_at`, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EvalRun
	for rows.Next() {
		var r EvalRun
		var answerText string
		if err := rows.Scan(&r.ID, &r.VersionID, &r.QuestionID, &r.Arm, &r.BlindedLabel, &answerText, &r.JudgedBy, &r.Result, &r.RunAt); err != nil {
			return nil, err
		}
		r.Answer = parseJSON(answerText)
		out = append(out, r)
	}
	return out, rows.Err()
}

// CleanGate mirrors Python's _require_clean_gate: across the whole history of the
// skill there must be at least one pass and zero fails.
func (s *Store) CleanGateOK(ctx context.Context, skillID string) (bool, error) {
	var pass, fail int
	err := s.Pool.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN r.result = 'pass' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN r.result = 'fail' THEN 1 ELSE 0 END), 0)
		FROM public.skill_evaluation_runs r
		JOIN public.skill_versions v ON v.id = r.version_id
		WHERE v.skill_id = $1`, skillID).Scan(&pass, &fail)
	if err != nil {
		return false, err
	}
	return pass >= 1 && fail == 0, nil
}

// CanaryWindowOK mirrors Python's _require_canary_window_pass: runs recorded after
// the version entered canary need at least one pass and zero fails.
func (s *Store) CanaryWindowOK(ctx context.Context, versionID string) (bool, error) {
	var pass, fail int
	err := s.Pool.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN r.result = 'pass' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN r.result = 'fail' THEN 1 ELSE 0 END), 0)
		FROM public.skill_evaluation_runs r
		WHERE r.version_id = $1 AND r.run_at >= (SELECT updated_at FROM public.skill_versions WHERE id = $1)`,
		versionID).Scan(&pass, &fail)
	if err != nil {
		return false, err
	}
	return pass >= 1 && fail == 0, nil
}

// ResolveCurrent: latest promoted wins; otherwise the newest canary.
func (s *Store) ResolveCurrent(ctx context.Context, skillID string) (*SkillVersion, error) {
	v, err := s.queryOneVersion(ctx, `
		SELECT id, skill_id, version, status, content, content_hash, created_by, created_at, updated_at
		FROM public.skill_versions
		WHERE skill_id = $1 AND status = 'promoted'
		ORDER BY updated_at DESC LIMIT 1`, skillID)
	if err != nil {
		return nil, err
	}
	if v != nil {
		return v, nil
	}
	return s.queryOneVersion(ctx, `
		SELECT id, skill_id, version, status, content, content_hash, created_by, created_at, updated_at
		FROM public.skill_versions
		WHERE skill_id = $1 AND status = 'canary'
		ORDER BY updated_at DESC LIMIT 1`, skillID)
}

func (s *Store) queryOneVersion(ctx context.Context, sql string, args ...any) (*SkillVersion, error) {
	rows, err := s.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	return scanVersion(rows)
}

func trimLower(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// ListMcpPolicies returns every MCP call policy ordered by server name.
func (s *Store) ListMcpPolicies(ctx context.Context) ([]McpPolicy, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT server_name, timeout_seconds, max_retries, retryable_only_reads, degraded_block_writes, required_task_features::text
		FROM public.mcp_server_policies ORDER BY server_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []McpPolicy
	for rows.Next() {
		var p McpPolicy
		var features string
		if err := rows.Scan(&p.ServerName, &p.TimeoutSeconds, &p.MaxRetries, &p.RetryableOnlyReads, &p.DegradedBlockWrites, &features); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(features), &p.RequiredTaskFeatures); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// FeatureEnabled returns the toggle; a missing row counts as enabled,
// matching the decision-chain store's behavior.
func (s *Store) FeatureEnabled(ctx context.Context, feature string) (bool, error) {
	var enabled bool
	err := s.Pool.QueryRow(ctx,
		`SELECT enabled FROM public.feature_settings WHERE feature = $1`, feature).Scan(&enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	return enabled, err
}

// SetFeature writes the toggle with its audit fields.
func (s *Store) SetFeature(ctx context.Context, feature string, enabled bool, updatedBy string) error {
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO public.feature_settings (feature, enabled, updated_by, updated_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (feature) DO UPDATE SET
		  enabled = EXCLUDED.enabled,
		  updated_by = EXCLUDED.updated_by,
		  updated_at = EXCLUDED.updated_at`,
		feature, enabled, updatedBy)
	return err
}

// --- Skill content serving -------------------------------------------------

// ResolveContent returns the SKILL.md content of the currently promoted (or
// newest canary) version for the named skill within the caller's organization.
// Returns ErrNoRows when the skill is unknown or has no active version.
func (s *Store) ResolveContent(ctx context.Context, organizationID, skillName string) (string, string, string, error) {
	sk, err := s.GetSkillByName(ctx, organizationID, skillName)
	if err != nil {
		return "", "", "", err
	}
	v, err := s.ResolveCurrent(ctx, sk.ID)
	if err != nil {
		return "", "", "", err
	}
	return v.Content, v.Version, v.ContentHash, nil
}

// --- MCP policy update -----------------------------------------------------

// UpdateMcpPolicy updates timeout, retries and degradation flags for a named
// MCP server policy. Returns ErrNoRows when the policy does not exist.
func (s *Store) UpdateMcpPolicy(ctx context.Context, serverID string, p McpPolicy) (*McpPolicy, error) {
	if p.TimeoutSeconds < 1 || p.TimeoutSeconds > 600 {
		return nil, Refused("mcp_policy_invalid", "timeout_seconds must be 1..600, got %d", p.TimeoutSeconds)
	}
	if p.MaxRetries < 0 || p.MaxRetries > 5 {
		return nil, Refused("mcp_policy_invalid", "max_retries must be 0..5, got %d", p.MaxRetries)
	}
	row := s.Pool.QueryRow(ctx, `
		UPDATE public.mcp_server_policies
		SET timeout_seconds = $2, max_retries = $3,
			retryable_only_reads = $4, degraded_block_writes = $5
		WHERE server_name = $1
		RETURNING server_name, timeout_seconds, max_retries, retryable_only_reads, degraded_block_writes, required_task_features::text`,
		serverID, p.TimeoutSeconds, p.MaxRetries, p.RetryableOnlyReads, p.DegradedBlockWrites)
	out := &McpPolicy{}
	var features string
	if err := row.Scan(&out.ServerName, &out.TimeoutSeconds, &out.MaxRetries,
		&out.RetryableOnlyReads, &out.DegradedBlockWrites, &features); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(features), &out.RequiredTaskFeatures); err != nil {
		out.RequiredTaskFeatures = []string{}
	}
	return out, nil
}

// --- Skill snapshots --------------------------------------------------------

// SnapshotEntry is one skill version pinned inside an organization snapshot.
type SnapshotEntry struct {
	SkillID     string `json:"skill_id"`
	Version     string `json:"version"`
	ContentHash string `json:"content_hash"`
}

// SkillSnapshot is an immutable set of skill versions pinned for a dispatch.
type SkillSnapshot struct {
	ID             string          `json:"id"`
	OrganizationID *string         `json:"organization_id"`
	Versions       []SnapshotEntry `json:"versions"`
	CreatedAt      time.Time       `json:"created_at"`
	SupersededAt   *time.Time      `json:"superseded_at"`
}

// CreateSnapshot pins the currently promoted (or newest canary) version of
// every skill visible to the organization into a new active snapshot.
func (s *Store) CreateSnapshot(ctx context.Context, organizationID string) (*SkillSnapshot, error) {
	entries, err := s.collectActiveEntries(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	blob, err := json.Marshal(entries)
	if err != nil {
		return nil, err
	}
	// Supersede any existing active snapshot for this org, then insert.
	if _, err := s.Pool.Exec(ctx, `
		UPDATE public.skill_snapshots SET superseded_at = now()
		 WHERE organization_id IS NOT DISTINCT FROM $1::uuid AND superseded_at IS NULL`,
		nullableUUID(organizationID)); err != nil {
		return nil, err
	}
	row := s.Pool.QueryRow(ctx, `
		INSERT INTO public.skill_snapshots (organization_id, versions)
		VALUES ($1::uuid, $2::jsonb)
		RETURNING id, organization_id::text, versions::text, created_at, superseded_at`,
		nullableUUID(organizationID), string(blob))
	return scanSnapshot(row)
}

// collectActiveEntries resolves the current version for every skill in scope.
func (s *Store) collectActiveEntries(ctx context.Context, organizationID string) ([]SnapshotEntry, error) {
	skills, err := s.ListSkills(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	var entries []SnapshotEntry
	for _, sk := range skills {
		v, err := s.ResolveCurrent(ctx, sk.ID)
		if err != nil {
			continue // skill has no promoted/canary version; skip it
		}
		entries = append(entries, SnapshotEntry{SkillID: sk.ID, Version: v.Version, ContentHash: v.ContentHash})
	}
	if entries == nil {
		entries = []SnapshotEntry{}
	}
	return entries, nil
}

// GetSnapshot returns one snapshot by id, or ErrNoRows.
func (s *Store) GetSnapshot(ctx context.Context, id string) (*SkillSnapshot, error) {
	row := s.Pool.QueryRow(ctx,
		`SELECT id, organization_id::text, versions::text, created_at, superseded_at
		 FROM public.skill_snapshots WHERE id = $1`, id)
	return scanSnapshot(row)
}

// ActiveSnapshot returns the current active snapshot for the organization,
// or ErrNoRows if none exists (caller should CreateSnapshot first).
func (s *Store) ActiveSnapshot(ctx context.Context, organizationID string) (*SkillSnapshot, error) {
	row := s.Pool.QueryRow(ctx, `
		SELECT id, organization_id::text, versions::text, created_at, superseded_at
		FROM public.skill_snapshots
		 WHERE organization_id IS NOT DISTINCT FROM $1::uuid AND superseded_at IS NULL
		 ORDER BY created_at DESC LIMIT 1`,
		nullableUUID(organizationID))
	return scanSnapshot(row)
}

// ListSnapshots returns all snapshots for the organization (active first).
func (s *Store) ListSnapshots(ctx context.Context, organizationID string) ([]SkillSnapshot, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT id, organization_id::text, versions::text, created_at, superseded_at
		FROM public.skill_snapshots
		 WHERE organization_id IS NOT DISTINCT FROM $1::uuid
		 ORDER BY superseded_at IS NULL DESC, created_at DESC`,
		nullableUUID(organizationID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SkillSnapshot
	for rows.Next() {
		snap, err := scanSnapshot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *snap)
	}
	return out, rows.Err()
}

// SnapshotInSpace checks whether the snapshot belongs to the caller's org (or is global).
func (s *Store) SnapshotInSpace(ctx context.Context, organizationID, snapshotID string) (bool, error) {
	var ok bool
	err := s.Pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM public.skill_snapshots
			 WHERE id = $1 AND (organization_id IS NULL OR organization_id = $2::uuid))`,
		snapshotID, nullableUUID(organizationID)).Scan(&ok)
	return ok, err
}

// SeedBindingsByRole creates active bindings for every seed skill's promoted
// 1.0.0 version, using a deterministic UUID per skill name for the agent_id.
// Idempotent: re-running never duplicates rows.
func (s *Store) SeedBindingsByRole(ctx context.Context) (int, error) {
	tag, err := s.Pool.Exec(ctx, `
		INSERT INTO public.agent_skill_bindings (agent_id, version_id, source, active)
		SELECT
			('00000000-0000-4000-8000-' || lpad(to_hex(abs(hashtext(s.name))), 12, '0'))::uuid,
			v.id, 'revision_auto', true
		FROM public.skills s
		JOIN public.skill_versions v ON v.skill_id = s.id AND v.version = '1.0.0' AND v.status = 'promoted'
		WHERE s.organization_id IS NULL
		  AND NOT EXISTS (
			SELECT 1 FROM public.agent_skill_bindings b
			WHERE b.version_id = v.id AND b.active = true)
		ON CONFLICT DO NOTHING`)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

func scanSnapshot(row rowScanner) (*SkillSnapshot, error) {
	snap := &SkillSnapshot{}
	var versionsText string
	if err := row.Scan(&snap.ID, &snap.OrganizationID, &versionsText, &snap.CreatedAt, &snap.SupersededAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(versionsText), &snap.Versions); err != nil {
		snap.Versions = []SnapshotEntry{}
	}
	return snap, nil
}
