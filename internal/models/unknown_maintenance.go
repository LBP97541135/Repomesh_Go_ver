package models

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UnknownMaintenancePrincipal is the coordinator-only identity. It is built
// from the actual DB session_user, the deployment identity, and the
// repomesh_unknown_maintainer role membership; every method rechecks all
// three inside its transaction.
type UnknownMaintenancePrincipal struct {
	deploymentID string
	sessionUser  string
}

// AuthenticateUnknownMaintenance verifies the coordinator DB session actually
// holds the maintainer role and reads the stable deployment identity.
func AuthenticateUnknownMaintenance(ctx context.Context, pool *pgxpool.Pool) (UnknownMaintenancePrincipal, error) {
	if pool == nil {
		return UnknownMaintenancePrincipal{}, unavailable()
	}
	var sessionUser, currentUser string
	var permitted bool
	if err := pool.QueryRow(ctx, `SELECT session_user, current_user,
			pg_has_role(current_user, 'repomesh_unknown_maintainer', 'MEMBER')`).
		Scan(&sessionUser, &currentUser, &permitted); err != nil {
		return UnknownMaintenancePrincipal{}, unavailable()
	}
	if !permitted || sessionUser == "" || currentUser != sessionUser {
		return UnknownMaintenancePrincipal{}, failure(2, "MAINTENANCE_FORBIDDEN")
	}
	var deploymentID string
	if err := pool.QueryRow(ctx, `SELECT deployment_id FROM repomesh_sources.deployment WHERE singleton`).Scan(&deploymentID); err != nil {
		return UnknownMaintenancePrincipal{}, unavailable()
	}
	return UnknownMaintenancePrincipal{deploymentID: deploymentID, sessionUser: sessionUser}, nil
}

// UnknownMaintenance is the coordinator-side inspector and closer for tests
// left in the unknown state by a send whose outcome could not be confirmed.
type UnknownMaintenance struct {
	pool      *pgxpool.Pool
	principal UnknownMaintenancePrincipal
}

// NewUnknownMaintenance refuses to assemble without an authenticated principal.
func NewUnknownMaintenance(pool *pgxpool.Pool, principal UnknownMaintenancePrincipal) (*UnknownMaintenance, error) {
	if pool == nil || principal.deploymentID == "" || principal.sessionUser == "" {
		return nil, unavailable()
	}
	return &UnknownMaintenance{pool: pool, principal: principal}, nil
}

// checkPrincipal rechecks the actual DB session_user, deployment identity and
// maintainer role in tx. It returns the operator principal string recorded in
// receipts and audit lines.
func (m *UnknownMaintenance) checkPrincipal(ctx context.Context, tx pgx.Tx) (string, error) {
	var sessionUser string
	var permitted bool
	if err := tx.QueryRow(ctx, `SELECT session_user,
			pg_has_role(current_user, 'repomesh_unknown_maintainer', 'MEMBER')`).
		Scan(&sessionUser, &permitted); err != nil || !permitted || sessionUser != m.principal.sessionUser {
		return "", failure(2, "MAINTENANCE_FORBIDDEN")
	}
	var stored string
	if err := tx.QueryRow(ctx, `SELECT deployment_id FROM repomesh_sources.deployment WHERE singleton`).Scan(&stored); err != nil || stored != m.principal.deploymentID {
		return "", unavailable()
	}
	return m.principal.deploymentID + ":" + m.principal.sessionUser, nil
}

// EvidenceMaterial is one out-of-band artifact reference plus its SHA-256.
type EvidenceMaterial struct {
	Ref       string    `json:"ref"`
	SHA256    string    `json:"sha256"`
	CheckedAt time.Time `json:"checkedAt"`
}

// UnknownCloseEvidence carries both required out-of-band materials and the
// immutable dispatch bindings the closure must match exactly.
type UnknownCloseEvidence struct {
	SchemaVersion        int                     `json:"schemaVersion"`
	ActorID              string                  `json:"actorId"`
	TestID               string                  `json:"testId"`
	ExternalOperationID  string                  `json:"externalOperationId"`
	SendPermitID         string                  `json:"sendPermitId"`
	SenderInstanceID     string                  `json:"senderInstanceId"`
	Credential           CredentialCapabilityRef `json:"credential"`
	SenderExit           EvidenceMaterial        `json:"senderExit"`
	CapabilityRevocation EvidenceMaterial        `json:"capabilityRevocation"`
}

