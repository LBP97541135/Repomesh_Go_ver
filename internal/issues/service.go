package issues

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/modelbudget"
	"repomesh.local/repomesh/internal/projects"
)

// Service implements atomic issue creation on top of the access, projects and
// modelbudget services. It owns no schema of its own beyond repomesh_issues.
type Service struct {
	pool          *pgxpool.Pool
	authorization *access.Service
	projects      *projects.Service
	budgets       *modelbudget.Store
	hook          transactionHook
}

// New wires the creation service to its collaborators.
func New(pool *pgxpool.Pool, authorization *access.Service, projectService *projects.Service, budgets *modelbudget.Store) *Service {
	return &Service{pool: pool, authorization: authorization, projects: projectService, budgets: budgets}
}

// transactionPhase enumerates the fixed pipeline positions inside the creation
// transaction; tests and the web layer can observe them through a hook.
type transactionPhase int

const (
	phasePrincipalLocked transactionPhase = iota
	phaseProjectLocked
	phaseOperationReserved
	phaseConversationInserted
	phaseIssueInserted
	phaseScopeInserted
	phaseMainChangeSetInserted
	phaseSourceAndCardInserted
	phaseOperationCompleted
	phaseContinuationInserted
	phaseEventInserted
	phaseBeforeCommit
)

type transactionHook func(context.Context, transactionPhase) error

func (s *Service) phase(ctx context.Context, at transactionPhase) error {
	if s.hook == nil {
		return nil
	}
	return s.hook(ctx, at)
}

