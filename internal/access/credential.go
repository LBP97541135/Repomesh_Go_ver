package access

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"repomesh.local/repomesh/internal/secrets"
)

type credential struct {
	actor, revision, token string
	epoch                  int64
	accessRef              string
}

func (s *Service) credential(ctx context.Context, actor string, canRefresh bool) (credential, error) {
	var c credential
	c.actor = actor
	var refreshRef, status, refreshState string
	var expires, refreshExpires *time.Time
	err := s.pool.QueryRow(ctx, `SELECT c.revision,c.access_epoch,c.access_ref,c.refresh_ref,c.access_expires_at,c.refresh_expires_at,c.status,c.refresh_state
	FROM repomesh_access.connections c JOIN repomesh_access.accounts a ON c.actor=a.id WHERE c.actor=$1 AND NOT a.disabled`, actor).Scan(&c.revision, &c.epoch, &c.accessRef, &refreshRef, &expires, &refreshExpires, &status, &refreshState)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, failure(503, "AUTHORIZATION_UNCONFIRMED")
	}
	if err != nil {
		return c, unavailable()
	}
	if status != "connected" || refreshState != "idle" {
		return c, failure(503, "AUTHORIZATION_UNCONFIRMED")
	}
	owner := secrets.Owner{Kind: "actor", ID: actor}
	if expires != nil && !expires.After(time.Now().Add(30*time.Second)) {
		if !canRefresh || refreshRef == "" || refreshExpires != nil && !refreshExpires.After(time.Now()) {
			return c, failure(503, "AUTHORIZATION_UNCONFIRMED")
		}
		command, err := s.pool.Exec(ctx, `UPDATE repomesh_access.connections SET refresh_state='claimed' WHERE actor=$1 AND revision=$2 AND access_epoch=$3 AND refresh_state='idle'`, actor, c.revision, c.epoch)
		if err != nil {
			return c, unavailable()
		}
		if command.RowsAffected() != 1 {
			return c, failure(503, "AUTHORIZATION_UNCONFIRMED")
		}
		plain, err := s.secrets.Open(ctx, secrets.VersionID(refreshRef), owner, secrets.Purpose("github-refresh-token"))
		if err != nil {
			s.refreshUnknown(ctx, c)
			return c, unavailable()
		}
		tokens, err := s.provider.Refresh(ctx, string(plain))
		if err != nil {
			s.refreshUnknown(ctx, c)
			if providerUnauthorized(err) {
				s.rejectCredential(ctx, c)
			}
			return c, failure(503, "AUTHORIZATION_UNCONFIRMED")
		}
		access, err := s.secrets.Seal(ctx, owner, secrets.Purpose("github-user-token"), []byte(tokens.AccessToken))
		if err != nil {
			s.refreshUnknown(ctx, c)
			return c, unavailable()
		}
		var refresh secrets.VersionID
		keep := false
		defer func() {
			if !keep {
				s.discard(access, refresh)
			}
		}()
		if tokens.RefreshToken != "" {
			refresh, err = s.secrets.Seal(ctx, owner, secrets.Purpose("github-refresh-token"), []byte(tokens.RefreshToken))
			if err != nil {
				s.refreshUnknown(ctx, c)
				return c, unavailable()
			}
		}
		revision := newID()
		command, err = s.pool.Exec(ctx, `UPDATE repomesh_access.connections SET revision=$4,access_epoch=access_epoch+1,access_ref=$5,refresh_ref=$6,
		access_expires_at=$7,refresh_expires_at=$8,status='connected',refresh_state='idle',observed_at=now(),credential_committed_at=clock_timestamp() WHERE actor=$1 AND revision=$2 AND access_epoch=$3 AND refresh_state='claimed'`, actor, c.revision, c.epoch, revision, string(access), string(refresh), tokens.AccessExpiresAt, tokens.RefreshExpiresAt)
		keep = err != nil || command.RowsAffected() == 1
		if err != nil || command.RowsAffected() != 1 {
			return c, unavailable()
		}
		c.revision = revision
		c.epoch++
		c.token = tokens.AccessToken
		return c, nil
	}
	plain, err := s.secrets.Open(ctx, secrets.VersionID(c.accessRef), owner, secrets.Purpose("github-user-token"))
	if err != nil {
		return c, failure(503, "AUTHORIZATION_UNCONFIRMED")
	}
	c.token = string(plain)
	return c, nil
}

