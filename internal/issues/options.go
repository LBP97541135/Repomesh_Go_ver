package issues

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/discovery"
	"repomesh.local/repomesh/internal/projects"
)

// RepositoryAnalysisCapability reports what the platform can currently do for
// repository analysis; B06 ships the permanently unavailable baseline that the
// contract's reasonCodes vocabulary expects.
type RepositoryAnalysisCapability struct {
	Availability string   `json:"availability"`
	ReasonCodes  []string `json:"reasonCodes"`
}

// CreationRepository is one selectable repository in the options projection.
type CreationRepository struct {
	RepositoryID string   `json:"repositoryId"`
	DisplayName  string   `json:"displayName"`
	Selectable   bool     `json:"selectable"`
	Reasons      []string `json:"reasons"`
	ObservedAt   string   `json:"observedAt"`
}

// CreationOptions is the projection behind the creation options query.
type CreationOptions struct {
	ProjectID                string                       `json:"projectId"`
	CreationContextRevision  string                       `json:"creationContextRevision"`
	DefaultConversationMode  string                       `json:"defaultConversationMode"`
	AllowedConversationModes []string                     `json:"allowedConversationModes"`
	CanSubmit                bool                         `json:"canSubmit"`
	BlockingReasons          []string                     `json:"blockingReasons"`
	Repositories             []CreationRepository         `json:"repositories"`
	NextCursor               string                       `json:"nextCursor"`
	RepositoryAnalysis       RepositoryAnalysisCapability `json:"repositoryAnalysis"`
}

// PageQuery bounds the options listing.
type PageQuery struct {
	Cursor string
	Limit  int
}

// ParsePageQuery applies the documented limits (1..100, default 50).
func ParsePageQuery(cursor string, limit int) PageQuery {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	return PageQuery{Cursor: cursor, Limit: limit}
}

