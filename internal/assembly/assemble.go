package assembly

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// AssemblyCommand requests the topology for one organization.
type AssemblyCommand struct {
	OrganizationID string
	// ProjectID：团队行（public.agent_teams.project_id NOT NULL）需要的项目作用域。
	// 2026-09-19 补：此前 assembly 只建人（public.agents）不组队——整个 Go 后端
	// 对 agent_teams 只有读、没有写，所以团队页恒为 0、任务没有队伍可指派。
	ProjectID      string
	Repositories   []string // repository ids from the scan
	WorkersPerRepo int
	LeaderName     string
}

// AssemblyResult reports the created role identities.
type AssemblyResult struct {
	LeaderAgentID string   `json:"leaderAgentId"`
	Managers      []string `json:"managers"`
	Workers       []string `json:"workers"`
	TeamRooms     []string `json:"teamRooms"`
}

// Assemble provisions the leader (org singleton), then one manager + N
// workers per repository. The leader-never-a-worker invariant is enforced by
// construction: leader identity is created once and never listed as worker.
func (s *Service) Assemble(ctx context.Context, command AssemblyCommand) (AssemblyResult, error) {
	if command.OrganizationID == "" || len(command.Repositories) == 0 || command.LeaderName == "" {
		return AssemblyResult{}, fmt.Errorf("assembly: organization, repositories and leader name are required")
	}
	if command.WorkersPerRepo < 1 {
		command.WorkersPerRepo = 1
	}
	if command.WorkersPerRepo > 8 {
		command.WorkersPerRepo = 8
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return AssemblyResult{}, fmt.Errorf("assembly: database unavailable: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	leaderID, err := s.ensureAgent(ctx, tx, command.OrganizationID, "leader", "", command.LeaderName)
	if err != nil {
		return AssemblyResult{}, err
	}
	result := AssemblyResult{LeaderAgentID: leaderID}
	for _, repositoryID := range command.Repositories {
		managerName := "mgr-" + shortName(repositoryID)
		managerID, err := s.ensureAgent(ctx, tx, command.OrganizationID, "manager", repositoryID, managerName)
		if err != nil {
			return AssemblyResult{}, err
		}
		result.Managers = append(result.Managers, managerID)
		var workerNames []string
		var workerIDs []string
		for workerIndex := 0; workerIndex < command.WorkersPerRepo; workerIndex++ {
			workerName := fmt.Sprintf("wrk-%s-%d", shortName(repositoryID), workerIndex)
			// 旧代码把返回的 agent id 丢掉了，只留名字；而团队行要的是 **id 列表**，
			// 所以这里必须收住 id，否则组队只能填名字（类型也不对）。
			workerID, err := s.ensureAgent(ctx, tx, command.OrganizationID, "worker", repositoryID, workerName)
			if err != nil {
				return AssemblyResult{}, err
			}
			workerIDs = append(workerIDs, workerID)
			workerNames = append(workerNames, workerName)
		}
		roomID := ""
		if s.provisioner != nil {
			roomRef, err := s.provisioner.ProvisionTeam(ctx, repositoryID, managerName, workerNames)
			if err != nil {
				return AssemblyResult{}, fmt.Errorf("assembly: remote provisioning failed for %s: %w", repositoryID, err)
			}
			if roomRef != "" {
				roomID = roomRef
				result.TeamRooms = append(result.TeamRooms, roomRef)
			}
		}
		// 组队：把这次建出的人登记成一支团队（一仓一队），按 project×repository 幂等。
		if command.ProjectID != "" {
			if err := s.ensureTeam(ctx, tx, command.ProjectID, repositoryID, leaderID, managerID, workerIDs, roomID); err != nil {
				return AssemblyResult{}, err
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return AssemblyResult{}, fmt.Errorf("assembly: commit failed: %w", err)
	}
	return result, nil
}

// ensureTeam 登记一支常驻团队（一仓一队）。此前 agent_teams 在整个后端**只有读、
// 没有写**——所以团队页永远 0 个、任务没有队伍可指派。按 idempotency_key 幂等，
// 重复建团只更新成员与房间，不产生第二支队伍。
func (s *Service) ensureTeam(ctx context.Context, tx pgxTx, projectID, repositoryID, leaderID, managerID string, workerIDs []string, roomID string) error {
	workerJSON, err := json.Marshal(workerIDs)
	if err != nil {
		return fmt.Errorf("assembly: team worker encode failed: %w", err)
	}
	teamID, err := newAgentUUID()
	if err != nil {
		return err
	}
	key := "assembly-team:" + projectID + ":" + repositoryID
	if _, err := tx.Exec(ctx, `INSERT INTO public.agent_teams
		(id, project_id, repository_id, leader_agent_id, manager_agent_id, worker_agent_ids,
		 team_name, room_id, execution_mode, runtime_status, idempotency_key)
		VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7,NULLIF($8,''),'leader','provisioned',$9)
		ON CONFLICT (idempotency_key) DO UPDATE SET
		  leader_agent_id=EXCLUDED.leader_agent_id,
		  manager_agent_id=EXCLUDED.manager_agent_id,
		  worker_agent_ids=EXCLUDED.worker_agent_ids,
		  team_name=EXCLUDED.team_name,
		  room_id=EXCLUDED.room_id,
		  runtime_status=EXCLUDED.runtime_status`,
		teamID, projectID, repositoryID, leaderID, managerID, string(workerJSON),
		"team-"+shortName(repositoryID), roomID, key); err != nil {
		return fmt.Errorf("assembly: team upsert failed: %w", err)
	}
	return nil
}

// ensureAgent inserts one agent identity keyed by its org+role+repository
// singleton; re-running the assembly is idempotent (singleton_key unique).
func (s *Service) ensureAgent(ctx context.Context, tx pgxTx, organizationID, role, repositoryID, name string) (string, error) {
	singleton := organizationID + ":" + role + ":" + repositoryID + ":" + name
	var id string
	err := tx.QueryRow(ctx, `SELECT id FROM public.agents WHERE singleton_key=$1`, singleton).Scan(&id)
	if err == nil {
		return id, nil
	}
	agentID, err := newAgentUUID()
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO public.agents (id, organization_id, role, repository_id, singleton_key, resource_ref)
		VALUES ($1,$2,$3,NULLIF($4,''),$5,'{}'::jsonb)`,
		agentID, organizationID, role, repositoryID, singleton); err != nil {
		return "", fmt.Errorf("assembly: agent insert failed: %w", err)
	}
	return agentID, nil
}

// CreateAgent 手动新建一个智能体（控制台智能体页的人工入口，2026-09-19 补）。
//
// 与 Assemble 的区别：Assemble 是"按仓库自动编制一队人"；这里是人**显式指名**
// 的一个成员，不参与自动编制，但仍写 singleton_key（org:role:repo:name）——
// 同名重放不建第二个人，与自动编制共用同一套幂等。
func (s *Service) CreateAgent(ctx context.Context, organizationID, role, repositoryID, name string) (string, error) {
	if organizationID == "" || name == "" {
		return "", fmt.Errorf("assembly: organization and name are required")
	}
	switch role {
	case "leader", "manager", "worker":
	default:
		return "", fmt.Errorf("assembly: role must be leader, manager or worker")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("assembly: database unavailable: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	agentID, err := s.ensureAgent(ctx, tx, organizationID, role, repositoryID, name)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("assembly: commit failed: %w", err)
	}
	return agentID, nil
}

// DeleteAgent 删除一个智能体（目录里的人工动作）。硬删：花名册与归属都直接少一行，
// 不做"停用"态——停用是另一个语义（保留归属、不再派工），要它得另开口子。
func (s *Service) DeleteAgent(ctx context.Context, agentID string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM public.agents WHERE id=$1`, agentID)
	if err != nil {
		return fmt.Errorf("assembly: agent delete failed: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// UpdateAgentProfile 设置一个智能体的预设提示词与 CLI 工具（迁移 0035，2026-09-19）。
// cliKind 空串 = 未指定 → 沿用项目级 agent_settings.agent_kind 与部署默认。
func (s *Service) UpdateAgentProfile(ctx context.Context, agentID, prompt, cliKind string) error {
	if len(prompt) > 20000 {
		return fmt.Errorf("assembly: prompt too long (max 20000)")
	}
	switch cliKind {
	case "", "codex_cli", "claude_cli":
	default:
		return fmt.Errorf("assembly: cli kind must be empty, codex_cli or claude_cli")
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE public.agents SET prompt=$2, cli_kind=$3 WHERE id=$1`, agentID, prompt, cliKind)
	if err != nil {
		return fmt.Errorf("assembly: agent profile update failed: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func shortName(repositoryID string) string {
	if len(repositoryID) > 12 {
		return repositoryID[len(repositoryID)-12:]
	}
	return repositoryID
}

// pgxTx is the transaction interface subset used by ensureAgent.
type pgxTx interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// TopologyRow is one agent identity in the project topology view.
type TopologyRow struct {
	ID           string `json:"id"`
	Role         string `json:"role"`
	RepositoryID string `json:"repositoryId,omitempty"`
	SingletonKey string `json:"singletonKey"`
}

// ListTopology returns the agent identities recorded for one project scope.
func (s *Service) ListTopology(ctx context.Context, projectID string) ([]TopologyRow, error) {
	rows, err := s.pool.Query(ctx, `SELECT id::text, role, COALESCE(repository_id,''), singleton_key
		FROM public.agents WHERE singleton_key LIKE '%:' || $1 || ':%' OR repository_id=$1
		ORDER BY role, singleton_key`, projectID)
	if err != nil {
		return nil, fmt.Errorf("assembly: topology query failed: %w", err)
	}
	defer rows.Close()
	result := []TopologyRow{}
	for rows.Next() {
		var row TopologyRow
		if rows.Scan(&row.ID, &row.Role, &row.RepositoryID, &row.SingletonKey) != nil {
			return nil, fmt.Errorf("assembly: topology scan failed")
		}
		result = append(result, row)
	}
	return result, rows.Err()
}
