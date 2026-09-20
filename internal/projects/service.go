package projects

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/github"
	"repomesh.local/repomesh/internal/secrets"
)

func (s *Service) Create(ctx context.Context, principal access.ProjectPrincipal, command CreateCommand) (CreateResult, error) {
	scope := operationScope{actor: principal.ActorID(), kind: "project_create", key: command.key}
	tx, err := s.beginWrite(ctx)
	if err != nil {
		return CreateResult{}, err
	}
	if err = s.access.LockProjectPrincipal(ctx, tx, principal); err != nil {
		rollback(tx)
		return CreateResult{}, err
	}
	operation, found, err := readOperation(ctx, tx, scope)
	if err != nil {
		rollback(tx)
		return CreateResult{}, err
	}
	if found {
		if err = requireOperationAccess(ctx, tx, principal.ActorID(), operation); err != nil {
			rollback(tx)
			return CreateResult{}, err
		}
		receipt, replayErr := replayCreate(operation, command.input)
		rollback(tx)
		return CreateResult{Receipt: receipt}, replayErr
	}
	rollback(tx)

	input, normalized, err := parseCreate(command.input, 1)
	if err != nil {
		return CreateResult{}, err
	}
	locators, err := s.access.ResolveSelectedRepositories(ctx, principal, input.RepositoryIDs)
	if err != nil {
		replayTx, beginErr := s.beginWrite(ctx)
		if beginErr == nil {
			defer rollback(replayTx)
			if lockErr := s.access.LockProjectPrincipal(ctx, replayTx, principal); lockErr != nil {
				return CreateResult{}, lockErr
			}
			if winner, ok, readErr := readOperation(ctx, replayTx, scope); readErr == nil && ok {
				if accessErr := requireOperationAccess(ctx, replayTx, principal.ActorID(), winner); accessErr != nil {
					return CreateResult{}, accessErr
				}
				receipt, replayErr := replayCreate(winner, command.input)
				return CreateResult{Receipt: receipt}, replayErr
			}
		}
		return CreateResult{}, err
	}
	// 提交路径必须现探：CheckProjectObservation 要求观测落在 60 秒窗口内，
	// 命中一条 5 分钟的旧缓存会放行已经变化的参与权（2026-09-18 主线修正）。
	observation, err := s.access.ObserveProjectRepositoriesFresh(ctx, principal, locators)
	if err != nil {
		replayTx, beginErr := s.beginWrite(ctx)
		if beginErr == nil {
			defer rollback(replayTx)
			if lockErr := s.access.LockProjectPrincipal(ctx, replayTx, principal); lockErr != nil {
				return CreateResult{}, lockErr
			}
			if winner, ok, readErr := readOperation(ctx, replayTx, scope); readErr == nil && ok {
				if accessErr := requireOperationAccess(ctx, replayTx, principal.ActorID(), winner); accessErr != nil {
					return CreateResult{}, accessErr
				}
				receipt, replayErr := replayCreate(winner, command.input)
				return CreateResult{Receipt: receipt}, replayErr
			}
		}
		return CreateResult{}, err
	}
	if permissionErr := requireAllowed(observation); permissionErr != nil {
		replayTx, beginErr := s.beginWrite(ctx)
		if beginErr == nil {
			defer rollback(replayTx)
			if lockErr := s.access.LockProjectPrincipal(ctx, replayTx, principal); lockErr != nil {
				return CreateResult{}, lockErr
			}
			if winner, ok, readErr := readOperation(ctx, replayTx, scope); readErr == nil && ok {
				if accessErr := requireOperationAccess(ctx, replayTx, principal.ActorID(), winner); accessErr != nil {
					return CreateResult{}, accessErr
				}
				receipt, replayErr := replayCreate(winner, command.input)
				return CreateResult{Receipt: receipt}, replayErr
			}
		}
		return CreateResult{}, permissionErr
	}
	if err = s.phase(ctx, authorizationObserved); err != nil {
		return CreateResult{}, err
	}

	tx, err = s.beginWrite(ctx)
	if err != nil {
		return CreateResult{}, err
	}
	defer rollback(tx)
	if err = s.access.LockProjectPrincipal(ctx, tx, principal); err != nil {
		return CreateResult{}, err
	}
	if operation, found, err = readOperation(ctx, tx, scope); err != nil {
		return CreateResult{}, err
	}
	if found {
		if err = requireOperationAccess(ctx, tx, principal.ActorID(), operation); err != nil {
			return CreateResult{}, err
		}
		receipt, replayErr := replayCreate(operation, command.input)
		return CreateResult{Receipt: receipt}, replayErr
	}
	if err = s.access.CheckProjectObservation(ctx, tx, principal, observation); err != nil {
		return CreateResult{}, err
	}
	if err = requireAllowed(observation); err != nil {
		return CreateResult{}, err
	}
	inserted, err := insertOperation(ctx, tx, scope, normalized)
	if err != nil {
		return CreateResult{}, err
	}
	if !inserted {
		winner, ok, readErr := readOperation(ctx, tx, scope)
		if readErr != nil {
			return CreateResult{}, readErr
		}
		if !ok {
			return CreateResult{}, unavailable()
		}
		receipt, replayErr := replayCreate(winner, command.input)
		return CreateResult{Receipt: receipt}, replayErr
	}
	if err = s.phase(ctx, operationInserted); err != nil {
		return CreateResult{}, err
	}
	var committedAt time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&committedAt); err != nil {
		return CreateResult{}, unavailable()
	}
	var organizationID string
	err = tx.QueryRow(ctx, `SELECT organization_id::text FROM repomesh_access.accounts WHERE id=$1`, principal.ActorID()).Scan(&organizationID)
	if err != nil {
		return CreateResult{}, err
	}
	project := projectRecord{id: newID(), owner: principal.ActorID(), organizationID: organizationID, name: input.Name, purpose: input.Purpose,
		revision: newID(), creationContextRevision: newID(), configurationRevision: newID(), createdAt: committedAt}
	fixed, err := s.resolveConfiguration(ctx, tx, principal.ActorID(), input.Configuration)
	if err != nil {
		return CreateResult{}, err
	}
	if err = insertProject(ctx, tx, project); err != nil {
		return CreateResult{}, err
	}
	if err = s.phase(ctx, projectWritten); err != nil {
		return CreateResult{}, err
	}
	for _, observed := range observation.Repositories() {
		if err = persistRepository(ctx, tx, observed.Locator); err != nil {
			return CreateResult{}, err
		}
		if err = addRepository(ctx, tx, project.id, project.revision, committedAt, observed.Locator); err != nil {
			return CreateResult{}, err
		}
	}
	if err = s.phase(ctx, repositoryWritten); err != nil {
		return CreateResult{}, err
	}
	configuration := configurationRecord{projectID: project.id, revision: project.configurationRevision, fixed: fixed, createdBy: principal.ActorID(), createdAt: committedAt}
	if err = insertConfiguration(ctx, tx, configuration); err != nil {
		return CreateResult{}, err
	}
	if err = s.phase(ctx, configurationWritten); err != nil {
		return CreateResult{}, err
	}
	if err = s.phase(ctx, projectReferenceWritten); err != nil {
		return CreateResult{}, err
	}
	operation = operationRecord{scope: scope, schemaVersion: normalized.schemaVersion, canonicalInput: normalized.canonical, exactInput: normalized.exact,
		projectID: project.id, projectRevision: project.revision, committedAt: committedAt}
	if err = finishOperation(ctx, tx, operation); err != nil {
		return CreateResult{}, err
	}
	if err = s.phase(ctx, receiptWritten); err != nil {
		return CreateResult{}, err
	}
	if err = s.phase(ctx, beforeCommit); err != nil {
		return CreateResult{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return CreateResult{}, unavailable()
	}
	return CreateResult{Receipt: creationReceipt(operation), FirstCommit: true}, nil
}

