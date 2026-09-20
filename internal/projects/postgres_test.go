package projects

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/github"
	"repomesh.local/repomesh/internal/secrets"
	"repomesh.local/repomesh/internal/testdb"
)

type projectTestProvider struct {
	mu                 sync.Mutex
	modes              map[int64]string
	repositoryOverride func(string, string) (github.Repository, error)
}

func (*projectTestProvider) AuthorizationURL(state, verifier string) string {
	return "https://github.invalid/authorize?state=" + url.QueryEscape(state) + "&code_challenge=" + url.QueryEscape(verifier)
}
func (*projectTestProvider) Exchange(context.Context, string, string) (github.TokenSet, error) {
	return github.TokenSet{AccessToken: "project-test-token"}, nil
}
func (*projectTestProvider) Refresh(context.Context, string) (github.TokenSet, error) {
	return github.TokenSet{}, &github.Error{Kind: "unavailable"}
}
func (*projectTestProvider) Identity(context.Context, string) (github.Identity, error) {
	return github.Identity{ID: 7301, DisplayName: "Project test actor"}, nil
}
func (p *projectTestProvider) Repositories(context.Context, string, int) (github.RepositoryPage, error) {
	return github.RepositoryPage{Items: []github.Repository{
		{ID: 7311, Owner: "project-test", Name: "one", FullName: "project-test/one"},
		{ID: 7312, Owner: "project-test", Name: "two", FullName: "project-test/two"},
	}}, nil
}
func (p *projectTestProvider) Repository(_ context.Context, _ string, owner, name string) (github.Repository, error) {
	if p.repositoryOverride != nil {
		return p.repositoryOverride(owner, name)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, repository := range []github.Repository{
		{ID: 7311, Owner: "project-test", Name: "one", FullName: "project-test/one"},
		{ID: 7312, Owner: "project-test", Name: "two", FullName: "project-test/two"},
	} {
		if repository.Owner == owner && repository.Name == name {
			switch p.modes[repository.ID] {
			case "denied":
				return github.Repository{}, &github.Error{Kind: "denied"}
			case "unknown":
				return github.Repository{}, &github.Error{Kind: "unavailable"}
			default:
				return repository, nil
			}
		}
	}
	return github.Repository{}, &github.Error{Kind: "denied"}
}
func (*projectTestProvider) AppCapability(context.Context, string, string) (github.Capability, error) {
	now := time.Now().UTC()
	return github.Capability{Status: "denied", ReasonCodes: []string{"APP_INSTALLATION_MISSING"}, ObservedAt: &now}, nil
}

func (p *projectTestProvider) ObserveAppInstallation(ctx context.Context, owner, name string) (github.AppInstallationObservation, error) {
	capability, err := p.AppCapability(ctx, owner, name)
	if err != nil {
		return github.AppInstallationObservation{}, err
	}
	permissions := map[string]string{"contents": "write", "pull_requests": "write", "metadata": "read"}
	return github.ObservedInstallation(1, 1001, false, permissions, capability, *capability.ObservedAt), nil
}

func TestPostgresProjectTransactionsAndAuthorizationInterleaving(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	root := make([]byte, 32)
	if _, err := rand.Read(root); err != nil {
		t.Fatal(err)
	}
	rootPath := filepath.Join(t.TempDir(), "root.key")
	if err := os.WriteFile(rootPath, root, 0600); err != nil {
		t.Fatal(err)
	}
	store, err := secrets.New(ctx, pool, secrets.Config{ActiveRootID: "projects-test", Roots: []secrets.RootFile{{ID: "projects-test", Path: rootPath}}})
	if err != nil {
		t.Fatal(err)
	}
	provider := &projectTestProvider{modes: map[int64]string{}}
	authorization := access.New(pool, store, provider)
	service := New(pool, authorization)

	newUUID := func() string {
		var value [16]byte
		if _, err := rand.Read(value[:]); err != nil {
			t.Fatal(err)
		}
		value[6] = value[6]&15 | 64
		value[8] = value[8]&63 | 128
		return hex.EncodeToString(value[:4]) + "-" + hex.EncodeToString(value[4:6]) + "-" + hex.EncodeToString(value[6:8]) + "-" + hex.EncodeToString(value[8:10]) + "-" + hex.EncodeToString(value[10:])
	}
	destination, err := access.ParseDestination([]byte(`{"kind":"home"}`))
	if err != nil {
		t.Fatal(err)
	}
	login := func(binding, session, purpose string) (string, string) {
		csrf := ""
		if session != "" {
			view, sessionErr := authorization.Session(ctx, session)
			if sessionErr != nil {
				t.Fatal(sessionErr)
			}
			csrf = view.CSRFToken
		}
		started, startErr := authorization.Start(ctx, access.StartCommand{ID: newUUID(), Purpose: purpose, BindingCookie: binding, SessionCookie: session, CSRF: csrf, Destination: destination})
		if startErr != nil {
			t.Fatal(startErr)
		}
		parsed, parseErr := url.Parse(started.Result.AuthorizationURL)
		if parseErr != nil {
			t.Fatal("login authorization URL parsing failed")
		}
		callbackBinding := started.BindingCookie
		if callbackBinding == "" {
			callbackBinding = binding
		}
		completed, callbackErr := authorization.CompleteCallback(ctx, access.Callback{BindingCookie: callbackBinding, State: parsed.Query().Get("state"), Code: "project-test"})
		if callbackErr != nil || completed.SessionCookie == "" {
			t.Fatalf("login callback failed: cookiePresent=%t callbackError=%t", completed.SessionCookie != "", callbackErr != nil)
		}
		return callbackBinding, completed.SessionCookie
	}
	bindingCookie, sessionCookie := login("", "", "login")
	session, err := authorization.Session(ctx, sessionCookie)
	if err != nil {
		t.Fatal(err)
	}
	principal, err := authorization.AuthenticateProjectRequest(ctx, sessionCookie, session.CSRFToken, true)
	if err != nil {
		t.Fatal(err)
	}

	// A project can exist before any repository is selected. Later explicit
	// joining uses the same revision/idempotency boundary as other updates.
	emptyRaw, err := ParseRawInput([]byte(`{"name":"Scope draft","purpose":"Join later","repositoryIds":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	emptyCommand, err := PrepareCreate(emptyRaw, newUUID())
	if err != nil {
		t.Fatal(err)
	}
	empty, err := service.Create(ctx, principal, emptyCommand)
	if err != nil {
		t.Fatalf("create empty draft: %v", err)
	}
	draft, err := service.Get(ctx, principal, empty.Receipt.ProjectID)
	if err != nil || draft.Actions.CanCreateIssue || draft.CreationReadiness.Status == "ready" {
		t.Fatalf("empty project readiness: %+v err=%v", draft, err)
	}
	joined, err := service.Repositories(ctx, principal, draft.ID, PageQuery{Limit: 100})
	if err != nil || len(joined.Items) != 0 {
		t.Fatalf("empty membership: %+v %v", joined, err)
	}
	for attempt := 0; attempt < 10; attempt++ {
		_, repositoryErr := authorization.Repositories(ctx, sessionCookie, access.RepositoryQuery{Limit: 50})
		if repositoryErr == nil {
			break
		}
		if _, runErr := authorization.RunOne(ctx); runErr != nil {
			t.Fatal(runErr)
		}
		if attempt == 9 {
			t.Fatal(repositoryErr)
		}
	}

	addRaw, err := ParseRawInput([]byte(`{"expectedProjectRevision":"` + draft.ProjectRevision + `","repositoryIdsToAdd":["repo_00000000000000007311"]}`))
	if err != nil {
		t.Fatal(err)
	}
	addCommand, err := PrepareUpdate(addRaw, draft.ID, newUUID())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Update(ctx, principal, addCommand); err != nil {
		t.Fatalf("join draft: %v", err)
	}
	joined, err = service.Repositories(ctx, principal, draft.ID, PageQuery{Limit: 100})
	if err != nil || len(joined.Items) != 1 {
		t.Fatalf("joined membership: %+v %v", joined, err)
	}

	createCommand := func(key, name string) CreateCommand {
		raw, parseErr := ParseRawInput([]byte(`{"name":"` + name + `","purpose":"transaction test","repositoryIds":["repo_00000000000000007311"]}`))
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		command, prepareErr := PrepareCreate(raw, key)
		if prepareErr != nil {
			t.Fatal(prepareErr)
		}
		return command
	}
	wantFailure := func(got error, status int, code string) {
		var projectFailure *Failure
		var accessFailure *access.Failure
		if errors.As(got, &projectFailure) && projectFailure.Status == status && projectFailure.Code == code {
			return
		}
		if errors.As(got, &accessFailure) && accessFailure.Status == status && accessFailure.Code == code {
			return
		}
		t.Fatalf("error=%v; want %d %s", got, status, code)
	}

	incompleteKey := "10000000-0000-1000-0000-000000000001"
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO repomesh_projects.creation_operations(actor,key,schema_version,canonical_input,exact_input) VALUES($1,$2,1,$3,$4)`, principal.ActorID(), incompleteKey, []byte(`{"a":1}`), []byte(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err == nil {
		t.Fatal("incomplete operation committed")
	}
	var incompleteCount int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM repomesh_projects.creation_operations WHERE actor=$1 AND key=$2`, principal.ActorID(), incompleteKey).Scan(&incompleteCount); err != nil || incompleteCount != 0 {
		t.Fatalf("incomplete operation survived count=%d err=%v", incompleteCount, err)
	}

	rollbackPhases := []transactionPhase{operationInserted, projectWritten, repositoryWritten, configurationWritten, projectReferenceWritten, receiptWritten, beforeCommit}
	for index, target := range rollbackPhases {
		key := "11000000-0000-1000-0000-" + hex.EncodeToString([]byte{0, 0, 0, 0, 0, byte(index + 1)})
		service.hook = func(_ context.Context, phase transactionPhase) error {
			if phase == target {
				return errors.New("injected rollback")
			}
			return nil
		}
		if _, err = service.Create(ctx, principal, createCommand(key, "rollback")); err == nil {
			t.Fatalf("create phase %s did not fail", target)
		}
		var operations, projects int
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM repomesh_projects.creation_operations WHERE actor=$1 AND key=$2`, principal.ActorID(), key).Scan(&operations); err != nil {
			t.Fatal(err)
		}
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM repomesh_projects.projects WHERE owner=$1`, principal.ActorID()).Scan(&projects); err != nil || operations != 0 || projects != 1 {
			t.Fatalf("create rollback phase=%s operations=%d projects=%d err=%v", target, operations, projects, err)
		}
	}
	service.hook = nil
	createKey := "12000000-0000-1000-0000-000000000001"
	created, err := service.Create(ctx, principal, createCommand(createKey, "baseline"))
	if err != nil || !created.FirstCommit {
		t.Fatalf("baseline create result=%+v err=%v", created, err)
	}
	_, normalized, err := parseCreate(createCommand(createKey, "baseline").input, 1)
	if err != nil {
		t.Fatal(err)
	}
	var canonical, exact []byte
	var canonicalType, exactType string
	if err = pool.QueryRow(ctx, `SELECT canonical_input,exact_input,pg_typeof(canonical_input)::text,pg_typeof(exact_input)::text FROM repomesh_projects.creation_operations WHERE actor=$1 AND key=$2`, principal.ActorID(), createKey).Scan(&canonical, &exact, &canonicalType, &exactType); err != nil {
		t.Fatal(err)
	}
	if string(canonical) != string(normalized.canonical) || string(exact) != string(normalized.exact) || canonicalType != "bytea" || exactType != "bytea" {
		t.Fatalf("operation bytes/type mismatch canonical=%q exact=%q types=%s/%s", canonical, exact, canonicalType, exactType)
	}

	for index, target := range rollbackPhases {
		key := "13000000-0000-1000-0000-" + hex.EncodeToString([]byte{0, 0, 0, 0, 0, byte(index + 1)})
		raw, parseErr := ParseRawInput([]byte(`{"expectedProjectRevision":"` + created.Receipt.ProjectRevision + `","name":"rollback update"}`))
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		command, prepareErr := PrepareUpdate(raw, created.Receipt.ProjectID, key)
		if prepareErr != nil {
			t.Fatal(prepareErr)
		}
		service.hook = func(_ context.Context, phase transactionPhase) error {
			if phase == target {
				return errors.New("injected rollback")
			}
			return nil
		}
		if _, err = service.Update(ctx, principal, command); err == nil {
			t.Fatalf("update phase %s did not fail", target)
		}
		var operations int
		var name, revision string
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM repomesh_projects.update_operations WHERE project_id=$1 AND actor=$2 AND key=$3`, created.Receipt.ProjectID, principal.ActorID(), key).Scan(&operations); err != nil {
			t.Fatal(err)
		}
		if err = pool.QueryRow(ctx, `SELECT name,revision FROM repomesh_projects.projects WHERE id=$1`, created.Receipt.ProjectID).Scan(&name, &revision); err != nil || operations != 0 || name != "baseline" || revision != created.Receipt.ProjectRevision {
			t.Fatalf("update rollback phase=%s operations=%d name=%q revision=%q err=%v", target, operations, name, revision, err)
		}
	}

	service.hook = nil
	createHeld := make(chan struct{})
	createRelease := make(chan struct{})
	service.hook = func(ctx context.Context, phase transactionPhase) error {
		if phase == authorizationObserved {
			close(createHeld)
			select {
			case <-createRelease:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}
	staleCreateKey := "14000000-0000-1000-0000-000000000001"
	createResult := make(chan error, 1)
	go func() {
		_, createErr := service.Create(ctx, principal, createCommand(staleCreateKey, "stale authorization"))
		createResult <- createErr
	}()
	<-createHeld
	_, newSessionCookie := login(bindingCookie, sessionCookie, "reconnect")
	close(createRelease)
	wantFailure(<-createResult, 401, "AUTHENTICATION_REQUIRED")
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM repomesh_projects.creation_operations WHERE actor=$1 AND key=$2`, principal.ActorID(), staleCreateKey).Scan(&incompleteCount); err != nil || incompleteCount != 0 {
		t.Fatalf("stale authorization create survived count=%d err=%v", incompleteCount, err)
	}
	newSession, err := authorization.Session(ctx, newSessionCookie)
	if err != nil {
		t.Fatal("new reconnect session is not valid", err)
	}
	principal, err = authorization.AuthenticateProjectRequest(ctx, newSessionCookie, newSession.CSRFToken, true)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 10; attempt++ {
		_, repositoryErr := authorization.Repositories(ctx, newSessionCookie, access.RepositoryQuery{Limit: 50})
		if repositoryErr == nil {
			break
		}
		if _, runErr := authorization.RunOne(ctx); runErr != nil {
			t.Fatal(runErr)
		}
		if attempt == 9 {
			t.Fatal(repositoryErr)
		}
	}

	service.hook = nil
	secondCreateKey := "15000000-0000-1000-0000-000000000001"
	second, err := service.Create(ctx, principal, createCommand(secondCreateKey, "epoch baseline"))
	if err != nil {
		t.Fatal(err)
	}
	updateRaw, err := ParseRawInput([]byte(`{"expectedProjectRevision":"` + second.Receipt.ProjectRevision + `","repositoryIdsToAdd":["repo_00000000000000007312"],"purpose":"must not commit"}`))
	if err != nil {
		t.Fatal(err)
	}
	updateCommand, err := PrepareUpdate(updateRaw, second.Receipt.ProjectID, "16000000-0000-1000-0000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	updateHeld := make(chan struct{})
	updateRelease := make(chan struct{})
	service.hook = func(ctx context.Context, phase transactionPhase) error {
		if phase == authorizationObserved {
			close(updateHeld)
			select {
			case <-updateRelease:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}
	updateResult := make(chan error, 1)
	go func() {
		_, updateErr := service.Update(ctx, principal, updateCommand)
		updateResult <- updateErr
	}()
	<-updateHeld
	if _, err = pool.Exec(ctx, `UPDATE repomesh_access.connections SET access_epoch=access_epoch+1 WHERE actor=$1`, principal.ActorID()); err != nil {
		t.Fatal(err)
	}
	close(updateRelease)
	wantFailure(<-updateResult, 503, "AUTHORIZATION_UNCONFIRMED")
	var revision, purpose string
	var repositoryCount, updateCount int
	if err = pool.QueryRow(ctx, `SELECT revision,purpose FROM repomesh_projects.projects WHERE id=$1`, second.Receipt.ProjectID).Scan(&revision, &purpose); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM repomesh_projects.project_repositories WHERE project_id=$1`, second.Receipt.ProjectID).Scan(&repositoryCount); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM repomesh_projects.update_operations WHERE project_id=$1 AND actor=$2 AND key=$3`, second.Receipt.ProjectID, principal.ActorID(), "16000000-0000-1000-0000-000000000001").Scan(&updateCount); err != nil {
		t.Fatal(err)
	}
	if revision != second.Receipt.ProjectRevision || purpose != "transaction test" || repositoryCount != 1 || updateCount != 0 {
		t.Fatalf("epoch race committed revision=%s purpose=%q repositories=%d operations=%d", revision, purpose, repositoryCount, updateCount)
	}
	if _, err = authorization.Session(ctx, newSessionCookie); err != nil {
		t.Fatal("current session was invalidated by credential epoch", err)
	}
	service.hook = nil
	for attempt := 0; attempt < 10; attempt++ {
		_, repositoryErr := authorization.Repositories(ctx, newSessionCookie, access.RepositoryQuery{Limit: 50})
		if repositoryErr == nil {
			break
		}
		if _, runErr := authorization.RunOne(ctx); runErr != nil {
			t.Fatal(runErr)
		}
		if attempt == 9 {
			t.Fatal(repositoryErr)
		}
	}

	var repositoryCalls atomic.Int32
	failedObservation := make(chan struct{})
	releaseFailedObservation := make(chan struct{})
	provider.repositoryOverride = func(owner, name string) (github.Repository, error) {
		if repositoryCalls.Add(1) == 1 {
			close(failedObservation)
			<-releaseFailedObservation
			return github.Repository{}, &github.Error{Kind: "unavailable"}
		}
		return github.Repository{ID: 7311, Owner: owner, Name: name, FullName: owner + "/" + name}, nil
	}
	winnerKey := "17000000-0000-1000-0000-000000000001"
	winnerCommand := createCommand(winnerKey, "winner replay")
	firstResult := make(chan CreateResult, 1)
	firstError := make(chan error, 1)
	go func() {
		result, createErr := service.Create(ctx, principal, winnerCommand)
		firstResult <- result
		firstError <- createErr
	}()
	<-failedObservation
	winner, err := service.Create(ctx, principal, winnerCommand)
	if err != nil || !winner.FirstCommit {
		t.Fatalf("concurrent winner result=%+v err=%v", winner, err)
	}
	close(releaseFailedObservation)
	loser := <-firstResult
	if err = <-firstError; err != nil || loser.FirstCommit || loser.Receipt != winner.Receipt {
		t.Fatalf("failed observation did not replay winner result=%+v err=%v winner=%+v", loser, err, winner)
	}
	provider.repositoryOverride = nil

	service.hook = nil
}

func TestPostgresInheritFollowsPinnedOrCurrentVersion(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	const actor = "owner-1"
	if _, err := pool.Exec(ctx, `INSERT INTO repomesh_access.accounts(id,github_id,display_name) VALUES ($1,9201,'Inherit owner')`, actor); err != nil {
		t.Fatal(err)
	}
	writer := NewCatalogWriter()
	inCatalog := func(label string, write func(tx pgx.Tx) error) {
		t.Helper()
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer rollback(tx)
		if err := writer.LockExclusive(ctx, tx); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		if err := write(tx); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
	}
	register := func(version string, concurrency int) {
		t.Helper()
		inCatalog("register "+version, func(tx pgx.Tx) error {
			return writer.RegisterExecutionVersion(ctx, tx, ExecutionVersionRegistration{Owner: actor, ProfileID: "exec", Version: version, Name: "执行 " + version, WorkerConcurrency: concurrency})
		})
	}
	resolve := func() (string, int, bool) {
		t.Helper()
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer rollback(tx)
		binding, version, err := resolveProfile(ctx, tx, actor, "execution", ProfileChoice{Mode: "inherit"})
		if err != nil || binding == nil || version == nil || version.workerConcurrency == nil || binding.DefaultRevision == nil {
			t.Fatalf("inherit resolution: binding=%+v version=%+v err=%v", binding, version, err)
		}
		return binding.Version, *version.workerConcurrency, version.parametersComplete
	}
	head := func() string {
		t.Helper()
		var version string
		if err := pool.QueryRow(ctx, `SELECT current_version FROM repomesh_projects.profiles WHERE kind='execution' AND id='exec'`).Scan(&version); err != nil {
			t.Fatal(err)
		}
		return version
	}

	register("v1", 2)
	// A B03 default row predates pinned_version, so it carries NULL there.
	if _, err := pool.Exec(ctx, `INSERT INTO repomesh_projects.defaults(actor,kind,profile_id,default_revision) VALUES ($1,'execution','exec',$2)`, actor, newID()); err != nil {
		t.Fatal(err)
	}
	if version, concurrency, complete := resolve(); version != "v1" || concurrency != 2 || complete {
		t.Fatalf("null pinned at v1 head: version=%s concurrency=%d complete=%t", version, concurrency, complete)
	}
	register("v2", 4)
	if version, concurrency, _ := resolve(); version != "v2" || concurrency != 4 {
		t.Fatalf("null pinned must follow the head: version=%s concurrency=%d", version, concurrency)
	}
	inCatalog("pin v1", func(tx pgx.Tx) error {
		_, err := writer.BindExecutionDefault(ctx, tx, actor, "exec", "v1")
		return err
	})
	if version, concurrency, complete := resolve(); version != "v1" || concurrency != 2 || complete {
		t.Fatalf("pinned v1 under v2 head: version=%s concurrency=%d complete=%t", version, concurrency, complete)
	}
	if head() != "v2" {
		t.Fatalf("pinning moved the head to %s", head())
	}
}

// archiveTestHarness 备好「已登录账号 + 项目服务」这套开局，与
// TestPostgresProjectTransactionsAndAuthorizationInterleaving 的脚手架一致，
// 只是收拢成函数，归档用例不再复制 80 行。
func newArchiveTestHarness(t *testing.T) (context.Context, *Service, access.ProjectPrincipal, func() string) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	root := make([]byte, 32)
	if _, err := rand.Read(root); err != nil {
		t.Fatal(err)
	}
	rootPath := filepath.Join(t.TempDir(), "root.key")
	if err := os.WriteFile(rootPath, root, 0600); err != nil {
		t.Fatal(err)
	}
	store, err := secrets.New(ctx, pool, secrets.Config{ActiveRootID: "projects-test", Roots: []secrets.RootFile{{ID: "projects-test", Path: rootPath}}})
	if err != nil {
		t.Fatal(err)
	}
	provider := &projectTestProvider{modes: map[int64]string{}}
	authorization := access.New(pool, store, provider)
	service := New(pool, authorization)

	newUUID := func() string {
		var value [16]byte
		if _, err := rand.Read(value[:]); err != nil {
			t.Fatal(err)
		}
		value[6] = value[6]&15 | 64
		value[8] = value[8]&63 | 128
		return hex.EncodeToString(value[:4]) + "-" + hex.EncodeToString(value[4:6]) + "-" + hex.EncodeToString(value[6:8]) + "-" + hex.EncodeToString(value[8:10]) + "-" + hex.EncodeToString(value[10:])
	}
	destination, err := access.ParseDestination([]byte(`{"kind":"home"}`))
	if err != nil {
		t.Fatal(err)
	}
	started, err := authorization.Start(ctx, access.StartCommand{ID: newUUID(), Purpose: "login", BindingCookie: "", SessionCookie: "", CSRF: "", Destination: destination})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(started.Result.AuthorizationURL)
	if err != nil {
		t.Fatal("authorization URL parsing failed")
	}
	completed, err := authorization.CompleteCallback(ctx, access.Callback{BindingCookie: started.BindingCookie, State: parsed.Query().Get("state"), Code: "project-test"})
	if err != nil || completed.SessionCookie == "" {
		t.Fatalf("login callback failed: cookiePresent=%t err=%t", completed.SessionCookie != "", err != nil)
	}
	session, err := authorization.Session(ctx, completed.SessionCookie)
	if err != nil {
		t.Fatal(err)
	}
	principal, err := authorization.AuthenticateProjectRequest(ctx, completed.SessionCookie, session.CSRFToken, true)
	if err != nil {
		t.Fatal(err)
	}
	return ctx, service, principal, newUUID
}

// 归档（软删除）回归锁：墓碑落下 → 在役读面消失、归档列表出现 → 还原即回来；
// 重复归档/重复还原 409；不存在（或不是你的）项目 404。
// 归属判据与 Get/List 同一条 owner 过滤，跨账号场景由既有授权用例覆盖。
func TestPostgresProjectArchiveAndRestore(t *testing.T) {
	ctx, service, principal, newUUID := newArchiveTestHarness(t)

	raw, err := ParseRawInput([]byte(`{"name":"Archive target","purpose":"Archive me","repositoryIds":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	command, err := PrepareCreate(raw, newUUID())
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.Create(ctx, principal, command)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	projectID := created.Receipt.ProjectID
	inList := func() bool {
		page, err := service.List(ctx, principal, ListQuery{Limit: 50})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		for _, item := range page.Items {
			if item.ID == projectID {
				return true
			}
		}
		return false
	}
	asFailure := func(err error) (*Failure, bool) {
		var result *Failure
		return result, errors.As(err, &result)
	}

	if !inList() {
		t.Fatalf("project %s missing from active list before archiving", projectID)
	}

	receipt, err := service.Archive(ctx, principal, projectID)
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	if !receipt.Archived || receipt.RemovedAt == nil {
		t.Fatalf("archive receipt = %+v; want archived with a timestamp", receipt)
	}
	if inList() {
		t.Fatalf("archived project %s still in the active list", projectID)
	}
	_, err = service.Get(ctx, principal, projectID)
	if got, ok := asFailure(err); !ok || got.Status != 404 {
		t.Fatalf("get archived project = %v; want 404", err)
	}
	archived, err := service.Archived(ctx, principal)
	if err != nil {
		t.Fatalf("archived list: %v", err)
	}
	var found bool
	for _, item := range archived.Items {
		if item.ID == projectID {
			found = true
			if item.RemovedAt == nil {
				t.Fatal("archived list row carries no removedAt")
			}
		}
	}
	if !found {
		t.Fatalf("archived project %s missing from the archived list", projectID)
	}
	_, err = service.Archive(ctx, principal, projectID)
	if got, ok := asFailure(err); !ok || got.Status != 409 || got.Code != "PROJECT_ALREADY_ARCHIVED" {
		t.Fatalf("second archive = %v; want 409 PROJECT_ALREADY_ARCHIVED", err)
	}

	restored, err := service.Restore(ctx, principal, projectID)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if restored.Archived || restored.RemovedAt != nil {
		t.Fatalf("restore receipt = %+v; want live with no timestamp", restored)
	}
	if !inList() {
		t.Fatalf("restored project %s not back in the active list", projectID)
	}
	archived, err = service.Archived(ctx, principal)
	if err != nil {
		t.Fatalf("archived list after restore: %v", err)
	}
	for _, item := range archived.Items {
		if item.ID == projectID {
			t.Fatalf("restored project %s still in the archived list", projectID)
		}
	}
	_, err = service.Restore(ctx, principal, projectID)
	if got, ok := asFailure(err); !ok || got.Status != 409 || got.Code != "PROJECT_NOT_ARCHIVED" {
		t.Fatalf("second restore = %v; want 409 PROJECT_NOT_ARCHIVED", err)
	}
	_, err = service.Archive(ctx, principal, newUUID())
	if got, ok := asFailure(err); !ok || got.Status != 404 {
		t.Fatalf("archive unknown project = %v; want 404", err)
	}
}