// Options returns the creation options projection: which repositories the
// caller may select, the default conversation mode, and whether submission is
// currently possible. Repositories not readable by the caller are hidden.
// 2026-09-20 起建项不再选仓:CanSubmit 只看配置与 App 就绪(选仓挪到①之后的
// 选仓门),凭据级观测失败也从 503 降级为非阻塞标记;逐仓 selectable 投影保留,
// 选仓门用它渲染建议列表。
func (s *Service) Options(ctx context.Context, principal access.ProjectPrincipal, projectID string, query PageQuery) (CreationOptions, error) {
	options := CreationOptions{
		ProjectID:                projectID,
		DefaultConversationMode:  "new",
		AllowedConversationModes: []string{"new", "existing"},
		Repositories:             []CreationRepository{},
		RepositoryAnalysis:       RepositoryAnalysisCapability{Availability: "unavailable", ReasonCodes: []string{"INTEGRATION_NOT_AVAILABLE"}},
	}
	tx, err := s.beginCreate(ctx)
	if err != nil {
		return CreationOptions{}, err
	}
	defer rollbackTx(tx)
	if err := s.authorization.CheckProjectPrincipal(ctx, tx, principal); err != nil {
		return CreationOptions{}, err
	}
	fixedRevision, hasFixed, err := s.readCreationContext(ctx, tx, principal, projectID, &options)
	if err != nil {
		return CreationOptions{}, err
	}
	configurationReady := false
	if hasFixed {
		checked, checkErr := s.projects.InspectFixedForCreation(ctx, tx, principal, projectID, projects.ConfigurationRevision(fixedRevision), time.Now().UTC())
		if checkErr != nil {
			return CreationOptions{}, checkErr
		}
		configurationReady = checked.Status == "ready"
		if !configurationReady {
			options.BlockingReasons = append(options.BlockingReasons, "CONFIGURATION_NOT_READY")
		}
	}
	repositories, err := readAllProjectRepositories(ctx, tx, projectID)
	if err != nil {
		return CreationOptions{}, err
	}
	rollbackTx(tx)
	observation, err := s.authorization.ObserveProjectRepositories(ctx, principal, repositories)
	if err != nil {
		return CreationOptions{}, err
	}
	observed := observation.Repositories()
	start := 0
	if query.Cursor != "" {
		for start < len(observed) && observed[start].Locator.ID <= query.Cursor {
			start++
		}
	}
	end := start + query.Limit
	if end > len(observed) {
		end = len(observed)
	}
	// 凭据级失败(所有仓都 unknown)从 503 降级为非阻塞标记(2026-09-20):
	// 建项不再选仓,表单能不能提交只看配置与 App 就绪;观测失败不再掀翻整个
	// 表单。单仓 unknown 是数据漂移(external id 不匹配/单仓探测失败)——
	// 那个仓不可选即可,同样不该拖垮整个表单。
	observationFailed := observation.AuthorizationFailed()
	if observationFailed {
		options.BlockingReasons = append(options.BlockingReasons, "AUTHORIZATION_UNCONFIRMED")
	}
	for index, item := range observed {
		if item.ParticipationStatus != "allowed" {
			continue
		}
		entry := CreationRepository{RepositoryID: item.Locator.ID, Selectable: true, Reasons: []string{}}
		if item.Item != nil {
			entry.DisplayName = item.Item.DisplayName
			entry.Selectable = item.Item.AppCapability.Status == "allowed"
			if !entry.Selectable {
				entry.Reasons = append(entry.Reasons, item.Item.AppCapability.ReasonCodes...)
				if len(entry.Reasons) == 0 {
					entry.Reasons = append(entry.Reasons, "APP_AUTHORIZATION_UNCONFIRMED")
				}
			}
		} else {
			entry.Selectable = false
			entry.Reasons = append(entry.Reasons, "AUTHORIZATION_UNCONFIRMED")
		}
		if item.ObservedAt != nil {
			entry.ObservedAt = item.ObservedAt.UTC().Format(time.RFC3339Nano)
		}
		if index >= start && index < end {
			options.Repositories = append(options.Repositories, entry)
		}
	}
	if end < len(observed) {
		options.NextCursor = observed[end-1].Locator.ID
	}
	// 建项不选仓(2026-09-20):能不能提交只看配置与 App 就绪;选几仓、有没有
	// 可选仓是①之后"选仓门"的事,这里不再用 available>0 挡提交。
	options.CanSubmit = configurationReady
	// Network observations cannot outlive their session or project revision.
	tx, err = s.beginCreate(ctx)
	if err != nil {
		return CreationOptions{}, err
	}
	defer rollbackTx(tx)
	if err = s.authorization.CheckProjectPrincipal(ctx, tx, principal); err != nil {
		return CreationOptions{}, err
	}
	current, err := s.projects.LockForConfiguration(ctx, tx, principal, projectID)
	if err != nil {
		return CreationOptions{}, err
	}
	if current.CreationContextRevision() != options.CreationContextRevision {
		return CreationOptions{}, failure(409, "CREATION_CONTEXT_CHANGED")
	}
	if !observationFailed {
		if err = s.authorization.CheckProjectObservationCached(ctx, tx, principal, observation); err != nil {
			return CreationOptions{}, err
		}
	}
	appReady, err := s.authorization.IssueAppCredentialReady(ctx, tx)
	if err != nil {
		return CreationOptions{}, err
	}
	if !appReady {
		options.CanSubmit = false
		options.BlockingReasons = append(options.BlockingReasons, "APP_AUTHORIZATION_UNCONFIRMED")
	}
	return options, nil
}

// readCreationContext fills the revision and submit-blocking fields of the
// options projection from the locked project row.
func (s *Service) readCreationContext(ctx context.Context, tx pgx.Tx, principal access.ProjectPrincipal, projectID string, options *CreationOptions) (string, bool, error) {
	project, err := s.projects.LockForConfiguration(ctx, tx, principal, projectID)
	if err != nil {
		return "", false, err
	}
	options.CreationContextRevision = project.CreationContextRevision()
	revision, ok := project.FixedRevision()
	if !ok {
		options.CanSubmit = false
		options.BlockingReasons = []string{"MODEL_CONFIG_MISSING"}
		return "", false, nil
	}
	return string(revision), true, nil
}

