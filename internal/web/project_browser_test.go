package web

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"
	"repomesh.local/repomesh/internal/access"
	"repomesh.local/repomesh/internal/github"
	"repomesh.local/repomesh/internal/jsoninput"
	"repomesh.local/repomesh/internal/models"
	"repomesh.local/repomesh/internal/projects"
	"repomesh.local/repomesh/internal/secrets"
	"repomesh.local/repomesh/internal/testdb"
)

type browserFixtureIDs struct {
	ActorA           string `json:"actorA"`
	ActorB           string `json:"actorB"`
	RepositoryA      string `json:"repositoryA"`
	RepositoryB      string `json:"repositoryB"`
	ModelProfile     string `json:"modelProfile"`
	ExecutionProfile string `json:"executionProfile"`
}
type browserFixtureOptions struct {
	EmptyCatalog bool
}
type browserTestManifest struct {
	Origin       string            `json:"origin"`
	LoginPath    string            `json:"loginPath"`
	ControlPath  string            `json:"controlPath"`
	StatusPath   string            `json:"statusPath"`
	EmptyCatalog bool              `json:"emptyCatalog"`
	Fixtures     browserFixtureIDs `json:"fixtures"`
}
type browserControl struct {
	Action       string `json:"action"`
	Method       string `json:"method,omitempty"`
	Path         string `json:"path,omitempty"`
	RepositoryID string `json:"repositoryId,omitempty"`
	Mode         string `json:"mode,omitempty"`
	Actor        string `json:"actor,omitempty"`
	Kind         string `json:"kind,omitempty"`
}
type browserControlResult struct {
	Accepted     bool `json:"accepted"`
	BarrierHeld  bool `json:"barrierHeld"`
	ResponseHeld bool `json:"responseHeld"`
	RequestHeld  bool `json:"requestHeld"`
}
type browserTestServer struct {
	pool              *pgxpool.Pool
	auth              *access.Service
	store             *secrets.Store
	provider          *browserProvider
	server            *httptest.Server
	fixtures          browserFixtureIDs
	emptyCatalog      bool
	mu                sync.Mutex
	dropMethod        string
	dropPath          string
	holdMethod        string
	holdPath          string
	responseRelease   chan struct{}
	responseHeld      bool
	nextRequestMethod string
	nextRequestPath   string
	requestRelease    chan struct{}
	requestHeld       bool
	barrierRelease    chan struct{}
	barrierHeld       bool
	shutdown          chan struct{}
	rootConfig        secrets.Config
	stopDiscovery     context.CancelFunc
	discoveryDone     <-chan error
	closeOnce         sync.Once
	closeErr          error
}
type browserProvider struct {
	mu           sync.Mutex
	repositories map[int64]github.Repository
	modes        map[int64]string
	tokens       map[string]int64
}

func TestProjectBrowserServer(t *testing.T) {
	if os.Getenv("REPOMESH_B03_BROWSER_TEST") != "1" {
		t.Skip("B03 browser server disabled")
	}
	assets := os.Getenv("REPOMESH_B03_BROWSER_ASSETS")
	directory := os.Getenv("REPOMESH_B03_BROWSER_MANIFEST_DIR")
	duration, err := time.ParseDuration(os.Getenv("REPOMESH_B03_BROWSER_DURATION"))
	if assets == "" || directory == "" || err != nil || duration <= 0 || duration > 30*time.Minute {
		t.Fatal("B03 browser server requires assets, manifest directory, and a duration of at most 30m")
	}
	var options browserFixtureOptions
	switch os.Getenv("REPOMESH_B03_BROWSER_EMPTY_CATALOG") {
	case "", "0":
	case "1":
		options.EmptyCatalog = true
	default:
		t.Fatal("REPOMESH_B03_BROWSER_EMPTY_CATALOG must be 0 or 1")
	}
	server := startProjectBrowserServerWithOptions(t, assets, options)
	if err := writeBrowserManifest(directory, server.manifest()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-server.shutdown:
	case <-time.After(duration):
	}
}

func startProjectBrowserServer(t *testing.T, assets string) *browserTestServer {
	t.Helper()
	return startProjectBrowserServerWithOptions(t, assets, browserFixtureOptions{})
}

func startProjectBrowserServerWithOptions(t *testing.T, assets string, options browserFixtureOptions) *browserTestServer {
	t.Helper()
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	root := make([]byte, 32)
	_, _ = rand.Read(root)
	rootPath := filepath.Join(t.TempDir(), "browser-root.key")
	if err := os.WriteFile(rootPath, root, 0600); err != nil {
		t.Fatal(err)
	}
	rootConfig := secrets.Config{ActiveRootID: "b03-browser", Roots: []secrets.RootFile{{ID: "b03-browser", Path: rootPath}}}
	store, err := secrets.New(ctx, pool, rootConfig)
	if err != nil {
		t.Fatal(err)
	}
	provider := &browserProvider{repositories: map[int64]github.Repository{
		301: {ID: 301, Owner: "fixture-a", Name: "one", FullName: "fixture-a/one"},
		302: {ID: 302, Owner: "fixture-b", Name: "two", FullName: "fixture-b/two"}}, modes: map[int64]string{301: "allowed", 302: "allowed"}, tokens: map[string]int64{}}
	fixtures, err := seedBrowserFixtures(ctx, pool, store, provider, options)
	if err != nil {
		t.Fatal(err)
	}
	if options.EmptyCatalog {
		if err := assertEmptyBrowserCatalog(ctx, pool); err != nil {
			t.Fatal(err)
		}
	}
	authService := access.New(pool, store, provider)
	discoveryCtx, stopDiscovery := context.WithCancel(context.Background())
	discoveryDone := make(chan error, 1)
	go func() { discoveryDone <- runBrowserDiscovery(discoveryCtx, authService) }()
	projectService := projects.New(pool, authService)
	authService.SetProjectDestinationResolver(projectService.ResolveDestination)
	modelService := models.New(pool, authService, store, projects.NewCatalogWriter())
	authService.SetModelSaveDestinationResolver(modelService.ResolveDestination)
	state := &browserTestServer{pool: pool, auth: authService, store: store, provider: provider, fixtures: fixtures, emptyCatalog: options.EmptyCatalog, shutdown: make(chan struct{}), rootConfig: rootConfig, stopDiscovery: stopDiscovery, discoveryDone: discoveryDone}
	assetsFS := os.DirFS(assets)
	if _, err := os.Stat(filepath.Join(assets, "index.html")); err != nil {
		t.Fatal("browser assets missing index.html")
	}
	server := httptest.NewUnstartedServer(nil)
	state.server = server
	origin := "https://" + server.Listener.Addr().String()
	product := handlerConfigured(assetsFS, Auth{Service: authService, Origin: origin}, Projects{Service: projectService}, Models{Service: modelService}, Scan{}, Decision{}, Skills{}, Issues{}, Messages{}, AgentTeams{}, Pipeline{}, HumanControl{}, ObserveV1{}, Discovery{}, Console{})
	server.Config.Handler = state.wrapProduct(product)
	server.StartTLS()
	t.Cleanup(func() {
		if err := state.closeFixture(); err != nil {
			t.Error(err)
		}
	})
	return state
}

func (s *browserTestServer) closeFixture() error {
	s.closeOnce.Do(func() {
		s.releaseCommit()
		s.releaseResponse()
		s.releaseRequest()
		if s.server != nil {
			s.server.Close()
		}
		if s.stopDiscovery != nil {
			s.stopDiscovery()
		}
		if s.discoveryDone != nil {
			select {
			case s.closeErr = <-s.discoveryDone:
			case <-time.After(10 * time.Second):
				s.closeErr = errors.New("browser discovery did not stop")
			}
		}
	})
	return s.closeErr
}

