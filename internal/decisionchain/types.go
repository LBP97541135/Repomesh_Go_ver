// Package decisionchain is the 历史决策 module (design:
// docs/历史决策终版设计-Go蓝图与可开可关-2026-09-15.md, D9-D14). It stores
// decision nodes (scope confirmations first), embeds them for semantic
// recall with a structural fallback, and exposes read + toggle APIs.
package decisionchain

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Event is the producer-side write shape (方案清单 §3 写路径①). One scope
// confirmation becomes one decision node with step=confirmation.
// Step/Status/ActorType/UpstreamRef are optional overrides — zero values
// keep the confirmation defaults; UpstreamRef links the triggering decision
// node (双轴挂钩：重规划节点指回 BLOCKED 上报).
type Event struct {
	Requirement    string         // raw requirement text, normalized here
	Actor          string         // session username
	ActorType      string         // optional: llm|human|service (default human)
	IdempotencyKey string         // producer idempotency key, stored as event_id
	RepositoryIDs  []string       // selected repository ids, resolved to names here
	Accepted       bool           // true = confirmed, false = rejected
	ProjectID      string         // optional: owning project (A套 project uuid); "" = chain root without project
	Step           DecisionStep   // optional; default confirmation
	Status         DecisionStatus // optional; default derived from Accepted
	UpstreamRef    string         // optional: parent decision node id (uuid)
	Action         string         // optional; default 范围圈定确认
	Rationale      string         // optional; default truncated requirement
	ContextRef     map[string]any // optional; default {"sourceIds": [...]}
}

// Config is the module assembly; every field maps to a design decision.
type Config struct {
	// EmbeddingBaseURL empty = semantic recall off; recall degrades to
	// structural (D11/D12 semantics, mirrors the Python behavior).
	EmbeddingBaseURL string
	EmbeddingAPIKey  string
	// EmbeddingModel defaults to bge-m3; vectors are stored as vector(1024).
	EmbeddingModel string
	// ResolveName maps a repository id to its normalized name. ok=false keeps
	// the raw id in affected_repositories (F4: names first, ids as fallback).
	ResolveName func(ctx context.Context, id string) (name string, ok bool)
}

// DecisionStep is the five-step chain, ordered as the Python contract.
type DecisionStep string

const (
	StepClassification DecisionStep = "classification"
	StepConfirmation   DecisionStep = "confirmation"
	StepIntegration    DecisionStep = "integration"
	StepTask           DecisionStep = "task"
	StepPR             DecisionStep = "pr"
)

// Valid reports whether the step is one of the five chain steps.
func (s DecisionStep) Valid() bool {
	switch s {
	case StepClassification, StepConfirmation, StepIntegration, StepTask, StepPR:
		return true
	}
	return false
}

// DecisionStatus enumerates the Python contract's full status set. Phase 1
// only writes confirmed/rejected (scope accept/reject).
type DecisionStatus string

const (
	StatusProposed         DecisionStatus = "proposed"
	StatusAdjusted         DecisionStatus = "adjusted"
	StatusConfirmed        DecisionStatus = "confirmed"
	StatusRejected         DecisionStatus = "rejected"
	StatusChangesRequested DecisionStatus = "changes_requested"
	StatusBlocked          DecisionStatus = "blocked"
	StatusSuperseded       DecisionStatus = "superseded"
	StatusMerged           DecisionStatus = "merged"
	StatusClosed           DecisionStatus = "closed"
)

// Valid reports whether the status is one of the contract states.
func (s DecisionStatus) Valid() bool {
	switch s {
	case StatusProposed, StatusAdjusted, StatusConfirmed, StatusRejected,
		StatusChangesRequested, StatusBlocked, StatusSuperseded, StatusMerged, StatusClosed:
		return true
	}
	return false
}

// NodeSource tells a live event from a manual backfill.
type NodeSource string

const (
	SourceEvent    NodeSource = "event"
	SourceBackfill NodeSource = "backfill"
)

// DecisionNode is the read shape: one decision sheet.
type DecisionNode struct {
	ID                   string         `json:"id"`
	EventID              string         `json:"eventId"`
	RequirementText      string         `json:"requirementText"`
	RequirementKey       string         `json:"requirementKey"`
	ProjectID            string         `json:"projectId"`
	ParentNodeID         string         `json:"parentNodeId"`
	Step                 DecisionStep   `json:"step"`
	Version              int            `json:"version"`
	Status               DecisionStatus `json:"status"`
	ActorType            string         `json:"actorType"`
	ActorID              string         `json:"actorId"`
	Action               string         `json:"action"`
	Rationale            string         `json:"rationale"`
	ContextRef           map[string]any `json:"contextRef"`
	AffectedRepositories []string       `json:"affectedRepositories"`
	Source               NodeSource     `json:"source"`
	CreatedAt            time.Time      `json:"createdAt"`
}

// NormalizeRequirement makes the retrieval key stable against accidental
// whitespace differences (F1): trim edges, collapse internal runs of any
// unicode whitespace to one space. Case is preserved on purpose — changing
// it would silently rewrite the requirement.
func NormalizeRequirement(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// RequirementKey hashes the normalized text into the retrieval/version key
// (sha256, first 32 hex chars).
func RequirementKey(normalized string) string {
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])[:32]
}

