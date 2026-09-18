package models

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/modelbudget"
	"repomesh.local/repomesh/internal/projects"
	"repomesh.local/repomesh/internal/secrets"
)

// TestService implements B05 model tests: preview, registration with budget
// reservation, and result reads.
type TestService struct {
	pool          *pgxpool.Pool
	service       *Service
	authorization *access.Service
	secrets       *secrets.Store
	budgets       *modelbudget.Store
}

// NewTestService assembles the test service.
func NewTestService(pool *pgxpool.Pool, authorization *access.Service, secretStore *secrets.Store, budgets *modelbudget.Store) *TestService {
	core := &Service{pool: pool, authorization: authorization, secrets: secretStore}
	return &TestService{pool: pool, service: core, authorization: authorization, secrets: secretStore, budgets: budgets}
}

func (s *TestService) beginWrite(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, unavailable()
	}
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout='2s'`); err != nil {
		rollback(tx)
		return nil, unavailable()
	}
	if _, err := tx.Exec(ctx, `SET LOCAL statement_timeout='5s'`); err != nil {
		rollback(tx)
		return nil, unavailable()
	}
	if _, err := tx.Exec(ctx, `SET LOCAL synchronous_commit=on`); err != nil {
		rollback(tx)
		return nil, unavailable()
	}
	return tx, nil
}

// Preview resolves the exact snapshot, locks the owner's test binding, and
// projects whether a new test may be submitted. No budget window is touched.
func (s *TestService) Preview(ctx context.Context, principal access.ProjectPrincipal, target SnapshotTarget) (TestPreview, error) {
	actor := principal.ActorID()
	tx, err := s.beginWrite(ctx)
	if err != nil {
		return TestPreview{}, err
	}
	defer rollback(tx)
	if err = s.authorization.LockProjectPrincipal(ctx, tx, principal); err != nil {
		return TestPreview{}, err
	}
	view, err := s.readProviderForTest(ctx, tx, actor, target)
	if err != nil {
		return TestPreview{}, err
	}
	binding, err := s.readTestBinding(ctx, tx, actor)
	if err != nil {
		return TestPreview{}, err
	}
	preview, err := s.buildTestPreview(ctx, tx, actor, target, view, binding, time.Now().UTC())
	if err != nil {
		return TestPreview{}, err
	}
	if err = insertTestPreview(ctx, tx, preview); err != nil {
		return TestPreview{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return TestPreview{}, unavailable()
	}
	return preview, nil
}

// insertTestPreview persists the test preview row. The three project columns
// stay NULL for kind='test' per the table CHECK constraints.
func insertTestPreview(ctx context.Context, tx pgx.Tx, preview TestPreview) error {
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_models.previews
		(id,kind,actor,provider_id,provider_revision,model_row_id,max_output_tokens,
		 budget_policy_id,budget_policy_version,time_limit_policy_id,time_limit_policy_version,
		 egress_policy_id,egress_policy_version,expires_at)
		VALUES ($1,'test',$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		preview.ID, preview.Actor, preview.Target.ProviderID, preview.Target.ProviderRevision,
		preview.Target.ModelRowID, preview.MaxOutputTokens,
		preview.Policy.Budget.Ref.ID, preview.Policy.Budget.Ref.Version,
		preview.Policy.Limits.Ref.ID, preview.Policy.Limits.Ref.Version,
		preview.Policy.EgressID, preview.Policy.EgressVersion, preview.ExpiresAt); err != nil {
		return unavailable()
	}
	return nil
}

// readProviderForTest reuses the provider reader but does not fail when the
// secret is currently unavailable; the preview carries the availability.
func (s *TestService) readProviderForTest(ctx context.Context, tx pgx.Tx, actor string, target SnapshotTarget) (ProviderView, error) {
	revision := target.ProviderRevision
	view, err := s.service.readProvider(ctx, tx, actor, target.ProviderID, &revision)
	if err != nil {
		return ProviderView{}, err
	}
	found := false
	for _, row := range view.Models {
		if row.ID == target.ModelRowID {
			found = true
			break
		}
	}
	if !found {
		return ProviderView{}, failure(404, "RESOURCE_NOT_FOUND")
	}
	return view, nil
}

// readTestBinding locks the owner's pinned policy combination.
func (s *TestService) readTestBinding(ctx context.Context, tx pgx.Tx, actor string) (TestPolicy, error) {
	var policy TestPolicy
	var budgetScope string
	var budgetEnabled bool
	err := tx.QueryRow(ctx, `SELECT b.combination_revision,
			b.budget_policy_id,b.budget_policy_version,p.scope,p.daily_limit,p.max_unresolved,p.enabled,
			b.time_limit_policy_id,b.time_limit_policy_version,t.model_request_seconds,t.worker_attempt_seconds,
			b.egress_policy_id,b.egress_policy_version,e.approved_base_urls,e.allow_private_addresses,e.follow_redirects
		FROM repomesh_sources.test_bindings b
		JOIN repomesh_projects.request_policy_versions p ON p.id=b.budget_policy_id AND p.version=b.budget_policy_version
		JOIN repomesh_projects.time_policy_versions t ON t.id=b.time_limit_policy_id AND t.version=b.time_limit_policy_version
		JOIN repomesh_sources.egress_policy_versions e ON e.id=b.egress_policy_id AND e.version=b.egress_policy_version
		WHERE b.owner=$1 FOR UPDATE OF b`, actor).Scan(
		&policy.Revision,
		&policy.Budget.Ref.ID, &policy.Budget.Ref.Version, &budgetScope, &policy.Budget.Limit, &policy.Budget.MaxUnresolved, &budgetEnabled,
		&policy.Limits.Ref.ID, &policy.Limits.Ref.Version, &policy.Limits.ModelRequestSeconds, &policy.Limits.WorkerAttemptSeconds,
		&policy.EgressID, &policy.EgressVersion, &policy.ApprovedBaseURLs, &policy.AllowPrivateAddr, &policy.FollowRedirects)
	policy.Budget.Scope = budgetScope
	policy.Budget.Enabled = budgetEnabled
	policy.Limits.Ref = projects.PolicyRef{ID: policy.Limits.Ref.ID, Version: policy.Limits.Ref.Version}
	if errorsIsNoRows(err) {
		return TestPolicy{}, failure(409, "TEST_BINDING_MISSING")
	}
	if err != nil {
		return TestPolicy{}, unavailable()
	}
	return policy, nil
}

func errorsIsNoRows(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no rows in result set")
}

// buildTestPreview projects the preview: outstanding tests, budget window
// headroom, secret availability, and expiry.
func (s *TestService) buildTestPreview(ctx context.Context, tx pgx.Tx, actor string, target SnapshotTarget, view ProviderView, policy TestPolicy, now time.Time) (TestPreview, error) {
	preview := TestPreview{
		ID:              newID(),
		Actor:           actor,
		Target:          target,
		ModelID:         "",
		Policy:          policy,
		MaxOutputTokens: 0,
		ExpiresAt:       now.Add(10 * time.Minute),
		CanSubmit:       true,
		Reasons:         []string{},
	}
	var modelRow ModelView
	var modelFound bool
	for _, row := range view.Models {
		if row.ID == target.ModelRowID {
			modelRow, modelFound = row, true
			break
		}
	}
	if !modelFound {
		return TestPreview{}, failure(404, "RESOURCE_NOT_FOUND")
	}
	preview.ModelID = modelRow.ModelID
	preview.MaxOutputTokens = modelRow.MaxOutputTokens
	preview.Model = projects.ModelBinding{
		Profile:          projects.ProfileVersionRef{ID: modelRow.ModelProfileID, Version: view.Revision},
		ProviderID:       target.ProviderID,
		ProviderRevision: target.ProviderRevision,
		ModelRowID:       target.ModelRowID,
		SecretVersion:    secrets.VersionID(*view.Secret.VersionID),
	}
	var existingID string
	var existingState string
	err := tx.QueryRow(ctx, `SELECT test_id,state FROM repomesh_models.tests
		WHERE actor=$1 AND provider_id=$2 AND provider_revision=$3 AND model_row_id=$4
		AND state IN ('queued','running','unknown') AND removed_at IS NULL
		ORDER BY accepted_at DESC LIMIT 1`, actor, target.ProviderID, target.ProviderRevision, target.ModelRowID).Scan(&existingID, &existingState)
	if err == nil {
		preview.ExistingTestID = &existingID
		preview.ExistingOutstanding = true
		if existingState != "unknown" {
			preview.CanSubmit = false
			preview.Reasons = append(preview.Reasons, "test_already_outstanding")
		}
	}
	if view.Secret.Availability != "available" {
		preview.CanSubmit = false
		preview.Reasons = append(preview.Reasons, "provider_secret_"+view.Secret.Availability)
	}
	var unresolved int64
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM repomesh_models.tests
		WHERE actor=$1 AND state='unknown' AND removed_at IS NULL`, actor).Scan(&unresolved); err != nil {
		return TestPreview{}, unavailable()
	}
	if unresolved >= policy.Budget.MaxUnresolved {
		preview.CanSubmit = false
		preview.Reasons = append(preview.Reasons, "unresolved_test_limit_reached")
	}
	if len(preview.Reasons) == 0 {
		preview.Reasons = []string{}
	}
	return preview, nil
}