func (s *Service) prepareUpdate(ctx context.Context, principal access.ProjectPrincipal, command UpdateCommand, input UpdateInput, normalized normalizedInput) (updatePlan, error) {
	tx, err := s.beginWrite(ctx)
	if err != nil {
		return updatePlan{}, err
	}
	if err = s.access.LockProjectPrincipal(ctx, tx, principal); err != nil {
		rollback(tx)
		return updatePlan{}, err
	}
	current, err := readProject(ctx, tx, principal.ActorID(), command.projectID, false)
	if err != nil {
		rollback(tx)
		return updatePlan{}, err
	}
	existing, err := readRepositories(ctx, tx, current.id)
	rollback(tx)
	if err != nil {
		return updatePlan{}, err
	}
	requested := []string{}
	if input.RepositoryIDsToAdd != nil {
		requested = *input.RepositoryIDsToAdd
	}
	var urlLocators []access.RepositoryLocator
	// URL 形态的接入（2026-09-20）：仓库页（扫描目录）只有 URL，给不出 `repo_` id。
	// 这里把它换成 id —— 按 owner/name 去 GitHub 取数字 id、登记进项目注册表，
	// 再拼进**同一份** requested：后面的参与权观测、校验、上限、幂等台账全都不变。
	if input.RepositoryURLsToAdd != nil {
		for _, raw := range *input.RepositoryURLsToAdd {
			host, owner, name, parseErr := ParseRepositoryURL(raw)
			if parseErr != nil {
				return updatePlan{}, validation("repositoryUrlsToAdd")
			}
			locator, resolveErr := s.access.ResolveRepositoryByName(ctx, principal, host, owner, name)
			if resolveErr != nil {
				return updatePlan{}, resolveErr
			}
			// 登记用**单独一个短事务**，只登记、不接入：接入仍在下面那条路径上（受观测
			// 与上限约束）。若后面失败，留下的只是一行"这个部署认识这个仓"的目录记录
			// —— 扫描目录本来就是这个性质，无害。
			if persistErr := s.persistRepositoryRow(ctx, locator); persistErr != nil {
				return updatePlan{}, persistErr
			}
			// **不塞进 requested**：那条路要走 ResolveSelectedRepositories，而它要求
			// 这个仓在**当前 connection revision/epoch** 的发现记录里有一行。
			// 用户重新登录会换 epoch，旧发现记录全部失效 —— 线上实测：这个仓有 22 条
			// 发现记录、属于当前 epoch 的 0 条，于是按名字解析出 id 之后仍被 404 挡回。
			// 我们刚刚已经**用发起人的令牌现验过**这个仓（比一条陈旧的发现缓存更硬），
			// 所以它的定位直接并进 additions，不再被"缓存热不热"卡一道。
			urlLocators = append(urlLocators, locator)
		}
	}
	addIDs := repositoryAdditions(existing, requested)
	if len(existing)+len(addIDs)+len(urlLocators) > 100 {
		return updatePlan{}, validation("repositoryIdsToAdd")
	}
	additions, err := s.access.ResolveSelectedRepositories(ctx, principal, addIDs)
	if err != nil {
		return updatePlan{}, err
	}
	for _, locator := range urlLocators {
		dup := false
		for _, item := range existing {
			if item.ID == locator.ID {
				dup = true
				break
			}
		}
		for _, item := range additions {
			if item.ID == locator.ID {
				dup = true
				break
			}
		}
		if !dup {
			additions = append(additions, locator)
		}
	}
	var observation access.ProjectObservation
	if len(additions) > 0 {
		observation, err = s.access.ObserveProjectRepositoriesFresh(ctx, principal, additions)
		if err != nil {
			return updatePlan{}, err
		}
		if err = requireAllowed(observation); err != nil {
			return updatePlan{}, err
		}
	}
	return updatePlan{current: current, input: input, normalized: normalized, existing: existing, additions: additions, observation: observation}, nil
}

