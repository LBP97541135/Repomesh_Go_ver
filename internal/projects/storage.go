package projects

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"reflect"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/github"
	"repomesh.local/repomesh/internal/secrets"
)

func (s *Service) beginWrite(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		// 开事务失败最常见的真身是"池里拿不到连接"或"ctx 已经过期"，两者都会
		// 被兜成裸 503 RESULT_UNCONFIRMED。2026-09-20 排查线上 503 时 PostgreSQL
		// 日志里一条错误都没有，就是因为失败发生在这里 —— 连事务都没开起来。
		log.Printf("projects: begin write failed: %v", err)
		return nil, unavailable()
	}
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout='2s'`); err != nil {
		rollback(tx)
		return nil, unavailable()
	}
	if _, err := tx.Exec(ctx, `SET LOCAL statement_timeout='5s'`); err != nil {
		rollback(tx)
		return nil, unavailable()
	}
	if _, err := tx.Exec(ctx, `SET LOCAL synchronous_commit=on`); err != nil {
		rollback(tx)
		return nil, unavailable()
	}
	return tx, nil
}

func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}

func (s *Service) phase(ctx context.Context, phase transactionPhase) error {
	if s.hook == nil {
		return nil
	}
	return s.hook(ctx, phase)
}

func readProject(ctx context.Context, tx pgx.Tx, actor, projectID string, lock bool) (projectRecord, error) {
	var result projectRecord
	query := `SELECT id,owner,name,purpose,revision,creation_context_revision,current_configuration_revision,created_at,removed_at
		FROM repomesh_projects.projects WHERE id=$1 AND owner=$2 AND removed_at IS NULL`
	if lock {
		query += ` FOR UPDATE`
	}
	err := tx.QueryRow(ctx, query, projectID, actor).Scan(&result.id, &result.owner, &result.name, &result.purpose, &result.revision, &result.creationContextRevision, &result.configurationRevision, &result.createdAt, &result.removedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return projectRecord{}, failure(404, "RESOURCE_NOT_FOUND")
	}
	if err != nil {
		return projectRecord{}, unavailable()
	}
	return result, nil
}

func readOperation(ctx context.Context, tx pgx.Tx, scope operationScope) (operationRecord, bool, error) {
	result := operationRecord{scope: scope}
	var err error
	if scope.kind == "project_create" {
		err = tx.QueryRow(ctx, `SELECT schema_version,canonical_input,exact_input,project_id,project_revision,committed_at,removed_at
			FROM repomesh_projects.creation_operations WHERE actor=$1 AND key=$2`, scope.actor, scope.key).Scan(
			&result.schemaVersion, &result.canonicalInput, &result.exactInput, &result.projectID, &result.projectRevision, &result.committedAt, &result.removedAt)
	} else {
		err = tx.QueryRow(ctx, `SELECT schema_version,canonical_input,exact_input,project_revision,committed_at,removed_at
			FROM repomesh_projects.update_operations WHERE project_id=$1 AND actor=$2 AND key=$3`, scope.projectID, scope.actor, scope.key).Scan(
			&result.schemaVersion, &result.canonicalInput, &result.exactInput, &result.projectRevision, &result.committedAt, &result.removedAt)
		result.projectID = scope.projectID
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return operationRecord{}, false, nil
	}
	if err != nil {
		return operationRecord{}, false, unavailable()
	}
	return result, true, nil
}

func requireOperationAccess(ctx context.Context, tx pgx.Tx, actor string, operation operationRecord) error {
	if operation.scope.actor != actor {
		return failure(404, "RESOURCE_NOT_FOUND")
	}
	if operation.projectID == "" {
		return failure(404, "RESOURCE_NOT_FOUND")
	}
	var owned, readable bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM repomesh_projects.projects WHERE id=$1 AND owner=$2),
		EXISTS(SELECT 1 FROM repomesh_projects.projects WHERE id=$1 AND owner=$2 AND removed_at IS NULL)`, operation.projectID, actor).Scan(&owned, &readable); err != nil {
		return unavailable()
	}
	if !owned || operation.removedAt == nil && !readable {
		return failure(404, "RESOURCE_NOT_FOUND")
	}
	return nil
}

