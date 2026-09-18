package access

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"repomesh.local/repomesh/internal/github"
	"repomesh.local/repomesh/internal/secrets"
	"repomesh.local/repomesh/internal/testdb"
)

type fakeGitHub struct {
	exchanges, refreshes atomic.Int32
	exchange             func(context.Context) (github.TokenSet, error)
	refresh              func(context.Context) (github.TokenSet, error)
	identity             func(context.Context) (github.Identity, error)
	repositories         func(int) (github.RepositoryPage, error)
	repository           func() (github.Repository, error)
	appCapability        func(owner, name string) (github.Capability, error)
}

func (f *fakeGitHub) AuthorizationURL(state, challenge string) string {
	return "https://github.com/login/oauth/authorize?state=" + state + "&code_challenge=" + challenge
}
func (f *fakeGitHub) Exchange(ctx context.Context, _, _ string) (github.TokenSet, error) {
	f.exchanges.Add(1)
	if f.exchange != nil {
		return f.exchange(ctx)
	}
	return github.TokenSet{AccessToken: "test-access", RefreshToken: "test-refresh"}, nil
}
func (f *fakeGitHub) Refresh(ctx context.Context, _ string) (github.TokenSet, error) {
	f.refreshes.Add(1)
	if f.refresh != nil {
		return f.refresh(ctx)
	}
	return github.TokenSet{AccessToken: "new-access", RefreshToken: "new-refresh"}, nil
}
func (f *fakeGitHub) Identity(ctx context.Context, _ string) (github.Identity, error) {
	if f.identity != nil {
		return f.identity(ctx)
	}
	return github.Identity{ID: 123, DisplayName: "Test account"}, nil
}
func (f *fakeGitHub) Repositories(_ context.Context, _ string, page int) (github.RepositoryPage, error) {
	if f.repositories != nil {
		return f.repositories(page)
	}
	return github.RepositoryPage{Items: []github.Repository{{ID: 10, Owner: "test", Name: "repo", FullName: "test/repo"}}}, nil
}
func (f *fakeGitHub) Repository(context.Context, string, string, string) (github.Repository, error) {
	if f.repository != nil {
		return f.repository()
	}
	return github.Repository{ID: 10, Owner: "test", Name: "repo", FullName: "test/repo"}, nil
}
func (f *fakeGitHub) AppCapability(_ context.Context, owner, name string) (github.Capability, error) {
	if f.appCapability != nil {
		return f.appCapability(owner, name)
	}
	now := time.Now().UTC()
	return github.Capability{Status: "denied", ReasonCodes: []string{"APP_INSTALLATION_MISSING"}, ObservedAt: &now}, nil
}

func (f *fakeGitHub) ObserveAppInstallation(ctx context.Context, owner, name string) (github.AppInstallationObservation, error) {
	capability, err := f.AppCapability(ctx, owner, name)
	if err != nil {
		return github.AppInstallationObservation{}, err
	}
	now := time.Now().UTC()
	observedAt := now
	if capability.ObservedAt != nil {
		observedAt = *capability.ObservedAt
	}
	permissions := map[string]string{"contents": "write", "pull_requests": "write", "metadata": "read"}
	return github.ObservedInstallation(1, 1001, false, permissions, capability, observedAt), nil
}

