package projects

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/secrets"
)

// ConfigurationRevision identifies one immutable committed configuration snapshot.
type ConfigurationRevision string

// ProfileVersionRef pins one exact profile version.
type ProfileVersionRef struct{ ID, Version string }

// PolicyRef pins one exact policy version.
type PolicyRef struct{ ID, Version string }

// ModelBinding is the exact model the project runs: profile version, provider
// snapshot coordinates and the secret version that authorizes it.
type ModelBinding struct {
	Profile          ProfileVersionRef
	ProviderID       string
	ProviderRevision string
	ModelRowID       string
	SecretVersion    secrets.VersionID
}

// RequestPolicy is a counted-request policy version.
type RequestPolicy struct {
	Ref           PolicyRef
	Scope         string
	Limit         int64
	MaxUnresolved int64
	Enabled       bool
}

// TimePolicy is a duration policy version.
type TimePolicy struct {
	Ref                  PolicyRef
	ModelRequestSeconds  int64
	WorkerAttemptSeconds int64
}

// ExecutionRecipe is the immutable recipe a complete execution version
// carries: template pin plus worker shape plus embedded policies.
type ExecutionRecipe struct {
	Template                 ProfileVersionRef
	WorkerConcurrency        int
	VerificationGroupEnabled bool
	Budget                   RequestPolicy
	Limits                   TimePolicy
}

// fixedSnapshot is the private inner state of an exported FixedConfiguration:
// the stored configuration record plus the hydrated model binding and
// execution recipe resolved from immutable history.
type fixedSnapshot struct {
	record configurationRecord
	model  *ModelBinding
	recipe *ExecutionRecipe
}

// FixedConfiguration is the opaque immutable snapshot of one configuration
// revision. External callers cannot edit or forge it.
type FixedConfiguration struct{ snap fixedSnapshot }

// Revision returns the configuration revision this snapshot pins.
func (f FixedConfiguration) Revision() ConfigurationRevision {
	return ConfigurationRevision(f.snap.record.revision)
}

// Model returns the exact model binding when one is hydrated.
func (f FixedConfiguration) Model() (ModelBinding, bool) {
	if f.snap.model == nil {
		return ModelBinding{}, false
	}
	return *f.snap.model, true
}

// HasModelReference reports whether the configuration references a model
// profile version even when the provider snapshot is unavailable.
func (f FixedConfiguration) HasModelReference() bool { return f.snap.record.fixed.Model != nil }

// Execution returns the pinned execution profile version when present.
func (f FixedConfiguration) Execution() (ProfileVersionRef, bool) {
	if f.snap.record.fixed.Execution == nil {
		return ProfileVersionRef{}, false
	}
	return ProfileVersionRef{ID: f.snap.record.fixed.Execution.ProfileID, Version: f.snap.record.fixed.Execution.Version}, true
}

// ExecutionRecipe returns the immutable execution recipe when the pinned
// execution version is complete (schema2 import).
func (f FixedConfiguration) ExecutionRecipe() (ExecutionRecipe, bool) {
	if f.snap.recipe == nil {
		return ExecutionRecipe{}, false
	}
	return *f.snap.recipe, true
}

// ModelChoice projects the model reference onto a profile choice.
func (f FixedConfiguration) ModelChoice() ProfileChoice {
	if f.snap.record.fixed.Model == nil {
		return ProfileChoice{Mode: "inherit"}
	}
	return ProfileChoice{Mode: "pinned", ID: f.snap.record.fixed.Model.ProfileID}
}

// ExecutionChoice projects the execution reference onto a profile choice.
func (f FixedConfiguration) ExecutionChoice() ProfileChoice {
	if f.snap.record.fixed.Execution == nil {
		return ProfileChoice{Mode: "inherit"}
	}
	return ProfileChoice{Mode: "pinned", ID: f.snap.record.fixed.Execution.ProfileID}
}

// LockedProject is a project row locked for configuration work.
type LockedProject struct{ record projectRecord }

// ID returns the project id.
func (p LockedProject) ID() string { return p.record.id }

// Revision returns the project revision observed at lock time.
func (p LockedProject) Revision() string { return p.record.revision }

// CreationContextRevision returns the creation context revision.
func (p LockedProject) CreationContextRevision() string { return p.record.creationContextRevision }

// FixedRevision returns the pinned configuration revision.
func (p LockedProject) FixedRevision() (ConfigurationRevision, bool) {
	if p.record.configurationRevision == "" {
		return "", false
	}
	return ConfigurationRevision(p.record.configurationRevision), true
}