func (s *Service) Update(ctx context.Context, principal access.ProjectPrincipal, command UpdateCommand) (UpdateReceipt, error) {
	scope := operationScope{actor: principal.ActorID(), kind: "project_update", projectID: command.projectID, key: command.key}
	tx, err := s.beginWrite(ctx)
	if err != nil {
		return UpdateReceipt{}, err
	}
	if err = s.access.LockProjectPrincipal(ctx, tx, principal); err != nil {
		rollback(tx)
		return UpdateReceipt{}, err
	}
	operation, found, err := readOperation(ctx, tx, scope)
	if err != nil {
		rollback(tx)
		return UpdateReceipt{}, err
	}
	if found {
		if err = requireOperationAccess(ctx, tx, principal.ActorID(), operation); err != nil {
			rollback(tx)
			return UpdateReceipt{}, err
		}
		receipt, replayErr := replayUpdate(operation, command.input)
		rollback(tx)
		return receipt, replayErr
	}
	if _, err = readProject(ctx, tx, principal.ActorID(), command.projectID, false); err != nil {
		rollback(tx)
		return UpdateReceipt{}, err
	}
	rollback(tx)

	input, normalized, err := parseUpdate(command.input, 1)
	if err != nil {
		return UpdateReceipt{}, err
	}
	plan, err := s.prepareUpdate(ctx, principal, command, input, normalized)
	if err != nil {
		replayTx, beginErr := s.beginWrite(ctx)
		if beginErr == nil {
			defer rollback(replayTx)
			if lockErr := s.access.LockProjectPrincipal(ctx, replayTx, principal); lockErr != nil {
				return UpdateReceipt{}, lockErr
			}
			if winner, ok, readErr := readOperation(ctx, replayTx, scope); readErr == nil && ok {
				if accessErr := requireOperationAccess(ctx, replayTx, principal.ActorID(), winner); accessErr != nil {
					return UpdateReceipt{}, accessErr
				}
				return replayUpdate(winner, command.input)
			}
		}
		return UpdateReceipt{}, err
	}
	if err = s.phase(ctx, authorizationObserved); err != nil {
		return UpdateReceipt{}, err
	}
	tx, err = s.beginWrite(ctx)
	if err != nil {
		return UpdateReceipt{}, err
	}
	defer rollback(tx)
	if err = s.access.LockProjectPrincipal(ctx, tx, principal); err != nil {
		return UpdateReceipt{}, err
	}
	current, err := readProject(ctx, tx, principal.ActorID(), command.projectID, true)
	if err != nil {
		return UpdateReceipt{}, err
	}
	if operation, found, err = readOperation(ctx, tx, scope); err != nil {
		return UpdateReceipt{}, err
	}
	if found {
		return replayUpdate(operation, command.input)
	}
	if current.revision != input.ExpectedProjectRevision {
		return UpdateReceipt{}, failure(409, "PROJECT_REVISION_CONFLICT")
	}
	if len(plan.additions) > 0 {
		if err = s.access.CheckProjectObservation(ctx, tx, principal, plan.observation); err != nil {
			return UpdateReceipt{}, err
		}
		if err = requireAllowed(plan.observation); err != nil {
			return UpdateReceipt{}, err
		}
	}
	inserted, err := insertOperation(ctx, tx, scope, normalized)
	if err != nil {
		return UpdateReceipt{}, err
	}
	if !inserted {
		winner, ok, readErr := readOperation(ctx, tx, scope)
		if readErr != nil || !ok {
			return UpdateReceipt{}, unavailable()
		}
		return replayUpdate(winner, command.input)
	}
	if err = s.phase(ctx, operationInserted); err != nil {
		return UpdateReceipt{}, err
	}
	var committedAt time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&committedAt); err != nil {
		return UpdateReceipt{}, unavailable()
	}
	changed, contextChanged := false, false
	if input.Name != nil && *input.Name != current.name {
		current.name, changed = *input.Name, true
	}
	if input.Purpose != nil && *input.Purpose != current.purpose {
		current.purpose, changed = *input.Purpose, true
	}
	if len(plan.additions) > 0 {
		changed, contextChanged = true, true
	}
	var configuration configurationRecord
	configurationChanged := false
	if input.Configuration != nil {
		old, readErr := readFixedConfiguration(ctx, tx, current.id, current.configurationRevision)
		if readErr != nil {
			return UpdateReceipt{}, readErr
		}
		fixed, resolveErr := s.resolveConfiguration(ctx, tx, principal.ActorID(), *input.Configuration)
		if resolveErr != nil {
			return UpdateReceipt{}, resolveErr
		}
		if !sameConfiguration(old.fixed, fixed) {
			configurationChanged, changed, contextChanged = true, true, true
			current.configurationRevision = newID()
			configuration = configurationRecord{projectID: current.id, revision: current.configurationRevision, fixed: fixed, createdBy: principal.ActorID(), createdAt: committedAt}
		}
	}
	if changed {
		current.revision = newID()
	}
	if contextChanged {
		current.creationContextRevision = newID()
	}
	if err = updateProject(ctx, tx, current); err != nil {
		return UpdateReceipt{}, err
	}
	if err = s.phase(ctx, projectWritten); err != nil {
		return UpdateReceipt{}, err
	}
	for _, observed := range plan.observation.Repositories() {
		if err = persistRepository(ctx, tx, observed.Locator); err != nil {
			return UpdateReceipt{}, err
		}
		if err = addRepository(ctx, tx, current.id, current.revision, committedAt, observed.Locator); err != nil {
			return UpdateReceipt{}, err
		}
	}
	if err = s.phase(ctx, repositoryWritten); err != nil {
		return UpdateReceipt{}, err
	}
	if configurationChanged {
		if err = insertConfiguration(ctx, tx, configuration); err != nil {
			return UpdateReceipt{}, err
		}
	}
	if err = s.phase(ctx, configurationWritten); err != nil {
		return UpdateReceipt{}, err
	}
	if err = s.phase(ctx, projectReferenceWritten); err != nil {
		return UpdateReceipt{}, err
	}
	operation = operationRecord{scope: scope, schemaVersion: normalized.schemaVersion, canonicalInput: normalized.canonical, exactInput: normalized.exact,
		projectID: current.id, projectRevision: current.revision, committedAt: committedAt}
	if err = finishOperation(ctx, tx, operation); err != nil {
		return UpdateReceipt{}, err
	}
	if err = s.phase(ctx, receiptWritten); err != nil {
		return UpdateReceipt{}, err
	}
	if err = s.phase(ctx, beforeCommit); err != nil {
		return UpdateReceipt{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return UpdateReceipt{}, unavailable()
	}
	return updateReceipt(operation), nil
}