func fixture(t *testing.T) (*Service, *fakeGitHub, context.Context) {
	t.Helper()
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	path := filepath.Join(t.TempDir(), "root.key")
	root := make([]byte, 32)
	rand.Read(root)
	if err := os.WriteFile(path, root, 0600); err != nil {
		t.Fatal(err)
	}
	store, err := secrets.New(ctx, pool, secrets.Config{ActiveRootID: "test-root", Roots: []secrets.RootFile{{ID: "test-root", Path: path}}})
	if err != nil {
		t.Fatal(err)
	}
	provider := &fakeGitHub{}
	return New(pool, store, provider), provider, ctx
}
func start(t *testing.T, s *Service, ctx context.Context, binding, cookie, purpose string) Started {
	t.Helper()
	destination, _ := ParseDestination([]byte(`{"kind":"home"}`))
	csrf := ""
	if cookie != "" {
		session, err := s.Session(ctx, cookie)
		if err != nil {
			t.Fatal(err)
		}
		csrf = session.CSRFToken
	}
	result, err := s.Start(ctx, StartCommand{ID: newID(), Purpose: purpose, BindingCookie: binding, SessionCookie: cookie, CSRF: csrf, Destination: destination})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func callbackInput(started Started, binding string) Callback {
	u, _ := url.Parse(started.Result.AuthorizationURL)
	return Callback{BindingCookie: binding, State: u.Query().Get("state"), Code: "test-code"}
}
func login(t *testing.T, s *Service, ctx context.Context) (string, string) {
	t.Helper()
	started := start(t, s, ctx, "", "", "login")
	result, err := s.CompleteCallback(ctx, callbackInput(started, started.BindingCookie))
	if err != nil || result.SessionCookie == "" {
		t.Fatalf("login result=%q error=%v", result.Page, err)
	}
	return started.BindingCookie, result.SessionCookie
}
func wantFailure(t *testing.T, err error, status int, code string) {
	t.Helper()
	var f *Failure
	if !errors.As(err, &f) || f.Status != status || f.Code != code {
		t.Fatalf("error=%v; want %d %s", err, status, code)
	}
}

func TestPostgresConcurrentStartAndCallback(t *testing.T) {
	s, p, ctx := fixture(t)
	first := start(t, s, ctx, "", "", "login")
	destination, _ := ParseDestination([]byte(`{"kind":"home"}`))
	command := StartCommand{ID: newID(), Purpose: "login", BindingCookie: first.BindingCookie, Destination: destination}
	results := make(chan Started, 20)
	errs := make(chan error, 20)
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() { result, err := s.Start(ctx, command); results <- result; errs <- err })
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var selected Started
	created := 0
	for result := range results {
		if selected.Result.AuthorizationURL != "" && result.Result.AuthorizationURL != selected.Result.AuthorizationURL {
			t.Fatal("same key produced different authorization URLs")
		}
		selected = result
		if result.Created {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("created %d attempts; want 1", created)
	}
	old, err := s.CompleteCallback(ctx, callbackInput(first, first.BindingCookie))
	if err != nil || old.SessionCookie != "" || p.exchanges.Load() != 0 {
		t.Fatal("superseded callback exchanged or signed a session")
	}
	input := callbackInput(selected, first.BindingCookie)
	callbacks := make(chan CallbackResult, 20)
	for range 20 {
		wg.Go(func() {
			result, err := s.CompleteCallback(ctx, input)
			if err != nil {
				t.Error(err)
			}
			callbacks <- result
		})
	}
	wg.Wait()
	close(callbacks)
	cookies := 0
	var cookie string
	for result := range callbacks {
		if result.SessionCookie != "" {
			cookies++
			cookie = result.SessionCookie
		}
	}
	if cookies != 1 || p.exchanges.Load() != 1 {
		t.Fatalf("cookies=%d exchanges=%d; want 1 each", cookies, p.exchanges.Load())
	}
	view, err := s.Session(ctx, cookie)
	if err != nil || view.User.DisplayName != "Test account" {
		t.Fatal("session identity not committed", err)
	}
	result, err := s.Attempt(ctx, first.BindingCookie, "", selected.Result.AttemptID)
	if err != nil || result.State != "confirmed" || result.NextPage != nil || result.Connection != nil {
		t.Fatal("cookie loss disclosed authority", result, err)
	}
	_, err = s.Attempt(ctx, randomToken(), cookie, selected.Result.AttemptID)
	wantFailure(t, err, 404, "RESOURCE_NOT_FOUND")
}

func TestPostgresUnknownExchangeNeverRepeats(t *testing.T) {
	s, p, ctx := fixture(t)
	p.exchange = func(context.Context) (github.TokenSet, error) { return github.TokenSet{}, context.DeadlineExceeded }
	attempt := start(t, s, ctx, "", "", "login")
	input := callbackInput(attempt, attempt.BindingCookie)
	for range 3 {
		if _, err := s.CompleteCallback(ctx, input); err != nil {
			t.Fatal(err)
		}
	}
	for range 3 {
		if _, err := s.RunOne(ctx); err != nil {
			t.Fatal(err)
		}
	}
	result, err := s.Attempt(ctx, attempt.BindingCookie, "", attempt.Result.AttemptID)
	if err != nil || result.State != "unknown" || p.exchanges.Load() != 1 {
		t.Fatalf("result=%+v exchanges=%d err=%v", result, p.exchanges.Load(), err)
	}
	destination, _ := ParseDestination([]byte(`{"kind":"home"}`))
	_, err = s.Start(ctx, StartCommand{ID: attempt.Result.AttemptID, Purpose: "login", BindingCookie: attempt.BindingCookie, Destination: destination})
	wantFailure(t, err, 409, "ATTEMPT_IN_PROGRESS")
}

func TestPostgresReadOnlyIdentityRecoveryAndCookieLoss(t *testing.T) {
	s, p, ctx := fixture(t)
	p.identity = func(context.Context) (github.Identity, error) { return github.Identity{}, context.DeadlineExceeded }
	attempt := start(t, s, ctx, "", "", "login")
	result, err := s.CompleteCallback(ctx, callbackInput(attempt, attempt.BindingCookie))
	if err != nil || result.SessionCookie != "" {
		t.Fatal("unknown identity signed a session", err)
	}
	p.identity = nil
	restarted := New(s.pool, s.secrets, p)
	worked, err := restarted.RunOne(ctx)
	if err != nil || !worked {
		t.Fatal("identity recovery did not run", err)
	}
	outcome, err := restarted.Attempt(ctx, attempt.BindingCookie, "", attempt.Result.AttemptID)
	if err != nil || outcome.State != "confirmed" || outcome.NextPage != nil || p.exchanges.Load() != 1 {
		t.Fatal("read recovery repeated exchange or granted cookie authority", outcome, err)
	}
	repeated, err := restarted.CompleteCallback(ctx, callbackInput(attempt, attempt.BindingCookie))
	if err != nil || repeated.SessionCookie != "" {
		t.Fatal("callback replay recovered a cookie", err)
	}
}

func TestPostgresReconnectMismatchAndLateLogout(t *testing.T) {
	s, p, ctx := fixture(t)
	binding, cookie := login(t, s, ctx)
	original, err := s.Session(ctx, cookie)
	if err != nil {
		t.Fatal(err)
	}
	attempt := start(t, s, ctx, binding, cookie, "reconnect")
	p.identity = func(context.Context) (github.Identity, error) {
		return github.Identity{ID: 456, DisplayName: "Other"}, nil
	}
	result, err := s.CompleteCallback(ctx, callbackInput(attempt, binding))
	if err != nil || result.SessionCookie != "" {
		t.Fatal("mismatched reconnect signed a cookie", err)
	}
	outcome, err := s.Attempt(ctx, binding, cookie, attempt.Result.AttemptID)
	if err != nil || outcome.State != "rejected" || outcome.ReasonCode == nil || *outcome.ReasonCode != "ACCOUNT_MISMATCH" {
		t.Fatal(outcome, err)
	}
	current, err := s.Session(ctx, cookie)
	if err != nil || current.User.ID != original.User.ID {
		t.Fatal("mismatch changed original session", err)
	}
	p.identity = nil
	attempt = start(t, s, ctx, binding, cookie, "reconnect")
	result, err = s.CompleteCallback(ctx, callbackInput(attempt, binding))
	if err != nil || result.SessionCookie == "" {
		t.Fatal("reconnect failed", err)
	}
	if err = s.Logout(ctx, cookie, original.CSRFToken); err != nil {
		t.Fatal(err)
	}
	current, err = s.Session(ctx, result.SessionCookie)
	if err != nil || current.User.ID != original.User.ID {
		t.Fatal("late logout revoked newer session", err)
	}
	outcome, err = s.Attempt(ctx, binding, result.SessionCookie, attempt.Result.AttemptID)
	if err != nil || outcome.Connection == nil || !outcome.Connection.IsCurrent {
		t.Fatal("missing reconnect receipt", outcome, err)
	}
}

func TestPostgresCallbackCannotWinAfterLogout(t *testing.T) {
	s, p, ctx := fixture(t)
	binding, cookie := login(t, s, ctx)
	view, _ := s.Session(ctx, cookie)
	attempt := start(t, s, ctx, binding, cookie, "reconnect")
	reached := make(chan struct{})
	release := make(chan struct{})
	p.exchange = func(context.Context) (github.TokenSet, error) {
		close(reached)
		<-release
		return github.TokenSet{AccessToken: "late-access"}, nil
	}
	done := make(chan CallbackResult, 1)
	go func() {
		result, err := s.CompleteCallback(ctx, callbackInput(attempt, binding))
		if err != nil {
			t.Error(err)
		}
		done <- result
	}()
	<-reached
	if err := s.Logout(ctx, cookie, view.CSRFToken); err != nil {
		t.Fatal(err)
	}
	close(release)
	if result := <-done; result.SessionCookie != "" {
		t.Fatal("late callback restored revoked generation")
	}
	_, err := s.Session(ctx, cookie)
	wantFailure(t, err, 401, "AUTHENTICATION_REQUIRED")
}

func TestPostgresRefreshHasOneOwnerAndUnknownStaysUnknown(t *testing.T) {
	s, p, ctx := fixture(t)
	_, cookie := login(t, s, ctx)
	view, _ := s.Session(ctx, cookie)
	if _, err := s.pool.Exec(ctx, `UPDATE repomesh_access.connections SET access_expires_at=now(),refresh_expires_at=now()+interval '1 day'`); err != nil {
		t.Fatal(err)
	}
	p.refresh = func(context.Context) (github.TokenSet, error) { return github.TokenSet{}, context.DeadlineExceeded }
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() { _, _ = s.credential(ctx, view.User.ID, true) })
	}
	wg.Wait()
	restarted := New(s.pool, s.secrets, p)
	for range 3 {
		_, _ = restarted.RunOne(ctx)
	}
	if p.refreshes.Load() != 1 {
		t.Fatalf("refresh attempted %d times", p.refreshes.Load())
	}
	current, err := s.Session(ctx, cookie)
	if err != nil || current.GitHubConnection.Status != "unknown" {
		t.Fatal("unknown refresh removed local session or reported connected", err)
	}
}