// NewUUIDv4 returns a random RFC 4122 version-4 UUID string.
func NewUUIDv4() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic("decisionchain: entropy unavailable: " + err.Error())
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	dst := make([]byte, 36)
	hex.Encode(dst[0:8], raw[0:4])
	dst[8] = '-'
	hex.Encode(dst[9:13], raw[4:6])
	dst[13] = '-'
	hex.Encode(dst[14:18], raw[6:8])
	dst[18] = '-'
	hex.Encode(dst[19:23], raw[8:10])
	dst[23] = '-'
	hex.Encode(dst[24:36], raw[10:16])
	return string(dst)
}

// Service assembles the decision chain module on the shared connection pool.
// The zero-value is unusable; build it with New.
type Service struct {
	pool   *pgxpool.Pool
	cfg    Config
	store  Store
	client *http.Client

	// Authenticate guards every decision endpoint (session + CSRF via the
	// composition root). Nil = unguarded — tests only.
	Authenticate func(r *http.Request) error
	// ActorName resolves the operator name for the settings audit trail.
	// Nil = recorded as empty.
	ActorName func(r *http.Request) string
}

// New wires the module onto the shared pool (composition root only).
func New(cfg Config, pool *pgxpool.Pool) *Service {
	if cfg.EmbeddingModel == "" {
		cfg.EmbeddingModel = "bge-m3"
	}
	return &Service{pool: pool, cfg: cfg, store: NewPostgresStore(pool), client: &http.Client{}}
}

// ErrDisabled is returned by Record while the feature toggle is off. The
// producer's fail-open policy (F3) treats it as a no-op with a log line.
var ErrDisabled = errors.New("decisionchain: feature toggle is off")

// Record persists one producer event as a decision node. Idempotent by
// Event.IdempotencyKey: a redelivery returns the already-stored node without
// writing again. A record failure must never fail the producer's own flow
// (F3 fail-open is the caller's policy; this method only reports). Gated by
// the feature toggle: off = ErrDisabled, nothing is written (D12).
func (s *Service) Record(ctx context.Context, e Event) error {
	if !s.Enabled() {
		return ErrDisabled
	}
	w, err := s.buildNodeWrite(ctx, e, s.cfg.ResolveName)
	if err != nil {
		return err
	}
	_, err = s.store.Record(ctx, w)
	return err
}

func (s *Service) buildNodeWrite(ctx context.Context, e Event,
	resolve func(ctx context.Context, id string) (string, bool)) (nodeWrite, error) {
	return buildNodeWrite(ctx, e, resolve)
}

// buildNodeWrite normalizes and resolves one event into the store write
// shape. resolve is optional — nil keeps raw ids (the replan sink feeds
// already-normalized names).
func buildNodeWrite(ctx context.Context, e Event,
	resolve func(ctx context.Context, id string) (string, bool)) (nodeWrite, error) {
	normalized := NormalizeRequirement(e.Requirement)
	if normalized == "" {
		return nodeWrite{}, fmt.Errorf("decisionchain: requirement text is required")
	}
	if e.IdempotencyKey == "" {
		return nodeWrite{}, fmt.Errorf("decisionchain: idempotency key is required")
	}
	names := make([]string, 0, len(e.RepositoryIDs))
	for _, id := range e.RepositoryIDs {
		if id == "" {
			continue
		}
		if resolve != nil {
			if name, ok := resolve(ctx, id); ok && name != "" {
				names = append(names, name)
				continue
			}
		}
		names = append(names, id)
	}
	status := StatusConfirmed
	if !e.Accepted {
		status = StatusRejected
	}
	if e.Status.Valid() {
		status = e.Status
	}
	step := StepConfirmation
	if e.Step.Valid() {
		step = e.Step
	}
	actorType := "human"
	switch e.ActorType {
	case "llm", "service":
		actorType = e.ActorType
	case "human", "":
	default:
		return nodeWrite{}, fmt.Errorf("decisionchain: unknown actor type %q", e.ActorType)
	}
	actor := e.Actor
	if actor == "" {
		actor = "unknown"
	}
	action := e.Action
	if action == "" {
		action = "范围圈定确认"
	}
	rationale := e.Rationale
	if rationale == "" {
		rationale = truncate(normalized, 200)
	}
	contextRef := e.ContextRef
	if contextRef == nil {
		contextRef = map[string]any{"sourceIds": e.RepositoryIDs}
	}
	return nodeWrite{
		EventID:              e.IdempotencyKey,
		RequirementText:      normalized,
		RequirementKey:       RequirementKey(normalized),
		ProjectID:            e.ProjectID,
		Step:                 step,
		Status:               status,
		ActorType:            actorType,
		ActorID:              actor,
		Action:               action,
		Rationale:            rationale,
		ContextRef:           contextRef,
		AffectedRepositories: names,
		Source:               SourceEvent,
		ParentNodeID:         e.UpstreamRef,
	}, nil
}

// Enabled reports the feature toggle (feature_settings row 'decision_chain',
// missing row counts as on; read errors fail open the same way). Bounded by
// a short internal timeout so toggle reads cannot hang a request.
func (s *Service) Enabled() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	on, err := s.store.FeatureEnabled(ctx, "decision_chain")
	if err != nil {
		return true
	}
	return on
}

// RegisterRoutes mounts the 7 endpoints — see http.go (P4).

func truncate(text string, max int) string {
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return string(runes[:max]) + "…"
}
