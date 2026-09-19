package database

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestPostgresFreshCheckAndMigrationPersistence(t *testing.T) {
	databaseURL := isolatedDatabase(t)
	db := openTestDatabase(t, databaseURL)
	assertState(t, db, SchemaState{Current: 0, Target: 1, Pending: 1})
	if got := publicTableCount(t, db); got != 0 {
		t.Fatalf("read-only check created %d tables", got)
	}
	state, err := db.Migrate(testContext(t))
	if err != nil || state != (SchemaState{Current: 1, Target: 1, Pending: 0}) {
		t.Fatalf("first migration state=%+v error=%v", state, err)
	}
	history := historySnapshot(t, db)
	var created time.Time
	if err := db.pool.QueryRow(testContext(t), "SELECT applied_at FROM public.repomesh_schema_migrations WHERE version = 1").Scan(&created); err != nil {
		t.Fatal(safeError("read application time", err))
	}
	if created.IsZero() || publicTableCount(t, db) != 1 {
		t.Fatal("bootstrap did not create exactly one populated ledger")
	}
	db.Close()
	db = openTestDatabase(t, databaseURL)
	assertState(t, db, SchemaState{Current: 1, Target: 1, Pending: 0})
	if _, err := db.Migrate(testContext(t)); err != nil {
		t.Fatal(err)
	}
	if got := historySnapshot(t, db); got != history {
		t.Fatalf("repeat migration changed history: got %s; want %s", got, history)
	}
	var repeated time.Time
	if err := db.pool.QueryRow(testContext(t), "SELECT applied_at FROM public.repomesh_schema_migrations WHERE version = 1").Scan(&repeated); err != nil {
		t.Fatal(safeError("read repeated application time", err))
	}
	if !created.Equal(repeated) {
		t.Fatal("repeat migration replaced the original application time")
	}
}

func TestPostgresProductMigrationUpgrade(t *testing.T) {
	databaseURL := isolatedDatabase(t)
	db := openTestDatabase(t, databaseURL)
	if _, err := db.Migrate(testContext(t)); err != nil {
		t.Fatal(err)
	}
	db.Close()
	current, err := Open(testContext(t), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer current.Close()
	target := int64(len(current.migrations))
	assertState(t, current, SchemaState{Current: 1, Target: target, Pending: int(target) - 1})
	state, err := current.Migrate(testContext(t))
	if err != nil || state != (SchemaState{Current: target, Target: target, Pending: 0}) {
		t.Fatal(state, err)
	}
	var tables int
	err = current.pool.QueryRow(testContext(t), `SELECT count(*) FROM information_schema.tables WHERE table_schema IN ('repomesh_access','repomesh_secrets')`).Scan(&tables)
	// 14 = 原 13 张 + 0045 的 repomesh_access.participation_observations
	//（参与权探测缓存，2026-09-18 主线并入）。
	if err != nil || tables != 14 {
		t.Fatalf("product tables=%d error=%v", tables, err)
	}
	before := historySnapshot(t, current)
	if _, err = current.Migrate(testContext(t)); err != nil {
		t.Fatal(err)
	}
	if historySnapshot(t, current) != before {
		t.Fatal("repeat product migration changed history")
	}
}

func TestPostgresTwentyConcurrentMigrations(t *testing.T) {
	db := openTestDatabase(t, isolatedDatabase(t))
	start := make(chan struct{})
	results := make(chan error, 20)
	ctx := testContext(t)
	var workers sync.WaitGroup
	for range 20 {
		workers.Go(func() {
			<-start
			state, err := db.Migrate(ctx)
			if err == nil && state != (SchemaState{Current: 1, Target: 1, Pending: 0}) {
				err = fmt.Errorf("unexpected migrated state %+v", state)
			}
			results <- err
		})
	}
	close(start)
	workers.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := db.pool.QueryRow(ctx, "SELECT count(*) FROM public.repomesh_schema_migrations").Scan(&count); err != nil {
		t.Fatal(safeError("count migration history", err))
	}
	if count != 1 || publicTableCount(t, db) != 1 {
		t.Fatalf("concurrent migration produced %d ledger rows", count)
	}
}

func TestPostgresSQLFailureRollsBackBootstrapAndDDL(t *testing.T) {
	db := openTestDatabase(t, isolatedDatabase(t))
	appendTestMigration(db, "CREATE TABLE public.rollback_probe (id integer); SELECT no_such_function_b01();")
	if _, err := db.Migrate(testContext(t)); err == nil {
		t.Fatal("invalid SQL was accepted")
	}
	assertState(t, db, SchemaState{Current: 0, Target: 2, Pending: 2})
	if got := publicTableCount(t, db); got != 0 {
		t.Fatalf("failed transaction left %d tables", got)
	}
}

func TestPostgresHistoryInsertFailureRollsBackDDL(t *testing.T) {
	db := openTestDatabase(t, isolatedDatabase(t))
	appendTestMigration(db, "CREATE TABLE public.history_insert_probe (id integer); INSERT INTO public.repomesh_schema_migrations (version, name, checksum) VALUES (2, 'occupied', decode(repeat('00', 32), 'hex'));")
	if _, err := db.Migrate(testContext(t)); err == nil {
		t.Fatal("conflicting ledger insert was accepted")
	}
	if got := publicTableCount(t, db); got != 0 {
		t.Fatalf("ledger failure left %d tables", got)
	}
}

func TestPostgresAtomicMultipleFileUpgrade(t *testing.T) {
	if os.Getenv("REPOMESH_TEST_DATABASE_URL") == "" {
		t.Skip("REPOMESH_TEST_DATABASE_URL is unset; real PostgreSQL integration test skipped")
	}
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprintf("fail=%t", fail), func(t *testing.T) {
			db := openTestDatabase(t, isolatedDatabase(t))
			if _, err := db.Migrate(testContext(t)); err != nil {
				t.Fatal(err)
			}
			originalHistory := historySnapshot(t, db)
			appendTestMigration(db, "CREATE TABLE public.upgrade_second (id integer PRIMARY KEY);")
			lastSQL := "CREATE TABLE public.upgrade_third (id integer PRIMARY KEY);"
			if fail {
				lastSQL += " SELECT no_such_upgrade_function_b01();"
			}
			appendTestMigration(db, lastSQL)
			assertState(t, db, SchemaState{Current: 1, Target: 3, Pending: 2})
			state, err := db.Migrate(testContext(t))
			if fail {
				if err == nil || historySnapshot(t, db) != originalHistory || publicTableCount(t, db) != 1 {
					t.Fatal("failed upgrade changed an earlier committed prefix or left pending DDL")
				}
				assertState(t, db, SchemaState{Current: 1, Target: 3, Pending: 2})
			} else if err != nil || state != (SchemaState{Current: 3, Target: 3, Pending: 0}) || publicTableCount(t, db) != 3 {
				t.Fatalf("atomic upgrade state=%+v error=%v", state, err)
			}
		})
	}
}