func replayCreate(operation operationRecord, raw RawInput) (CreationReceipt, error) {
	if operation.removedAt != nil {
		return CreationReceipt{}, failure(410, "PROJECT_CREATION_RESULT_REMOVED")
	}
	_, normalized, err := parseCreate(raw, operation.schemaVersion)
	if err != nil {
		return CreationReceipt{}, err
	}
	if !sameInput(operation, normalized) {
		return CreationReceipt{}, failure(409, "IDEMPOTENCY_CONFLICT")
	}
	return creationReceipt(operation), nil
}

func replayUpdate(operation operationRecord, raw RawInput) (UpdateReceipt, error) {
	if operation.removedAt != nil {
		return UpdateReceipt{}, failure(410, "PROJECT_UPDATE_RESULT_REMOVED")
	}
	_, normalized, err := parseUpdate(raw, operation.schemaVersion)
	if err != nil {
		return UpdateReceipt{}, err
	}
	if !sameInput(operation, normalized) {
		return UpdateReceipt{}, failure(409, "IDEMPOTENCY_CONFLICT")
	}
	return updateReceipt(operation), nil
}

func creationReceipt(operation operationRecord) CreationReceipt {
	return CreationReceipt{ProjectCreationID: operation.scope.key, Status: "committed", ProjectID: operation.projectID,
		ProjectRevision: operation.projectRevision, CreatedAt: operation.committedAt,
		Links: Links{Project: "/api/projects/" + operation.projectID, Operation: "/api/project-creations/" + operation.scope.key}}
}

func updateReceipt(operation operationRecord) UpdateReceipt {
	return UpdateReceipt{UpdateID: operation.scope.key, Status: "committed", ProjectID: operation.projectID,
		ProjectRevision: operation.projectRevision, UpdatedAt: operation.committedAt,
		Links: Links{Project: "/api/projects/" + operation.projectID, Operation: "/api/projects/" + operation.projectID + "/updates/" + operation.scope.key}}
}

func insertOperation(ctx context.Context, tx pgx.Tx, scope operationScope, normalized normalizedInput) (bool, error) {
	var rows int64
	if scope.kind == "project_create" {
		tag, err := tx.Exec(ctx, `INSERT INTO repomesh_projects.creation_operations(actor,key,schema_version,canonical_input,exact_input)
			VALUES($1,$2,$3,$4,$5) ON CONFLICT(actor,key) DO NOTHING`, scope.actor, scope.key, normalized.schemaVersion, normalized.canonical, normalized.exact)
		if err != nil {
			return false, unavailable()
		}
		rows = tag.RowsAffected()
	} else {
		tag, err := tx.Exec(ctx, `INSERT INTO repomesh_projects.update_operations(project_id,actor,key,schema_version,canonical_input,exact_input)
			VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(project_id,actor,key) DO NOTHING`, scope.projectID, scope.actor, scope.key, normalized.schemaVersion, normalized.canonical, normalized.exact)
		if err != nil {
			return false, unavailable()
		}
		rows = tag.RowsAffected()
	}
	return rows == 1, nil
}

func finishOperation(ctx context.Context, tx pgx.Tx, operation operationRecord) error {
	var result interface{ RowsAffected() int64 }
	var err error
	if operation.scope.kind == "project_create" {
		result, err = tx.Exec(ctx, `UPDATE repomesh_projects.creation_operations SET project_id=$3,project_revision=$4,committed_at=$5
			WHERE actor=$1 AND key=$2 AND committed_at IS NULL`, operation.scope.actor, operation.scope.key, operation.projectID, operation.projectRevision, operation.committedAt)
	} else {
		result, err = tx.Exec(ctx, `UPDATE repomesh_projects.update_operations SET project_revision=$4,committed_at=$5
			WHERE project_id=$1 AND actor=$2 AND key=$3 AND committed_at IS NULL`, operation.projectID, operation.scope.actor, operation.scope.key, operation.projectRevision, operation.committedAt)
	}
	if err != nil || result.RowsAffected() != 1 {
		return unavailable()
	}
	return nil
}

