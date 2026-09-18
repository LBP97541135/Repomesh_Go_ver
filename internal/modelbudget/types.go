// Package modelbudget is a narrow counted-request accounting module shared by
// real consumers. It owns only windows, reservations and conservative
// consumption: no money settlement, no scheduling, no network. It never owns a
// pool and never begins, commits or rolls back a transaction — every function
// runs on the caller's tx.
package modelbudget

import (
	"time"

	"repomesh.local/repomesh/internal/projects"
)

// Store is an empty struct on purpose: all state lives in PostgreSQL and all
// work happens on caller-supplied transactions.
type Store struct{}

// New returns the shared accounting store.
func New() *Store { return &Store{} }

// WindowID identifies one accounting window: an actor model-test scope or a
// project model-runtime scope for one UTC day.
type WindowID struct {
	ScopeKind string
	ScopeID   string
	StartUTC  time.Time
}

// Window is one initialized daily window row.
type Window struct {
	ID       WindowID
	EndUTC   time.Time
	Limit    int64
	Reserved int64
	Consumed int64
	Revision string
}

// Reservation is one outstanding test reservation.
type Reservation struct {
	ActorID string
	TestID  string
	Window  WindowID
	Policy  projects.PolicyRef
}

// Observation is the sealed sum returned by Observe. Only Known and Unknown
// implement it.
type Observation interface{ budgetObservation() }

// Known reports a trustworthy initialized window.
type Known struct {
	Window         Window
	EffectiveLimit int64
	Remaining      int64
	At             time.Time
}

func (Known) budgetObservation() {}

// Unknown reports that no trustworthy quota observation is possible. A nil At
// means the failure carries no timestamp evidence.
type Unknown struct {
	Reasons []string
	At      *time.Time
}

func (Unknown) budgetObservation() {}

// EmptyWindowEvidence is produced only by CheckEmptyWindowHistory from locked
// durable local history. Its fields are private so callers cannot forge it.
type EmptyWindowEvidence struct {
	scope                  WindowID
	checkedHistoryRevision string
}

// NotSentProof is produced only by ProveUnsent inside the modelbudget package.
// ReleaseUnsent refuses reservations without one; timeout alone never suffices.
type NotSentProof struct {
	actorID        string
	testID         string
	permitRevision string
	evidenceID     string
}
