package access

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"repomesh.local/repomesh/internal/github"
	"repomesh.local/repomesh/internal/secrets"
)

func (s *Service) CompleteCallback(ctx context.Context, input Callback) (CallbackResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	callbackDeadline, _ := ctx.Deadline()

	invalid := CallbackResult{Page: "/login"}
	if !validCookie(input.BindingCookie) || !validCookie(input.State) || len(input.Code) > 2048 || len(input.ProviderError) > 128 {
		return invalid, nil
	}
	binding := digest(input.BindingCookie)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return invalid, unavailable()
	}
	defer tx.Rollback(ctx)
	var identity, attempt int64
	err = tx.QueryRow(ctx, `SELECT identity_generation,attempt_generation FROM repomesh_access.bindings WHERE hash=$1 AND expires_at>now() FOR UPDATE`, binding).Scan(&identity, &attempt)
	if errors.Is(err, pgx.ErrNoRows) {
		return invalid, nil
	}
	if err != nil {
		return invalid, unavailable()
	}
	var id, state, materialRef string
	var expectedIdentity, expectedAttempt int64
	var expires time.Time
	err = tx.QueryRow(ctx, `SELECT id,state,material_ref,identity_generation,attempt_generation,expires_at FROM repomesh_access.attempts WHERE binding=$1 AND state_hash=$2 FOR UPDATE`, binding, digest(input.State)).Scan(&id, &state, &materialRef, &expectedIdentity, &expectedAttempt, &expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return invalid, nil
	}
	if err != nil {
		return invalid, unavailable()
	}
	result := CallbackResult{Page: "/auth/result/" + id}
	if state != "pending" || identity != expectedIdentity || attempt != expectedAttempt || !expires.After(time.Now()) {
		return result, nil
	}
	if input.ProviderError != "" || input.Code == "" {
		terminal, reason := "unknown", "EXCHANGE_UNCONFIRMED"
		if input.ProviderError == "access_denied" {
			terminal, reason = "cancelled", "USER_CANCELLED"
		}
		_, err = tx.Exec(ctx, `UPDATE repomesh_access.attempts SET state=$3,reason=$4,observed_at=now() WHERE binding=$1 AND id=$2`, binding, id, terminal, reason)
		if err != nil || tx.Commit(ctx) != nil {
			return result, unavailable()
		}
		return result, nil
	}
	if _, err = tx.Exec(ctx, `UPDATE repomesh_access.attempts SET state='exchanging',observed_at=now() WHERE binding=$1 AND id=$2`, binding, id); err != nil {
		return result, unavailable()
	}
	if tx.Commit(ctx) != nil {
		return result, unavailable()
	}
	materialBytes, err := s.secrets.Open(ctx, secrets.VersionID(materialRef), attemptOwner(binding, id), secrets.Purpose("auth-material"))
	if err != nil {
		s.unknownAttempt(ctx, binding, id)
		return result, nil
	}
	var material exchangeMaterial
	if json.Unmarshal(materialBytes, &material) != nil {
		s.unknownAttempt(ctx, binding, id)
		return result, nil
	}
	tokens, err := s.provider.Exchange(ctx, input.Code, material.Verifier)
	if err != nil {
		s.unknownAttempt(ctx, binding, id)
		return result, nil
	}
	encoded, _ := json.Marshal(tokens)
	ref, err := s.secrets.Seal(ctx, attemptOwner(binding, id), secrets.Purpose("auth-exchange-result"), encoded)
	if err != nil {
		s.unknownAttempt(ctx, binding, id)
		return result, nil
	}
	command, err := s.pool.Exec(ctx, `UPDATE repomesh_access.attempts SET state='identity_pending',token_ref=$3,observed_at=now(),next_run_at=$4 WHERE binding=$1 AND id=$2 AND state='exchanging' AND expires_at>now()`, binding, id, string(ref), callbackDeadline.UTC())
	if err == nil && command.RowsAffected() == 0 {
		s.discard(ref)
	}
	if err != nil || command.RowsAffected() != 1 {
		return result, nil
	}
	confirmed, confirmErr := s.confirmIdentity(ctx, binding, id, tokens, true)
	if confirmed.SessionCookie == "" {
		_, _ = s.pool.Exec(ctx, `UPDATE repomesh_access.attempts SET next_run_at=now() WHERE binding=$1 AND id=$2 AND identity_generation=$3 AND attempt_generation=$4 AND state='identity_pending'`, binding, id, expectedIdentity, expectedAttempt)
	}
	return confirmed, confirmErr
}

func (s *Service) unknownAttempt(ctx context.Context, binding, id string) {
	_, _ = s.pool.Exec(ctx, `UPDATE repomesh_access.attempts SET state='unknown',reason='EXCHANGE_UNCONFIRMED',observed_at=now() WHERE binding=$1 AND id=$2 AND state='exchanging'`, binding, id)
}

