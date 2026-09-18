package projects

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// InitializeRequestWindow is the combination callback projects calls in the
// write path to initialize a daily quota window. It is injected by the
// composition root after both packages exist, so projects never imports
// modelbudget.
type InitializeRequestWindow func(ctx context.Context, tx pgx.Tx, scopeID string, policy RequestPolicy) error

// ObserveRequestQuota is the combination callback projects calls on the read
// path. Nil means unknown, never zero.
type ObserveRequestQuota func(ctx context.Context, tx pgx.Tx, scopeID string, revision ConfigurationRevision, policy RequestPolicy, now time.Time) (QuotaObservation, error)

// SetRequestWindowInitializer wires the write-path window initializer.
// Existing New retains nil until B05 composition.
func (s *Service) SetRequestWindowInitializer(initializer InitializeRequestWindow) {
	s.requestWindowInitializer = initializer
}

// SetRequestQuotaObserver wires the read-path quota observer.
func (s *Service) SetRequestQuotaObserver(observer ObserveRequestQuota) {
	s.requestQuotaObserver = observer
}

// BudgetSummary is the JSON projection of the project's request budget state.
// Exactly one of available or unknown is present.
type BudgetSummary struct {
	available *RequestPolicy
	reasons   []string
}

func (b BudgetSummary) MarshalJSON() ([]byte, error) {
	if b.available != nil {
		return json.Marshal(struct {
			State  string        `json:"state"`
			Policy RequestPolicy `json:"policy"`
		}{State: "available", Policy: *b.available})
	}
	reasons := b.reasons
	if reasons == nil {
		reasons = []string{}
	}
	return json.Marshal(struct {
		State   string   `json:"state"`
		Reasons []string `json:"reasons"`
	}{State: "unknown", Reasons: reasons})
}

// TimeLimitSummary is the JSON projection of the project's time limit state.
type TimeLimitSummary struct {
	available *TimePolicy
	reasons   []string
}

func (t TimeLimitSummary) MarshalJSON() ([]byte, error) {
	if t.available != nil {
		return json.Marshal(struct {
			State  string     `json:"state"`
			Policy TimePolicy `json:"policy"`
		}{State: "available", Policy: *t.available})
	}
	reasons := t.reasons
	if reasons == nil {
		reasons = []string{}
	}
	return json.Marshal(struct {
		State   string   `json:"state"`
		Reasons []string `json:"reasons"`
	}{State: "unknown", Reasons: reasons})
}

// KnownQuota reports one trustworthy quota observation.
type KnownQuota struct {
	Policy         PolicyRef `json:"policy"`
	WindowStart    time.Time `json:"windowStart"`
	WindowEnd      time.Time `json:"windowEnd"`
	ObservedAt     time.Time `json:"observedAt"`
	EffectiveLimit int64     `json:"effectiveLimit"`
	Reserved       int64     `json:"reserved"`
	Consumed       int64     `json:"consumed"`
	Remaining      int64     `json:"remaining"`
}

type quotaUnknown struct {
	reasons    []string
	observedAt *time.Time
}

// QuotaObservation is the sealed quota projection returned on the read path.
// Exactly one branch marshals.
type QuotaObservation struct {
	configurationRevision ConfigurationRevision
	known                 *KnownQuota
	unknown               *quotaUnknown
	notConfigured         []string
}

// KnownQuotaObservation builds the known branch.
func KnownQuotaObservation(revision ConfigurationRevision, known KnownQuota) QuotaObservation {
	return QuotaObservation{configurationRevision: revision, known: &known}
}

// UnknownQuotaObservation builds the unknown branch.
func UnknownQuotaObservation(revision ConfigurationRevision, reasons []string, observedAt *time.Time) QuotaObservation {
	if reasons == nil {
		reasons = []string{}
	}
	return QuotaObservation{configurationRevision: revision, unknown: &quotaUnknown{reasons: reasons, observedAt: observedAt}}
}

