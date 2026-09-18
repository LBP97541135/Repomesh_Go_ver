package models

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/projects"
	"repomesh.local/repomesh/internal/secrets"
)

func (s *Service) List(ctx context.Context, principal access.ProjectPrincipal, query ListQuery) (ProviderPage, error) {
	tx, err := s.beginWrite(ctx)
	if err != nil {
		return ProviderPage{}, err
	}
	defer rollback(tx)
	if err = s.authorization.LockProjectPrincipal(ctx, tx, principal); err != nil {
		return ProviderPage{}, err
	}
	after := ""
	if query.Cursor != "" {
		var storedActor, storedQuery string
		var storedLimit int
		var expires time.Time
		err = tx.QueryRow(ctx, `SELECT actor,query,page_limit,expires_at FROM repomesh_projects.cursors WHERE id=$1 AND kind='providers'`, query.Cursor).Scan(&storedActor, &storedQuery, &storedLimit, &expires)
		if errors.Is(err, pgx.ErrNoRows) || storedActor != principal.ActorID() || storedQuery != query.Text || storedLimit != query.Limit {
			return ProviderPage{}, failure(400, "INVALID_CURSOR")
		}
		if err != nil {
			return ProviderPage{}, unavailable()
		}
		if !expires.After(time.Now()) {
			return ProviderPage{}, failure(409, "CURSOR_EXPIRED")
		}
		if err = tx.QueryRow(ctx, `SELECT after_id FROM repomesh_projects.cursors WHERE id=$1`, query.Cursor).Scan(&after); err != nil {
			return ProviderPage{}, unavailable()
		}
	}
	rows, err := tx.Query(ctx, `SELECT p.id,r.name,p.head_revision,
		(SELECT count(*) FROM repomesh_models.model_snapshots s WHERE s.provider_id=p.id AND s.provider_revision=p.head_revision)
		FROM repomesh_models.providers p
		JOIN repomesh_models.provider_revisions r ON r.provider_id=p.id AND r.revision=p.head_revision
		WHERE p.owner=$1 AND ($2='' OR strpos(lower(r.name), lower($2))>0) AND ($3='' OR p.id>$3)
		ORDER BY p.id LIMIT $4`, principal.ActorID(), query.Text, after, query.Limit+1)
	if err != nil {
		return ProviderPage{}, unavailable()
	}
	defer rows.Close()
	var result ProviderPage
	for rows.Next() {
		var item ProviderSummary
		if err := rows.Scan(&item.ID, &item.Name, &item.Revision, &item.ModelCount); err != nil {
			return ProviderPage{}, unavailable()
		}
		result.Items = append(result.Items, item)
	}
	if rows.Err() != nil {
		return ProviderPage{}, unavailable()
	}
	if result.Items == nil {
		result.Items = []ProviderSummary{}
	}
	if len(result.Items) > query.Limit {
		result.Items = result.Items[:query.Limit]
		cursor := newID()
		if _, err = tx.Exec(ctx, `INSERT INTO repomesh_projects.cursors(id,actor,kind,query,page_limit,after_id,expires_at)
			VALUES ($1,$2,'providers',$3,$4,$5,now()+interval '10 minutes')`, cursor, principal.ActorID(), query.Text, query.Limit, result.Items[len(result.Items)-1].ID); err != nil {
			return ProviderPage{}, unavailable()
		}
		result.NextCursor = &cursor
	}
	if err = tx.Commit(ctx); err != nil {
		return ProviderPage{}, unavailable()
	}
	return result, nil
}

func (s *Service) Get(ctx context.Context, principal access.ProjectPrincipal, providerID string) (ProviderView, error) {
	return s.getRevision(ctx, principal, providerID, nil)
}

func (s *Service) GetVersion(ctx context.Context, principal access.ProjectPrincipal, providerID, revision string) (ProviderView, error) {
	if !validUUID(providerID) || !validUUID(revision) {
		return ProviderView{}, failure(404, "RESOURCE_NOT_FOUND")
	}
	return s.getRevision(ctx, principal, providerID, &revision)
}