func (s *Service) confirmIdentity(ctx context.Context, binding, id string, tokens github.TokenSet, issueCookie bool) (CallbackResult, error) {
	result := CallbackResult{Page: "/auth/result/" + id}
	external, err := s.provider.Identity(ctx, tokens.AccessToken)
	if err != nil {
		return result, nil
	}
	var purpose string
	var expected *int64
	if err = s.pool.QueryRow(ctx, `SELECT purpose,expected_github_id FROM repomesh_access.attempts WHERE binding=$1 AND id=$2 AND state='identity_pending' AND expires_at>now()`, binding, id).Scan(&purpose, &expected); err != nil {
		return result, nil
	}
	if purpose == "reconnect" && (expected == nil || *expected != external.ID) {
		_, err = s.pool.Exec(ctx, `UPDATE repomesh_access.attempts SET state='rejected',reason='ACCOUNT_MISMATCH',observed_at=now() WHERE binding=$1 AND id=$2 AND state='identity_pending'`, binding, id)
		if err != nil {
			return result, unavailable()
		}
		return result, nil
	}
	var actor string
	err = s.pool.QueryRow(ctx, `INSERT INTO repomesh_access.accounts(id,github_id,display_name) VALUES($1,$2,$3)
		ON CONFLICT(github_id) DO UPDATE SET display_name=EXCLUDED.display_name RETURNING id`, newID(), external.ID, external.DisplayName).Scan(&actor)
	if err != nil {
		return result, unavailable()
	}
	owner := secrets.Owner{Kind: "actor", ID: actor}
	accessRef, err := s.secrets.Seal(ctx, owner, secrets.Purpose("github-user-token"), []byte(tokens.AccessToken))
	if err != nil {
		return result, unavailable()
	}
	var refreshRef secrets.VersionID
	keep := false
	defer func() {
		if !keep {
			s.discard(accessRef, refreshRef)
		}
	}()
	if tokens.RefreshToken != "" {
		refreshRef, err = s.secrets.Seal(ctx, owner, secrets.Purpose("github-refresh-token"), []byte(tokens.RefreshToken))
		if err != nil {
			return result, unavailable()
		}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return result, unavailable()
	}
	defer tx.Rollback(ctx)
	var identity, attempt int64
	if err = tx.QueryRow(ctx, `SELECT identity_generation,attempt_generation FROM repomesh_access.bindings WHERE hash=$1 AND expires_at>now() FOR UPDATE`, binding).Scan(&identity, &attempt); err != nil {
		return result, nil
	}
	var state string
	var expectedRevision *string
	var expectedIdentity, expectedAttempt int64
	var expires, started time.Time
	if err = tx.QueryRow(ctx, `SELECT state,identity_generation,attempt_generation,expires_at,expected_connection_revision,created_at FROM repomesh_access.attempts WHERE binding=$1 AND id=$2 FOR UPDATE`, binding, id).Scan(&state, &expectedIdentity, &expectedAttempt, &expires, &expectedRevision, &started); err != nil {
		return result, unavailable()
	}
	if state != "identity_pending" || identity != expectedIdentity || attempt != expectedAttempt || !expires.After(time.Now()) {
		return result, nil
	}
	var disabled bool
	if err = tx.QueryRow(ctx, `SELECT disabled FROM repomesh_access.accounts WHERE id=$1 FOR UPDATE`, actor).Scan(&disabled); err != nil {
		return result, unavailable()
	}
	if disabled {
		return result, failure(401, "AUTHENTICATION_REQUIRED")
	}
	var currentRevision string
	var committed time.Time
	err = tx.QueryRow(ctx, `SELECT revision,credential_committed_at FROM repomesh_access.connections WHERE actor=$1 FOR UPDATE`, actor).Scan(&currentRevision, &committed)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return result, unavailable()
	}
	if purpose == "reconnect" && (expectedRevision == nil || currentRevision != *expectedRevision) || purpose == "login" && committed.After(started) {
		if _, err = tx.Exec(ctx, `UPDATE repomesh_access.attempts SET state='superseded',reason='NEWER_ATTEMPT',observed_at=now() WHERE binding=$1 AND id=$2`, binding, id); err != nil {
			return result, unavailable()
		}
		if err = tx.Commit(ctx); err != nil {
			return result, unavailable()
		}
		return result, nil
	}
	revision := newID()
	_, err = tx.Exec(ctx, `INSERT INTO repomesh_access.connections(actor,revision,access_ref,refresh_ref,access_expires_at,refresh_expires_at,status,observed_at)
	VALUES($1,$2,$3,$4,$5,$6,'connected',now()) ON CONFLICT(actor) DO UPDATE SET revision=EXCLUDED.revision,access_epoch=repomesh_access.connections.access_epoch+1,
	access_ref=EXCLUDED.access_ref,refresh_ref=EXCLUDED.refresh_ref,access_expires_at=EXCLUDED.access_expires_at,refresh_expires_at=EXCLUDED.refresh_expires_at,status='connected',observed_at=now(),refresh_state='idle',credential_committed_at=clock_timestamp()`, actor, revision, string(accessRef), string(refreshRef), tokens.AccessExpiresAt, tokens.RefreshExpiresAt)
	if err != nil {
		return result, unavailable()
	}
	if _, err = tx.Exec(ctx, `UPDATE repomesh_access.bindings SET identity_generation=identity_generation+1,last_actor=$2 WHERE hash=$1`, binding, actor); err != nil {
		return result, unavailable()
	}
	if _, err = tx.Exec(ctx, `UPDATE repomesh_access.sessions SET revoked=true WHERE binding=$1`, binding); err != nil {
		return result, unavailable()
	}
	cookie := randomToken()
	if _, err = tx.Exec(ctx, `INSERT INTO repomesh_access.sessions(hash,actor,binding,generation,expires_at) VALUES($1,$2,$3,$4,now()+interval '12 hours')`, digest(cookie), actor, binding, identity+1); err != nil {
		return result, unavailable()
	}
	if _, err = tx.Exec(ctx, `UPDATE repomesh_access.attempts SET state='confirmed',reason=NULL,actor=$3,connection_revision=$4,observed_at=now() WHERE binding=$1 AND id=$2`, binding, id, actor, revision); err != nil {
		return result, unavailable()
	}
	keep = true
	if tx.Commit(ctx) != nil {
		return result, unavailable()
	}
	if issueCookie {
		result.SessionCookie = cookie
	}
	return result, nil
}

