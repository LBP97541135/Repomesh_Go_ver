//go:build unix

package secrets

import (
	"bytes"
	"context"
	"testing"

	"repomesh.local/repomesh/internal/testdb"
)

func TestPrepareInsertPreparedRoundTrip(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	first := rootFile(t, "root-prepare")
	s := storeFor(t, pool, Config{ActiveRootID: first.ID, Roots: []RootFile{first}})
	owner := Owner{Kind: "model-provider", ID: "11111111-1111-4111-8111-111111111111"}
	plain := []byte("model-key-bytes")
	before := wrapCount(t, pool, first.ID)
	prepared, err := s.Prepare(ctx, owner, ModelProviderKey, plain)
	if err != nil {
		t.Fatal(err)
	}
	if wrapCount(t, pool, first.ID) != before+1 {
		t.Fatal("prepare did not precharge wrap count")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.InsertPrepared(ctx, tx, prepared)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.OpenInTx(ctx, tx, id, owner, ModelProviderKey)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatal("prepared secret did not open in caller tx")
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Open(ctx, id, owner, ModelProviderKey); err == nil {
		t.Fatal("rolled back insert remained readable")
	}
	if wrapCount(t, pool, first.ID) != before+1 {
		t.Fatal("wrap precharge was refunded")
	}
}

func TestPrepareRejectsWrongOwnerPurpose(t *testing.T) {
	pool := testdb.Open(t)
	s := storeFor(t, pool, Config{ActiveRootID: "root-prepare-2", Roots: []RootFile{rootFile(t, "root-prepare-2")}})
	if _, err := s.Prepare(context.Background(), Owner{Kind: "actor", ID: "alice"}, ModelProviderKey, []byte("x")); err == nil {
		t.Fatal("wrong owner kind accepted")
	}
	if _, err := s.Prepare(context.Background(), Owner{Kind: "provider-save-input", ID: "alice:save"}, OperationInput, []byte("vault")); err != nil {
		t.Fatal(err)
	}
}
