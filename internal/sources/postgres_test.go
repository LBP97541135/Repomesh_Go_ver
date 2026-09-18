//go:build unix

package sources

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/models"
	"repomesh.local/repomesh/internal/projects"
	"repomesh.local/repomesh/internal/secrets"
	"repomesh.local/repomesh/internal/testdb"
)

func TestPostgresImportReplayAndPinnedDefault(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	if _, err := pool.Exec(ctx, `INSERT INTO repomesh_access.accounts(id,github_id,display_name) VALUES ('owner-1',9101,'Import owner')`); err != nil {
		t.Fatal(err)
	}
	principal, err := AuthenticateDeployment(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	importer, err := NewImporter(pool, principal, projects.NewCatalogWriter())
	if err != nil {
		t.Fatal(err)
	}
	command, err := ParseImport([]byte(`{
		"schemaVersion":1,
		"importId":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		"environmentTemplates":[{"id":"tpl","version":"v1","executorPoolId":"pool","approvedTemplateDigest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","networkPolicyRef":"net","resourceClassId":"class","enabled":true}],
		"executionProfiles":[{"id":"exec","version":"v1","ownerId":"owner-1","name":"执行","templateId":"tpl","templateVersion":"v1","workerConcurrency":2,"verificationGroupEnabled":false}],
		"defaultBindings":[{"kind":"execution","ownerId":"owner-1","profileId":"exec","profileVersion":"v1"}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	first, err := importer.Import(ctx, command)
	if err != nil || first.Replayed || first.Receipt.DefaultBindings[0].ProfileVersion != "v1" {
		t.Fatalf("first import: %+v %v", first, err)
	}
	second, err := importer.Import(ctx, command)
	if err != nil || !second.Replayed || second.Receipt.CommittedAt != first.Receipt.CommittedAt {
		t.Fatalf("replay: %+v %v", second, err)
	}
	var pinned string
	if err := pool.QueryRow(ctx, `SELECT pinned_version FROM repomesh_projects.defaults WHERE actor='owner-1' AND kind='execution'`).Scan(&pinned); err != nil || pinned != "v1" {
		t.Fatalf("pinned=%q %v", pinned, err)
	}
	conflicting := command
	conflicting.canonical = append([]byte(nil), command.canonical...)
	conflicting.canonical[len(conflicting.canonical)-2]++
	if _, err := importer.Import(ctx, conflicting); err == nil {
		t.Fatal("conflicting import accepted")
	}
	got, err := importer.Get(ctx, command.value.ImportID)
	if err != nil || got.ImportID != first.Receipt.ImportID {
		t.Fatalf("get: %+v %v", got, err)
	}
}

func TestPostgresImportRelistedOlderVersionKeepsHead(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	if _, err := pool.Exec(ctx, `INSERT INTO repomesh_access.accounts(id,github_id,display_name) VALUES ('owner-1',9101,'Import owner'),('owner-2',9102,'Other owner')`); err != nil {
		t.Fatal(err)
	}
	principal, err := AuthenticateDeployment(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	importer, err := NewImporter(pool, principal, projects.NewCatalogWriter())
	if err != nil {
		t.Fatal(err)
	}
	const template = `{"id":"tpl","version":"v1","executorPoolId":"pool","approvedTemplateDigest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","networkPolicyRef":"net","resourceClassId":"class","enabled":true}`
	const v1 = `{"id":"exec","version":"v1","ownerId":"owner-1","name":"执行 v1","templateId":"tpl","templateVersion":"v1","workerConcurrency":2,"verificationGroupEnabled":false}`
	const v2 = `{"id":"exec","version":"v2","ownerId":"owner-1","name":"执行 v2","templateId":"tpl","templateVersion":"v1","workerConcurrency":4,"verificationGroupEnabled":true}`
	manifest := func(importID, profiles, pinned string) ImportCommand {
		command, parseErr := ParseImport([]byte(`{"schemaVersion":1,"importId":"` + importID + `","environmentTemplates":[` + template + `],"executionProfiles":[` + profiles + `],"defaultBindings":[{"kind":"execution","ownerId":"owner-1","profileId":"exec","profileVersion":"` + pinned + `"}]}`))
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		return command
	}
	head := func() (string, string, string) {
		var version, name, pinned string
		if err := pool.QueryRow(ctx, `SELECT p.current_version,p.name,d.pinned_version FROM repomesh_projects.profiles p
			JOIN repomesh_projects.defaults d ON d.kind=p.kind AND d.profile_id=p.id AND d.actor='owner-1'
			WHERE p.kind='execution' AND p.id='exec'`).Scan(&version, &name, &pinned); err != nil {
			t.Fatal(err)
		}
		return version, name, pinned
	}
	if _, err := importer.Import(ctx, manifest("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaa1", v1, "v1")); err != nil {
		t.Fatal(err)
	}
	if version, name, pinned := head(); version != "v1" || name != "执行 v1" || pinned != "v1" {
		t.Fatalf("after v1 import head=%q name=%q pinned=%q", version, name, pinned)
	}
	if _, err := importer.Import(ctx, manifest("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaa2", v1+","+v2, "v2")); err != nil {
		t.Fatal(err)
	}
	if version, name, pinned := head(); version != "v2" || name != "执行 v2" || pinned != "v2" {
		t.Fatalf("after v2 import head=%q name=%q pinned=%q", version, name, pinned)
	}
	relisted, err := importer.Import(ctx, manifest("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaa3", v1, "v1"))
	if err != nil || relisted.Replayed || relisted.Receipt.DefaultBindings[0].ProfileVersion != "v1" {
		t.Fatalf("relist v1: %+v %v", relisted, err)
	}
	if version, name, pinned := head(); version != "v2" || name != "执行 v2" || pinned != "v1" {
		t.Fatalf("after relisting v1 head=%q name=%q pinned=%q", version, name, pinned)
	}
	var versions int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM repomesh_projects.profile_versions WHERE kind='execution' AND profile_id='exec'`).Scan(&versions); err != nil || versions != 2 {
		t.Fatalf("profile versions=%d %v", versions, err)
	}
	changed := strings.Replace(v1, `"workerConcurrency":2`, `"workerConcurrency":3`, 1)
	_, err = importer.Import(ctx, manifest("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaa4", changed, "v1"))
	var conflict *Failure
	if !errors.As(err, &conflict) || conflict.Code != "EXECUTION_CONFLICT" {
		t.Fatalf("same version different content: %v", err)
	}
	var imports int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM repomesh_sources.imports`).Scan(&imports); err != nil || imports != 3 {
		t.Fatalf("imports after conflict=%d %v", imports, err)
	}
	if version, _, pinned := head(); version != "v2" || pinned != "v1" {
		t.Fatalf("conflict changed catalog head=%q pinned=%q", version, pinned)
	}
	stolen := strings.Replace(v2, `"ownerId":"owner-1"`, `"ownerId":"owner-2"`, 1)
	_, err = importer.Import(ctx, manifest("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaa5", stolen, "v2"))
	var owner *projects.Failure
	if !errors.As(err, &owner) || owner.Code != "PROFILE_OWNER_CONFLICT" {
		t.Fatalf("other owner re-listing exec: %v", err)
	}
	var profileOwner string
	if err := pool.QueryRow(ctx, `SELECT owner FROM repomesh_projects.profiles WHERE kind='execution' AND id='exec'`).Scan(&profileOwner); err != nil || profileOwner != "owner-1" {
		t.Fatalf("profile owner after rejected import=%q %v", profileOwner, err)
	}
}

const concurrentSaveBody = `{"providerId":null,"expectedRevision":null,"name":"开发网关","baseUrl":"https://gateway.example.invalid/v1","apiFormat":"openai_chat_completions","secret":{"mode":"replace","value":"sk-concurrent-key"},"models":[{"id":null,"modelId":"deepseek-chat","displayName":"对话","contextWindow":256000,"maxOutputTokens":128000,"reasoning":true,"vision":false}]}`

func digestCookie(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func randomCookie() string {
	var value [32]byte
	_, _ = rand.Read(value[:])
	return base64.RawURLEncoding.EncodeToString(value[:])
}

func writeSecretRoot(t *testing.T) string {
	t.Helper()
	root := make([]byte, 32)
	_, _ = rand.Read(root)
	path := filepath.Join(t.TempDir(), "root")
	if err := os.WriteFile(path, root, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func newSecretStore(t *testing.T, pool *pgxpool.Pool) *secrets.Store {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	store, err := secrets.New(ctx, pool, secrets.Config{ActiveRootID: "sources-s10", Roots: []secrets.RootFile{{ID: "sources-s10", Path: writeSecretRoot(t)}}})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func sessionPrincipal(t *testing.T, pool *pgxpool.Pool, store *secrets.Store, actor string) access.ProjectPrincipal {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	auth := access.New(pool, store, nil)
	sessionCookie, bindingCookie := randomCookie(), randomCookie()
	if _, err := pool.Exec(ctx, `INSERT INTO repomesh_access.bindings(hash,identity_generation,expires_at) VALUES ($1,1,now()+interval '1 day')`, digestCookie(bindingCookie)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO repomesh_access.sessions(hash,actor,binding,generation,expires_at,last_active_at) VALUES ($1,$2,$3,1,now()+interval '1 day',now())`, digestCookie(sessionCookie), actor, digestCookie(bindingCookie)); err != nil {
		t.Fatal(err)
	}
	principal, err := auth.AuthenticateProjectRequest(ctx, sessionCookie, "", false)
	if err != nil {
		t.Fatal(err)
	}
	return principal
}

func saveProvider(service *models.Service, principal access.ProjectPrincipal, key, body string) error {
	raw, err := models.ReadSaveBody([]byte(body))
	if err != nil {
		return err
	}
	command, err := models.NewSaveCommand(key, "req-s10", raw)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	result, err := service.Save(ctx, principal, command)
	if err != nil {
		return err
	}
	if result.HTTPStatus != 201 {
		return fmt.Errorf("save status=%d", result.HTTPStatus)
	}
	return nil
}

func twoOwnerManifest(t *testing.T, importID string) ImportCommand {
	t.Helper()
	command, err := ParseImport([]byte(`{
		"schemaVersion":1,
		"importId":"` + importID + `",
		"environmentTemplates":[{"id":"tpl","version":"v1","executorPoolId":"pool","approvedTemplateDigest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","networkPolicyRef":"net","resourceClassId":"class","enabled":true}],
		"executionProfiles":[
			{"id":"exec-a","version":"v1","ownerId":"10000000-0000-4000-8000-0000000000a1","name":"执行A","templateId":"tpl","templateVersion":"v1","workerConcurrency":2,"verificationGroupEnabled":false},
			{"id":"exec-b","version":"v1","ownerId":"10000000-0000-4000-8000-0000000000a2","name":"执行B","templateId":"tpl","templateVersion":"v1","workerConcurrency":3,"verificationGroupEnabled":true}
		],
		"defaultBindings":[
			{"kind":"execution","ownerId":"10000000-0000-4000-8000-0000000000a1","profileId":"exec-a","profileVersion":"v1"},
			{"kind":"execution","ownerId":"10000000-0000-4000-8000-0000000000a2","profileId":"exec-b","profileVersion":"v1"}
		]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	return command
}

func assertTwoOwnerDefaults(t *testing.T, pool *pgxpool.Pool, saveOwner string, providers int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	const ownerA = "10000000-0000-4000-8000-0000000000a1"
	const ownerB = "10000000-0000-4000-8000-0000000000a2"
	var profileA, profileB, ownerExecA, ownerExecB string
	if err := pool.QueryRow(ctx, `SELECT d.profile_id,p.owner FROM repomesh_projects.defaults d JOIN repomesh_projects.profiles p ON p.kind=d.kind AND p.id=d.profile_id WHERE d.actor=$1 AND d.kind='execution'`, ownerA).Scan(&profileA, &ownerExecA); err != nil || profileA != "exec-a" || ownerExecA != ownerA {
		t.Fatalf("owner A default profile=%q owner=%q err=%v", profileA, ownerExecA, err)
	}
	if err := pool.QueryRow(ctx, `SELECT d.profile_id,p.owner FROM repomesh_projects.defaults d JOIN repomesh_projects.profiles p ON p.kind=d.kind AND p.id=d.profile_id WHERE d.actor=$1 AND d.kind='execution'`, ownerB).Scan(&profileB, &ownerExecB); err != nil || profileB != "exec-b" || ownerExecB != ownerB {
		t.Fatalf("owner B default profile=%q owner=%q err=%v", profileB, ownerExecB, err)
	}
	var saved int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM repomesh_models.providers WHERE owner=$1`, saveOwner).Scan(&saved); err != nil || saved != providers {
		t.Fatalf("providers for %s = %d want %d", saveOwner, saved, providers)
	}
	var crossed int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM repomesh_projects.defaults WHERE actor=$1 AND kind='execution' AND profile_id='exec-b'`, ownerA).Scan(&crossed); err != nil || crossed != 0 {
		t.Fatalf("owner A received B default count=%d", crossed)
	}
}

func TestPostgresConcurrentImportAndProviderSave(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	const ownerA = "10000000-0000-4000-8000-0000000000a1"
	const ownerB = "10000000-0000-4000-8000-0000000000a2"
	if _, err := pool.Exec(ctx, `INSERT INTO repomesh_access.accounts(id,github_id,display_name) VALUES ($1,9201,'Import A'),($2,9202,'Import B')`, ownerA, ownerB); err != nil {
		t.Fatal(err)
	}
	principal, err := AuthenticateDeployment(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	importer, err := NewImporter(pool, principal, projects.NewCatalogWriter())
	if err != nil {
		t.Fatal(err)
	}
	store := newSecretStore(t, pool)
	actor := sessionPrincipal(t, pool, store, ownerA)
	service := models.New(pool, access.New(pool, store, nil), store, projects.NewCatalogWriter())
	for round := range 8 {
		command := twoOwnerManifest(t, fmt.Sprintf("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbb%02d", round))
		key := fmt.Sprintf("72000000-0000-4000-8000-0000000000%02d", round)
		imported := make(chan error, 1)
		saved := make(chan error, 1)
		go func() {
			_, importErr := importer.Import(ctx, command)
			imported <- importErr
		}()
		go func() {
			saved <- saveProvider(service, actor, key, concurrentSaveBody)
		}()
		select {
		case err := <-imported:
			if err != nil {
				t.Fatalf("import round %d: %v", round, err)
			}
		case <-time.After(15 * time.Second):
			t.Fatalf("import round %d deadlocked", round)
		}
		select {
		case err := <-saved:
			if err != nil {
				t.Fatalf("save round %d: %v", round, err)
			}
		case <-time.After(15 * time.Second):
			t.Fatalf("save round %d deadlocked", round)
		}
	}
	assertTwoOwnerDefaults(t, pool, ownerA, 8)
}

func TestPostgresImportHoldsOwnersWhileOtherOwnerSaves(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	const ownerA = "10000000-0000-4000-8000-0000000000a1"
	const ownerB = "10000000-0000-4000-8000-0000000000a2"
	const ownerC = "10000000-0000-4000-8000-0000000000a3"
	if _, err := pool.Exec(ctx, `INSERT INTO repomesh_access.accounts(id,github_id,display_name) VALUES ($1,9201,'Import A'),($2,9202,'Import B'),($3,9203,'Save C')`, ownerA, ownerB, ownerC); err != nil {
		t.Fatal(err)
	}
	principal, err := AuthenticateDeployment(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	importer, err := NewImporter(pool, principal, projects.NewCatalogWriter())
	if err != nil {
		t.Fatal(err)
	}
	ownersHeld := make(chan struct{})
	releaseOwners := make(chan struct{})
	importer.hook = func(hookCtx context.Context, phase importPhase) error {
		if phase != ownersLocked {
			return nil
		}
		close(ownersHeld)
		select {
		case <-releaseOwners:
			return nil
		case <-hookCtx.Done():
			return hookCtx.Err()
		}
	}
	command := twoOwnerManifest(t, "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbc1")
	imported := make(chan error, 1)
	go func() {
		_, importErr := importer.Import(ctx, command)
		imported <- importErr
	}()
	select {
	case <-ownersHeld:
	case <-time.After(5 * time.Second):
		t.Fatal("import did not lock owners")
	}
	store := newSecretStore(t, pool)
	actor := sessionPrincipal(t, pool, store, ownerC)
	service := models.New(pool, access.New(pool, store, nil), store, projects.NewCatalogWriter())
	saved := make(chan error, 1)
	go func() {
		saved <- saveProvider(service, actor, "72000000-0000-4000-8000-0000000000c1", concurrentSaveBody)
	}()
	select {
	case err := <-saved:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("provider save waited for import owner locks")
	}
	close(releaseOwners)
	select {
	case err := <-imported:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("import did not finish after owner release")
	}
	assertTwoOwnerDefaults(t, pool, ownerC, 1)
}

func TestPostgresDeploymentRoleRejected(t *testing.T) {
	pool, databaseURL := testdb.OpenWithURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	if _, err := AuthenticateDeployment(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatal(err)
	}
	role := "repomesh_s11_" + hex.EncodeToString(suffix[:])
	if _, err := pool.Exec(ctx, `CREATE ROLE `+role+` NOLOGIN`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		_, _ = pool.Exec(cleanup, `DROP ROLE IF EXISTS `+role)
	})
	if _, err := pool.Exec(ctx, `GRANT `+role+` TO CURRENT_USER`); err != nil {
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	config.AfterConnect = func(connectCtx context.Context, conn *pgx.Conn) error {
		_, roleErr := conn.Exec(connectCtx, "SET ROLE "+role)
		return roleErr
	}
	restricted, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restricted.Close)
	_, err = AuthenticateDeployment(ctx, restricted)
	var forbidden *Failure
	if !errors.As(err, &forbidden) || forbidden.Code != "DEPLOYMENT_FORBIDDEN" {
		t.Fatalf("restricted role: %v", err)
	}
	if _, err = AuthenticateDeployment(ctx, pool); err != nil {
		t.Fatal(err)
	}
}