func TestPostgresDiscoveryPersistsEmptyCursorAndPartialCoverage(t *testing.T) {
	s, p, ctx := fixture(t)
	_, cookie := login(t, s, ctx)
	p.repositories = func(page int) (github.RepositoryPage, error) {
		switch page {
		case 1:
			return github.RepositoryPage{Items: []github.Repository{{ID: 1, Owner: "test", Name: "other", FullName: "test/other"}}, NextPage: 2}, nil
		case 2:
			return github.RepositoryPage{Items: []github.Repository{}, NextPage: 3}, nil
		default:
			return github.RepositoryPage{Items: []github.Repository{{ID: 10, Owner: "test", Name: "match", FullName: "test/match"}}}, nil
		}
	}
	query := RepositoryQuery{Text: "match", Limit: 1}
	_, err := s.Repositories(ctx, cookie, query)
	wantFailure(t, err, 503, "RESULT_UNCONFIRMED")
	if _, err = s.RunOne(ctx); err != nil {
		t.Fatal(err)
	}
	first, err := s.Repositories(ctx, cookie, query)
	if err != nil || len(first.Items) != 0 || first.NextCursor == nil || first.Coverage.Status != "partial" {
		t.Fatal("filtered empty page lost continuation", first, err)
	}
	if _, err = s.pool.Exec(ctx, `UPDATE repomesh_access.discovery_batches SET next_run_at=now()`); err != nil {
		t.Fatal(err)
	}
	restarted := New(s.pool, s.secrets, p)
	if _, err = restarted.RunOne(ctx); err != nil {
		t.Fatal(err)
	}
	query.Cursor = *first.NextCursor
	last, err := restarted.Repositories(ctx, cookie, query)
	if err != nil || len(last.Items) != 1 || last.Items[0].DisplayName != "test/match" || last.NextCursor != nil || last.Coverage.Status != "partial" {
		t.Fatal("persisted continuation disappeared or scope became complete", last, err)
	}
	query.Cursor = ""
	query.Limit = 100
	reused, err := restarted.Repositories(ctx, cookie, query)
	if err != nil || len(reused.Items) != 1 || reused.Items[0].DisplayName != "test/match" {
		t.Fatal("delivered discovery was not reread", reused, err)
	}
	query.Refresh = true
	_, err = restarted.Repositories(ctx, cookie, query)
	wantFailure(t, err, 503, "RESULT_UNCONFIRMED")
	if _, err = s.pool.Exec(ctx, `UPDATE repomesh_access.connections SET access_epoch=access_epoch+1`); err != nil {
		t.Fatal(err)
	}
	query.Cursor = *first.NextCursor
	_, err = s.Repositories(ctx, cookie, query)
	wantFailure(t, err, 409, "CURSOR_EXPIRED")
}

