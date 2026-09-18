package testdb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"repomesh.local/repomesh/internal/database"
)

func Open(t testing.TB) *pgxpool.Pool {
	t.Helper()
	pool, _ := OpenWithURL(t)
	return pool
}

func OpenWithURL(t testing.TB) (*pgxpool.Pool, string) {
	t.Helper()
	databaseURL := os.Getenv("REPOMESH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("REPOMESH_TEST_DATABASE_URL is unset; real PostgreSQL integration test skipped")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	config, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal("invalid test database configuration")
	}
	admin, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		t.Fatal("cannot connect test database administrator")
	}
	defer admin.Close(ctx)
	var suffix [12]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatal(err)
	}
	name := "repomesh_b02_it_" + hex.EncodeToString(suffix[:])
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal("cannot create isolated test database")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		admin, err := pgx.ConnectConfig(ctx, config)
		if err != nil {
			t.Error("cannot connect for isolated database cleanup")
			return
		}
		defer admin.Close(ctx)
		if _, err := admin.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Error("cannot drop owned isolated database")
		}
	})
	localURL := databaseURL + " dbname='" + name + "'"
	if strings.HasPrefix(databaseURL, "postgres://") || strings.HasPrefix(databaseURL, "postgresql://") {
		parsed, err := url.Parse(databaseURL)
		if err != nil {
			t.Fatal("invalid test database URL")
		}
		query := parsed.Query()
		query.Set("dbname", name)
		parsed.RawQuery = query.Encode()
		localURL = parsed.String()
	}
	db, err := database.Open(ctx, localURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if _, err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return db.Pool(), localURL
}
