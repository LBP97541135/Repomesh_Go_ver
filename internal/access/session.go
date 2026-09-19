package access

import (
	"context"
	"crypto/subtle"
	"errors"

	"github.com/jackc/pgx/v5"
)

func (s *Service) Session(ctx context.Context, cookie string) (Session, error) {
	if !validCookie(cookie) {
		return Session{}, failure(401, "AUTHENTICATION_REQUIRED")
	}
	var view Session
	err := s.pool.QueryRow(ctx, `UPDATE repomesh_access.sessions AS s SET last_active_at=now()
		FROM repomesh_access.bindings b, repomesh_access.accounts a
		WHERE s.hash=$1 AND s.binding=b.hash AND s.actor=a.id AND NOT s.revoked AND NOT a.disabled
		AND s.expires_at>now() AND s.last_active_at>now()-interval '30 minutes'
		AND b.expires_at>now() AND s.generation=b.identity_generation
		RETURNING a.id,a.display_name,a.github_id,s.binding,s.generation`, digest(cookie)).Scan(&view.User.ID, &view.User.DisplayName, &view.GitHubID, &view.Binding, &view.Generation)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, failure(401, "AUTHENTICATION_REQUIRED")
	}
	if err != nil {
		return Session{}, unavailable()
	}
	view.CSRFToken = digest("repomesh-csrf:" + cookie)
	view.GitHubConnection.Status = "missing"
	err = s.pool.QueryRow(ctx, `SELECT CASE WHEN status='connected' AND (refresh_state<>'idle' OR access_expires_at<=now() OR observed_at<now()-interval '60 seconds') THEN 'unknown' ELSE status END,observed_at FROM repomesh_access.connections WHERE actor=$1`, view.User.ID).Scan(&view.GitHubConnection.Status, &view.GitHubConnection.ObservedAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Session{}, unavailable()
	}
	return view, nil
}

// IsAdmin reports whether the authenticated actor's own account is an
// administrator. Mutating surfaces still call this on every request — the
// browser-side session fact is only for hiding controls, never authority.
func (s *Service) IsAdmin(ctx context.Context, actor string) (bool, error) {
	var isAdmin bool
	err := s.pool.QueryRow(ctx, `SELECT is_admin FROM repomesh_access.accounts WHERE id=$1 AND NOT disabled`, actor).Scan(&isAdmin)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, failure(401, "AUTHENTICATION_REQUIRED")
	}
	if err != nil {
		return false, unavailable()
	}
	return isAdmin, nil
}

func requireCSRF(session Session, csrf string) error {
	if subtle.ConstantTimeCompare([]byte(session.CSRFToken), []byte(csrf)) != 1 {
		return failure(403, "CSRF_REJECTED")
	}
	return nil
}

func (s *Service) Logout(ctx context.Context, cookie, csrf string) error {
	session, err := s.Session(ctx, cookie)
	if err != nil {
		var failure *Failure
		if errors.As(err, &failure) && failure.Status == 401 {
			return nil
		}
		return err
	}
	if err := requireCSRF(session, csrf); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return unavailable()
	}
	defer tx.Rollback(ctx)
	var generation int64
	if err := tx.QueryRow(ctx, `SELECT identity_generation FROM repomesh_access.bindings WHERE hash=$1 FOR UPDATE`, session.Binding).Scan(&generation); err != nil {
		return unavailable()
	}
	var revoked bool
	if err := tx.QueryRow(ctx, `SELECT revoked FROM repomesh_access.sessions WHERE hash=$1 FOR UPDATE`, digest(cookie)).Scan(&revoked); err != nil {
		return unavailable()
	}
	if !revoked {
		if _, err = tx.Exec(ctx, `UPDATE repomesh_access.sessions SET revoked=true WHERE hash=$1`, digest(cookie)); err != nil {
			return unavailable()
		}
		if generation == session.Generation {
			if _, err = tx.Exec(ctx, `UPDATE repomesh_access.bindings SET identity_generation=identity_generation+1,attempt_generation=attempt_generation+1 WHERE hash=$1`, session.Binding); err != nil {
				return unavailable()
			}
			if _, err = tx.Exec(ctx, `UPDATE repomesh_access.attempts SET state='superseded',reason='NEWER_ATTEMPT',observed_at=now() WHERE binding=$1 AND identity_generation=$2 AND state IN ('pending','exchanging','identity_pending','unknown')`, session.Binding, session.Generation); err != nil {
				return unavailable()
			}
		}
	}
	if tx.Commit(ctx) != nil {
		return unavailable()
	}
	return nil
}