func TestPostgresDiscoveryReverifiesStaleIdentityWithoutOpeningANewBatch(t *testing.T) {
	s, p, ctx := fixture(t)
	_, cookie := login(t, s, ctx)
	query := RepositoryQuery{Limit: 50}
	_, _ = s.Repositories(ctx, cookie, query)
	if _, err := s.RunOne(ctx); err != nil {
		t.Fatal(err)
	}
	first, err := s.Repositories(ctx, cookie, query)
	if err != nil || len(first.Items) != 1 {
		t.Fatal("fresh snapshot missing", first, err)
	}
	if _, err = s.pool.Exec(ctx, `UPDATE repomesh_access.discovery_batches SET observed_at=now()-interval '90 seconds'`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, `UPDATE repomesh_access.discovered_repositories SET observed_at=now()-interval '90 seconds'`); err != nil {
		t.Fatal(err)
	}
	var repoCalls atomic.Int32
	p.repository = func() (github.Repository, error) {
		repoCalls.Add(1)
		return github.Repository{ID: 10, Owner: "test", Name: "repo", FullName: "test/repo"}, nil
	}
	page, err := s.Repositories(ctx, cookie, query)
	if err != nil || len(page.Items) != 1 {
		t.Fatal("successful snapshot was discarded instead of re-verified", page, err)
	}
	if repoCalls.Load() != 1 {
		t.Fatal("stale identity was not re-verified", repoCalls.Load())
	}
}

