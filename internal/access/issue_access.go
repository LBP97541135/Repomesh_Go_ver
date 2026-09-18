package access

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"repomesh.local/repomesh/internal/github"
	"repomesh.local/repomesh/internal/secrets"
)

// IssueAccessRequest carries the exact locators the caller will read (the protected
// content union) and the locators it will write (the selected work scope). Both lists
// are constructed by access from current credentials; callers never mint their own.
type IssueAccessRequest struct {
	ReadRepositories []RepositoryLocator
	WorkRepositories []RepositoryLocator
}

// issueAppObservation records one App installation observation bound to a repository.
type issueAppObservation struct {
	repositoryID       string
	installationID     string
	permissionRevision string
	observedAt         time.Time
	allowed            bool
}

// IssueObservation is the full authorization surface for one issue creation attempt.
// credentialVersion is the exact access_ref backing the user token used for reads;
// it is distinct from connectionRevision/accessEpoch which track connection rotation.
type IssueObservation struct {
	actor               string
	connectionRevision  string
	credentialVersion   string
	accessEpoch         int64
	appKeyVersion       secrets.VersionID
	read                []RepositoryObservation
	work                []RepositoryObservation
	app                 []issueAppObservation
	authorizationFailed bool
}

// ReadRepositories returns the observed user-side state of every read repository.
func (o IssueObservation) ReadRepositories() []RepositoryObservation {
	return append([]RepositoryObservation(nil), o.read...)
}

// WorkRepositories returns the observed user-side state of every work repository.
func (o IssueObservation) WorkRepositories() []RepositoryObservation {
	return append([]RepositoryObservation(nil), o.work...)
}

func observationFor(repositories []RepositoryObservation, id string) (RepositoryObservation, bool) {
	for _, observation := range repositories {
		if observation.Locator.ID == id {
			return observation, true
		}
	}
	return RepositoryObservation{}, false
}

// observeIssueApp resolves the GitHub App installation for one repository using the
// runtime App credential; network errors degrade to unknown, never to a false denial.
func (s *Service) observeIssueApp(ctx context.Context, repository RepositoryLocator) (github.AppInstallationObservation, error) {
	return s.provider.ObserveAppInstallation(ctx, repository.Owner, repository.Name)
}

// ObserveIssueAccess performs all network observation outside any transaction lock.
// User reads use the exact current access_ref; App installation state is observed per
// repository. Unknown providers degrade to unconfirmed observations the checker will
// reject with 503 rather than a false denial.
func (s *Service) ObserveIssueAccess(ctx context.Context, principal ProjectPrincipal, request IssueAccessRequest) (IssueObservation, error) {
	observation := IssueObservation{actor: principal.actor}
	credential, err := s.credential(ctx, principal.actor, false)
	if err != nil {
		var denied *Failure
		if errors.As(err, &denied) && denied.Status == 503 {
			observation.authorizationFailed = true
			for _, locator := range request.ReadRepositories {
				observation.read = append(observation.read, RepositoryObservation{Locator: locator, ParticipationStatus: "unknown", ParticipationReasons: []string{"AUTHORIZATION_UNCONFIRMED"}})
			}
			for _, locator := range request.WorkRepositories {
				observation.work = append(observation.work, RepositoryObservation{Locator: locator, ParticipationStatus: "unknown", ParticipationReasons: []string{"AUTHORIZATION_UNCONFIRMED"}})
			}
			return observation, nil
		}
		return IssueObservation{}, err
	}
	observation.connectionRevision = credential.revision
	observation.accessEpoch = credential.epoch
	observation.credentialVersion = credential.accessRef
	observation.appKeyVersion = s.issueAppKeyVersion
	appendLocator := func(list []RepositoryLocator) []RepositoryObservation {
		result := make([]RepositoryObservation, 0, len(list))
		for _, locator := range list {
			entry := RepositoryObservation{Locator: locator, ParticipationStatus: "unknown", ParticipationReasons: []string{"AUTHORIZATION_UNCONFIRMED"}}
			repository, repositoryErr := s.provider.Repository(ctx, credential.token, locator.Owner, locator.Name)
			observed := time.Now().UTC()
			if repositoryErr == nil && repository.ID == locator.ExternalID {
				locator.Owner, locator.Name = repository.Owner, repository.Name
				entry.Locator = locator
				entry.ParticipationStatus = "allowed"
				entry.ParticipationReasons = []string{}
				entry.ObservedAt = &observed
			} else if repositoryErr != nil {
				var providerErr *github.Error
				if errors.As(repositoryErr, &providerErr) && providerErr.Kind == "denied" {
					entry.ParticipationStatus = "denied"
					entry.ParticipationReasons = []string{"REPOSITORY_ACCESS_DENIED"}
					entry.ObservedAt = &observed
				} else if errors.As(repositoryErr, &providerErr) && providerErr.Kind == "unauthorized" {
					s.rejectCredential(ctx, credential)
				}
			}
			result = append(result, entry)
		}
		return result
	}
	observation.read = appendLocator(request.ReadRepositories)
	observation.work = appendLocator(request.WorkRepositories)
	appFor := func(list []RepositoryObservation) {
		for _, entry := range list {
			if entry.ParticipationStatus != "allowed" {
				continue
			}
			app, appErr := s.observeIssueApp(ctx, entry.Locator)
			if appErr != nil {
				observation.app = append(observation.app, issueAppObservation{repositoryID: entry.Locator.ID, observedAt: time.Now().UTC(), allowed: false})
				continue
			}
			capability := app.Capability()
			observation.app = append(observation.app, issueAppObservation{
				repositoryID:       entry.Locator.ID,
				installationID:     fmt.Sprintf("%d", app.InstallationID()),
				permissionRevision: app.PermissionRevision(),
				observedAt:         app.ObservedAt(),
				allowed:            capability.Status == "allowed",
			})
		}
	}
	appFor(observation.read)
	appFor(observation.work)
	return observation, nil
}