// LockForConfiguration locks the project row for configuration mutation. The
// caller must already hold the owner account lock.
func (s *Service) LockForConfiguration(ctx context.Context, tx pgx.Tx, principal access.ProjectPrincipal, projectID string) (LockedProject, error) {
	record, err := readProject(ctx, tx, principal.ActorID(), projectID, true)
	if err != nil {
		return LockedProject{}, err
	}
	return LockedProject{record: record}, nil
}

// ReadFixed reads the immutable fixed configuration snapshot at the requested
// revision on the caller's tx. An empty revision reads the current one; a
// non-empty revision that does not match the current pin refuses.
func (s *Service) ReadFixed(ctx context.Context, tx pgx.Tx, principal access.ProjectPrincipal, projectID string, revision ConfigurationRevision) (FixedConfiguration, error) {
	record, err := readProject(ctx, tx, principal.ActorID(), projectID, false)
	if err != nil {
		return FixedConfiguration{}, err
	}
	if string(revision) != "" && record.configurationRevision != string(revision) {
		return FixedConfiguration{}, failure(409, "CONFIGURATION_REVISION_MISMATCH")
	}
	return s.hydrateFixedConfiguration(ctx, tx, record)
}

// hydrateFixedConfiguration re-reads the exact immutable sources the stored
// fixed configuration points at: the configuration_revisions row, the model
// provider link and the complete execution version. Missing history keeps the
// record and the reference but yields no usable model or recipe.
func (s *Service) hydrateFixedConfiguration(ctx context.Context, tx pgx.Tx, record projectRecord) (FixedConfiguration, error) {
	stored, err := readFixedConfiguration(ctx, tx, record.id, record.configurationRevision)
	if err != nil {
		return FixedConfiguration{}, err
	}
	fixed := FixedConfiguration{snap: fixedSnapshot{record: stored}}

	if stored.fixed.Model != nil {
		binding, err := readModelBinding(ctx, tx, stored.fixed.Model.ProfileID, stored.fixed.Model.Version)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			// History gone: keep the reference, no usable model.
		case err != nil:
			return FixedConfiguration{}, err
		default:
			fixed.snap.model = &binding
		}
	}

	if stored.fixed.Execution != nil {
		recipe, err := readExecutionRecipe(ctx, tx, stored.fixed.Execution.ProfileID, stored.fixed.Execution.Version)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			// History gone or schema1 import: keep the reference, no recipe.
		case err != nil:
			return FixedConfiguration{}, err
		default:
			fixed.snap.recipe = &recipe
		}
	}
	return fixed, nil
}

// readModelBinding resolves one model profile version to its provider snapshot
// coordinates through the authoritative profile_links join.
func readModelBinding(ctx context.Context, tx pgx.Tx, profileID, version string) (ModelBinding, error) {
	var binding ModelBinding
	binding.Profile = ProfileVersionRef{ID: profileID, Version: version}
	var secretVersion string
	err := tx.QueryRow(ctx, `SELECT pl.provider_id, pl.provider_revision, pl.row_id, pl.secret_version_id
		FROM repomesh_models.profile_links pl
		WHERE pl.profile_id=$1 AND pl.profile_version=$2`,
		profileID, version).Scan(&binding.ProviderID, &binding.ProviderRevision, &binding.ModelRowID, &secretVersion)
	if err != nil {
		return ModelBinding{}, err
	}
	binding.SecretVersion = secrets.VersionID(secretVersion)
	return binding, nil
}

// readExecutionRecipe resolves one execution profile version to its complete
// recipe from the schema2 columns of execution_versions.
func readExecutionRecipe(ctx context.Context, tx pgx.Tx, profileID, version string) (ExecutionRecipe, error) {
	var recipe ExecutionRecipe
	var complete bool
	var templateID, templateVersion string
	var budgetID, budgetVersion, timeID, timeVersion *string
	err := tx.QueryRow(ctx, `SELECT ev.template_id, ev.template_version, ev.worker_concurrency, ev.verification_group_enabled,
		   ev.budget_policy_id, ev.budget_policy_version, ev.time_limit_policy_id, ev.time_limit_policy_version, ev.complete
		FROM repomesh_sources.execution_versions ev
		WHERE ev.profile_id=$1 AND ev.version=$2`,
		profileID, version).Scan(&templateID, &templateVersion, &recipe.WorkerConcurrency, &recipe.VerificationGroupEnabled,
		&budgetID, &budgetVersion, &timeID, &timeVersion, &complete)
	if err != nil {
		return ExecutionRecipe{}, err
	}
	recipe.Template = ProfileVersionRef{ID: templateID, Version: templateVersion}
	if !complete || budgetID == nil || budgetVersion == nil || timeID == nil || timeVersion == nil {
		return ExecutionRecipe{}, pgx.ErrNoRows
	}
	budget, err := readRequestPolicyVersion(ctx, tx, *budgetID, *budgetVersion)
	if err != nil {
		return ExecutionRecipe{}, err
	}
	limits, err := readTimePolicyVersion(ctx, tx, *timeID, *timeVersion)
	if err != nil {
		return ExecutionRecipe{}, err
	}
	recipe.Budget = budget
	recipe.Limits = limits
	return recipe, nil
}