func TestPostgresDiscoveryHidesUnconfirmedNames(t *testing.T) {
	s, p, ctx := fixture(t)
	_, cookie := login(t, s, ctx)
	query := RepositoryQuery{Limit: 50}
	_, _ = s.Repositories(ctx, cookie, query)
	if _, err := s.RunOne(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE repomesh_access.discovered_repositories SET observed_at=now()-interval '61 seconds'`); err != nil {
		t.Fatal(err)
	}
	p.repository = func() (github.Repository, error) { return github.Repository{}, &github.Error{Kind: "denied"} }
	result, err := s.Repositories(ctx, cookie, query)
	wantFailure(t, err, 503, "AUTHORIZATION_UNCONFIRMED")
	if len(result.Items) != 0 {
		t.Fatal("old private repository name was disclosed")
	}
}

func TestPostgresDiscoveryRefreshesParticipationObservation(t *testing.T) {
	s, _, ctx := fixture(t)
	_, cookie := login(t, s, ctx)
	query := RepositoryQuery{Limit: 50}
	_, _ = s.Repositories(ctx, cookie, query)
	if _, err := s.RunOne(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE repomesh_access.discovered_repositories SET observed_at=now()-interval '30 seconds'`); err != nil {
		t.Fatal(err)
	}
	page, err := s.Repositories(ctx, cookie, query)
	if err != nil || len(page.Items) != 1 || page.Items[0].UserParticipation.ObservedAt == nil {
		t.Fatal("verified page missing", page, err)
	}
	if age := time.Since(*page.Items[0].UserParticipation.ObservedAt); age > 2*time.Second {
		t.Fatal("verified observation was not refreshed", age)
	}
}