func (s *Service) beginCreate(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, unavailableWith(err)
	}
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout='2s'`); err != nil {
		rollbackTx(tx)
		return nil, unavailableWith(err)
	}
	if _, err := tx.Exec(ctx, `SET LOCAL statement_timeout='10s'`); err != nil {
		rollbackTx(tx)
		return nil, unavailableWith(err)
	}
	if _, err := tx.Exec(ctx, `SET LOCAL synchronous_commit=on`); err != nil {
		rollbackTx(tx)
		return nil, unavailableWith(err)
	}
	return tx, nil
}

func rollbackTx(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}

// operationIdentity is the idempotency scope: project + trusted actor + page
// entry + client minted creationId.
type operationIdentity struct {
	projectID string
	actor     string
	entry     string
	key       OperationID
}

func pageIdentity(principal access.ProjectPrincipal, command PageCommand) operationIdentity {
	return operationIdentity{projectID: command.ProjectID, actor: principal.ActorID(), entry: "issue_page", key: command.Key}
}

// operationRecord is one row of creation_operations projected into memory.
type operationRecord struct {
	identity      operationIdentity
	schemaVersion int
	canonical     []byte
	issueID       IssueID
	changeSetID   ChangeSetID
	conversation  ConversationID
	configuration string
	receipt       []byte
	removed       bool
}

// committedCreation carries the aggregate identities one successful creation
// produced.
type committedCreation struct {
	issueID               IssueID
	issueNumber           int64
	mainChangeSetID       ChangeSetID
	conversationID        ConversationID
	configurationRevision projects.ConfigurationRevision
	createdAt             time.Time
}

// ScopeSnapshot is the observable state used by the options projection.
type ScopeSnapshot struct {
	ProjectID                 string
	IssueID                   IssueID
	ConversationID            ConversationID
	IssueScopeRevision        string
	ConversationScopeRevision string
	ReadRepositories          []access.RepositoryLocator
	WorkRepositories          []access.RepositoryLocator
}

// PageSource records the page that produced an issue.
type PageSource struct {
	operation operationIdentity
}

// CreationReceipt is the durable creation receipt persisted alongside the
// operation and returned in the HTTP envelope.
type CreationReceipt struct {
	OperationID OperationID       `json:"-"`
	ProjectID   string            `json:"projectId"`
	Result      committedCreation `json:"-"`
}

type receiptWire struct {
	IssueID         string `json:"issueId"`
	IssueNumber     int64  `json:"issueNumber"`
	MainChangeSetID string `json:"mainChangesetId"`
	ConversationID  string `json:"conversationId"`
	InitialConfig   string `json:"initialConfigurationRevision"`
	CreatedAt       string `json:"createdAt"`
}

// MarshalJSON emits the storage receipt projection whose keys the
// creation_receipt_consistent trigger validates.
func (r CreationReceipt) MarshalJSON() ([]byte, error) {
	return json.Marshal(receiptWire{
		IssueID:         r.Result.issueID,
		IssueNumber:     r.Result.issueNumber,
		MainChangeSetID: r.Result.mainChangeSetID,
		ConversationID:  r.Result.conversationID,
		InitialConfig:   string(r.Result.configurationRevision),
		CreatedAt:       r.Result.createdAt.UTC().Format(time.RFC3339Nano),
	})
}

// IssueLocation is the canonical API path of the created issue.
func (r CreationReceipt) IssueLocation() string {
	return "/api/issues/" + r.Result.issueID
}

// CreationResult distinguishes first commits from replays.
type CreationResult struct {
	Receipt  CreationReceipt
	Replayed bool
}

// ReadResult is the projection of a committed operation for the query route.
type ReadResult = CreationReceipt

// CreatePage runs the full page creation pipeline: authorization, idempotent
// lookup, out-of-transaction observation, then the single short commit
// transaction that either establishes the whole aggregate or nothing.
func (s *Service) CreatePage(ctx context.Context, principal access.ProjectPrincipal, command PageCommand) (CreationResult, error) {
	identity := pageIdentity(principal, command)
	observation := creationObservation{}

	authorized, err := s.readAuthorizedOperation(ctx, principal, identity)
	if err != nil {
		return CreationResult{}, err
	}
	if authorized.removed {
		return CreationResult{}, failure(410, "CREATION_RESULT_REMOVED")
	}
	if authorized.receipt != nil {
		replay, err := s.replayAfterPreflight(ctx, principal, identity, command, authorized)
		if err != nil {
			return CreationResult{}, err
		}
		if replay != nil {
			return *replay, nil
		}
	}

	input, err := parseNewInput(command.Body)
	if err != nil {
		return CreationResult{}, err
	}
	if err := s.observeAndCheck(ctx, principal, identity, input, &observation); err != nil {
		return CreationResult{}, err
	}

	result, err := s.commitNew(ctx, principal, command, identity, input, observation)
	if err != nil {
		if isConflict(err) {
			conflict, replayErr := s.replayAfterPreflight(ctx, principal, identity, command, authorized)
			if replayErr == nil && conflict != nil {
				return *conflict, nil
			}
		}
		return CreationResult{}, err
	}
	return result, nil
}

// creationObservation carries the out-of-transaction authorization observation
// into the commit transaction.
type creationObservation struct {
	accessObservation access.IssueObservation
	request           access.IssueAccessRequest
	observed          bool
}

func isConflict(err error) bool {
	var conflict *Failure
	if errors.As(err, &conflict) {
		return conflict.Status == 409
	}
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		state := pgErr.SQLState()
		return state == "23505" || state == "40001"
	}
	return false
}

// readAuthorizedOperation loads the existing operation row for this identity,
// if any, after confirming the caller may read the project and its results.
func (s *Service) readAuthorizedOperation(ctx context.Context, principal access.ProjectPrincipal, identity operationIdentity) (operationRecord, error) {
	tx, err := s.beginCreate(ctx)
	if err != nil {
		return operationRecord{}, err
	}
	defer rollbackTx(tx)
	if err := s.authorization.LockProjectPrincipal(ctx, tx, principal); err != nil {
		return operationRecord{}, err
	}
	record, err := readOperation(ctx, tx, identity)
	if err != nil {
		return operationRecord{}, err
	}
	return record, nil
}

func readOperation(ctx context.Context, tx pgx.Tx, identity operationIdentity) (operationRecord, error) {
	var record operationRecord
	var canonical, exact, receipt []byte
	var issueID, changeSetID, conversationID, configuration *string
	var removed *time.Time
	err := tx.QueryRow(ctx, `SELECT id, schema_version, canonical_input, exact_input, issue_id, main_changeset_id,
		conversation_id, initial_configuration_revision, receipt, removed_at
		FROM repomesh_issues.creation_operations
		WHERE project_id=$1 AND actor=$2 AND entry=$3 AND creation_id=$4`,
		identity.projectID, identity.actor, identity.entry, string(identity.key)).Scan(
		&record.identity, &record.schemaVersion, &canonical, &exact, &issueID, &changeSetID, &conversationID, &configuration, &receipt, &removed)
	record.identity = identity
	if errors.Is(err, pgx.ErrNoRows) {
		return operationRecord{identity: identity, schemaVersion: 1}, nil
	}
	if err != nil {
		return operationRecord{}, unavailable()
	}
	record.canonical = canonical
	if issueID != nil {
		record.issueID = *issueID
		record.changeSetID = deref(changeSetID)
		record.conversation = deref(conversationID)
		record.configuration = deref(configuration)
		record.receipt = receipt
	}
	record.removed = removed != nil
	return record, nil
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// replayAfterPreflight re-checks read authorization for a committed result and
// returns the stored receipt. It never re-evaluates creation requirements for a
// historical success; a missing or unreadable result surfaces 410 or 404.
func (s *Service) replayAfterPreflight(ctx context.Context, principal access.ProjectPrincipal, identity operationIdentity, command PageCommand, record operationRecord) (*CreationResult, error) {
	if record.receipt == nil {
		return nil, nil
	}
	var wire receiptWire
	if err := json.Unmarshal(record.receipt, &wire); err != nil {
		return nil, unavailable()
	}
	createdAt, err := time.Parse(time.RFC3339Nano, wire.CreatedAt)
	if err != nil {
		return nil, unavailable()
	}
	receipt := CreationReceipt{
		OperationID: record.identity.key,
		ProjectID:   identity.projectID,
		Result: committedCreation{
			issueID:               wire.IssueID,
			issueNumber:           wire.IssueNumber,
			mainChangeSetID:       wire.MainChangeSetID,
			conversationID:        wire.ConversationID,
			configurationRevision: projects.ConfigurationRevision(wire.InitialConfig),
			createdAt:             createdAt,
		},
	}
	return &CreationResult{Receipt: receipt, Replayed: true}, nil
}

// observeAndCheck runs the out-of-transaction authorization observation: the
// read set (protected content union) is every project repository; the work set
// is the selected repositories. Unknown providers degrade to 503 inside the
// commit transaction, never to a false denial.
func (s *Service) observeAndCheck(ctx context.Context, principal access.ProjectPrincipal, identity operationIdentity, input pageInput, observation *creationObservation) error {
	tx, err := s.beginCreate(ctx)
	if err != nil {
		return err
	}
	defer rollbackTx(tx)
	if err := s.authorization.LockProjectPrincipal(ctx, tx, principal); err != nil {
		return err
	}
	locators, err := readProjectRepositories(ctx, tx, identity.projectID, input.repositories)
	if err != nil {
		return err
	}
	observation.request = access.IssueAccessRequest{ReadRepositories: locators, WorkRepositories: locators}
	rollbackTx(tx)

	observed, err := s.authorization.ObserveIssueAccess(ctx, principal, observation.request)
	if err != nil {
		return err
	}
	observation.accessObservation = observed
	observation.observed = true
	return nil
}

// readProjectRepositories resolves the selected repository IDs against the
// project's joined repositories; a selection outside the project is 404.
// hitlModeOrDefault 把这次建项的人审门模式收进一个合法值。
//
// 默认 hitl（门等真人）：最保守的缺省，不替任何人做主 —— 协调器的自动托管循环
// 只在 ai 模式下才代行 ③ 分档审批与 ⑤ 物化确认。
func hitlModeOrDefault(mode string) string {
	switch mode {
	case "ai", "hitl":
		return mode
	default:
		return "hitl"
	}
}

func readProjectRepositories(ctx context.Context, tx pgx.Tx, projectID string, selected []string) ([]access.RepositoryLocator, error) {
	set := make(map[string]bool, len(selected))
	for _, id := range selected {
		set[id] = true
	}
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
		if set[item.ID] {
			result = append(result, item)
			delete(set, item.ID)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, unavailable()
	}
	if len(set) > 0 {
		return nil, failure(422, "VALIDATION_FAILED")
	}
	return result, nil
}

// checkedCreation binds the locked project, fixed configuration and input
// together for the commit pipeline.
type checkedCreation struct {
	project   projects.LockedProject
	fixed     projects.FixedConfiguration
	input     pageInput
	scope     creationObservation
	checkedAt time.Time
}

// commitNew is the single short transaction that either establishes the whole
// aggregate or nothing. Phases fire the hook between steps so tests can observe
// pipeline position; every failure rolls back completely.
func (s *Service) commitNew(ctx context.Context, principal access.ProjectPrincipal, command PageCommand, identity operationIdentity, input pageInput, observation creationObservation) (CreationResult, error) {
	tx, err := s.beginCreate(ctx)
	if err != nil {
		return CreationResult{}, err
	}
	defer rollbackTx(tx)

	if err := s.authorization.LockProjectPrincipal(ctx, tx, principal); err != nil {
		return CreationResult{}, err
	}
	if err := s.phase(ctx, phasePrincipalLocked); err != nil {
		return CreationResult{}, err
	}

	project, err := s.projects.LockForConfiguration(ctx, tx, principal, identity.projectID)
	if err != nil {
		return CreationResult{}, err
	}
	if err := s.phase(ctx, phaseProjectLocked); err != nil {
		return CreationResult{}, err
	}

	existing, err := readOperation(ctx, tx, identity)
	if err != nil {
		return CreationResult{}, err
	}
	if existing.removed {
		return CreationResult{}, failure(410, "CREATION_RESULT_REMOVED")
	}
	if existing.receipt != nil {
		replay, err := s.replayAfterPreflight(ctx, principal, identity, command, existing)
		if err != nil {
			return CreationResult{}, err
		}
		if replay != nil {
			return *replay, nil
		}
	}
	canonical := canonicalize(input)
	if existing.canonical != nil && !bytes.Equal(existing.canonical, canonical) {
		return CreationResult{}, failure(409, "IDEMPOTENCY_CONFLICT")
	}

	checked := checkedCreation{project: project, input: input, scope: observation, checkedAt: time.Now().UTC()}
	fixedRevision, hasFixed := project.FixedRevision()
	if !hasFixed {
		return CreationResult{}, failure(409, "CREATION_REQUIREMENTS_UNMET")
	}
	fixed, err := s.projects.InspectFixedForCreation(ctx, tx, principal, identity.projectID, fixedRevision, checked.checkedAt)
	if err != nil {
		return CreationResult{}, err
	}
	checked.fixed = fixed.Configuration
	if fixed.Status != "ready" {
		return CreationResult{}, &Failure{Status: 409, Code: "CREATION_REQUIREMENTS_UNMET",
			Fields: []projects.FieldError{}}
	}
	if input.expectedContext != project.CreationContextRevision() {
		return CreationResult{}, failure(409, "CREATION_CONTEXT_CHANGED")
	}

	if !observation.observed {
		return CreationResult{}, failure(503, "AUTHORIZATION_UNCONFIRMED")
	}
	if err := s.authorization.CheckIssueObservation(ctx, tx, principal, observation.accessObservation, observation.request); err != nil {
		return CreationResult{}, err
	}
	if err := s.authorization.CheckIssueCredentialAvailability(ctx, tx, observation.accessObservation); err != nil {
		return CreationResult{}, err
	}
	if err := s.authorization.CheckIssueObservationTime(ctx, tx, principal, observation.accessObservation); err != nil {
		return CreationResult{}, err
	}

	reserved, err := reserveCreationIdentity(ctx, tx, identity, canonical, command.Body)
	if err != nil {
		return CreationResult{}, err
	}
	if !reserved && existing.canonical == nil {
		return CreationResult{}, failure(503, "RESULT_UNCONFIRMED")
	}
	if err := s.phase(ctx, phaseOperationReserved); err != nil {
		return CreationResult{}, err
	}

	issueNumber, err := allocateIssueNumber(ctx, tx, identity.projectID)
	if err != nil {
		return CreationResult{}, err
	}
	conversationID, err := s.insertNecessaryConversation(ctx, tx, identity, input)
	if err != nil {
		return CreationResult{}, err
	}
	if err := s.phase(ctx, phaseConversationInserted); err != nil {
		return CreationResult{}, err
	}
	created, err := insertIssueAndMainChangeSet(ctx, tx, identity, input, issueNumber, conversationID, string(fixedRevision))
	if err != nil {
		return CreationResult{}, err
	}
	if err := s.phase(ctx, phaseIssueInserted); err != nil {
		return CreationResult{}, err
	}

	if err := insertWorkScope(ctx, tx, identity, created, input.repositories); err != nil {
		return CreationResult{}, err
	}
	if err := appendProtectedScope(ctx, tx, identity, created, input.repositories); err != nil {
		return CreationResult{}, err
	}
	if err := s.phase(ctx, phaseScopeInserted); err != nil {
		return CreationResult{}, err
	}
	if err := insertPageSourceAndCard(ctx, tx, identity, created, input.title); err != nil {
		return CreationResult{}, err
	}
	if err := s.phase(ctx, phaseSourceAndCardInserted); err != nil {
		return CreationResult{}, err
	}

	receipt := CreationReceipt{OperationID: identity.key, ProjectID: identity.projectID, Result: created}
	receiptJSON, err := json.Marshal(receipt)
	if err != nil {
		return CreationResult{}, unavailable()
	}
	if err := completeCreationOperation(ctx, tx, identity, created, string(fixedRevision), receiptJSON); err != nil {
		return CreationResult{}, err
	}
	if err := s.phase(ctx, phaseOperationCompleted); err != nil {
		return CreationResult{}, err
	}
	if err := insertBlockedContinuation(ctx, tx, identity, created); err != nil {
		return CreationResult{}, err
	}
	if err := s.phase(ctx, phaseContinuationInserted); err != nil {
		return CreationResult{}, err
	}
	if err := appendCreationInvalidation(ctx, tx, identity, created); err != nil {
		return CreationResult{}, err
	}
	if err := s.phase(ctx, phaseEventInserted); err != nil {
		return CreationResult{}, err
	}
	if err := s.phase(ctx, phaseBeforeCommit); err != nil {
		return CreationResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CreationResult{}, unavailable()
	}
	return CreationResult{Receipt: receipt}, nil
}

// reserveCreationIdentity inserts the uncommitted placeholder row for this
// creation identity. It returns false when the row already exists with the
// same canonical input (another attempt owns the aggregate build).
func reserveCreationIdentity(ctx context.Context, tx pgx.Tx, identity operationIdentity, canonical, exact []byte) (bool, error) {
	operationID, err := newID("opr_")
	if err != nil {
		return false, err
	}
	digestBytes := []byte(digest(string(canonical)))
	command, err := tx.Exec(ctx, `INSERT INTO repomesh_issues.creation_operations
		(project_id, actor, entry, creation_id, id, schema_version, canonical_input, exact_input, input_digest)
		VALUES ($1,$2,'issue_page',$3,$4,1,$5,$6,$7)
		ON CONFLICT (project_id, actor, entry, creation_id) DO NOTHING`,
		identity.projectID, identity.actor, string(identity.key), operationID, canonical, exact, digestBytes)
	if err != nil {
		return false, unavailable()
	}
	return command.RowsAffected() == 1, nil
}

// allocateIssueNumber atomically advances the project counter and returns the
// fresh issue number.
func allocateIssueNumber(ctx context.Context, tx pgx.Tx, projectID string) (int64, error) {
	var number int64
	err := tx.QueryRow(ctx, `INSERT INTO repomesh_issues.project_issue_counters (project_id, next_number)
		VALUES ($1, 2)
		ON CONFLICT (project_id) DO UPDATE SET next_number = repomesh_issues.project_issue_counters.next_number + 1
		RETURNING CASE WHEN next_number = 2 AND xmax = 0 THEN 1 ELSE next_number - 1 END`,
		projectID).Scan(&number)
	if err != nil {
		return 0, unavailable()
	}
	return number, nil
}

// insertNecessaryConversation creates the new main conversation for mode=new.
// The existing-conversation mode is rejected with 409 CONVERSATION_UNAVAILABLE
// because the aggregate trigger requires the conversation's creation and title
// origin to be this very operation.
func (s *Service) insertNecessaryConversation(ctx context.Context, tx pgx.Tx, identity operationIdentity, input pageInput) (ConversationID, error) {
	switch input.conversation.(type) {
	case existingConversation:
		return "", failure(409, "CONVERSATION_UNAVAILABLE")
	}
	conversationID, err := newID("conv_")
	if err != nil {
		return "", err
	}
	operationRow, err := operationRowID(ctx, tx, identity)
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_issues.conversations
		(id, project_id, title, created_by_operation_id, title_origin_operation_id, content_scope_revision)
		VALUES ($1,$2,$3,$4,$4,$5)`,
		conversationID, identity.projectID, input.title, operationRow, identity.projectID+":creation"); err != nil {
		return "", unavailable()
	}
	return conversationID, nil
}

