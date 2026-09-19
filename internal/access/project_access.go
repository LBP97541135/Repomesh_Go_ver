package access

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"repomesh.local/repomesh/internal/github"
	"repomesh.local/repomesh/internal/secrets"
)

type ProjectPrincipal struct {
	actor       string
	sessionHash string
	binding     string
	generation  int64
}

func (p ProjectPrincipal) ActorID() string { return p.actor }

type RepositoryLocator struct {
	ID         string
	Host       string
	ExternalID int64
	Owner      string
	Name       string
}

type RepositoryObservation struct {
	Locator              RepositoryLocator
	ParticipationStatus  string
	ParticipationReasons []string
	ObservedAt           *time.Time
	Item                 *RepositoryItem
}

type ProjectObservation struct {
	actor               string
	connectionRevision  string
	accessEpoch         int64
	repositories        []RepositoryObservation
	requiresConnection  bool
	authorizationFailed bool
}

func (o ProjectObservation) Repositories() []RepositoryObservation {
	return append([]RepositoryObservation(nil), o.repositories...)
}

func (s *Service) AuthenticateProjectRequest(ctx context.Context, cookie, csrf string, write bool) (ProjectPrincipal, error) {
	session, err := s.Session(ctx, cookie)
	if err != nil {
		return ProjectPrincipal{}, err
	}
	if write {
		if err := requireCSRF(session, csrf); err != nil {
			return ProjectPrincipal{}, err
		}
	}
	return ProjectPrincipal{actor: session.User.ID, sessionHash: digest(cookie), binding: session.Binding, generation: session.Generation}, nil
}

func (s *Service) LockProjectPrincipal(ctx context.Context, tx pgx.Tx, principal ProjectPrincipal) error {
	var identity int64
	var bindingExpires time.Time
	if err := tx.QueryRow(ctx, `SELECT identity_generation,expires_at FROM repomesh_access.bindings WHERE hash=$1 FOR UPDATE`, principal.binding).Scan(&identity, &bindingExpires); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return failure(401, "AUTHENTICATION_REQUIRED")
		}
		return unavailable()
	}
	var actor, binding string
	var generation int64
	var expires, lastActive time.Time
	var revoked bool
	if err := tx.QueryRow(ctx, `SELECT actor,binding,generation,expires_at,last_active_at,revoked FROM repomesh_access.sessions WHERE hash=$1 FOR UPDATE`, principal.sessionHash).Scan(&actor, &binding, &generation, &expires, &lastActive, &revoked); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return failure(401, "AUTHENTICATION_REQUIRED")
		}
		return unavailable()
	}
	var disabled bool
	if err := tx.QueryRow(ctx, `SELECT disabled FROM repomesh_access.accounts WHERE id=$1 FOR UPDATE`, principal.actor).Scan(&disabled); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return failure(401, "AUTHENTICATION_REQUIRED")
		}
		return unavailable()
	}
	now := time.Now()
	if disabled || revoked || actor != principal.actor || binding != principal.binding || generation != principal.generation || identity != principal.generation || !bindingExpires.After(now) || !expires.After(now) || !lastActive.After(now.Add(-30*time.Minute)) {
		return failure(401, "AUTHENTICATION_REQUIRED")
	}
	return nil
}

func (s *Service) RecheckProjectPrincipal(ctx context.Context, principal ProjectPrincipal) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return unavailable()
	}
	defer tx.Rollback(ctx)
	if err := s.LockProjectPrincipal(ctx, tx, principal); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return unavailable()
	}
	return nil
}

func (s *Service) ResolveSelectedRepositories(ctx context.Context, principal ProjectPrincipal, ids []string) ([]RepositoryLocator, error) {
	if err := s.RecheckProjectPrincipal(ctx, principal); err != nil {
		return nil, err
	}
	result := make([]RepositoryLocator, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if seen[id] || !strings.HasPrefix(id, "repo_") || len(id) != 25 {
			return nil, failure(404, "RESOURCE_NOT_FOUND")
		}
		external, err := strconv.ParseInt(strings.TrimPrefix(id, "repo_"), 10, 64)
		if err != nil || external <= 0 || id != fmt.Sprintf("repo_%020d", external) {
			return nil, failure(404, "RESOURCE_NOT_FOUND")
		}
		seen[id] = true
		var locator RepositoryLocator
		locator.ID, locator.Host, locator.ExternalID = id, "github.com", external
		err = s.pool.QueryRow(ctx, `SELECT r.owner,r.name FROM repomesh_access.discovered_repositories r
			JOIN repomesh_access.discovery_batches b ON b.id=r.batch
			JOIN repomesh_access.connections c ON c.actor=b.actor
			WHERE b.actor=$1 AND r.github_id=$2 AND b.connection_revision=c.revision AND b.access_epoch=c.access_epoch
			ORDER BY r.observed_at DESC LIMIT 1`, principal.actor, external).Scan(&locator.Owner, &locator.Name)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, failure(404, "RESOURCE_NOT_FOUND")
		}
		if err != nil {
			return nil, unavailable()
		}
		result = append(result, locator)
	}
	return result, nil
}