func TestPostgresStaleRefreshDiscardsUncommittedSecrets(t *testing.T) {
	s, p, ctx := fixture(t)
	binding, cookie := login(t, s, ctx)
	view, _ := s.Session(ctx, cookie)
	if _, err := s.pool.Exec(ctx, `UPDATE repomesh_access.connections SET access_expires_at=now(),refresh_expires_at=now()+interval '1 day'`); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	p.refresh = func(context.Context) (github.TokenSet, error) {
		close(entered)
		<-release
		return github.TokenSet{AccessToken: "obsolete-access", RefreshToken: "obsolete-refresh"}, nil
	}
	done := make(chan error, 1)
	go func() { _, err := s.credential(ctx, view.User.ID, true); done <- err }()
	<-entered
	attempt := start(t, s, ctx, binding, cookie, "reconnect")
	result, err := s.CompleteCallback(ctx, callbackInput(attempt, binding))
	if err != nil || result.SessionCookie == "" {
		t.Fatal("replacement reconnect failed", err)
	}
	close(release)
	if err := <-done; err == nil {
		t.Fatal("stale refresh reported success")
	}
	var dangling int
	err = s.pool.QueryRow(ctx, `SELECT count(*) FROM repomesh_secrets.versions v JOIN repomesh_secrets.availability a USING(version_id)
	WHERE v.purpose IN ('github-user-token','github-refresh-token') AND a.enabled AND v.destroyed_at IS NULL
	AND NOT EXISTS(SELECT 1 FROM repomesh_access.connections c WHERE c.access_ref=v.version_id OR c.refresh_ref=v.version_id)`).Scan(&dangling)
	if err != nil || dangling != 2 {
		t.Fatalf("unreferenced versions=%d want only 2 formerly committed versions; error=%v", dangling, err)
	}
	if _, err = s.pool.Exec(ctx, `UPDATE repomesh_secrets.versions SET created_at=now()-interval '16 minutes'`); err != nil {
		t.Fatal(err)
	}
	if err = s.maintain(ctx); err != nil {
		t.Fatal(err)
	}
	err = s.pool.QueryRow(ctx, `SELECT count(*) FROM repomesh_secrets.versions v WHERE v.destroyed_at IS NULL AND NOT EXISTS(SELECT 1 FROM repomesh_access.connections c WHERE c.access_ref=v.version_id OR c.refresh_ref=v.version_id)`).Scan(&dangling)
	if err != nil || dangling != 0 {
		t.Fatalf("orphan cleanup left %d live secrets; error=%v", dangling, err)
	}
	if _, err = s.Session(ctx, result.SessionCookie); err != nil {
		t.Fatal("cleanup invalidated current session", err)
	}
	if _, err = s.credential(ctx, view.User.ID, false); err != nil {
		t.Fatal("cleanup destroyed committed credential", err)
	}
}

