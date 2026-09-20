package projects

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/github"
	"repomesh.local/repomesh/internal/secrets"
)

type FieldError struct {
	Field string `json:"field"`
	Code  string `json:"code"`
}

type Failure struct {
	Status      int
	Code        string
	FieldErrors []FieldError
	// Details carries opaque structured context (e.g. outstanding-test
	// pointers) that the web layer serializes under error.details.
	Details any
}

func (e *Failure) Error() string { return e.Code }

type ProfileChoice struct {
	Mode string `json:"mode"`
	ID   string `json:"id,omitempty"`
}

type ConfigurationChoice struct {
	ModelProfile     ProfileChoice `json:"modelProfile"`
	ExecutionProfile ProfileChoice `json:"executionProfile"`
}

type CreateInput struct {
	Name          string              `json:"name"`
	Purpose       string              `json:"purpose"`
	RepositoryIDs []string            `json:"repositoryIds"`
	Configuration ConfigurationChoice `json:"configuration"`
}

type UpdateInput struct {
	ExpectedProjectRevision string               `json:"expectedProjectRevision"`
	Name                    *string              `json:"name,omitempty"`
	Purpose                 *string              `json:"purpose,omitempty"`
	RepositoryIDsToAdd      *[]string            `json:"repositoryIdsToAdd,omitempty"`
	// RepositoryURLsToAdd 用**仓库 URL** 指名要接入的仓（2026-09-20）。
	//
	// repositoryIdsToAdd 只吃 `repo_<20 位 GitHub 数字 id>`，而那个 id 只有**发现面**
	// 给得出来；仓库页列的是**扫描目录**，id 是 32 位随机 hex —— 线上实测：用户点
	// 「接入本项目」四次，后端四次 422 VALIDATION_FAILED。扫描目录本来就有全 URL，
	// 所以让它直接报 URL：后端按 owner/name 去 GitHub 取数字 id、登记进项目注册表、
	// 再走**同一条**更新路径（revision、幂等台账、参与权观测、上限保护全都复用）。
	RepositoryURLsToAdd     *[]string            `json:"repositoryUrlsToAdd,omitempty"`
	Configuration           *ConfigurationChoice `json:"configuration,omitempty"`
}

type RawInput struct {
	fields map[string]json.RawMessage
	data   []byte
}

type CreateCommand struct {
	key   string
	input RawInput
}

type UpdateCommand struct {
	projectID string
	key       string
	input     RawInput
}

type Links struct {
	Project   string `json:"project"`
	Operation string `json:"operation"`
}

type CreationReceipt struct {
	ProjectCreationID string    `json:"projectCreationId"`
	Status            string    `json:"status"`
	ProjectID         string    `json:"projectId"`
	ProjectRevision   string    `json:"projectRevision"`
	CreatedAt         time.Time `json:"createdAt"`
	Links             Links     `json:"links"`
}

type UpdateReceipt struct {
	UpdateID        string    `json:"updateId"`
	Status          string    `json:"status"`
	ProjectID       string    `json:"projectId"`
	ProjectRevision string    `json:"projectRevision"`
	UpdatedAt       time.Time `json:"updatedAt"`
	Links           Links     `json:"links"`
	// AddedRepositoryIDs 是**本次更新真正新增**的仓库（项目侧 id，repo_…）。
	//
	// 2026-09-20 用户裁定："不是扫描就建队，是**确认接入**的时候才建队。" 判据就在
	// 这里：一次更新新增了几个仓库。单仓「接入本项目」= 人确认了一个 → 恰好 1 个；
	// 批量「全部接入本项目」= 不是逐仓确认 → 一次几十个。上层据此决定要不要建队。
	AddedRepositoryIDs []string `json:"addedRepositoryIds,omitempty"`
}

type CreateResult struct {
	Receipt     CreationReceipt
	FirstCommit bool
}

type EffectiveConfiguration struct {
	ConfigurationRevision    string  `json:"configurationRevision"`
	ModelProfileID           *string `json:"modelProfileId"`
	ExecutionProfileID       *string `json:"executionProfileId"`
	WorkerConcurrency        *int    `json:"workerConcurrency"`
	BudgetPolicyID           *string `json:"budgetPolicyId"`
	TimeLimitPolicyID        *string `json:"timeLimitPolicyId"`
	VerificationGroupEnabled *bool   `json:"verificationGroupEnabled"`
}

type ConfigurationView struct {
	ModelProfile     ProfileChoice          `json:"modelProfile"`
	ExecutionProfile ProfileChoice          `json:"executionProfile"`
	Effective        EffectiveConfiguration `json:"effective"`
	Checks           github.Capability      `json:"checks"`
	FixedSummary     FixedSummary           `json:"fixedSummary"`
	QuotaObservation QuotaObservation       `json:"quotaObservation"`
}

type Actions struct {
	CanEdit        bool `json:"canEdit"`
	CanCreateIssue bool `json:"canCreateIssue"`
}