func (s *Service) Attempt(ctx context.Context, bindingCookie, sessionCookie, id string) (AttemptResult, error) {
	if !validCookie(bindingCookie) || !ValidID(id) {
		return AttemptResult{}, failure(404, "RESOURCE_NOT_FOUND")
	}
	var result AttemptResult
	var actor, original, revision *string
	var destination string
	var created, expires time.Time
	err := s.pool.QueryRow(ctx, `SELECT a.id,a.purpose,a.state,a.reason,a.observed_at,a.actor,a.original_actor,a.connection_revision,a.destination::text,a.created_at,a.expires_at
	FROM repomesh_access.attempts a JOIN repomesh_access.bindings b ON a.binding=b.hash WHERE a.binding=$1 AND a.id=$2 AND b.expires_at>now()`, digest(bindingCookie), id).Scan(&result.AttemptID, &result.Purpose, &result.State, &result.ReasonCode, &result.ObservedAt, &actor, &original, &revision, &destination, &created, &expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, failure(404, "RESOURCE_NOT_FOUND")
	}
	if err != nil {
		return result, unavailable()
	}
	current, sessionErr := s.Session(ctx, sessionCookie)
	if sessionErr != nil {
		var f *Failure
		if !errors.As(sessionErr, &f) || f.Status != 401 {
			return AttemptResult{}, sessionErr
		}
	}
	if result.Purpose == "reconnect" {
		if sessionErr != nil {
			return AttemptResult{}, failure(401, "AUTHENTICATION_REQUIRED")
		}
		if actor == nil || *actor != current.User.ID {
			return AttemptResult{}, failure(404, "RESOURCE_NOT_FOUND")
		}
	}
	if time.Since(created) > 24*time.Hour {
		return AttemptResult{}, failure(410, "AUTH_ATTEMPT_RESULT_REMOVED")
	}
	if result.State == "exchanging" || result.State == "identity_pending" {
		result.State = "unknown"
		reason := "EXCHANGE_UNCONFIRMED"
		result.ReasonCode = &reason
	}
	if result.State == "pending" && !expires.After(time.Now()) {
		result.State = "expired"
		reason := "ATTEMPT_EXPIRED"
		result.ReasonCode = &reason
	}
	if result.Purpose == "reconnect" && result.State == "confirmed" && revision != nil {
		var latest string
		if err = s.pool.QueryRow(ctx, `SELECT revision FROM repomesh_access.connections WHERE actor=$1`, current.User.ID).Scan(&latest); err != nil {
			return AttemptResult{}, unavailable()
		}
		result.Connection = &Receipt{CommittedRevision: *revision, IsCurrent: latest == *revision}
	}
	if sessionErr == nil {
		target, err := ParseDestination([]byte(destination))
		if err != nil {
			return AttemptResult{}, unavailable()
		}
		if original != nil && *original != current.User.ID || actor != nil && *actor != current.User.ID {
			home := "/"
			result.NextPage = &home
		} else if target.home {
			home := "/"
			result.NextPage = &home
		} else if next, resolveErr := s.resolveProjectDestination(ctx, current.User.ID, target); resolveErr == nil && next != nil {
			result.NextPage = next
		} else if next, resolveErr := s.resolveModelSaveDestination(ctx, current.User.ID, target); resolveErr == nil && next != nil {
			result.NextPage = next
		} else if next, resolveErr := s.resolveModelTestDestination(ctx, current.User.ID, target); resolveErr == nil && next != nil {
			result.NextPage = next
		} else if next, resolveErr := s.resolveModelApplyDestination(ctx, current.User.ID, target); resolveErr == nil && next != nil {
			result.NextPage = next
		} else if resolveErr != nil {
			home := "/"
			result.NextPage = &home
		}
	}
	return result, nil
}
