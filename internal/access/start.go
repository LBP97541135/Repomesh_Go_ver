package access

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"repomesh.local/repomesh/internal/secrets"
)

type exchangeMaterial struct{ State, Verifier string }

func attemptOwner(binding, id string) secrets.Owner {
	return secrets.Owner{Kind: "auth-attempt", ID: binding + ":" + id}
}

func (s *Service) Start(ctx context.Context, command StartCommand) (Started, error) {
	if !ValidID(command.ID) || command.Destination.canonical == "" || (command.Purpose != "login" && command.Purpose != "reconnect" && command.Purpose != "switch") {
		return Started{}, failure(422, "VALIDATION_FAILED")
	}
	current, sessionErr := s.Session(ctx, command.SessionCookie)
	if sessionErr != nil {
		var f *Failure
		if !errors.As(sessionErr, &f) || f.Status != 401 {
			return Started{}, sessionErr
		}
	}
	if command.Purpose == "login" && sessionErr == nil {
		return Started{}, failure(409, "SESSION_ALREADY_ACTIVE")
	}
	// switch：已登录状态下主动换成另一个 GitHub 账号。
	// 与 login 的差别只有一条——允许带着活跃会话发起（login 遇活跃会话直接 409）；
	// 与 reconnect 的差别是**不写 expected_github_id**，因此回调允许换成完全不同的身份。
	// 旧会话不需要在这里清理：callback.confirmIdentity 成功后会 identity_generation+1
	// 并把该 binding 上所有 sessions 置 revoked，再种一条新代际的会话
	//（callback.go 的 bindings/sessions 两处 UPDATE），旧 cookie 随即失效。
	if command.Purpose == "switch" {
		if sessionErr != nil {
			return Started{}, sessionErr
		}
		if err := requireCSRF(current, command.CSRF); err != nil {
			return Started{}, err
		}
		if !validCookie(command.BindingCookie) || digest(command.BindingCookie) != current.Binding {
			return Started{}, failure(403, "ORIGIN_REJECTED")
		}
	}
	if command.Purpose == "reconnect" {
		if sessionErr != nil {
			return Started{}, sessionErr
		}
		if err := requireCSRF(current, command.CSRF); err != nil {
			return Started{}, err
		}
		if !validCookie(command.BindingCookie) || digest(command.BindingCookie) != current.Binding {
			return Started{}, failure(403, "ORIGIN_REJECTED")
		}
	}
	bindingCookie := command.BindingCookie
	var exists bool
	if validCookie(bindingCookie) {
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM repomesh_access.bindings WHERE hash=$1 AND expires_at>now())`, digest(bindingCookie)).Scan(&exists); err != nil {
			return Started{}, unavailable()
		}
	}
	newBinding := !exists
	if newBinding {
		bindingCookie = randomToken()
	}
	binding := digest(bindingCookie)
	if exists {
		result, found, err := s.replayStart(ctx, binding, command)
		if found || err != nil {
			return Started{Result: result}, err
		}
	}
	material := exchangeMaterial{State: randomToken(), Verifier: randomToken()}
	encoded, _ := json.Marshal(material)
	ref, err := s.secrets.Seal(ctx, attemptOwner(binding, command.ID), secrets.Purpose("auth-material"), encoded)
	if err != nil {
		return Started{}, unavailable()
	}
	keep := false
	defer func() {
		if !keep {
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = s.secrets.Destroy(cleanup, ref)
		}
	}()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Started{}, unavailable()
	}
	defer tx.Rollback(ctx)
	if newBinding {
		if _, err = tx.Exec(ctx, `INSERT INTO repomesh_access.bindings(hash,expires_at) VALUES($1,now()+interval '7 days')`, binding); err != nil {
			return Started{}, unavailable()
		}
	}
	var identity, attempt int64
	var original *string
	if err = tx.QueryRow(ctx, `SELECT identity_generation,attempt_generation,last_actor FROM repomesh_access.bindings WHERE hash=$1 AND expires_at>now() FOR UPDATE`, binding).Scan(&identity, &attempt, &original); err != nil {
		return Started{}, unavailable()
	}
	var occupied bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM repomesh_access.attempts WHERE binding=$1 AND id=$2)`, binding, command.ID).Scan(&occupied); err != nil {
		return Started{}, unavailable()
	}
	if occupied {
		_ = tx.Rollback(ctx)
		result, _, err := s.replayStart(ctx, binding, command)
		return Started{Result: result}, err
	}
	var actor *string
	var githubID *int64
	var expectedRevision *string
	if command.Purpose == "reconnect" {
		var active bool
		if err = tx.QueryRow(ctx, `SELECT NOT revoked AND expires_at>now() AND last_active_at>now()-interval '30 minutes' FROM repomesh_access.sessions WHERE hash=$1 FOR UPDATE`, digest(command.SessionCookie)).Scan(&active); err != nil {
			return Started{}, unavailable()
		}
		if !active || identity != current.Generation {
			return Started{}, failure(401, "AUTHENTICATION_REQUIRED")
		}
		actor = &current.User.ID
		githubID = &current.GitHubID
		if _, err = tx.Exec(ctx, `SELECT id FROM repomesh_access.accounts WHERE id=$1 FOR UPDATE`, *actor); err != nil {
			return Started{}, unavailable()
		}
		if err = tx.QueryRow(ctx, `SELECT revision FROM repomesh_access.connections WHERE actor=$1`, *actor).Scan(&expectedRevision); err != nil {
			return Started{}, unavailable()
		}
	}
	var browserCount, accountCount int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM repomesh_access.attempts WHERE binding=$1 AND created_at>now()-interval '1 minute'`, binding).Scan(&browserCount); err != nil {
		return Started{}, unavailable()
	}
	if actor != nil {
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM repomesh_access.attempts WHERE actor=$1 AND created_at>now()-interval '1 minute'`, *actor).Scan(&accountCount); err != nil {
			return Started{}, unavailable()
		}
	}
	if browserCount >= 5 || accountCount >= 10 {
		return Started{}, &Failure{Status: 429, Code: "RATE_LIMITED", RetryAfter: 60}
	}
	if _, err = tx.Exec(ctx, `UPDATE repomesh_access.attempts SET state='superseded',reason='NEWER_ATTEMPT',observed_at=now() WHERE binding=$1 AND state IN ('pending','exchanging','identity_pending','unknown')`, binding); err != nil {
		return Started{}, unavailable()
	}
	if _, err = tx.Exec(ctx, `UPDATE repomesh_access.bindings SET attempt_generation=attempt_generation+1 WHERE hash=$1`, binding); err != nil {
		return Started{}, unavailable()
	}
	var expires time.Time
	err = tx.QueryRow(ctx, `INSERT INTO repomesh_access.attempts(binding,id,purpose,actor,original_actor,expected_github_id,identity_generation,attempt_generation,destination,state_hash,material_ref,state,expires_at,expected_connection_revision)
	VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10,$11,'pending',now()+interval '10 minutes',$12) RETURNING expires_at`, binding, command.ID, command.Purpose, actor, original, githubID, identity, attempt+1, command.Destination.canonical, digest(material.State), string(ref), expectedRevision).Scan(&expires)
	if err != nil {
		return Started{}, unavailable()
	}
	keep = true
	if tx.Commit(ctx) != nil {
		return Started{}, unavailable()
	}
	result := Started{Result: s.startResult(command.ID, expires, material), Created: true}
	if newBinding {
		result.BindingCookie = bindingCookie
	}
	return result, nil
}