func (s *Service) refreshUnknown(ctx context.Context, c credential) {
	_, _ = s.pool.Exec(ctx, `UPDATE repomesh_access.connections SET refresh_state='unknown',status='unknown',observed_at=now() WHERE actor=$1 AND revision=$2 AND access_epoch=$3 AND refresh_state='claimed'`, c.actor, c.revision, c.epoch)
}

// UserGitHubToken 返回该账号当前有效的 GitHub **用户**令牌（解封后的明文）。
//
// 用途：仓库扫描。公有部署下"扫我自己的仓库"应当**以登录用户本人的身份**去读——
// 配额 5000 次/小时（匿名只有 60），且天然按账号隔离（读别人的私有仓库 GitHub
// 直接 404）。这跟仓库发现（discovery）走的是同一套凭据体系。
//
// 令牌临期时会尝试刷新一次（canRefresh=true）；拿不到就返回错误，由调用方
// 决定是降级到部署级令牌还是匿名——**绝不假装拿到了**。
func (s *Service) UserGitHubToken(ctx context.Context, actor string) (string, error) {
	if actor == "" {
		return "", failure(401, "AUTHENTICATION_REQUIRED")
	}
	c, err := s.credential(ctx, actor, true)
	if err != nil {
		return "", err
	}
	if c.token == "" {
		return "", failure(503, "AUTHORIZATION_UNCONFIRMED")
	}
	return c.token, nil
}

// OrganizationOf 返回该账号所属空间（organization）的 id；没有归属时返回空串。
//
// 2026-09-19 账号隔离：技能库、扫描目录这类"共享目录"要按调用者的空间裁剪，
// 需要一个把 actor 解析成空间的统一入口。空串表示"没有空间"——调用方据此
// 只认全局共享内容，而不是退化成"看到全部"。
func (s *Service) OrganizationOf(ctx context.Context, actor string) (string, error) {
	if actor == "" {
		return "", nil
	}
	var organization *string
	err := s.pool.QueryRow(ctx, `SELECT organization_id::text FROM repomesh_access.accounts WHERE id=$1`, actor).Scan(&organization)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", unavailable()
	}
	if organization == nil {
		return "", nil
	}
	return *organization, nil
}

// IssueInOrganization 判断该 issue 是否挂在该账号名下的项目上。
//
// 2026-09-19 账号隔离：`/api/issues/{issueId}/...` 一族（发现链、归档、清理）
// 此前只认 issue id —— 拿到别人的 id 就能读它的发现状态、替它跑规划、甚至归档它。
// 归属规则与项目读面一致：**项目的 owner 必须是调用者**。
func (s *Service) IssueInOrganization(ctx context.Context, actor, issueID string) (bool, error) {
	if actor == "" || issueID == "" {
		return false, nil
	}
	var ok bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM repomesh_issues.issues i
			 JOIN repomesh_projects.projects p ON p.id = i.project_id
			 WHERE i.id = $1 AND p.owner = $2)`, issueID, actor).Scan(&ok)
	return ok, err
}

// AgentInOrganization 判断该智能体是否落在该账号的空间里。
//
// 2026-09-19 账号隔离：`DELETE/PATCH /api/agents/{id}` 此前只认 id；
// 而 `POST /api/agents` 更是**从请求体里取 organizationId** —— 想往哪个空间塞
// 就往哪个空间塞。写面必须只认调用者自己的空间。
func (s *Service) AgentInOrganization(ctx context.Context, actor, agentID string) (bool, error) {
	if actor == "" || agentID == "" {
		return false, nil
	}
	var ok bool
	// id::text 比较：调用者可能传垃圾字符串，那样会得到 22P02 而不是干净的"不是你的"。
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM public.agents g
			 JOIN repomesh_access.accounts a ON a.organization_id = g.organization_id
			 WHERE g.id::text = $1 AND a.id = $2)`, agentID, actor).Scan(&ok)
	return ok, err
}

func (s *Service) rejectCredential(ctx context.Context, c credential) {
	_, _ = s.pool.Exec(ctx, `UPDATE repomesh_access.connections SET status='missing',access_epoch=access_epoch+1,observed_at=now() WHERE actor=$1 AND revision=$2 AND access_epoch=$3`, c.actor, c.revision, c.epoch)
}