func (s *Service) getRevision(ctx context.Context, principal access.ProjectPrincipal, providerID string, revision *string) (ProviderView, error) {
	if !validUUID(providerID) {
		return ProviderView{}, failure(404, "RESOURCE_NOT_FOUND")
	}
	tx, err := s.beginWrite(ctx)
	if err != nil {
		return ProviderView{}, err
	}
	defer rollback(tx)
	if err = s.authorization.LockProjectPrincipal(ctx, tx, principal); err != nil {
		return ProviderView{}, err
	}
	view, err := s.readProvider(ctx, tx, principal.ActorID(), providerID, revision)
	if err != nil {
		return ProviderView{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return ProviderView{}, unavailable()
	}
	return view, nil
}

func (s *Service) Save(ctx context.Context, principal access.ProjectPrincipal, command SaveCommand) (SaveResult, error) {
	defer command.Clear()
	tx, err := s.beginWrite(ctx)
	if err != nil {
		return SaveResult{}, err
	}
	if err = s.authorization.LockProjectPrincipal(ctx, tx, principal); err != nil {
		rollback(tx)
		return SaveResult{}, err
	}
	operation, found, err := readOperation(ctx, tx, principal.ActorID(), command.key)
	if err != nil {
		rollback(tx)
		return SaveResult{}, err
	}
	if found {
		result, replayErr := s.replaySave(ctx, tx, operation, command.body)
		rollback(tx)
		return result, replayErr
	}
	rollback(tx)

	input, canonical, err := parseSave(command.body, 1)
	if err != nil {
		return s.replayAfterPreflight(ctx, principal, command, err)
	}
	providerID := newID()
	if input.providerID != nil {
		providerID = *input.providerID
	}
	prepared, err := s.prepareSave(ctx, principal.ActorID(), command.key, providerID, canonical, input.replace)
	if err != nil {
		return s.replayAfterPreflight(ctx, principal, command, err)
	}
	defer prepared.clear()
	if err = s.phase(ctx, secretPrepared); err != nil {
		return s.replayAfterPreflight(ctx, principal, command, err)
	}
	return s.commitSave(ctx, principal, command, input, canonical, prepared)
}

func (s *Service) prepareSave(ctx context.Context, actor, saveID, providerID string, input canonicalInput, prepareBusiness bool) (savePreparation, error) {
	prepared := savePreparation{providerID: providerID}
	encoded, err := encodeProtectedInput(input)
	if err != nil {
		return savePreparation{}, err
	}
	replace, ok := input.(replaceInput)
	if ok {
		prepared.vault, err = s.secrets.Prepare(ctx, secrets.Owner{Kind: "provider-save-input", ID: actor + ":" + saveID}, secrets.OperationInput, encoded)
		if err != nil {
			return savePreparation{}, secretError(err)
		}
		if prepareBusiness {
			prepared.business, err = s.secrets.Prepare(ctx, secrets.Owner{Kind: "model-provider", ID: providerID}, secrets.ModelProviderKey, replace.secret)
			if err != nil {
				prepared.clear()
				return savePreparation{}, secretError(err)
			}
		}
	}
	return prepared, nil
}

func secretError(err error) error {
	if errors.Is(err, secrets.ErrWrapLimit) {
		return failure(503, "RESULT_UNCONFIRMED")
	}
	return unavailable()
}

func (s *Service) commitSave(ctx context.Context, principal access.ProjectPrincipal, command SaveCommand, input saveInput, canonical canonicalInput, prepared savePreparation) (SaveResult, error) {
	tx, err := s.beginWrite(ctx)
	if err != nil {
		return SaveResult{}, err
	}
	defer rollback(tx)
	if err = s.authorization.LockProjectPrincipal(ctx, tx, principal); err != nil {
		return SaveResult{}, err
	}
	if err = s.phase(ctx, principalLocked); err != nil {
		return SaveResult{}, err
	}
	operation, found, err := readOperation(ctx, tx, principal.ActorID(), command.key)
	if err != nil {
		return SaveResult{}, err
	}
	if found {
		return s.replaySave(ctx, tx, operation, command.body)
	}
	if err = s.phase(ctx, slotLocked); err != nil {
		return SaveResult{}, err
	}
	if s.catalog == nil {
		return SaveResult{}, unavailable()
	}
	// Adopted lock order is slot → catalog → Provider. Import already locks
	// owners then catalog; reversing catalog and Provider here would deadlock.
	if err = s.catalog.LockExclusive(ctx, tx); err != nil {
		return SaveResult{}, err
	}
	var at time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at); err != nil {
		return SaveResult{}, unavailable()
	}
	if input.providerID != nil {
		current, lockErr := lockProvider(ctx, tx, principal.ActorID(), *input.providerID)
		if lockErr != nil {
			return SaveResult{}, lockErr
		}
		if current.Revision != *input.expectedRevision {
			receipt := rejectedReceipt(command.key, command.requestID, "PROVIDER_REVISION_CONFLICT", []FieldError{{Field: "expectedRevision", Code: "CONFLICT"}}, at)
			if persistErr := s.finishRejected(ctx, tx, principal.ActorID(), command.key, canonical, prepared, receipt, at); persistErr != nil {
				return SaveResult{}, persistErr
			}
			if err = tx.Commit(ctx); err != nil {
				return SaveResult{}, unavailable()
			}
			return SaveResult{Receipt: receipt, HTTPStatus: 409}, nil
		}
		full, readErr := s.readProvider(ctx, tx, principal.ActorID(), *input.providerID, input.expectedRevision)
		if readErr != nil {
			return SaveResult{}, readErr
		}
		if err = validateRetainedRows(full, input); err != nil {
			return SaveResult{}, err
		}
	}
	committed, err := s.writeProvider(ctx, tx, principal.ActorID(), command.key, input, prepared, at)
	if err != nil {
		return SaveResult{}, err
	}
	receipt := committedReceipt(committed)
	var vault *secrets.VersionID
	if prepared.vault != nil {
		id := prepared.vault.VersionID()
		vault = &id
	}
	if err = persistReceipt(ctx, tx, principal.ActorID(), command.key, &committed.ProviderID, receipt, at, "save_input", "committed", 1, canonicalBytes(canonical), vault); err != nil {
		return SaveResult{}, err
	}
	if err = s.phase(ctx, receiptInserted); err != nil {
		return SaveResult{}, err
	}
	if err = s.phase(ctx, beforeCommit); err != nil {
		return SaveResult{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return SaveResult{}, unavailable()
	}
	status := 200
	if input.providerID == nil {
		status = 201
	}
	return SaveResult{Receipt: receipt, HTTPStatus: status}, nil
}

func canonicalBytes(input canonicalInput) []byte {
	switch value := input.(type) {
	case keepInput:
		return value.canonical
	case replaceInput:
		return value.canonical
	default:
		return nil
	}
}

func (s *Service) finishRejected(ctx context.Context, tx pgx.Tx, actor, key string, canonical canonicalInput, prepared savePreparation, receipt SaveReceipt, at time.Time) error {
	var vault *secrets.VersionID
	if prepared.vault != nil {
		if _, err := s.secrets.InsertPrepared(ctx, tx, prepared.vault); err != nil {
			return secretError(err)
		}
		id := prepared.vault.VersionID()
		vault = &id
	}
	return persistReceipt(ctx, tx, actor, key, nil, receipt, at, "save_input", "rejected", 1, canonicalBytes(canonical), vault)
}

func (s *Service) writeProvider(ctx context.Context, tx pgx.Tx, actor, saveID string, input saveInput, prepared savePreparation, at time.Time) (CommittedSave, error) {
	providerID := prepared.providerID
	revision := newID()
	secretID := secrets.VersionID("")
	if input.replace {
		if prepared.business == nil {
			return CommittedSave{}, unavailable()
		}
		id, err := s.secrets.InsertPrepared(ctx, tx, prepared.business)
		if err != nil {
			return CommittedSave{}, secretError(err)
		}
		secretID = id
	} else {
		var stored string
		if err := tx.QueryRow(ctx, `SELECT secret_version_id FROM repomesh_models.provider_revisions WHERE provider_id=$1 AND revision=$2`, providerID, *input.expectedRevision).Scan(&stored); err != nil {
			return CommittedSave{}, unavailable()
		}
		secretID = secrets.VersionID(stored)
	}
	if prepared.vault != nil {
		if _, err := s.secrets.InsertPrepared(ctx, tx, prepared.vault); err != nil {
			return CommittedSave{}, secretError(err)
		}
	}
	if err := s.phase(ctx, secretsInserted); err != nil {
		return CommittedSave{}, err
	}
	if input.providerID == nil {
		if _, err := tx.Exec(ctx, `INSERT INTO repomesh_models.providers(id,owner,head_revision,enabled) VALUES ($1,$2,$3,true)`, providerID, actor, revision); err != nil {
			return CommittedSave{}, unavailable()
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_models.provider_revisions(provider_id,revision,owner,name,base_url,api_format,secret_version_id,secret_owner_kind,secret_owner_id,secret_purpose,created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,'model-provider',$1,'model-provider-key',$8)`,
		providerID, revision, actor, input.name, input.baseURL, input.apiFormat, string(secretID), at); err != nil {
		return CommittedSave{}, unavailable()
	}
	if err := s.phase(ctx, providerInserted); err != nil {
		return CommittedSave{}, err
	}
	for _, model := range input.models {
		rowID := newID()
		profileID := newID()
		if model.ID != nil {
			rowID = *model.ID
			if err := tx.QueryRow(ctx, `SELECT profile_id FROM repomesh_models.model_rows WHERE provider_id=$1 AND id=$2`, providerID, rowID).Scan(&profileID); err != nil {
				return CommittedSave{}, unavailable()
			}
		}
		if err := s.catalog.RegisterModelVersion(ctx, tx, projects.ModelVersionRegistration{
			Owner: actor, ProfileID: profileID, Name: displayName(model), ParametersComplete: true,
			ProviderID: providerID, ProviderRevision: revision, ModelRowID: rowID, SecretVersion: secretID,
		}); err != nil {
			return CommittedSave{}, err
		}
		if model.ID == nil {
			if _, err := tx.Exec(ctx, `INSERT INTO repomesh_models.model_rows(id,provider_id,owner,profile_id) VALUES ($1,$2,$3,$4)`, rowID, providerID, actor, profileID); err != nil {
				return CommittedSave{}, unavailable()
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO repomesh_models.model_snapshots(provider_id,provider_revision,row_id,model_id,display_name,context_window,max_output_tokens,reasoning,vision)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, providerID, revision, rowID, model.ModelID, displayName(model), model.ContextWindow, model.MaxOutputTokens, model.Reasoning, model.Vision); err != nil {
			return CommittedSave{}, unavailable()
		}
		if _, err := tx.Exec(ctx, `INSERT INTO repomesh_models.profile_links(profile_id,profile_version,provider_id,provider_revision,row_id,owner,secret_version_id)
			VALUES ($1,$2,$3,$2,$4,$5,$6)`, profileID, revision, providerID, rowID, actor, string(secretID)); err != nil {
			return CommittedSave{}, unavailable()
		}
	}
	if err := s.phase(ctx, modelsInserted); err != nil {
		return CommittedSave{}, err
	}
	if err := s.phase(ctx, profilesInserted); err != nil {
		return CommittedSave{}, err
	}
	if input.providerID != nil {
		if _, err := tx.Exec(ctx, `UPDATE repomesh_models.providers SET head_revision=$2,access_epoch=access_epoch+1 WHERE id=$1 AND owner=$3`, providerID, revision, actor); err != nil {
			return CommittedSave{}, unavailable()
		}
	}
	if err := s.phase(ctx, headUpdated); err != nil {
		return CommittedSave{}, err
	}
	return CommittedSave{
		SaveID: saveID, Outcome: "committed", ProviderID: providerID, ProviderRevision: revision,
		SecretVersionID: string(secretID), CommittedAt: at,
		Links: SaveLinks{Provider: providerPath(providerID), Operation: operationPath(saveID)},
	}, nil
}

func (s *Service) replaySave(ctx context.Context, tx pgx.Tx, operation operationRecord, body RawSaveBody) (SaveResult, error) {
	if err := requireOperationOwner(ctx, tx, operation.actor, operation); err != nil {
		return SaveResult{}, err
	}
	if operation.removedAt != nil {
		return SaveResult{}, failure(410, "MODEL_SAVE_RESULT_REMOVED")
	}
	if operation.kind == "save_closed" {
		return SaveResult{}, failure(409, "MODEL_SAVE_CLOSED")
	}
	incoming, canonical, err := parseSave(body, 1)
	if err != nil {
		receipt, readErr := receiptFromRecord(operation)
		if readErr != nil {
			return SaveResult{}, readErr
		}
		return SaveResult{Receipt: receipt, HTTPStatus: 409}, failure(409, "IDEMPOTENCY_CONFLICT")
	}
	_ = incoming
	if err = s.compareInput(ctx, tx, operation, canonical); err != nil {
		receipt, readErr := receiptFromRecord(operation)
		if readErr != nil {
			return SaveResult{}, readErr
		}
		return SaveResult{Receipt: receipt, HTTPStatus: 409}, err
	}
	receipt, err := receiptFromRecord(operation)
	if err != nil {
		return SaveResult{}, err
	}
	status := 200
	if receipt.rejected != nil {
		status = 409
	}
	return SaveResult{Receipt: receipt, HTTPStatus: status}, nil
}

func (s *Service) compareInput(ctx context.Context, tx pgx.Tx, operation operationRecord, input canonicalInput) error {
	encoded, err := encodeProtectedInput(input)
	if err != nil {
		return err
	}
	if operation.inputVersion != nil {
		stored, openErr := s.secrets.OpenInTx(ctx, tx, *operation.inputVersion, secrets.Owner{Kind: "provider-save-input", ID: operation.actor + ":" + operation.key}, secrets.OperationInput)
		if openErr != nil {
			return unavailable()
		}
		defer clear(stored)
		return compareProtectedInput(stored, encoded)
	}
	keep, ok := input.(keepInput)
	if !ok {
		return failure(409, "IDEMPOTENCY_CONFLICT")
	}
	if !bytes.Equal(operation.canonical, keep.canonical) {
		return failure(409, "IDEMPOTENCY_CONFLICT")
	}
	return nil
}

func (s *Service) replayAfterPreflight(ctx context.Context, principal access.ProjectPrincipal, command SaveCommand, preflightErr error) (SaveResult, error) {
	tx, err := s.beginWrite(ctx)
	if err != nil {
		return SaveResult{}, preflightErr
	}
	defer rollback(tx)
	if lockErr := s.authorization.LockProjectPrincipal(ctx, tx, principal); lockErr != nil {
		return SaveResult{}, lockErr
	}
	operation, found, readErr := readOperation(ctx, tx, principal.ActorID(), command.key)
	if readErr == nil && found {
		return s.replaySave(ctx, tx, operation, command.body)
	}
	return SaveResult{}, preflightErr
}

func (s *Service) GetSave(ctx context.Context, principal access.ProjectPrincipal, saveID string) (SaveReceipt, error) {
	saveID = normalizeKey(saveID)
	if !validUUID(saveID) {
		return SaveReceipt{}, failure(404, "MODEL_SAVE_NOT_FOUND")
	}
	tx, err := s.beginWrite(ctx)
	if err != nil {
		return SaveReceipt{}, err
	}
	defer rollback(tx)
	if err = s.authorization.LockProjectPrincipal(ctx, tx, principal); err != nil {
		return SaveReceipt{}, err
	}
	operation, found, err := readOperation(ctx, tx, principal.ActorID(), saveID)
	if err != nil {
		return SaveReceipt{}, err
	}
	if !found {
		return SaveReceipt{}, failure(404, "MODEL_SAVE_NOT_FOUND")
	}
	if err = requireOperationOwner(ctx, tx, principal.ActorID(), operation); err != nil {
		return SaveReceipt{}, err
	}
	receipt, err := receiptFromRecord(operation)
	if err != nil {
		return SaveReceipt{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return SaveReceipt{}, unavailable()
	}
	return receipt, nil
}

func (s *Service) CloseSave(ctx context.Context, principal access.ProjectPrincipal, command CloseCommand) (SaveReceipt, error) {
	tx, err := s.beginWrite(ctx)
	if err != nil {
		return SaveReceipt{}, err
	}
	defer rollback(tx)
	if err = s.authorization.LockProjectPrincipal(ctx, tx, principal); err != nil {
		return SaveReceipt{}, err
	}
	if err = s.phase(ctx, principalLocked); err != nil {
		return SaveReceipt{}, err
	}
	operation, found, err := readOperation(ctx, tx, principal.ActorID(), command.key)
	if err != nil {
		return SaveReceipt{}, err
	}
	if found {
		if err = requireOperationOwner(ctx, tx, principal.ActorID(), operation); err != nil {
			return SaveReceipt{}, err
		}
		receipt, readErr := receiptFromRecord(operation)
		if readErr != nil {
			return SaveReceipt{}, readErr
		}
		if err = tx.Commit(ctx); err != nil {
			return SaveReceipt{}, unavailable()
		}
		return receipt, nil
	}
	var at time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at); err != nil {
		return SaveReceipt{}, unavailable()
	}
	receipt, err := insertClosed(ctx, tx, principal.ActorID(), command.key, at)
	if err != nil {
		return SaveReceipt{}, err
	}
	if err = s.phase(ctx, closedInserted); err != nil {
		return SaveReceipt{}, err
	}
	if err = s.phase(ctx, beforeCommit); err != nil {
		return SaveReceipt{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return SaveReceipt{}, unavailable()
	}
	return receipt, nil
}

func (s *Service) ResolveDestination(ctx context.Context, actor, saveID string) (*string, error) {
	saveID = normalizeKey(saveID)
	if !validUUID(saveID) {
		return nil, failure(404, "RESOURCE_NOT_FOUND")
	}
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM repomesh_models.save_operations WHERE actor=$1 AND save_id=$2)`, actor, saveID).Scan(&exists); err != nil {
		return nil, unavailable()
	}
	if !exists {
		return nil, failure(404, "RESOURCE_NOT_FOUND")
	}
	path := "/settings/model-saves/" + saveID
	return &path, nil
}

func (m *Maintenance) RemoveSaveResult(ctx context.Context, actor, saveID string) error {
	saveID = normalizeKey(saveID)
	tx, err := m.service.beginWrite(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	operation, found, err := readOperation(ctx, tx, actor, saveID)
	if err != nil {
		return err
	}
	if !found {
		return failure(404, "MODEL_SAVE_NOT_FOUND")
	}
	if operation.removedAt != nil {
		return tx.Commit(ctx)
	}
	if operation.kind != "save_input" {
		return unavailable()
	}
	if operation.inputVersion != nil {
		if err = m.service.secrets.DestroyInTx(ctx, tx, *operation.inputVersion, secrets.Owner{Kind: "provider-save-input", ID: actor + ":" + saveID}, secrets.OperationInput); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE repomesh_models.save_operations SET receipt=NULL,canonical_nonsecret=NULL,input_vault_version=NULL,removed_at=now()
		WHERE actor=$1 AND save_id=$2`, actor, saveID); err != nil {
		return unavailable()
	}
	return tx.Commit(ctx)
}

func normalizeKey(value string) string {
	return strings.ToLower(value)
}
