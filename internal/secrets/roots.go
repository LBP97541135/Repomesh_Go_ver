package secrets

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"sort"

	"github.com/jackc/pgx/v5"
)

const rootRegistryLock int64 = 0x524d534543524554
const selfCheckText = "RepoMesh secret storage self-check v1"

func (s *Store) registerRoots(ctx context.Context) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ErrUnavailable
	}
	defer rollback(tx)
	if _, err := tx.Exec(ctx, "SELECT pg_catalog.pg_advisory_xact_lock($1)", rootRegistryLock); err != nil {
		return ErrUnavailable
	}
	// Finish authorized writes before checking which roots their ciphertext needs.
	if _, err := tx.Exec(ctx, `SELECT root_id FROM repomesh_secrets.root_keys WHERE active_wrap FOR UPDATE`); err != nil {
		return ErrUnavailable
	}
	ids := make([]string, 0, len(s.roots))
	for id := range s.roots {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		key := s.roots[id]
		fingerprint := sha256.Sum256(key[:])
		if _, err := tx.Exec(ctx, `INSERT INTO repomesh_secrets.root_keys(root_id,fingerprint) VALUES ($1,$2) ON CONFLICT(root_id) DO NOTHING`, id, fingerprint[:]); err != nil {
			return ErrConfiguration
		}
		var stored []byte
		if err := tx.QueryRow(ctx, `SELECT fingerprint FROM repomesh_secrets.root_keys WHERE root_id=$1`, id).Scan(&stored); err != nil {
			return ErrUnavailable
		}
		if !bytes.Equal(stored, fingerprint[:]) {
			return ErrConfiguration
		}
	}
	rows, err := tx.Query(ctx, `SELECT root_id FROM repomesh_secrets.versions WHERE destroyed_at IS NULL UNION SELECT root_id FROM repomesh_secrets.self_check`)
	if err != nil {
		return ErrUnavailable
	}
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			rows.Close()
			return ErrUnavailable
		}
		if _, ok := s.roots[id]; !ok {
			rows.Close()
			return ErrConfiguration
		}
	}
	rows.Close()
	if rows.Err() != nil {
		return ErrUnavailable
	}
	e, err := readSelfCheck(ctx, tx)
	if err == nil {
		if err := s.verifySelfCheck(e); err != nil {
			return err
		}
	} else {
		if !errors.Is(err, pgx.ErrNoRows) {
			return ErrUnavailable
		}
		var hasVersions bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM repomesh_secrets.versions WHERE destroyed_at IS NULL)`).Scan(&hasVersions); err != nil || hasVersions {
			return ErrUnavailable
		}
	}
	var retired bool
	if err := tx.QueryRow(ctx, `SELECT retired_at IS NOT NULL FROM repomesh_secrets.root_keys WHERE root_id=$1`, s.activeRootID).Scan(&retired); err != nil {
		return ErrUnavailable
	}
	if retired {
		return ErrConfiguration
	}
	if _, err := tx.Exec(ctx, `UPDATE repomesh_secrets.root_keys SET active_wrap=false,retired_at=now() WHERE active_wrap AND root_id<>$1`, s.activeRootID); err != nil {
		return ErrUnavailable
	}
	if _, err := tx.Exec(ctx, `UPDATE repomesh_secrets.root_keys SET active_wrap=true,activated_at=COALESCE(activated_at,now()) WHERE root_id=$1`, s.activeRootID); err != nil {
		return ErrUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return ErrUnavailable
	}
	return nil
}

type querier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func readSelfCheck(ctx context.Context, q querier) (envelope, error) {
	e := envelope{id: "self-check-v1", owner: Owner{Kind: "system", ID: "secret-store"}, purpose: "self-check", format: formatVersion}
	err := q.QueryRow(ctx, `SELECT ciphertext,wrapped_dek,root_id,wrap_revision FROM repomesh_secrets.self_check WHERE singleton`).Scan(&e.ciphertext, &e.wrappedDEK, &e.rootID, &e.revision)
	return e, err
}

func (s *Store) verifySelfCheck(e envelope) error {
	root, ok := s.roots[e.rootID]
	if !ok {
		return ErrConfiguration
	}
	plaintext, err := e.open(root[:])
	if err != nil {
		return err
	}
	defer clear(plaintext)
	if string(plaintext) != selfCheckText {
		return ErrUnavailable
	}
	return nil
}

func (s *Store) check(ctx context.Context) error {
	e, err := readSelfCheck(ctx, s.pool)
	missing := errors.Is(err, pgx.ErrNoRows)
	if err != nil && !missing {
		return ErrUnavailable
	}
	if !missing {
		if err := s.verifySelfCheck(e); err != nil {
			return err
		}
		if e.rootID == s.activeRootID {
			return nil
		}
	}
	if err := s.reserveWrap(ctx); err != nil {
		return err
	}
	root := s.roots[s.activeRootID]
	if missing {
		e.rootID = s.activeRootID
		if err := e.seal(root[:], []byte(selfCheckText)); err != nil {
			return err
		}
	} else {
		oldRoot := s.roots[e.rootID]
		if err := e.rewrap(oldRoot[:], root[:], s.activeRootID); err != nil {
			return err
		}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ErrUnavailable
	}
	defer rollback(tx)
	if err := s.requireActive(ctx, tx); err != nil {
		return err
	}
	if missing {
		_, err = tx.Exec(ctx, `INSERT INTO repomesh_secrets.self_check(ciphertext,wrapped_dek,root_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, e.ciphertext, e.wrappedDEK, e.rootID)
	} else {
		_, err = tx.Exec(ctx, `UPDATE repomesh_secrets.self_check SET wrapped_dek=$1,root_id=$2,wrap_revision=wrap_revision+1 WHERE singleton AND wrap_revision=$3`, e.wrappedDEK, e.rootID, e.revision)
	}
	if err != nil {
		return ErrUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return ErrUnavailable
	}
	verified, err := readSelfCheck(ctx, s.pool)
	if err != nil {
		return ErrUnavailable
	}
	return s.verifySelfCheck(verified)
}