func operationRowID(ctx context.Context, tx pgx.Tx, identity operationIdentity) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `SELECT id FROM repomesh_issues.creation_operations
		WHERE project_id=$1 AND actor=$2 AND entry=$3 AND creation_id=$4`,
		identity.projectID, identity.actor, identity.entry, string(identity.key)).Scan(&id)
	if err != nil {
		return "", unavailable()
	}
	return id, nil
}

// insertIssueAndMainChangeSet writes the issue row and its main changeset in
// one step; the changeset's deferred issue FK resolves at commit time.
func insertIssueAndMainChangeSet(ctx context.Context, tx pgx.Tx, identity operationIdentity, input pageInput, number int64, conversationID ConversationID, configuration string) (committedCreation, error) {
	operationRow, err := operationRowID(ctx, tx, identity)
	if err != nil {
		return committedCreation{}, err
	}
	issueID, err := newID("iss_")
	if err != nil {
		return committedCreation{}, err
	}
	changeSetID, err := newID("cs_")
	if err != nil {
		return committedCreation{}, err
	}
	criteria, err := json.Marshal(input.criteria)
	if err != nil {
		criteria = []byte("[]")
	}
	revision, err := newID("irev_")
	if err != nil {
		return committedCreation{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_issues.issues
		(id, project_id, number, title, description, criteria, revision, main_conversation_id, main_changeset_id,
		 initial_configuration_revision, creation_operation_id, hitl_mode)
		VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7,$8,$9,$10,$11,$12)`,
		issueID, identity.projectID, number, input.title, input.description, string(criteria), revision,
		conversationID, changeSetID, configuration, operationRow, hitlModeOrDefault(input.hitlMode)); err != nil {
		return committedCreation{}, unavailable()
	}
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_issues.changesets (id, project_id, issue_id, kind)
		VALUES ($1,$2,$3,'main')`, changeSetID, identity.projectID, issueID); err != nil {
		return committedCreation{}, unavailable()
	}
	return committedCreation{
		issueID:               issueID,
		issueNumber:           number,
		mainChangeSetID:       changeSetID,
		conversationID:        conversationID,
		configurationRevision: projects.ConfigurationRevision(configuration),
		createdAt:             time.Now().UTC(),
	}, nil
}

