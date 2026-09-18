package models

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/projects"
	"repomesh.local/repomesh/internal/secrets"
)

// ApplicationService implements B05 dedicated model application: preview the
// candidate against the project's fixed configuration, then apply it through
// projects.ReplaceModel while retaining the execution selection. Application
// writes retain the existing project path: principal/account, project,
// operation, catalog/profile, Provider, preview and secret availability.
type ApplicationService struct {
	pool          *pgxpool.Pool
	service       *Service
	projects      *projects.Service
	authorization *access.Service
	secrets       *secrets.Store
}

// NewApplicationService assembles the application service.
func NewApplicationService(pool *pgxpool.Pool, authorization *access.Service, projectService *projects.Service, secretStore *secrets.Store) *ApplicationService {
	core := &Service{pool: pool, authorization: authorization, secrets: secretStore}
	return &ApplicationService{pool: pool, service: core, projects: projectService, authorization: authorization, secrets: secretStore}
}

func (s *ApplicationService) beginWrite(ctx context.Context) (pgx.Tx, error) {
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

// readProviderForApply reuses the provider reader but does not fail when the
// secret is currently unavailable; the preview carries the availability.
func (s *ApplicationService) readProviderForApply(ctx context.Context, tx pgx.Tx, actor string, target SnapshotTarget) (ProviderView, error) {
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

// readApplyPolicy locks the owner's pinned policy combination. Apply previews
// reuse the same combination source as test previews: it is the only complete
// budget/time/egress triple an owner has in the current schema.
func (s *ApplicationService) readApplyPolicy(ctx context.Context, tx pgx.Tx, actor string) (TestPolicy, error) {
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
	if errorsIsNoRows(err) {
		return TestPolicy{}, failure(409, "TEST_BINDING_MISSING")
	}
	if err != nil {
		return TestPolicy{}, unavailable()
	}
	return policy, nil
}

// projectOriginalModel builds the authorized projection of the current model
// binding. A missing historical binding is restricted, never resolved from a
// latest-provider lookup.
func projectOriginalModel(fixed projects.FixedConfiguration) OriginalModel {
	if !fixed.HasModelReference() {
		return UnconfiguredOriginal{}
	}
	if binding, ok := fixed.Model(); ok {
		return ReadableOriginal{
			Binding:      binding,
			Reference:    fixed.ModelChoice(),
			SnapshotPath: providerPath(binding.ProviderID) + "/versions/" + binding.ProviderRevision,
		}
	}
	return RestrictedOriginal{Reasons: []string{"ORIGINAL_MODEL_UNAVAILABLE"}}
}

// Preview builds the apply preview: the candidate snapshot against the
// project's current fixed configuration. The original model is projected as
// unconfigured, readable, or restricted.
func (s *ApplicationService) Preview(ctx context.Context, principal access.ProjectPrincipal, projectID string, target SnapshotTarget) (ApplicationPreview, error) {
	projectID = normalizeKey(projectID)
	if !validUUID(projectID) {
		return ApplicationPreview{}, validation("projectId")
	}
	actor := principal.ActorID()
	tx, err := s.beginWrite(ctx)
	if err != nil {
		return ApplicationPreview{}, err
	}
	defer rollback(tx)
	if err = s.authorization.LockProjectPrincipal(ctx, tx, principal); err != nil {
		return ApplicationPreview{}, err
	}
	view, err := s.readProviderForApply(ctx, tx, actor, target)
	if err != nil {
		return ApplicationPreview{}, err
	}
	policy, err := s.readApplyPolicy(ctx, tx, actor)
	if err != nil {
		return ApplicationPreview{}, err
	}
	project, err := s.projects.LockForConfiguration(ctx, tx, principal, projectID)
	if err != nil {
		return ApplicationPreview{}, err
	}
	fixed, err := s.projects.ReadFixed(ctx, tx, principal, projectID, "")
	if err != nil {
		return ApplicationPreview{}, err
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
		return ApplicationPreview{}, failure(404, "RESOURCE_NOT_FOUND")
	}
	candidate := projects.ModelBinding{
		Profile:          projects.ProfileVersionRef{ID: modelRow.ModelProfileID, Version: target.ProviderRevision},
		ProviderID:       target.ProviderID,
		ProviderRevision: target.ProviderRevision,
		ModelRowID:       target.ModelRowID,
		SecretVersion:    secrets.VersionID(*view.Secret.VersionID),
	}
	reasons := []string{}
	canApply := true
	if view.Secret.Availability != "available" {
		canApply = false
		reasons = append(reasons, "provider_secret_"+view.Secret.Availability)
	}
	now := time.Now().UTC()
	preview := ApplicationPreview{
		ID:                    newID(),
		Actor:                 actor,
		ProjectID:             projectID,
		ProjectRevision:       project.Revision(),
		Before:                fixed,
		original:              projectOriginalModel(fixed),
		candidateModelID:      modelRow.ModelID,
		Candidate:             candidate,
		configurationRevision: string(fixed.Revision()),
		Target:                target,
		MaxOutputTokens:       modelRow.MaxOutputTokens,
		ExpiresAt:             now.Add(5 * time.Minute),
		CanApply:              canApply,
		Reasons:               reasons,
	}
	if err = insertApplyPreview(ctx, tx, preview, policy); err != nil {
		return ApplicationPreview{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return ApplicationPreview{}, unavailable()
	}
	return preview, nil
}

// applicationSnapshot is the immutable before-state stored with the preview
// row: the configuration revision, the before model binding, and the
// authorized original projection.
type applicationSnapshot struct {
	ConfigurationRevision string            `json:"configurationRevision"`
	Model                 *modelBindingJSON `json:"model,omitempty"`
	Original              originalModelJSON `json:"original"`
}

// committedCandidateJSON is the candidate snapshot stored with a committed
// application operation; it also carries the created configuration revision
// so replay can rebuild the original receipt.
type committedCandidateJSON struct {
	modelBindingJSON
	ConfigurationRevision string `json:"configurationRevision"`
}

func candidateBindingJSON(binding projects.ModelBinding) modelBindingJSON {
	return modelBindingJSON{
		ProfileID:        binding.Profile.ID,
		ProfileVersion:   binding.Profile.Version,
		ProviderID:       binding.ProviderID,
		ProviderRevision: binding.ProviderRevision,
		ModelRowID:       binding.ModelRowID,
	}
}

func beforeModelJSON(fixed projects.FixedConfiguration) *modelBindingJSON {
	binding, ok := fixed.Model()
	if !ok {
		return nil
	}
	value := candidateBindingJSON(binding)
	return &value
}

func insertApplyPreview(ctx context.Context, tx pgx.Tx, preview ApplicationPreview, policy TestPolicy) error {
	snapshot := applicationSnapshot{
		ConfigurationRevision: preview.configurationRevision,
		Model:                 beforeModelJSON(preview.Before),
		Original:              marshalOriginal(preview.original),
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return unavailable()
	}
	if _, err = tx.Exec(ctx, `INSERT INTO repomesh_models.previews
		(id,kind,actor,provider_id,provider_revision,model_row_id,max_output_tokens,
		 budget_policy_id,budget_policy_version,time_limit_policy_id,time_limit_policy_version,
		 egress_policy_id,egress_policy_version,project_id,project_revision,configuration_snapshot,expires_at)
		VALUES ($1,'apply',$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15::jsonb,$16)`,
		preview.ID, preview.Actor, preview.Candidate.ProviderID, preview.Candidate.ProviderRevision,
		preview.Candidate.ModelRowID, preview.MaxOutputTokens,
		policy.Budget.Ref.ID, policy.Budget.Ref.Version,
		policy.Limits.Ref.ID, policy.Limits.Ref.Version, policy.EgressID, policy.EgressVersion,
		preview.ProjectID, preview.ProjectRevision, string(raw), preview.ExpiresAt); err != nil {
		return unavailable()
	}
	return nil
}

// Apply consumes a valid apply preview and swaps the project's model through
// projects.ReplaceModel. A preview that no longer holds produces a determined
// rejected receipt; the operation and its receipt commit atomically either
// way. An unchanged configuration is still a committed receipt without a new
// revision... ReplaceModel always creates a revision; the receipt records it.
func (s *ApplicationService) Apply(ctx context.Context, principal access.ProjectPrincipal, command ApplicationCommand) (ApplicationReceipt, error) {
	actor := principal.ActorID()
	tx, err := s.beginWrite(ctx)
	if err != nil {
		return nil, err
	}
	defer rollback(tx)
	if err = s.authorization.LockProjectPrincipal(ctx, tx, principal); err != nil {
		return nil, err
	}
	if receipt, found, err := s.replayApplication(ctx, tx, actor, command); err != nil || found {
		if found {
			if err = tx.Commit(ctx); err != nil {
				return nil, unavailable()
			}
			return receipt, nil
		}
		return nil, err
	}
	row, err := lockApplicationPreview(ctx, tx, actor, command.projectID, command.previewID)
	if err != nil {
		return nil, err
	}
	head, err := lockProvider(ctx, tx, actor, row.providerID)
	if err != nil {
		return nil, err
	}
	project, err := s.projects.LockForConfiguration(ctx, tx, principal, command.projectID)
	if err != nil {
		return nil, err
	}
	fixed, err := s.projects.ReadFixed(ctx, tx, principal, command.projectID, "")
	if err != nil {
		return nil, err
	}
	candidate, err := buildCandidateBinding(ctx, tx, row)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if err = compareApplicationPreview(row, head, project, fixed, now); err != nil {
		code, determined := failureCode(err)
		if !determined {
			return nil, err
		}
		receipt := ApplicationRejected{ApplicationID: command.key, Error: APIError{Code: code, Message: code}, DecidedAt: now}
		if err = persistApplication(ctx, tx, actor, command, string(fixed.Revision()), candidate, receipt); err != nil {
			return nil, err
		}
		if err = tx.Commit(ctx); err != nil {
			return nil, unavailable()
		}
		return receipt, nil
	}
	change, err := s.projects.ReplaceModel(ctx, tx, project, projects.ModelReplacement{
		ExpectedProjectRevision: project.Revision(),
		Before:                  fixed,
		Candidate:               candidate,
	}, now)
	if err != nil {
		return nil, err
	}
	retained, _ := fixed.Execution()
	applied := Applied{
		ApplicationID:         command.key,
		ProjectID:             command.projectID,
		ProjectRevision:       change.ProjectRevision,
		ConfigurationRevision: change.ConfigurationRevision,
		Candidate:             candidate,
		RetainedExecution:     retained,
		CommittedAt:           change.CommittedAt,
	}
	if err = persistApplication(ctx, tx, actor, command, string(fixed.Revision()), candidate, applied); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `UPDATE repomesh_models.previews
		SET consumed_by_operation=$3, consumed_at=$4 WHERE actor=$1 AND id=$2`,
		actor, command.previewID, command.key, now); err != nil {
		return nil, unavailable()
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, unavailable()
	}
	return applied, nil
}

// failureCode extracts the determined code from a Failure error.
func failureCode(err error) (string, bool) {
	var f *Failure
	if errors.As(err, &f) {
		return f.Code, true
	}
	return "", false
}

// applicationPreviewRow is the locked preview row as apply consumes it.
type applicationPreviewRow struct {
	previewID        string
	actor            string
	projectID        string
	providerID       string
	providerRevision string
	rowID            string
	maxOutput        int64
	projectRevision  string
	snapshot         applicationSnapshot
	expiresAt        time.Time
	consumedBy       *string
}

// lockApplicationPreview locks the actor's unconsumed apply preview and
// parses its stored snapshot. The stored row is the only apply input.
func lockApplicationPreview(ctx context.Context, tx pgx.Tx, actor, projectID, previewID string) (applicationPreviewRow, error) {
	var row applicationPreviewRow
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT provider_id,provider_revision,model_row_id,max_output_tokens,
			project_revision,configuration_snapshot,expires_at,consumed_by_operation
		FROM repomesh_models.previews
		WHERE actor=$1 AND id=$2 AND kind='apply' AND project_id=$3
		FOR UPDATE`, actor, previewID, projectID).Scan(
		&row.providerID, &row.providerRevision, &row.rowID, &row.maxOutput,
		&row.projectRevision, &raw, &row.expiresAt, &row.consumedBy)
	if errorsIsNoRows(err) {
		return applicationPreviewRow{}, failure(404, "RESOURCE_NOT_FOUND")
	}
	if err != nil {
		return applicationPreviewRow{}, unavailable()
	}
	row.previewID, row.actor, row.projectID = previewID, actor, projectID
	if err = json.Unmarshal(raw, &row.snapshot); err != nil {
		return applicationPreviewRow{}, unavailable()
	}
	return row, nil
}

// buildCandidateBinding resolves the candidate's model profile through the
// authoritative links: model_rows for the profile id, profile_links for the
// secret version pinned to the exact snapshot.
func buildCandidateBinding(ctx context.Context, tx pgx.Tx, row applicationPreviewRow) (projects.ModelBinding, error) {
	var profileID string
	err := tx.QueryRow(ctx, `SELECT m.profile_id FROM repomesh_models.model_rows m
		WHERE m.provider_id=$1 AND m.id=$2`, row.providerID, row.rowID).Scan(&profileID)
	if errorsIsNoRows(err) {
		return projects.ModelBinding{}, failure(404, "RESOURCE_NOT_FOUND")
	}
	if err != nil {
		return projects.ModelBinding{}, unavailable()
	}
	var secretVersion string
	err = tx.QueryRow(ctx, `SELECT pl.secret_version_id FROM repomesh_models.profile_links pl
		WHERE pl.profile_id=$1 AND pl.profile_version=$2`, profileID, row.providerRevision).Scan(&secretVersion)
	if err != nil {
		return projects.ModelBinding{}, unavailable()
	}
	return projects.ModelBinding{
		Profile:          projects.ProfileVersionRef{ID: profileID, Version: row.providerRevision},
		ProviderID:       row.providerID,
		ProviderRevision: row.providerRevision,
		ModelRowID:       row.rowID,
		SecretVersion:    secrets.VersionID(secretVersion),
	}, nil
}

// compareApplicationPreview re-checks every preview assumption at apply time.
// Any determined mismatch returns a Failure whose code becomes the rejection
// receipt code.
func compareApplicationPreview(row applicationPreviewRow, head ProviderView, project projects.LockedProject, fixed projects.FixedConfiguration, now time.Time) error {
	if row.consumedBy != nil {
		return failure(409, "PREVIEW_ALREADY_CONSUMED")
	}
	if now.After(row.expiresAt) {
		return failure(409, "PREVIEW_EXPIRED")
	}
	if row.projectRevision != project.Revision() {
		return failure(409, "PROJECT_REVISION_MISMATCH")
	}
	if string(fixed.Revision()) != row.snapshot.ConfigurationRevision {
		return failure(409, "CONFIGURATION_REVISION_MISMATCH")
	}
	current, hasCurrent := fixed.Model()
	switch {
	case row.snapshot.Model == nil && hasCurrent:
		return failure(409, "MODEL_CHANGED")
	case row.snapshot.Model != nil && !hasCurrent:
		return failure(409, "MODEL_CHANGED")
	case row.snapshot.Model != nil &&
		(row.snapshot.Model.ProfileID != current.Profile.ID || row.snapshot.Model.ProfileVersion != current.Profile.Version):
		return failure(409, "MODEL_CHANGED")
	}
	if head.Revision != row.providerRevision {
		return failure(409, "PROVIDER_HEAD_CHANGED")
	}
	return nil
}

// persistApplication writes the operation receipt. The receipt is immutable
// once written; removed_at is the only mutable column afterwards.
func persistApplication(ctx context.Context, tx pgx.Tx, actor string, command ApplicationCommand, beforeRevision string, candidate projects.ModelBinding, receipt ApplicationReceipt) error {
	var outcome string
	var rejectionCode, projectRevisionAfter string
	var retainedID, retainedVersion *string
	committedAt := time.Now().UTC()
	configurationRevision := ""
	switch value := receipt.(type) {
	case Applied:
		outcome = "committed"
		projectRevisionAfter = value.ProjectRevision
		configurationRevision = string(value.ConfigurationRevision)
		retainedID = &value.RetainedExecution.ID
		retainedVersion = &value.RetainedExecution.Version
		committedAt = value.CommittedAt
	case ApplicationRejected:
		outcome = "rejected"
		rejectionCode = value.Error.Code
	}
	snapshot := committedCandidateJSON{
		modelBindingJSON:      candidateBindingJSON(candidate),
		ConfigurationRevision: configurationRevision,
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return unavailable()
	}
	var after, rejection *string
	if projectRevisionAfter != "" {
		after = &projectRevisionAfter
	}
	if rejectionCode != "" {
		rejection = &rejectionCode
	}
	if _, err = tx.Exec(ctx, `INSERT INTO repomesh_models.application_operations
		(project_id,actor,application_id,request_id,preview_id,project_revision_before,project_revision_after,
		 candidate_snapshot,outcome,rejection_code,retained_execution_profile_id,retained_execution_profile_version,committed_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,$10,$11,$12,$13)`,
		command.projectID, actor, command.key, command.requestID, command.previewID,
		beforeRevision, after, string(raw), outcome, rejection, retainedID, retainedVersion, committedAt); err != nil {
		return unavailable()
	}
	return nil
}

// receiptFromOperation rebuilds the stored receipt from one operation row.
func receiptFromOperation(applicationID, outcome, rejectionCode string, projectRevisionAfter *string, raw []byte, retainedID, retainedVersion *string, committedAt time.Time) (ApplicationReceipt, error) {
	var snapshot committedCandidateJSON
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return nil, unavailable()
	}
	candidate := projects.ModelBinding{
		Profile:          projects.ProfileVersionRef{ID: snapshot.ProfileID, Version: snapshot.ProfileVersion},
		ProviderID:       snapshot.ProviderID,
		ProviderRevision: snapshot.ProviderRevision,
		ModelRowID:       snapshot.ModelRowID,
	}
	switch outcome {
	case "committed":
		var after string
		if projectRevisionAfter != nil {
			after = *projectRevisionAfter
		}
		retained := projects.ProfileVersionRef{}
		if retainedID != nil {
			retained.ID = *retainedID
		}
		if retainedVersion != nil {
			retained.Version = *retainedVersion
		}
		return Applied{
			ApplicationID:         applicationID,
			ProjectRevision:       after,
			ConfigurationRevision: projects.ConfigurationRevision(snapshot.ConfigurationRevision),
			Candidate:             candidate,
			RetainedExecution:     retained,
			CommittedAt:           committedAt,
		}, nil
	case "rejected":
		return ApplicationRejected{
			ApplicationID: applicationID,
			Error:         APIError{Code: rejectionCode, Message: rejectionCode},
			DecidedAt:     committedAt,
		}, nil
	}
	return nil, unavailable()
}

// replayApplication returns the stored receipt for an already-used command
// key. The original receipt replays even after maintenance removal; it takes
// priority over any preview expiry.
func (s *ApplicationService) replayApplication(ctx context.Context, tx pgx.Tx, actor string, command ApplicationCommand) (ApplicationReceipt, bool, error) {
	var outcome, rejectionCode string
	var projectRevisionAfter *string
	var raw []byte
	var retainedID, retainedVersion *string
	var committedAt time.Time
	err := tx.QueryRow(ctx, `SELECT outcome,rejection_code,project_revision_after,candidate_snapshot,
			retained_execution_profile_id,retained_execution_profile_version,committed_at
		FROM repomesh_models.application_operations
		WHERE project_id=$1 AND actor=$2 AND application_id=$3 FOR UPDATE`,
		command.projectID, actor, command.key).Scan(
		&outcome, &rejectionCode, &projectRevisionAfter, &raw, &retainedID, &retainedVersion, &committedAt)
	if errorsIsNoRows(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, unavailable()
	}
	receipt, err := receiptFromOperation(command.key, outcome, rejectionCode, projectRevisionAfter, raw, retainedID, retainedVersion, committedAt)
	if err != nil {
		return nil, false, err
	}
	return receipt, true, nil
}

// Get returns the stored receipt of one application operation. The original
// receipt replays even after maintenance removal.
func (s *ApplicationService) Get(ctx context.Context, principal access.ProjectPrincipal, projectID, applicationID string) (ApplicationReceipt, error) {
	projectID = normalizeKey(projectID)
	if !validUUID(projectID) {
		return nil, validation("projectId")
	}
	applicationID = normalizeKey(applicationID)
	if len(applicationID) < 1 || len(applicationID) > 128 {
		return nil, validation("applicationId")
	}
	actor := principal.ActorID()
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, unavailable()
	}
	defer rollback(tx)
	if err = s.authorization.LockProjectPrincipal(ctx, tx, principal); err != nil {
		return nil, err
	}
	var outcome, rejectionCode string
	var projectRevisionAfter *string
	var raw []byte
	var retainedID, retainedVersion *string
	var committedAt time.Time
	err = tx.QueryRow(ctx, `SELECT outcome,rejection_code,project_revision_after,candidate_snapshot,
			retained_execution_profile_id,retained_execution_profile_version,committed_at
		FROM repomesh_models.application_operations
		WHERE project_id=$1 AND actor=$2 AND application_id=$3`,
		projectID, actor, applicationID).Scan(
		&outcome, &rejectionCode, &projectRevisionAfter, &raw, &retainedID, &retainedVersion, &committedAt)
	if errorsIsNoRows(err) {
		return nil, failure(404, "APPLICATION_NOT_FOUND")
	}
	if err != nil {
		return nil, unavailable()
	}
	receipt, err := receiptFromOperation(applicationID, outcome, rejectionCode, projectRevisionAfter, raw, retainedID, retainedVersion, committedAt)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, unavailable()
	}
	return receipt, nil
}

// RemoveApplicationResult marks one application operation removed. The
// receipt itself is immutable; removal only stamps removed_at.
func (m *Maintenance) RemoveApplicationResult(ctx context.Context, actor, projectID, applicationID string) error {
	projectID = normalizeKey(projectID)
	if !validUUID(projectID) {
		return validation("projectId")
	}
	applicationID = normalizeKey(applicationID)
	if len(applicationID) < 1 || len(applicationID) > 128 {
		return validation("applicationId")
	}
	tx, err := m.service.beginWrite(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	tag, err := tx.Exec(ctx, `UPDATE repomesh_models.application_operations SET removed_at=now()
		WHERE project_id=$1 AND actor=$2 AND application_id=$3 AND removed_at IS NULL`,
		projectID, actor, applicationID)
	if err != nil {
		return unavailable()
	}
	if tag.RowsAffected() == 0 {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM repomesh_models.application_operations
			WHERE project_id=$1 AND actor=$2 AND application_id=$3)`, projectID, actor, applicationID).Scan(&exists); err != nil {
			return unavailable()
		}
		if !exists {
			return failure(404, "APPLICATION_NOT_FOUND")
		}
	}
	return tx.Commit(ctx)
}

