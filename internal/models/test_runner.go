package models

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"repomesh.local/repomesh/internal/modelbudget"
	"repomesh.local/repomesh/internal/projects"
	"repomesh.local/repomesh/internal/secrets"
)

// TestRunner claims queued tests, authorizes one send per claim, dispatches
// through the single-request transport, and records the observation. Handler
// registration and heartbeat are independent short transactions; a RunOne
// iteration never carries their locks.
type TestRunner struct {
	service    *TestService
	transport  *SingleRequestTransport
	instanceID string
}

func NewTestRunner(service *TestService, transport *SingleRequestTransport) *TestRunner {
	return &TestRunner{service: service, transport: transport}
}

type handlerObservation struct {
	instanceID      string
	protocolVersion string
	leaseUntil      time.Time
}

// RegisterHandler claims a fresh lease row for this instance. Conflict on the
// instance id means another handler exists; the caller surfaces it.
func (r *TestRunner) RegisterHandler(ctx context.Context, protocolVersion string) error {
	instanceID := newID()
	tx, err := r.service.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return unavailable()
	}
	defer rollback(tx)
	now := time.Now().UTC()
	_, err = tx.Exec(ctx, `INSERT INTO repomesh_models.test_handler_leases
		(instance_id,protocol_version,last_seen,lease_until)
		VALUES ($1,$2,$3,$4)`,
		instanceID, protocolVersion, now, now.Add(30*time.Second))
	if err != nil {
		return unavailable()
	}
	if err = tx.Commit(ctx); err != nil {
		return unavailable()
	}
	r.instanceID = instanceID
	return nil
}

// HeartbeatHandler extends this instance's lease. An expired or retired lease
// is renewed here; expiry itself is never revocation proof.
func (r *TestRunner) HeartbeatHandler(ctx context.Context) error {
	tx, err := r.service.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return unavailable()
	}
	defer rollback(tx)
	now := time.Now().UTC()
	tag, err := tx.Exec(ctx, `UPDATE repomesh_models.test_handler_leases
		SET last_seen=$2, lease_until=$3 WHERE instance_id=$1 AND retired_at IS NULL`,
		r.instanceID, now, now.Add(30*time.Second))
	if err != nil {
		return unavailable()
	}
	if tag.RowsAffected() == 0 {
		return failure(409, "TEST_HANDLER_LEASE_LOST")
	}
	if err = tx.Commit(ctx); err != nil {
		return unavailable()
	}
	return nil
}

// readTestHandler projects the most recent matching handler lease. Absence of
// a matching lease means no handler can serve the protocol; a query failure
// is unconfirmed, never "absent".
func readTestHandler(ctx context.Context, tx pgx.Tx, protocolVersion string, now time.Time) (handlerObservation, error) {
	var obs handlerObservation
	err := tx.QueryRow(ctx, `SELECT instance_id,protocol_version,lease_until
		FROM repomesh_models.test_handler_leases
		WHERE protocol_version=$1 AND retired_at IS NULL AND lease_until > $2
		ORDER BY last_seen DESC LIMIT 1`, protocolVersion, now).Scan(
		&obs.instanceID, &obs.protocolVersion, &obs.leaseUntil)
	if errorsIsNoRows(err) {
		return obs, failure(409, "TEST_HANDLER_UNAVAILABLE")
	}
	if err != nil {
		return obs, failure(503, "TEST_HANDLER_UNCONFIRMED")
	}
	return obs, nil
}

// checkTestProtocol verifies that a live handler supports the exact protocol
// version the preview was built with. Used by Preview paths.
func (s *TestService) checkTestProtocol(tx pgx.Tx, protocolVersion string) error {
	obs, err := readTestHandler(context.Background(), tx, protocolVersion, time.Now().UTC())
	if err != nil {
		return err
	}
	_ = obs
	return nil
}