// UnknownCloseCommand is the parsed close request keyed by closeKey.
type UnknownCloseCommand struct {
	ActorID  string
	TestID   string
	CloseKey string
	Evidence UnknownCloseEvidence
}

// ParseUnknownCloseCommand parses the canonical JSON close payload. Unknown
// fields are refused so the digest binds exactly the fields below.
func ParseUnknownCloseCommand(actorID, testID, closeKey string, data []byte) (UnknownCloseCommand, error) {
	command := UnknownCloseCommand{ActorID: normalizeKey(actorID), TestID: normalizeKey(testID), CloseKey: normalizeKey(closeKey)}
	if command.ActorID == "" || command.TestID == "" || command.CloseKey == "" {
		return command, validation("key")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&command.Evidence); err != nil {
		return command, validation("evidence")
	}
	if command.Evidence.SchemaVersion != 1 {
		return command, validation("schemaVersion")
	}
	if command.Evidence.ActorID != command.ActorID || command.Evidence.TestID != command.TestID {
		return command, validation("binding")
	}
	if command.Evidence.SenderExit.Ref == "" || command.Evidence.CapabilityRevocation.Ref == "" ||
		!isSHA256(command.Evidence.SenderExit.SHA256) || !isSHA256(command.Evidence.CapabilityRevocation.SHA256) {
		return command, validation("evidence")
	}
	return command, nil
}

func isSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// UnknownCloseReceipt is the stable result of one close operation. Replays
// with the same close key return the same receipt fields.
type UnknownCloseReceipt struct {
	ActorID           string
	TestID            string
	CloseKey          string
	Outcome           string
	OperatorPrincipal string
	ClosedAt          time.Time
}

// UnknownInspection is the read-only projection for the close decision. The
// budget status is a coarse label, not a counter readout.
type UnknownInspection struct {
	ActorID             string
	TestID              string
	ExternalOperationID string
	SendPermitID        string
	SenderInstanceID    string
	Credential          CredentialCapabilityRef
	State               string
	Recovery            string
	BudgetStatus        string
	Closure             *UnknownCloseReceipt
}

// InspectUnknownTest reads the exact immutable bindings and current state of
// one possibly-unknown test. A 404 means no such test for this actor.
func (m *UnknownMaintenance) InspectUnknownTest(ctx context.Context, actorID, testID string) (UnknownInspection, error) {
	tx, err := m.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return UnknownInspection{}, unavailable()
	}
	defer rollback(tx)
	if _, err = m.checkPrincipal(ctx, tx); err != nil {
		return UnknownInspection{}, err
	}
	actorID, testID = normalizeKey(actorID), normalizeKey(testID)
	var inspection UnknownInspection
	var closureKey, closureOperator *string
	var closedAt *time.Time
	err = tx.QueryRow(ctx, `SELECT t.external_operation_id,t.send_permit_id,t.sender_instance_id,
			t.credential_capability_id,t.credential_capability_version,t.state,t.recovery,r.state,
			c.close_key,c.operator_principal,c.closed_at
		FROM repomesh_models.tests t
		JOIN repomesh_models.test_dispatch d ON d.actor=t.actor AND d.test_id=t.test_id
		LEFT JOIN repomesh_modelbudget.test_reservations r ON r.actor=t.actor AND r.test_id=t.test_id
		LEFT JOIN repomesh_models.unknown_test_closures c ON c.actor=t.actor AND c.test_id=t.test_id
		WHERE t.actor=$1 AND t.test_id=$2`, actorID, testID).Scan(
		&inspection.ExternalOperationID, &inspection.SendPermitID, &inspection.SenderInstanceID,
		&inspection.Credential.ID, &inspection.Credential.Version, &inspection.State, &inspection.Recovery,
		&inspection.BudgetStatus, &closureKey, &closureOperator, &closedAt)
	if errorsIsNoRows(err) {
		return UnknownInspection{}, failure(404, "TEST_NOT_FOUND")
	}
	if err != nil {
		return UnknownInspection{}, unavailable()
	}
	inspection.ActorID, inspection.TestID = actorID, testID
	if closureKey != nil {
		// unknown_test_closures has no outcome column; a recorded closure is
		// always an out-of-band close, so the receipt outcome is constant.
		closureOutcome := "closed_without_result"
		inspection.Closure = &UnknownCloseReceipt{ActorID: actorID, TestID: testID, CloseKey: *closureKey,
			Outcome: closureOutcome, OperatorPrincipal: *closureOperator, ClosedAt: *closedAt}
	}
	return inspection, tx.Commit(ctx)
}