func readRequestPolicyVersion(ctx context.Context, tx pgx.Tx, id, version string) (RequestPolicy, error) {
	var policy RequestPolicy
	err := tx.QueryRow(ctx, `SELECT id, version, scope, daily_limit, max_unresolved, enabled
		FROM repomesh_projects.request_policy_versions WHERE id=$1 AND version=$2`,
		id, version).Scan(&policy.Ref.ID, &policy.Ref.Version, &policy.Scope, &policy.Limit, &policy.MaxUnresolved, &policy.Enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return RequestPolicy{}, failure(2, "POLICY_HISTORY_MISSING")
	}
	if err != nil {
		return RequestPolicy{}, unavailable()
	}
	return policy, nil
}

func readTimePolicyVersion(ctx context.Context, tx pgx.Tx, id, version string) (TimePolicy, error) {
	var policy TimePolicy
	err := tx.QueryRow(ctx, `SELECT id, version, model_request_seconds, worker_attempt_seconds
		FROM repomesh_projects.time_policy_versions WHERE id=$1 AND version=$2`,
		id, version).Scan(&policy.Ref.ID, &policy.Ref.Version, &policy.ModelRequestSeconds, &policy.WorkerAttemptSeconds)
	if errors.Is(err, pgx.ErrNoRows) {
		return TimePolicy{}, failure(2, "POLICY_HISTORY_MISSING")
	}
	if err != nil {
		return TimePolicy{}, unavailable()
	}
	return policy, nil
}

// ModelReplacement carries the caller's expectations for one model swap.
type ModelReplacement struct {
	ExpectedProjectRevision string
	Before                  FixedConfiguration
	Candidate               ModelBinding
}

// ConfigurationChange reports one committed configuration transition.
type ConfigurationChange struct {
	ProjectRevision       string
	ConfigurationRevision ConfigurationRevision
	Changed               bool
	CommittedAt           time.Time
}

