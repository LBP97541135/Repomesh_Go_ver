// Package execution implements the G3/G4 controlled-execution ledger:
// atomic worker + capacity reservation (ADR-0017), launch re-verification,
// stop with verified write-capability revocation, and host resource
// lifecycle ownership. The coordinator writes formal state; the host
// executor records observations and provisions/revokes resources.
package execution

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Service is the reservation and lifecycle facade used by the coordinator.
type Service struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

func newID(prefix string) (string, error) {
	buffer := make([]byte, 10)
	if _, err := rand.Read(buffer); err != nil {
		return "", unavailable()
	}
	return prefix + hex.EncodeToString(buffer), nil
}

// ReservationCommand requests one execution attempt for an issue round.
type ReservationCommand struct {
	ProjectID             string
	IssueID               string
	ConfigurationRevision string
	WorkerID              string
}

// Reservation records the atomically committed reservation.
type Reservation struct {
	AttemptID string
	WorkerID  string
}

// Reserve runs the ADR-0017 short transaction: register the attempt, occupy
// the worker, verify the configuration matches the issue pinned revision.
// Competing requests for the same worker cannot both succeed; a loser waits
// with the real reason rather than a fabricated queue position.
func (s *Service) Reserve(ctx context.Context, command ReservationCommand) (Reservation, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return Reservation{}, unavailable()
	}
	defer rollback(tx)
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout='2s'`); err != nil {
		return Reservation{}, unavailable()
	}
	if _, err := tx.Exec(ctx, `SET LOCAL statement_timeout='5s'`); err != nil {
		return Reservation{}, unavailable()
	}
	attemptID, err := newID("att_")
	if err != nil {
		return Reservation{}, err
	}
	tag, err := tx.Exec(ctx, `UPDATE repomesh_execution.workers
		SET active_attempts = active_attempts + 1
		WHERE id=$1 AND retired_at IS NULL AND active_attempts < concurrency_limit`, command.WorkerID)
	if err != nil {
		return Reservation{}, unavailable()
	}
	if tag.RowsAffected() != 1 {
		return Reservation{}, failure(409, "WORKER_UNAVAILABLE")
	}
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_execution.attempts
		(id, project_id, issue_id, worker_id, configuration_revision, state)
		VALUES ($1,$2,$3,$4,$5,'reserved')`,
		attemptID, command.ProjectID, command.IssueID, command.WorkerID, command.ConfigurationRevision); err != nil {
		return Reservation{}, unavailable()
	}
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_execution.attempt_events
		(attempt_id, sequence, kind, source, payload) VALUES ($1,1,'reserved','coordinator',NULL)`,
		attemptID); err != nil {
		return Reservation{}, unavailable()
	}
	if err := tx.Commit(ctx); err != nil {
		return Reservation{}, unavailable()
	}
	return Reservation{AttemptID: attemptID, WorkerID: command.WorkerID}, nil
}

// VerifyLaunch re-checks the reservation immediately before the executor
// starts the environment: occupancy must still belong to this attempt and the
// pinned configuration must still match (preparation-time revocation guard,
// ADR-0017 "启动前再次核验").
func (s *Service) VerifyLaunch(ctx context.Context, attemptID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return unavailable()
	}
	defer rollback(tx)
	var workerID string
	var state string
	err = tx.QueryRow(ctx, `SELECT worker_id, state FROM repomesh_execution.attempts
		WHERE id=$1 FOR UPDATE`, attemptID).Scan(&workerID, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return failure(404, "RESOURCE_NOT_FOUND")
	}
	if err != nil {
		return unavailable()
	}
	if state != "preparing" && state != "reserved" {
		return failure(409, "LAUNCH_CONTEXT_CHANGED")
	}
	var alive bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM repomesh_execution.workers
		WHERE id=$1 AND retired_at IS NULL)`, workerID).Scan(&alive); err != nil {
		return unavailable()
	}
	if !alive {
		return failure(409, "WORKER_RETIRED")
	}
	if _, err := tx.Exec(ctx, `UPDATE repomesh_execution.attempts SET state='launch_verified', launch_verified_at=clock_timestamp()
		WHERE id=$1`, attemptID); err != nil {
		return unavailable()
	}
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_execution.attempt_events (attempt_id, sequence, kind, source)
		SELECT $1, COALESCE(MAX(sequence),0)+1, 'launch_verified', 'coordinator' FROM repomesh_execution.attempt_events WHERE attempt_id=$1`,
		attemptID); err != nil {
		return unavailable()
	}
	if err := tx.Commit(ctx); err != nil {
		return unavailable()
	}
	return nil
}

// RequestStop asks for a verified stop: the executor must revoke write
// capability and release resources before the attempt may transition to
// stopped (the attempt_stop_requires_cleanup trigger enforces this in SQL).
func (s *Service) RequestStop(ctx context.Context, attemptID string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE repomesh_execution.attempts SET state='stop_requested'
		WHERE id=$1 AND state IN ('launch_verified','running')`, attemptID)
	if err != nil {
		return unavailable()
	}
	if tag.RowsAffected() != 1 {
		return failure(409, "STOP_CONTEXT_CHANGED")
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO repomesh_execution.attempt_events (attempt_id, sequence, kind, source)
		SELECT $1, COALESCE(MAX(sequence),0)+1, 'stop_requested', 'coordinator' FROM repomesh_execution.attempt_events WHERE attempt_id=$1`,
		attemptID)
	if err != nil {
		return unavailable()
	}
	return nil
}

// ConfirmStopped records the executor's verified cleanup: every resource must
// already be released with write revoked, otherwise the confirmation is
// rejected and the attempt stays stop_requested with the real reason.
func (s *Service) ConfirmStopped(ctx context.Context, attemptID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return unavailable()
	}
	defer rollback(tx)
	var pending int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM repomesh_execution.host_resources
		WHERE attempt_id=$1 AND (write_enabled = true OR state = 'provisioned')`, attemptID).Scan(&pending); err != nil {
		return unavailable()
	}
	if pending > 0 {
		return failure(409, "CLEANUP_INCOMPLETE")
	}
	tag, err := tx.Exec(ctx, `UPDATE repomesh_execution.attempts SET state='stopped', stopped_at=clock_timestamp()
		WHERE id=$1 AND state='stop_requested'`, attemptID)
	if err != nil {
		return unavailable()
	}
	if tag.RowsAffected() != 1 {
		return failure(409, "STOP_CONTEXT_CHANGED")
	}
	if _, err := tx.Exec(ctx, `UPDATE repomesh_execution.workers SET active_attempts = GREATEST(active_attempts-1, 0)
		WHERE id = (SELECT worker_id FROM repomesh_execution.attempts WHERE id=$1)`, attemptID); err != nil {
		return unavailable()
	}
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_execution.attempt_events (attempt_id, sequence, kind, source)
		SELECT $1, COALESCE(MAX(sequence),0)+1, 'stopped', 'coordinator' FROM repomesh_execution.attempt_events WHERE attempt_id=$1`,
		attemptID); err != nil {
		return unavailable()
	}
	if err := tx.Commit(ctx); err != nil {
		return unavailable()
	}
	return nil
}

// RevokeWrite flips one resource's write capability off; this is the only
// revocation entry and it is recorded as evidence.
func (s *Service) RevokeWrite(ctx context.Context, resourceID string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE repomesh_execution.host_resources SET write_enabled=false
		WHERE id=$1 AND state='provisioned'`, resourceID)
	if err != nil {
		return unavailable()
	}
	if tag.RowsAffected() != 1 {
		return failure(404, "RESOURCE_NOT_FOUND")
	}
	return nil
}

