package typesafe

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"repomesh.local/repomesh/internal/secrets"
	"repomesh.local/repomesh/internal/testdb"
)

type fixture struct {
	s       *Service
	pool    *pgxpool.Pool
	project testdb.ProjectFixture
	issue   string
	calls   atomic.Int32
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{pool: testdb.Open(t), issue: "typesafe-issue"}
	f.project = testdb.SeedProject(t, f.pool, "", "", "test/repository")
	testdb.SeedIssue(t, f.pool, f.project, f.issue, "test/repository")
	root := make([]byte, 32)
	_, _ = rand.Read(root)
	rootPath := filepath.Join(t.TempDir(), "root")
	if err := os.WriteFile(rootPath, root, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := secrets.New(context.Background(), f.pool, secrets.Config{ActiveRootID: "typesafe-test", Roots: []secrets.RootFile{{ID: "typesafe-test", Path: rootPath}}})
	if err != nil {
		t.Fatal(err)
	}
	f.s = New(f.pool, store)
	f.s.client.http.Transport = roundTrip(func(r *http.Request) (*http.Response, error) { f.calls.Add(1); return fakeResponse(r), nil })
	return f
}
func (f *fixture) save(t *testing.T, revision int64, enabled bool, mode string) Settings {
	t.Helper()
	in := SaveInput{ExpectedRevision: revision, Enabled: enabled, Model: DefaultModel}
	in.Secret.Mode = mode
	if mode == "replace" {
		in.Secret.Value = "fixture-typesafe-provider-key"
	}
	v, err := f.s.Save(context.Background(), f.project.OwnerID, f.project.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func (f *fixture) run(t *testing.T, name, kind string) *Grant {
	t.Helper()
	ctx := context.Background()
	_, err := f.pool.Exec(ctx, `INSERT INTO repomesh_execution.workers(id,host,kind) VALUES('typesafe-worker','fixture','host_executor') ON CONFLICT DO NOTHING`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.pool.Exec(ctx, `INSERT INTO repomesh_execution.attempts(id,project_id,issue_id,worker_id,configuration_revision,state,reservation_generation)
		VALUES($1,$2,$3,'typesafe-worker',$4,'running',(SELECT COALESCE(MAX(reservation_generation),0)+1 FROM repomesh_execution.attempts WHERE project_id=$2 AND issue_id=$3))`, name+"-attempt", f.project.ID, f.issue, f.project.ConfigurationID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.pool.Exec(ctx, `INSERT INTO repomesh_execution.agent_runs(id,attempt_id,agent_kind,command,workspace,task_package_ref,state,pid,started_at)
		VALUES($1,$2,$3,'fixture','/fixture','fixture-task','running',1,clock_timestamp())`, name, name+"-attempt", kind)
	if err != nil {
		t.Fatal(err)
	}
	g, err := IssueGrant(ctx, f.pool, "typesafe-worker", name)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestPostgresSettingsScopeVaultAndRevision(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	v := f.save(t, 0, true, "replace")
	if !v.Enabled || !v.Configured || v.Revision != 1 || f.calls.Load() != 0 {
		t.Fatal("saving must not call upstream")
	}
	raw, _ := json.Marshal(v)
	if strings.Contains(string(raw), "fixture-typesafe-provider-key") {
		t.Fatal("read exposed key")
	}
	var ciphertext []byte
	if err := f.pool.QueryRow(ctx, `SELECT ciphertext FROM repomesh_secrets.versions WHERE purpose='typesafe-api-key'`).Scan(&ciphertext); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(ciphertext), "fixture-typesafe-provider-key") {
		t.Fatal("vault contains plaintext")
	}
	if _, err := f.s.Settings(ctx, "different-owner", f.project.ID); errorCode(err) != "RESOURCE_NOT_FOUND" {
		t.Fatal("cross-owner settings read allowed")
	}
	bad := SaveInput{ExpectedRevision: 0, Model: DefaultModel}
	bad.Secret.Mode = "keep"
	if _, err := f.s.Save(ctx, f.project.OwnerID, f.project.ID, bad); errorCode(err) != "TYPESAFE_REVISION_CONFLICT" {
		t.Fatal("stale save accepted")
	}
	if _, err := f.s.Save(ctx, "different-owner", f.project.ID, bad); errorCode(err) != "RESOURCE_NOT_FOUND" {
		t.Fatal("cross-owner settings write allowed")
	}
	g := f.run(t, "test-run", "test_agent")
	if g == nil {
		t.Fatal("enabled test lacks grant")
	}
	v = f.save(t, v.Revision, false, "clear")
	if v.Configured || v.Enabled {
		t.Fatal("clear did not disable")
	}
	if _, err := f.s.Evaluate(ctx, g.Token, sampleInput()); errorCode(err) != "TYPESAFE_GRANT_REJECTED" {
		t.Fatal("cleared key still usable")
	}
	var destroyed bool
	if err := f.pool.QueryRow(ctx, `SELECT destroyed_at IS NOT NULL AND ciphertext IS NULL FROM repomesh_secrets.versions WHERE purpose='typesafe-api-key'`).Scan(&destroyed); err != nil || !destroyed {
		t.Fatal("old key not destroyed")
	}
}

func TestPostgresGrantPurposeQuotaAndRevocation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.save(t, 0, true, "replace")
	if g := f.run(t, "developer", "codex_cli"); g != nil {
		t.Fatal("developer received Jev grant")
	}
	g := f.run(t, "reviewer", "review_agent")
	if g == nil || g.Purpose != "code_review" {
		t.Fatal("review purpose not derived from run")
	}
	for i := 0; i < MaxCalls; i++ {
		in := sampleInput()
		in.RequestID = string(rune('a' + i))
		e, err := f.s.Evaluate(ctx, g.Token, in)
		if err != nil {
			t.Fatal(err)
		}
		if e.Status != "completed" || e.Purpose != "code_review" || e.ProjectID != f.project.ID || e.TaskID != "fixture-task" {
			t.Fatal("evaluation lost scope")
		}
	}
	if _, err := f.s.Evaluate(ctx, g.Token, sampleInput()); errorCode(err) != "TYPESAFE_RUN_LIMIT" {
		t.Fatal("quota not enforced")
	}
	if err := FinishGrant(ctx, f.pool, "reviewer", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Evaluate(ctx, g.Token, sampleInput()); errorCode(err) != "TYPESAFE_GRANT_REJECTED" {
		t.Fatal("revoked token accepted")
	}
	if _, err := f.s.List(ctx, "different-owner", f.project.ID, f.issue); errorCode(err) != "RESOURCE_NOT_FOUND" {
		t.Fatal("cross-owner evidence read allowed")
	}
	items, err := f.s.List(ctx, f.project.OwnerID, f.project.ID, f.issue)
	if err != nil || len(items) != MaxCalls {
		t.Fatal("record read lost completed calls")
	}
	if f.calls.Load() != MaxCalls {
		t.Fatal("unexpected billable request count")
	}
}

func TestPostgresConcurrentReplayDoesNotResend(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.save(t, 0, true, "replace")
	g := f.run(t, "idempotent", "test_agent")
	entered, release := make(chan struct{}), make(chan struct{})
	f.s.client.http.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		f.calls.Add(1)
		close(entered)
		<-release
		return fakeResponse(r), nil
	})
	done := make(chan error, 1)
	go func() { _, err := f.s.Evaluate(ctx, g.Token, sampleInput()); done <- err }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("request not sent")
	}
	row, err := f.s.Evaluate(ctx, g.Token, sampleInput())
	if err != nil || row.Status != "pending" {
		t.Fatal("concurrent replay not returned")
	}
	changed := sampleInput()
	changed.Evidence = "different evidence"
	if _, err := f.s.Evaluate(ctx, g.Token, changed); errorCode(err) != "TYPESAFE_REQUEST_CONFLICT" {
		t.Fatal("request id reused for different evidence")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	row, err = f.s.Evaluate(ctx, g.Token, sampleInput())
	if err != nil || row.Status != "completed" || f.calls.Load() != 1 {
		t.Fatal("completed replay resent the request")
	}
}

func TestPostgresUnknownAndCheckDoNotForgeSuccess(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	v := f.save(t, 0, true, "replace")
	v, err := f.s.Check(ctx, f.project.OwnerID, f.project.ID, v.Revision)
	if err != nil || v.CheckStatus != "ready" {
		t.Fatal("synthetic inference check failed")
	}
	if _, err := f.s.Check(ctx, f.project.OwnerID, f.project.ID, v.Revision); errorCode(err) != "TYPESAFE_CHECK_COOLDOWN" {
		t.Fatal("check cooldown bypassed")
	}
	g := f.run(t, "unknown", "test_agent")
	f.s.client.http.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
		f.calls.Add(1)
		return nil, errors.New("connection reset")
	})
	row, err := f.s.Evaluate(ctx, g.Token, sampleInput())
	if err != nil || row.Status != "unknown" || row.Response != nil {
		t.Fatal("ambiguous outcome reported as known")
	}
	_, _ = f.s.Evaluate(ctx, g.Token, sampleInput())
	if f.calls.Load() != 2 {
		t.Fatal("unknown request automatically resent")
	}
	if err := FinishGrant(ctx, f.pool, "unknown", ""); err != nil {
		t.Fatal(err)
	}
	g = f.run(t, "unused", "test_agent")
	if g == nil {
		t.Fatal("missing grant")
	}
	if err := FinishGrant(ctx, f.pool, "unused", "TYPESAFE_BROKER_NOT_CONFIGURED"); err != nil {
		t.Fatal(err)
	}
	items, err := f.s.List(ctx, f.project.OwnerID, f.project.ID, f.issue)
	if err != nil {
		t.Fatal(err)
	}
	if items[0].Status != "unavailable" || items[0].ErrorCode != "TYPESAFE_BROKER_NOT_CONFIGURED" {
		t.Fatal("unavailable tool silently disappeared")
	}
}

func TestPostgresSwitchExpiryAndAccountDisableRevokeCalls(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	v := f.save(t, 0, true, "replace")
	g := f.run(t, "switch-run", "test_agent")
	v = f.save(t, v.Revision, false, "keep")
	if _, err := f.s.Evaluate(ctx, g.Token, sampleInput()); errorCode(err) != "TYPESAFE_GRANT_REJECTED" {
		t.Fatal("disabled project can call Jev")
	}
	v = f.save(t, v.Revision, true, "keep")
	if _, err := f.s.Evaluate(ctx, g.Token, sampleInput()); errorCode(err) != "TYPESAFE_GRANT_REJECTED" {
		t.Fatal("re-enabling revived an old revision grant")
	}
	g = f.run(t, "expired-run", "review_agent")
	if _, err := f.pool.Exec(ctx, `UPDATE repomesh_typesafe.run_grants SET expires_at=now()-interval '1 second' WHERE run_id='expired-run'`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Evaluate(ctx, g.Token, sampleInput()); errorCode(err) != "TYPESAFE_GRANT_REJECTED" {
		t.Fatal("expired grant accepted")
	}
	g = f.run(t, "disabled-account", "review_agent")
	if _, err := f.pool.Exec(ctx, `UPDATE repomesh_access.accounts SET disabled=true WHERE id=$1`, f.project.OwnerID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Evaluate(ctx, g.Token, sampleInput()); errorCode(err) != "TYPESAFE_GRANT_REJECTED" {
		t.Fatal("disabled account still spends its key")
	}
	if f.calls.Load() != 0 {
		t.Fatal("revoked requests reached upstream")
	}
}

func TestPostgresSaveCannotBypassCheckCooldown(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	v := f.save(t, 0, true, "replace")
	v, err := f.s.Check(ctx, f.project.OwnerID, f.project.ID, v.Revision)
	if err != nil {
		t.Fatal(err)
	}
	v = f.save(t, v.Revision, true, "keep")
	if _, err := f.s.Check(ctx, f.project.OwnerID, f.project.ID, v.Revision); errorCode(err) != "TYPESAFE_CHECK_COOLDOWN" {
		t.Fatal("saving reset the project connection-check budget")
	}
	if f.calls.Load() != 1 {
		t.Fatal("unexpected extra connection request")
	}
}
