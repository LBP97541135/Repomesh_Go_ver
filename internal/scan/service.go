package scan

import (
	"context"
	"sync"
)

// Config is the scan service assembly. Every knob here maps to a decision in
// the design doc; nothing else is configurable.
type Config struct {
	// ScopeAssistEnabled is the 辅助选仓 switch: true = the suggester runs,
	// false = manual selection only. Off does NOT disable scanning — the
	// graph is still required by the completeness checker and planning.
	ScopeAssistEnabled bool
	// MaxWorkers bounds concurrent per-repository scans in an org run.
	MaxWorkers int
}

// CatalogStore persists and reads repository cards. The scan service owns
// refresh semantics (same-name refresh, failed-never-registered); the
// implementation only stores rows. #8.
type CatalogStore interface {
	Add(ctx context.Context, card RepositoryCard) error
	List(ctx context.Context) ([]RepositoryCard, error)
	Get(ctx context.Context, id string) (*RepositoryCard, error)
	// GetByName resolves a registration name to its row, if any.
	GetByName(ctx context.Context, name string) (*RepositoryCard, error)
	// UpdateAutoCard whole-replaces the scan-derived card of an existing row
	// (re-scan refresh) and bumps its profile time. Operator-owned fields
	// (description, topics, test commands/paths) must survive untouched.
	UpdateAutoCard(ctx context.Context, id string, card AutoCard, languages []string, fingerprint string) error
	// ReplaceObservedCalls whole-replaces the runtime-observed call block of
	// an existing row (mechanism 6 import). Idempotent; observed data
	// survives scan refreshes the same way operator fields do.
	ReplaceObservedCalls(ctx context.Context, id string, calls []ObservedCall) error
}

// ScopeSuggester proposes repositories for a requirement. Two implementations
// by design: the AutoCard/graph engine, and a no-op for manual-only mode.
// It only suggests; the user's submission is always the final scope. #10.
type ScopeSuggester interface {
	Suggest(requirement string, limit int) ([]Suggestion, error)
}

// Suggestion is one proposed repository with the reasons that put it there.
type Suggestion struct {
	RepositoryID string   `json:"repositoryId"`
	Name         string   `json:"name"`
	Score        float64  `json:"score"`
	MatchedTerms []string `json:"matchedTerms"`
	Rationale    string   `json:"rationale"`
}

// Service assembles the scan domain. Channels and the store register here;
// the graph, completeness checker, and suggester build on the catalog.
type Service struct {
	mu       sync.Mutex
	cfg      Config
	store    CatalogStore
	channels []EvidenceChannel
}

// New assembles the service from config and the catalog store.
func New(cfg Config, store CatalogStore) *Service {
	if cfg.MaxWorkers <= 0 {
		cfg.MaxWorkers = 5
	}
	return &Service{cfg: cfg, store: store}
}

// RegisterChannel adds an evidence channel to the scan table. Registration
// order is scan order; later registrants see deduped output of earlier ones.
func (s *Service) RegisterChannel(ch EvidenceChannel) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.channels = append(s.channels, ch)
}

// Channels returns the registered channel table in scan order.
func (s *Service) Channels() []EvidenceChannel {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]EvidenceChannel(nil), s.channels...)
}

// ScopeAssistEnabled reports whether the suggestion engine may run. Off means
// manual selection only — scanning itself stays on either way.
func (s *Service) ScopeAssistEnabled() bool { return s.cfg.ScopeAssistEnabled }

// MaxWorkers returns the effective concurrency for org scans.
func (s *Service) MaxWorkers() int { return s.cfg.MaxWorkers }