// claimUnsent locks one queued test and takes a short dispatch lease on it.
// The claim fails cleanly when another instance holds the lease.
func (r *TestRunner) claimUnsent(ctx context.Context) (TestClaim, bool, error) {
	tx, err := r.service.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return TestClaim{}, false, unavailable()
	}
	defer rollback(tx)
	var claim TestClaim
	now := time.Now().UTC()
	err = tx.QueryRow(ctx, `SELECT t.actor,t.test_id,d.generation
		FROM repomesh_models.tests t
		JOIN repomesh_models.test_dispatch d ON d.actor=t.actor AND d.test_id=t.test_id
		WHERE t.state='queued' AND t.removed_at IS NULL AND d.may_have_sent_at IS NULL
			AND d.capability_revoked_at IS NULL
			AND (d.lease_until IS NULL OR d.lease_until < $1)
		ORDER BY t.accepted_at LIMIT 1 FOR UPDATE OF t`, now).Scan(
		&claim.owner, &claim.testID, &claim.generation)
	if errorsIsNoRows(err) {
		return TestClaim{}, false, nil
	}
	if err != nil {
		return TestClaim{}, false, unavailable()
	}
	leaseUntil := now.Add(60 * time.Second)
	if _, err = tx.Exec(ctx, `UPDATE repomesh_models.test_dispatch
		SET lease_until=$3, sender_instance_id=$4
		WHERE actor=$1 AND test_id=$2 AND (lease_until IS NULL OR lease_until < $5)`,
		claim.owner, claim.testID, leaseUntil, r.instanceID, now); err != nil {
		return TestClaim{}, false, unavailable()
	}
	if _, err = tx.Exec(ctx, `UPDATE repomesh_models.tests SET state='running' WHERE actor=$1 AND test_id=$2 AND state='queued'`,
		claim.owner, claim.testID); err != nil {
		return TestClaim{}, false, unavailable()
	}
	if err = tx.Commit(ctx); err != nil {
		return TestClaim{}, false, unavailable()
	}
	claim.leaseUntil = leaseUntil
	return claim, true, nil
}

// AuthorizedDispatch carries the open key and the exact stored policy. It is
// created only after the permit tx commits; any later failure clears it.
type AuthorizedDispatch struct {
	permit    SendPermit
	model     ProviderView
	rowID     string
	policy    TestPolicy
	key       []byte
	maxOutput int64
}

func (d *AuthorizedDispatch) clear() {
	for i := range d.key {
		d.key[i] = 0
	}
	d.key = nil
}

// loadDispatchInTx re-reads the dispatch row under the claim's lease and
// proves the secret is openable before the permit tx commits anything.
func (r *TestRunner) loadDispatchInTx(ctx context.Context, tx pgx.Tx, claim TestClaim) (*AuthorizedDispatch, error) {
	now := time.Now().UTC()
	var dispatch AuthorizedDispatch
	var credential CredentialCapabilityRef
	var leaseUntil *time.Time
	var sender *string
	err := tx.QueryRow(ctx, `SELECT external_operation_id,send_permit_id,generation,lease_until,sender_instance_id,
			credential_capability_id,credential_capability_version
		FROM repomesh_models.test_dispatch
		WHERE actor=$1 AND test_id=$2 AND may_have_sent_at IS NULL AND capability_revoked_at IS NULL
		FOR UPDATE`, claim.owner, claim.testID).Scan(
		&dispatch.permit.externalOperationID, &dispatch.permit.permitID, &claim.generation, &leaseUntil, &sender,
		&credential.ID, &credential.Version)
	if errorsIsNoRows(err) {
		return nil, failure(404, "DISPATCH_NOT_FOUND")
	}
	if err != nil {
		return nil, unavailable()
	}
	if err = checkDispatchLease(leaseUntil, sender, r.instanceID, now); err != nil {
		return nil, err
	}
	dispatch.permit.actorID = claim.owner
	dispatch.permit.testID = claim.testID
	dispatch.permit.credential = credential
	return &dispatch, nil
}

// checkDispatchLease refuses to act on a lease this instance no longer holds.
// An expired lease is a stale-claim condition, never revocation evidence.
func checkDispatchLease(leaseUntil *time.Time, sender *string, instanceID string, now time.Time) error {
	if sender == nil || leaseUntil == nil {
		return failure(409, "DISPATCH_LEASE_LOST")
	}
	if now.After(*leaseUntil) {
		return failure(409, "DISPATCH_LEASE_LOST")
	}
	if *sender != instanceID {
		return failure(409, "DISPATCH_LEASE_HELD_ELSEWHERE")
	}
	return nil
}

