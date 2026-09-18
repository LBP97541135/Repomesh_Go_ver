package models

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"repomesh.local/repomesh/internal/secrets"
)

func (s *Service) beginWrite(ctx context.Context) (pgx.Tx, error) {
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

func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}

func (s *Service) phase(ctx context.Context, phase transactionPhase) error {
	if s.hook == nil {
		return nil
	}
	return s.hook(ctx, phase)
}

func readOperation(ctx context.Context, tx pgx.Tx, actor, key string) (operationRecord, bool, error) {
	var record operationRecord
	record.actor, record.key = actor, key
	var vault *string
	var schema *int
	err := tx.QueryRow(ctx, `SELECT kind,outcome,target,schema_version,canonical_nonsecret,input_vault_version,receipt,removed_at
		FROM repomesh_models.save_operations WHERE actor=$1 AND save_id=$2 FOR UPDATE`, actor, key).Scan(
		&record.kind, &record.outcome, &record.target, &schema, &record.canonical, &vault, &record.receipt, &record.removedAt)
	if schema != nil {
		record.schemaVersion = *schema
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return operationRecord{}, false, nil
	}
	if err != nil {
		return operationRecord{}, false, unavailable()
	}
	if vault != nil {
		id := secrets.VersionID(*vault)
		record.inputVersion = &id
	}
	return record, true, nil
}

func requireOperationOwner(_ context.Context, _ pgx.Tx, actor string, operation operationRecord) error {
	if operation.actor != actor {
		return failure(404, "RESOURCE_NOT_FOUND")
	}
	return nil
}

