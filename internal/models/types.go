package models

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/projects"
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
}

func (e *Failure) Error() string { return e.Code }

type APIError struct {
	Code        string       `json:"code"`
	Message     string       `json:"message"`
	FieldErrors []FieldError `json:"fieldErrors"`
	RequestID   string       `json:"requestId"`
}

type SecretView struct {
	Configured   bool    `json:"configured"`
	VersionID    *string `json:"versionId"`
	Availability string  `json:"availability"`
}

type ModelView struct {
	ID              string `json:"id"`
	ModelProfileID  string `json:"modelProfileId"`
	ModelID         string `json:"modelId"`
	DisplayName     string `json:"displayName"`
	ContextWindow   int64  `json:"contextWindow"`
	MaxOutputTokens int64  `json:"maxOutputTokens"`
	Reasoning       bool   `json:"reasoning"`
	Vision          bool   `json:"vision"`
}

type ProviderView struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Revision  string      `json:"revision"`
	BaseURL   string      `json:"baseUrl"`
	APIFormat string      `json:"apiFormat"`
	Secret    SecretView  `json:"secret"`
	Models    []ModelView `json:"models"`
	UpdatedAt time.Time   `json:"updatedAt"`
}

type ProviderSummary struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Revision   string `json:"revision"`
	ModelCount int    `json:"modelCount"`
}

type ProviderPage struct {
	Items      []ProviderSummary `json:"items"`
	NextCursor *string           `json:"nextCursor"`
}

type ModelInput struct {
	ID              *string `json:"id"`
	ModelID         string  `json:"modelId"`
	DisplayName     *string `json:"displayName"`
	ContextWindow   int64   `json:"contextWindow"`
	MaxOutputTokens int64   `json:"maxOutputTokens"`
	Reasoning       bool    `json:"reasoning"`
	Vision          bool    `json:"vision"`
}

type RawSaveBody struct {
	data   []byte
	fields map[string]json.RawMessage
}

type SaveCommand struct {
	key       string
	requestID string
	body      RawSaveBody
}

func (c *SaveCommand) Clear() {
	if c == nil {
		return
	}
	clear(c.body.data)
	c.body.data = nil
	c.body.fields = nil
}

type CloseCommand struct{ key string }

type SaveLinks struct {
	Provider  string `json:"provider"`
	Operation string `json:"operation"`
}

type OperationLinks struct {
	Operation string `json:"operation"`
}

type CommittedSave struct {
	SaveID           string    `json:"saveId"`
	Outcome          string    `json:"outcome"`
	ProviderID       string    `json:"providerId"`
	ProviderRevision string    `json:"providerRevision"`
	SecretVersionID  string    `json:"secretVersionId"`
	CommittedAt      time.Time `json:"committedAt"`
	Links            SaveLinks `json:"links"`
}

type RejectedSave struct {
	SaveID    string    `json:"saveId"`
	Outcome   string    `json:"outcome"`
	Error     APIError  `json:"error"`
	DecidedAt time.Time `json:"decidedAt"`
}

type ClosedSave struct {
	SaveID   string         `json:"saveId"`
	Outcome  string         `json:"outcome"`
	ClosedAt time.Time      `json:"closedAt"`
	Links    OperationLinks `json:"links"`
}

type SaveReceipt struct {
	committed *CommittedSave
	rejected  *RejectedSave
	closed    *ClosedSave
}

func (r SaveReceipt) MarshalJSON() ([]byte, error) {
	switch {
	case r.committed != nil:
		return json.Marshal(r.committed)
	case r.rejected != nil:
		return json.Marshal(r.rejected)
	case r.closed != nil:
		return json.Marshal(r.closed)
	default:
		return nil, unavailable()
	}
}

type SaveResult struct {
	Receipt    SaveReceipt
	HTTPStatus int
}

type ListQuery struct {
	Text   string
	Cursor string
	Limit  int
}

type Service struct {
	pool          *pgxpool.Pool
	authorization *access.Service
	secrets       *secrets.Store
	catalog       *projects.CatalogWriter
	hook          transactionHook
}

func New(pool *pgxpool.Pool, authorization *access.Service, secretStore *secrets.Store, catalog *projects.CatalogWriter) *Service {
	return &Service{pool: pool, authorization: authorization, secrets: secretStore, catalog: catalog}
}

type Maintenance struct{ service *Service }

func NewMaintenance(service *Service) *Maintenance { return &Maintenance{service: service} }

type saveInput struct {
	providerID       *string
	expectedRevision *string
	name             string
	baseURL          string
	apiFormat        string
	replace          bool
	secret           []byte
	models           []ModelInput
}

type canonicalInput interface{ canonicalSaveInput() }

type keepInput struct {
	schemaVersion int
	canonical     []byte
}

func (keepInput) canonicalSaveInput() {}

type replaceInput struct {
	schemaVersion int
	canonical     []byte
	secret        []byte
}

func (replaceInput) canonicalSaveInput() {}

type operationRecord struct {
	actor         string
	key           string
	schemaVersion int
	kind          string
	target        *string
	canonical     []byte
	inputVersion  *secrets.VersionID
	receipt       []byte
	removedAt     *time.Time
	outcome       string
}

type savePreparation struct {
	providerID string
	business   *secrets.PreparedSecret
	vault      *secrets.PreparedSecret
}

func (p *savePreparation) clear() {
	if p.business != nil {
		p.business.Discard()
	}
	if p.vault != nil {
		p.vault.Discard()
	}
}

type transactionPhase string

const (
	principalLocked  transactionPhase = "principal_locked"
	slotLocked       transactionPhase = "slot_locked"
	secretPrepared   transactionPhase = "secret_prepared"
	secretsInserted  transactionPhase = "secrets_inserted"
	providerInserted transactionPhase = "provider_inserted"
	modelsInserted   transactionPhase = "models_inserted"
	profilesInserted transactionPhase = "profiles_inserted"
	headUpdated      transactionPhase = "head_updated"
	receiptInserted  transactionPhase = "receipt_inserted"
	closedInserted   transactionPhase = "closed_inserted"
	beforeCommit     transactionPhase = "before_commit"
)

type transactionHook func(context.Context, transactionPhase) error