// readAllProjectRepositories returns every repository joined to the project.
func readAllProjectRepositories(ctx context.Context, tx pgx.Tx, projectID string) ([]access.RepositoryLocator, error) {
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
	if err := rows.Err(); err != nil {
		return nil, unavailable()
	}
	return result, nil
}

// ConversationCandidate is one selectable existing conversation.
type ConversationCandidate struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// ConversationPage is the paginated existing-conversation listing.
type ConversationPage struct {
	Items      []ConversationCandidate `json:"items"`
	NextCursor *string                 `json:"nextCursor"`
}

// Conversations lists the caller's selectable conversations for the
// existing-conversation mode. Only live conversations of this project are
// returned; removed conversations never reappear.
func (s *Service) Conversations(ctx context.Context, principal access.ProjectPrincipal, projectID string, query PageQuery) (ConversationPage, error) {
	tx, err := s.beginCreate(ctx)
	if err != nil {
		return ConversationPage{}, err
	}
	defer rollbackTx(tx)
	if err := s.authorization.CheckProjectPrincipal(ctx, tx, principal); err != nil {
		return ConversationPage{}, err
	}
	rows, err := tx.Query(ctx, `SELECT id, COALESCE(title,'') FROM repomesh_issues.conversations
		WHERE project_id=$1 AND removed_at IS NULL AND id > $2 ORDER BY id LIMIT $3`,
		projectID, query.Cursor, query.Limit)
	if err != nil {
		return ConversationPage{}, unavailable()
	}
	defer rows.Close()
	page := ConversationPage{Items: []ConversationCandidate{}}
	for rows.Next() {
		var candidate ConversationCandidate
		if rows.Scan(&candidate.ID, &candidate.Title) != nil {
			return ConversationPage{}, unavailable()
		}
		page.Items = append(page.Items, candidate)
	}
	if err := rows.Err(); err != nil {
		return ConversationPage{}, unavailable()
	}
	if len(page.Items) == query.Limit && query.Limit > 0 {
		last := page.Items[len(page.Items)-1].ID
		page.NextCursor = &last
	}
	return page, nil
}

// ScopeSelectionCommand 是选仓门批量确认端点的输入(spec 2026-09-20 §3.2):
// 确认的集合就是本 issue 的仓库范围。
type ScopeSelectionCommand struct {
	ProjectID                       string
	IssueID                         string
	RepositoryIDs                   []string
	DecidedBy                       string // manual|ai|timeout
	IdempotencyKey                  string
	ExpectedCreationContextRevision string
}

// ScopeSelectionReceipt 是确认端点的回执;重放返回 status=replayed。
type ScopeSelectionReceipt struct {
	Status          string `json:"status"`
	RepositoryCount int    `json:"repositoryCount"`
}

