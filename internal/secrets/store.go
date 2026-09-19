package secrets

import (
	"fmt"
	"context"
	"crypto/sha256"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Owner struct{ Kind, ID string }
type Purpose string
type VersionID string
type RootFile struct{ ID, Path string }
type Config struct {
	ActiveRootID string
	Roots        []RootFile
}

const (
	AuthMaterial          Purpose = "auth-material"
	AuthExchangeResult    Purpose = "auth-exchange-result"
	GitHubUserToken       Purpose = "github-user-token"
	GitHubRefreshToken    Purpose = "github-refresh-token"
	GitHubAppClientSecret Purpose = "github-app-client-secret"
	GitHubAppPrivateKey   Purpose = "github-app-private-key"
	ModelProviderKey      Purpose = "model-provider-key"
	OperationInput        Purpose = "operation-input"
)

var (
	ErrConfiguration = errors.New("secret store configuration is invalid")
	ErrUnavailable   = errors.New("secret source is unavailable")
	ErrWrapLimit     = errors.New("secret wrapping requires a new root")
)

type Store struct {
	pool         *pgxpool.Pool
	activeRootID string
	roots        map[string][32]byte
}

func New(ctx context.Context, pool *pgxpool.Pool, config Config) (*Store, error) {
	if pool == nil || config.ActiveRootID == "" || len(config.Roots) == 0 {
		return nil, ErrConfiguration
	}
	s := &Store{pool: pool, activeRootID: config.ActiveRootID, roots: make(map[string][32]byte)}
	for _, file := range config.Roots {
		if file.ID == "" || len(file.ID) > 128 {
			return nil, ErrConfiguration
		}
		if _, exists := s.roots[file.ID]; exists {
			return nil, ErrConfiguration
		}
		key, err := readRootFile(file.Path)
		if err != nil {
			return nil, err
		}
		s.roots[file.ID] = [32]byte(key)
		clear(key)
	}
	if _, ok := s.roots[config.ActiveRootID]; !ok {
		return nil, ErrConfiguration
	}
	if err := s.registerRoots(ctx); err != nil {
		// 2026-09-20 事故教训：这里原来直接返回裸的 sentinel（ErrUnavailable /
		// ErrConfiguration），启动失败时日志只有一句「secret source is unavailable」，
		// 无从定位是哪一步、哪个表。带上步骤与因果链。
		return nil, fmt.Errorf("secret store register roots: %w", err)
	}
	if err := s.check(ctx); err != nil {
		return nil, fmt.Errorf("secret store self-check: %w", err)
	}
	return s, nil
}

func validOwnerPurpose(owner Owner, purpose Purpose) bool {
	if owner.Kind == "" || len(owner.Kind) > 128 || owner.ID == "" || len(owner.ID) > 256 {
		return false
	}
	switch purpose {
	case AuthMaterial, AuthExchangeResult, GitHubUserToken, GitHubRefreshToken, GitHubAppClientSecret, GitHubAppPrivateKey:
		return true
	case ModelProviderKey:
		return owner.Kind == "model-provider"
	case OperationInput:
		return owner.Kind == "provider-save-input"
	default:
		return false
	}
}

func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}

func (s *Store) reserveWrap(ctx context.Context) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ErrUnavailable
	}
	defer rollback(tx)
	if _, err := tx.Exec(ctx, "SET LOCAL synchronous_commit = on"); err != nil {
		return ErrUnavailable
	}
	key := s.roots[s.activeRootID]
	fingerprint := sha256.Sum256(key[:])
	var count int64
	err = tx.QueryRow(ctx, `UPDATE repomesh_secrets.root_keys SET wrap_count=wrap_count+1
		WHERE root_id=$1 AND fingerprint=$2 AND active_wrap AND wrap_count<1000000 RETURNING wrap_count`,
		s.activeRootID, fingerprint[:]).Scan(&count)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrWrapLimit
	}
	if err != nil {
		return ErrUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return ErrUnavailable
	}
	return nil
}

func (s *Store) requireActive(ctx context.Context, tx pgx.Tx) error {
	var active bool
	if err := tx.QueryRow(ctx, `SELECT active_wrap FROM repomesh_secrets.root_keys WHERE root_id=$1 FOR SHARE`, s.activeRootID).Scan(&active); err != nil || !active {
		return ErrUnavailable
	}
	return nil
}

func (s *Store) Seal(ctx context.Context, owner Owner, purpose Purpose, plaintext []byte) (VersionID, error) {
	prepared, err := s.Prepare(ctx, owner, purpose, plaintext)
	if err != nil {
		return "", err
	}
	defer prepared.Discard()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", ErrUnavailable
	}
	defer rollback(tx)
	id, err := s.InsertPrepared(ctx, tx, prepared)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", ErrUnavailable
	}
	return id, nil
}