func insertProject(ctx context.Context, tx pgx.Tx, project projectRecord) error {
	_, err := tx.Exec(ctx, `INSERT INTO repomesh_projects.projects(id,owner,organization_id,name,purpose,revision,creation_context_revision,current_configuration_revision,created_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, project.id, project.owner, project.organizationID, project.name, project.purpose, project.revision, project.creationContextRevision, project.configurationRevision, project.createdAt)
	if err != nil {
		return unavailable()
	}
	return nil
}

func updateProject(ctx context.Context, tx pgx.Tx, project projectRecord) error {
	result, err := tx.Exec(ctx, `UPDATE repomesh_projects.projects SET name=$3,purpose=$4,revision=$5,creation_context_revision=$6,current_configuration_revision=$7
		WHERE id=$1 AND owner=$2 AND removed_at IS NULL`, project.id, project.owner, project.name, project.purpose, project.revision, project.creationContextRevision, project.configurationRevision)
	if err != nil || result.RowsAffected() != 1 {
		return unavailable()
	}
	return nil
}

func readRepositories(ctx context.Context, tx pgx.Tx, projectID string) ([]access.RepositoryLocator, error) {
	rows, err := tx.Query(ctx, `SELECT r.id,r.host,r.github_id,r.owner,r.name FROM repomesh_projects.project_repositories pr
		JOIN repomesh_projects.repositories r ON r.id=pr.repository_id WHERE pr.project_id=$1 ORDER BY r.id`, projectID)
	if err != nil {
		return nil, unavailable()
	}
	defer rows.Close()
	result := []access.RepositoryLocator{}
	for rows.Next() {
		var item access.RepositoryLocator
		if rows.Scan(&item.ID, &item.Host, &item.ExternalID, &item.Owner, &item.Name) != nil {
			return nil, unavailable()
		}
		result = append(result, item)
	}
	if rows.Err() != nil {
		return nil, unavailable()
	}
	return result, nil
}

func persistRepository(ctx context.Context, tx pgx.Tx, repository access.RepositoryLocator) error {
	result, err := tx.Exec(ctx, `INSERT INTO repomesh_projects.repositories(id,host,github_id,owner,name) VALUES($1,$2,$3,$4,$5)
		ON CONFLICT(id) DO UPDATE SET owner=EXCLUDED.owner,name=EXCLUDED.name WHERE repomesh_projects.repositories.host=EXCLUDED.host AND repomesh_projects.repositories.github_id=EXCLUDED.github_id`, repository.ID, repository.Host, repository.ExternalID, repository.Owner, repository.Name)
	if err != nil {
		log.Printf("projects: persist repository failed id=%s owner=%s/%s: %v", repository.ID, repository.Owner, repository.Name, err)
		return unavailable()
	}
	// **这条分支不会在 PostgreSQL 日志里留痕**：SQL 成功了，只是 ON CONFLICT 的
	// WHERE 不成立，于是影响 0 行。线上只看现象的话，它和"数据库挂了"长得一模一样
	// （都是裸 503 RESULT_UNCONFIRMED）。所以必须由我们记一笔。
	if result.RowsAffected() != 1 {
		log.Printf("projects: persist repository affected %d rows id=%s host=%s github_id=%d owner=%s/%s", result.RowsAffected(), repository.ID, repository.Host, repository.ExternalID, repository.Owner, repository.Name)
		return unavailable()
	}
	return nil
}

// persistRepositoryRows 把一批仓库登记进项目注册表，**一个事务**登记完。
//
// 只给「URL 形态的接入」用（2026-09-20）：那条路要先拿到 `repo_` id 才能走下面的
// 观测与校验，而 id 必须先有注册表行。**只登记、不接入** —— 接入受观测与上限约束，
// 留在原有路径上。失败时留下的只是一批目录记录，与扫描目录同性质，无害。
//
// 2026-09-20 从「每仓一个事务」改成批量：批量接入 43 个仓时那是 43 个事务、每个
// 6 次数据库往返，而每个事务里只写一行。单仓成本因此被抬到 ~430ms（裸调 GitHub
// 只有 ~114ms），第 35 个仓就撞上请求预算。
func (s *Service) persistRepositoryRows(ctx context.Context, repositories []access.RepositoryLocator) error {
	if len(repositories) == 0 {
		return nil
	}
	tx, err := s.beginWrite(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	for _, repository := range repositories {
		if err := persistRepository(ctx, tx, repository); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		log.Printf("projects: persist repository rows commit failed count=%d: %v", len(repositories), err)
		return unavailable()
	}
	return nil
}

func addRepository(ctx context.Context, tx pgx.Tx, projectID, revision string, at time.Time, repository access.RepositoryLocator) error {
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_projects.project_repositories(project_id,repository_id,joined_revision,joined_at)
		VALUES($1,$2,$3,$4) ON CONFLICT(project_id,repository_id) DO NOTHING`, projectID, repository.ID, revision, at); err != nil {
		return unavailable()
	}
	return nil
}