func (s *Service) replayStart(ctx context.Context, binding string, command StartCommand) (StartResult, bool, error) {
	var purpose, destination, ref, state string
	var expires time.Time
	var actor *string
	err := s.pool.QueryRow(ctx, `SELECT purpose,destination::text,material_ref,state,expires_at,actor FROM repomesh_access.attempts WHERE binding=$1 AND id=$2`, binding, command.ID).Scan(&purpose, &destination, &ref, &state, &expires, &actor)
	if errors.Is(err, pgx.ErrNoRows) {
		return StartResult{}, false, nil
	}
	if err != nil {
		return StartResult{}, false, unavailable()
	}
	parsed, err := ParseDestination([]byte(destination))
	if err != nil {
		return StartResult{}, true, unavailable()
	}
	if purpose != command.Purpose || parsed.canonical != command.Destination.canonical {
		return StartResult{}, true, failure(409, "IDEMPOTENCY_CONFLICT")
	}
	if command.Purpose == "reconnect" {
		current, err := s.Session(ctx, command.SessionCookie)
		if err != nil {
			return StartResult{}, true, err
		}
		if actor == nil || current.User.ID != *actor {
			return StartResult{}, true, failure(404, "RESOURCE_NOT_FOUND")
		}
	}
	if state != "pending" || !expires.After(time.Now()) || ref == "" {
		return StartResult{}, true, failure(409, "ATTEMPT_IN_PROGRESS")
	}
	plain, err := s.secrets.Open(ctx, secrets.VersionID(ref), attemptOwner(binding, command.ID), secrets.Purpose("auth-material"))
	if err != nil {
		return StartResult{}, true, unavailable()
	}
	var material exchangeMaterial
	if json.Unmarshal(plain, &material) != nil {
		return StartResult{}, true, unavailable()
	}
	return s.startResult(command.ID, expires, material), true, nil
}

func (s *Service) startResult(id string, expires time.Time, material exchangeMaterial) StartResult {
	hash := sha256.Sum256([]byte(material.Verifier))
	return StartResult{AttemptID: id, AuthorizationURL: s.provider.AuthorizationURL(material.State, base64.RawURLEncoding.EncodeToString(hash[:])), ExpiresAt: expires, ResultPage: "/auth/result/" + id}
}