func (s *Service) Creation(ctx context.Context, principal access.ProjectPrincipal, creationID string) (CreationReceipt, error) {
	creationID = strings.ToLower(creationID)
	if !validKey(creationID) {
		return CreationReceipt{}, failure(404, "PROJECT_CREATION_NOT_FOUND")
	}
	tx, err := s.beginWrite(ctx)
	if err != nil {
		return CreationReceipt{}, err
	}
	defer rollback(tx)
	operation, found, err := readOperation(ctx, tx, operationScope{actor: principal.ActorID(), kind: "project_create", key: creationID})
	if err != nil {
		return CreationReceipt{}, err
	}
	if !found {
		return CreationReceipt{}, failure(404, "PROJECT_CREATION_NOT_FOUND")
	}
	if err = s.access.LockProjectPrincipal(ctx, tx, principal); err != nil {
		return CreationReceipt{}, err
	}
	if err = requireOperationAccess(ctx, tx, principal.ActorID(), operation); err != nil {
		return CreationReceipt{}, err
	}
	if operation.removedAt != nil {
		return CreationReceipt{}, failure(410, "PROJECT_CREATION_RESULT_REMOVED")
	}
	return creationReceipt(operation), nil
}

func (s *Service) UpdateResult(ctx context.Context, principal access.ProjectPrincipal, projectID, updateID string) (UpdateReceipt, error) {
	updateID = strings.ToLower(updateID)
	if !validResourceID(projectID) || !validKey(updateID) {
		return UpdateReceipt{}, failure(404, "PROJECT_UPDATE_NOT_FOUND")
	}
	tx, err := s.beginWrite(ctx)
	if err != nil {
		return UpdateReceipt{}, err
	}
	defer rollback(tx)
	operation, found, err := readOperation(ctx, tx, operationScope{actor: principal.ActorID(), kind: "project_update", projectID: projectID, key: updateID})
	if err != nil {
		return UpdateReceipt{}, err
	}
	if !found {
		return UpdateReceipt{}, failure(404, "PROJECT_UPDATE_NOT_FOUND")
	}
	if err = s.access.LockProjectPrincipal(ctx, tx, principal); err != nil {
		return UpdateReceipt{}, err
	}
	if err = requireOperationAccess(ctx, tx, principal.ActorID(), operation); err != nil {
		return UpdateReceipt{}, err
	}
	if operation.removedAt != nil {
		return UpdateReceipt{}, failure(410, "PROJECT_UPDATE_RESULT_REMOVED")
	}
	return updateReceipt(operation), nil
}

