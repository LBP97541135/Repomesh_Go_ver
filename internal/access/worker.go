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

func (s *Service) RunOne(ctx context.Context) (bool, error) {
	if count, err := s.secrets.Rewrap(ctx, 20); err != nil {
		return false, err
	} else if count > 0 {
		return true, nil
	}
	if err := s.maintain(ctx); err != nil {
		return false, err
	}
	var binding, id, ref string
	err := s.pool.QueryRow(ctx, `UPDATE repomesh_access.attempts SET next_run_at=now()+interval '5 seconds' WHERE (binding,id) IN
	(SELECT a.binding,a.id FROM repomesh_access.attempts a JOIN repomesh_access.bindings b ON a.binding=b.hash
	WHERE a.state='identity_pending' AND a.expires_at>now() AND a.next_run_at<=now() AND b.expires_at>now()
	AND a.identity_generation=b.identity_generation AND a.attempt_generation=b.attempt_generation LIMIT 1 FOR UPDATE OF a SKIP LOCKED)
	RETURNING binding,id,token_ref`).Scan(&binding, &id, &ref)
	if err == nil {
		plain, err := s.secrets.Open(ctx, secrets.VersionID(ref), attemptOwner(binding, id), secrets.Purpose("auth-exchange-result"))
		if err != nil {
			return true, unavailable()
		}
		var tokens github.TokenSet
		if json.Unmarshal(plain, &tokens) != nil {
			return true, unavailable()
		}
		_, err = s.confirmIdentity(ctx, binding, id, tokens, false)
		return true, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return false, unavailable()
	}
	var actor string
	err = s.pool.QueryRow(ctx, `SELECT actor FROM repomesh_access.connections WHERE status='connected' AND refresh_state='idle' AND refresh_ref<>'' AND access_expires_at<=now()+interval '30 seconds' AND refresh_expires_at>now() LIMIT 1`).Scan(&actor)
	if err == nil {
		_, err = s.credential(ctx, actor, true)
		return true, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return false, unavailable()
	}
	var claimed int64
	err = s.pool.QueryRow(ctx, `UPDATE repomesh_access.discovery_batches SET claim_version=claim_version+1,lease_until=now()+interval '15 seconds'
	WHERE id=(SELECT b.id FROM repomesh_access.discovery_batches b JOIN repomesh_access.connections c ON b.actor=c.actor
	WHERE NOT b.complete AND b.expires_at>now() AND b.next_run_at<=now() AND (b.lease_until IS NULL OR b.lease_until<now())
	AND b.connection_revision=c.revision AND b.access_epoch=c.access_epoch ORDER BY b.next_run_at,b.id LIMIT 1 FOR UPDATE OF b SKIP LOCKED)
	RETURNING id,claim_version`).Scan(&id, &claimed)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, unavailable()
	}
	b, err := s.readBatch(ctx, id)
	if err != nil {
		return true, err
	}
	if b.claim != claimed {
		return true, nil
	}
	c, err := s.credential(ctx, b.actor, false)
	if err != nil {
		s.deferBatch(ctx, b, 5*time.Second)
		return true, err
	}
	workCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var found []github.Repository
	next := b.upstream
	hadSuccess := false
	var observed time.Time
	retryDelay := time.Second
	for count := 0; count < 2 && next > 0; count++ {
		page, err := s.provider.Repositories(workCtx, c.token, next)
		if err != nil {
			var upstream *github.Error
			if errors.As(err, &upstream) && upstream.RetryAfter > retryDelay {
				retryDelay = upstream.RetryAfter
			}
			if providerUnauthorized(err) {
				s.rejectCredential(ctx, c)
			}
			break
		}
		hadSuccess = true
		observed = time.Now().UTC()
		found = append(found, page.Items...)
		next = page.NextPage
	}
	if !hadSuccess {
		if retryDelay < 5*time.Second {
			retryDelay = 5 * time.Second
		}
		s.deferBatch(ctx, b, retryDelay)
		return true, failure(503, "AUTHORIZATION_UNCONFIRMED")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return true, unavailable()
	}
	defer tx.Rollback(ctx)
	var current bool
	err = tx.QueryRow(ctx, `SELECT revision=$2 AND access_epoch=$3 AND status='connected' AND refresh_state='idle' FROM repomesh_access.connections WHERE actor=$1 FOR UPDATE`, b.actor, b.revision, b.epoch).Scan(&current)
	if err != nil {
		return true, unavailable()
	}
	if !current {
		return true, nil
	}
	var claim int64
	err = tx.QueryRow(ctx, `SELECT claim_version FROM repomesh_access.discovery_batches WHERE id=$1 AND expires_at>now() FOR UPDATE`, b.id).Scan(&claim)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return true, unavailable()
	}
	if claim != b.claim {
		return true, nil
	}
	for _, repo := range found {
		if _, err = tx.Exec(ctx, `INSERT INTO repomesh_access.discovered_repositories(batch,github_id,owner,name,full_name,observed_at) VALUES($1,$2,$3,$4,$5,$6)
		ON CONFLICT(batch,github_id) DO UPDATE SET owner=EXCLUDED.owner,name=EXCLUDED.name,full_name=EXCLUDED.full_name,observed_at=EXCLUDED.observed_at`, b.id, repo.ID, repo.Owner, repo.Name, repo.FullName, observed); err != nil {
			return true, unavailable()
		}
	}
	complete := next == 0
	if complete {
		next = b.upstream
	}
	if _, err = tx.Exec(ctx, `UPDATE repomesh_access.discovery_batches SET upstream_page=$2,complete=$3,had_success=true,observed_at=$4,lease_until=NULL,next_run_at=now()+$6*interval '1 second' WHERE id=$1 AND claim_version=$5`, b.id, next, complete, observed, b.claim, int64(retryDelay.Seconds())+1); err != nil {
		return true, unavailable()
	}
	if tx.Commit(ctx) != nil {
		return true, unavailable()
	}
	return true, nil
}