func seedBrowserFixtures(ctx context.Context, pool *pgxpool.Pool, store *secrets.Store, provider *browserProvider, options browserFixtureOptions) (browserFixtureIDs, error) {
	result := browserFixtureIDs{ActorA: "10000000-0000-4000-8000-000000000001", ActorB: "10000000-0000-4000-8000-000000000002",
		RepositoryA: "repo_00000000000000000301", RepositoryB: "repo_00000000000000000302"}
	if !options.EmptyCatalog {
		result.ModelProfile = "model_fixture_a"
		result.ExecutionProfile = "execution_fixture_a"
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return browserFixtureIDs{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `INSERT INTO repomesh_access.accounts(id,github_id,display_name) VALUES($1,1001,'Fixture A'),($2,1002,'Fixture B')`, result.ActorA, result.ActorB); err != nil {
		return browserFixtureIDs{}, err
	}
	if !options.EmptyCatalog {
		for _, actor := range []string{result.ActorA, result.ActorB} {
			suffix := "a"
			if actor == result.ActorB {
				suffix = "b"
			}
			if _, err = tx.Exec(ctx, `INSERT INTO repomesh_projects.profiles(kind,id,owner,name,current_version) VALUES
			('model',$2,$1,'Fixture model','v1'),('execution',$3,$1,'Fixture execution','v1')`, actor, "model_fixture_"+suffix, "execution_fixture_"+suffix); err != nil {
				return browserFixtureIDs{}, err
			}
			if _, err = tx.Exec(ctx, `INSERT INTO repomesh_projects.profile_versions(kind,profile_id,version,parameters_complete,worker_concurrency,budget_policy_id,time_limit_policy_id,verification_group_enabled) VALUES
			('model',$1,'v1',true,NULL,'budget_fixture','time_fixture',NULL),('execution',$2,'v1',true,1,'budget_fixture','time_fixture',false)`, "model_fixture_"+suffix, "execution_fixture_"+suffix); err != nil {
				return browserFixtureIDs{}, err
			}
			if _, err = tx.Exec(ctx, `INSERT INTO repomesh_projects.defaults(actor,kind,profile_id,default_revision) VALUES
			($1,'model',$2,$4),($1,'execution',$3,$5)`, actor, "model_fixture_"+suffix, "execution_fixture_"+suffix, "20000000-0000-4000-8000-000000000001", "20000000-0000-4000-8000-000000000002"); err != nil {
				return browserFixtureIDs{}, err
			}
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return browserFixtureIDs{}, err
	}
	for actor, token := range map[string]string{result.ActorA: "browser-token-a", result.ActorB: "browser-token-b"} {
		ref, sealErr := store.Seal(ctx, secrets.Owner{Kind: "actor", ID: actor}, secrets.GitHubUserToken, []byte(token))
		if sealErr != nil {
			return browserFixtureIDs{}, sealErr
		}
		if _, err = pool.Exec(ctx, `INSERT INTO repomesh_access.connections(actor,revision,access_ref,status,observed_at)
			VALUES($1,$2,$3,'connected',now())`, actor, map[string]string{
			result.ActorA: "25000000-0000-4000-8000-000000000001",
			result.ActorB: "25000000-0000-4000-8000-000000000002",
		}[actor], string(ref)); err != nil {
			return browserFixtureIDs{}, err
		}
	}
	provider.mu.Lock()
	provider.tokens["browser-token-a"] = 1001
	provider.tokens["browser-token-b"] = 1002
	provider.mu.Unlock()
	return result, nil
}

func assertEmptyBrowserCatalog(ctx context.Context, pool *pgxpool.Pool) error {
	var profilesEmpty, defaultsEmpty, profileVersionsEmpty bool
	var projectsEmpty, configurationRevisionsEmpty bool
	var creationOperationsEmpty, updateOperationsEmpty, catalogSingleton bool
	err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) = 0 FROM repomesh_projects.profiles),
		(SELECT count(*) = 0 FROM repomesh_projects.defaults),
		(SELECT count(*) = 0 FROM repomesh_projects.profile_versions),
		(SELECT count(*) = 0 FROM repomesh_projects.projects),
		(SELECT count(*) = 0 FROM repomesh_projects.configuration_revisions),
		(SELECT count(*) = 0 FROM repomesh_projects.creation_operations),
		(SELECT count(*) = 0 FROM repomesh_projects.update_operations),
		(SELECT count(*) = 1 FROM repomesh_projects.catalog)`).Scan(
		&profilesEmpty,
		&defaultsEmpty,
		&profileVersionsEmpty,
		&projectsEmpty,
		&configurationRevisionsEmpty,
		&creationOperationsEmpty,
		&updateOperationsEmpty,
		&catalogSingleton,
	)
	if err != nil {
		return errors.New("cannot inspect empty browser catalog")
	}
	switch {
	case !profilesEmpty:
		return errors.New("empty browser catalog has profiles")
	case !defaultsEmpty:
		return errors.New("empty browser catalog has defaults")
	case !profileVersionsEmpty:
		return errors.New("empty browser catalog has profile versions")
	case !projectsEmpty:
		return errors.New("empty browser catalog has projects")
	case !configurationRevisionsEmpty:
		return errors.New("empty browser catalog has configuration revisions")
	case !creationOperationsEmpty:
		return errors.New("empty browser catalog has creation operations")
	case !updateOperationsEmpty:
		return errors.New("empty browser catalog has update operations")
	case !catalogSingleton:
		return errors.New("empty browser catalog does not have one catalog row")
	default:
		return nil
	}
}

func runBrowserDiscovery(ctx context.Context, service *access.Service) error {
	for {
		worked, err := service.RunOne(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if worked {
			continue
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

func (s *browserTestServer) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || r.URL.Query().Get("actor") != "a" && r.URL.Query().Get("actor") != "b" || len(r.URL.Query()) != 1 {
		http.Error(w, "invalid test login", 400)
		return
	}
	actor := r.URL.Query().Get("actor")
	destination, _ := access.ParseDestination([]byte(`{"kind":"home"}`))
	started, err := s.auth.Start(r.Context(), access.StartCommand{ID: map[string]string{"a": "30000000-0000-4000-8000-000000000001", "b": "30000000-0000-4000-8000-000000000002"}[actor], Purpose: "login", Destination: destination})
	if err != nil {
		http.Error(w, "test login unavailable", 503)
		return
	}
	parsed, _ := url.Parse(started.Result.AuthorizationURL)
	result, err := s.auth.CompleteCallback(r.Context(), access.Callback{BindingCookie: started.BindingCookie, State: parsed.Query().Get("state"), Code: actor})
	if err != nil || result.SessionCookie == "" {
		http.Error(w, "test login unavailable", 503)
		return
	}
	setCookie(w, bindingCookie, started.BindingCookie, 7*24*60*60)
	setCookie(w, sessionCookie, result.SessionCookie, 12*60*60)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *browserTestServer) control(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || len(r.Header.Values("Origin")) != 1 || r.Header.Get("Origin") != s.server.URL {
		http.Error(w, "test control rejected", 403)
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32<<10))
	var command browserControl
	if err != nil || jsoninput.Decode(data, &command, 32<<10) != nil {
		http.Error(w, "invalid test control", 400)
		return
	}
	accepted := true
	switch command.Action {
	case "repository":
		accepted = command.Method == "" && command.Path == "" && command.Actor == "" && command.Kind == "" && s.provider.setRepositoryMode(command.RepositoryID, command.Mode) == nil
	case "expand_project":
		projectID := strings.TrimPrefix(command.Path, "/api/projects/")
		accepted = command.Method == "" && projectID != command.Path && validBrowserProjectID(projectID) && command.RepositoryID == "" && command.Mode == "" && command.Actor == "" && command.Kind == "" && s.expandProject(r.Context(), projectID) == nil
	case "remove_project":
		projectID := strings.TrimPrefix(command.Path, "/api/projects/")
		accepted = command.Method == "" && projectID != command.Path && validBrowserProjectID(projectID) && command.RepositoryID == "" && command.Mode == "" && command.Actor == "" && command.Kind == "" && s.removeProject(r.Context(), projectID) == nil
	case "drop_response":
		accepted = (command.Method == http.MethodPost || command.Method == http.MethodPatch) && strings.HasPrefix(command.Path, "/api/") && command.RepositoryID == "" && command.Mode == "" && command.Actor == "" && command.Kind == ""
		if accepted {
			s.mu.Lock()
			s.dropMethod, s.dropPath = command.Method, command.Path
			s.mu.Unlock()
		}
	case "hold_response":
		projectSegment := strings.TrimPrefix(command.Path, "/api/projects/")
		safeProjectWrite := command.Method == http.MethodPatch && projectSegment != command.Path && projectSegment != "" && projectSegment != "." && projectSegment != ".." && utf8.ValidString(projectSegment) && utf8.RuneCountInString(projectSegment) <= 128 && !strings.ContainsAny(projectSegment, "/%\\?#\x00")
		accepted = (command.Method == http.MethodGet && strings.HasPrefix(command.Path, "/api/") || command.Method == http.MethodPost && command.Path == "/api/projects" || safeProjectWrite) && command.RepositoryID == "" && command.Mode == "" && command.Actor == "" && command.Kind == ""
		if accepted {
			s.mu.Lock()
			if s.responseRelease != nil || s.responseHeld {
				accepted = false
			} else {
				s.holdMethod, s.holdPath, s.responseRelease = command.Method, command.Path, make(chan struct{})
			}
			s.mu.Unlock()
		}
	case "hold_request":
		projectSegment := strings.TrimPrefix(command.Path, "/api/projects/")
		safeProjectWrite := command.Method == http.MethodPatch && projectSegment != command.Path && projectSegment != "" && projectSegment != "." && projectSegment != ".." && utf8.ValidString(projectSegment) && utf8.RuneCountInString(projectSegment) <= 128 && !strings.ContainsAny(projectSegment, "/%\\?#\x00")
		accepted = (command.Method == http.MethodPost && command.Path == "/api/projects" || safeProjectWrite) && command.RepositoryID == "" && command.Mode == "" && command.Actor == "" && command.Kind == ""
		if accepted {
			s.mu.Lock()
			if s.requestRelease != nil || s.requestHeld {
				accepted = false
			} else {
				s.nextRequestMethod, s.nextRequestPath, s.requestRelease = command.Method, command.Path, make(chan struct{})
			}
			s.mu.Unlock()
		}
	case "release_request":
		s.releaseRequest()
	case "release_response":
		s.releaseResponse()
	case "hold_commit":
		accepted = s.holdCommit(r.Context()) == nil
	case "release_commit":
		s.releaseCommit()
	case "advance_profile":
		accepted = s.advanceProfile(r.Context(), command.Actor, command.Kind) == nil
	case "revoke_session":
		accepted = s.revokeSessions(r.Context(), command.Actor) == nil
	case "shutdown":
		s.releaseCommit()
		s.releaseResponse()
		s.releaseRequest()
		select {
		case <-s.shutdown:
		default:
			close(s.shutdown)
		}
	default:
		accepted = false
	}
	if !accepted {
		http.Error(w, "test control rejected", 422)
		return
	}
	writeJSON(w, 200, browserControlResult{Accepted: true})
}

func validBrowserProjectID(value string) bool {
	return value != "" && value != "." && value != ".." && utf8.ValidString(value) && utf8.RuneCountInString(value) <= 128 && !strings.ContainsAny(value, "/%\\?#\x00")
}

func (s *browserTestServer) expandProject(ctx context.Context, projectID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var revision string
	if err = tx.QueryRow(ctx, `SELECT revision FROM repomesh_projects.projects WHERE id=$1 AND removed_at IS NULL FOR UPDATE`, projectID).Scan(&revision); err != nil {
		return err
	}
	repositories := make(map[int64]github.Repository, 50)
	for index := range 50 {
		externalID := int64(10000 + index)
		repositoryID := fmt.Sprintf("repo_%020d", externalID)
		repository := github.Repository{ID: externalID, Owner: "fixture-extra", Name: fmt.Sprintf("repo-%02d", index), FullName: fmt.Sprintf("fixture-extra/repo-%02d", index)}
		if _, err = tx.Exec(ctx, `INSERT INTO repomesh_projects.repositories(id,host,github_id,owner,name) VALUES($1,'github.com',$2,$3,$4) ON CONFLICT DO NOTHING`, repositoryID, externalID, repository.Owner, repository.Name); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO repomesh_projects.project_repositories(project_id,repository_id,joined_revision,joined_at) VALUES($1,$2,$3,clock_timestamp()) ON CONFLICT DO NOTHING`, projectID, repositoryID, revision); err != nil {
			return err
		}
		repositories[externalID] = repository
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	s.provider.mu.Lock()
	defer s.provider.mu.Unlock()
	for externalID, repository := range repositories {
		s.provider.repositories[externalID] = repository
		s.provider.modes[externalID] = "allowed"
	}
	return nil
}