func requireAllowed(observation access.ProjectObservation) error {
	for _, item := range observation.Repositories() {
		switch item.ParticipationStatus {
		case "allowed":
		case "denied":
			return failure(404, "RESOURCE_NOT_FOUND")
		default:
			return failure(503, "AUTHORIZATION_UNCONFIRMED")
		}
	}
	return nil
}

// notAllowed 把「没通过」的仓和它们的状态拼成一行，只给日志用。
// 界面上这一档只留一个错误码（404 / 503），出问题时总得有个地方能看出
// 是哪个仓、什么状态 —— 否则"接不进去"这件事只能靠猜。
func notAllowed(observation access.ProjectObservation) string {
	parts := make([]string, 0, len(observation.Repositories()))
	for _, item := range observation.Repositories() {
		if item.ParticipationStatus != "allowed" {
			parts = append(parts, fmt.Sprintf("%s/%s=%s[%s]",
				item.Locator.Owner, item.Locator.Name, item.ParticipationStatus, strings.Join(item.ParticipationReasons, "+")))
		}
	}
	if len(parts) == 0 {
		return "(none)"
	}
	return strings.Join(parts, " ")
}

func repositoryAdditions(existing []access.RepositoryLocator, requested []string) []string {
	known := make(map[string]bool, len(existing))
	for _, item := range existing {
		known[item.ID] = true
	}
	result := []string{}
	for _, id := range requested {
		if !known[id] {
			result = append(result, id)
		}
	}
	return result
}

func lockCatalog(ctx context.Context, tx pgx.Tx) error {
	var singleton bool
	if err := tx.QueryRow(ctx, `SELECT singleton FROM repomesh_projects.catalog WHERE singleton FOR SHARE`).Scan(&singleton); err != nil || !singleton {
		return unavailable()
	}
	return nil
}

func (s *Service) resolveConfiguration(ctx context.Context, tx pgx.Tx, actor string, choice ConfigurationChoice) (fixedConfiguration, error) {
	if err := lockCatalog(ctx, tx); err != nil {
		return fixedConfiguration{}, err
	}
	model, modelVersion, err := resolveProfile(ctx, tx, actor, "model", choice.ModelProfile)
	if err != nil {
		return fixedConfiguration{}, err
	}
	execution, executionVersion, err := resolveProfile(ctx, tx, actor, "execution", choice.ExecutionProfile)
	if err != nil {
		return fixedConfiguration{}, err
	}
	result := fixedConfiguration{Selection: choice, Model: model, Execution: execution}
	if modelVersion != nil {
		result.BudgetPolicyID = modelVersion.budgetPolicyID
		result.TimeLimitPolicyID = modelVersion.timeLimitPolicyID
	}
	if executionVersion != nil {
		result.WorkerConcurrency = executionVersion.workerConcurrency
		if executionVersion.budgetPolicyID != nil {
			result.BudgetPolicyID = executionVersion.budgetPolicyID
		}
		if executionVersion.timeLimitPolicyID != nil {
			result.TimeLimitPolicyID = executionVersion.timeLimitPolicyID
		}
		result.VerificationGroupEnabled = executionVersion.verificationGroupEnabled
	}
	return result, nil
}

