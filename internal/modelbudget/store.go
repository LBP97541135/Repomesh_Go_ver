package modelbudget

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"repomesh.local/repomesh/internal/projects"
)

// Observe answers a read-only quota observation on the caller's tx. It never
// allocates: a read-only GET cannot create a window. Absence of a trustworthy
// initialized window yields Unknown, not zero.
func (s *Store) Observe(ctx context.Context, tx pgx.Tx, scope WindowID, policy projects.RequestPolicy) (Observation, error) {
	window, err := readWindow(ctx, tx, scope)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if window == nil {
		return Unknown{Reasons: []string{"window_not_initialized"}, At: &now}, nil
	}
	if err = checkWindowUsable(*window, now); err != nil {
		return nil, err
	}
	effective, reasons := effectiveLimit(*window, policy)
	if len(reasons) > 0 {
		return Unknown{Reasons: reasons, At: &now}, nil
	}
	return Known{
		Window:         *window,
		EffectiveLimit: effective,
		Remaining:      effective - window.Reserved,
		At:             now,
	}, nil
}

// CheckEmptyWindowHistory scans locked durable local history for the scope and
// returns evidence that the requested window day has no record. The absence of
// history is not evidence: without rows to lock and verify it refuses.
func (s *Store) CheckEmptyWindowHistory(ctx context.Context, tx pgx.Tx, scope WindowID) (EmptyWindowEvidence, error) {
	var revision string
	// Lock the latest same-scope window row; its revision is the checked
	// history revision. A missing window row for the scope is not evidence.
	err := tx.QueryRow(ctx, `SELECT revision FROM repomesh_modelbudget.windows
		WHERE scope_kind=$1 AND scope_id=$2
		ORDER BY start_utc DESC LIMIT 1 FOR UPDATE`, scope.ScopeKind, scope.ScopeID).Scan(&revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return EmptyWindowEvidence{}, failure(2, "WINDOW_HISTORY_NOT_PROVABLE")
	}
	if err != nil {
		return EmptyWindowEvidence{}, unavailable()
	}
	return EmptyWindowEvidence{scope: scope, checkedHistoryRevision: revision}, nil
}

// EnsureProjectWindow returns the initialized window for the day, or, with
// valid evidence, creates it. Evidence must match the requested scope. The
// window limit only decreases for the rest of the day (DB trigger).
func (s *Store) EnsureProjectWindow(ctx context.Context, tx pgx.Tx, scope WindowID, policy projects.RequestPolicy, evidence EmptyWindowEvidence) (Window, error) {
	if evidence.scope.ScopeKind != scope.ScopeKind || evidence.scope.ScopeID != scope.ScopeID || !evidence.scope.StartUTC.Equal(scope.StartUTC) {
		return Window{}, failure(2, "WINDOW_EVIDENCE_MISMATCH")
	}
	start, end, err := windowBounds(scope.StartUTC)
	if err != nil {
		return Window{}, err
	}
	revision, err := newRevision()
	if err != nil {
		return Window{}, err
	}
	// ON CONFLICT DO NOTHING + re-read keeps concurrent initializers linear:
	// exactly one writer inserts, everyone reads the same row afterwards.
	tag, err := tx.Exec(ctx, `INSERT INTO repomesh_modelbudget.windows
		(scope_kind, actor_scope_id, project_scope_id, start_utc, end_utc, daily_limit, revision)
		VALUES ($1,
		        CASE WHEN $1 = 'actor_model_test' THEN $2 ELSE NULL END,
		        CASE WHEN $1 = 'project_model_runtime' THEN $2 ELSE NULL END,
		        $3, $4, $5, $6)
		ON CONFLICT (scope_kind, scope_id, start_utc) DO NOTHING`,
		scope.ScopeKind, scope.ScopeID, start, end, policy.Limit, revision)
	if err != nil {
		return Window{}, unavailable()
	}
	if tag.RowsAffected() == 0 {
		// Existing window: its stored limit may already be lower than the
		// policy value; lowerWindowLimit enforces decrease-only.
		window, err := lockWindow(ctx, tx, scope)
		if err != nil {
			return Window{}, err
		}
		return lowerWindowLimit(ctx, tx, *window, policy.Limit)
	}
	window, err := lockWindow(ctx, tx, scope)
	if err != nil {
		return Window{}, err
	}
	return *window, nil
}