// NotConfiguredQuotaObservation builds the not-configured branch: the project
// has no budget policy pinned at all.
func NotConfiguredQuotaObservation(revision ConfigurationRevision, reasons []string) QuotaObservation {
	if reasons == nil {
		reasons = []string{}
	}
	return QuotaObservation{configurationRevision: revision, notConfigured: reasons}
}

func (q QuotaObservation) MarshalJSON() ([]byte, error) {
	switch {
	case q.known != nil:
		return json.Marshal(struct {
			State                 string     `json:"state"`
			ConfigurationRevision string     `json:"configurationRevision"`
			Known                 KnownQuota `json:"known"`
		}{State: "known", ConfigurationRevision: string(q.configurationRevision), Known: *q.known})
	case q.unknown != nil:
		return json.Marshal(struct {
			State                 string     `json:"state"`
			ConfigurationRevision string     `json:"configurationRevision"`
			Reasons               []string   `json:"reasons"`
			ObservedAt            *time.Time `json:"observedAt,omitempty"`
		}{State: "unknown", ConfigurationRevision: string(q.configurationRevision), Reasons: q.unknown.reasons, ObservedAt: q.unknown.observedAt})
	default:
		return json.Marshal(struct {
			State                 string   `json:"state"`
			ConfigurationRevision string   `json:"configurationRevision"`
			Reasons               []string `json:"reasons"`
		}{State: "not_configured", ConfigurationRevision: string(q.configurationRevision), Reasons: q.notConfigured})
	}
}

// readConfigurationSummary projects the fixed configuration's budget and time
// policies plus a live quota observation onto the read path. Only a missing
// budget policy yields not_configured; a missing window or a failed
// observation yields unknown; connection or snapshot failures return 503.
func (s *Service) readConfigurationSummary(ctx context.Context, tx pgx.Tx, projectID string, fixed FixedConfiguration, now time.Time) (FixedSummary, QuotaObservation, error) {
	summary := FixedSummary{ConfigurationRevision: fixed.Revision()}

	recipe, hasRecipe := fixed.ExecutionRecipe()
	var budget *RequestPolicy
	var limits *TimePolicy
	if hasRecipe {
		budget = &recipe.Budget
		limits = &recipe.Limits
		summary.Budget = BudgetSummary{available: budget}
		summary.TimeLimits = TimeLimitSummary{available: limits}
		if ref, ok := fixed.Execution(); ok {
			summary.ExecutionVersionID = &ref.Version
		}
	}

	observation := QuotaObservation{}
	if budget == nil {
		observation = NotConfiguredQuotaObservation(fixed.Revision(), []string{"budget_policy_missing"})
		return summary, observation, nil
	}
	if s.requestQuotaObserver == nil {
		observation = UnknownQuotaObservation(fixed.Revision(), []string{"quota_observer_unavailable"}, nil)
		return summary, observation, nil
	}

	// The observer runs inside a savepoint: its failure downgrades to unknown
	// instead of failing the whole read.
	if _, err := tx.Exec(ctx, `SAVEPOINT quota_observation`); err != nil {
		return FixedSummary{}, QuotaObservation{}, unavailable()
	}
	observation, err := s.requestQuotaObserver(ctx, tx, projectID, fixed.Revision(), *budget, now)
	if _, rbErr := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT quota_observation`); rbErr != nil {
		return FixedSummary{}, QuotaObservation{}, unavailable()
	}
	if err != nil {
		observation = UnknownQuotaObservation(fixed.Revision(), []string{"quota_observation_failed"}, &now)
	}
	return summary, observation, nil
}

// FixedSummary is the read-path projection of one fixed configuration: pinned
// execution version, budget state and time limit state. It never opens a
// transaction, never initializes quota and never chases latest defaults.
type FixedSummary struct {
	ConfigurationRevision ConfigurationRevision `json:"configurationRevision"`
	ExecutionVersionID    *string               `json:"executionVersionId"`
	Budget                BudgetSummary         `json:"budget"`
	TimeLimits            TimeLimitSummary      `json:"timeLimits"`
}

var _ = errors.Is // keep errors import stable for savepoint error paths