// RemoveTestResult marks one registered test removed once it is no longer
// recent. The result identity is immutable; removal only stamps removed_at
// and clears the outstanding pointer.
func (m *Maintenance) RemoveTestResult(ctx context.Context, actor, testID string, cutoff time.Time) error {
	testID = normalizeKey(testID)
	if len(testID) < 1 || len(testID) > 128 {
		return validation("testId")
	}
	tx, err := m.service.beginWrite(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	tag, err := tx.Exec(ctx, `UPDATE repomesh_models.tests SET removed_at=now()
		WHERE actor=$1 AND test_id=$2 AND removed_at IS NULL AND accepted_at<=$3`,
		actor, testID, cutoff)
	if err != nil {
		return unavailable()
	}
	if tag.RowsAffected() == 0 {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM repomesh_models.tests
			WHERE actor=$1 AND test_id=$2)`, actor, testID).Scan(&exists); err != nil {
			return unavailable()
		}
		if !exists {
			return failure(404, "MODEL_TEST_NOT_FOUND")
		}
		return tx.Commit(ctx)
	}
	if _, err = tx.Exec(ctx, `DELETE FROM repomesh_models.actor_outstanding_tests
		WHERE actor=$1 AND test_id=$2`, actor, testID); err != nil {
		return unavailable()
	}
	return tx.Commit(ctx)
}