// authorizeOneSend runs the permit transaction: it re-verifies the dispatch is
// still unsent and unrevoked, re-reads the exact stored policy, opens the
// provider key, stamps may_have_sent_at and consumes the budget reservation.
// Only after commit does it return a dispatch carrying the open key.
func (r *TestRunner) authorizeOneSend(ctx context.Context, claim TestClaim) (*AuthorizedDispatch, error) {
	tx, err := r.service.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, unavailable()
	}
	defer rollback(tx)
	dispatch, err := r.loadDispatchInTx(ctx, tx, claim)
	if err != nil {
		return nil, err
	}
	model, rowID, policy, maxOutput, err := r.readDispatchContext(ctx, tx, claim, dispatch)
	if err != nil {
		return nil, err
	}
	key, err := r.service.secrets.Open(ctx, secrets.VersionID(dispatch.permit.credential.Version),
		secrets.Owner{Kind: "model-provider", ID: dispatch.permit.credential.ID}, secrets.ModelProviderKey)
	if err != nil {
		return nil, failure(409, "PROVIDER_KEY_UNAVAILABLE")
	}
	if _, err = tx.Exec(ctx, `UPDATE repomesh_models.test_dispatch SET may_have_sent_at=$3
		WHERE actor=$1 AND test_id=$2 AND may_have_sent_at IS NULL AND capability_revoked_at IS NULL`,
		claim.owner, claim.testID, time.Now().UTC()); err != nil {
		clear(key)
		return nil, unavailable()
	}
	reservation := modelbudget.Reservation{ActorID: claim.owner, TestID: claim.testID}
	if err = r.service.budgets.ConsumeTest(ctx, tx, reservation, dispatch.permit.externalOperationID); err != nil {
		clear(key)
		return nil, convertBudgetFailure(err)
	}
	if err = tx.Commit(ctx); err != nil {
		clear(key)
		return nil, unavailable()
	}
	dispatch.permit.nonce = newID()
	dispatch.model = model
	dispatch.rowID = rowID
	dispatch.policy = policy
	dispatch.key = key
	dispatch.maxOutput = maxOutput
	return dispatch, nil
}

// readDispatchContext re-reads the exact stored test inputs: the model row,
// the policy combination, and the secret version. Nothing is re-resolved from
// current state; the registered snapshot travels unchanged.
func (r *TestRunner) readDispatchContext(ctx context.Context, tx pgx.Tx, claim TestClaim, dispatch *AuthorizedDispatch) (ProviderView, string, TestPolicy, int64, error) {
	var view ProviderView
	var rowID string
	var maxOutput int64
	var budgetRef, timeRef projects.PolicyRef
	var budgetScope string
	var budgetLimit, budgetMaxUnresolved int64
	var budgetEnabled bool
	var timeSeconds, workerSeconds int64
	var egressID, egressVersion string
	var approvedURLs []string
	var allowPrivate, followRedirects bool
	var secretVersion string
	err := tx.QueryRow(ctx, `SELECT t.provider_id,t.provider_revision,t.model_row_id,t.max_output_tokens,
			t.budget_policy_id,t.budget_policy_version,p.scope,p.daily_limit,p.max_unresolved,p.enabled,
			t.time_limit_policy_id,t.time_limit_policy_version,tv.model_request_seconds,tv.worker_attempt_seconds,
			t.egress_policy_id,t.egress_policy_version,e.approved_base_urls,e.allow_private_addresses,e.follow_redirects,
			r.secret_version_id
		FROM repomesh_models.tests t
		JOIN repomesh_projects.request_policy_versions p ON p.id=t.budget_policy_id AND p.version=t.budget_policy_version
		JOIN repomesh_projects.time_policy_versions tv ON tv.id=t.time_limit_policy_id AND tv.version=t.time_limit_policy_version
		JOIN repomesh_sources.egress_policy_versions e ON e.id=t.egress_policy_id AND e.version=t.egress_policy_version
		JOIN repomesh_models.providers pr ON pr.id=t.provider_id
		JOIN repomesh_models.provider_revisions r ON r.provider_id=pr.id AND r.revision=t.provider_revision
		WHERE t.actor=$1 AND t.test_id=$2`, claim.owner, claim.testID).Scan(
		&view.ID, &view.Revision, &rowID, &maxOutput,
		&budgetRef.ID, &budgetRef.Version, &budgetScope, &budgetLimit, &budgetMaxUnresolved, &budgetEnabled,
		&timeRef.ID, &timeRef.Version, &timeSeconds, &workerSeconds,
		&egressID, &egressVersion, &approvedURLs, &allowPrivate, &followRedirects,
		&secretVersion)
	if errorsIsNoRows(err) {
		return ProviderView{}, "", TestPolicy{}, 0, failure(404, "DISPATCH_NOT_FOUND")
	}
	if err != nil {
		return ProviderView{}, "", TestPolicy{}, 0, unavailable()
	}
	if secretVersion != dispatch.permit.credential.Version {
		return ProviderView{}, "", TestPolicy{}, 0, failure(409, "CREDENTIAL_SUPERSEDED")
	}
	var name, baseURL, apiFormat string
	if err = tx.QueryRow(ctx, `SELECT r.name,r.base_url,r.api_format FROM repomesh_models.provider_revisions r
		WHERE r.provider_id=$1 AND r.revision=$2`, view.ID, view.Revision).Scan(&name, &baseURL, &apiFormat); err != nil {
		return ProviderView{}, "", TestPolicy{}, 0, unavailable()
	}
	view.Name, view.BaseURL, view.APIFormat = name, baseURL, apiFormat
	var modelID, profileID, displayName string
	var contextWindow int64
	var reasoning, vision bool
	if err = tx.QueryRow(ctx, `SELECT s.model_id,m.profile_id,s.display_name,s.context_window,s.max_output_tokens,s.reasoning,s.vision
		FROM repomesh_models.model_snapshots s
		JOIN repomesh_models.model_rows m ON m.provider_id=s.provider_id AND m.id=s.row_id
		WHERE s.provider_id=$1 AND s.provider_revision=$2 AND s.row_id=$3`,
		view.ID, view.Revision, rowID).Scan(&modelID, &profileID, &displayName, &contextWindow, &maxOutput, &reasoning, &vision); err != nil {
		return ProviderView{}, "", TestPolicy{}, 0, unavailable()
	}
	view.Models = []ModelView{{ID: modelID, ModelProfileID: profileID, ModelID: modelID, DisplayName: displayName, ContextWindow: contextWindow, MaxOutputTokens: maxOutput, Reasoning: reasoning, Vision: vision}}
	policy := TestPolicy{
		Revision:         budgetRef.ID + ":" + budgetRef.Version,
		Budget:           projects.RequestPolicy{Ref: budgetRef, Scope: budgetScope, Limit: budgetLimit, MaxUnresolved: budgetMaxUnresolved, Enabled: budgetEnabled},
		Limits:           projects.TimePolicy{Ref: timeRef, ModelRequestSeconds: timeSeconds, WorkerAttemptSeconds: workerSeconds},
		EgressID:         egressID,
		EgressVersion:    egressVersion,
		ApprovedBaseURLs: approvedURLs,
		AllowPrivateAddr: allowPrivate,
		FollowRedirects:  followRedirects,
	}
	return view, rowID, policy, maxOutput, nil
}