func TestPostgresHistoryMismatchIsRejectedWithoutWrites(t *testing.T) {
	if os.Getenv("REPOMESH_TEST_DATABASE_URL") == "" {
		t.Skip("REPOMESH_TEST_DATABASE_URL is unset; real PostgreSQL integration test skipped")
	}
	for _, test := range []struct {
		name       string
		corruptSQL string
	}{
		{"future version", "INSERT INTO public.repomesh_schema_migrations(version,name,checksum) VALUES (4,'future',decode(repeat('00',32),'hex'))"},
		{"missing middle", "DELETE FROM public.repomesh_schema_migrations WHERE version = 2"},
		{"changed name", "UPDATE public.repomesh_schema_migrations SET name = 'secret-name-must-not-escape' WHERE version = 1"},
		{"changed checksum", "UPDATE public.repomesh_schema_migrations SET checksum = decode(repeat('ff',32),'hex') WHERE version = 1"},
		{"missing bootstrap", "DELETE FROM public.repomesh_schema_migrations"},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := openTestDatabase(t, isolatedDatabase(t))
			appendTestMigration(db, "CREATE TABLE public.mismatch_second (id integer);")
			appendTestMigration(db, "CREATE TABLE public.mismatch_third (id integer);")
			if _, err := db.Migrate(testContext(t)); err != nil {
				t.Fatal(err)
			}
			execTestSQL(t, db, test.corruptSQL)
			before := historySnapshot(t, db)
			for _, operation := range []func(context.Context) (SchemaState, error){db.Check, db.Migrate} {
				if _, err := operation(testContext(t)); !errors.Is(err, ErrHistoryMismatch) {
					t.Fatalf("corrupted history got %v; want history mismatch", err)
				} else if strings.Contains(err.Error(), "secret-name") {
					t.Fatal("history diagnostic exposed a stored name")
				}
				if historySnapshot(t, db) != before || publicTableCount(t, db) != 3 {
					t.Fatal("history rejection wrote to the database")
				}
			}
		})
	}
}

