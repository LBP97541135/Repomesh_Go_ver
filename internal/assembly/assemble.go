package assembly

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// AssemblyCommand requests the topology for one project.
//
// 2026-09-20（迁移 0053）：编制的作用域是**项目**，不是组织。组织只回答
// 「这是哪个账号的数据」（账号隔离，0037），不参与业务怎么分组；而组织与账号
// 当前 1:1，编制建在组织上会让同一账号下的两个项目挂同一个仓库时撞成同一个人（串）。
// 组织仍写进 agents.organization_id，但由项目反查得到，是**冗余租户戳**。
type AssemblyCommand struct {
	// ProjectID：编制的业务作用域（必填）。团队行（public.agent_teams.project_id
	// NOT NULL）与编制成员（public.agents.project_id）都以它为准。
	// 2026-09-19 补：此前 assembly 只建人（public.agents）不组队——整个 Go 后端
	// 对 agent_teams 只有读、没有写，所以团队页恒为 0、任务没有队伍可指派。
	ProjectID      string
	Repositories   []string // repository ids from the scan
	WorkersPerRepo int
}

// AssemblyResult reports the created role identities.
type AssemblyResult struct {
	LeaderAgentID string   `json:"leaderAgentId"`
	Managers      []string `json:"managers"`
	Workers       []string `json:"workers"`
	TeamRooms     []string `json:"teamRooms"`
}

// Assemble provisions the project leader, then one manager + N workers per
// repository. The leader-never-a-worker invariant is enforced by construction:
// leader identity is created once and never listed as worker.
//
// 总领导的名字由**项目**派生（leader-<项目 id 后 12 位>），不再由调用方传：
// 一个项目只能有一个总领导（ADR-0001 D02），让客户端决定名字就等于允许它
// 把同一个项目建出第二个总领导来。
func (s *Service) Assemble(ctx context.Context, command AssemblyCommand) (AssemblyResult, error) {
	if command.ProjectID == "" || len(command.Repositories) == 0 {
		return AssemblyResult{}, fmt.Errorf("assembly: project and repositories are required")
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
	// 组织只是冗余租户戳：由项目反查得到，不由调用方提供（0053）。
	organizationID, err := resolveOrganization(ctx, tx, command.ProjectID)
	if err != nil {
		return AssemblyResult{}, err
	}
	leaderID, err := s.ensureAgent(ctx, tx, command.ProjectID, organizationID, "leader", "",
		"leader-"+shortName(command.ProjectID))
	if err != nil {
		return AssemblyResult{}, err
	}
	result := AssemblyResult{LeaderAgentID: leaderID}
	for _, repositoryID := range command.Repositories {
		managerName := "mgr-" + shortName(repositoryID)
		managerID, err := s.ensureAgent(ctx, tx, command.ProjectID, organizationID, "manager", repositoryID, managerName)
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
			workerID, err := s.ensureAgent(ctx, tx, command.ProjectID, organizationID, "worker", repositoryID, workerName)
			if err != nil {
				return AssemblyResult{}, err
			}
			workerIDs = append(workerIDs, workerID)
			workerNames = append(workerNames, workerName)
			// 2026-09-20（回归测试抓到的旧漏）：此前只收进局部 workerIDs，**没有**写回
			// result.Workers —— 后端建了人，回执里却恒为空数组，前端建团弹窗因此
			// 永远显示「0 worker」。团队行要 id 列表，回执同样要。
			result.Workers = append(result.Workers, workerID)
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
		if err := s.ensureTeam(ctx, tx, command.ProjectID, repositoryID, leaderID, managerID, workerIDs, roomID); err != nil {
			return AssemblyResult{}, err
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

// resolveOrganization 从项目反查它所属的账号空间。
//
// 组织不再由调用方提供（0053）：它只是冗余租户戳，业务侧一律以项目为准。
// 项目不存在时返回错误——宁可失败，也不拿调用方给的组织 id 去建人。
func resolveOrganization(ctx context.Context, tx pgxTx, projectID string) (string, error) {
	var organizationID string
	err := tx.QueryRow(ctx,
		`SELECT organization_id::text FROM repomesh_projects.projects WHERE id::text=$1`,
		projectID).Scan(&organizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("assembly: project %q not found", projectID)
	}
	if err != nil {
		return "", fmt.Errorf("assembly: resolve project organization: %w", err)
	}
	return organizationID, nil
}

// ensureAgent 插入/复用一个编制成员。幂等键是 **(project_id, singleton_key)**，
// singleton_key = 角色:仓库:名字 —— **组织不出现在键里**（0053）。
//
// 2026-09-19 时这里按 (organization_id, singleton_key) 幂等，防的是"跨租户串人"；
// 但组织与账号 1:1，那道墙挡得住别的账号、挡不住同一账号下的第二个项目。
// 现在项目进键，同一仓库挂到两个项目上会各得一套人。
// organization_id 仍然写入，但只是租户戳。
func (s *Service) ensureAgent(ctx context.Context, tx pgxTx, projectID, organizationID, role, repositoryID, name string) (string, error) {
	singleton := role + ":" + repositoryID + ":" + name
	var id string
	err := tx.QueryRow(ctx,
		`SELECT id FROM public.agents WHERE project_id=$2::text AND singleton_key=$1`,
		singleton, projectID).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("assembly: agent lookup failed: %w", err)
	}
	agentID, err := newAgentUUID()
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO public.agents (id, organization_id, project_id, role, repository_id, singleton_key, resource_ref)
		VALUES ($1,$2::uuid,$3::text,$4,NULLIF($5,''),$6,'{}'::jsonb)`,
		agentID, organizationID, projectID, role, repositoryID, singleton); err != nil {
		return "", fmt.Errorf("assembly: agent insert failed: %w", err)
	}
	return agentID, nil
}

// CreateAgent 手动新建一个智能体（控制台智能体页的人工入口，2026-09-19 补）。
//
// 与 Assemble 的区别：Assemble 是"按仓库自动编制一队人"；这里是人**显式指名**
// 的一个成员，不参与自动编制，但仍写 singleton_key（角色:仓库:名字）并按
// **项目**幂等（0053）——同名重放不建第二个人，与自动编制共用同一套幂等。
func (s *Service) CreateAgent(ctx context.Context, projectID, role, repositoryID, name string) (string, error) {
	if projectID == "" || name == "" {
		return "", fmt.Errorf("assembly: project and name are required")
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
	organizationID, err := resolveOrganization(ctx, tx, projectID)
	if err != nil {
		return "", err
	}
	agentID, err := s.ensureAgent(ctx, tx, projectID, organizationID, role, repositoryID, name)
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
//
// 2026-09-20（0053）：改按 project_id 取。此前用 `singleton_key LIKE '%:<id>:%'`
// 猜组织前缀，那是因为项目不在键里；现在项目是真实列，直接判等。
func (s *Service) ListTopology(ctx context.Context, projectID string) ([]TopologyRow, error) {
	rows, err := s.pool.Query(ctx, `SELECT id::text, role, COALESCE(repository_id,''), COALESCE(singleton_key,'')
		FROM public.agents WHERE project_id=$1::text
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