// checkSendStillAuthorized re-reads the dispatch facts inside any tx that is
// about to touch the send lifecycle. may_have_sent or revocation at this point
// means the send authorization window has closed for good.
func (r *TestRunner) checkSendStillAuthorized(ctx context.Context, dispatch *AuthorizedDispatch) error {
	tx, err := r.service.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return unavailable()
	}
	defer rollback(tx)
	var mayHaveSent, revokedAt *time.Time
	err = tx.QueryRow(ctx, `SELECT may_have_sent_at,capability_revoked_at FROM repomesh_models.test_dispatch
		WHERE actor=$1 AND test_id=$2`, dispatch.permit.actorID, dispatch.permit.testID).Scan(&mayHaveSent, &revokedAt)
	if errorsIsNoRows(err) {
		return failure(404, "DISPATCH_NOT_FOUND")
	}
	if err != nil {
		return unavailable()
	}
	if mayHaveSent != nil || revokedAt != nil {
		return failure(409, "SEND_ALREADY_AUTHORIZED")
	}
	return nil
}

// recordObservation persists the send outcome and advances the test state in
// one transaction: passed/failed terminal, unknown opens the outstanding
// pointer for maintenance. It is idempotent per evidence id.
func (r *TestRunner) recordObservation(ctx context.Context, observation DispatchObservation) error {
	tx, err := r.service.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return unavailable()
	}
	defer rollback(tx)
	now := time.Now().UTC()
	var inputTokens, outputTokens *int64
	if observation.Usage != nil {
		in, out := observation.Usage.InputTokens, observation.Usage.OutputTokens
		inputTokens, outputTokens = &in, &out
	}
	if _, err = tx.Exec(ctx, `INSERT INTO repomesh_models.test_observations
		(evidence_id,actor,test_id,external_operation_id,started_at,observed_at,code,latency_ms,input_tokens,output_tokens,generation,recorded_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		observation.EvidenceID, observation.ActorID, observation.TestID, observation.ExternalOperationID,
		observation.StartedAt, observation.ObservedAt, observation.Code, observation.LatencyMS,
		inputTokens, outputTokens, observation.Generation, r.instanceID); err != nil {
		return unavailable()
	}
	var state string
	switch {
	case observation.Code == "success":
		state = "passed"
	case observation.Code == "unknown":
		state = "unknown"
	default:
		state = "failed"
	}
	if _, err = tx.Exec(ctx, `UPDATE repomesh_models.tests SET state=$3,result_code=$4,observed_at=$5,
		result_revision=result_revision+1 WHERE actor=$1 AND test_id=$2`,
		observation.ActorID, observation.TestID, state, observation.Code, now); err != nil {
		return unavailable()
	}
	if state == "unknown" {
		if _, err = tx.Exec(ctx, `INSERT INTO repomesh_models.actor_outstanding_tests (actor,test_id,opened_reason,opened_at)
			VALUES ($1,$2,'send_unknown',$3) ON CONFLICT (actor) DO UPDATE SET test_id=EXCLUDED.test_id,
			opened_reason=EXCLUDED.opened_reason,opened_at=EXCLUDED.opened_at
			WHERE repomesh_models.actor_outstanding_tests.test_id=EXCLUDED.test_id`,
			observation.ActorID, observation.TestID, now); err != nil {
			return unavailable()
		}
	} else {
		if _, err = tx.Exec(ctx, `DELETE FROM repomesh_models.actor_outstanding_tests
			WHERE actor=$1 AND test_id=$2`, observation.ActorID, observation.TestID); err != nil {
			return unavailable()
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE repomesh_models.test_dispatch SET lease_until=NULL,sender_instance_id=NULL
		WHERE actor=$1 AND test_id=$2`, observation.ActorID, observation.TestID); err != nil {
		return unavailable()
	}
	if err = tx.Commit(ctx); err != nil {
		return unavailable()
	}
	return nil
}