func resolveProfile(ctx context.Context, tx pgx.Tx, actor, kind string, choice ProfileChoice) (*profileBinding, *profileVersion, error) {
	var profile profileRecord
	var defaultRevision *string
	if choice.Mode == "inherit" {
		var pinned *string
		err := tx.QueryRow(ctx, `SELECT p.kind,p.id,p.owner,p.name,p.enabled,p.current_version,d.default_revision,d.pinned_version
			FROM repomesh_projects.defaults d JOIN repomesh_projects.profiles p ON p.kind=d.kind AND p.id=d.profile_id
			WHERE d.actor=$1 AND d.kind=$2 AND p.owner=$1 FOR SHARE OF d,p`, actor, kind).Scan(&profile.kind, &profile.id, &profile.owner, &profile.name, &profile.enabled, &profile.currentVersion, &defaultRevision, &pinned)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, nil
		}
		if err != nil {
			return nil, nil, unavailable()
		}
		if pinned != nil && *pinned != "" {
			profile.currentVersion = *pinned
		}
	} else {
		err := tx.QueryRow(ctx, `SELECT kind,id,owner,name,enabled,current_version FROM repomesh_projects.profiles
			WHERE kind=$1 AND id=$2 AND owner=$3 FOR SHARE`, kind, choice.ID, actor).Scan(&profile.kind, &profile.id, &profile.owner, &profile.name, &profile.enabled, &profile.currentVersion)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, failure(404, "RESOURCE_NOT_FOUND")
		}
		if err != nil {
			return nil, nil, unavailable()
		}
	}
	var version profileVersion
	version.kind, version.profileID, version.version = kind, profile.id, profile.currentVersion
	var secretID, secretOwnerKind, secretOwnerID, secretPurpose *string
	err := tx.QueryRow(ctx, `SELECT secret_version_id,secret_owner_kind,secret_owner_id,secret_purpose,parameters_complete,
		worker_concurrency,budget_policy_id,time_limit_policy_id,verification_group_enabled
		FROM repomesh_projects.profile_versions WHERE kind=$1 AND profile_id=$2 AND version=$3 FOR SHARE`, kind, profile.id, profile.currentVersion).Scan(
		&secretID, &secretOwnerKind, &secretOwnerID, &secretPurpose, &version.parametersComplete, &version.workerConcurrency, &version.budgetPolicyID, &version.timeLimitPolicyID, &version.verificationGroupEnabled)
	if err != nil {
		return nil, nil, unavailable()
	}
	if secretID != nil {
		version.secret = &secretReference{VersionID: secrets.VersionID(*secretID), OwnerKind: *secretOwnerKind, OwnerID: *secretOwnerID, Purpose: secrets.Purpose(*secretPurpose)}
	}
	binding := &profileBinding{ProfileID: profile.id, Version: profile.currentVersion, DefaultRevision: defaultRevision, Secret: version.secret}
	return binding, &version, nil
}