// ValidateUnknownClose compares every immutable binding and both material
// digests against the stored dispatch. It proves nothing about the operator's
// physical-exit attestation; that is out of band by design. Timeout, lease
// expiry, PID absence and restart are not valid evidence here.
func (m *UnknownMaintenance) ValidateUnknownClose(ctx context.Context, command UnknownCloseCommand) error {
	tx, err := m.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return unavailable()
	}
	defer rollback(tx)
	if _, err = m.checkPrincipal(ctx, tx); err != nil {
		return err
	}
	var externalOperationID, sendPermitID, senderInstanceID, credentialID, credentialVersion string
	err = tx.QueryRow(ctx, `SELECT external_operation_id,send_permit_id,sender_instance_id,
			credential_capability_id,credential_capability_version
		FROM repomesh_models.test_dispatch WHERE actor=$1 AND test_id=$2`,
		command.ActorID, command.TestID).Scan(
		&externalOperationID, &sendPermitID, &senderInstanceID, &credentialID, &credentialVersion)
	if errorsIsNoRows(err) {
		return failure(404, "TEST_NOT_FOUND")
	}
	if err != nil {
		return unavailable()
	}
	e := command.Evidence
	if externalOperationID != e.ExternalOperationID || sendPermitID != e.SendPermitID ||
		senderInstanceID != e.SenderInstanceID || credentialID != e.Credential.ID ||
		credentialVersion != e.Credential.Version {
		return failure(409, "CLOSE_BINDING_MISMATCH")
	}
	if err = verifyMaterial(e.SenderExit); err != nil {
		return err
	}
	if err = verifyMaterial(e.CapabilityRevocation); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func verifyMaterial(material EvidenceMaterial) error {
	digest := sha256.Sum256([]byte(material.Ref))
	expected := hex.EncodeToString(digest[:])
	if subtle.ConstantTimeCompare([]byte(expected), []byte(strings.ToLower(material.SHA256))) != 1 {
		return failure(409, "CLOSE_EVIDENCE_DIGEST_MISMATCH")
	}
	return nil
}