// CheckIssueObservation locks the connection row and validates every observation in
// the request against what was actually observed. It runs inside the caller's
// transaction after principal/project locks; no network and no nested transaction.
func (s *Service) CheckIssueObservation(ctx context.Context, tx pgx.Tx, principal ProjectPrincipal, observation IssueObservation, request IssueAccessRequest) error {
	if observation.actor != principal.actor || observation.authorizationFailed {
		return failure(503, "AUTHORIZATION_UNCONFIRMED")
	}
	var current bool
	if err := tx.QueryRow(ctx, `SELECT revision=$2 AND access_epoch=$3 AND status='connected' AND refresh_state='idle'
		FROM repomesh_access.connections WHERE actor=$1 FOR UPDATE`, principal.actor, observation.connectionRevision, observation.accessEpoch).Scan(&current); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return failure(503, "AUTHORIZATION_UNCONFIRMED")
		}
		return unavailable()
	}
	if !current {
		return failure(503, "AUTHORIZATION_UNCONFIRMED")
	}
	if err := s.checkRepositoryObservations(observation.read, request.ReadRepositories); err != nil {
		return err
	}
	return s.checkRepositoryObservations(observation.work, request.WorkRepositories)
}

// checkRepositoryObservations rejects stale, future, missing, or non-allowed entries.
func (s *Service) checkRepositoryObservations(observed []RepositoryObservation, requested []RepositoryLocator) error {
	if len(observed) != len(requested) {
		return failure(503, "AUTHORIZATION_UNCONFIRMED")
	}
	oldest := func() time.Time { return time.Now().Add(-60 * time.Second) }
	now := time.Now()
	for _, entry := range observed {
		if entry.ParticipationStatus != "allowed" || entry.ObservedAt == nil {
			return failure(503, "AUTHORIZATION_UNCONFIRMED")
		}
		if entry.ObservedAt.Before(oldest()) || entry.ObservedAt.After(now) {
			return failure(503, "AUTHORIZATION_UNCONFIRMED")
		}
		if _, ok := observationFor(observed, entry.Locator.ID); !ok {
			return failure(503, "AUTHORIZATION_UNCONFIRMED")
		}
	}
	return nil
}

// CheckIssueObservationTime uses the database clock immediately before commit and
// rechecks local principal expiration; every necessary observation must be at most
// 60 seconds old and none may sit in the future.
func (s *Service) CheckIssueObservationTime(ctx context.Context, tx pgx.Tx, principal ProjectPrincipal, observation IssueObservation) error {
	var transactionNow time.Time
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&transactionNow); err != nil {
		return unavailable()
	}
	oldestAllowed := transactionNow.Add(-60 * time.Second)
	stale := func(list []RepositoryObservation) bool {
		for _, entry := range list {
			if entry.ParticipationStatus == "allowed" && (entry.ObservedAt == nil || entry.ObservedAt.Before(oldestAllowed) || entry.ObservedAt.After(transactionNow)) {
				return true
			}
		}
		return false
	}
	if stale(observation.read) || stale(observation.work) {
		return failure(503, "AUTHORIZATION_UNCONFIRMED")
	}
	for _, app := range observation.app {
		if app.allowed && (app.observedAt.Before(oldestAllowed) || app.observedAt.After(transactionNow)) {
			return failure(503, "AUTHORIZATION_UNCONFIRMED")
		}
	}
	// 复用调用方传入的事务:此时外层 commit 事务已持有 bindings 行锁
	// (LockProjectPrincipal),另开事务再锁同一行会自我死锁,只能等到
	// 请求超时 503。
	return s.LockProjectPrincipal(ctx, tx, principal)
}

// CheckIssueCredentialAvailability locks the exact user token version and App key
// version in sorted versionID order and validates stored owner/purpose/enabled/root
// metadata. Called after fixed-model availability and before the final time check.
// Unknown versions return 503, never a silent pass.
func (s *Service) CheckIssueCredentialAvailability(ctx context.Context, tx pgx.Tx, observation IssueObservation) error {
	if observation.credentialVersion == "" || observation.appKeyVersion == "" {
		return failure(503, "AUTHORIZATION_UNCONFIRMED")
	}
	versions := []secrets.VersionID{secrets.VersionID(observation.credentialVersion), observation.appKeyVersion}
	sort.Slice(versions, func(i, j int) bool { return versions[i] < versions[j] })
	for _, version := range versions {
		owner := secrets.Owner{Kind: "actor", ID: observation.actor}
		purpose := secrets.Purpose("github-user-token")
		if version == observation.appKeyVersion {
			owner = secrets.Owner{Kind: "github-app", ID: s.appID}
			purpose = secrets.Purpose("github-app-private-key")
		}
		inspection, err := s.secrets.InspectVersion(ctx, tx, version, owner, purpose)
		if err != nil {
			return unavailable()
		}
		if !inspection.Found || !inspection.Enabled || inspection.Destroyed || !inspection.RootAvailable {
			return failure(503, "AUTHORIZATION_UNCONFIRMED")
		}
	}
	return nil
}

// setIssueAppCredential records the exact App private-key version OpenRuntime imported
// and whose bytes the provider's PrivateKey closure decrypts, so credential availability
// checks lock the same version the provider will use.
func (s *Service) setIssueAppCredential(appID string, version secrets.VersionID) {
	s.appID = appID
	s.issueAppKeyVersion = version
}
