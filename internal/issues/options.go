package issues

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"repomesh.local/repomesh/internal/access"
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
// currently possible. Repositories not readable by the caller are hidden, and
// repositories whose live observation is unknown collapse to a 503.
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
	// 凭据级失败(所有仓都 unknown)仍然整体失败;单仓 unknown 是数据漂移
	// (external id 不匹配/单仓探测失败)——那个仓不可选即可,不该拖垮整个表单。
	if observation.AuthorizationFailed() {
		return CreationOptions{}, failure(503, "AUTHORIZATION_UNCONFIRMED")
	}
	available := 0
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
		if entry.Selectable {
			available++
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
	options.CanSubmit = configurationReady && available > 0
	if available == 0 {
		options.BlockingReasons = append(options.BlockingReasons, "NO_AVAILABLE_REPOSITORIES")
	}
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
	if err = s.authorization.CheckProjectObservationCached(ctx, tx, principal, observation); err != nil {
		return CreationOptions{}, err
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