// Submit registers a new test from a valid preview. The whole write —
// principal, replay, registration, budget reservation, dispatch row, and
// outstanding pointer — commits atomically.
func (s *TestService) Submit(ctx context.Context, principal access.ProjectPrincipal, command TestCommand) (TestResult, error) {
	actor := principal.ActorID()
	tx, err := s.beginWrite(ctx)
	if err != nil {
		return TestResult{}, err
	}
	defer rollback(tx)
	if err = s.authorization.LockProjectPrincipal(ctx, tx, principal); err != nil {
		return TestResult{}, err
	}
	if replay, err := s.replayTest(ctx, tx, actor, command.key); err != nil || replay.found {
		if replay.found {
			if err = tx.Commit(ctx); err != nil {
				return TestResult{}, unavailable()
			}
			return replay.result, nil
		}
		return TestResult{}, err
	}
	preview, err := s.lockApplicationPreviewTest(ctx, tx, actor, command.previewID)
	if err != nil {
		return TestResult{}, err
	}
	if err = validatePreviewForNewTest(preview, actor, command, time.Now().UTC()); err != nil {
		return TestResult{}, err
	}
	result, err := s.registerTest(ctx, tx, actor, command, preview)
	if err != nil {
		return TestResult{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return TestResult{}, unavailable()
	}
	return result, nil
}

type testReplay struct {
	found  bool
	result TestResult
}

// replayTest returns the stored result for an already-used command key.
func (s *TestService) replayTest(ctx context.Context, tx pgx.Tx, actor, key string) (testReplay, error) {
	var replay testReplay
	var acceptedAt time.Time
	var resultRevision int64
	var state, recovery string
	var removedAt *time.Time
	err := tx.QueryRow(ctx, `SELECT accepted_at,result_revision,state,recovery,removed_at
		FROM repomesh_models.tests WHERE actor=$1 AND test_id=$2 FOR UPDATE`, actor, key).Scan(
		&acceptedAt, &resultRevision, &state, &recovery, &removedAt)
	if errorsIsNoRows(err) {
		return replay, nil
	}
	if err != nil {
		return replay, unavailable()
	}
	replay.found = true
	result, readErr := s.readTestResultInTx(ctx, tx, actor, key)
	if readErr != nil {
		return replay, readErr
	}
	replay.result = result
	return replay, nil
}

// lockApplicationPreviewTest locks the actor's unconsumed test preview and
// re-projects it as a TestPreview. The stored preview row is the only
// registration input; nothing is re-resolved from current state.
func (s *TestService) lockApplicationPreviewTest(ctx context.Context, tx pgx.Tx, actor, previewID string) (TestPreview, error) {
	var kind string
	var target SnapshotTarget
	var maxOutput int64
	var budgetRef, timeRef projects.PolicyRef
	var budgetScope string
	var budgetLimit, budgetMaxUnresolved int64
	var budgetEnabled bool
	var timeSeconds, workerSeconds int64
	var egressID, egressVersion string
	var approvedURLs []string
	var allowPrivate, followRedirects bool
	err := tx.QueryRow(ctx, `SELECT v.kind,v.provider_id,v.provider_revision,v.model_row_id,v.max_output_tokens,
			v.budget_policy_id,v.budget_policy_version,p.scope,p.daily_limit,p.max_unresolved,p.enabled,
			v.time_limit_policy_id,v.time_limit_policy_version,t.model_request_seconds,t.worker_attempt_seconds,
			v.egress_policy_id,v.egress_policy_version,e.approved_base_urls,e.allow_private_addresses,e.follow_redirects
		FROM repomesh_models.previews v
		JOIN repomesh_projects.request_policy_versions p ON p.id=v.budget_policy_id AND p.version=v.budget_policy_version
		JOIN repomesh_projects.time_policy_versions t ON t.id=v.time_limit_policy_id AND t.version=v.time_limit_policy_version
		JOIN repomesh_sources.egress_policy_versions e ON e.id=v.egress_policy_id AND e.version=v.egress_policy_version
		WHERE v.actor=$1 AND v.id=$2 FOR UPDATE OF v`,
		actor, previewID).Scan(&kind, &target.ProviderID, &target.ProviderRevision, &target.ModelRowID,
		&maxOutput, &budgetRef.ID, &budgetRef.Version, &budgetScope, &budgetLimit, &budgetMaxUnresolved, &budgetEnabled,
		&timeRef.ID, &timeRef.Version, &timeSeconds, &workerSeconds,
		&egressID, &egressVersion, &approvedURLs, &allowPrivate, &followRedirects)
	if errorsIsNoRows(err) {
		return TestPreview{}, failure(404, "RESOURCE_NOT_FOUND")
	}
	if err != nil {
		return TestPreview{}, unavailable()
	}
	if kind != "test" {
		return TestPreview{}, failure(404, "RESOURCE_NOT_FOUND")
	}
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
	preview := TestPreview{ID: previewID, Actor: actor, Target: target, Policy: policy, MaxOutputTokens: maxOutput, CanSubmit: true, Reasons: []string{}}
	var modelID string
	err = tx.QueryRow(ctx, `SELECT s.model_id FROM repomesh_models.model_snapshots s
		WHERE s.provider_id=$1 AND s.provider_revision=$2 AND s.row_id=$3`,
		target.ProviderID, target.ProviderRevision, target.ModelRowID).Scan(&modelID)
	if errorsIsNoRows(err) {
		return TestPreview{}, failure(404, "RESOURCE_NOT_FOUND")
	}
	if err != nil {
		return TestPreview{}, unavailable()
	}
	preview.ModelID = modelID
	return preview, nil
}

// validatePreviewForNewTest re-checks the locked preview's submit conditions
// at registration time. TEST_ALREADY_OUTSTANDING carries the blocker details.
func validatePreviewForNewTest(preview TestPreview, actor string, command TestCommand, now time.Time) error {
	if actor != preview.Actor {
		return failure(404, "RESOURCE_NOT_FOUND")
	}
	if now.After(preview.ExpiresAt) {
		return failure(409, "PREVIEW_EXPIRED")
	}
	if !command.confirmPotentialCharge {
		field := []FieldError{{Field: "confirmPotentialCharge", Code: "REQUIRED"}}
		return &TestSubmitFailure{Status: 422, Code: "VALIDATION_FAILED", FieldErrors: field}
	}
	if preview.ExistingOutstanding {
		details := &TestOutstandingDetails{
			ExistingTestID: *preview.ExistingTestID,
			Links:          TestOperationLinks{Operation: "/api/model-tests/" + *preview.ExistingTestID},
		}
		return &TestSubmitFailure{Status: 409, Code: "TEST_ALREADY_OUTSTANDING", Details: details}
	}
	return nil
}

// registerTest writes the tests row, the dispatch responsibility, the
// outstanding pointer, and the budget reservation in the caller's tx, then
// consumes the preview. A missing actor window is initialized in the same tx
// and the reservation retried once.
func (s *TestService) registerTest(ctx context.Context, tx pgx.Tx, actor string, command TestCommand, preview TestPreview) (TestResult, error) {
	reservation, err := s.budgets.ReserveTest(ctx, tx, actor, command.key, preview.Policy.Budget, time.Now().UTC())
	if err != nil {
		var mf *modelbudget.Failure
		if errors.As(err, &mf) && mf.Code == "WINDOW_NOT_INITIALIZED" {
			if ensureErr := s.ensureTestWindow(ctx, tx, actor, preview.Policy.Budget); ensureErr != nil {
				return TestResult{}, ensureErr
			}
			reservation, err = s.budgets.ReserveTest(ctx, tx, actor, command.key, preview.Policy.Budget, time.Now().UTC())
		}
		if err != nil {
			return TestResult{}, convertBudgetFailure(err)
		}
	}
	return s.persistTestRegistration(ctx, tx, actor, command, preview, reservation)
}

// ensureTestWindow creates today's actor window when absent. The DB trigger
// keeps the limit decrease-only, so ON CONFLICT DO NOTHING is safe.
func (s *TestService) ensureTestWindow(ctx context.Context, tx pgx.Tx, actor string, policy projects.RequestPolicy) error {
	now := time.Now().UTC()
	utc := now
	start := time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	_, err := tx.Exec(ctx, `INSERT INTO repomesh_modelbudget.windows
		(scope_kind, actor_scope_id, project_scope_id, start_utc, end_utc, daily_limit, revision)
		VALUES ('actor_model_test', $1, NULL, $2, $3, $4, $5)
		ON CONFLICT (scope_kind, scope_id, start_utc) DO NOTHING`,
		actor, start, end, policy.Limit, newID())
	if err != nil {
		return unavailable()
	}
	return nil
}

func convertBudgetFailure(err error) error {
	var mf *modelbudget.Failure
	if errors.As(err, &mf) {
		switch mf.Code {
		case "QUOTA_EXHAUSTED":
			return failure(409, "QUOTA_EXHAUSTED")
		case "WINDOW_EXPIRED":
			return failure(409, "QUOTA_WINDOW_EXPIRED")
		case "WINDOW_HISTORY_NOT_PROVABLE", "WINDOW_EVIDENCE_MISMATCH", "WINDOW_NOT_INITIALIZED", "WINDOW_LIMIT_BELOW_RESERVATIONS":
			return failure(503, "RESULT_UNCONFIRMED")
		case "EXTERNAL_OPERATION_REQUIRED", "RELEASE_PROOF_MISMATCH", "RESERVATION_NOT_OUTSTANDING", "SEND_ALREADY_AUTHORIZED", "DISPATCH_NOT_FOUND":
			return failure(mf.Status, mf.Code)
		}
		return failure(503, "RESULT_UNCONFIRMED")
	}
	return unavailable()
}

func (s *TestService) persistTestRegistration(ctx context.Context, tx pgx.Tx, actor string, command TestCommand, preview TestPreview, reservation modelbudget.Reservation) (TestResult, error) {
	now := time.Now().UTC()
	canonical := map[string]any{
		"previewId":              preview.ID,
		"providerId":             preview.Target.ProviderID,
		"providerRevision":       preview.Target.ProviderRevision,
		"modelRowId":             preview.Target.ModelRowID,
		"maxOutputTokens":        preview.MaxOutputTokens,
		"confirmPotentialCharge": command.confirmPotentialCharge,
	}
	canonicalJSON, err := json.Marshal(canonical)
	if err != nil {
		return TestResult{}, unavailable()
	}
	budgetStatus, err := reservationState(ctx, tx, actor, command.key)
	if err != nil {
		return TestResult{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO repomesh_models.tests
		(actor,test_id,provider_id,provider_revision,model_row_id,max_output_tokens,
		 budget_policy_id,budget_policy_version,time_limit_policy_id,time_limit_policy_version,
		 egress_policy_id,egress_policy_version,preview_id,canonical_input,state,result_revision,
		 recovery,accepted_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14::jsonb,'queued',1,'none',$15)`,
		actor, command.key, preview.Target.ProviderID, preview.Target.ProviderRevision, preview.Target.ModelRowID,
		preview.MaxOutputTokens, preview.Policy.Budget.Ref.ID, preview.Policy.Budget.Ref.Version,
		preview.Policy.Limits.Ref.ID, preview.Policy.Limits.Ref.Version,
		preview.Policy.EgressID, preview.Policy.EgressVersion, preview.ID, string(canonicalJSON), now); err != nil {
		return TestResult{}, unavailable()
	}
	externalOperationID := newID()
	permitID := newID()
	if _, err = tx.Exec(ctx, `INSERT INTO repomesh_models.test_dispatch
		(actor,test_id,external_operation_id,send_permit_id,credential_capability_id,credential_capability_version)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		actor, command.key, externalOperationID, permitID,
		preview.Target.ProviderID, string(preview.Model.SecretVersion)); err != nil {
		return TestResult{}, unavailable()
	}
	if _, err = tx.Exec(ctx, `INSERT INTO repomesh_models.actor_outstanding_tests
		(actor,test_id,opened_reason,opened_at) VALUES ($1,$2,'test_registered',$3)
		ON CONFLICT (actor) DO NOTHING`, actor, command.key, now); err != nil {
		return TestResult{}, unavailable()
	}
	if _, err = tx.Exec(ctx, `UPDATE repomesh_models.previews
		SET consumed_by_operation=$3, consumed_at=$4 WHERE actor=$1 AND id=$2`,
		actor, preview.ID, command.key, now); err != nil {
		return TestResult{}, unavailable()
	}
	return TestResult{
		ID:             command.key,
		Actor:          actor,
		Target:         preview.Target,
		ModelID:        preview.ModelID,
		AcceptedAt:     now,
		ResultRevision: strconv.FormatInt(1, 10),
		State:          "queued",
		BudgetStatus:   budgetStatus,
		Recovery:       "none",
	}, nil
}

func reservationState(ctx context.Context, tx pgx.Tx, actor, testID string) (string, error) {
	var state string
	err := tx.QueryRow(ctx, `SELECT state FROM repomesh_modelbudget.test_reservations
		WHERE actor=$1 AND test_id=$2`, actor, testID).Scan(&state)
	if err == nil {
		return state, nil
	}
	return "reserved", nil
}

// readTestResultInTx projects one test: identity from tests, model id from
// snapshots, budget status from the reservation, latest observation optional.
func (s *TestService) readTestResultInTx(ctx context.Context, tx pgx.Tx, actor, testID string) (TestResult, error) {
	var result TestResult
	var resultRevision int64
	var acceptedAt time.Time
	var observedAt *time.Time
	var resultCode *string
	var removedAt *time.Time
	err := tx.QueryRow(ctx, `SELECT t.test_id,t.provider_id,t.provider_revision,t.model_row_id,
			s.model_id,t.accepted_at,t.result_revision,t.state,t.recovery,t.observed_at,t.result_code,t.removed_at
		FROM repomesh_models.tests t
		JOIN repomesh_models.model_snapshots s
		  ON s.provider_id=t.provider_id AND s.provider_revision=t.provider_revision AND s.row_id=t.model_row_id
		WHERE t.actor=$1 AND t.test_id=$2`, actor, testID).Scan(
		&result.ID, &result.Target.ProviderID, &result.Target.ProviderRevision, &result.Target.ModelRowID,
		&result.ModelID, &acceptedAt, &resultRevision, &result.State, &result.Recovery, &observedAt, &resultCode, &removedAt)
	if errorsIsNoRows(err) {
		return TestResult{}, failure(404, "RESOURCE_NOT_FOUND")
	}
	if err != nil {
		return TestResult{}, unavailable()
	}
	result.Actor = actor
	result.AcceptedAt = acceptedAt
	result.ResultRevision = strconv.FormatInt(resultRevision, 10)
	result.RemovedAt = removedAt
	budgetStatus, budgetErr := reservationState(ctx, tx, actor, testID)
	if budgetErr != nil {
		return TestResult{}, budgetErr
	}
	result.BudgetStatus = budgetStatus
	var externalOperationID string
	obsErr := tx.QueryRow(ctx, `SELECT d.external_operation_id FROM repomesh_models.test_dispatch d
		WHERE d.actor=$1 AND d.test_id=$2`, actor, testID).Scan(&externalOperationID)
	if obsErr == nil {
		result.Observation = s.readLatestObservation(ctx, tx, actor, testID, externalOperationID)
	} else if !errorsIsNoRows(obsErr) {
		return TestResult{}, unavailable()
	}
	return result, nil
}

func (s *TestService) readLatestObservation(ctx context.Context, tx pgx.Tx, actor, testID, externalOperationID string) *DispatchObservation {
	var obs DispatchObservation
	var external string
	var startedAt *time.Time
	var latency *int64
	var inputTokens, outputTokens *int64
	err := tx.QueryRow(ctx, `SELECT o.evidence_id,o.external_operation_id,o.started_at,o.observed_at,o.code,
			o.latency_ms,o.input_tokens,o.output_tokens
		FROM repomesh_models.test_observations o
		WHERE o.actor=$1 AND o.test_id=$2
		ORDER BY o.observed_at DESC LIMIT 1`, actor, testID).Scan(
		&obs.EvidenceID, &external, &startedAt, &obs.ObservedAt, &obs.Code, &latency, &inputTokens, &outputTokens)
	if err != nil {
		return nil
	}
	obs.ExternalOperationID = external
	obs.StartedAt = startedAt
	obs.LatencyMS = latency
	if inputTokens != nil && outputTokens != nil {
		obs.Usage = &TokenUsage{InputTokens: *inputTokens, OutputTokens: *outputTokens}
	}
	return &obs
}

// Get returns the current projection of one test. Removed tests read as gone.
func (s *TestService) Get(ctx context.Context, principal access.ProjectPrincipal, testID string) (TestResult, error) {
	actor := principal.ActorID()
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return TestResult{}, unavailable()
	}
	defer rollback(tx)
	if err = s.authorization.LockProjectPrincipal(ctx, tx, principal); err != nil {
		return TestResult{}, err
	}
	result, err := s.readTestResultInTx(ctx, tx, actor, testID)
	if err != nil {
		return TestResult{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return TestResult{}, unavailable()
	}
	return result, nil
}
