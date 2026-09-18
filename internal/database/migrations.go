package database

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

const migrationLockKey int64 = 0x5245504f4d455348

var migrationFilename = regexp.MustCompile("^([0-9]{4})_([a-z][a-z0-9_]*)\\.sql$")

type migration struct {
	version  int64
	name     string
	sql      string
	checksum [sha256.Size]byte
}

func loadMigrations(source fs.FS) ([]migration, error) {
	entries, err := fs.ReadDir(source, "migrations")
	if err != nil || len(entries) == 0 {
		return nil, errors.New("embedded migration manifest is missing")
	}
	result := make([]migration, 0, len(entries))
	for index, entry := range entries {
		match := migrationFilename.FindStringSubmatch(entry.Name())
		if entry.IsDir() || match == nil {
			return nil, errors.New("embedded migration filename is invalid")
		}
		version, _ := strconv.ParseInt(match[1], 10, 64)
		if version != int64(index+1) {
			return nil, errors.New("embedded migration versions must be contiguous from one")
		}
		sql, err := fs.ReadFile(source, "migrations/"+entry.Name())
		if err != nil || strings.TrimSpace(string(sql)) == "" {
			return nil, errors.New("embedded migration SQL is missing")
		}
		result = append(result, migration{
			version: version, name: entry.Name(), sql: string(sql), checksum: sha256.Sum256(sql),
		})
	}
	return result, nil
}

func (db *DB) readHistory(ctx context.Context, tx pgx.Tx) (SchemaState, error) {
	state := SchemaState{Target: int64(len(db.migrations)), Pending: len(db.migrations)}
	var exists bool
	if err := tx.QueryRow(ctx, "SELECT pg_catalog.to_regclass('public.repomesh_schema_migrations') IS NOT NULL").Scan(&exists); err != nil {
		return SchemaState{}, safeError("inspect migration history", err)
	}
	if !exists {
		return state, nil
	}
	rows, err := tx.Query(ctx, "SELECT version, name, checksum FROM public.repomesh_schema_migrations ORDER BY version")
	if err != nil {
		return SchemaState{}, safeError("read migration history", err)
	}
	defer rows.Close()
	index := 0
	for rows.Next() {
		var version int64
		var name string
		var checksum []byte
		if err := rows.Scan(&version, &name, &checksum); err != nil {
			return SchemaState{}, safeError("decode migration history", err)
		}
		if index >= len(db.migrations) || version > state.Target {
			return SchemaState{}, fmt.Errorf("%w: unsupported version", ErrHistoryMismatch)
		}
		expected := db.migrations[index]
		if version != expected.version {
			return SchemaState{}, fmt.Errorf("%w: versions are not a contiguous prefix", ErrHistoryMismatch)
		}
		if name != expected.name || !bytes.Equal(checksum, expected.checksum[:]) {
			return SchemaState{}, fmt.Errorf("%w: name or checksum differs at version %d", ErrHistoryMismatch, version)
		}
		state.Current = version
		index++
	}
	if err := rows.Err(); err != nil {
		return SchemaState{}, safeError("read migration history", err)
	}
	if index == 0 {
		return SchemaState{}, fmt.Errorf("%w: ledger is missing its bootstrap version", ErrHistoryMismatch)
	}
	state.Pending -= index
	return state, nil
}

func (db *DB) Migrate(ctx context.Context) (SchemaState, error) {
	if err := requireDeadline(ctx); err != nil {
		return SchemaState{}, err
	}
	tx, err := db.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return SchemaState{}, safeError("begin migration", err)
	}
	defer rollback(tx)
	if _, err := tx.Exec(ctx, "SELECT pg_catalog.pg_advisory_xact_lock($1)", migrationLockKey); err != nil {
		return SchemaState{}, safeError("lock migrations", err)
	}
	// READ COMMITTED takes this snapshot after the preceding lock holder commits.
	state, err := db.readHistory(ctx, tx)
	if err != nil {
		return SchemaState{}, err
	}
	for _, migration := range db.migrations[state.Current:] {
		if _, err := tx.Exec(ctx, migration.sql); err != nil {
			return SchemaState{}, safeError("execute migration", err)
		}
		if _, err := tx.Exec(ctx,
			"INSERT INTO public.repomesh_schema_migrations (version, name, checksum) VALUES ($1, $2, $3)",
			migration.version, migration.name, migration.checksum[:]); err != nil {
			return SchemaState{}, safeError("record migration", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return SchemaState{}, errors.Join(ErrCommitUnconfirmed, safeError("commit migration", err))
	}
	return SchemaState{Current: state.Target, Target: state.Target, Pending: 0}, nil
}