func (s *Service) ObserveProjectRepositories(ctx context.Context, principal ProjectPrincipal, repositories []RepositoryLocator) (ProjectObservation, error) {
	result := ProjectObservation{actor: principal.actor, repositories: make([]RepositoryObservation, 0, len(repositories))}
	if len(repositories) == 0 {
		return result, nil
	}
	credential, err := s.credential(ctx, principal.actor, false)
	if err != nil {
		var denied *Failure
		if errors.As(err, &denied) && denied.Status == 503 {
			result.authorizationFailed = true
			for _, locator := range repositories {
				result.repositories = append(result.repositories, RepositoryObservation{Locator: locator, ParticipationStatus: "unknown", ParticipationReasons: []string{"AUTHORIZATION_UNCONFIRMED"}})
			}
			return result, nil
		}
		return ProjectObservation{}, err
	}
	result.connectionRevision, result.accessEpoch, result.requiresConnection = credential.revision, credential.epoch, true
	for _, locator := range repositories {
		observation := RepositoryObservation{Locator: locator, ParticipationStatus: "unknown", ParticipationReasons: []string{"AUTHORIZATION_UNCONFIRMED"}}
		repository, repositoryErr := s.provider.Repository(ctx, credential.token, locator.Owner, locator.Name)
		observed := time.Now().UTC()
		if repositoryErr != nil {
			log.Printf("access: participation probe failed actor=%s repo=%s/%s err=%v", principal.ActorID(), locator.Owner, locator.Name, repositoryErr)
			var providerErr *github.Error
			if errors.As(repositoryErr, &providerErr) && providerErr.Kind == "denied" {
				observation.ParticipationStatus = "denied"
				observation.ParticipationReasons = []string{"REPOSITORY_ACCESS_DENIED"}
				observation.ObservedAt = &observed
			} else if errors.As(repositoryErr, &providerErr) && providerErr.Kind == "unauthorized" {
				s.rejectCredential(ctx, credential)
			}
			result.repositories = append(result.repositories, observation)
			continue
		}
		if repository.ID != locator.ExternalID {
			log.Printf("access: external id mismatch repo=%s/%s stored=%d github=%d", locator.Owner, locator.Name, locator.ExternalID, repository.ID)
			result.repositories = append(result.repositories, observation)
			continue
		}
		locator.Owner, locator.Name = repository.Owner, repository.Name
		observation.Locator = locator
		observation.ParticipationStatus = "allowed"
		observation.ParticipationReasons = []string{}
		observation.ObservedAt = &observed
		app, appErr := s.provider.AppCapability(ctx, repository.Owner, repository.Name)
		if appErr != nil {
			app = github.Capability{Status: "unknown", ReasonCodes: []string{"APP_AUTHORIZATION_UNCONFIRMED"}}
		}
		item := RepositoryItem{ID: locator.ID, DisplayName: repository.FullName,
			UserParticipation: github.Capability{Status: "allowed", ReasonCodes: []string{}, ObservedAt: &observed}, AppCapability: app}
		observation.Item = &item
		result.repositories = append(result.repositories, observation)
	}
	return result, nil
}

func (s *Service) CheckProjectObservation(ctx context.Context, tx pgx.Tx, principal ProjectPrincipal, observation ProjectObservation) error {
	if observation.actor != principal.actor {
		return failure(503, "AUTHORIZATION_UNCONFIRMED")
	}
	if observation.authorizationFailed {
		return failure(503, "AUTHORIZATION_UNCONFIRMED")
	}
	if !observation.requiresConnection {
		return nil
	}
	var current bool
	var transactionNow time.Time
	if err := tx.QueryRow(ctx, `SELECT revision=$2 AND access_epoch=$3 AND status='connected' AND refresh_state='idle', transaction_timestamp()
		FROM repomesh_access.connections WHERE actor=$1 FOR UPDATE`, principal.actor, observation.connectionRevision, observation.accessEpoch).Scan(&current, &transactionNow); err != nil {
		return failure(503, "AUTHORIZATION_UNCONFIRMED")
	}
	if !current {
		return failure(503, "AUTHORIZATION_UNCONFIRMED")
	}
	oldestAllowed := transactionNow.Add(-60 * time.Second)
	for _, repository := range observation.repositories {
		if repository.ParticipationStatus == "allowed" && (repository.ObservedAt == nil || repository.ObservedAt.Before(oldestAllowed) || repository.ObservedAt.After(transactionNow)) {
			return failure(503, "AUTHORIZATION_UNCONFIRMED")
		}
	}
	return nil
}

func (s *Service) InspectProjectSecret(ctx context.Context, tx pgx.Tx, id secrets.VersionID, owner secrets.Owner, purpose secrets.Purpose) (secrets.VersionInspection, error) {
	return s.secrets.InspectVersion(ctx, tx, id, owner, purpose)
}
