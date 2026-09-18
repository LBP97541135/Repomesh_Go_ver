//go:build unix

package secrets

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"repomesh.local/repomesh/internal/testdb"
)

func rootFile(t *testing.T, id string) RootFile {
	t.Helper()
	key := make([]byte, 32)
	rand.Read(key)
	path := filepath.Join(t.TempDir(), "root")
	if err := os.WriteFile(path, key, 0600); err != nil {
		t.Fatal(err)
	}
	return RootFile{ID: id, Path: path}
}

func storeFor(t *testing.T, pool *pgxpool.Pool, config Config) *Store {
	t.Helper()
	s, err := New(context.Background(), pool, config)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func wrapCount(t *testing.T, pool *pgxpool.Pool, id string) int64 {
	t.Helper()
	var count int64
	if err := pool.QueryRow(context.Background(), `SELECT wrap_count FROM repomesh_secrets.root_keys WHERE root_id=$1`, id).Scan(&count); err != nil {
		t.Fatal("read wrap count failed")
	}
	return count
}

func TestPostgresSecretLifecycleAndRotation(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	first := rootFile(t, "root-a")
	config := Config{ActiveRootID: first.ID, Roots: []RootFile{first}}
	s := storeFor(t, pool, config)
	if n := wrapCount(t, pool, first.ID); n != 1 {
		t.Fatalf("self-check wraps = %d, want 1", n)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM repomesh_secrets.versions`).Scan(&n); err != nil || n != 0 {
		t.Fatal("self-check created a user secret version")
	}
	owner := Owner{Kind: "actor", ID: "alice"}
	plain := []byte("private-persistent-github-token")
	id, err := s.Seal(ctx, owner, GitHubUserToken, plain)
	if err != nil {
		t.Fatal(err)
	}
	var body, wrapper []byte
	if err := pool.QueryRow(ctx, `SELECT ciphertext,wrapped_dek FROM repomesh_secrets.versions WHERE version_id=$1`, string(id)).Scan(&body, &wrapper); err != nil {
		t.Fatal("read ciphertext failed")
	}
	if bytes.Contains(body, plain) || bytes.Contains(wrapper, plain) {
		t.Fatal("database contains plaintext")
	}
	if got, err := s.Open(ctx, id, owner, GitHubUserToken); err != nil || !bytes.Equal(got, plain) {
		t.Fatal("stored token did not round trip")
	}
	if got, err := s.Open(ctx, id, Owner{Kind: "actor", ID: "bob"}, GitHubUserToken); err == nil || got != nil {
		t.Fatal("wrong owner decrypted token")
	}
	if got, err := s.Open(ctx, id, owner, GitHubRefreshToken); err == nil || got != nil {
		t.Fatal("wrong purpose decrypted token")
	}
	if err := s.SetEnabled(ctx, id, false); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Open(ctx, id, owner, GitHubUserToken); err == nil || got != nil {
		t.Fatal("disabled token decrypted")
	}
	if err := s.SetEnabled(ctx, id, true); err != nil {
		t.Fatal(err)
	}
	var epoch int64
	if err := pool.QueryRow(ctx, `SELECT access_epoch FROM repomesh_secrets.availability WHERE version_id=$1`, string(id)).Scan(&epoch); err != nil || epoch != 3 {
		t.Fatal("availability changes did not advance epoch")
	}
	restarted := storeFor(t, pool, config)
	if got, err := restarted.Open(ctx, id, owner, GitHubUserToken); err != nil || !bytes.Equal(got, plain) {
		t.Fatal("restart lost token")
	}
	if n := wrapCount(t, pool, first.ID); n != 2 {
		t.Fatalf("restart changed wrap count to %d", n)
	}
	second := rootFile(t, "root-b")
	rotated := storeFor(t, pool, Config{ActiveRootID: second.ID, Roots: []RootFile{first, second}})
	if _, err := s.Seal(ctx, owner, GitHubUserToken, plain); !errors.Is(err, ErrWrapLimit) {
		t.Fatal("stale process wrapped with retired root")
	}
	if _, err := New(ctx, pool, config); err == nil {
		t.Fatal("stale configuration reactivated retired root")
	}
	if got, err := rotated.Open(ctx, id, owner, GitHubUserToken); err != nil || !bytes.Equal(got, plain) {
		t.Fatal("old root could not unwrap during rotation")
	}
	if done, err := rotated.Rewrap(ctx, 100); err != nil || done != 1 {
		t.Fatalf("rewrap completed %d, error %v", done, err)
	}
	if done, err := rotated.Rewrap(ctx, 100); err != nil || done != 0 {
		t.Fatal("repeated rewrap changed completed versions")
	}
	var newBody, newWrapper []byte
	var newRoot string
	if err := pool.QueryRow(ctx, `SELECT ciphertext,wrapped_dek,root_id FROM repomesh_secrets.versions WHERE version_id=$1`, string(id)).Scan(&newBody, &newWrapper, &newRoot); err != nil {
		t.Fatal("read rewrapped version failed")
	}
	if !bytes.Equal(body, newBody) || bytes.Equal(wrapper, newWrapper) || newRoot != second.ID {
		t.Fatal("rotation changed body or failed to replace wrapper")
	}
	if wrapCount(t, pool, first.ID) != 2 || wrapCount(t, pool, second.ID) != 2 {
		t.Fatal("rotation did not account for self-check and version wraps")
	}
	withoutOld := storeFor(t, pool, Config{ActiveRootID: second.ID, Roots: []RootFile{second}})
	if got, err := withoutOld.Open(ctx, id, owner, GitHubUserToken); err != nil || !bytes.Equal(got, plain) {
		t.Fatal("rewrapped token still requires old root")
	}
}

func TestPostgresSecretConcurrentWrapLimit(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	root := rootFile(t, "root-a")
	config := Config{ActiveRootID: root.ID, Roots: []RootFile{root}}
	stores := []*Store{storeFor(t, pool, config), storeFor(t, pool, config)}
	if _, err := pool.Exec(ctx, `UPDATE repomesh_secrets.root_keys SET wrap_count=999990 WHERE root_id=$1`, root.ID); err != nil {
		t.Fatal("prepare limit failed")
	}
	var succeeded, refused atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := stores[i%len(stores)].Seal(ctx, Owner{Kind: "actor", ID: "alice"}, GitHubUserToken, []byte("quota-test-token"))
			if err == nil {
				succeeded.Add(1)
			} else if errors.Is(err, ErrWrapLimit) {
				refused.Add(1)
			} else {
				t.Error("unexpected seal failure")
			}
		}(i)
	}
	wg.Wait()
	if succeeded.Load() != 10 || refused.Load() != 22 {
		t.Fatalf("success/refusal = %d/%d, want 10/22", succeeded.Load(), refused.Load())
	}
	if count := wrapCount(t, pool, root.ID); count != 1000000 {
		t.Fatalf("wrap count = %d, want 1000000", count)
	}
	storeFor(t, pool, config)
	if count := wrapCount(t, pool, root.ID); count != 1000000 {
		t.Fatal("restart reset exhausted wrap counter")
	}
}

func TestPostgresSecretFailedInsertKeepsReservation(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	root := rootFile(t, "root-a")
	config := Config{ActiveRootID: root.ID, Roots: []RootFile{root}}
	s := storeFor(t, pool, config)
	_, err := pool.Exec(ctx, `CREATE FUNCTION repomesh_secrets.reject_availability() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'private-database-error-sentinel'; END; $$;
		CREATE TRIGGER reject_availability BEFORE INSERT ON repomesh_secrets.availability
		FOR EACH ROW EXECUTE FUNCTION repomesh_secrets.reject_availability()`)
	if err != nil {
		t.Fatal("prepare rollback trigger failed")
	}
	id, err := s.Seal(ctx, Owner{Kind: "actor", ID: "alice"}, GitHubUserToken, []byte("private-token-sentinel"))
	if err == nil || id != "" {
		t.Fatal("failed save returned a usable secret version")
	}
	if strings.Contains(err.Error(), "sentinel") {
		t.Fatal("sensitive failure details escaped")
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM repomesh_secrets.versions`).Scan(&count); err != nil || count != 0 {
		t.Fatal("failed availability insert left a partial version")
	}
	if wrapCount(t, pool, root.ID) != 2 {
		t.Fatal("business rollback refunded wrapping reservation")
	}
	storeFor(t, pool, config)
	if wrapCount(t, pool, root.ID) != 2 {
		t.Fatal("restart lost failed-save reservation")
	}
}

func TestPostgresSecretRootIdentityAndSelfCheck(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	root := rootFile(t, "root-a")
	config := Config{ActiveRootID: root.ID, Roots: []RootFile{root}}
	storeFor(t, pool, config)
	wrong := rootFile(t, root.ID)
	if _, err := New(ctx, pool, Config{ActiveRootID: root.ID, Roots: []RootFile{wrong}}); err == nil {
		t.Fatal("changed root fingerprint was accepted")
	}
	alias := root
	alias.ID = "root-alias"
	if _, err := New(ctx, pool, Config{ActiveRootID: alias.ID, Roots: []RootFile{root, alias}}); err == nil {
		t.Fatal("root alias reset cryptographic usage quota")
	}
	if _, err := pool.Exec(ctx, `UPDATE repomesh_secrets.self_check SET ciphertext=set_byte(ciphertext,5,get_byte(ciphertext,5)#1)`); err != nil {
		t.Fatal("tamper self-check failed")
	}
	if _, err := New(ctx, pool, config); err == nil {
		t.Fatal("corrupt known ciphertext passed startup self-check")
	}
	if wrapCount(t, pool, root.ID) != 1 {
		t.Fatal("failed validation changed existing root counter")
	}
}

func TestPostgresSecretAuthoritativeAAD(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	root := rootFile(t, "root-a")
	other := rootFile(t, "root-b")
	s := storeFor(t, pool, Config{ActiveRootID: root.ID, Roots: []RootFile{root, other}})
	owner := Owner{Kind: "actor", ID: "alice"}
	cases := []struct {
		name, sql string
		owner     Owner
		purpose   Purpose
	}{
		{"owner", `UPDATE repomesh_secrets.versions SET owner_id='bob' WHERE version_id=$1`, Owner{Kind: "actor", ID: "bob"}, GitHubUserToken},
		{"purpose", `UPDATE repomesh_secrets.versions SET purpose='github-refresh-token' WHERE version_id=$1`, owner, GitHubRefreshToken},
		{"root", `UPDATE repomesh_secrets.versions SET root_id='root-b' WHERE version_id=$1`, owner, GitHubUserToken},
		{"body", `UPDATE repomesh_secrets.versions SET ciphertext=set_byte(ciphertext,5,get_byte(ciphertext,5)#1) WHERE version_id=$1`, owner, GitHubUserToken},
		{"wrapper", `UPDATE repomesh_secrets.versions SET wrapped_dek=set_byte(wrapped_dek,5,get_byte(wrapped_dek,5)#1) WHERE version_id=$1`, owner, GitHubUserToken},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, err := s.Seal(ctx, owner, GitHubUserToken, []byte("private-authority-token"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, tc.sql, string(id)); err != nil {
				t.Fatal("tamper authoritative record failed")
			}
			if got, err := s.Open(ctx, id, tc.owner, tc.purpose); err == nil || got != nil {
				t.Fatal("tampered authoritative data returned plaintext")
			}
		})
	}
}