func (s *browserTestServer) removeProject(ctx context.Context, projectID string) error {
	result, err := s.pool.Exec(ctx, `UPDATE repomesh_projects.projects SET removed_at=clock_timestamp() WHERE id=$1 AND removed_at IS NULL`, projectID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return errors.New("project removal target missing")
	}
	return nil
}

func (s *browserTestServer) status(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	var barrier bool
	_ = s.pool.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event='advisory' AND query LIKE '%repomesh_projects.%operations%')`).Scan(&barrier)
	s.mu.Lock()
	s.barrierHeld = barrier
	result := browserControlResult{Accepted: true, BarrierHeld: s.barrierHeld, ResponseHeld: s.responseHeld, RequestHeld: s.requestHeld}
	s.mu.Unlock()
	writeJSON(w, 200, result)
}

func (s *browserTestServer) wrapProduct(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/__test/login":
			s.login(w, r)
			return
		case "/__test/control":
			s.control(w, r)
			return
		case "/__test/status":
			s.status(w, r)
			return
		}
		if err := s.awaitRequest(r.Context(), r.Method, r.URL.RequestURI()); err != nil {
			return
		}
		s.mu.Lock()
		drop := s.dropMethod == r.Method && s.dropPath == r.URL.RequestURI()
		if drop {
			s.dropMethod, s.dropPath = "", ""
		}
		hold := s.holdMethod == r.Method && s.holdPath == r.URL.RequestURI() && s.responseRelease != nil
		release := s.responseRelease
		s.mu.Unlock()
		if !drop && !hold {
			next.ServeHTTP(w, r)
			return
		}
		recorder := httptest.NewRecorder()
		next.ServeHTTP(recorder, r)
		if drop && recorder.Code >= 200 && recorder.Code < 300 {
			panic(http.ErrAbortHandler)
		}
		if hold {
			s.mu.Lock()
			s.responseHeld = true
			s.mu.Unlock()
			select {
			case <-release:
			case <-r.Context().Done():
			}
			s.mu.Lock()
			s.responseHeld = false
			s.holdMethod, s.holdPath, s.responseRelease = "", "", nil
			s.mu.Unlock()
		}
		for key, values := range recorder.Header() {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(recorder.Code)
		_, _ = io.Copy(w, recorder.Body)
	})
}

func (s *browserTestServer) awaitRequest(ctx context.Context, method, path string) error {
	s.mu.Lock()
	if s.nextRequestMethod != method || s.nextRequestPath != path || s.requestRelease == nil {
		s.mu.Unlock()
		return nil
	}
	release := s.requestRelease
	s.nextRequestMethod, s.nextRequestPath = "", ""
	s.requestHeld = true
	s.mu.Unlock()

	var err error
	select {
	case <-release:
	case <-ctx.Done():
		err = ctx.Err()
	}
	s.mu.Lock()
	if s.requestRelease == release {
		s.requestRelease = nil
	}
	s.requestHeld = false
	s.mu.Unlock()
	return err
}

func (s *browserTestServer) releaseRequest() {
	s.mu.Lock()
	if s.requestRelease != nil {
		release := s.requestRelease
		s.requestRelease = nil
		close(release)
	}
	s.nextRequestMethod, s.nextRequestPath = "", ""
	s.requestHeld = false
	s.mu.Unlock()
}

func (s *browserTestServer) holdCommit(ctx context.Context) error {
	s.mu.Lock()
	if s.barrierRelease != nil {
		s.mu.Unlock()
		return errors.New("commit barrier already active")
	}
	release := make(chan struct{})
	s.barrierRelease = release
	s.mu.Unlock()
	if _, err := s.pool.Exec(ctx, `CREATE OR REPLACE FUNCTION repomesh_projects.browser_hold_commit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(4770303); RETURN NEW; END $$;
		DROP TRIGGER IF EXISTS browser_hold_creation ON repomesh_projects.creation_operations;
		DROP TRIGGER IF EXISTS browser_hold_update ON repomesh_projects.update_operations;
		CREATE TRIGGER browser_hold_creation BEFORE UPDATE ON repomesh_projects.creation_operations FOR EACH ROW EXECUTE FUNCTION repomesh_projects.browser_hold_commit();
		CREATE TRIGGER browser_hold_update BEFORE UPDATE ON repomesh_projects.update_operations FOR EACH ROW EXECUTE FUNCTION repomesh_projects.browser_hold_commit()`); err != nil {
		s.releaseCommit()
		return err
	}
	ready := make(chan error, 1)
	go func() {
		connection, err := s.pool.Acquire(context.Background())
		if err != nil {
			ready <- err
			return
		}
		defer connection.Release()
		_, err = connection.Exec(context.Background(), `SELECT pg_advisory_lock(4770303)`)
		ready <- err
		if err != nil {
			return
		}
		<-release
		_, _ = connection.Exec(context.Background(), `SELECT pg_advisory_unlock(4770303)`)
	}()
	select {
	case err := <-ready:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *browserTestServer) releaseCommit() {
	s.mu.Lock()
	released := false
	if s.barrierRelease != nil {
		close(s.barrierRelease)
		s.barrierRelease = nil
		released = true
	}
	s.barrierHeld = false
	s.mu.Unlock()
	if released {
		_, _ = s.pool.Exec(context.Background(), `SELECT pg_advisory_lock(4770303); SELECT pg_advisory_unlock(4770303)`)
	}
}

func (s *browserTestServer) releaseResponse() {
	s.mu.Lock()
	if s.responseRelease != nil {
		release := s.responseRelease
		s.responseRelease = nil
		close(release)
	}
	s.mu.Unlock()
}

func (s *browserTestServer) advanceProfile(ctx context.Context, actor, kind string) error {
	actorID := map[string]string{"a": s.fixtures.ActorA, "b": s.fixtures.ActorB}[actor]
	if actorID == "" || kind != "model" && kind != "execution" {
		return errors.New("invalid profile target")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT singleton FROM repomesh_projects.catalog WHERE singleton FOR UPDATE`); err != nil {
		return err
	}
	var profileID, current string
	if err = tx.QueryRow(ctx, `SELECT id,current_version FROM repomesh_projects.profiles WHERE owner=$1 AND kind=$2 FOR UPDATE`, actorID, kind).Scan(&profileID, &current); err != nil {
		return err
	}
	version := "v" + strconv.FormatInt(time.Now().UnixNano(), 10)
	var secretID, ownerKind, ownerID, purpose *string
	if kind == "model" {
		if err = tx.QueryRow(ctx, `SELECT c.access_ref,v.owner_kind,v.owner_id,v.purpose FROM repomesh_access.connections c JOIN repomesh_secrets.versions v ON v.version_id=c.access_ref WHERE c.actor=$1`, actorID).Scan(&secretID, &ownerKind, &ownerID, &purpose); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO repomesh_projects.profile_versions(kind,profile_id,version,secret_version_id,secret_owner_kind,secret_owner_id,secret_purpose,parameters_complete,worker_concurrency,budget_policy_id,time_limit_policy_id,verification_group_enabled)
		VALUES($1,$2,$3,$4,$5,$6,$7,true,CASE WHEN $1='execution' THEN 1 ELSE NULL END,'budget_fixture','time_fixture',CASE WHEN $1='execution' THEN false ELSE NULL END)`, kind, profileID, version, secretID, ownerKind, ownerID, purpose); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE repomesh_projects.profiles SET current_version=$3 WHERE kind=$1 AND id=$2`, kind, profileID, version); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE repomesh_projects.defaults SET default_revision=$3 WHERE actor=$1 AND kind=$2`, actorID, kind, map[string]string{"model": "50000000-0000-4000-8000-000000000001", "execution": "50000000-0000-4000-8000-000000000002"}[kind]); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *browserTestServer) revokeSessions(ctx context.Context, actor string) error {
	actorID := map[string]string{"a": s.fixtures.ActorA, "b": s.fixtures.ActorB}[actor]
	if actorID == "" {
		return errors.New("invalid actor")
	}
	_, err := s.pool.Exec(ctx, `UPDATE repomesh_access.sessions SET revoked=true WHERE actor=$1`, actorID)
	return err
}

func (s *browserTestServer) manifest() browserTestManifest {
	return browserTestManifest{Origin: s.server.URL, LoginPath: "/__test/login", ControlPath: "/__test/control", StatusPath: "/__test/status", EmptyCatalog: s.emptyCatalog, Fixtures: s.fixtures}
}

func writeBrowserManifest(directory string, manifest browserTestManifest) error {
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("browser manifest directory must be an existing 0700 directory")
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(directory, "manifest.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err = file.Write(append(data, '\n')); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func (p *browserProvider) AuthorizationURL(state, verifier string) string {
	return "https://github.invalid/authorize?state=" + url.QueryEscape(state) + "&code_challenge=" + url.QueryEscape(verifier)
}
func (p *browserProvider) Exchange(_ context.Context, code, _ string) (github.TokenSet, error) {
	id := map[string]int64{"a": 1001, "b": 1002}[code]
	if id == 0 {
		return github.TokenSet{}, &github.Error{Kind: "rejected"}
	}
	token := "browser-token-" + code
	p.mu.Lock()
	p.tokens[token] = id
	p.mu.Unlock()
	return github.TokenSet{AccessToken: token}, nil
}
func (p *browserProvider) Refresh(context.Context, string) (github.TokenSet, error) {
	return github.TokenSet{}, &github.Error{Kind: "unavailable"}
}
func (p *browserProvider) Identity(_ context.Context, token string) (github.Identity, error) {
	p.mu.Lock()
	id := p.tokens[token]
	p.mu.Unlock()
	if id == 0 {
		return github.Identity{}, &github.Error{Kind: "unauthorized"}
	}
	return github.Identity{ID: id, DisplayName: map[int64]string{1001: "Fixture A", 1002: "Fixture B"}[id]}, nil
}
func (p *browserProvider) Repositories(_ context.Context, token string, page int) (github.RepositoryPage, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.tokens[token] == 0 || page != 1 {
		return github.RepositoryPage{}, &github.Error{Kind: "unauthorized"}
	}
	items := []github.Repository{}
	for _, item := range p.repositories {
		items = append(items, item)
	}
	return github.RepositoryPage{Items: items}, nil
}
func (p *browserProvider) Repository(_ context.Context, token, owner, name string) (github.Repository, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.tokens[token] == 0 {
		return github.Repository{}, &github.Error{Kind: "unauthorized"}
	}
	for id, repository := range p.repositories {
		if strings.EqualFold(repository.Owner, owner) && strings.EqualFold(repository.Name, name) {
			switch p.modes[id] {
			case "allowed":
				return repository, nil
			case "denied":
				return github.Repository{}, &github.Error{Kind: "denied"}
			default:
				return github.Repository{}, &github.Error{Kind: "unavailable"}
			}
		}
	}
	return github.Repository{}, &github.Error{Kind: "denied"}
}
func (p *browserProvider) AppCapability(_ context.Context, owner, name string) (github.Capability, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, repository := range p.repositories {
		if strings.EqualFold(repository.Owner, owner) && strings.EqualFold(repository.Name, name) {
			if p.modes[id] != "allowed" {
				return github.Capability{}, &github.Error{Kind: p.modes[id]}
			}
			observed := time.Now().UTC()
			return github.Capability{Status: "allowed", ReasonCodes: []string{}, ObservedAt: &observed}, nil
		}
	}
	return github.Capability{}, &github.Error{Kind: "denied"}
}

func (p *browserProvider) ObserveAppInstallation(ctx context.Context, owner, name string) (github.AppInstallationObservation, error) {
	capability, err := p.AppCapability(ctx, owner, name)
	if err != nil {
		return github.AppInstallationObservation{}, err
	}
	observedAt := time.Now().UTC()
	if capability.ObservedAt != nil {
		observedAt = *capability.ObservedAt
	}
	permissions := map[string]string{"contents": "write", "pull_requests": "write", "metadata": "read"}
	return github.ObservedInstallation(1, 1001, false, permissions, capability, observedAt), nil
}
func (p *browserProvider) setRepositoryMode(id string, mode string) error {
	if mode != "allowed" && mode != "denied" && mode != "unknown" {
		return errors.New("invalid mode")
	}
	external, err := strconv.ParseInt(strings.TrimPrefix(id, "repo_"), 10, 64)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.repositories[external]; !ok {
		return errors.New("unknown repository")
	}
	p.modes[external] = mode
	return nil
}

func TestPostgresConfigurationProfileSecretAvailability(t *testing.T) {
	assets := t.TempDir()
	if err := os.WriteFile(filepath.Join(assets, "index.html"), []byte("<!doctype html><title>B03 profiles</title>"), 0600); err != nil {
		t.Fatal(err)
	}
	server := startProjectBrowserServer(t, assets)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var secretID, ownerKind, ownerID, purpose string
	if err := server.pool.QueryRow(ctx, `SELECT c.access_ref,v.owner_kind,v.owner_id,v.purpose
		FROM repomesh_access.connections c JOIN repomesh_secrets.versions v ON v.version_id=c.access_ref
		WHERE c.actor=$1`, server.fixtures.ActorA).Scan(&secretID, &ownerKind, &ownerID, &purpose); err != nil {
		t.Fatal(err)
	}
	tx, err := server.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `INSERT INTO repomesh_projects.profile_versions(kind,profile_id,version,secret_version_id,secret_owner_kind,secret_owner_id,secret_purpose,parameters_complete,budget_policy_id,time_limit_policy_id)
		VALUES('model',$1,'v2',$2,$3,$4,$5,true,'budget_fixture','time_fixture')`, server.fixtures.ModelProfile, secretID, ownerKind, ownerID, purpose); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE repomesh_projects.profiles SET current_version='v2' WHERE kind='model' AND id=$1`, server.fixtures.ModelProfile); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO repomesh_projects.profiles(kind,id,owner,name,current_version)
		VALUES('model','model_fixture_a2',$1,'Fixture model second','v1')`, server.fixtures.ActorA); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO repomesh_projects.profile_versions(kind,profile_id,version,secret_version_id,secret_owner_kind,secret_owner_id,secret_purpose,parameters_complete,budget_policy_id,time_limit_policy_id)
		VALUES('model','model_fixture_a2','v1',$1,$2,$3,$4,true,'budget_fixture','time_fixture')`, secretID, ownerKind, ownerID, purpose); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := server.server.Client()
	client.Jar = jar
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	get := func(path string) (int, []byte) {
		t.Helper()
		response, requestErr := client.Get(server.server.URL + path)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		defer response.Body.Close()
		data, readErr := io.ReadAll(response.Body)
		if readErr != nil {
			t.Fatal(readErr)
		}
		return response.StatusCode, data
	}
	status, _ := get("/__test/login?actor=a")
	if status != http.StatusSeeOther {
		t.Fatalf("test login status=%d", status)
	}
	readPage := func(path string) projects.ProfilePage {
		t.Helper()
		status, data := get(path)
		var page projects.ProfilePage
		if status != http.StatusOK || json.Unmarshal(data, &page) != nil {
			t.Fatalf("profile response status=%d body=%s", status, data)
		}
		if strings.Contains(string(data), "model_fixture_b") || strings.Contains(string(data), "canCreateIssue") {
			t.Fatalf("profile response disclosed another owner or issue readiness: %s", data)
		}
		return page
	}
	first := readPage("/api/configuration-profiles?kind=model&limit=1")
	if len(first.Items) != 1 || first.Items[0].ID != server.fixtures.ModelProfile || first.Items[0].Availability.Status != "allowed" || first.NextCursor == nil || first.DefaultProfileID == nil || *first.DefaultProfileID != server.fixtures.ModelProfile {
		t.Fatalf("first profile page=%+v", first)
	}
	second := readPage("/api/configuration-profiles?kind=model&limit=1&cursor=" + url.QueryEscape(*first.NextCursor))
	if len(second.Items) != 1 || second.Items[0].ID != "model_fixture_a2" || second.Items[0].Availability.Status != "allowed" || second.NextCursor != nil || second.DefaultProfileID == nil || *second.DefaultProfileID != server.fixtures.ModelProfile {
		t.Fatalf("second profile page=%+v", second)
	}
	if err = server.store.SetEnabled(ctx, secrets.VersionID(secretID), false); err != nil {
		t.Fatal(err)
	}
	disabled := readPage("/api/configuration-profiles?kind=model&limit=1")
	if len(disabled.Items) != 1 || disabled.Items[0].Availability.Status != "denied" || !slices.Contains(disabled.Items[0].Availability.ReasonCodes, "MODEL_SECRET_UNAVAILABLE") {
		t.Fatalf("disabled secret profile page=%+v", disabled)
	}
	if err = server.store.SetEnabled(ctx, secrets.VersionID(secretID), true); err != nil {
		t.Fatal(err)
	}
	if err = server.store.Destroy(ctx, secrets.VersionID(secretID)); err != nil {
		t.Fatal(err)
	}
	destroyed := readPage("/api/configuration-profiles?kind=model&limit=1")
	if len(destroyed.Items) != 1 || destroyed.Items[0].Availability.Status != "denied" || !slices.Contains(destroyed.Items[0].Availability.ReasonCodes, "MODEL_SECRET_UNAVAILABLE") {
		t.Fatalf("destroyed secret profile page=%+v", destroyed)
	}
}