func readFixedConfiguration(ctx context.Context, tx pgx.Tx, projectID, revision string) (configurationRecord, error) {
	var result configurationRecord
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT project_id,revision,fixed,created_by,created_at FROM repomesh_projects.configuration_revisions
		WHERE project_id=$1 AND revision=$2`, projectID, revision).Scan(&result.projectID, &result.revision, &raw, &result.createdBy, &result.createdAt)
	if err != nil || json.Unmarshal(raw, &result.fixed) != nil {
		return configurationRecord{}, unavailable()
	}
	return result, nil
}

func insertConfiguration(ctx context.Context, tx pgx.Tx, configuration configurationRecord) error {
	raw, err := json.Marshal(configuration.fixed)
	if err != nil {
		return unavailable()
	}
	var modelID, modelVersion, executionID, executionVersion *string
	if configuration.fixed.Model != nil {
		modelID, modelVersion = &configuration.fixed.Model.ProfileID, &configuration.fixed.Model.Version
	}
	if configuration.fixed.Execution != nil {
		executionID, executionVersion = &configuration.fixed.Execution.ProfileID, &configuration.fixed.Execution.Version
	}
	_, err = tx.Exec(ctx, `INSERT INTO repomesh_projects.configuration_revisions(project_id,revision,fixed,model_profile_id,model_profile_version,
		execution_profile_id,execution_profile_version,created_by,created_at) VALUES($1,$2,$3::jsonb,$4,$5,$6,$7,$8,$9)`,
		configuration.projectID, configuration.revision, string(raw), modelID, modelVersion, executionID, executionVersion, configuration.createdBy, configuration.createdAt)
	if err != nil {
		return unavailable()
	}
	return nil
}

func sameConfiguration(a, b fixedConfiguration) bool { return reflect.DeepEqual(a, b) }

func (s *Service) inspectConfiguration(ctx context.Context, tx pgx.Tx, actor string, fixed fixedConfiguration) (github.Capability, error) {
	model, err := s.inspectProfile(ctx, tx, actor, "model", fixed.Model)
	if err != nil {
		return github.Capability{}, err
	}
	execution, err := s.inspectProfile(ctx, tx, actor, "execution", fixed.Execution)
	if err != nil {
		return github.Capability{}, err
	}
	// 合成：任一 unknown → unknown；任一 denied → denied（带两边的原因码）；
	// 两个都 allowed 才是 allowed——读面 creationReadiness 靠它点亮「就绪」。
	status := "allowed"
	reasons := append([]string{}, model.ReasonCodes...)
	reasons = append(reasons, execution.ReasonCodes...)
	if model.Status == "unknown" || execution.Status == "unknown" {
		status = "unknown"
	} else if model.Status != "allowed" || execution.Status != "allowed" {
		status = "denied"
	}
	observed := time.Now().UTC()
	return github.Capability{Status: status, ReasonCodes: reasons, ObservedAt: &observed}, nil
}

func (s *Service) inspectProfile(ctx context.Context, tx pgx.Tx, actor, kind string, binding *profileBinding) (github.Capability, error) {
	observed := time.Now().UTC()
	prefix := strings.ToUpper(kind)
	if binding == nil {
		return github.Capability{Status: "denied", ReasonCodes: []string{prefix + "_CONFIG_MISSING"}, ObservedAt: &observed}, nil
	}
	var enabled, complete bool
	var secretID, ownerKind, ownerID, purpose *string
	err := tx.QueryRow(ctx, `SELECT p.enabled,v.parameters_complete,v.secret_version_id,v.secret_owner_kind,v.secret_owner_id,v.secret_purpose
		FROM repomesh_projects.profiles p JOIN repomesh_projects.profile_versions v ON v.kind=p.kind AND v.profile_id=p.id
		WHERE p.kind=$1 AND p.id=$2 AND p.owner=$3 AND v.version=$4`, kind, binding.ProfileID, actor, binding.Version).Scan(&enabled, &complete, &secretID, &ownerKind, &ownerID, &purpose)
	if errors.Is(err, pgx.ErrNoRows) {
		return github.Capability{Status: "denied", ReasonCodes: []string{prefix + "_CONFIG_UNAVAILABLE"}, ObservedAt: &observed}, nil
	}
	if err != nil {
		return github.Capability{}, unavailable()
	}
	if !enabled || !complete {
		return github.Capability{Status: "denied", ReasonCodes: []string{prefix + "_CONFIG_UNAVAILABLE"}, ObservedAt: &observed}, nil
	}
	if secretID != nil {
		inspection, err := s.access.InspectProjectSecret(ctx, tx, secrets.VersionID(*secretID), secrets.Owner{Kind: *ownerKind, ID: *ownerID}, secrets.Purpose(*purpose))
		if err != nil {
			return github.Capability{}, unavailable()
		}
		if !inspection.Found || !inspection.Enabled || inspection.Destroyed || !inspection.RootAvailable {
			return github.Capability{Status: "denied", ReasonCodes: []string{prefix + "_SECRET_UNAVAILABLE"}, ObservedAt: inspection.CheckedAt}, nil
		}
	} else if kind == "model" {
		return github.Capability{Status: "denied", ReasonCodes: []string{"MODEL_CONFIG_MISSING"}, ObservedAt: &observed}, nil
	}
	// 2026-09-20 修：此前这里恒返回 denied（prefix_INTEGRATION_NOT_AVAILABLE），
	// 于是 profile 启用、参数完整、密钥可用也照样报「集成不可用」，每个项目的
	// 创建就绪度永远 restricted——「请先完成执行配置」的提示常亮，人无从下手。
	// 走到这里 = profile 与密钥的检查全部通过。
	return github.Capability{Status: "allowed", ObservedAt: &observed}, nil
}

func configurationView(record configurationRecord, checks github.Capability) ConfigurationView {
	result := ConfigurationView{ModelProfile: record.fixed.Selection.ModelProfile, ExecutionProfile: record.fixed.Selection.ExecutionProfile,
		Effective: EffectiveConfiguration{ConfigurationRevision: record.revision,
			WorkerConcurrency: record.fixed.WorkerConcurrency, BudgetPolicyID: record.fixed.BudgetPolicyID,
			TimeLimitPolicyID: record.fixed.TimeLimitPolicyID, VerificationGroupEnabled: record.fixed.VerificationGroupEnabled}, Checks: checks}
	if record.fixed.Model != nil {
		result.Effective.ModelProfileID = &record.fixed.Model.ProfileID
	}
	if record.fixed.Execution != nil {
		result.Effective.ExecutionProfileID = &record.fixed.Execution.ProfileID
	}
	return result
}

func creationReadiness(checks github.Capability) CreationReadiness {
	status := "restricted"
	if checks.Status == "unknown" {
		status = "unknown"
	}
	if checks.Status == "allowed" {
		status = "ready"
	}
	return CreationReadiness{Status: status, ReasonCodes: append([]string(nil), checks.ReasonCodes...), ObservedAt: checks.ObservedAt}
}

func readCursor(ctx context.Context, tx pgx.Tx, id string, scope cursorScope) (cursorRecord, error) {
	var result cursorRecord
	result.id = id
	err := tx.QueryRow(ctx, `SELECT actor,kind,project_id,query,page_limit,after_id,project_revision,expires_at
		FROM repomesh_projects.cursors WHERE id=$1`, id).Scan(&result.scope.actor, &result.scope.kind, &result.scope.projectID, &result.scope.query, &result.scope.limit, &result.afterID, &result.projectRevision, &result.expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return cursorRecord{}, failure(400, "INVALID_CURSOR")
	}
	if err != nil {
		return cursorRecord{}, unavailable()
	}
	if result.scope != scope {
		return cursorRecord{}, failure(400, "INVALID_CURSOR")
	}
	if !result.expiresAt.After(time.Now()) {
		return cursorRecord{}, failure(409, "CURSOR_EXPIRED")
	}
	return result, nil
}

func writeCursor(ctx context.Context, tx pgx.Tx, record cursorRecord) error {
	_, err := tx.Exec(ctx, `INSERT INTO repomesh_projects.cursors(id,actor,kind,project_id,query,page_limit,after_id,project_revision,expires_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, record.id, record.scope.actor, record.scope.kind, record.scope.projectID, record.scope.query, record.scope.limit, record.afterID, record.projectRevision, record.expiresAt)
	if err != nil {
		return unavailable()
	}
	return nil
}
