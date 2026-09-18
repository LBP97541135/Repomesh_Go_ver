package access

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"repomesh.local/repomesh/internal/github"
)

func TestPostgresCallbackOwnsIdentityConfirmationWhileLive(t *testing.T) {
	for _, purpose := range []string{"login", "reconnect"} {
		t.Run(purpose, func(t *testing.T) {
			s, provider, ctx := fixture(t)
			var binding, oldCookie string
			if purpose == "reconnect" {
				binding, oldCookie = login(t, s, ctx)
			}
			attempt := start(t, s, ctx, binding, oldCookie, purpose)
			if binding == "" {
				binding = attempt.BindingCookie
			}

			entered := make(chan struct{})
			release := make(chan struct{})
			var releaseOnce sync.Once
			releaseIdentity := func() { releaseOnce.Do(func() { close(release) }) }
			defer releaseIdentity()
			var identityCalls atomic.Int32
			provider.identity = func(ctx context.Context) (github.Identity, error) {
				if identityCalls.Add(1) == 1 {
					close(entered)
					select {
					case <-release:
					case <-ctx.Done():
						return github.Identity{}, ctx.Err()
					}
				}
				return github.Identity{ID: 123, DisplayName: "Test account"}, nil
			}

			type callbackOutcome struct {
				result CallbackResult
				err    error
			}
			callbackDone := make(chan callbackOutcome, 1)
			go func() {
				result, err := s.CompleteCallback(ctx, callbackInput(attempt, binding))
				callbackDone <- callbackOutcome{result: result, err: err}
			}()
			select {
			case <-entered:
			case <-time.After(2 * time.Second):
				t.Fatal("callback did not reach identity confirmation")
			}

			var sessionsBefore, revokedBefore int
			if err := s.pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE revoked) FROM repomesh_access.sessions`).Scan(&sessionsBefore, &revokedBefore); err != nil {
				t.Fatal(err)
			}
			worked, workerErr := s.RunOne(ctx)
			var stateDuring string
			if err := s.pool.QueryRow(ctx, `SELECT state FROM repomesh_access.attempts WHERE id=$1`, attempt.Result.AttemptID).Scan(&stateDuring); err != nil {
				t.Fatal(err)
			}
			var sessionsDuring, revokedDuring int
			if err := s.pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE revoked) FROM repomesh_access.sessions`).Scan(&sessionsDuring, &revokedDuring); err != nil {
				t.Fatal(err)
			}

			releaseIdentity()
			var callback callbackOutcome
			select {
			case callback = <-callbackDone:
			case <-time.After(2 * time.Second):
				t.Fatal("callback did not finish after identity release")
			}
			if workerErr != nil || worked || identityCalls.Load() != 1 || stateDuring != "identity_pending" ||
				sessionsDuring != sessionsBefore || revokedDuring != revokedBefore || callback.err != nil || callback.result.SessionCookie == "" {
				t.Fatalf("worker=%t workerErr=%v identityCalls=%d stateDuring=%s sessions=%d/%d revoked=%d/%d callbackCookie=%t callbackErr=%v",
					worked, workerErr, identityCalls.Load(), stateDuring, sessionsDuring, sessionsBefore,
					revokedDuring, revokedBefore, callback.result.SessionCookie != "", callback.err)
			}
		})
	}
}

func TestPostgresCallbackDeadlineReleasesIdentityRecovery(t *testing.T) {
	s, provider, ctx := fixture(t)
	attempt := start(t, s, ctx, "", "", "login")
	var identityCalls atomic.Int32
	provider.identity = func(callCtx context.Context) (github.Identity, error) {
		if identityCalls.Add(1) == 1 {
			<-callCtx.Done()
			return github.Identity{}, callCtx.Err()
		}
		return github.Identity{ID: 123, DisplayName: "Test account"}, nil
	}

	callbackCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	result, err := s.CompleteCallback(callbackCtx, callbackInput(attempt, attempt.BindingCookie))
	if err != nil || result.SessionCookie != "" {
		t.Fatal("timed-out callback returned authority", err)
	}
	worked, err := s.RunOne(ctx)
	if err != nil || !worked || identityCalls.Load() != 2 {
		t.Fatalf("deadline recovery worked=%t identityCalls=%d error=%v", worked, identityCalls.Load(), err)
	}
	var state string
	if err := s.pool.QueryRow(ctx, `SELECT state FROM repomesh_access.attempts WHERE id=$1`, attempt.Result.AttemptID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "confirmed" {
		t.Fatalf("recovery state=%s; want confirmed", state)
	}
}