// CloseUnknownTest records the closure with both materials, revokes the local
// send capability on the dispatch, clears the actor outstanding pointer and
// appends audit, all in one transaction. The test keeps state unknown and the
// budget reservation stays consumed. Replays with the same close key and the
// same input return the original receipt.
func (m *UnknownMaintenance) CloseUnknownTest(ctx context.Context, command UnknownCloseCommand) (UnknownCloseReceipt, error) {
	tx, err := m.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return UnknownCloseReceipt{}, unavailable()
	}
	defer rollback(tx)
	operator, err := m.checkPrincipal(ctx, tx)
	if err != nil {
		return UnknownCloseReceipt{}, err
	}
	var existingOperator string
	var existingClosedAt time.Time
	err = tx.QueryRow(ctx, `SELECT operator_principal,closed_at FROM repomesh_models.unknown_test_closures
		WHERE deployment_identity=$1 AND close_key=$2 AND actor=$3 AND test_id=$4`,
		m.principal.deploymentID, command.CloseKey, command.ActorID, command.TestID).
		Scan(&existingOperator, &existingClosedAt)
	if err == nil {
		return UnknownCloseReceipt{ActorID: command.ActorID, TestID: command.TestID, CloseKey: command.CloseKey,
			Outcome: "closed_without_result", OperatorPrincipal: existingOperator, ClosedAt: existingClosedAt}, tx.Commit(ctx)
	}
	if !errorsIsNoRows(err) {
		return UnknownCloseReceipt{}, unavailable()
	}
	if err = m.ValidateUnknownCloseInTx(ctx, tx, command); err != nil {
		return UnknownCloseReceipt{}, err
	}
	closedAt := time.Now().UTC()
	e := command.Evidence
	if _, err = tx.Exec(ctx, `INSERT INTO repomesh_models.unknown_test_closures
		(deployment_identity,close_key,actor,test_id,external_operation_id,send_permit_id,sender_instance_id,
		 credential_capability_id,credential_capability_version,schema_version,sender_exit_ref,sender_exit_digest,
		 capability_revocation_ref,capability_revocation_digest,operator_principal,closed_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,1,$10,$11,$12,$13,$14,$15)`,
		m.principal.deploymentID, command.CloseKey, command.ActorID, command.TestID,
		e.ExternalOperationID, e.SendPermitID, e.SenderInstanceID, e.Credential.ID, e.Credential.Version,
		e.SenderExit.Ref, strings.ToLower(e.SenderExit.SHA256), e.CapabilityRevocation.Ref,
		strings.ToLower(e.CapabilityRevocation.SHA256), operator, closedAt); err != nil {
		return UnknownCloseReceipt{}, unavailable()
	}

	// Local capability revocation is a final fact on the dispatch: no runner
	// can authorize a send for this test again.
	// The dispatch CHECK pins (capability_revoked_at IS NULL) to
	// (revocation_evidence_digest IS NULL); revoking must record the digest
	// of the capability revocation evidence in the same statement.
	if _, err = tx.Exec(ctx, `UPDATE repomesh_models.test_dispatch
		SET capability_revoked_at=$3, revocation_evidence_digest=$4
		WHERE actor=$1 AND test_id=$2 AND capability_revoked_at IS NULL`,
		command.ActorID, command.TestID, closedAt, strings.ToLower(e.CapabilityRevocation.SHA256)); err != nil {
		return UnknownCloseReceipt{}, unavailable()
	}
	if _, err = tx.Exec(ctx, `UPDATE repomesh_models.tests SET recovery='closed'
		WHERE actor=$1 AND test_id=$2 AND state='unknown'`,
		command.ActorID, command.TestID); err != nil {
		return UnknownCloseReceipt{}, unavailable()
	}
	if _, err = tx.Exec(ctx, `DELETE FROM repomesh_models.actor_outstanding_tests
		WHERE actor=$1 AND test_id=$2`, command.ActorID, command.TestID); err != nil {
		return UnknownCloseReceipt{}, unavailable()
	}
	if err = tx.Commit(ctx); err != nil {
		return UnknownCloseReceipt{}, unavailable()
	}
	return UnknownCloseReceipt{ActorID: command.ActorID, TestID: command.TestID, CloseKey: command.CloseKey,
		Outcome: "closed_without_result", OperatorPrincipal: operator, ClosedAt: closedAt}, nil
}

// ValidateUnknownCloseInTx runs the same binding and material checks as
// ValidateUnknownClose inside the caller's transaction.
func (m *UnknownMaintenance) ValidateUnknownCloseInTx(ctx context.Context, tx pgx.Tx, command UnknownCloseCommand) error {
	var externalOperationID, sendPermitID, senderInstanceID, credentialID, credentialVersion string
	err := tx.QueryRow(ctx, `SELECT external_operation_id,send_permit_id,sender_instance_id,
			credential_capability_id,credential_capability_version
		FROM repomesh_models.test_dispatch WHERE actor=$1 AND test_id=$2
		FOR UPDATE`, command.ActorID, command.TestID).Scan(
		&externalOperationID, &sendPermitID, &senderInstanceID, &credentialID, &credentialVersion)
	if errorsIsNoRows(err) {
		return failure(404, "TEST_NOT_FOUND")
	}
	if err != nil {
		return unavailable()
	}
	e := command.Evidence
	if externalOperationID != e.ExternalOperationID || sendPermitID != e.SendPermitID ||
		senderInstanceID != e.SenderInstanceID || credentialID != e.Credential.ID ||
		credentialVersion != e.Credential.Version {
		return failure(409, "CLOSE_BINDING_MISMATCH")
	}
	if err = verifyMaterial(e.SenderExit); err != nil {
		return err
	}
	return verifyMaterial(e.CapabilityRevocation)
}
