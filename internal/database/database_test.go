package database

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestMigrationManifest(t *testing.T) {
	for _, test := range []struct {
		name  string
		files fstest.MapFS
	}{
		{"missing", fstest.MapFS{}},
		{"starts at zero", fstest.MapFS{"migrations/0000_zero.sql": {Data: []byte("SELECT 1")}}},
		{"gap", fstest.MapFS{"migrations/0001_first.sql": {Data: []byte("SELECT 1")}, "migrations/0003_third.sql": {Data: []byte("SELECT 3")}}},
		{"duplicate version", fstest.MapFS{"migrations/0001_first.sql": {Data: []byte("SELECT 1")}, "migrations/0001_second.sql": {Data: []byte("SELECT 2")}}},
		{"invalid name", fstest.MapFS{"migrations/one.sql": {Data: []byte("SELECT 1")}}},
		{"empty SQL", fstest.MapFS{"migrations/0001_first.sql": {Data: []byte(" \n")}}},
		{"directory", fstest.MapFS{"migrations/0001_first.sql/child": {Data: []byte("SELECT 1")}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := loadMigrations(test.files); err == nil {
				t.Fatal("invalid manifest was accepted")
			}
		})
	}
	t.Run("ordered and checksummed", func(t *testing.T) {
		manifest, err := loadMigrations(fstest.MapFS{
			"migrations/0002_second.sql": {Data: []byte("SELECT 2;\n")},
			"migrations/0001_first.sql":  {Data: []byte("SELECT 1;\n")},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(manifest) != 2 || manifest[0].version != 1 || manifest[1].version != 2 ||
			manifest[0].name != "0001_first.sql" || manifest[0].sql != "SELECT 1;\n" ||
			manifest[0].checksum != sha256.Sum256([]byte("SELECT 1;\n")) {
			t.Fatal("manifest lost file order, identity, or exact bytes")
		}
	})
}

func TestOpenRejectsInvalidConfigurationWithoutSecrets(t *testing.T) {
	for _, input := range []string{
		"", " ", "postgres://account:very-secret-password@localhost:wrong-port/database",
		"host=localhost password='very-secret-password",
		"postgres://account:very-secret-password@localhost/database?pool_max_conns=not-a-number",
	} {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		db, err := Open(ctx, input)
		cancel()
		if db != nil || !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("invalid configuration result was db=%v, error=%v", db != nil, err)
		}
		if strings.Contains(err.Error(), "very-secret") || strings.Contains(err.Error(), input) && input != "" && input != " " {
			t.Fatal("configuration error exposed its input")
		}
	}
}

func TestOpenRequiresBoundedContextBeforeConnecting(t *testing.T) {
	db, err := Open(context.Background(), "postgres://localhost/example")
	if db != nil || err == nil || err.Error() != "database operation requires a context deadline" {
		t.Fatalf("unbounded open got db=%v, error=%v", db != nil, err)
	}
}

func TestSafeErrorPreservesOnlySafeDiagnostic(t *testing.T) {
	secret := "postgres://private-user:secret-password@private-host/private-db"
	for _, test := range []struct {
		name     string
		err      error
		expected string
		cause    error
	}{
		{"transport", errors.New(secret), "database connect failed (connection or protocol error)", nil},
		{"postgres", &pgconn.PgError{Code: "28P01", Message: secret, Detail: secret}, "database connect failed (SQLSTATE 28P01)", nil},
		{"malformed sqlstate", &pgconn.PgError{Code: secret, Message: secret}, "database connect failed (connection or protocol error)", nil},
		{"canceled", fmt.Errorf("%s: %w", secret, context.Canceled), "database connect: context canceled", context.Canceled},
		{"deadline", fmt.Errorf("%s: %w", secret, context.DeadlineExceeded), "database connect: context deadline exceeded", context.DeadlineExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := safeError("connect", test.err)
			if got.Error() != test.expected {
				t.Fatalf("got %q; want %q", got, test.expected)
			}
			if test.cause != nil && !errors.Is(got, test.cause) {
				t.Fatalf("lost cancellation cause %v", test.cause)
			}
			var raw *pgconn.PgError
			if errors.As(got, &raw) {
				t.Fatal("raw PostgreSQL error escaped the boundary")
			}
		})
	}
}