func (s *Service) deferBatch(ctx context.Context, b batch, delay time.Duration) {
	_, _ = s.pool.Exec(ctx, `UPDATE repomesh_access.discovery_batches SET lease_until=NULL,next_run_at=now()+$3*interval '1 second' WHERE id=$1 AND claim_version=$2`, b.id, b.claim, int64(delay.Seconds())+1)
}

func (s *Service) maintain(ctx context.Context) error {
	if err := s.cleanOrphans(ctx); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `UPDATE repomesh_access.attempts SET state=CASE WHEN state='pending' THEN 'expired' ELSE 'unknown' END,
	reason=CASE WHEN state='pending' THEN 'ATTEMPT_EXPIRED' ELSE 'EXCHANGE_UNCONFIRMED' END,observed_at=now()
	WHERE expires_at<=now() AND state IN ('pending','exchanging','identity_pending')`)
	if err != nil {
		return unavailable()
	}
	rows, err := s.pool.Query(ctx, `SELECT binding,id,material_ref,token_ref,state FROM repomesh_access.attempts
	WHERE (material_ref<>'' AND state NOT IN ('pending','exchanging')) OR (token_ref<>'' AND state NOT IN ('identity_pending','exchanging')) LIMIT 20`)
	if err != nil {
		return unavailable()
	}
	type cleanup struct{ binding, id, material, token, state string }
	var items []cleanup
	for rows.Next() {
		var item cleanup
		if rows.Scan(&item.binding, &item.id, &item.material, &item.token, &item.state) != nil {
			rows.Close()
			return unavailable()
		}
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return unavailable()
	}
	for _, item := range items {
		if item.material != "" {
			if err := s.secrets.Destroy(ctx, secrets.VersionID(item.material)); err != nil {
				return unavailable()
			}
			if _, err = s.pool.Exec(ctx, `UPDATE repomesh_access.attempts SET material_ref='' WHERE binding=$1 AND id=$2 AND material_ref=$3`, item.binding, item.id, item.material); err != nil {
				return unavailable()
			}
		}
		if item.token != "" && item.state != "identity_pending" {
			if err := s.secrets.Destroy(ctx, secrets.VersionID(item.token)); err != nil {
				return unavailable()
			}
			if _, err = s.pool.Exec(ctx, `UPDATE repomesh_access.attempts SET token_ref='' WHERE binding=$1 AND id=$2 AND token_ref=$3`, item.binding, item.id, item.token); err != nil {
				return unavailable()
			}
		}
	}
	return nil
}