func TestPostgresCancellationDuringSQLRollsBack(t *testing.T) {
	db := openTestDatabase(t, isolatedDatabase(t))
	appendTestMigration(db, "CREATE TABLE public.cancel_during_sql (id integer); SELECT pg_catalog.pg_sleep(30);")
	ctx, cancel := context.WithCancel(testContext(t))
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := db.Migrate(ctx); result <- err }()
	waitForDatabaseCondition(t, db, "SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_stat_activity WHERE datname = current_database() AND state = 'active' AND query LIKE '%cancel_during_sql%' AND pid <> pg_backend_pid())")
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled SQL got %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("canceled SQL did not return")
	}
	assertState(t, db, SchemaState{Current: 0, Target: 2, Pending: 2})
	if got := publicTableCount(t, db); got != 0 {
		t.Fatalf("canceled transaction left %d tables", got)
	}
}

func TestPostgresCancellationDuringAdvisoryLock(t *testing.T) {
	db := openTestDatabase(t, isolatedDatabase(t))
	tx, err := db.pool.Begin(testContext(t))
	if err != nil {
		t.Fatal(safeError("begin lock holder", err))
	}
	defer rollback(tx)
	if _, err := tx.Exec(testContext(t), "SELECT pg_catalog.pg_advisory_xact_lock($1)", migrationLockKey); err != nil {
		t.Fatal(safeError("hold migration lock", err))
	}
	ctx, cancel := context.WithCancel(testContext(t))
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := db.Migrate(ctx); result <- err }()
	waitForDatabaseCondition(t, db, "SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_locks WHERE locktype = 'advisory' AND NOT granted AND database = (SELECT oid FROM pg_catalog.pg_database WHERE datname = current_database()))")
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled advisory lock got %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("canceled lock wait did not return")
	}
	rollback(tx)
	assertState(t, db, SchemaState{Current: 0, Target: 1, Pending: 1})
	if got := publicTableCount(t, db); got != 0 {
		t.Fatalf("canceled lock wait left %d tables", got)
	}
	if _, err := db.Migrate(testContext(t)); err != nil {
		t.Fatalf("lock was not released for a subsequent migration: %v", err)
	}
}

func TestPostgresDeadlineDuringAdvisoryLock(t *testing.T) {
	db := openTestDatabase(t, isolatedDatabase(t))
	tx, err := db.pool.Begin(testContext(t))
	if err != nil {
		t.Fatal(safeError("begin lock holder", err))
	}
	defer rollback(tx)
	if _, err := tx.Exec(testContext(t), "SELECT pg_catalog.pg_advisory_xact_lock($1)", migrationLockKey); err != nil {
		t.Fatal(safeError("hold migration lock", err))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if _, err := db.Migrate(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expired lock deadline got %v", err)
	}
	rollback(tx)
	assertState(t, db, SchemaState{Current: 0, Target: 1, Pending: 1})
}

func TestPostgresInvalidCredentialsAreRedacted(t *testing.T) {
	databaseURL := isolatedDatabase(t)
	secret := "b01-password-that-must-not-appear"
	invalidURL := withDatabaseParameter(databaseURL, "password", secret)
	db, err := Open(testContext(t), invalidURL)
	if err == nil {
		db.Close()
		t.Fatal("incorrect password was accepted; integration server must enforce password authentication")
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), invalidURL) ||
		!strings.Contains(err.Error(), "SQLSTATE 28P01") {
		t.Fatalf("expected safe authentication SQLSTATE, got %v", err)
	}
}

func TestPostgresDatabasesAreIsolated(t *testing.T) {
	firstURL, secondURL := isolatedDatabase(t), isolatedDatabase(t)
	first, second := openTestDatabase(t, firstURL), openTestDatabase(t, secondURL)
	if firstURL == secondURL {
		t.Fatal("independent test databases received the same identity")
	}
	execTestSQL(t, first, "CREATE TABLE public.isolation_probe (id integer)")
	if _, err := first.Migrate(testContext(t)); err != nil {
		t.Fatal(err)
	}
	assertState(t, second, SchemaState{Current: 0, Target: 1, Pending: 1})
	if publicTableCount(t, second) != 0 {
		t.Fatal("tables crossed the test database boundary")
	}
}