// ReleaseResource marks one resource released after the executor confirmed
// teardown. Unknown state keeps orphan_suspected rather than a silent release.
func (s *Service) ReleaseResource(ctx context.Context, resourceID string, orphanSuspected bool) error {
	state := "released"
	if orphanSuspected {
		state = "orphan_suspected"
	}
	tag, err := s.pool.Exec(ctx, `UPDATE repomesh_execution.host_resources
		SET state=$2, released_at=clock_timestamp() WHERE id=$1 AND state='provisioned'`, resourceID, state)
	if err != nil {
		return unavailable()
	}
	if tag.RowsAffected() != 1 {
		return failure(409, "RESOURCE_CONTEXT_CHANGED")
	}
	return nil
}

// RecordObservation appends one executor observation without touching formal
// state transitions (G3: executor evidence, coordinator authority).
func (s *Service) RecordObservation(ctx context.Context, attemptID, kind string, payload []byte) error {
	if kind != "environment_prepared" && kind != "started" && kind != "observed_unknown" {
		return failure(422, "VALIDATION_FAILED")
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO repomesh_execution.attempt_events (attempt_id, sequence, kind, source, payload)
		SELECT $1, COALESCE(MAX(sequence),0)+1, $2, 'host_executor', $3::jsonb FROM repomesh_execution.attempt_events WHERE attempt_id=$1`,
		attemptID, kind, payload)
	if err != nil {
		return unavailable()
	}
	return nil
}

// ProvisionResource registers one new host resource under the executor's
// lifecycle ownership with write capability initially enabled.
func (s *Service) ProvisionResource(ctx context.Context, attemptID, kind, externalRef string) (string, error) {
	if kind != "container" && kind != "directory" && kind != "volume" && kind != "network" {
		return "", failure(422, "VALIDATION_FAILED")
	}
	resourceID, err := newID("res_")
	if err != nil {
		return "", err
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO repomesh_execution.host_resources
		(id, attempt_id, kind, external_ref, lifecycle_owner, state, write_enabled)
		VALUES ($1,$2,$3,$4,'host_executor','provisioned',true)`,
		resourceID, attemptID, kind, externalRef); err != nil {
		return "", unavailable()
	}
	return resourceID, nil
}