func persistReceipt(ctx context.Context, tx pgx.Tx, actor, key string, target *string, receipt SaveReceipt, at time.Time, kind, outcome string, schema int, canonical []byte, vault *secrets.VersionID) error {
	body, err := json.Marshal(receipt)
	if err != nil {
		return unavailable()
	}
	var vaultID *string
	if vault != nil {
		value := string(*vault)
		vaultID = &value
	}
	_, err = tx.Exec(ctx, `INSERT INTO repomesh_models.save_operations(actor,save_id,kind,outcome,target,schema_version,canonical_nonsecret,input_vault_version,receipt)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, actor, key, kind, outcome, target, nullableInt(schema), canonical, vaultID, body)
	if err != nil {
		return unavailable()
	}
	return nil
}

func insertClosed(ctx context.Context, tx pgx.Tx, actor, key string, at time.Time) (SaveReceipt, error) {
	receipt := closedReceipt(key, at)
	body, err := json.Marshal(receipt)
	if err != nil {
		return SaveReceipt{}, unavailable()
	}
	_, err = tx.Exec(ctx, `INSERT INTO repomesh_models.save_operations(actor,save_id,kind,outcome,receipt)
		VALUES ($1,$2,'save_closed','closed_without_save',$3)`, actor, key, body)
	if err != nil {
		return SaveReceipt{}, unavailable()
	}
	return receipt, nil
}

func nullableInt(value int) *int {
	if value == 0 {
		return nil
	}
	return &value
}

func rejectedReceipt(key, requestID, code string, fields []FieldError, at time.Time) SaveReceipt {
	if fields == nil {
		fields = []FieldError{}
	}
	return SaveReceipt{rejected: &RejectedSave{
		SaveID: key, Outcome: "rejected", DecidedAt: at,
		Error: APIError{Code: code, Message: "The request could not be completed.", FieldErrors: fields, RequestID: requestID},
	}}
}

func committedReceipt(value CommittedSave) SaveReceipt {
	value.Outcome = "committed"
	return SaveReceipt{committed: &value}
}

func closedReceipt(key string, at time.Time) SaveReceipt {
	return SaveReceipt{closed: &ClosedSave{
		SaveID: key, Outcome: "closed_without_save", ClosedAt: at,
		Links: OperationLinks{Operation: operationPath(key)},
	}}
}

func receiptFromRecord(record operationRecord) (SaveReceipt, error) {
	if record.removedAt != nil {
		return SaveReceipt{}, failure(410, "MODEL_SAVE_RESULT_REMOVED")
	}
	var receipt SaveReceipt
	if json.Unmarshal(record.receipt, &receipt) == nil && (receipt.committed != nil || receipt.rejected != nil || receipt.closed != nil) {
		return receipt, nil
	}
	var closed ClosedSave
	if json.Unmarshal(record.receipt, &closed) == nil && closed.Outcome == "closed_without_save" {
		return SaveReceipt{closed: &closed}, nil
	}
	var committed CommittedSave
	if json.Unmarshal(record.receipt, &committed) == nil && committed.Outcome == "committed" {
		return SaveReceipt{committed: &committed}, nil
	}
	var rejected RejectedSave
	if json.Unmarshal(record.receipt, &rejected) == nil && rejected.Outcome == "rejected" {
		return SaveReceipt{rejected: &rejected}, nil
	}
	return SaveReceipt{}, unavailable()
}

func (r *SaveReceipt) UnmarshalJSON(data []byte) error {
	var probe map[string]json.RawMessage
	if json.Unmarshal(data, &probe) != nil {
		return unavailable()
	}
	var outcome string
	_ = json.Unmarshal(probe["outcome"], &outcome)
	switch outcome {
	case "committed":
		var value CommittedSave
		if json.Unmarshal(data, &value) != nil {
			return unavailable()
		}
		r.committed = &value
	case "rejected":
		var value RejectedSave
		if json.Unmarshal(data, &value) != nil {
			return unavailable()
		}
		r.rejected = &value
	case "closed_without_save":
		var value ClosedSave
		if json.Unmarshal(data, &value) != nil {
			return unavailable()
		}
		r.closed = &value
	default:
		return unavailable()
	}
	return nil
}

func lockProvider(ctx context.Context, tx pgx.Tx, actor, providerID string) (ProviderView, error) {
	var view ProviderView
	err := tx.QueryRow(ctx, `SELECT id,head_revision FROM repomesh_models.providers WHERE id=$1 AND owner=$2 FOR UPDATE`, providerID, actor).Scan(&view.ID, &view.Revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProviderView{}, failure(404, "RESOURCE_NOT_FOUND")
	}
	if err != nil {
		return ProviderView{}, unavailable()
	}
	return view, nil
}

func (s *Service) readProvider(ctx context.Context, tx pgx.Tx, actor, providerID string, revision *string) (ProviderView, error) {
	var view ProviderView
	var secretID string
	query := `SELECT p.id,r.name,r.revision,r.base_url,r.api_format,r.secret_version_id,r.created_at
		FROM repomesh_models.providers p
		JOIN repomesh_models.provider_revisions r ON r.provider_id=p.id AND r.revision=COALESCE($3::text,p.head_revision)
		WHERE p.id=$1 AND p.owner=$2`
	err := tx.QueryRow(ctx, query, providerID, actor, revision).Scan(&view.ID, &view.Name, &view.Revision, &view.BaseURL, &view.APIFormat, &secretID, &view.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProviderView{}, failure(404, "RESOURCE_NOT_FOUND")
	}
	if err != nil {
		return ProviderView{}, unavailable()
	}
	view.Secret = SecretView{Configured: true, VersionID: &secretID, Availability: "unknown"}
	inspection, inspectErr := s.secrets.InspectVersion(ctx, tx, secrets.VersionID(secretID), secrets.Owner{Kind: "model-provider", ID: providerID}, secrets.ModelProviderKey)
	if inspectErr != nil {
		return ProviderView{}, unavailable()
	}
	switch {
	case !inspection.Found:
		view.Secret.Availability = "missing"
	case inspection.Destroyed || !inspection.Enabled:
		view.Secret.Availability = "unavailable"
	case !inspection.RootAvailable:
		view.Secret.Availability = "unknown"
	default:
		view.Secret.Availability = "available"
	}
	rows, err := tx.Query(ctx, `SELECT s.row_id,m.profile_id,s.model_id,s.display_name,s.context_window,s.max_output_tokens,s.reasoning,s.vision
		FROM repomesh_models.model_snapshots s
		JOIN repomesh_models.model_rows m ON m.provider_id=s.provider_id AND m.id=s.row_id
		WHERE s.provider_id=$1 AND s.provider_revision=$2 ORDER BY s.row_id`, providerID, view.Revision)
	if err != nil {
		return ProviderView{}, unavailable()
	}
	defer rows.Close()
	for rows.Next() {
		var model ModelView
		if err := rows.Scan(&model.ID, &model.ModelProfileID, &model.ModelID, &model.DisplayName, &model.ContextWindow, &model.MaxOutputTokens, &model.Reasoning, &model.Vision); err != nil {
			return ProviderView{}, unavailable()
		}
		view.Models = append(view.Models, model)
	}
	if rows.Err() != nil || len(view.Models) == 0 {
		return ProviderView{}, unavailable()
	}
	return view, nil
}

func validateRetainedRows(current ProviderView, input saveInput) error {
	known := map[string]bool{}
	for _, model := range current.Models {
		known[model.ID] = true
	}
	seen := map[string]bool{}
	for _, model := range input.models {
		if model.ID == nil {
			continue
		}
		if !known[*model.ID] {
			return validation("models.id")
		}
		seen[*model.ID] = true
	}
	for id := range known {
		if !seen[id] {
			return validation("models.id")
		}
	}
	return nil
}
