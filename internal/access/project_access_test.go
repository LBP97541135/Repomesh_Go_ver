package access

import (
	"testing"
	"time"
)

func TestPostgresProjectObservationMustRemainFreshUnderLock(t *testing.T) {
	service, _, ctx := fixture(t)
	_, cookie := login(t, service, ctx)
	session, err := service.Session(ctx, cookie)
	if err != nil {
		t.Fatal(err)
	}
	principal, err := service.AuthenticateProjectRequest(ctx, cookie, session.CSRFToken, true)
	if err != nil {
		t.Fatal(err)
	}
	var revision string
	var epoch int64
	if err = service.pool.QueryRow(ctx, `SELECT revision,access_epoch FROM repomesh_access.connections WHERE actor=$1`, principal.ActorID()).Scan(&revision, &epoch); err != nil {
		t.Fatal(err)
	}

	check := func(observation ProjectObservation) error {
		tx, beginErr := service.pool.Begin(ctx)
		if beginErr != nil {
			t.Fatal(beginErr)
		}
		defer tx.Rollback(ctx)
		if lockErr := service.LockProjectPrincipal(ctx, tx, principal); lockErr != nil {
			t.Fatal(lockErr)
		}
		return service.CheckProjectObservation(ctx, tx, principal, observation)
	}
	now := time.Now().UTC()
	fresh := ProjectObservation{actor: principal.ActorID(), connectionRevision: revision, accessEpoch: epoch, requiresConnection: true,
		repositories: []RepositoryObservation{{ParticipationStatus: "allowed", ObservedAt: &now}}}
	if err = check(fresh); err != nil {
		t.Fatal("fresh observation rejected", err)
	}
	staleAt := time.Now().UTC().Add(-61 * time.Second)
	stale := fresh
	stale.repositories = []RepositoryObservation{{ParticipationStatus: "allowed", ObservedAt: &staleAt}}
	wantFailure(t, check(stale), 503, "AUTHORIZATION_UNCONFIRMED")
	failed := ProjectObservation{actor: principal.ActorID(), authorizationFailed: true}
	wantFailure(t, check(failed), 503, "AUTHORIZATION_UNCONFIRMED")
}
