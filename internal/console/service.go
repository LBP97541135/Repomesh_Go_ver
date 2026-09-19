// Package console implements the v0.2 console directory read surface:
// organizations, teams, agents (the roster the Teams/Agents pages render).
// One SQL view per response, over the 0009 tables; runtime blocks stay null
// (honest "not wired" per contract §4.3 — awake/uptime have no data source).
package console

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Service reads the console directory.
type Service struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

// ---- organizations ----

type Organization struct {
	OrganizationID string    `json:"organization_id"`
	Name           string    `json:"name"`
	CreatedAt      time.Time `json:"created_at"`
	AgentCount     int64     `json:"agent_count"`
}

type OrganizationsResponse struct {
	Organizations []Organization `json:"organizations"`
}

// Organizations 只返回**调用者自己所属的那个空间**。
//
// 2026-09-19 账号隔离：此前 `FROM public.organizations` 全库返回，公有部署
// （一账号一空间）下任何登录账号都能看到别人的空间名与智能体数量。
func (s *Service) Organizations(ctx context.Context, actor string) (OrganizationsResponse, error) {
	rows, err := s.pool.Query(ctx, `SELECT o.id::text, COALESCE(o.name,''), o.created_at,
		(SELECT count(*) FROM public.agents a WHERE a.organization_id=o.id)
		FROM public.organizations o
		WHERE o.id = (SELECT organization_id FROM repomesh_access.accounts WHERE id=$1)
		ORDER BY o.created_at DESC LIMIT 200`, actor)
	if err != nil {
		return OrganizationsResponse{}, fmt.Errorf("console: orgs: %w", err)
	}
	defer rows.Close()
	out := OrganizationsResponse{Organizations: []Organization{}}
	for rows.Next() {
		var org Organization
		if err := rows.Scan(&org.OrganizationID, &org.Name, &org.CreatedAt, &org.AgentCount); err != nil {
			return OrganizationsResponse{}, err
		}
		out.Organizations = append(out.Organizations, org)
	}
	return out, rows.Err()
}

// ---- agents ----

type Agent struct {
	AgentID               string          `json:"agent_id"`
	OrganizationID        string          `json:"organization_id"`
	Role                  string          `json:"role"`
	Status                string          `json:"status"`
	AgentteamsResourceRef string          `json:"agentteams_resource_name"`
	LeaderAgentID         *string         `json:"leader_agent_id"`
	RepositoryID          *string         `json:"repository_id"`
	RepositoryName        *string         `json:"repository_name"`
	ResponsibilityPaths   []string        `json:"responsibility_paths"`
	TeamID                *string         `json:"team_id"`
	IssueID               *string         `json:"issue_id"`
	ActiveTaskCount       int64           `json:"active_task_count"`
	Runtime               *map[string]any `json:"runtime"`
}

type AgentsResponse struct {
	Agents []Agent `json:"agents"`
}

// ── 平台就绪检查（2026-09-19 补）────────────────────────────────────────────
// 前端 ConsoleShell / SettingsPage / SetupWizardPage 一直在打 GET /api/setup/status，
// 而 Go 后端从未实现这条路由 → 404 → setupReady 恒 false（装机向导与设置页的
// 「就绪」判定永远不通过）。这里按**真实状态**补齐九项检查。
//
// 与 Python 原型的一处**有意偏离**：原型只取「前五项」参与 ready（其中含
// agentteams / matrix 两个依赖外部控制面的项），在当前部署下恒 false。这里改为
// 取**真正决定能否建项目**的四项，并在 next_actions 里如实列出未通过项。
type SetupCounts struct {
	Accounts     int `json:"accounts"`
	Agents       int `json:"agents"`
	Repositories int `json:"repositories"`
}

type SetupStatusView struct {
	ReadyForProjectCreation bool             `json:"ready_for_project_creation"`
	Checks                  map[string]bool  `json:"checks"`
	Dependencies            []map[string]any `json:"dependencies"`
	Counts                  SetupCounts      `json:"counts"`
	NextActions             []string         `json:"next_actions"`
}