// ReplaceModel atomically pins a new model while retaining the existing
// execution selection. It requires the project, catalog, profile and
// availability lock order and never calls resolveConfiguration.
func (s *Service) ReplaceModel(ctx context.Context, tx pgx.Tx, project LockedProject, replacement ModelReplacement, at time.Time) (ConfigurationChange, error) {
	if project.record.revision != replacement.ExpectedProjectRevision {
		return ConfigurationChange{}, failure(409, "PROJECT_REVISION_MISMATCH")
	}
	next := retainExecutionAndReplaceModel(replacement.Before.snap, replacement.Candidate)
	revision := newCatalogID()
	projectRevision := newCatalogID()

	next.record.projectID = project.record.id
	next.record.revision = revision
	next.record.createdBy = project.record.owner
	next.record.createdAt = at
	if err := insertConfiguration(ctx, tx, next.record); err != nil {
		return ConfigurationChange{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE repomesh_projects.projects
		SET current_configuration_revision=$2, revision=$3
		WHERE id=$1 AND revision=$4`,
		project.record.id, revision, projectRevision, project.record.revision); err != nil {
		return ConfigurationChange{}, unavailable()
	}
	return ConfigurationChange{
		ProjectRevision:       projectRevision,
		ConfigurationRevision: ConfigurationRevision(revision),
		Changed:               true,
		CommittedAt:           at,
	}, nil
}

// retainExecutionAndReplaceModel copies the before snapshot and swaps only the
// model selection.
func retainExecutionAndReplaceModel(before fixedSnapshot, candidate ModelBinding) fixedSnapshot {
	next := before
	next.record.fixed.Model = &profileBinding{ProfileID: candidate.Profile.ID, Version: candidate.Profile.Version}
	model := candidate
	next.model = &model
	return next
}

// CheckedFixed is the result of a local fixed-configuration inspection: no
// network calls, only local ledger facts.
type CheckedFixed struct {
	Configuration FixedConfiguration
	Status        string
	Reasons       []string
	ObservedAt    time.Time
}

// inheritedProfileUsable 判定 inherit 形态下、该账号的默认档案能不能用。
//
// 返回 (可用, 原因码)。原因码**刻意分成两种**，因为修法完全不同：
//
//	default_<kind>_missing —— 该账号压根没设默认档案 → 动作是"去设置里选一个默认"
//	<kind>_unavailable     —— 设了，但解析不出来（档案被停用 / 版本行缺失）
//	                          → 动作是"查这个档案本身"
//
// 合成一个码就等于把两种修法不同的事混成一句"不可用"，用户还是不知道该干什么。
//
// 解析路径与 storage.go 的 resolveProfile **同一张表**（defaults → profiles），
// 不另造一套：inherit 的语义在系统里只能有一个来源。
func (s *Service) inheritedProfileUsable(ctx context.Context, tx pgx.Tx, actor, kind string) (bool, string) {
	var profileID, currentVersion string
	var enabled bool
	err := tx.QueryRow(ctx, `
		SELECT p.id, p.current_version, p.enabled
		FROM repomesh_projects.defaults d
		JOIN repomesh_projects.profiles p ON p.kind = d.kind AND p.id = d.profile_id
		WHERE d.actor = $1 AND d.kind = $2 AND p.owner = $1`, actor, kind).
		Scan(&profileID, &currentVersion, &enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, "default_" + kind + "_missing"
	}
	if err != nil {
		return false, kind + "_unavailable"
	}
	if !enabled || currentVersion == "" {
		return false, kind + "_unavailable"
	}
	// 默认档案的**当前版本**必须真的有对应的版本行 —— 否则"设了默认"只是挂了个名字，
	// 执行面照样解析不出配方。
	var versionExists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM repomesh_projects.profile_versions
		WHERE kind = $1 AND profile_id = $2 AND version = $3)`,
		kind, profileID, currentVersion).Scan(&versionExists); err != nil || !versionExists {
		return false, kind + "_unavailable"
	}
	return true, ""
}

// InspectFixedForCreation inspects a fixed configuration for creation-context
// use. It never initializes quota windows.
func (s *Service) InspectFixedForCreation(ctx context.Context, tx pgx.Tx, principal access.ProjectPrincipal, projectID string, revision ConfigurationRevision, now time.Time) (CheckedFixed, error) {
	fixed, err := s.ReadFixed(ctx, tx, principal, projectID, revision)
	if err != nil {
		return CheckedFixed{}, err
	}
	checked := CheckedFixed{Configuration: fixed, Status: "ready", ObservedAt: now}
	actor := principal.ActorID()
	// 2026-09-21 用户实测（catmem 的 1111 / 111test）：此前这里**只认显式钉住的档案**，
	// 而 "mode":"inherit" 恰恰是**默认形态**（ModelChoice() 在 nil 时返回 inherit）——
	// 于是"没钉档案"被当成"档案坏了"，把**默认配置**判成了故障，这两个项目因此
	// 永久建不了 issue（界面只显示笼统的「执行配置未完成」）。
	//
	// 现在按契约里的两种形态分别判：
	//   钉住了 → 就看钉住的那个能不能解析（原来的逻辑，保持不变）；
	//   inherit → 回退到该账号的默认档案（与 resolveProfile 同一张表）。
	if _, ok := fixed.Model(); !ok {
		if fixed.HasModelReference() {
			// 引用了却解析不出来 → 真的是历史/档案缺失，不是没配。
			checked.Status = "blocked"
			checked.Reasons = append(checked.Reasons, "model_unavailable")
		} else if usable, reason := s.inheritedProfileUsable(ctx, tx, actor, "model"); !usable {
			checked.Status = "blocked"
			checked.Reasons = append(checked.Reasons, reason)
		}
	}
	if _, ok := fixed.ExecutionRecipe(); !ok {
		if _, pinned := fixed.Execution(); pinned {
			// 同上：钉了但解析不出配方（历史缺失 / schema1 旧格式）。
			checked.Status = "blocked"
			checked.Reasons = append(checked.Reasons, "execution_recipe_unavailable")
		} else if usable, reason := s.inheritedProfileUsable(ctx, tx, actor, "execution"); !usable {
			checked.Status = "blocked"
			checked.Reasons = append(checked.Reasons, reason)
		}
	}
	return checked, nil
}