// insertWorkScope writes the work (repository) scope rows; appendProtectedScope
// writes the content scope for issue and conversation. Both derive from the
// same selected repository set.
func insertWorkScope(ctx context.Context, tx pgx.Tx, identity operationIdentity, created committedCreation, repositories []string) error {
	scopeRevision, err := newID("srev_")
	if err != nil {
		return err
	}
	for _, repositoryID := range repositories {
		if _, err := tx.Exec(ctx, `INSERT INTO repomesh_issues.issue_repository_scope
			(issue_id, repository_id, project_id, scope_revision) VALUES ($1,$2,$3,$4)`,
			created.issueID, repositoryID, identity.projectID, scopeRevision); err != nil {
			return unavailable()
		}
	}
	return nil
}

func appendProtectedScope(ctx context.Context, tx pgx.Tx, identity operationIdentity, created committedCreation, repositories []string) error {
	operationRow, err := operationRowID(ctx, tx, identity)
	if err != nil {
		return err
	}
	for _, repositoryID := range repositories {
		if _, err := tx.Exec(ctx, `INSERT INTO repomesh_issues.issue_content_scope
			(issue_id, repository_id, project_id, introduced_by_operation) VALUES ($1,$2,$3,$4)`,
			created.issueID, repositoryID, identity.projectID, operationRow); err != nil {
			return unavailable()
		}
		if _, err := tx.Exec(ctx, `INSERT INTO repomesh_issues.conversation_content_scope
			(conversation_id, repository_id, project_id, introduced_by_operation) VALUES ($1,$2,$3,$4)`,
			created.conversationID, repositoryID, identity.projectID, operationRow); err != nil {
			return unavailable()
		}
	}
	return nil
}