func TestPostgresSecretDisableSerializesWithOpen(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	root := rootFile(t, "root-a")
	s := storeFor(t, pool, Config{ActiveRootID: root.ID, Roots: []RootFile{root}})
	owner := Owner{Kind: "actor", ID: "alice"}
	id, err := s.Seal(ctx, owner, GitHubUserToken, []byte("private-locked-token"))
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin disable transaction failed")
	}
	defer rollback(tx)
	if _, err := tx.Exec(ctx, `UPDATE repomesh_secrets.availability SET enabled=false,access_epoch=access_epoch+1 WHERE version_id=$1`, string(id)); err != nil {
		t.Fatal("disable update failed")
	}
	result := make(chan error, 1)
	go func() {
		got, err := s.Open(ctx, id, owner, GitHubUserToken)
		if got != nil {
			result <- errors.New("plaintext escaped concurrent disable")
			return
		}
		result <- err
	}()
	select {
	case <-result:
		t.Fatal("Open bypassed uncommitted availability change")
	case <-time.After(30 * time.Millisecond):
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal("commit disable failed")
	}
	select {
	case err := <-result:
		if !errors.Is(err, ErrUnavailable) {
			t.Fatal("Open did not reject committed disable")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Open did not resume after disable")
	}
}

func TestPostgresSecretDestroyIsPermanent(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	root := rootFile(t, "root-a")
	s := storeFor(t, pool, Config{ActiveRootID: root.ID, Roots: []RootFile{root}})
	owner := Owner{Kind: "auth-attempt", ID: "binding-digest:attempt-uuid"}
	id, err := s.Seal(ctx, owner, AuthExchangeResult, []byte("short-lived-oauth-result"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Destroy(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := s.Destroy(ctx, id); err != nil {
		t.Fatal("repeated destruction failed")
	}
	if got, err := s.Open(ctx, id, owner, AuthExchangeResult); err == nil || got != nil {
		t.Fatal("destroyed material returned plaintext")
	}
	if err := s.SetEnabled(ctx, id, true); err == nil {
		t.Fatal("destroyed material was reenabled")
	}
	var tombstone bool
	var epoch int64
	if err := pool.QueryRow(ctx, `SELECT v.destroyed_at IS NOT NULL AND v.ciphertext IS NULL AND v.wrapped_dek IS NULL AND NOT a.enabled,a.access_epoch
		FROM repomesh_secrets.versions v JOIN repomesh_secrets.availability a USING(version_id) WHERE v.version_id=$1`, string(id)).Scan(&tombstone, &epoch); err != nil || !tombstone || epoch != 2 {
		t.Fatal("destruction did not leave an idempotent tombstone")
	}
	next := rootFile(t, "root-b")
	rotated := storeFor(t, pool, Config{ActiveRootID: next.ID, Roots: []RootFile{root, next}})
	if done, err := rotated.Rewrap(ctx, 10); err != nil || done != 0 {
		t.Fatal("rewrap attempted to restore destroyed material")
	}
	storeFor(t, pool, Config{ActiveRootID: next.ID, Roots: []RootFile{next}})
}

func TestPostgresSecretDestroyRacesEnable(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	root := rootFile(t, "root-a")
	s := storeFor(t, pool, Config{ActiveRootID: root.ID, Roots: []RootFile{root}})
	owner := Owner{Kind: "actor", ID: "alice"}
	id, err := s.Seal(ctx, owner, AuthMaterial, []byte("temporary-auth-material"))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := s.SetEnabled(ctx, id, true)
			if err != nil && !errors.Is(err, ErrUnavailable) {
				t.Error("unexpected enable failure")
			}
		}()
	}
	if err := s.Destroy(ctx, id); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	if got, err := s.Open(ctx, id, owner, AuthMaterial); err == nil || got != nil {
		t.Fatal("concurrent enable resurrected destroyed material")
	}
	var enabled bool
	if err := pool.QueryRow(ctx, `SELECT enabled FROM repomesh_secrets.availability WHERE version_id=$1`, string(id)).Scan(&enabled); err != nil || enabled {
		t.Fatal("destroyed material remained enabled")
	}
}

func TestPostgresSecretFailedRewrapKeepsReservation(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	root := rootFile(t, "root-a")
	s := storeFor(t, pool, Config{ActiveRootID: root.ID, Roots: []RootFile{root}})
	id, err := s.Seal(ctx, Owner{Kind: "actor", ID: "alice"}, GitHubUserToken, []byte("private-token"))
	if err != nil {
		t.Fatal(err)
	}
	next := rootFile(t, "root-b")
	rotated := storeFor(t, pool, Config{ActiveRootID: next.ID, Roots: []RootFile{root, next}})
	if _, err := pool.Exec(ctx, `UPDATE repomesh_secrets.versions SET wrapped_dek=set_byte(wrapped_dek,5,get_byte(wrapped_dek,5)#1) WHERE version_id=$1`, string(id)); err != nil {
		t.Fatal("tamper wrapper failed")
	}
	if done, err := rotated.Rewrap(ctx, 10); err == nil || done != 0 {
		t.Fatal("rewrap accepted corrupt wrapper")
	}
	if wrapCount(t, pool, next.ID) != 2 {
		t.Fatal("failed rewrap refunded root quota")
	}
	var recordedRoot string
	if err := pool.QueryRow(ctx, `SELECT root_id FROM repomesh_secrets.versions WHERE version_id=$1`, string(id)).Scan(&recordedRoot); err != nil || recordedRoot != root.ID {
		t.Fatal("failed rewrap changed version root")
	}
}
