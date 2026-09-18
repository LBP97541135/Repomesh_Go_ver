package access

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"repomesh.local/repomesh/internal/secrets"
)

type credential struct {
	actor, revision, token string
	epoch                  int64
	accessRef              string
}

func (s *Service) credential(ctx context.Context, actor string, canRefresh bool) (credential, error) {
	var c credential
	c.actor = actor
	var refreshRef, status, refreshState string
	var expires, refreshExpires *time.Time
	err := s.pool.QueryRow(ctx, `SELECT c.revision,c.access_epoch,c.access_ref,c.refresh_ref,c.access_expires_at,c.refresh_expires_at,c.status,c.refresh_state
	FROM repomesh_access.connections c JOIN repomesh_access.accounts a ON c.actor=a.id WHERE c.actor=$1 AND NOT a.disabled`, actor).Scan(&c.revision, &c.epoch, &c.accessRef, &refreshRef, &expires, &refreshExpires, &status, &refreshState)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, failure(503, "AUTHORIZATION_UNCONFIRMED")
	}
	if err != nil {
		return c, unavailable()
	}
	if status != "connected" || refreshState != "idle" {
		return c, failure(503, "AUTHORIZATION_UNCONFIRMED")
	}
	owner := secrets.Owner{Kind: "actor", ID: actor}
	if expires != nil && !expires.After(time.Now().Add(30*time.Second)) {
		if !canRefresh || refreshRef == "" || refreshExpires != nil && !refreshExpires.After(time.Now()) {
			return c, failure(503, "AUTHORIZATION_UNCONFIRMED")
		}
		command, err := s.pool.Exec(ctx, `UPDATE repomesh_access.connections SET refresh_state='claimed' WHERE actor=$1 AND revision=$2 AND access_epoch=$3 AND refresh_state='idle'`, actor, c.revision, c.epoch)
		if err != nil {
			return c, unavailable()
		}
		if command.RowsAffected() != 1 {
			return c, failure(503, "AUTHORIZATION_UNCONFIRMED")
		}
		plain, err := s.secrets.Open(ctx, secrets.VersionID(refreshRef), owner, secrets.Purpose("github-refresh-token"))
		if err != nil {
			s.refreshUnknown(ctx, c)
			return c, unavailable()
		}
		tokens, err := s.provider.Refresh(ctx, string(plain))
		if err != nil {
			s.refreshUnknown(ctx, c)
			if providerUnauthorized(err) {
				s.rejectCredential(ctx, c)
			}
			return c, failure(503, "AUTHORIZATION_UNCONFIRMED")
		}
		access, err := s.secrets.Seal(ctx, owner, secrets.Purpose("github-user-token"), []byte(tokens.AccessToken))
		if err != nil {
			s.refreshUnknown(ctx, c)
			return c, unavailable()
		}
		var refresh secrets.VersionID
		keep := false
		defer func() {
			if !keep {
				s.discard(access, refresh)
			}
		}()
		if tokens.RefreshToken != "" {
			refresh, err = s.secrets.Seal(ctx, owner, secrets.Purpose("github-refresh-token"), []byte(tokens.RefreshToken))
			if err != nil {
				s.refreshUnknown(ctx, c)
				return c, unavailable()
			}
		}
		revision := newID()
		command, err = s.pool.Exec(ctx, `UPDATE repomesh_access.connections SET revision=$4,access_epoch=access_epoch+1,access_ref=$5,refresh_ref=$6,
		access_expires_at=$7,refresh_expires_at=$8,status='connected',refresh_state='idle',observed_at=now(),credential_committed_at=clock_timestamp() WHERE actor=$1 AND revision=$2 AND access_epoch=$3 AND refresh_state='claimed'`, actor, c.revision, c.epoch, revision, string(access), string(refresh), tokens.AccessExpiresAt, tokens.RefreshExpiresAt)
		keep = err != nil || command.RowsAffected() == 1
		if err != nil || command.RowsAffected() != 1 {
			return c, unavailable()
		}
		c.revision = revision
		c.epoch++
		c.token = tokens.AccessToken
		return c, nil
	}
	plain, err := s.secrets.Open(ctx, secrets.VersionID(c.accessRef), owner, secrets.Purpose("github-user-token"))
	if err != nil {
		return c, failure(503, "AUTHORIZATION_UNCONFIRMED")
	}
	c.token = string(plain)
	return c, nil
}

func (s *Service) refreshUnknown(ctx context.Context, c credential) {
	_, _ = s.pool.Exec(ctx, `UPDATE repomesh_access.connections SET refresh_state='unknown',status='unknown',observed_at=now() WHERE actor=$1 AND revision=$2 AND access_epoch=$3 AND refresh_state='claimed'`, c.actor, c.revision, c.epoch)
}

func (s *Service) rejectCredential(ctx context.Context, c credential) {
	_, _ = s.pool.Exec(ctx, `UPDATE repomesh_access.connections SET status='missing',access_epoch=access_epoch+1,observed_at=now() WHERE actor=$1 AND revision=$2 AND access_epoch=$3`, c.actor, c.revision, c.epoch)
}
