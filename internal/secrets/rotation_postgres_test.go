//go:build unix

package secrets

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"repomesh.local/repomesh/internal/testdb"
)

type rotationQueryGate struct {
	match   func(string) bool
	after   bool
	reached chan struct{}
	release <-chan struct{}
	once    sync.Once
}

type rotationQueryKey struct{}

func (g *rotationQueryGate) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if g.match(data.SQL) {
		if g.after {
			return context.WithValue(ctx, rotationQueryKey{}, true)
		}
		g.pause(ctx)
	}
	return ctx
}

func (g *rotationQueryGate) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if ctx.Value(rotationQueryKey{}) == true && data.Err == nil {
		g.pause(ctx)
	}
}

func (g *rotationQueryGate) pause(ctx context.Context) {
	g.once.Do(func() {
		close(g.reached)
		if g.release != nil {
			select {
			case <-g.release:
			case <-ctx.Done():
			}
		}
	})
}

func tracedRotationPool(t *testing.T, pool *pgxpool.Pool, tracer pgx.QueryTracer) *pgxpool.Pool {
	t.Helper()
	config := pool.Config()
	config.ConnConfig.Tracer = tracer
	traced, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal("create traced rotation pool failed")
	}
	t.Cleanup(traced.Close)
	return traced
}

func TestPostgresSecretRotationWaitsForExistingWriter(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	a, b, c := rootFile(t, "root-a"), rootFile(t, "root-b"), rootFile(t, "root-c")
	storeFor(t, pool, Config{ActiveRootID: a.ID, Roots: []RootFile{a}})
	releaseB := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseB) }) }
	defer release()
	bGate := &rotationQueryGate{
		match: func(sql string) bool {
			return strings.Contains(sql, "SELECT active_wrap FROM repomesh_secrets.root_keys") && strings.Contains(sql, "FOR SHARE")
		},
		after: true, reached: make(chan struct{}), release: releaseB,
	}
	bPool := tracedRotationPool(t, pool, bGate)
	type result struct {
		s   *Store
		err error
	}
	bResult := make(chan result, 1)
	go func() {
		s, err := New(ctx, bPool, Config{ActiveRootID: b.ID, Roots: []RootFile{a, b}})
		bResult <- result{s, err}
	}()
	select {
	case <-bGate.reached:
	case <-ctx.Done():
		t.Fatal("B did not reach self-check publication")
	}
	cGate := &rotationQueryGate{
		match: func(sql string) bool {
			return strings.Contains(sql, "WHERE active_wrap FOR UPDATE") || strings.Contains(sql, "SET active_wrap=false")
		},
		reached: make(chan struct{}),
	}
	cPool := tracedRotationPool(t, pool, cGate)
	cResult := make(chan error, 1)
	go func() { _, err := New(ctx, cPool, Config{ActiveRootID: c.ID, Roots: []RootFile{a, c}}); cResult <- err }()
	select {
	case <-cGate.reached:
	case <-ctx.Done():
		t.Fatal("C did not reach the active-root barrier")
	}
	release()
	var initializedB result
	select {
	case initializedB = <-bResult:
	case <-ctx.Done():
		t.Fatal("B initialization did not finish")
	}
	if initializedB.err != nil {
		t.Fatalf("B initialization failed: %v", initializedB.err)
	}
	select {
	case err := <-cResult:
		if err == nil {
			t.Fatal("C accepted a configuration missing the referenced B root")
		}
	case <-ctx.Done():
		t.Fatal("C initialization did not finish")
	}
	var active string
	if err := pool.QueryRow(ctx, `SELECT root_id FROM repomesh_secrets.root_keys WHERE active_wrap`).Scan(&active); err != nil {
		t.Fatal("read active root failed")
	}
	if active != b.ID {
		t.Fatalf("failed C initialization left active root %q, want %q", active, b.ID)
	}
	if _, err := initializedB.s.Seal(ctx, Owner{Kind: "actor", ID: "alice"}, GitHubUserToken, []byte("rotation-survivor")); err != nil {
		t.Fatalf("B lost wrapping ability after rejected C configuration: %v", err)
	}
}