func isolatedDatabase(t *testing.T) string {
	t.Helper()
	adminURL := os.Getenv("REPOMESH_TEST_DATABASE_URL")
	if adminURL == "" {
		t.Skip("REPOMESH_TEST_DATABASE_URL is unset; real PostgreSQL integration test skipped")
	}
	adminConfig, err := pgx.ParseConfig(adminURL)
	if err != nil {
		t.Fatal("invalid REPOMESH_TEST_DATABASE_URL")
	}
	var suffix [12]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatal("cannot generate isolated database identity")
	}
	name := "repomesh_b01_it_" + hex.EncodeToString(suffix[:])
	admin, err := pgx.ConnectConfig(testContext(t), adminConfig.Copy())
	if err != nil {
		t.Fatal(safeError("connect test administrator", err))
	}
	_, createErr := admin.Exec(testContext(t), "CREATE DATABASE "+pgx.Identifier{name}.Sanitize())
	_ = admin.Close(testContext(t))
	if createErr != nil {
		t.Fatal(safeError("create isolated test database", createErr))
	}
	t.Cleanup(func() {
		if !strings.HasPrefix(name, "repomesh_b01_it_") || len(name) != len("repomesh_b01_it_")+24 {
			t.Error("refused cleanup of an unexpected database identity")
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		admin, err := pgx.ConnectConfig(ctx, adminConfig.Copy())
		if err != nil {
			t.Error(safeError("connect for test cleanup", err))
			return
		}
		defer admin.Close(ctx)
		if _, err := admin.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Error(safeError("drop isolated test database", err))
		}
	})
	return withDatabaseParameter(adminURL, "dbname", name)
}

func withDatabaseParameter(input, key, value string) string {
	if strings.HasPrefix(input, "postgres://") || strings.HasPrefix(input, "postgresql://") {
		parsed, _ := url.Parse(input)
		query := parsed.Query()
		query.Set(key, value)
		parsed.RawQuery = query.Encode()
		return parsed.String()
	}
	quoted := strings.ReplaceAll(strings.ReplaceAll(value, "\\", "\\\\"), "'", "\\'")
	return input + " " + key + "='" + quoted + "'"
}

func openTestDatabase(t *testing.T, databaseURL string) *DB {
	t.Helper()
	db, err := Open(testContext(t), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	db.migrations = db.migrations[:1]
	t.Cleanup(db.Close)
	return db
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func appendTestMigration(db *DB, sql string) {
	version := int64(len(db.migrations) + 1)
	db.migrations = append(db.migrations, migration{
		version:  version,
		name:     fmt.Sprintf("%04d_test.sql", version),
		sql:      sql,
		checksum: sha256.Sum256([]byte(sql)),
	})
}

func assertState(t *testing.T, db *DB, expected SchemaState) {
	t.Helper()
	got, err := db.Check(testContext(t))
	if err != nil || got != expected {
		t.Fatalf("schema state=%+v error=%v; want %+v", got, err, expected)
	}
}

func execTestSQL(t *testing.T, db *DB, sql string) {
	t.Helper()
	if _, err := db.pool.Exec(testContext(t), sql); err != nil {
		t.Fatal(safeError("execute test SQL", err))
	}
}

func publicTableCount(t *testing.T, db *DB) int {
	t.Helper()
	var count int
	if err := db.pool.QueryRow(testContext(t), "SELECT count(*) FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'public' AND c.relkind IN ('r', 'p')").Scan(&count); err != nil {
		t.Fatal(safeError("inspect test tables", err))
	}
	return count
}

func historySnapshot(t *testing.T, db *DB) string {
	t.Helper()
	var snapshot string
	if err := db.pool.QueryRow(testContext(t), "SELECT COALESCE(jsonb_agg(jsonb_build_array(version, name, encode(checksum, 'hex')) ORDER BY version)::text, '[]') FROM public.repomesh_schema_migrations").Scan(&snapshot); err != nil {
		t.Fatal(safeError("snapshot migration history", err))
	}
	return snapshot
}

func waitForDatabaseCondition(t *testing.T, db *DB, sql string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var ready bool
		if err := db.pool.QueryRow(ctx, sql).Scan(&ready); err != nil {
			t.Fatal(safeError("observe migration stage", err))
		}
		if ready {
			return
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("migration did not reach the expected PostgreSQL stage")
		}
	}
}

func TestPostgresCommitFailureNeverReportsSuccess(t *testing.T) {
	db := openTestDatabase(t, isolatedDatabase(t))
	if _, err := db.Migrate(testContext(t)); err != nil {
		t.Fatal(err)
	}
	before := historySnapshot(t, db)
	appendTestMigration(db, "CREATE TABLE public.commit_failure_probe (id integer PRIMARY KEY, parent integer REFERENCES public.commit_failure_probe(id) DEFERRABLE INITIALLY DEFERRED); INSERT INTO public.commit_failure_probe(id, parent) VALUES (1, 2);")
	state, err := db.Migrate(testContext(t))
	if !errors.Is(err, ErrCommitUnconfirmed) || state != (SchemaState{}) {
		t.Fatalf("commit failure returned state=%+v error=%v", state, err)
	}
	if historySnapshot(t, db) != before || publicTableCount(t, db) != 1 {
		t.Fatal("commit failure left a partial schema or advanced the history")
	}
	assertState(t, db, SchemaState{Current: 1, Target: 2, Pending: 1})
}
