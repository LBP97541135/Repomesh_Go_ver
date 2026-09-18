package secrets

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type VersionInspection struct {
	Found         bool
	Enabled       bool
	Destroyed     bool
	RootAvailable bool
	AccessEpoch   int64
	CheckedAt     *time.Time
}

func (s *Store) InspectVersion(ctx context.Context, tx pgx.Tx, id VersionID, owner Owner, purpose Purpose) (VersionInspection, error) {
	if id == "" || owner.Kind == "" || owner.ID == "" || purpose == "" {
		return VersionInspection{}, nil
	}
	var result VersionInspection
	if err := tx.QueryRow(ctx, `SELECT enabled,access_epoch,checked_at
		FROM repomesh_secrets.availability WHERE version_id=$1 FOR SHARE`, string(id)).Scan(&result.Enabled, &result.AccessEpoch, &result.CheckedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return VersionInspection{}, nil
		}
		return VersionInspection{}, ErrUnavailable
	}
	var stored Owner
	var storedPurpose Purpose
	var rootID string
	var hasCiphertext, hasWrappedDEK bool
	if err := tx.QueryRow(ctx, `SELECT owner_kind,owner_id,purpose,root_id,destroyed_at IS NOT NULL,
		ciphertext IS NOT NULL,wrapped_dek IS NOT NULL
		FROM repomesh_secrets.versions WHERE version_id=$1`, string(id)).Scan(
		&stored.Kind, &stored.ID, &storedPurpose, &rootID, &result.Destroyed, &hasCiphertext, &hasWrappedDEK,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return VersionInspection{}, nil
		}
		return VersionInspection{}, ErrUnavailable
	}
	if stored != owner || storedPurpose != purpose {
		return VersionInspection{}, nil
	}
	result.Found = true
	_, result.RootAvailable = s.roots[rootID]
	if !hasCiphertext || !hasWrappedDEK {
		result.Destroyed = true
	}
	return result, nil
}