type CreationReadiness struct {
	Status      string     `json:"status"`
	ReasonCodes []string   `json:"reasonCodes"`
	ObservedAt  *time.Time `json:"observedAt"`
}

type ProjectView struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	Purpose           string            `json:"purpose"`
	ProjectRevision   string            `json:"projectRevision"`
	CreatedAt         time.Time         `json:"createdAt"`
	Configuration     ConfigurationView `json:"configuration"`
	Actions           Actions           `json:"actions"`
	CreationReadiness CreationReadiness `json:"creationReadiness"`
}

type ProjectListItem struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	ProjectRevision string    `json:"projectRevision"`
	CreatedAt       time.Time `json:"createdAt"`
}

type ProjectPage struct {
	Items      []ProjectListItem `json:"items"`
	NextCursor *string           `json:"nextCursor"`
}

type ProjectRepositoryPage struct {
	Items                     []access.RepositoryItem `json:"items"`
	NextCursor                *string                 `json:"nextCursor"`
	ProjectRevision           string                  `json:"projectRevision"`
	RestrictedRepositoryCount int                     `json:"restrictedRepositoryCount"`
}

type ProfileItem struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Availability github.Capability `json:"availability"`
}

type ProfilePage struct {
	Items            []ProfileItem `json:"items"`
	NextCursor       *string       `json:"nextCursor"`
	DefaultProfileID *string       `json:"defaultProfileId"`
}

type ListQuery struct {
	Text, Cursor string
	Limit        int
}
type PageQuery struct {
	Cursor string
	Limit  int
}
type ProfileQuery struct {
	Kind, Cursor string
	Limit        int
}

type Service struct {
	pool   *pgxpool.Pool
	access *access.Service
	hook   transactionHook
	// Combination callbacks injected by the composition root. projects never
	// imports modelbudget; nil means the runtime quota surface is unavailable.
	requestWindowInitializer InitializeRequestWindow
	requestQuotaObserver     ObserveRequestQuota
}

func New(pool *pgxpool.Pool, authorization *access.Service) *Service {
	return &Service{pool: pool, access: authorization}
}

type operationScope struct{ actor, kind, projectID, key string }
type operationRecord struct {
	scope                      operationScope
	schemaVersion              int
	canonicalInput, exactInput []byte
	projectID, projectRevision string
	committedAt                time.Time
	removedAt                  *time.Time
}
type projectRecord struct {
	id, owner, organizationID, name, purpose, revision string
	creationContextRevision            string
	configurationRevision              string
	createdAt                          time.Time
	removedAt                          *time.Time
}
type secretReference struct {
	VersionID secrets.VersionID `json:"versionId"`
	OwnerKind string            `json:"ownerKind"`
	OwnerID   string            `json:"ownerId"`
	Purpose   secrets.Purpose   `json:"purpose"`
}
type profileBinding struct {
	ProfileID       string           `json:"profileId"`
	Version         string           `json:"version"`
	DefaultRevision *string          `json:"defaultRevision"`
	Secret          *secretReference `json:"secret"`
}
type fixedConfiguration struct {
	Selection                ConfigurationChoice `json:"selection"`
	Model                    *profileBinding     `json:"model"`
	Execution                *profileBinding     `json:"execution"`
	WorkerConcurrency        *int                `json:"workerConcurrency"`
	BudgetPolicyID           *string             `json:"budgetPolicyId"`
	TimeLimitPolicyID        *string             `json:"timeLimitPolicyId"`
	VerificationGroupEnabled *bool               `json:"verificationGroupEnabled"`
}
type profileRecord struct {
	kind, id, owner, name string
	enabled               bool
	currentVersion        string
}
type profileVersion struct {
	kind, profileID, version          string
	secret                            *secretReference
	parametersComplete                bool
	workerConcurrency                 *int
	budgetPolicyID, timeLimitPolicyID *string
	verificationGroupEnabled          *bool
}
type configurationRecord struct {
	projectID, revision string
	fixed               fixedConfiguration
	createdBy           string
	createdAt           time.Time
}
type normalizedInput struct {
	schemaVersion    int
	canonical, exact []byte
}
type updatePlan struct {
	current             projectRecord
	input               UpdateInput
	normalized          normalizedInput
	existing, additions []access.RepositoryLocator
	observation         access.ProjectObservation
}
type cursorScope struct {
	actor, kind, projectID, query string
	limit                         int
}
type cursorRecord struct {
	id                       string
	scope                    cursorScope
	afterID, projectRevision string
	expiresAt                time.Time
}
type transactionPhase string

const (
	authorizationObserved   transactionPhase = "authorization_observed"
	operationInserted       transactionPhase = "operation_inserted"
	projectWritten          transactionPhase = "project_written"
	repositoryWritten       transactionPhase = "repository_written"
	configurationWritten    transactionPhase = "configuration_written"
	projectReferenceWritten transactionPhase = "project_reference_written"
	receiptWritten          transactionPhase = "receipt_written"
	beforeCommit            transactionPhase = "before_commit"
)

type transactionHook func(context.Context, transactionPhase) error