func TestPostgresReconnectCannotReplaceNewerOtherBrowserConnection(t *testing.T) {
	s, _, ctx := fixture(t)
	bindingA, cookieA := login(t, s, ctx)
	bindingB, cookieB := login(t, s, ctx)
	older := start(t, s, ctx, bindingA, cookieA, "reconnect")
	newer := start(t, s, ctx, bindingB, cookieB, "reconnect")
	accepted, err := s.CompleteCallback(ctx, callbackInput(newer, bindingB))
	if err != nil || accepted.SessionCookie == "" {
		t.Fatal("newer reconnect failed", err)
	}
	late, err := s.CompleteCallback(ctx, callbackInput(older, bindingA))
	if err != nil || late.SessionCookie != "" {
		t.Fatal("stale reconnect signed a session", err)
	}
	result, err := s.Attempt(ctx, bindingA, cookieA, older.Result.AttemptID)
	if err != nil || result.State != "superseded" {
		t.Fatal("stale cross-browser reconnect not fenced", result, err)
	}
	result, err = s.Attempt(ctx, bindingB, accepted.SessionCookie, newer.Result.AttemptID)
	if err != nil || result.Connection == nil || !result.Connection.IsCurrent {
		t.Fatal("late reconnect replaced newer connection", result, err)
	}
}

func TestPostgresAttemptLimitExpiryAndConflict(t *testing.T) {
	s, _, ctx := fixture(t)
	first := start(t, s, ctx, "", "", "login")
	other, _ := ParseDestination([]byte(`{"kind":"project","projectId":"prj_1"}`))
	_, err := s.Start(ctx, StartCommand{ID: first.Result.AttemptID, Purpose: "login", BindingCookie: first.BindingCookie, Destination: other})
	wantFailure(t, err, 409, "IDEMPOTENCY_CONFLICT")
	for range 4 {
		start(t, s, ctx, first.BindingCookie, "", "login")
	}
	home, _ := ParseDestination([]byte(`{"kind":"home"}`))
	_, err = s.Start(ctx, StartCommand{ID: newID(), Purpose: "login", BindingCookie: first.BindingCookie, Destination: home})
	wantFailure(t, err, 429, "RATE_LIMITED")
	var f *Failure
	errors.As(err, &f)
	if f.RetryAfter != 60 {
		t.Fatal("missing retry deadline")
	}
	if _, err = s.pool.Exec(ctx, `UPDATE repomesh_access.attempts SET created_at=now()-interval '25 hours' WHERE binding=$1 AND id=$2`, digest(first.BindingCookie), first.Result.AttemptID); err != nil {
		t.Fatal(err)
	}
	_, err = s.Attempt(ctx, first.BindingCookie, "", first.Result.AttemptID)
	wantFailure(t, err, 410, "AUTH_ATTEMPT_RESULT_REMOVED")
	_, err = s.Attempt(ctx, randomToken(), "", first.Result.AttemptID)
	wantFailure(t, err, 404, "RESOURCE_NOT_FOUND")
}