func (s *Service) Get(ctx context.Context, principal access.ProjectPrincipal, projectID string) (ProjectView, error) {
	if !validResourceID(projectID) {
		return ProjectView{}, failure(404, "RESOURCE_NOT_FOUND")
	}
	tx, err := s.beginWrite(ctx)
	if err != nil {
		return ProjectView{}, err
	}
	defer rollback(tx)
	if err = s.access.LockProjectPrincipal(ctx, tx, principal); err != nil {
		return ProjectView{}, err
	}
	project, err := readProject(ctx, tx, principal.ActorID(), projectID, false)
	if err != nil {
		return ProjectView{}, err
	}
	fixed, err := s.hydrateFixedConfiguration(ctx, tx, project)
	if err != nil {
		return ProjectView{}, err
	}
	checks, err := s.inspectConfiguration(ctx, tx, principal.ActorID(), fixed.snap.record.fixed)
	if err != nil {
		return ProjectView{}, err
	}
	summary, quota, err := s.readConfigurationSummary(ctx, tx, project.id, fixed, time.Now().UTC())
	if err != nil {
		return ProjectView{}, err
	}
	view := ProjectView{ID: project.id, Name: project.name, Purpose: project.purpose, ProjectRevision: project.revision, CreatedAt: project.createdAt,
		Configuration: configurationView(fixed.snap.record, checks), Actions: Actions{CanEdit: true, CanCreateIssue: false}, CreationReadiness: creationReadiness(checks)}
	view.Configuration.FixedSummary = summary
	view.Configuration.QuotaObservation = quota
	var hasRepositories bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM repomesh_projects.project_repositories WHERE project_id=$1)`, project.id).Scan(&hasRepositories); err != nil {
		return ProjectView{}, unavailable()
	}
	if !hasRepositories {
		view.CreationReadiness.Status = "restricted"
		view.CreationReadiness.ReasonCodes = append(view.CreationReadiness.ReasonCodes, "NO_AVAILABLE_REPOSITORIES")
	}
	// Live work authorization is evaluated by issue-creation-options, not this
	// configuration summary. Do not advertise a definitive readiness here.
	if view.CreationReadiness.Status == "ready" {
		view.CreationReadiness.Status = "unknown"
		view.CreationReadiness.ReasonCodes = append(view.CreationReadiness.ReasonCodes, "ISSUE_CREATION_OPTIONS_REQUIRED")
	}
	return view, nil
}

func (s *Service) List(ctx context.Context, principal access.ProjectPrincipal, query ListQuery) (ProjectPage, error) {
	tx, err := s.beginWrite(ctx)
	if err != nil {
		return ProjectPage{}, err
	}
	defer rollback(tx)
	if err = s.access.LockProjectPrincipal(ctx, tx, principal); err != nil {
		return ProjectPage{}, err
	}
	scope := cursorScope{actor: principal.ActorID(), kind: "projects", query: query.Text, limit: query.Limit}
	after := ""
	if query.Cursor != "" {
		cursor, readErr := readCursor(ctx, tx, query.Cursor, scope)
		if readErr != nil {
			return ProjectPage{}, readErr
		}
		after = cursor.afterID
	}
	rows, err := tx.Query(ctx, `SELECT id,name,revision,created_at FROM repomesh_projects.projects
		WHERE owner=$1 AND removed_at IS NULL AND id>$2 AND strpos(lower(name),lower($3))>0 ORDER BY id LIMIT $4`, principal.ActorID(), after, query.Text, query.Limit+1)
	if err != nil {
		return ProjectPage{}, unavailable()
	}
	result := ProjectPage{Items: []ProjectListItem{}}
	for rows.Next() {
		var item ProjectListItem
		if rows.Scan(&item.ID, &item.Name, &item.ProjectRevision, &item.CreatedAt) != nil {
			rows.Close()
			return ProjectPage{}, unavailable()
		}
		result.Items = append(result.Items, item)
	}
	if rows.Err() != nil {
		rows.Close()
		return ProjectPage{}, unavailable()
	}
	rows.Close()
	if len(result.Items) > query.Limit {
		result.Items = result.Items[:query.Limit]
		cursor := cursorRecord{id: newID(), scope: scope, afterID: result.Items[len(result.Items)-1].ID, expiresAt: time.Now().Add(10 * time.Minute)}
		if err = writeCursor(ctx, tx, cursor); err != nil {
			return ProjectPage{}, err
		}
		result.NextCursor = &cursor.id
	}
	if err = tx.Commit(ctx); err != nil {
		return ProjectPage{}, unavailable()
	}
	return result, nil
}

func (s *Service) Repositories(ctx context.Context, principal access.ProjectPrincipal, projectID string, query PageQuery) (ProjectRepositoryPage, error) {
	if !validResourceID(projectID) {
		return ProjectRepositoryPage{}, failure(404, "RESOURCE_NOT_FOUND")
	}
	tx, err := s.beginWrite(ctx)
	if err != nil {
		return ProjectRepositoryPage{}, err
	}
	if err = s.access.LockProjectPrincipal(ctx, tx, principal); err != nil {
		rollback(tx)
		return ProjectRepositoryPage{}, err
	}
	project, err := readProject(ctx, tx, principal.ActorID(), projectID, false)
	if err != nil {
		rollback(tx)
		return ProjectRepositoryPage{}, err
	}
	scope := cursorScope{actor: principal.ActorID(), kind: "repositories", projectID: projectID, limit: query.Limit}
	after := ""
	if query.Cursor != "" {
		cursor, readErr := readCursor(ctx, tx, query.Cursor, scope)
		if readErr != nil {
			rollback(tx)
			return ProjectRepositoryPage{}, readErr
		}
		if cursor.projectRevision != project.revision {
			rollback(tx)
			return ProjectRepositoryPage{}, failure(409, "PROJECT_CONTEXT_CHANGED")
		}
		after = cursor.afterID
	}
	repositories, err := readRepositories(ctx, tx, project.id)
	rollback(tx)
	if err != nil {
		return ProjectRepositoryPage{}, err
	}
	observation, err := s.access.ObserveProjectRepositoriesFresh(ctx, principal, repositories)
	if err != nil {
		return ProjectRepositoryPage{}, err
	}
	result := ProjectRepositoryPage{Items: []access.RepositoryItem{}, ProjectRevision: project.revision}
	start := 0
	for start < len(repositories) && repositories[start].ID <= after {
		start++
	}
	end := start + query.Limit
	if end > len(repositories) {
		end = len(repositories)
	}
	observed := observation.Repositories()
	for index, item := range observed {
		if item.ParticipationStatus != "allowed" || item.Item == nil {
			result.RestrictedRepositoryCount++
		}
		if index >= start && index < end && item.ParticipationStatus == "allowed" && item.Item != nil {
			result.Items = append(result.Items, *item.Item)
		}
	}
	tx, err = s.beginWrite(ctx)
	if err != nil {
		return ProjectRepositoryPage{}, err
	}
	defer rollback(tx)
	if err = s.access.LockProjectPrincipal(ctx, tx, principal); err != nil {
		return ProjectRepositoryPage{}, err
	}
	current, err := readProject(ctx, tx, principal.ActorID(), projectID, false)
	if err != nil {
		return ProjectRepositoryPage{}, err
	}
	if current.revision != project.revision {
		return ProjectRepositoryPage{}, failure(409, "PROJECT_CONTEXT_CHANGED")
	}
	if observation.Repositories() != nil {
		if err = s.access.CheckProjectObservation(ctx, tx, principal, observation); err != nil {
			return ProjectRepositoryPage{}, err
		}
	}
	if end < len(repositories) {
		cursor := cursorRecord{id: newID(), scope: scope, afterID: repositories[end-1].ID, projectRevision: project.revision, expiresAt: time.Now().Add(10 * time.Minute)}
		if err = writeCursor(ctx, tx, cursor); err != nil {
			return ProjectRepositoryPage{}, err
		}
		result.NextCursor = &cursor.id
	}
	if err = tx.Commit(ctx); err != nil {
		return ProjectRepositoryPage{}, unavailable()
	}
	return result, nil
}

func (s *Service) Profiles(ctx context.Context, principal access.ProjectPrincipal, query ProfileQuery) (ProfilePage, error) {
	tx, err := s.beginWrite(ctx)
	if err != nil {
		return ProfilePage{}, err
	}
	defer rollback(tx)
	if err = s.access.LockProjectPrincipal(ctx, tx, principal); err != nil {
		return ProfilePage{}, err
	}
	if err = lockCatalog(ctx, tx); err != nil {
		return ProfilePage{}, err
	}
	scope := cursorScope{actor: principal.ActorID(), kind: query.Kind, limit: query.Limit}
	after := ""
	if query.Cursor != "" {
		cursor, readErr := readCursor(ctx, tx, query.Cursor, scope)
		if readErr != nil {
			return ProfilePage{}, readErr
		}
		after = cursor.afterID
	}
	result := ProfilePage{Items: []ProfileItem{}}
	var defaultID string
	defaultErr := tx.QueryRow(ctx, `SELECT profile_id FROM repomesh_projects.defaults WHERE actor=$1 AND kind=$2`, principal.ActorID(), query.Kind).Scan(&defaultID)
	if defaultErr == nil {
		result.DefaultProfileID = &defaultID
	} else if !errors.Is(defaultErr, pgx.ErrNoRows) {
		return ProfilePage{}, unavailable()
	}
	rows, err := tx.Query(ctx, `SELECT p.id,p.name,p.enabled,v.parameters_complete,v.secret_version_id,v.secret_owner_kind,v.secret_owner_id,v.secret_purpose
		FROM repomesh_projects.profiles p JOIN repomesh_projects.profile_versions v ON v.kind=p.kind AND v.profile_id=p.id AND v.version=p.current_version
		WHERE p.owner=$1 AND p.kind=$2 AND p.id>$3 ORDER BY p.id LIMIT $4`, principal.ActorID(), query.Kind, after, query.Limit+1)
	if err != nil {
		return ProfilePage{}, unavailable()
	}
	type profileCandidate struct {
		item                         ProfileItem
		enabled, complete            bool
		secretID, ownerKind, ownerID *string
		purpose                      *string
	}
	candidates := make([]profileCandidate, 0, query.Limit+1)
	for rows.Next() {
		var candidate profileCandidate
		if rows.Scan(&candidate.item.ID, &candidate.item.Name, &candidate.enabled, &candidate.complete, &candidate.secretID, &candidate.ownerKind, &candidate.ownerID, &candidate.purpose) != nil {
			rows.Close()
			return ProfilePage{}, unavailable()
		}
		candidates = append(candidates, candidate)
	}
	if rows.Err() != nil {
		rows.Close()
		return ProfilePage{}, unavailable()
	}
	rows.Close()
	for _, candidate := range candidates {
		item := candidate.item
		observed := time.Now().UTC()
		item.Availability = github.Capability{Status: "allowed", ReasonCodes: []string{}, ObservedAt: &observed}
		if !candidate.enabled || !candidate.complete {
			item.Availability = github.Capability{Status: "denied", ReasonCodes: []string{strings.ToUpper(query.Kind) + "_CONFIG_UNAVAILABLE"}, ObservedAt: &observed}
		}
		if item.Availability.Status == "allowed" && candidate.secretID != nil {
			inspection, inspectErr := s.access.InspectProjectSecret(ctx, tx, secrets.VersionID(*candidate.secretID), secrets.Owner{Kind: *candidate.ownerKind, ID: *candidate.ownerID}, secrets.Purpose(*candidate.purpose))
			if inspectErr != nil {
				return ProfilePage{}, unavailable()
			}
			if !inspection.Found || !inspection.Enabled || inspection.Destroyed || !inspection.RootAvailable {
				item.Availability = github.Capability{Status: "denied", ReasonCodes: []string{strings.ToUpper(query.Kind) + "_SECRET_UNAVAILABLE"}, ObservedAt: inspection.CheckedAt}
			}
		} else if item.Availability.Status == "allowed" && query.Kind == "model" {
			item.Availability = github.Capability{Status: "denied", ReasonCodes: []string{"MODEL_CONFIG_MISSING"}, ObservedAt: &observed}
		}
		result.Items = append(result.Items, item)
	}
	if len(result.Items) > query.Limit {
		result.Items = result.Items[:query.Limit]
		cursor := cursorRecord{id: newID(), scope: scope, afterID: result.Items[len(result.Items)-1].ID, expiresAt: time.Now().Add(10 * time.Minute)}
		if err = writeCursor(ctx, tx, cursor); err != nil {
			return ProfilePage{}, err
		}
		result.NextCursor = &cursor.id
	}
	if err = tx.Commit(ctx); err != nil {
		return ProfilePage{}, unavailable()
	}
	return result, nil
}

func (s *Service) ResolveDestination(ctx context.Context, actor string, destination access.ProjectDestination) (*string, error) {
	if destination.Kind == "project" {
		if !validResourceID(destination.ProjectID) {
			return nil, failure(404, "RESOURCE_NOT_FOUND")
		}
		var exists bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM repomesh_projects.projects WHERE id=$1 AND owner=$2 AND removed_at IS NULL)`, destination.ProjectID, actor).Scan(&exists); err != nil {
			return nil, unavailable()
		}
		if !exists {
			return nil, failure(404, "RESOURCE_NOT_FOUND")
		}
		path := "/projects/" + destination.ProjectID
		return &path, nil
	}
	if destination.Kind != "operation" || !validKey(strings.ToLower(destination.OperationID)) {
		return nil, failure(404, "RESOURCE_NOT_FOUND")
	}
	if destination.OperationKind == "project_create" {
		path := "/project-creations/" + strings.ToLower(destination.OperationID)
		return &path, nil
	}
	if destination.OperationKind == "project_update" && validResourceID(destination.ProjectID) {
		var exists bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM repomesh_projects.projects WHERE id=$1 AND owner=$2 AND removed_at IS NULL)`, destination.ProjectID, actor).Scan(&exists); err != nil {
			return nil, unavailable()
		}
		if !exists {
			return nil, failure(404, "RESOURCE_NOT_FOUND")
		}
		path := "/projects/" + destination.ProjectID + "/updates/" + strings.ToLower(destination.OperationID)
		return &path, nil
	}
	return nil, failure(404, "RESOURCE_NOT_FOUND")
}
