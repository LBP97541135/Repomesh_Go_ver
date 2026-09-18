package database

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrInvalidConfig     = errors.New("invalid database configuration")
	ErrHistoryMismatch   = errors.New("database migration history does not match this binary")
	ErrCommitUnconfirmed = errors.New("migration commit result is unconfirmed; run db check before retrying")
)

type SchemaState struct {
	Current int64
	Target  int64
	Pending int
}

type DB struct {
	pool       *pgxpool.Pool
	migrations []migration
}

func Open(ctx context.Context, databaseURL string) (*DB, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, fmt.Errorf("%w: a connection string is required", ErrInvalidConfig)
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, ErrInvalidConfig
	}
	if err := requireDeadline(ctx); err != nil {
		return nil, err
	}
	migrations, err := loadMigrations(migrationFiles)
	if err != nil {
		return nil, err
	}
	config.ConnConfig.RuntimeParams["search_path"] = "pg_catalog"
	config.AfterConnect = func(_ context.Context, conn *pgx.Conn) error {
		conn.TypeMap().RegisterType(&pgtype.Type{
			Name:  "timestamptz",
			OID:   pgtype.TimestamptzOID,
			Codec: &pgtype.TimestamptzCodec{ScanLocation: time.UTC},
		})
		return nil
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, safeError("open", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, safeError("connect", err)
	}
	return &DB{pool: pool, migrations: migrations}, nil
}

func (db *DB) Close() {
	db.pool.Close()
}

func (db *DB) Pool() *pgxpool.Pool {
	return db.pool
}

func (db *DB) Check(ctx context.Context) (SchemaState, error) {
	if err := requireDeadline(ctx); err != nil {
		return SchemaState{}, err
	}
	tx, err := db.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return SchemaState{}, safeError("begin schema check", err)
	}
	defer rollback(tx)
	state, err := db.readHistory(ctx, tx)
	if err != nil {
		return SchemaState{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SchemaState{}, safeError("finish schema check", err)
	}
	return state, nil
}

func requireDeadline(ctx context.Context) error {
	if _, ok := ctx.Deadline(); !ok {
		return errors.New("database operation requires a context deadline")
	}
	return nil
}

func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}

func safeError(operation string, err error) error {
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("database %s: %w", operation, context.Canceled)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("database %s: %w", operation, context.DeadlineExceeded)
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && validSQLState(postgresError.Code) {
		return fmt.Errorf("database %s failed (SQLSTATE %s)", operation, postgresError.Code)
	}
	return fmt.Errorf("database %s failed (connection or protocol error)", operation)
}

func validSQLState(code string) bool {
	if len(code) != 5 {
		return false
	}
	for _, r := range code {
		if !(r >= '0' && r <= '9' || r >= 'A' && r <= 'Z') {
			return false
		}
	}
	return true
}