// insertPageSourceAndCard records the page origin and its conversation card.
func insertPageSourceAndCard(ctx context.Context, tx pgx.Tx, identity operationIdentity, created committedCreation, summary string) error {
	operationRow, err := operationRowID(ctx, tx, identity)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_issues.page_sources
		(operation_id, project_id, issue_id, conversation_id, summary) VALUES ($1,$2,$3,$4,$5)`,
		operationRow, identity.projectID, created.issueID, created.conversationID, summary); err != nil {
		return unavailable()
	}
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_issues.conversation_cards
		(source_id, project_id, conversation_id, issue_id, operation_id, summary) VALUES ($1,$2,$3,$4,$5,$6)`,
		operationRow, identity.projectID, created.conversationID, created.issueID, operationRow, summary); err != nil {
		return unavailable()
	}
	return nil
}

// completeCreationOperation performs the single UPDATE that fills every
// identity column and the receipt in one shot; immutable_creation_identity
// forbids touching them again afterwards.
func completeCreationOperation(ctx context.Context, tx pgx.Tx, identity operationIdentity, created committedCreation, configuration string, receiptJSON []byte) error {
	command, err := tx.Exec(ctx, `UPDATE repomesh_issues.creation_operations
		SET issue_id=$5, main_changeset_id=$6, conversation_id=$7, initial_configuration_revision=$8, receipt=$9::jsonb
		WHERE project_id=$1 AND actor=$2 AND entry=$3 AND creation_id=$4`,
		identity.projectID, identity.actor, identity.entry, string(identity.key),
		created.issueID, created.mainChangeSetID, created.conversationID, configuration, receiptJSON)
	if err != nil {
		return unavailable()
	}
	if command.RowsAffected() != 1 {
		return failure(503, "RESULT_UNCONFIRMED")
	}
	return nil
}