func scanEnvelope(row pgx.Row) (envelope, error) {
	var e envelope
	err := row.Scan(&e.id, &e.owner.Kind, &e.owner.ID, &e.purpose, &e.format, &e.ciphertext, &e.wrappedDEK, &e.rootID, &e.revision)
	return e, err
}

func (s *Store) Open(ctx context.Context, id VersionID, owner Owner, purpose Purpose) ([]byte, error) {
	if !validOwnerPurpose(owner, purpose) || id == "" {
		return nil, ErrUnavailable
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer rollback(tx)
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
	if err := tx.Commit(ctx); err != nil {
		clear(plaintext)
		return nil, ErrUnavailable
	}
	return plaintext, nil
}

func (s *Store) SetEnabled(ctx context.Context, id VersionID, enabled bool) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ErrUnavailable
	}
	defer rollback(tx)
	destroyed, err := lockAvailability(ctx, tx, id)
	if err != nil {
		return err
	}
	if destroyed {
		return ErrUnavailable
	}
	result, err := tx.Exec(ctx, `UPDATE repomesh_secrets.availability SET enabled=$2,access_epoch=access_epoch+1,checked_at=now()
		WHERE version_id=$1`, string(id), enabled)
	if err != nil || result.RowsAffected() != 1 {
		return ErrUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return ErrUnavailable
	}
	return nil
}

func lockAvailability(ctx context.Context, tx pgx.Tx, id VersionID) (bool, error) {
	var stored string
	if err := tx.QueryRow(ctx, `SELECT version_id FROM repomesh_secrets.availability WHERE version_id=$1 FOR UPDATE`, string(id)).Scan(&stored); err != nil {
		return false, ErrUnavailable
	}
	var destroyed bool
	if err := tx.QueryRow(ctx, `SELECT destroyed_at IS NOT NULL FROM repomesh_secrets.versions WHERE version_id=$1`, string(id)).Scan(&destroyed); err != nil {
		return false, ErrUnavailable
	}
	return destroyed, nil
}

func (s *Store) Destroy(ctx context.Context, id VersionID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ErrUnavailable
	}
	defer rollback(tx)
	destroyed, err := lockAvailability(ctx, tx, id)
	if err != nil {
		return err
	}
	if !destroyed {
		if _, err := tx.Exec(ctx, `UPDATE repomesh_secrets.versions SET ciphertext=NULL,wrapped_dek=NULL,destroyed_at=now(),wrap_revision=wrap_revision+1 WHERE version_id=$1`, string(id)); err != nil {
			return ErrUnavailable
		}
		if _, err := tx.Exec(ctx, `UPDATE repomesh_secrets.availability SET enabled=false,access_epoch=access_epoch+1,checked_at=now() WHERE version_id=$1`, string(id)); err != nil {
			return ErrUnavailable
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return ErrUnavailable
	}
	return nil
}

func (s *Store) Rewrap(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 1000 {
		return 0, ErrUnavailable
	}
	rows, err := s.pool.Query(ctx, `SELECT version_id,owner_kind,owner_id,purpose,format_version,ciphertext,wrapped_dek,root_id,wrap_revision
		FROM repomesh_secrets.versions WHERE root_id<>$1 AND destroyed_at IS NULL ORDER BY version_id LIMIT $2`, s.activeRootID, limit)
	if err != nil {
		return 0, ErrUnavailable
	}
	var batch []envelope
	for rows.Next() {
		e, err := scanEnvelope(rows)
		if err != nil {
			rows.Close()
			return 0, ErrUnavailable
		}
		batch = append(batch, e)
	}
	rows.Close()
	if rows.Err() != nil {
		return 0, ErrUnavailable
	}
	completed := 0
	for _, e := range batch {
		oldRoot, ok := s.roots[e.rootID]
		if !ok {
			return completed, ErrUnavailable
		}
		if err := s.reserveWrap(ctx); err != nil {
			return completed, err
		}
		root := s.roots[s.activeRootID]
		oldRootID := e.rootID
		if err := e.rewrap(oldRoot[:], root[:], s.activeRootID); err != nil {
			return completed, err
		}
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return completed, ErrUnavailable
		}
		if err := s.requireActive(ctx, tx); err != nil {
			rollback(tx)
			return completed, err
		}
		result, err := tx.Exec(ctx, `UPDATE repomesh_secrets.versions SET wrapped_dek=$1,root_id=$2,wrap_revision=wrap_revision+1
			WHERE version_id=$3 AND root_id=$4 AND wrap_revision=$5 AND destroyed_at IS NULL`, e.wrappedDEK, e.rootID, string(e.id), oldRootID, e.revision)
		if err != nil {
			rollback(tx)
			return completed, ErrUnavailable
		}
		if err := tx.Commit(ctx); err != nil {
			rollback(tx)
			return completed, ErrUnavailable
		}
		completed += int(result.RowsAffected())
	}
	return completed, nil
}