// ReserveTest reserves one counted request for an actor model test. The
// caller owns the whole registration transaction: it has locked the account,
// created the tests row and must consume the reservation in the same permit
// transaction. The window row must already exist; a missing window is not
// created here.
func (s *Store) ReserveTest(ctx context.Context, tx pgx.Tx, actor, testID string, policy projects.RequestPolicy, now time.Time) (Reservation, error) {
	scope := WindowID{ScopeKind: "actor_model_test", ScopeID: actor, StartUTC: now.UTC()}
	window, err := lockWindow(ctx, tx, scope)
	if err != nil {
		return Reservation{}, err
	}
	if err = checkWindowUsable(*window, now.UTC()); err != nil {
		return Reservation{}, err
	}
	effective, reasons := effectiveLimit(*window, policy)
	if len(reasons) > 0 || effective-window.Reserved < 1 {
		return Reservation{}, failure(409, "QUOTA_EXHAUSTED")
	}
	tag, err := tx.Exec(ctx, `UPDATE repomesh_modelbudget.windows
		SET reserved = reserved + 1, revision = $4
		WHERE scope_kind=$1 AND scope_id=$2 AND start_utc=$3 AND reserved < daily_limit`,
		scope.ScopeKind, scope.ScopeID, scope.StartUTC, newRevisionValue())
	if err != nil {
		return Reservation{}, unavailable()
	}
	if tag.RowsAffected() == 0 {
		return Reservation{}, failure(409, "QUOTA_EXHAUSTED")
	}
	if _, err = tx.Exec(ctx, `INSERT INTO repomesh_modelbudget.test_reservations
		(actor, test_id, scope_kind, scope_id, start_utc, budget_policy_id, budget_policy_version,
		 amount, state, reserved_at)
		VALUES ($1, $2, 'actor_model_test', $1, $3, $4, $5, 1, 'reserved', $6)`,
		actor, testID, scope.StartUTC, policy.Ref.ID, policy.Ref.Version, now.UTC()); err != nil {
		return Reservation{}, unavailable()
	}
	return Reservation{ActorID: actor, TestID: testID, Window: scope, Policy: policy.Ref}, nil
}

// ConsumeTest moves one reserved unit to consumed and settles the window
// counter in the same transaction. The caller invokes it inside the send-permit
// transaction once externalOperationID is committed; consumed is final.
func (s *Store) ConsumeTest(ctx context.Context, tx pgx.Tx, reservation Reservation, externalOperationID string) error {
	if externalOperationID == "" {
		return failure(2, "EXTERNAL_OPERATION_REQUIRED")
	}
	tag, err := tx.Exec(ctx, `UPDATE repomesh_modelbudget.test_reservations
		SET state='consumed', settled_at=now()
		WHERE actor=$1 AND test_id=$2 AND state='reserved'`,
		reservation.ActorID, reservation.TestID)
	if err != nil {
		return unavailable()
	}
	if tag.RowsAffected() == 0 {
		return failure(409, "RESERVATION_NOT_OUTSTANDING")
	}
	if _, err = tx.Exec(ctx, `UPDATE repomesh_modelbudget.windows
		SET reserved = reserved - 1, consumed = consumed + 1, revision = $4
		WHERE scope_kind=$1 AND scope_id=$2 AND start_utc=$3`,
		reservation.Window.ScopeKind, reservation.Window.ScopeID, reservation.Window.StartUTC, newRevisionValue()); err != nil {
		return unavailable()
	}
	return nil
}

// ReleaseUnsent returns one reserved unit to the window. proof must come from
// ProveUnsent in the same transaction; timeout alone never releases.
func (s *Store) ReleaseUnsent(ctx context.Context, tx pgx.Tx, reservation Reservation, proof NotSentProof) error {
	if proof.actorID != reservation.ActorID || proof.testID != reservation.TestID {
		return failure(2, "RELEASE_PROOF_MISMATCH")
	}
	tag, err := tx.Exec(ctx, `UPDATE repomesh_modelbudget.test_reservations
		SET state='released', settled_at=now()
		WHERE actor=$1 AND test_id=$2 AND state='reserved'`,
		reservation.ActorID, reservation.TestID)
	if err != nil {
		return unavailable()
	}
	if tag.RowsAffected() == 0 {
		return failure(409, "RESERVATION_NOT_OUTSTANDING")
	}
	if _, err = tx.Exec(ctx, `UPDATE repomesh_modelbudget.windows
		SET reserved = reserved - 1, revision = $4
		WHERE scope_kind=$1 AND scope_id=$2 AND start_utc=$3`,
		reservation.Window.ScopeKind, reservation.Window.ScopeID, reservation.Window.StartUTC, newRevisionValue()); err != nil {
		return unavailable()
	}
	return nil
}

// ProveUnsent verifies inside the caller's tx that the test's dispatch row
// still exists with a usable, unconsumed, unrevoked send permit and no
// may-have-sent fact. It returns the private proof; only the caller holding
// both this proof and the reservation in one tx can release.
func (s *Store) ProveUnsent(ctx context.Context, tx pgx.Tx, actorID, testID string) (NotSentProof, error) {
	var permitID, revision string
	var mayHaveSent *time.Time
	var revokedAt *time.Time
	err := tx.QueryRow(ctx, `SELECT d.send_permit_id, d.may_have_sent_at, d.capability_revoked_at
		FROM repomesh_models.test_dispatch d
		JOIN repomesh_models.tests t ON t.actor=d.actor AND t.test_id=d.test_id
		WHERE d.actor=$1 AND d.test_id=$2
		FOR UPDATE OF d`, actorID, testID).Scan(&permitID, &mayHaveSent, &revokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return NotSentProof{}, failure(404, "DISPATCH_NOT_FOUND")
	}
	if err != nil {
		return NotSentProof{}, unavailable()
	}
	if mayHaveSent != nil || revokedAt != nil {
		return NotSentProof{}, failure(409, "SEND_ALREADY_AUTHORIZED")
	}
	// The permit revision recorded here binds the proof to the exact permit
	// generation the caller observed; ReleaseUnsent rechecks inside its tx.
	revision = permitID
	return NotSentProof{actorID: actorID, testID: testID, permitRevision: revision, evidenceID: newRevisionValue()}, nil
}

