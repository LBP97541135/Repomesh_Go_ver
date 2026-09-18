package access

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"repomesh.local/repomesh/internal/github"
)

const repositoryVerifyConcurrency = 16

type RepositoryItem struct {
	ID                string            `json:"id"`
	DisplayName       string            `json:"displayName"`
	UserParticipation github.Capability `json:"userParticipation"`
	AppCapability     github.Capability `json:"appCapability"`
}

type Coverage struct {
	Status      string     `json:"status"`
	ReasonCodes []string   `json:"reasonCodes"`
	ObservedAt  *time.Time `json:"observedAt"`
}
type RepositoryPage struct {
	Items      []RepositoryItem `json:"items"`
	NextCursor *string          `json:"nextCursor"`
	Coverage   Coverage         `json:"coverage"`
}
type RepositoryQuery struct {
	Text, Cursor string
	Limit        int
	Refresh      bool
}
type batch struct {
	id, actor, revision, query string
	epoch, claim               int64
	upstream                   int
	complete, hadSuccess       bool
	expires                    time.Time
	observed                   *time.Time
}

func (s *Service) Repositories(ctx context.Context, cookie string, query RepositoryQuery) (RepositoryPage, error) {
	view, err := s.Session(ctx, cookie)
	if err != nil {
		return RepositoryPage{}, err
	}
	if !utf8.ValidString(query.Text) || utf8.RuneCountInString(query.Text) > 200 || query.Limit < 1 || query.Limit > 100 || len(query.Cursor) > 128 {
		return RepositoryPage{}, failure(422, "VALIDATION_FAILED")
	}
	var b batch
	var after int64
	if query.Cursor != "" {
		var savedQuery string
		err = s.pool.QueryRow(ctx, `SELECT batch,after_id,query FROM repomesh_access.discovery_cursors WHERE id=$1 AND actor=$2`, query.Cursor, view.User.ID).Scan(&b.id, &after, &savedQuery)
		if errors.Is(err, pgx.ErrNoRows) {
			return RepositoryPage{}, failure(400, "INVALID_CURSOR")
		}
		if err != nil {
			return RepositoryPage{}, unavailable()
		}
		if savedQuery != query.Text {
			return RepositoryPage{}, failure(400, "INVALID_CURSOR")
		}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return RepositoryPage{}, unavailable()
	}
	defer tx.Rollback(ctx)
	var revision string
	var epoch int64
	if err = tx.QueryRow(ctx, `SELECT revision,access_epoch FROM repomesh_access.connections WHERE actor=$1 FOR UPDATE`, view.User.ID).Scan(&revision, &epoch); err != nil {
		return RepositoryPage{}, failure(503, "AUTHORIZATION_UNCONFIRMED")
	}
	if b.id == "" {
		lookup := `SELECT id FROM repomesh_access.discovery_batches WHERE actor=$1 AND connection_revision=$2 AND access_epoch=$3 AND query=$4 AND expires_at>now() AND (NOT complete OR NOT delivered) ORDER BY expires_at DESC LIMIT 1`
		if !query.Refresh {
			lookup = `SELECT id FROM repomesh_access.discovery_batches WHERE actor=$1 AND connection_revision=$2 AND access_epoch=$3 AND query=$4 AND expires_at>now() AND (NOT complete OR NOT delivered OR had_success) ORDER BY CASE WHEN NOT complete OR NOT delivered THEN 0 ELSE 1 END, expires_at DESC LIMIT 1`
		}
		err = tx.QueryRow(ctx, lookup, view.User.ID, revision, epoch, query.Text).Scan(&b.id)
		if errors.Is(err, pgx.ErrNoRows) {
			b.id = newID()
			_, err = tx.Exec(ctx, `INSERT INTO repomesh_access.discovery_batches(id,actor,connection_revision,access_epoch,query,expires_at) VALUES($1,$2,$3,$4,$5,now()+interval '10 minutes')`, b.id, view.User.ID, revision, epoch, query.Text)
		}
		if err != nil {
			return RepositoryPage{}, unavailable()
		}
	}
	if tx.Commit(ctx) != nil {
		return RepositoryPage{}, unavailable()
	}
	b, err = s.readBatch(ctx, b.id)
	if err != nil {
		return RepositoryPage{}, err
	}
	if b.actor != view.User.ID || b.revision != revision || b.epoch != epoch || !b.expires.After(time.Now()) {
		return RepositoryPage{}, failure(409, "CURSOR_EXPIRED")
	}
	if !b.hadSuccess {
		return RepositoryPage{}, failure(503, "RESULT_UNCONFIRMED")
	}
	c, err := s.credential(ctx, view.User.ID, false)
	if err != nil {
		return RepositoryPage{}, err
	}
	rows, err := s.pool.Query(ctx, `SELECT github_id,owner,name,full_name,observed_at FROM repomesh_access.discovered_repositories WHERE batch=$1 AND github_id>$2 ORDER BY github_id LIMIT $3`, b.id, after, query.Limit+1)
	if err != nil {
		return RepositoryPage{}, unavailable()
	}
	var observed []observedRepo
	for rows.Next() {
		var r observedRepo
		if rows.Scan(&r.id, &r.owner, &r.name, &r.fullName, &r.at) != nil {
			rows.Close()
			return RepositoryPage{}, unavailable()
		}
		observed = append(observed, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return RepositoryPage{}, unavailable()
	}
	more := len(observed) > query.Limit
	if more {
		observed = observed[:query.Limit]
	}
	result := RepositoryPage{Items: []RepositoryItem{}, Coverage: Coverage{Status: "partial", ReasonCodes: []string{"APP_INSTALLATION_SCOPE"}, ObservedAt: b.observed}}
	if len(observed) > 0 {
		after = observed[len(observed)-1].id
	}
	verified := s.verifyDiscovered(ctx, c, query.Text, observed)
	unconfirmed := false
	unauthorized := false
	for _, item := range verified {
		if item.unauthorized {
			unauthorized = true
		}
		if item.unconfirmed {
			unconfirmed = true
			continue
		}
		if item.item == nil {
			continue
		}
		result.Items = append(result.Items, *item.item)
	}
	if unauthorized {
		s.rejectCredential(ctx, c)
	}
	if unconfirmed && len(result.Items) == 0 {
		return RepositoryPage{}, failure(503, "AUTHORIZATION_UNCONFIRMED")
	}
	if _, err = s.Session(ctx, cookie); err != nil {
		return RepositoryPage{}, err
	}
	var current bool
	if err = s.pool.QueryRow(ctx, `SELECT revision=$2 AND access_epoch=$3 AND status='connected' AND refresh_state='idle' FROM repomesh_access.connections WHERE actor=$1`, view.User.ID, b.revision, b.epoch).Scan(&current); err != nil {
		return RepositoryPage{}, unavailable()
	}
	if !current {
		return RepositoryPage{}, failure(409, "CURSOR_EXPIRED")
	}
	if more || !b.complete {
		cursor := randomToken()
		if _, err = s.pool.Exec(ctx, `INSERT INTO repomesh_access.discovery_cursors(id,batch,actor,query,after_id) VALUES($1,$2,$3,$4,$5)`, cursor, b.id, view.User.ID, query.Text, after); err != nil {
			return RepositoryPage{}, unavailable()
		}
		result.NextCursor = &cursor
	}
	if _, err = s.pool.Exec(ctx, `UPDATE repomesh_access.discovery_batches SET delivered=true WHERE id=$1`, b.id); err != nil {
		return RepositoryPage{}, unavailable()
	}
	return result, nil
}

func (s *Service) readBatch(ctx context.Context, id string) (batch, error) {
	var b batch
	err := s.pool.QueryRow(ctx, `SELECT id,actor,connection_revision,access_epoch,query,upstream_page,complete,had_success,claim_version,expires_at,observed_at FROM repomesh_access.discovery_batches WHERE id=$1`, id).Scan(&b.id, &b.actor, &b.revision, &b.epoch, &b.query, &b.upstream, &b.complete, &b.hadSuccess, &b.claim, &b.expires, &b.observed)
	if errors.Is(err, pgx.ErrNoRows) {
		return b, failure(409, "CURSOR_EXPIRED")
	}
	if err != nil {
		return b, unavailable()
	}
	return b, nil
}

type observedRepo struct {
	id                    int64
	owner, name, fullName string
	at                    time.Time
}

type verifiedRepo struct {
	item         *RepositoryItem
	unconfirmed  bool
	unauthorized bool
}

func (s *Service) verifyDiscovered(ctx context.Context, c credential, queryText string, observed []observedRepo) []verifiedRepo {
	result := make([]verifiedRepo, len(observed))
	if len(observed) == 0 {
		return result
	}
	limit := repositoryVerifyConcurrency
	if limit > len(observed) {
		limit = len(observed)
	}
	slots := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for i, repo := range observed {
		wg.Add(1)
		slots <- struct{}{}
		go func(i int, repo observedRepo) {
			defer wg.Done()
			defer func() { <-slots }()
			result[i] = s.verifyOneDiscovered(ctx, c, queryText, repo)
		}(i, repo)
	}
	wg.Wait()
	return result
}

func (s *Service) verifyOneDiscovered(ctx context.Context, c credential, queryText string, r observedRepo) verifiedRepo {
	if time.Since(r.at) > 60*time.Second {
		fresh, err := s.provider.Repository(ctx, c.token, r.owner, r.name)
		if err != nil {
			return verifiedRepo{unconfirmed: true, unauthorized: providerUnauthorized(err)}
		}
		if fresh.ID != r.id {
			return verifiedRepo{unconfirmed: true}
		}
		r.fullName = fresh.FullName
		r.owner = fresh.Owner
		r.name = fresh.Name
		r.at = time.Now().UTC()
	}
	if !strings.Contains(strings.ToLower(r.fullName), strings.ToLower(queryText)) {
		return verifiedRepo{}
	}
	capability, err := s.provider.AppCapability(ctx, r.owner, r.name)
	if err != nil {
		capability = github.Capability{Status: "unknown", ReasonCodes: []string{"APP_AUTHORIZATION_UNCONFIRMED"}}
	}
	observedAt := time.Now().UTC()
	item := RepositoryItem{ID: fmt.Sprintf("repo_%020d", r.id), DisplayName: r.fullName, UserParticipation: github.Capability{Status: "allowed", ReasonCodes: []string{}, ObservedAt: &observedAt}, AppCapability: capability}
	return verifiedRepo{item: &item}
}