// ConfirmScopeSelection 一个事务里完成选仓门的批量确认:校验(≥1 仓、都在
// 项目内、creation context revision 匹配)→ 写 issue_repository_scope **和**
// issue_content_scope(双表,建项路径 insertWorkScope/appendProtectedScope 同款,
// 整组同一把 scope_revision)→ 门单列 CAS 置 resolved(消费 discovery.ResolveGateInTx)。
//
// ⚠️ 不复用单仓 AppendRepository:那条路径不原子、revision 逐仓碎裂、还漏写
// content_scope。幂等:同键重放返回原结果,不重写范围、不改门。
func (s *Service) ConfirmScopeSelection(ctx context.Context, principal access.ProjectPrincipal, command ScopeSelectionCommand) (ScopeSelectionReceipt, error) {
	repositories := make([]string, 0, len(command.RepositoryIDs))
	seen := map[string]bool{}
	for _, raw := range command.RepositoryIDs {
		repositoryID := strings.TrimSpace(raw)
		if repositoryID == "" || seen[repositoryID] {
			continue
		}
		seen[repositoryID] = true
		repositories = append(repositories, repositoryID)
	}
	switch {
	case len(repositories) == 0 && command.DecidedBy != "ai":
		// 「让 AI 定」允许空集合(spec 2026-09-20 修订):门先出现,点了才生成建议。
		return ScopeSelectionReceipt{}, failure(422, "REPOSITORIES_REQUIRED")
	case len(repositories) > 100:
		return ScopeSelectionReceipt{}, failure(422, "REPOSITORY_IDS_TOO_MANY")
	case command.DecidedBy != "manual" && command.DecidedBy != "ai" && command.DecidedBy != "timeout":
		return ScopeSelectionReceipt{}, failure(422, "DECIDED_BY_INVALID")
	case command.IdempotencyKey == "":
		return ScopeSelectionReceipt{}, failure(422, "IDEMPOTENCY_KEY_REQUIRED")
	case command.ExpectedCreationContextRevision == "":
		return ScopeSelectionReceipt{}, failure(422, "EXPECTED_CREATION_CONTEXT_REVISION_REQUIRED")
	}
	tx, err := s.beginCreate(ctx)
	if err != nil {
		return ScopeSelectionReceipt{}, err
	}
	defer rollbackTx(tx)
	if err := s.authorization.LockProjectPrincipal(ctx, tx, principal); err != nil {
		return ScopeSelectionReceipt{}, err
	}
	locked, err := s.projects.LockForConfiguration(ctx, tx, principal, command.ProjectID)
	if err != nil {
		return ScopeSelectionReceipt{}, err
	}
	var issueOperation string
	err = tx.QueryRow(ctx, `SELECT creation_operation_id FROM repomesh_issues.issues
		WHERE project_id=$1 AND id=$2 AND removed_at IS NULL`, command.ProjectID, command.IssueID).Scan(&issueOperation)
	if errors.Is(err, pgx.ErrNoRows) {
		return ScopeSelectionReceipt{}, failure(404, "RESOURCE_NOT_FOUND")
	}
	if err != nil {
		return ScopeSelectionReceipt{}, unavailableWith(err)
	}
	// 幂等重放先于其余校验:同键重放拿回原结果,不重写范围、不改门。
	if receipt, found, lookupErr := discovery.ScopeSelectionReceiptInTx(ctx, tx, command.IssueID, command.IdempotencyKey); lookupErr != nil {
		return ScopeSelectionReceipt{}, unavailableWith(lookupErr)
	} else if found {
		count, _ := receipt["repository_count"].(float64)
		// ai_requested 的重放**返回同状态**(spec 2026-09-20 修订):前端据 status
		// 决定提示,重放不该变成另一种说法。
		status := "replayed"
		if stored, _ := receipt["status"].(string); stored == "ai_requested" {
			status = "ai_requested"
		}
		return ScopeSelectionReceipt{Status: status, RepositoryCount: int(count)}, nil
	}
	if locked.CreationContextRevision() != command.ExpectedCreationContextRevision {
		return ScopeSelectionReceipt{}, failure(409, "CREATION_CONTEXT_CHANGED")
	}
	// 「让 AI 定」:请求里没有仓库集合(decidedBy=ai && repositoryIds 空)。
	//
	// spec 2026-09-20 修订:门在 ① 分析之后**就已出现**(那时建议为空),人在门上
	// 点了才去生成建议。所以这里分两种情形:
	//   · 建议已生成 → 采纳建议集合为范围,走原有双表+整组 revision+CAS 路径;
	//   · 建议还没生成 → 只记下"请 AI 定仓"(门单列 CAS 置 ai_requested),不写范围、
	//     不解决门,返回 status=ai_requested;协调器随后生成建议并**自动采纳**。
	if command.DecidedBy == "ai" && len(repositories) == 0 {
		gate, gateErr := discovery.GateInTx(ctx, tx, command.IssueID)
		if gateErr != nil {
			return ScopeSelectionReceipt{}, unavailableWith(gateErr)
		}
		if gate != nil && gate.State == discovery.GatePending && len(gate.Suggested) > 0 {
			adopted, resolveErr := discovery.RepositoryIDsForNamesInTx(ctx, tx, command.ProjectID, gate.Suggested)
			if resolveErr != nil {
				return ScopeSelectionReceipt{}, unavailableWith(resolveErr)
			}
			repositories = adopted
		} else {
			if _, markErr := discovery.MarkGateAIRequestedInTx(ctx, tx, command.IssueID); markErr != nil {
				return ScopeSelectionReceipt{}, unavailableWith(markErr)
			}
			receipt := ScopeSelectionReceipt{Status: "ai_requested", RepositoryCount: 0}
			if err := discovery.RecordScopeSelectionInTx(ctx, tx, command.IssueID, command.IdempotencyKey, map[string]any{
				"status": receipt.Status, "repository_count": 0, "decided_by": "ai",
			}); err != nil {
				return ScopeSelectionReceipt{}, unavailableWith(err)
			}
			if err := tx.Commit(ctx); err != nil {
				return ScopeSelectionReceipt{}, unavailableWith(err)
			}
			return receipt, nil
		}
	}
	var attached int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM repomesh_projects.project_repositories
		WHERE project_id=$1 AND repository_id = ANY($2)`, command.ProjectID, repositories).Scan(&attached); err != nil {
		return ScopeSelectionReceipt{}, unavailableWith(err)
	}
	if attached != len(repositories) {
		return ScopeSelectionReceipt{}, failure(409, "REPOSITORY_NOT_IN_PROJECT")
	}
	// 整组同一把 scope_revision:确认的集合作为一个整体有据可查。
	scopeRevision, err := newID("srev_")
	if err != nil {
		return ScopeSelectionReceipt{}, err
	}
	for _, repositoryID := range repositories {
		if _, err := tx.Exec(ctx, `INSERT INTO repomesh_issues.issue_repository_scope
			(issue_id, repository_id, project_id, scope_revision) VALUES ($1,$2,$3,$4)
			ON CONFLICT (issue_id, repository_id) DO UPDATE SET scope_revision = EXCLUDED.scope_revision`,
			command.IssueID, repositoryID, command.ProjectID, scopeRevision); err != nil {
			return ScopeSelectionReceipt{}, unavailableWith(err)
		}
		// 内容保护只增不减:已在保护内的仓保留原 introduced_by_operation。
		if _, err := tx.Exec(ctx, `INSERT INTO repomesh_issues.issue_content_scope
			(issue_id, repository_id, project_id, introduced_by_operation) VALUES ($1,$2,$3,$4)
			ON CONFLICT (issue_id, repository_id) DO NOTHING`,
			command.IssueID, repositoryID, command.ProjectID, issueOperation); err != nil {
			return ScopeSelectionReceipt{}, unavailableWith(err)
		}
	}
	// 门置 resolved:与双表写同一事务,整组提交或整组回滚。门不存在或已
	// resolved(查漏「补上并继续」的追加场景)不阻断,如实返回。
	if _, err := discovery.ResolveGateInTx(ctx, tx, command.IssueID, command.DecidedBy); err != nil {
		return ScopeSelectionReceipt{}, unavailableWith(err)
	}
	receipt := ScopeSelectionReceipt{Status: "committed", RepositoryCount: len(repositories)}
	if err := discovery.RecordScopeSelectionInTx(ctx, tx, command.IssueID, command.IdempotencyKey, map[string]any{
		"status": receipt.Status, "repository_count": receipt.RepositoryCount, "decided_by": command.DecidedBy,
	}); err != nil {
		return ScopeSelectionReceipt{}, unavailableWith(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ScopeSelectionReceipt{}, unavailableWith(err)
	}
	return receipt, nil
}