// SetupStatus 九项检查全部按真实状态算；算不出来的如实 false，绝不编。
func (s *Service) SetupStatus(ctx context.Context) (SetupStatusView, error) {
	view := SetupStatusView{
		Checks:       map[string]bool{},
		Dependencies: []map[string]any{},
		NextActions:  []string{},
	}
	var providers, appCreds, admins, repos, accounts, agents int
	if err := s.pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM repomesh_models.providers),
		(SELECT count(*) FROM repomesh_access.app_credentials),
		(SELECT count(*) FROM repomesh_access.accounts WHERE is_admin),
		(SELECT count(*) FROM repomesh_scan.repositories),
		(SELECT count(*) FROM repomesh_access.accounts),
		(SELECT count(*) FROM public.agents)`).
		Scan(&providers, &appCreds, &admins, &repos, &accounts, &agents); err != nil {
		return SetupStatusView{}, fmt.Errorf("console: setup status: %w", err)
	}
	view.Counts = SetupCounts{Accounts: accounts, Agents: agents, Repositories: repos}
	view.Checks["database"] = true
	view.Checks["model"] = providers > 0
	view.Checks["github_app"] = appCreds > 0
	view.Checks["administrator"] = admins > 0
	view.Checks["repositories"] = repos > 0
	view.Checks["agent_directory"] = agents > 0
	// 这两项依赖外部控制面（AgentTeams Controller / Matrix 消息面），当前部署未接，
	// 如实为 false；它们不参与 ready 判定。
	view.Checks["agentteams"] = false
	view.Checks["matrix"] = false
	view.Checks["internal_auth"] = appCreds > 0
	view.ReadyForProjectCreation = view.Checks["database"] && view.Checks["github_app"] &&
		view.Checks["administrator"] && view.Checks["repositories"]
	for _, name := range []string{"database", "model", "github_app", "administrator", "repositories", "agent_directory"} {
		if !view.Checks[name] {
			view.NextActions = append(view.NextActions, name)
		}
	}
	return view, nil
}

// Agents 只返回调用者所属空间里的智能体（此前全库返回）。
func (s *Service) Agents(ctx context.Context, actor string, withRuntime bool) (AgentsResponse, error) {
	rows, err := s.pool.Query(ctx, `SELECT a.id::text, a.organization_id::text, a.role, a.status,
		COALESCE(a.resource_ref->>'name', a.resource_ref->>'resource_name', ''),
		a.parent_agent_id::text, a.repository_id, NULL,
		a.responsibility_paths,
		-- 2026-09-19 修正：这里原本是 t.id::text 连写两遍——团队 id 被选了两遍，
		-- 第二列当成了 issue_id，于是控制台把**团队 id 显示成 issue**（title 写着
		-- "issue 416fdc25-…"，而那是 agent_teams 的行 id）。agents 表本就没有 issue
		-- 关联，这一列如实为 NULL；团队归属由 TeamID 那一列承担。
		t.id::text, NULL,
		0
		FROM public.agents a
		LEFT JOIN public.agent_teams t ON t.leader_agent_id=a.id OR t.manager_agent_id=a.id OR t.worker_agent_ids ? a.id::text
		WHERE a.organization_id = (SELECT organization_id FROM repomesh_access.accounts WHERE id=$1)
		ORDER BY a.role, a.id LIMIT 500`, actor)
	if err != nil {
		return AgentsResponse{}, fmt.Errorf("console: agents: %w", err)
	}
	defer rows.Close()
	out := AgentsResponse{Agents: []Agent{}}
	for rows.Next() {
		var agent Agent
		var paths []byte
		if err := rows.Scan(&agent.AgentID, &agent.OrganizationID, &agent.Role, &agent.Status,
			&agent.AgentteamsResourceRef, &agent.LeaderAgentID, &agent.RepositoryID, &agent.RepositoryName,
			&paths, &agent.TeamID, &agent.IssueID, &agent.ActiveTaskCount); err != nil {
			return AgentsResponse{}, err
		}
		agent.ResponsibilityPaths = []string{}
		if len(paths) > 0 {
			_ = json.Unmarshal(paths, &agent.ResponsibilityPaths)
		}
		agent.Runtime = nil
		out.Agents = append(out.Agents, agent)
	}
	return out, rows.Err()
}

// ---- teams ----

type Member struct {
	AgentID string  `json:"agent_id"`
	Name    *string `json:"name"`
	Role    string  `json:"role"`
}

type Team struct {
	TeamID             string          `json:"team_id"`
	AgentteamsTeamName string          `json:"agentteams_team_name"`
	IssueID            string          `json:"issue_id"`
	RepositoryID       string          `json:"repository_id"`
	RepositoryName     *string         `json:"repository_name"`
	RuntimeStatus      string          `json:"runtime_status"`
	DecompositionMode  string          `json:"decomposition_mode"`
	TeamRoomID         *string         `json:"team_room_id"`
	LeaderRoomID       *string         `json:"leader_room_id"`
	Leader             Member          `json:"leader"`
	Workers            []Member        `json:"workers"`
	Runtime            *map[string]any `json:"runtime"`
}

type TeamsResponse struct {
	Teams []Team `json:"teams"`
}

// Teams 只返回挂在**调用者自己项目**上的团队（此前全库返回）。
func (s *Service) Teams(ctx context.Context, actor string, withRuntime bool) (TeamsResponse, error) {
	rows, err := s.pool.Query(ctx, `SELECT t.id::text, COALESCE(t.team_name,''), ''::text, COALESCE(t.repository_id,''),
		NULL, t.runtime_status,
		CASE WHEN t.execution_mode='leader' THEN 'leader' ELSE 'server' END,
		t.room_id, NULL,
		t.leader_agent_id::text, COALESCE(l.resource_ref->>'name', ''), 'repository_leader',
		COALESCE(t.worker_agent_ids, '[]'::jsonb)
		FROM public.agent_teams t
		LEFT JOIN public.agents l ON l.id=t.leader_agent_id
		WHERE EXISTS (SELECT 1 FROM repomesh_projects.projects p
			-- 2026-09-20 现网实测：这里是 **text = uuid**，查询必然报错
			-- （"console: teams: ERROR: operator does not exist: text = uuid"）——
			-- repomesh_projects.projects.id 是 text，而 public.agent_teams.project_id
			-- 是 uuid。同一批 JOIN 里其它比较（project_repositories.project_id、
			-- issues.project_id）两边都是 text，所以只有这一处会炸。
			WHERE p.id=t.project_id::text
			  AND p.organization_id = (SELECT organization_id FROM repomesh_access.accounts WHERE id=$1))
		ORDER BY t.id LIMIT 500`, actor)
	if err != nil {
		return TeamsResponse{}, fmt.Errorf("console: teams: %w", err)
	}
	defer rows.Close()
	out := TeamsResponse{Teams: []Team{}}
	for rows.Next() {
		var team Team
		var leaderID, leaderName, leaderRole string
		var workersJSON []byte
		if err := rows.Scan(&team.TeamID, &team.AgentteamsTeamName, &team.IssueID, &team.RepositoryID,
			&team.RepositoryName, &team.RuntimeStatus, &team.DecompositionMode,
			&team.TeamRoomID, &team.LeaderRoomID,
			&leaderID, &leaderName, &leaderRole, &workersJSON); err != nil {
			return TeamsResponse{}, err
		}
		team.Leader = Member{AgentID: leaderID, Name: &leaderName, Role: leaderRole}
		team.Workers = []Member{}
		var workerIDs []string
		if json.Unmarshal(workersJSON, &workerIDs) == nil {
			for _, workerID := range workerIDs {
				name := workerID
				team.Workers = append(team.Workers, Member{AgentID: workerID, Name: &name, Role: "worker"})
			}
		}
		team.Runtime = nil
		out.Teams = append(out.Teams, team)
	}
	return out, rows.Err()
}

// ---- repositories (catalog view) ----

type Repository struct {
	RepositoryID   string  `json:"repository_id"`
	Name           string  `json:"name"`
	OrganizationID *string `json:"organization_id"`
}

type RepositoriesResponse struct {
	Repositories []Repository `json:"repositories"`
}

// Repositories 只返回**调用者自己的项目引用到的仓库**（此前是全库目录）。
func (s *Service) Repositories(ctx context.Context, actor string) (RepositoriesResponse, error) {
	// repomesh_projects.repositories 没有组织归属列（组织在扫描目录
	// repomesh_scan.repositories 维护，两表 id 空间不同不可 join）——
	// 这里投影 NULL，消费方按「无组织」处理；可见性靠"本项目引用过"来裁剪。
	rows, err := s.pool.Query(ctx, `SELECT id::text, name, NULL::text
		FROM repomesh_projects.repositories
		WHERE id IN (SELECT pr.repository_id FROM repomesh_projects.project_repositories pr
			JOIN repomesh_projects.projects p ON p.id = pr.project_id
			WHERE p.owner = $1)
		ORDER BY id LIMIT 500`, actor)
	if err != nil {
		return RepositoriesResponse{}, fmt.Errorf("console: repositories: %w", err)
	}
	defer rows.Close()
	out := RepositoriesResponse{Repositories: []Repository{}}
	for rows.Next() {
		var repo Repository
		if err := rows.Scan(&repo.RepositoryID, &repo.Name, &repo.OrganizationID); err != nil {
			return RepositoriesResponse{}, err
		}
		out.Repositories = append(out.Repositories, repo)
	}
	return out, rows.Err()
}