// lockWindow locks and reads one window row. scope_id is a GENERATED column;
// inserts never list it.
func lockWindow(ctx context.Context, tx pgx.Tx, scope WindowID) (*Window, error) {
	window, err := readWindowForUpdate(ctx, tx, scope)
	if err != nil {
		return nil, err
	}
	if window == nil {
		return nil, failure(2, "WINDOW_NOT_INITIALIZED")
	}
	return window, nil
}

// lowerWindowLimit applies a possibly lower policy limit to an existing window.
// The DB trigger refuses any increase.
func lowerWindowLimit(ctx context.Context, tx pgx.Tx, window Window, currentLimit int64) (Window, error) {
	if currentLimit >= window.Limit {
		return window, nil
	}
	if currentLimit < window.Reserved {
		return Window{}, failure(2, "WINDOW_LIMIT_BELOW_RESERVATIONS")
	}
	tag, err := tx.Exec(ctx, `UPDATE repomesh_modelbudget.windows SET daily_limit=$3, revision=$4
		WHERE scope_kind=$1 AND scope_id=$2 AND start_utc=$5 AND daily_limit > $3`,
		window.ID.ScopeKind, window.ID.ScopeID, currentLimit, newRevisionValue(), window.ID.StartUTC)
	if err != nil {
		return Window{}, unavailable()
	}
	if tag.RowsAffected() == 0 {
		// Another writer already lowered it between lock and update.
		fresh, err := readWindowForUpdate(ctx, tx, window.ID)
		if err != nil {
			return Window{}, err
		}
		if fresh == nil {
			return Window{}, failure(2, "WINDOW_NOT_INITIALIZED")
		}
		return *fresh, nil
	}
	window.Limit = currentLimit
	return window, nil
}

func readWindow(ctx context.Context, tx pgx.Tx, scope WindowID) (*Window, error) {
	return scanWindow(tx.QueryRow(ctx, `SELECT scope_kind, scope_id, start_utc, end_utc, daily_limit, reserved, consumed, revision
		FROM repomesh_modelbudget.windows WHERE scope_kind=$1 AND scope_id=$2 AND start_utc=$3`,
		scope.ScopeKind, scope.ScopeID, scope.StartUTC))
}

func readWindowForUpdate(ctx context.Context, tx pgx.Tx, scope WindowID) (*Window, error) {
	return scanWindow(tx.QueryRow(ctx, `SELECT scope_kind, scope_id, start_utc, end_utc, daily_limit, reserved, consumed, revision
		FROM repomesh_modelbudget.windows WHERE scope_kind=$1 AND scope_id=$2 AND start_utc=$3 FOR UPDATE`,
		scope.ScopeKind, scope.ScopeID, scope.StartUTC))
}

func scanWindow(row pgx.Row) (*Window, error) {
	window := &Window{}
	var start, end time.Time
	err := row.Scan(&window.ID.ScopeKind, &window.ID.ScopeID, &start, &end,
		&window.Limit, &window.Reserved, &window.Consumed, &window.Revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, unavailable()
	}
	window.ID.StartUTC = start
	window.EndUTC = end
	return window, nil
}

// checkWindowUsable refuses observations outside the requested day window.
func checkWindowUsable(window Window, now time.Time) error {
	if now.Before(window.ID.StartUTC) || !now.Before(window.EndUTC) {
		return failure(2, "WINDOW_EXPIRED")
	}
	return nil
}

func effectiveLimit(window Window, policy projects.RequestPolicy) (int64, []string) {
	if !policy.Enabled {
		return 0, []string{"policy_disabled"}
	}
	if policy.Limit <= 0 {
		return 0, []string{"policy_limit_invalid"}
	}
	if policy.Limit < window.Limit {
		// The window can only be more restrictive than the policy.
		return window.Limit, nil
	}
	return policy.Limit, nil
}

// windowBounds normalizes any instant to its UTC day start and end.
func windowBounds(at time.Time) (time.Time, time.Time, error) {
	utc := at.UTC()
	start := time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
	if start.After(utc) {
		return time.Time{}, time.Time{}, failure(2, "WINDOW_BOUNDS_INVALID")
	}
	return start, start.Add(24 * time.Hour), nil
}

func newRevisionValue() string {
	value, err := newRevision()
	if err != nil {
		return ""
	}
	return value
}
