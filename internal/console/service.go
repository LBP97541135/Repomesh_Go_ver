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

func (s *Service) Organizations(ctx context.Context) (OrganizationsResponse, error) {
	rows, err := s.pool.Query(ctx, `SELECT o.id::text, COALESCE(o.name,''), o.created_at,
		(SELECT count(*) FROM public.agents a WHERE a.organization_id=o.id)
		FROM public.organizations o ORDER BY o.created_at DESC LIMIT 200`)
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

func (s *Service) Agents(ctx context.Context, withRuntime bool) (AgentsResponse, error) {
	rows, err := s.pool.Query(ctx, `SELECT a.id::text, a.organization_id::text, a.role, a.status,
		COALESCE(a.resource_ref->>'name', a.resource_ref->>'resource_name', ''),
		a.parent_agent_id::text, a.repository_id, NULL,
		a.responsibility_paths,
		t.id::text, t.id::text,
		0
		FROM public.agents a
		LEFT JOIN public.agent_teams t ON t.leader_agent_id=a.id OR t.manager_agent_id=a.id OR t.worker_agent_ids ? a.id::text
		ORDER BY a.role, a.id LIMIT 500`)
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
	TeamID               string          `json:"team_id"`
	AgentteamsTeamName   string          `json:"agentteams_team_name"`
	IssueID              string          `json:"issue_id"`
	RepositoryID         string          `json:"repository_id"`
	RepositoryName       *string         `json:"repository_name"`
	RuntimeStatus        string          `json:"runtime_status"`
	DecompositionMode    string          `json:"decomposition_mode"`
	TeamRoomID           *string         `json:"team_room_id"`
	LeaderRoomID         *string         `json:"leader_room_id"`
	Leader               Member          `json:"leader"`
	Workers              []Member        `json:"workers"`
	Runtime              *map[string]any `json:"runtime"`
}

type TeamsResponse struct {
	Teams []Team `json:"teams"`
}

func (s *Service) Teams(ctx context.Context, withRuntime bool) (TeamsResponse, error) {
	rows, err := s.pool.Query(ctx, `SELECT t.id::text, COALESCE(t.team_name,''), ''::text, COALESCE(t.repository_id,''),
		NULL, t.runtime_status,
		CASE WHEN t.execution_mode='leader' THEN 'leader' ELSE 'server' END,
		t.room_id, NULL,
		t.leader_agent_id::text, COALESCE(l.resource_ref->>'name', ''), 'repository_leader',
		COALESCE(t.worker_agent_ids, '[]'::jsonb)
		FROM public.agent_teams t
		LEFT JOIN public.agents l ON l.id=t.leader_agent_id
		ORDER BY t.id LIMIT 500`)
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

func (s *Service) Repositories(ctx context.Context) (RepositoriesResponse, error) {
	// repomesh_projects.repositories 没有组织归属列（组织在扫描目录
	// repomesh_scan.repositories 维护，两表 id 空间不同不可 join）——
	// 这里投影 NULL，消费方按「无组织」处理。
	rows, err := s.pool.Query(ctx, `SELECT id::text, name, NULL::text
		FROM repomesh_projects.repositories ORDER BY id LIMIT 500`)
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