// insertBlockedContinuation writes the single blocked continuation work row the
// aggregate trigger requires; the runtime continuation flow is B07 scope.
func insertBlockedContinuation(ctx context.Context, tx pgx.Tx, identity operationIdentity, created committedCreation) error {
	workID, err := newID("wrk_")
	if err != nil {
		return err
	}
	operationRow, err := operationRowID(ctx, tx, identity)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_issues.continuation_work
		(work_id, project_id, issue_id, cause_operation_id, kind, state, reason)
		VALUES ($1,$2,$3,$4,'issue_continue','blocked','INTEGRATION_NOT_AVAILABLE')`,
		workID, identity.projectID, created.issueID, operationRow); err != nil {
		return unavailable()
	}
	return nil
}

// appendCreationInvalidation writes the first event stream generation and the
// snapshot_invalidated event that listeners consume to refresh projections.
func appendCreationInvalidation(ctx context.Context, tx pgx.Tx, identity operationIdentity, created committedCreation) error {
	payload, err := json.Marshal(map[string]any{
		"issueId":     created.issueID,
		"projectId":   identity.projectID,
		"operationId": string(identity.key),
	})
	if err != nil {
		payload = []byte("{}")
	}
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_issues.issue_event_streams
		(issue_id, project_id, generation, last_sequence) VALUES ($1,$2,1,0)`,
		created.issueID, identity.projectID); err != nil {
		return unavailable()
	}
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_issues.issue_events
		(issue_id, generation, sequence, kind, payload) VALUES ($1,1,1,'snapshot_invalidated',$2::jsonb)`,
		created.issueID, payload); err != nil {
		return unavailable()
	}
	return nil
}

// GetPageCreation returns the committed receipt for one creation identity; a
// missing operation is 404, a removed one is 410.
func (s *Service) GetPageCreation(ctx context.Context, principal access.ProjectPrincipal, projectID string, key OperationID) (ReadResult, error) {
	identity := operationIdentity{projectID: projectID, actor: principal.ActorID(), entry: "issue_page", key: key}
	tx, err := s.beginCreate(ctx)
	if err != nil {
		return ReadResult{}, err
	}
	defer rollbackTx(tx)
	if err := s.authorization.LockProjectPrincipal(ctx, tx, principal); err != nil {
		return ReadResult{}, err
	}
	record, err := readOperation(ctx, tx, identity)
	if err != nil {
		return ReadResult{}, err
	}
	if record.removed {
		return ReadResult{}, failure(410, "CREATION_RESULT_REMOVED")
	}
	if record.receipt == nil {
		return ReadResult{}, failure(404, "CREATION_NOT_FOUND")
	}
	receipt, err := decodeReceipt(record)
	if err != nil {
		return ReadResult{}, err
	}
	return receipt, nil
}

func decodeReceipt(record operationRecord) (CreationReceipt, error) {
	var wire receiptWire
	if err := json.Unmarshal(record.receipt, &wire); err != nil {
		return CreationReceipt{}, unavailable()
	}
	createdAt, err := time.Parse(time.RFC3339Nano, wire.CreatedAt)
	if err != nil {
		return CreationReceipt{}, unavailable()
	}
	return CreationReceipt{
		OperationID: record.identity.key,
		ProjectID:   record.identity.projectID,
		Result: committedCreation{
			issueID:               wire.IssueID,
			issueNumber:           wire.IssueNumber,
			mainChangeSetID:       wire.MainChangeSetID,
			conversationID:        wire.ConversationID,
			configurationRevision: projects.ConfigurationRevision(wire.InitialConfig),
			createdAt:             createdAt,
		},
	}, nil
}