func TestPostgresDelayedAnonymousLoginCannotReplaceReconnectedActor(t *testing.T) {
	s, p, ctx := fixture(t)
	bindingB, cookieB := login(t, s, ctx)
	older := start(t, s, ctx, "", "", "login")
	entered := make(chan struct{})
	release := make(chan struct{})
	var queries atomic.Int32
	p.identity = func(context.Context) (github.Identity, error) {
		if queries.Add(1) == 1 {
			close(entered)
			<-release
		}
		return github.Identity{ID: 123, DisplayName: "Test account"}, nil
	}
	done := make(chan CallbackResult, 1)
	go func() {
		result, err := s.CompleteCallback(ctx, callbackInput(older, older.BindingCookie))
		if err != nil {
			t.Error(err)
		}
		done <- result
	}()
	<-entered
	newer := start(t, s, ctx, bindingB, cookieB, "reconnect")
	accepted, err := s.CompleteCallback(ctx, callbackInput(newer, bindingB))
	if err != nil || accepted.SessionCookie == "" {
		t.Fatal("new reconnect failed", err)
	}
	close(release)
	if result := <-done; result.SessionCookie != "" {
		t.Fatal("late anonymous login signed a new session")
	}
	result, err := s.Attempt(ctx, older.BindingCookie, "", older.Result.AttemptID)
	if err != nil || result.State != "superseded" || result.Connection != nil {
		t.Fatal("late anonymous login was not fenced", result, err)
	}
	result, err = s.Attempt(ctx, bindingB, accepted.SessionCookie, newer.Result.AttemptID)
	if err != nil || result.Connection == nil || !result.Connection.IsCurrent {
		t.Fatal("late login replaced current connection", result, err)
	}
}

func TestPostgresDiscoveryOldClaimCannotPublish(t *testing.T) {
	s, p, ctx := fixture(t)
	_, cookie := login(t, s, ctx)
	_, _ = s.Repositories(ctx, cookie, RepositoryQuery{Limit: 50})
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	p.repositories = func(int) (github.RepositoryPage, error) {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
			return github.RepositoryPage{Items: []github.Repository{{ID: 1, Owner: "test", Name: "stale", FullName: "test/stale"}}}, nil
		}
		return github.RepositoryPage{Items: []github.Repository{{ID: 2, Owner: "test", Name: "current", FullName: "test/current"}}}, nil
	}
	done := make(chan error, 1)
	go func() { _, err := s.RunOne(ctx); done <- err }()
	<-entered
	if _, err := s.pool.Exec(ctx, `UPDATE repomesh_access.discovery_batches SET lease_until=now()-interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	restarted := New(s.pool, s.secrets, p)
	if worked, err := restarted.RunOne(ctx); err != nil || !worked {
		t.Fatal("replacement claim did not execute", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	result, err := s.Repositories(ctx, cookie, RepositoryQuery{Limit: 50})
	if err != nil || len(result.Items) != 1 || result.Items[0].DisplayName != "test/current" {
		t.Fatal("expired claim overwrote replacement", result, err)
	}
}

func TestPostgresDiscoveryVerifiesRepositoriesConcurrently(t *testing.T) {
	s, p, ctx := fixture(t)
	_, cookie := login(t, s, ctx)
	items := make([]github.Repository, 8)
	for i := range items {
		id := int64(i + 1)
		items[i] = github.Repository{ID: id, Owner: "test", Name: fmt.Sprintf("repo%d", id), FullName: fmt.Sprintf("test/repo%d", id)}
	}
	p.repositories = func(int) (github.RepositoryPage, error) {
		return github.RepositoryPage{Items: items}, nil
	}
	p.appCapability = func(string, string) (github.Capability, error) {
		time.Sleep(200 * time.Millisecond)
		now := time.Now().UTC()
		return github.Capability{Status: "allowed", ReasonCodes: []string{}, ObservedAt: &now}, nil
	}
	if _, err := s.Repositories(ctx, cookie, RepositoryQuery{Limit: 50}); err == nil {
		t.Fatal("expected unconfirmed discovery before worker")
	}
	if _, err := s.RunOne(ctx); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	result, err := s.Repositories(ctx, cookie, RepositoryQuery{Limit: 50})
	elapsed := time.Since(started)
	if err != nil || len(result.Items) != 8 {
		t.Fatalf("page = %+v, err = %v", result, err)
	}
	if elapsed > 800*time.Millisecond {
		t.Fatalf("serial capability checks took %s", elapsed)
	}
	for i, item := range result.Items {
		if item.DisplayName != items[i].FullName || item.AppCapability.Status != "allowed" {
			t.Fatalf("item %d = %+v", i, item)
		}
	}
}
