package secrets

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type PreparedSecret struct {
	value     envelope
	store     *Store
	inserted  bool
	discarded bool
}

func (p *PreparedSecret) VersionID() VersionID {
	if p == nil {
		return ""
	}
	return p.value.id
}

func (p *PreparedSecret) Discard() {
	if p == nil || p.discarded {
		return
	}
	p.discarded = true
	clear(p.value.ciphertext)
	clear(p.value.wrappedDEK)
	p.value.ciphertext = nil
	p.value.wrappedDEK = nil
}

func (s *Store) Prepare(ctx context.Context, owner Owner, purpose Purpose, plaintext []byte) (*PreparedSecret, error) {
	if !validOwnerPurpose(owner, purpose) || len(plaintext) == 0 || len(plaintext) > 1024*1024 {
		return nil, ErrUnavailable
	}
	if err := s.reserveWrap(ctx); err != nil {
		return nil, err
	}
	e := envelope{id: newVersionID(), owner: owner, purpose: purpose, format: formatVersion, rootID: s.activeRootID}
	root := s.roots[s.activeRootID]
	if err := e.seal(root[:], plaintext); err != nil {
		return nil, err
	}
	return &PreparedSecret{value: e, store: s}, nil
}

func (s *Store) InsertPrepared(ctx context.Context, tx pgx.Tx, prepared *PreparedSecret) (VersionID, error) {
	if prepared == nil || prepared.store != s || prepared.inserted || prepared.discarded {
		return "", ErrUnavailable
	}
	if err := s.requireRoot(ctx, tx, prepared.value.rootID); err != nil {
		return "", err
	}
	e := prepared.value
	_, err := tx.Exec(ctx, `INSERT INTO repomesh_secrets.versions
		(version_id,owner_kind,owner_id,purpose,format_version,ciphertext,wrapped_dek,root_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, string(e.id), e.owner.Kind, e.owner.ID, string(e.purpose), e.format, e.ciphertext, e.wrappedDEK, e.rootID)
	if err != nil {
		return "", ErrUnavailable
	}
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_secrets.availability(version_id) VALUES ($1)`, string(e.id)); err != nil {
		return "", ErrUnavailable
	}
	prepared.inserted = true
	return e.id, nil
}

func (s *Store) requireRoot(ctx context.Context, tx pgx.Tx, rootID string) error {
	var active bool
	if err := tx.QueryRow(ctx, `SELECT active_wrap FROM repomesh_secrets.root_keys WHERE root_id=$1 FOR SHARE`, rootID).Scan(&active); err != nil || !active {
		return ErrUnavailable
	}
	return nil
}

func (s *Store) OpenInTx(ctx context.Context, tx pgx.Tx, id VersionID, owner Owner, purpose Purpose) ([]byte, error) {
	if !validOwnerPurpose(owner, purpose) || id == "" {
		return nil, ErrUnavailable
	}
	e, err := scanEnvelope(tx.QueryRow(ctx, `SELECT v.version_id,v.owner_kind,v.owner_id,v.purpose,v.format_version,
		v.ciphertext,v.wrapped_dek,v.root_id,v.wrap_revision FROM repomesh_secrets.versions v
		JOIN repomesh_secrets.availability a USING(version_id) WHERE v.version_id=$1 AND a.enabled AND v.destroyed_at IS NULL FOR SHARE OF a`, string(id)))
	if err != nil || e.owner != owner || e.purpose != purpose {
		return nil, ErrUnavailable
	}
	root, ok := s.roots[e.rootID]
	if !ok {
		return nil, ErrUnavailable
	}
	plaintext, err := e.open(root[:])
	if err != nil {
		return nil, err
	}
	return plaintext, nil
}

func (s *Store) DestroyInTx(ctx context.Context, tx pgx.Tx, id VersionID, owner Owner, purpose Purpose) error {
	if !validOwnerPurpose(owner, purpose) || id == "" {
		return ErrUnavailable
	}
	destroyed, err := lockAvailability(ctx, tx, id)
	if err != nil {
		return err
	}
	var stored Owner
	var storedPurpose Purpose
	if err := tx.QueryRow(ctx, `SELECT owner_kind,owner_id,purpose FROM repomesh_secrets.versions WHERE version_id=$1`, string(id)).Scan(&stored.Kind, &stored.ID, &storedPurpose); err != nil {
		return ErrUnavailable
	}
	if stored != owner || storedPurpose != purpose {
		return ErrUnavailable
	}
	if destroyed {
		return nil
	}
	if _, err := tx.Exec(ctx, `UPDATE repomesh_secrets.versions SET ciphertext=NULL,wrapped_dek=NULL,destroyed_at=now(),wrap_revision=wrap_revision+1 WHERE version_id=$1`, string(id)); err != nil {
		return ErrUnavailable
	}
	if _, err := tx.Exec(ctx, `UPDATE repomesh_secrets.availability SET enabled=false,access_epoch=access_epoch+1,checked_at=now() WHERE version_id=$1`, string(id)); err != nil {
		return ErrUnavailable
	}
	return nil
}
