//go:build unix

package models

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/projects"
	"repomesh.local/repomesh/internal/secrets"
	"repomesh.local/repomesh/internal/testdb"
)

const saveJSON = `{"providerId":null,"expectedRevision":null,"name":"开发网关","baseUrl":"https://gateway.example.invalid/v1","apiFormat":"openai_chat_completions","secret":{"mode":"replace","value":"sk-test-key"},"models":[{"id":null,"modelId":"deepseek-chat","displayName":"对话","contextWindow":256000,"maxOutputTokens":128000,"reasoning":true,"vision":false}]}`

func bodyWithSecret(value string) string {
	return strings.Replace(saveJSON, `"value":"sk-test-key"`, `"value":"`+value+`"`, 1)
}

func bodyWithName(name string) string {
	return strings.Replace(saveJSON, `"name":"开发网关"`, `"name":"`+name+`"`, 1)
}

func followOnBody(t *testing.T, view ProviderView, revision, secretJSON string) string {
	t.Helper()
	type model struct {
		ID              string  `json:"id"`
		ModelID         string  `json:"modelId"`
		DisplayName     *string `json:"displayName"`
		ContextWindow   int64   `json:"contextWindow"`
		MaxOutputTokens int64   `json:"maxOutputTokens"`
		Reasoning       bool    `json:"reasoning"`
		Vision          bool    `json:"vision"`
	}
	models := make([]model, len(view.Models))
	for index, item := range view.Models {
		name := item.DisplayName
		models[index] = model{
			ID: item.ID, ModelID: item.ModelID, DisplayName: &name,
			ContextWindow: item.ContextWindow, MaxOutputTokens: item.MaxOutputTokens,
			Reasoning: item.Reasoning, Vision: item.Vision,
		}
	}
	body, err := json.Marshal(map[string]any{
		"providerId": view.ID, "expectedRevision": revision, "name": view.Name,
		"baseUrl": view.BaseURL, "apiFormat": view.APIFormat,
		"secret": json.RawMessage(secretJSON), "models": models,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

var saveArtifactTables = []string{
	"repomesh_secrets.versions", "repomesh_secrets.availability",
	"repomesh_models.providers", "repomesh_models.provider_revisions", "repomesh_models.model_rows",
	"repomesh_models.model_snapshots", "repomesh_models.profile_links",
	"repomesh_projects.profiles", "repomesh_projects.profile_versions",
	"repomesh_models.save_operations",
}

type fixture struct {
	ctx           context.Context
	pool          *pgxpool.Pool
	store         *secrets.Store
	authorization *access.Service
	service       *Service
	principal     access.ProjectPrincipal
	rootPaths     map[string]string
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func cookie() string {
	var value [32]byte
	_, _ = rand.Read(value[:])
	return base64.RawURLEncoding.EncodeToString(value[:])
}

func writeRoot(t *testing.T) string {
	t.Helper()
	root := make([]byte, 32)
	_, _ = rand.Read(root)
	path := filepath.Join(t.TempDir(), "root")
	if err := os.WriteFile(path, root, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	f := &fixture{ctx: ctx, pool: pool, rootPaths: map[string]string{"models-a": writeRoot(t)}}
	store, err := secrets.New(ctx, pool, secrets.Config{ActiveRootID: "models-a", Roots: []secrets.RootFile{{ID: "models-a", Path: f.rootPaths["models-a"]}}})
	if err != nil {
		t.Fatal(err)
	}
	f.store = store
	f.authorization = access.New(pool, store, nil)
	f.principal = f.principalFor(t, "10000000-0000-4000-8000-000000000001", 9001)
	f.service = New(pool, f.authorization, store, projects.NewCatalogWriter())
	return f
}

func (f *fixture) principalFor(t *testing.T, actor string, githubID int64) access.ProjectPrincipal {
	t.Helper()
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO repomesh_access.accounts(id,github_id,display_name) VALUES ($1,$2,'Model actor')`, actor, githubID); err != nil {
		t.Fatal(err)
	}
	sessionCookie, bindingCookie := cookie(), cookie()
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO repomesh_access.bindings(hash,identity_generation,expires_at) VALUES ($1,1,now()+interval '1 day')`, digest(bindingCookie)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO repomesh_access.sessions(hash,actor,binding,generation,expires_at,last_active_at) VALUES ($1,$2,$3,1,now()+interval '1 day',now())`, digest(sessionCookie), actor, digest(bindingCookie)); err != nil {
		t.Fatal(err)
	}
	principal, err := f.authorization.AuthenticateProjectRequest(f.ctx, sessionCookie, "", false)
	if err != nil {
		t.Fatal(err)
	}
	return principal
}

// storeWithNewRoot opens a second store that still holds every earlier root and
// activates a fresh one, which retires the previous active root in root_keys.
func (f *fixture) storeWithNewRoot(t *testing.T, id string) *secrets.Store {
	t.Helper()
	f.rootPaths[id] = writeRoot(t)
	roots := make([]secrets.RootFile, 0, len(f.rootPaths))
	for rootID, path := range f.rootPaths {
		roots = append(roots, secrets.RootFile{ID: rootID, Path: path})
	}
	store, err := secrets.New(f.ctx, f.pool, secrets.Config{ActiveRootID: id, Roots: roots})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func (f *fixture) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(f.ctx, query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (f *fixture) wrapCount(t *testing.T, rootID string) int64 {
	t.Helper()
	var n int64
	if err := f.pool.QueryRow(f.ctx, `SELECT wrap_count FROM repomesh_secrets.root_keys WHERE root_id=$1`, rootID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (f *fixture) requireNoSaveArtifacts(t *testing.T, label string) {
	t.Helper()
	for _, table := range saveArtifactTables {
		if n := f.count(t, "SELECT count(*) FROM "+table); n != 0 {
			t.Fatalf("%s: %s kept %d rows", label, table, n)
		}
	}
}

func (f *fixture) save(t *testing.T, principal access.ProjectPrincipal, key, body string) (SaveResult, error) {
	t.Helper()
	raw, err := ReadSaveBody([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	command, err := NewSaveCommand(key, "req-1", raw)
	if err != nil {
		t.Fatal(err)
	}
	return f.service.Save(f.ctx, principal, command)
}

func (f *fixture) close(principal access.ProjectPrincipal, key string) (SaveReceipt, error) {
	return f.service.CloseSave(f.ctx, principal, CloseCommand{key: key})
}

func wantFailure(t *testing.T, err error, status int, code string) {
	t.Helper()
	var failure *Failure
	if !errors.As(err, &failure) || failure.Status != status || failure.Code != code {
		t.Fatalf("error=%v; want %d %s", err, status, code)
	}
}

func committedRevision(t *testing.T, result SaveResult, err error, status int) string {
	t.Helper()
	if err != nil || result.HTTPStatus != status || result.Receipt.committed == nil {
		t.Fatalf("save: status=%d receipt=%+v err=%v; want %d committed", result.HTTPStatus, result.Receipt, err, status)
	}
	return result.Receipt.committed.ProviderRevision
}

func TestPostgresSaveIdempotentAndClose(t *testing.T) {
	f := newFixture(t)
	key := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	result, err := f.save(t, f.principal, key, saveJSON)
	revision := committedRevision(t, result, err, 201)
	replay, err := f.save(t, f.principal, key, saveJSON)
	if committedRevision(t, replay, err, 200) != revision {
		t.Fatal("replay returned a different revision")
	}
	if f.count(t, `SELECT count(*) FROM repomesh_models.providers`) != 1 || f.count(t, `SELECT count(*) FROM repomesh_models.provider_revisions`) != 1 || f.count(t, `SELECT count(*) FROM repomesh_secrets.versions`) != 2 {
		t.Fatal("replay duplicated provider or secret rows")
	}
	closed, err := f.close(f.principal, key)
	if err != nil || closed.committed == nil || closed.committed.ProviderRevision != revision {
		t.Fatalf("close after save: %+v %v", closed, err)
	}
	late, err := f.save(t, f.principal, key, bodyWithSecret("sk-other-key"))
	wantFailure(t, err, 409, "IDEMPOTENCY_CONFLICT")
	if late.HTTPStatus != 409 || late.Receipt.committed == nil || late.Receipt.committed.ProviderRevision != revision {
		t.Fatalf("late save with a different key: %+v", late)
	}
}

func TestPostgresTwentyWaySameSave(t *testing.T) {
	f := newFixture(t)
	key := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	var wg sync.WaitGroup
	results := make([]SaveResult, 20)
	errs := make([]error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = f.save(t, f.principal, key, saveJSON)
		}(i)
	}
	wg.Wait()
	var committed string
	created := 0
	for i := 0; i < 20; i++ {
		if errs[i] != nil || results[i].Receipt.committed == nil {
			t.Fatalf("worker %d: %+v %v", i, results[i], errs[i])
		}
		if results[i].HTTPStatus == 201 {
			created++
		}
		if committed == "" {
			committed = results[i].Receipt.committed.ProviderRevision
		} else if results[i].Receipt.committed.ProviderRevision != committed {
			t.Fatal("concurrent saves created different revisions")
		}
	}
	if created != 1 {
		t.Fatalf("created=%d; want exactly one 201", created)
	}
	for table, want := range map[string]int{
		"repomesh_models.providers": 1, "repomesh_models.provider_revisions": 1, "repomesh_models.model_snapshots": 1,
		"repomesh_secrets.versions": 2, "repomesh_models.save_operations": 1,
		"repomesh_projects.profiles": 1, "repomesh_projects.profile_versions": 1, "repomesh_models.profile_links": 1,
	} {
		if n := f.count(t, "SELECT count(*) FROM "+table); n != want {
			t.Fatalf("%s=%d; want %d", table, n, want)
		}
	}
}

func TestPostgresDifferentInputConflictComparesVault(t *testing.T) {
	f := newFixture(t)
	key := "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	result, err := f.save(t, f.principal, key, saveJSON)
	revision := committedRevision(t, result, err, 201)

	same, _ := ReadSaveBody([]byte(saveJSON))
	other, _ := ReadSaveBody([]byte(bodyWithSecret("sk-other-key")))
	_, sameCanonical, _ := parseSave(same, 1)
	_, otherCanonical, _ := parseSave(other, 1)
	if !bytes.Equal(canonicalBytes(sameCanonical), canonicalBytes(otherCanonical)) {
		t.Fatal("secret-only change altered the non-secret canonical form; the vault comparison is not what this test exercises")
	}
	conflict, err := f.save(t, f.principal, key, bodyWithSecret("sk-other-key"))
	wantFailure(t, err, 409, "IDEMPOTENCY_CONFLICT")
	if conflict.HTTPStatus != 409 || conflict.Receipt.committed == nil || conflict.Receipt.committed.ProviderRevision != revision {
		t.Fatalf("different key replay: %+v", conflict)
	}
	_, err = f.save(t, f.principal, key, bodyWithName("另一个网关"))
	wantFailure(t, err, 409, "IDEMPOTENCY_CONFLICT")

	if f.count(t, `SELECT count(*) FROM repomesh_models.save_operations WHERE position(convert_to('sk-test-key','UTF8') in canonical_nonsecret)>0 OR receipt::text LIKE '%sk-test-key%'`) != 0 {
		t.Fatal("business key stored in the operation record")
	}
	if f.count(t, `SELECT count(*) FROM repomesh_secrets.versions WHERE position(convert_to('sk-test-key','UTF8') in ciphertext)>0`) != 0 {
		t.Fatal("business key stored in plaintext ciphertext")
	}

	if err := f.store.Destroy(f.ctx, secrets.VersionID(result.Receipt.committed.SecretVersionID)); err != nil {
		t.Fatal(err)
	}
	replay, err := f.save(t, f.principal, key, saveJSON)
	if committedRevision(t, replay, err, 200) != revision {
		t.Fatal("replay after key revocation returned a different revision")
	}
	_, err = f.save(t, f.principal, key, bodyWithSecret("sk-other-key"))
	wantFailure(t, err, 409, "IDEMPOTENCY_CONFLICT")
	if f.count(t, `SELECT count(*) FROM repomesh_secrets.versions`) != 2 || f.count(t, `SELECT count(*) FROM repomesh_models.provider_revisions`) != 1 {
		t.Fatal("revoked key caused a rebuild")
	}
	view, err := f.service.Get(f.ctx, f.principal, result.Receipt.committed.ProviderID)
	if err != nil || view.Secret.Availability != "unavailable" {
		t.Fatalf("provider after revocation: %+v %v", view, err)
	}
}

func TestPostgresSaveRollsBackAtEveryPhase(t *testing.T) {
	f := newFixture(t)
	key := "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	injected := errors.New("injected rollback")
	phases := []transactionPhase{secretPrepared, principalLocked, slotLocked, secretsInserted, providerInserted, modelsInserted, profilesInserted, headUpdated, receiptInserted, beforeCommit}
	for _, target := range phases {
		before := f.wrapCount(t, "models-a")
		f.service.hook = func(_ context.Context, phase transactionPhase) error {
			if phase == target {
				return injected
			}
			return nil
		}
		_, err := f.save(t, f.principal, key, saveJSON)
		if !errors.Is(err, injected) {
			t.Fatalf("phase %s: err=%v", target, err)
		}
		f.requireNoSaveArtifacts(t, string(target))
		if after := f.wrapCount(t, "models-a"); after != before+2 {
			t.Fatalf("phase %s: wrap_count %d -> %d; want the two reservations retained", target, before, after)
		}
	}
	f.service.hook = nil
	result, err := f.save(t, f.principal, key, saveJSON)
	committedRevision(t, result, err, 201)
}

func TestPostgresCloseVersusLateSave(t *testing.T) {
	f := newFixture(t)
	saved := "cccccccc-cccc-4ccc-8ccc-ccccccccccc1"
	result, err := f.save(t, f.principal, saved, saveJSON)
	revision := committedRevision(t, result, err, 201)
	closed, err := f.close(f.principal, saved)
	if err != nil || closed.committed == nil || closed.committed.ProviderRevision != revision {
		t.Fatalf("close after save must return the committed receipt: %+v %v", closed, err)
	}

	// The close lands after the late save has prepared its secrets but before it commits.
	late := "cccccccc-cccc-4ccc-8ccc-ccccccccccc2"
	before := f.wrapCount(t, "models-a")
	f.service.hook = func(_ context.Context, phase transactionPhase) error {
		if phase != secretPrepared {
			return nil
		}
		receipt, closeErr := f.close(f.principal, late)
		if closeErr != nil || receipt.closed == nil {
			t.Errorf("close during late save: %+v %v", receipt, closeErr)
		}
		return closeErr
	}
	_, err = f.save(t, f.principal, late, saveJSON)
	f.service.hook = nil
	wantFailure(t, err, 409, "MODEL_SAVE_CLOSED")
	if f.count(t, `SELECT count(*) FROM repomesh_models.providers`) != 1 || f.count(t, `SELECT count(*) FROM repomesh_secrets.versions`) != 2 {
		t.Fatal("late save created provider or secret rows")
	}
	if f.count(t, `SELECT count(*) FROM repomesh_models.save_operations WHERE save_id=$1 AND kind='save_closed'`, late) != 1 {
		t.Fatal("closed slot missing")
	}
	if after := f.wrapCount(t, "models-a"); after != before+2 {
		t.Fatalf("wrap_count %d -> %d; want the late save's reservations retained", before, after)
	}
	lateReceipt, err := f.service.GetSave(f.ctx, f.principal, late)
	if err != nil || lateReceipt.closed == nil {
		t.Fatalf("closed slot receipt: %+v %v", lateReceipt, err)
	}

	empty := "cccccccc-cccc-4ccc-8ccc-ccccccccccc3"
	var wg sync.WaitGroup
	receipts := make([]SaveReceipt, 10)
	errs := make([]error, 10)
	for i := range receipts {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			receipts[i], errs[i] = f.close(f.principal, empty)
		}(i)
	}
	wg.Wait()
	for i := range receipts {
		if errs[i] != nil || receipts[i].closed == nil || !receipts[i].closed.ClosedAt.Equal(receipts[0].closed.ClosedAt) {
			t.Fatalf("concurrent close %d: %+v %v", i, receipts[i], errs[i])
		}
	}
	if f.count(t, `SELECT count(*) FROM repomesh_models.save_operations WHERE save_id=$1`, empty) != 1 {
		t.Fatal("concurrent closes wrote more than one slot")
	}
	_, err = f.save(t, f.principal, empty, saveJSON)
	wantFailure(t, err, 409, "MODEL_SAVE_CLOSED")
}

func TestPostgresPreparedRootRetiredBeforeInsert(t *testing.T) {
	f := newFixture(t)
	key := "ffffffff-ffff-4fff-8fff-ffffffffffff"
	retired := false
	f.service.hook = func(_ context.Context, phase transactionPhase) error {
		if phase == secretPrepared && !retired {
			retired = true
			f.storeWithNewRoot(t, "models-b")
		}
		return nil
	}
	_, err := f.save(t, f.principal, key, saveJSON)
	f.service.hook = nil
	wantFailure(t, err, 503, "RESULT_UNCONFIRMED")
	if !retired {
		t.Fatal("root rotation hook did not run")
	}
	f.requireNoSaveArtifacts(t, "root retired between Prepare and Insert")
	var activeA, activeB bool
	var retiredAt *time.Time
	if err := f.pool.QueryRow(f.ctx, `SELECT active_wrap,retired_at FROM repomesh_secrets.root_keys WHERE root_id='models-a'`).Scan(&activeA, &retiredAt); err != nil || activeA || retiredAt == nil {
		t.Fatalf("root a active=%t retired=%v err=%v", activeA, retiredAt, err)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT active_wrap FROM repomesh_secrets.root_keys WHERE root_id='models-b'`).Scan(&activeB); err != nil || !activeB {
		t.Fatalf("root b active=%t err=%v", activeB, err)
	}
	closed, err := f.close(f.principal, key)
	if err != nil || closed.closed == nil {
		t.Fatalf("close after failed save: %+v %v", closed, err)
	}
	_, err = f.save(t, f.principal, key, saveJSON)
	wantFailure(t, err, 409, "MODEL_SAVE_CLOSED")
}

func TestPostgresReplayWithMissingVaultRoot(t *testing.T) {
	f := newFixture(t)
	underA := "abababab-abab-4bab-8bab-abababababab"
	result, err := f.save(t, f.principal, underA, saveJSON)
	revisionA := committedRevision(t, result, err, 201)

	storeB := f.storeWithNewRoot(t, "models-b")
	serviceB := New(f.pool, f.authorization, storeB, projects.NewCatalogWriter())
	underB := "bcbcbcbc-bcbc-4cbc-8cbc-bcbcbcbcbcbc"
	raw, _ := ReadSaveBody([]byte(saveJSON))
	command, _ := NewSaveCommand(underB, "req-b", raw)
	resultB, err := serviceB.Save(f.ctx, f.principal, command)
	revisionB := committedRevision(t, resultB, err, 201)

	// Service A never loaded root b, so it cannot open the vault written by B.
	_, err = f.save(t, f.principal, underB, saveJSON)
	wantFailure(t, err, 503, "RESULT_UNCONFIRMED")
	var receiptPresent, vaultPresent bool
	var removedAt *time.Time
	if err := f.pool.QueryRow(f.ctx, `SELECT receipt IS NOT NULL,input_vault_version IS NOT NULL,removed_at FROM repomesh_models.save_operations WHERE actor=$1 AND save_id=$2`, f.principal.ActorID(), underB).Scan(&receiptPresent, &vaultPresent, &removedAt); err != nil || !receiptPresent || !vaultPresent || removedAt != nil {
		t.Fatalf("slot after unconfirmed compare: receipt=%t vault=%t removed=%v err=%v", receiptPresent, vaultPresent, removedAt, err)
	}
	if f.count(t, `SELECT count(*) FROM repomesh_secrets.versions`) != 4 {
		t.Fatal("unconfirmed compare changed secret rows")
	}

	closed, err := f.close(f.principal, "cdcdcdcd-cdcd-4dcd-8dcd-cdcdcdcdcdcd")
	if err != nil || closed.closed == nil {
		t.Fatalf("close empty slot without root b: %+v %v", closed, err)
	}
	replayA, err := f.save(t, f.principal, underA, saveJSON)
	if committedRevision(t, replayA, err, 200) != revisionA {
		t.Fatal("service A replay under its own root changed revision")
	}

	raw, _ = ReadSaveBody([]byte(saveJSON))
	command, _ = NewSaveCommand(underB, "req-b", raw)
	replayB, err := serviceB.Save(f.ctx, f.principal, command)
	if committedRevision(t, replayB, err, 200) != revisionB {
		t.Fatal("service B replay changed revision")
	}
	raw, _ = ReadSaveBody([]byte(saveJSON))
	command, _ = NewSaveCommand(underA, "req-b", raw)
	replayAB, err := serviceB.Save(f.ctx, f.principal, command)
	if committedRevision(t, replayAB, err, 200) != revisionA {
		t.Fatal("service B replay of the root-a slot changed revision")
	}

	_, err = secrets.New(f.ctx, f.pool, secrets.Config{ActiveRootID: "models-b", Roots: []secrets.RootFile{{ID: "models-b", Path: f.rootPaths["models-b"]}}})
	if !errors.Is(err, secrets.ErrConfiguration) {
		t.Fatalf("cold start without root a: %v; want configuration rejection", err)
	}
}

func TestPostgresRemoveSaveResultKeepsBusinessSecret(t *testing.T) {
	f := newFixture(t)
	other := f.principalFor(t, "10000000-0000-4000-8000-000000000002", 9002)
	key := "12121212-1212-4212-8212-121212121212"
	result, err := f.save(t, f.principal, key, saveJSON)
	committedRevision(t, result, err, 201)
	business := result.Receipt.committed.SecretVersionID
	providerID := result.Receipt.committed.ProviderID
	var vault string
	if err := f.pool.QueryRow(f.ctx, `SELECT input_vault_version FROM repomesh_models.save_operations WHERE actor=$1 AND save_id=$2`, f.principal.ActorID(), key).Scan(&vault); err != nil || vault == "" {
		t.Fatalf("vault version: %q %v", vault, err)
	}

	_, err = f.service.GetSave(f.ctx, other, key)
	wantFailure(t, err, 404, "MODEL_SAVE_NOT_FOUND")
	_, err = f.service.ResolveDestination(f.ctx, other.ActorID(), key)
	wantFailure(t, err, 404, "RESOURCE_NOT_FOUND")
	_, err = f.service.Get(f.ctx, other, providerID)
	wantFailure(t, err, 404, "RESOURCE_NOT_FOUND")

	maintenance := NewMaintenance(f.service)
	if err := maintenance.RemoveSaveResult(f.ctx, f.principal.ActorID(), key); err != nil {
		t.Fatal(err)
	}
	if err := maintenance.RemoveSaveResult(f.ctx, f.principal.ActorID(), key); err != nil {
		t.Fatalf("second removal: %v", err)
	}
	err = maintenance.RemoveSaveResult(f.ctx, f.principal.ActorID(), "34343434-3434-4434-8434-343434343434")
	wantFailure(t, err, 404, "MODEL_SAVE_NOT_FOUND")

	_, err = f.service.GetSave(f.ctx, f.principal, key)
	wantFailure(t, err, 410, "MODEL_SAVE_RESULT_REMOVED")
	_, err = f.save(t, f.principal, key, saveJSON)
	wantFailure(t, err, 410, "MODEL_SAVE_RESULT_REMOVED")
	_, err = f.close(f.principal, key)
	wantFailure(t, err, 410, "MODEL_SAVE_RESULT_REMOVED")

	var receiptPresent, canonicalPresent, vaultPresent bool
	var target *string
	var removedAt *time.Time
	if err := f.pool.QueryRow(f.ctx, `SELECT receipt IS NOT NULL,canonical_nonsecret IS NOT NULL,input_vault_version IS NOT NULL,target,removed_at FROM repomesh_models.save_operations WHERE actor=$1 AND save_id=$2`, f.principal.ActorID(), key).Scan(&receiptPresent, &canonicalPresent, &vaultPresent, &target, &removedAt); err != nil {
		t.Fatal(err)
	}
	if receiptPresent || canonicalPresent || vaultPresent || target == nil || *target != providerID || removedAt == nil {
		t.Fatalf("removed slot: receipt=%t canonical=%t vault=%t target=%v removed=%v", receiptPresent, canonicalPresent, vaultPresent, target, removedAt)
	}
	secretState := func(id string) (destroyed, enabled bool) {
		if err := f.pool.QueryRow(f.ctx, `SELECT v.destroyed_at IS NOT NULL OR v.ciphertext IS NULL,a.enabled FROM repomesh_secrets.versions v JOIN repomesh_secrets.availability a USING(version_id) WHERE v.version_id=$1`, id).Scan(&destroyed, &enabled); err != nil {
			t.Fatal(err)
		}
		return destroyed, enabled
	}
	if destroyed, enabled := secretState(vault); !destroyed || enabled {
		t.Fatalf("vault after removal: destroyed=%t enabled=%t", destroyed, enabled)
	}
	if destroyed, enabled := secretState(business); destroyed || !enabled {
		t.Fatalf("business key after removal: destroyed=%t enabled=%t", destroyed, enabled)
	}
	view, err := f.service.Get(f.ctx, f.principal, providerID)
	if err != nil || view.Secret.Availability != "available" || view.Secret.VersionID == nil || *view.Secret.VersionID != business {
		t.Fatalf("provider after removal: %+v %v", view, err)
	}
}

func TestPostgresRemoveRejectedSaveResultDestroysVault(t *testing.T) {
	f := newFixture(t)
	created, err := f.save(t, f.principal, "12121212-1212-4212-8212-121212121212", saveJSON)
	committedRevision(t, created, err, 201)
	business := created.Receipt.committed.SecretVersionID
	providerID := created.Receipt.committed.ProviderID
	view, err := f.service.Get(f.ctx, f.principal, providerID)
	if err != nil {
		t.Fatal(err)
	}

	keepKey := "23232323-2323-4232-8232-232323232323"
	kept, err := f.save(t, f.principal, keepKey, followOnBody(t, view, view.Revision, `{"mode":"keep"}`))
	committedRevision(t, kept, err, 200)
	var keepVault *string
	if err := f.pool.QueryRow(f.ctx, `SELECT input_vault_version FROM repomesh_models.save_operations WHERE actor=$1 AND save_id=$2`, f.principal.ActorID(), keepKey).Scan(&keepVault); err != nil || keepVault != nil {
		t.Fatalf("keep vault=%v err=%v; want null", keepVault, err)
	}

	rejectedKey := "34343434-3434-4434-8434-343434343434"
	rejected, err := f.save(t, f.principal, rejectedKey, followOnBody(t, view, "99999999-9999-4999-8999-999999999999", `{"mode":"replace","value":"sk-rejected-key"}`))
	if err != nil || rejected.HTTPStatus != 409 || rejected.Receipt.rejected == nil || rejected.Receipt.rejected.Error.Code != "PROVIDER_REVISION_CONFLICT" {
		t.Fatalf("rejected save: %+v %v", rejected, err)
	}
	var rejectedVault string
	var rejectedTarget *string
	if err := f.pool.QueryRow(f.ctx, `SELECT input_vault_version,target FROM repomesh_models.save_operations WHERE actor=$1 AND save_id=$2`, f.principal.ActorID(), rejectedKey).Scan(&rejectedVault, &rejectedTarget); err != nil || rejectedVault == "" || rejectedTarget != nil {
		t.Fatalf("rejected slot vault=%q target=%v err=%v", rejectedVault, rejectedTarget, err)
	}

	maintenance := NewMaintenance(f.service)
	if err := maintenance.RemoveSaveResult(f.ctx, f.principal.ActorID(), rejectedKey); err != nil {
		t.Fatal(err)
	}
	_, err = f.service.GetSave(f.ctx, f.principal, rejectedKey)
	wantFailure(t, err, 410, "MODEL_SAVE_RESULT_REMOVED")
	_, err = f.save(t, f.principal, rejectedKey, followOnBody(t, view, "99999999-9999-4999-8999-999999999999", `{"mode":"replace","value":"sk-rejected-key"}`))
	wantFailure(t, err, 410, "MODEL_SAVE_RESULT_REMOVED")

	var receiptPresent, vaultPresent bool
	var removedAt *time.Time
	if err := f.pool.QueryRow(f.ctx, `SELECT receipt IS NOT NULL,input_vault_version IS NOT NULL,removed_at FROM repomesh_models.save_operations WHERE actor=$1 AND save_id=$2`, f.principal.ActorID(), rejectedKey).Scan(&receiptPresent, &vaultPresent, &removedAt); err != nil {
		t.Fatal(err)
	}
	if receiptPresent || vaultPresent || removedAt == nil {
		t.Fatalf("removed rejected slot: receipt=%t vault=%t removed=%v", receiptPresent, vaultPresent, removedAt)
	}
	secretState := func(id string) (destroyed, enabled bool) {
		if err := f.pool.QueryRow(f.ctx, `SELECT v.destroyed_at IS NOT NULL OR v.ciphertext IS NULL,a.enabled FROM repomesh_secrets.versions v JOIN repomesh_secrets.availability a USING(version_id) WHERE v.version_id=$1`, id).Scan(&destroyed, &enabled); err != nil {
			t.Fatal(err)
		}
		return destroyed, enabled
	}
	if destroyed, enabled := secretState(rejectedVault); !destroyed || enabled {
		t.Fatalf("rejected vault after removal: destroyed=%t enabled=%t", destroyed, enabled)
	}
	if destroyed, enabled := secretState(business); destroyed || !enabled {
		t.Fatalf("business key after rejected cleanup: destroyed=%t enabled=%t", destroyed, enabled)
	}
	current, err := f.service.Get(f.ctx, f.principal, providerID)
	if err != nil || current.Secret.Availability != "available" || current.Secret.VersionID == nil || *current.Secret.VersionID != business {
		t.Fatalf("provider after rejected cleanup: %+v %v", current, err)
	}
}