// reconcile runs when a send could not be confirmed either way. It proves the
// dispatch still carries a usable permit and releases the budget reservation,
// then returns the test to queued (or unknown when the permit is spent).
func (r *TestRunner) reconcile(ctx context.Context, actorID, testID string) error {
	tx, err := r.service.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return unavailable()
	}
	defer rollback(tx)
	proof, err := r.service.budgets.ProveUnsent(ctx, tx, actorID, testID)
	if err != nil {
		return convertBudgetFailure(err)
	}
	reservation := modelbudget.Reservation{ActorID: actorID, TestID: testID}
	if err = r.service.budgets.ReleaseUnsent(ctx, tx, reservation, proof); err != nil {
		return convertBudgetFailure(err)
	}
	var mayHaveSent, revokedAt *time.Time
	err = tx.QueryRow(ctx, `SELECT may_have_sent_at,capability_revoked_at FROM repomesh_models.test_dispatch
		WHERE actor=$1 AND test_id=$2 FOR UPDATE`, actorID, testID).Scan(&mayHaveSent, &revokedAt)
	if errorsIsNoRows(err) {
		return failure(404, "DISPATCH_NOT_FOUND")
	}
	if err != nil {
		return unavailable()
	}
	if mayHaveSent != nil || revokedAt != nil {
		return failure(409, "SEND_ALREADY_AUTHORIZED")
	}
	if _, err = tx.Exec(ctx, `UPDATE repomesh_models.tests SET state='queued',result_revision=result_revision+1
		WHERE actor=$1 AND test_id=$2 AND state='running'`, actorID, testID); err != nil {
		return unavailable()
	}
	if _, err = tx.Exec(ctx, `UPDATE repomesh_models.test_dispatch SET lease_until=NULL,sender_instance_id=NULL
		WHERE actor=$1 AND test_id=$2`, actorID, testID); err != nil {
		return unavailable()
	}
	if err = tx.Commit(ctx); err != nil {
		return unavailable()
	}
	return nil
}

// RunOne performs at most one claim-authorize-send-record cycle. It returns
// false when no queued test was available. Every failure path either records
// an observation or reconciles; no state is left mid-flight.
func (r *TestRunner) RunOne(ctx context.Context) (bool, error) {
	claim, found, err := r.claimUnsent(ctx)
	if err != nil || !found {
		return false, err
	}
	dispatch, err := r.authorizeOneSend(ctx, claim)
	if err != nil {
		if rerr := r.reconcile(ctx, claim.owner, claim.testID); rerr != nil {
			return true, rerr
		}
		return true, err
	}
	observation := r.transport.sendOnce(ctx, dispatch)
	dispatch.clear()
	if observation.Code == "transport_failure" {
		if rerr := r.reconcile(ctx, claim.owner, claim.testID); rerr != nil {
			return true, rerr
		}
		return true, nil
	}
	if err = r.recordObservation(ctx, observation); err != nil {
		return true, err
	}
	return true, nil
}