func TestPostgresProjectHTTPContract(t *testing.T) {
	assets := t.TempDir()
	if err := os.WriteFile(filepath.Join(assets, "index.html"), []byte("<!doctype html><title>B03</title>"), 0600); err != nil {
		t.Fatal(err)
	}
	server := startProjectBrowserServer(t, assets)
	jar, _ := cookiejar.New(nil)
	client := server.server.Client()
	client.Jar = jar
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	request := func(method, path, body, origin, csrf, key string) (int, []byte, http.Header) {
		t.Helper()
		req, err := http.NewRequest(method, server.server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, data, response.Header
	}
	status, _, _ := request("GET", "/__test/login?actor=a", "", "", "", "")
	if status != http.StatusSeeOther {
		t.Fatalf("test login status=%d", status)
	}
	status, data, _ := request("GET", "/api/session", "", "", "", "")
	var session access.Session
	if status != 200 || json.Unmarshal(data, &session) != nil || session.User.ID != server.fixtures.ActorA {
		t.Fatalf("session response invalid: status=%d", status)
	}
	origin, csrf := server.server.URL, session.CSRFToken
	discoveryDeadline := time.Now().Add(5 * time.Second)
	for {
		status, data, _ = request("GET", "/api/repositories/candidates?limit=50", "", "", "", "")
		if status == 200 && strings.Contains(string(data), server.fixtures.RepositoryA) && strings.Contains(string(data), server.fixtures.RepositoryB) {
			break
		}
		if status != 503 || time.Now().After(discoveryDeadline) {
			t.Fatalf("repository discovery status=%d body=%s", status, data)
		}
		time.Sleep(20 * time.Millisecond)
	}
	createKey := "60000000-0000-4000-8000-000000000001"
	payload := `{"name":" Project α ","purpose":"Purpose","repositoryIds":["` + server.fixtures.RepositoryA + `"]}`
	statuses := make(chan int, 20)
	bodies := make(chan []byte, 20)
	var workers sync.WaitGroup
	for range 20 {
		workers.Go(func() {
			code, body, _ := request("POST", "/api/projects", payload, origin, csrf, createKey)
			statuses <- code
			bodies <- body
		})
	}
	workers.Wait()
	close(statuses)
	close(bodies)
	created := 0
	statusCounts := map[int]int{}
	for code := range statuses {
		statusCounts[code]++
		if code == 201 {
			created++
		} else if code != 200 && code != 503 {
			t.Fatalf("concurrent create status=%d", code)
		}
	}
	if created != 1 {
		t.Fatalf("first create responses=%d statuses=%v", created, statusCounts)
	}
	var receipt projects.CreationReceipt
	for body := range bodies {
		var candidate projects.CreationReceipt
		if json.Unmarshal(body, &candidate) == nil && candidate.ProjectID != "" {
			receipt = candidate
		}
	}
	if receipt.ProjectID == "" {
		t.Fatal("concurrent create returned no receipt")
	}
	status, data, header := request("GET", "/api/project-creations/"+createKey, "", "", "", "")
	var recovered projects.CreationReceipt
	if status != 200 || header.Get("Cache-Control") != "no-store" || json.Unmarshal(data, &recovered) != nil || recovered != receipt {
		t.Fatalf("creation recovery status=%d body=%s", status, data)
	}
	status, data, _ = request("POST", "/api/projects", payload, origin, csrf, strings.ToUpper(createKey))
	if status != 200 || json.Unmarshal(data, &recovered) != nil || recovered.ProjectCreationID != createKey {
		t.Fatalf("uppercase creation replay status=%d body=%s", status, data)
	}
	status, data, _ = request("GET", "/api/project-creations/"+strings.ToUpper(createKey), "", "", "", "")
	if status != 200 || json.Unmarshal(data, &recovered) != nil || recovered.ProjectCreationID != createKey {
		t.Fatalf("uppercase creation lookup status=%d body=%s", status, data)
	}
	status, data, _ = request("GET", "/api/projects/"+receipt.ProjectID, "", "", "", "")
	var project projects.ProjectView
	if status != 200 || json.Unmarshal(data, &project) != nil || project.Name != " Project α " || project.Actions.CanCreateIssue {
		t.Fatalf("project status=%d body=%s", status, data)
	}
	if project.Configuration.Effective.ModelProfileID == nil || *project.Configuration.Effective.ModelProfileID != server.fixtures.ModelProfile {
		t.Fatalf("inherit did not fix default: %+v", project.Configuration)
	}
	initialConfiguration := project.Configuration.Effective.ConfigurationRevision

	conflictCreateKey := "61000000-0000-1000-0000-000000000001"
	conflictCreateBodies := []string{
		`{"name":"winner-a","purpose":"A","repositoryIds":["` + server.fixtures.RepositoryA + `"]}`,
		`{"name":"winner-b","purpose":"B","repositoryIds":["` + server.fixtures.RepositoryA + `"]}`,
	}
	statuses = make(chan int, 20)
	for index := range 20 {
		body := conflictCreateBodies[index%2]
		workers.Go(func() {
			code, _, _ := request("POST", "/api/projects", body, origin, csrf, conflictCreateKey)
			statuses <- code
		})
	}
	workers.Wait()
	close(statuses)
	created, conflicts := 0, 0
	for code := range statuses {
		switch code {
		case 201:
			created++
		case 409:
			conflicts++
		case 200, 503:
		default:
			t.Fatalf("different-input create status=%d", code)
		}
	}
	if created != 1 || conflicts == 0 {
		t.Fatalf("different-input create created=%d conflicts=%d", created, conflicts)
	}
	status, data, _ = request("GET", "/api/project-creations/"+conflictCreateKey, "", "", "", "")
	var conflictCreation projects.CreationReceipt
	if status != 200 || json.Unmarshal(data, &conflictCreation) != nil {
		t.Fatalf("different-input create recovery status=%d body=%s", status, data)
	}
	status, data, _ = request("GET", "/api/projects/"+conflictCreation.ProjectID, "", "", "", "")
	var conflictProject projects.ProjectView
	if status != 200 || json.Unmarshal(data, &conflictProject) != nil || conflictProject.Name != "winner-a" && conflictProject.Name != "winner-b" {
		t.Fatalf("different-input winner project status=%d body=%s", status, data)
	}
	conflictUpdateKey := "62000000-0000-1000-0000-000000000001"
	conflictUpdateBodies := []string{
		`{"expectedProjectRevision":"` + conflictProject.ProjectRevision + `","name":"update-a"}`,
		`{"expectedProjectRevision":"` + conflictProject.ProjectRevision + `","name":"update-b"}`,
	}
	statuses = make(chan int, 20)
	for index := range 20 {
		body := conflictUpdateBodies[index%2]
		workers.Go(func() {
			code, _, _ := request("PATCH", "/api/projects/"+conflictCreation.ProjectID, body, origin, csrf, conflictUpdateKey)
			statuses <- code
		})
	}
	workers.Wait()
	close(statuses)
	conflicts, updateSuccesses := 0, 0
	for code := range statuses {
		switch code {
		case 200:
			updateSuccesses++
		case 409:
			conflicts++
		case 503:
		default:
			t.Fatalf("different-input update status=%d", code)
		}
	}
	if updateSuccesses == 0 || conflicts == 0 {
		t.Fatalf("different-input update successes=%d conflicts=%d", updateSuccesses, conflicts)
	}
	status, data, _ = request("GET", "/api/projects/"+conflictCreation.ProjectID+"/updates/"+conflictUpdateKey, "", "", "", "")
	if status != 200 {
		t.Fatalf("different-input update recovery status=%d body=%s", status, data)
	}

	updateKey := "60000000-0000-4000-8000-000000000002"
	updateBody := `{"expectedProjectRevision":"` + project.ProjectRevision + `","name":"Renamed"}`
	statuses = make(chan int, 20)
	for range 20 {
		workers.Go(func() {
			code, _, _ := request("PATCH", "/api/projects/"+receipt.ProjectID, updateBody, origin, csrf, updateKey)
			statuses <- code
		})
	}
	workers.Wait()
	close(statuses)
	for code := range statuses {
		if code != 200 && code != 503 {
			t.Fatalf("concurrent update status=%d", code)
		}
	}
	status, data, _ = request("GET", "/api/projects/"+receipt.ProjectID+"/updates/"+updateKey, "", "", "", "")
	var update projects.UpdateReceipt
	if status != 200 || json.Unmarshal(data, &update) != nil {
		t.Fatalf("update recovery status=%d body=%s", status, data)
	}
	status, data, _ = request("PATCH", "/api/projects/"+receipt.ProjectID, updateBody, origin, csrf, updateKey)
	if status != 200 {
		t.Fatalf("old successful key lost priority: %d %s", status, data)
	}
	status, data, _ = request("PATCH", "/api/projects/"+receipt.ProjectID, `{"expectedProjectRevision":"`+project.ProjectRevision+`","purpose":"stale"}`, origin, csrf, "60000000-0000-4000-8000-000000000003")
	if status != 409 || !strings.Contains(string(data), "PROJECT_REVISION_CONFLICT") {
		t.Fatalf("stale update status=%d body=%s", status, data)
	}

	status, data, _ = request("GET", "/api/projects/"+receipt.ProjectID, "", "", "", "")
	if status != 200 || json.Unmarshal(data, &project) != nil {
		t.Fatal("updated project unavailable")
	}
	noChangeRevision := project.ProjectRevision
	status, data, _ = request("PATCH", "/api/projects/"+receipt.ProjectID, `{"expectedProjectRevision":"`+project.ProjectRevision+`","name":"Renamed"}`, origin, csrf, "60000000-0000-4000-8000-000000000004")
	var noChange projects.UpdateReceipt
	if status != 200 || json.Unmarshal(data, &noChange) != nil || noChange.ProjectRevision != noChangeRevision {
		t.Fatalf("no-change update status=%d body=%s", status, data)
	}

	if err := server.provider.setRepositoryMode(server.fixtures.RepositoryB, "denied"); err != nil {
		t.Fatal(err)
	}
	status, data, _ = request("PATCH", "/api/projects/"+receipt.ProjectID, `{"expectedProjectRevision":"`+noChangeRevision+`","repositoryIdsToAdd":["`+server.fixtures.RepositoryB+`"],"purpose":"must rollback"}`, origin, csrf, "60000000-0000-4000-8000-000000000005")
	if status != 404 {
		t.Fatalf("denied add status=%d body=%s", status, data)
	}
	status, data, _ = request("GET", "/api/projects/"+receipt.ProjectID, "", "", "", "")
	if json.Unmarshal(data, &project) != nil || project.ProjectRevision != noChangeRevision || project.Purpose == "must rollback" {
		t.Fatal("failed add changed project")
	}
	if err := server.provider.setRepositoryMode(server.fixtures.RepositoryB, "unknown"); err != nil {
		t.Fatal(err)
	}
	status, data, _ = request("PATCH", "/api/projects/"+receipt.ProjectID, `{"expectedProjectRevision":"`+noChangeRevision+`","repositoryIdsToAdd":["`+server.fixtures.RepositoryB+`"],"purpose":"unknown must rollback"}`, origin, csrf, "60000000-0000-4000-8000-000000000009")
	if status != 503 || !strings.Contains(string(data), "AUTHORIZATION_UNCONFIRMED") {
		t.Fatalf("unknown add status=%d body=%s", status, data)
	}
	status, data, _ = request("GET", "/api/projects/"+receipt.ProjectID, "", "", "", "")
	if json.Unmarshal(data, &project) != nil || project.ProjectRevision != noChangeRevision || project.Purpose == "unknown must rollback" {
		t.Fatal("unconfirmed add changed project")
	}
	if err := server.provider.setRepositoryMode(server.fixtures.RepositoryA, "denied"); err != nil {
		t.Fatal(err)
	}
	status, data, _ = request("PATCH", "/api/projects/"+receipt.ProjectID, `{"expectedProjectRevision":"`+project.ProjectRevision+`","purpose":"metadata repair"}`, origin, csrf, "60000000-0000-4000-8000-000000000006")
	if status != 200 {
		t.Fatalf("restricted original blocked metadata repair: %d %s", status, data)
	}
	status, data, _ = request("GET", "/api/projects/"+receipt.ProjectID+"/repositories", "", "", "", "")
	var repositoryPage projects.ProjectRepositoryPage
	if status != 200 || json.Unmarshal(data, &repositoryPage) != nil || len(repositoryPage.Items) != 0 || repositoryPage.RestrictedRepositoryCount != 1 || strings.Contains(string(data), "fixture-a") {
		t.Fatalf("restricted repository leaked: %d %s", status, data)
	}

	if err := server.advanceProfile(context.Background(), "a", "model"); err != nil {
		t.Fatal(err)
	}
	status, data, _ = request("GET", "/api/projects/"+receipt.ProjectID, "", "", "", "")
	if json.Unmarshal(data, &project) != nil || project.Configuration.Effective.ConfigurationRevision != initialConfiguration {
		t.Fatal("default change altered fixed configuration")
	}
	status, data, _ = request("PATCH", "/api/projects/"+receipt.ProjectID, `{"expectedProjectRevision":"`+project.ProjectRevision+`","configuration":{"modelProfile":{"mode":"inherit"},"executionProfile":{"mode":"inherit"}}}`, origin, csrf, "60000000-0000-4000-8000-000000000007")
	if status != 200 {
		t.Fatalf("configuration repair status=%d body=%s", status, data)
	}
	status, data, _ = request("GET", "/api/projects/"+receipt.ProjectID, "", "", "", "")
	if json.Unmarshal(data, &project) != nil || project.Configuration.Effective.ConfigurationRevision == initialConfiguration {
		t.Fatal("explicit configuration did not bind new version")
	}
	var fixedModelVersion string
	if err := server.pool.QueryRow(context.Background(), `SELECT fixed->'model'->>'version' FROM repomesh_projects.configuration_revisions WHERE project_id=$1 AND revision=$2`, receipt.ProjectID, project.Configuration.Effective.ConfigurationRevision).Scan(&fixedModelVersion); err != nil || fixedModelVersion == "v1" {
		t.Fatal("new model version not fixed", err)
	}
	if err := server.advanceProfile(context.Background(), "a", "model"); err != nil {
		t.Fatal(err)
	}
	status, data, _ = request("PATCH", "/api/projects/"+receipt.ProjectID, `{"expectedProjectRevision":"`+project.ProjectRevision+`","purpose":"no implicit rebind"}`, origin, csrf, "60000000-0000-4000-8000-000000000008")
	if status != 200 {
		t.Fatalf("metadata after default advance status=%d body=%s", status, data)
	}
	var stillFixed string
	if err := server.pool.QueryRow(context.Background(), `SELECT fixed->'model'->>'version' FROM repomesh_projects.configuration_revisions WHERE project_id=$1 AND revision=(SELECT current_configuration_revision FROM repomesh_projects.projects WHERE id=$1)`, receipt.ProjectID).Scan(&stillFixed); err != nil || stillFixed != fixedModelVersion {
		t.Fatal("metadata update switched fixed version", err)
	}
	var fixedSecretID string
	if err := server.pool.QueryRow(context.Background(), `SELECT v.secret_version_id FROM repomesh_projects.configuration_revisions c
		JOIN repomesh_projects.profile_versions v ON v.kind='model' AND v.profile_id=c.model_profile_id AND v.version=c.model_profile_version
		WHERE c.project_id=$1 AND c.revision=(SELECT current_configuration_revision FROM repomesh_projects.projects WHERE id=$1)`, receipt.ProjectID).Scan(&fixedSecretID); err != nil || fixedSecretID == "" {
		t.Fatal("fixed model secret reference missing", err)
	}
	if err := server.store.SetEnabled(context.Background(), secrets.VersionID(fixedSecretID), false); err != nil {
		t.Fatal(err)
	}
	status, data, _ = request("GET", "/api/projects/"+receipt.ProjectID, "", "", "", "")
	if status != 200 || json.Unmarshal(data, &project) != nil || !slices.Contains(project.Configuration.Checks.ReasonCodes, "MODEL_SECRET_UNAVAILABLE") {
		t.Fatalf("disabled fixed secret status=%d body=%s", status, data)
	}
	if err := server.store.SetEnabled(context.Background(), secrets.VersionID(fixedSecretID), true); err != nil {
		t.Fatal(err)
	}

	if _, err := server.pool.Exec(context.Background(), `UPDATE repomesh_projects.creation_operations SET canonical_input=NULL,exact_input=NULL,removed_at=now() WHERE actor=$1 AND key=$2`, server.fixtures.ActorA, createKey); err != nil {
		t.Fatal(err)
	}
	status, data, _ = request("POST", "/api/projects", `{}`, origin, csrf, createKey)
	if status != 410 || !strings.Contains(string(data), "PROJECT_CREATION_RESULT_REMOVED") {
		t.Fatalf("removed result priority status=%d body=%s", status, data)
	}

	for _, invalid := range []struct {
		body, key string
		status    int
		code      string
	}{
		{`{"name":"a","name":"b"}`, "60000000-0000-4000-8000-000000000011", 400, "INVALID_JSON"},
		{`[]`, "60000000-0000-4000-8000-000000000012", 422, "VALIDATION_FAILED"},
		{`{"unknown":true}`, "60000000-0000-4000-8000-000000000013", 422, "VALIDATION_FAILED"},
		{`{}`, "", 400, "INVALID_IDEMPOTENCY_KEY"},
	} {
		code, body, _ := request("POST", "/api/projects", invalid.body, origin, csrf, invalid.key)
		if code != invalid.status || !strings.Contains(string(body), invalid.code) {
			t.Fatalf("invalid request status=%d body=%s", code, body)
		}
	}
	status, data, _ = request("POST", "/api/projects", payload, "https://wrong.invalid", csrf, "60000000-0000-4000-8000-000000000014")
	if status != 403 || strings.Contains(string(data), "Project α") {
		t.Fatalf("cross-origin status=%d body=%s", status, data)
	}
	status, data, _ = request("POST", "/api/projects", strings.Repeat(" ", 256*1024+1), origin, csrf, "60000000-0000-4000-8000-000000000015")
	if status != 413 || !strings.Contains(string(data), "REQUEST_TOO_LARGE") {
		t.Fatalf("large request status=%d body=%s", status, data)
	}

	if err := server.provider.setRepositoryMode(server.fixtures.RepositoryA, "allowed"); err != nil {
		t.Fatal(err)
	}
	requestBarrierKey := "60000000-0000-4000-8000-000000000016"
	status, data, _ = request("POST", "/__test/control", `{"action":"hold_request","method":"POST","path":"/api/projects"}`, origin, "", "")
	if status != 200 {
		t.Fatalf("hold request control status=%d body=%s", status, data)
	}
	requestBarrierDone := make(chan int, 1)
	go func() {
		code, _, _ := request("POST", "/api/projects", payload, origin, csrf, requestBarrierKey)
		requestBarrierDone <- code
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		status, data, _ = request("GET", "/__test/status", "", "", "", "")
		var testStatus browserControlResult
		if status == 200 && json.Unmarshal(data, &testStatus) == nil && testStatus.RequestHeld {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("create did not reach request barrier")
		}
		time.Sleep(10 * time.Millisecond)
	}
	status, data, _ = request("GET", "/api/project-creations/"+requestBarrierKey, "", "", "", "")
	if status != 404 || !strings.Contains(string(data), "PROJECT_CREATION_NOT_FOUND") {
		t.Fatalf("request-held query status=%d body=%s", status, data)
	}
	status, data, _ = request("POST", "/__test/control", `{"action":"release_request"}`, origin, "", "")
	if status != 200 {
		t.Fatalf("release request control status=%d body=%s", status, data)
	}
	if code := <-requestBarrierDone; code != 201 {
		t.Fatalf("request barrier create status=%d", code)
	}
	status, data, _ = request("GET", "/api/project-creations/"+requestBarrierKey, "", "", "", "")
	if status != 200 {
		t.Fatalf("request barrier recovery status=%d body=%s", status, data)
	}

	barrierKey := "60000000-0000-4000-8000-000000000018"
	if err := server.holdCommit(context.Background()); err != nil {
		t.Fatal(err)
	}
	barrierDone := make(chan int, 1)
	go func() {
		code, _, _ := request("POST", "/api/projects", payload, origin, csrf, barrierKey)
		barrierDone <- code
	}()
	deadline = time.Now().Add(5 * time.Second)
	for {
		var held bool
		if err := server.pool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event='advisory' AND query LIKE '%creation_operations%')`).Scan(&held); err != nil {
			t.Fatal(err)
		}
		if held {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("create did not reach commit barrier")
		}
		time.Sleep(10 * time.Millisecond)
	}
	queryDone := make(chan int, 1)
	go func() {
		code, _, _ := request("GET", "/api/project-creations/"+barrierKey, "", "", "", "")
		queryDone <- code
	}()
	select {
	case code := <-queryDone:
		t.Fatalf("commit-held query did not wait, status=%d", code)
	case <-time.After(100 * time.Millisecond):
	}
	server.releaseCommit()
	if code := <-barrierDone; code != 201 {
		t.Fatalf("barrier create status=%d", code)
	}
	if code := <-queryDone; code != 200 {
		t.Fatalf("commit-held query status=%d", code)
	}

	dropKey := "60000000-0000-4000-8000-000000000017"
	server.mu.Lock()
	server.dropMethod, server.dropPath = http.MethodPost, "/api/projects"
	server.mu.Unlock()
	req, err := http.NewRequest(http.MethodPost, server.server.URL+"/api/projects", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", origin)
	req.Header.Set("X-CSRF-Token", csrf)
	req.Header.Set("Idempotency-Key", dropKey)
	dropClient := server.server.Client()
	dropClient.Jar = jar
	transport := dropClient.Transport.(*http.Transport).Clone()
	transport.DisableKeepAlives = true
	dropClient.Transport = transport
	if response, err := dropClient.Do(req); err == nil {
		response.Body.Close()
		t.Fatal("dropped response reached client")
	}
	projectService := projects.New(server.pool, server.auth)
	server.auth.SetProjectDestinationResolver(projectService.ResolveDestination)
	modelService := models.New(server.pool, server.auth, server.store, projects.NewCatalogWriter())
	server.auth.SetModelSaveDestinationResolver(modelService.ResolveDestination)
	restarted := httptest.NewUnstartedServer(nil)
	restartedOrigin := "https://" + restarted.Listener.Addr().String()
	restarted.Config.Handler = handlerConfigured(os.DirFS(assets), Auth{Service: server.auth, Origin: restartedOrigin}, Projects{Service: projectService}, Models{Service: modelService}, Scan{}, Decision{}, Skills{}, Issues{}, Messages{}, AgentTeams{}, Pipeline{}, HumanControl{}, ObserveV1{}, Discovery{}, Console{})
	restarted.StartTLS()
	defer restarted.Close()
	restartedClient := restarted.Client()
	restartedClient.Jar = jar
	restartedRequest, err := http.NewRequest(http.MethodGet, restarted.URL+"/api/project-creations/"+dropKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	restartedResponse, err := restartedClient.Do(restartedRequest)
	if err != nil {
		t.Fatal(err)
	}
	restartedData, readErr := io.ReadAll(restartedResponse.Body)
	restartedResponse.Body.Close()
	if readErr != nil || restartedResponse.StatusCode != 200 || !strings.Contains(string(restartedData), dropKey) {
		t.Fatalf("restart recovery status=%d body=%s error=%v", restartedResponse.StatusCode, restartedData, readErr)
	}
	if err := server.store.Destroy(context.Background(), secrets.VersionID(fixedSecretID)); err != nil {
		t.Fatal(err)
	}
	status, data, _ = request("GET", "/api/projects/"+receipt.ProjectID, "", "", "", "")
	if status != 200 || json.Unmarshal(data, &project) != nil || !slices.Contains(project.Configuration.Checks.ReasonCodes, "MODEL_SECRET_UNAVAILABLE") {
		t.Fatalf("destroyed fixed secret status=%d body=%s", status, data)
	}
	if _, err := server.pool.Exec(context.Background(), `UPDATE repomesh_projects.update_operations SET canonical_input=NULL,exact_input=NULL,removed_at=now()
		WHERE project_id=$1 AND actor=$2 AND key=$3`, receipt.ProjectID, server.fixtures.ActorA, updateKey); err != nil {
		t.Fatal(err)
	}
	if _, err := server.pool.Exec(context.Background(), `UPDATE repomesh_projects.projects SET removed_at=now() WHERE id=$1`, receipt.ProjectID); err != nil {
		t.Fatal(err)
	}
	status, data, _ = request("GET", "/api/projects/"+receipt.ProjectID+"/updates/"+updateKey, "", "", "", "")
	if status != 410 || !strings.Contains(string(data), "PROJECT_UPDATE_RESULT_REMOVED") {
		t.Fatalf("owner update tombstone status=%d body=%s", status, data)
	}
	status, data, _ = request("GET", "/api/project-creations/"+createKey, "", "", "", "")
	if status != 410 || !strings.Contains(string(data), "PROJECT_CREATION_RESULT_REMOVED") {
		t.Fatalf("owner creation tombstone status=%d body=%s", status, data)
	}
	otherJar, _ := cookiejar.New(nil)
	otherClient := server.server.Client()
	otherClient.Jar = otherJar
	otherClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	otherLogin, err := otherClient.Get(server.server.URL + "/__test/login?actor=b")
	if err != nil {
		t.Fatal(err)
	}
	otherLogin.Body.Close()
	if otherLogin.StatusCode != http.StatusSeeOther {
		t.Fatalf("other actor login status=%d", otherLogin.StatusCode)
	}
	for _, operationPath := range []string{"/api/projects/" + receipt.ProjectID + "/updates/" + updateKey, "/api/project-creations/" + createKey} {
		response, requestErr := otherClient.Get(server.server.URL + operationPath)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		body, readErr := io.ReadAll(response.Body)
		response.Body.Close()
		if readErr != nil || response.StatusCode != 404 || strings.Contains(string(body), "PROJECT_UPDATE_RESULT_REMOVED") || strings.Contains(string(body), "PROJECT_CREATION_RESULT_REMOVED") {
			t.Fatalf("cross-actor tombstone path=%s status=%d", operationPath, response.StatusCode)
		}
	}
}
