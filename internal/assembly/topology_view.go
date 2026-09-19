package assembly

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// 项目拓扑读面（前端 ProjectAgentTopologyView 逐字对应）。
//
// 为什么必须返回这个形状、而不是"agent 列表"：前端 `fetchProjectTopology` 用它的
// **有无**判定监管策略草稿窗口开不开（§3.4：拓扑一落地，草稿就定死了），并读
// execution_mode / required_checkpoints / human_grants 渲染只读段。此前这个端点回的是
// `{"items":[agents]}` —— 恒 200、字段全对不上，于是前端永远判成"档案已锁死"，
// 策略卡片只剩一个标题、连「配置」按钮都不出现：用户根本没有地方设卡点。
//
// 404 的语义（契约明文）：该需求还没有拓扑 = 监管策略尚未设定，不是错误。
type ProjectTopologyView struct {
	ID                   string             `json:"id"`
	OrganizationID       string             `json:"organization_id"`
	ProjectID            string             `json:"project_id"`
	OrganizationLeaderID string             `json:"organization_leader_id"`
	RepositoryTeams      []TopologyTeamView `json:"repository_teams"`
	ExecutionMode        string             `json:"execution_mode"`
	RequiredCheckpoints  []string           `json:"required_checkpoints"`
	HumanGrants          []json.RawMessage  `json:"human_grants"`
	OperationalStatus    string             `json:"operational_status"`
	// PolicyFrozen：监管策略是否已随首次物化定死（草稿的 frozen_at）。
	// 界面据此把「修改」换成只读——定死与否是存储层的事实，不是界面的判断。
	PolicyFrozen bool `json:"policy_frozen"`
}

// TopologyTeamView 是拓扑上的一支仓库团队。
type TopologyTeamView struct {
	ID                 string   `json:"id"`
	ProjectID          string   `json:"project_id"`
	RepositoryID       string   `json:"repository_id"`
	LeaderAgentID      string   `json:"leader_agent_id"`
	WorkerAgentIDs     []string `json:"worker_agent_ids"`
	AgentTeamsTeamName string   `json:"agentteams_team_name"`
	RuntimeStatus      string   `json:"runtime_status"`
	RoomID             *string  `json:"room_id"`
	LeaderRoomID       *string  `json:"leader_room_id"`
}

// ProjectTopology 组装一个项目的拓扑读面；**没有任何拓扑痕迹时返回 pgx.ErrNoRows
// （→404）**：草稿与团队都还没有，就是「尚未设定」。
//
// execution_mode / required_checkpoints / human_grants 取自**监管策略草稿**
// （public.project_policy_drafts，迁移 0040）——草稿是这三件事在这套实现里的唯一来源：
// agent_teams.execution_mode 那一列已被 assembly 占用成 'leader'/'server'，
// 两套语义不能叠在一格里（见 internal/humancontrol/policy.go 的说明）。
func (s *Service) ProjectTopology(ctx context.Context, projectID string) (ProjectTopologyView, error) {
	view := ProjectTopologyView{
		ProjectID:           projectID,
		RepositoryTeams:     []TopologyTeamView{},
		ExecutionMode:       "auto",
		RequiredCheckpoints: []string{},
		HumanGrants:         []json.RawMessage{},
		OperationalStatus:   "active",
	}
	var organizationID *string
	if err := s.pool.QueryRow(ctx, `SELECT id::text, organization_id::text, status
		 FROM repomesh_projects.projects WHERE id=$1::text`, projectID).
		Scan(&view.ID, &organizationID, &view.OperationalStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ProjectTopologyView{}, pgx.ErrNoRows
		}
		return ProjectTopologyView{}, fmt.Errorf("assembly: topology project lookup: %w", err)
	}
	if organizationID != nil {
		view.OrganizationID = *organizationID
		// 组织 leader：契约里的 organization_leader_id。没有就如实留空。
		_ = s.pool.QueryRow(ctx, `SELECT id::text FROM public.agents
			 WHERE organization_id=$1::uuid AND role='leader' ORDER BY id LIMIT 1`, *organizationID).
			Scan(&view.OrganizationLeaderID)
	}

	rows, err := s.pool.Query(ctx, `SELECT id::text, project_id::text, COALESCE(repository_id,''),
		 leader_agent_id::text, COALESCE(worker_agent_ids,'[]'::jsonb),
		 COALESCE(team_name,''), COALESCE(runtime_status,'idle'), room_id
		 FROM public.agent_teams WHERE project_id=$1::uuid ORDER BY repository_id, id`, projectID)
	if err != nil {
		return ProjectTopologyView{}, fmt.Errorf("assembly: topology teams: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var team TopologyTeamView
		var workers []byte
		if err := rows.Scan(&team.ID, &team.ProjectID, &team.RepositoryID, &team.LeaderAgentID,
			&workers, &team.AgentTeamsTeamName, &team.RuntimeStatus, &team.RoomID); err != nil {
			return ProjectTopologyView{}, fmt.Errorf("assembly: topology team scan: %w", err)
		}
		team.WorkerAgentIDs = []string{}
		if len(workers) > 0 {
			_ = json.Unmarshal(workers, &team.WorkerAgentIDs)
		}
		view.RepositoryTeams = append(view.RepositoryTeams, team)
	}
	if err := rows.Err(); err != nil {
		return ProjectTopologyView{}, fmt.Errorf("assembly: topology teams: %w", err)
	}

	// 草稿：没设过时保持默认（auto / 零卡点 / 零授权）——「没人决定过」在执行面上的
	// 真实形态就是全自动，这与前端「未设定」那句陈述同源。
	var mode string
	var checkpoints, grants []byte
	err = s.pool.QueryRow(ctx, `SELECT execution_mode, required_checkpoints, human_grants, frozen_at IS NOT NULL
		 FROM public.project_policy_drafts WHERE project_id=$1::uuid`, projectID).
		Scan(&mode, &checkpoints, &grants, &view.PolicyFrozen)
	switch {
	case err == nil:
		view.ExecutionMode = mode
		if len(checkpoints) > 0 {
			_ = json.Unmarshal(checkpoints, &view.RequiredCheckpoints)
		}
		if len(grants) > 0 {
			_ = json.Unmarshal(grants, &view.HumanGrants)
		}
	case errors.Is(err, pgx.ErrNoRows):
		// 未设定：保持默认。
	default:
		return ProjectTopologyView{}, fmt.Errorf("assembly: topology policy: %w", err)
	}

	// 既没有草稿、也没有任何团队 = 这个需求还没有拓扑 → 404（草稿窗口开着）。
	if len(view.RepositoryTeams) == 0 && view.ExecutionMode == "auto" &&
		len(view.RequiredCheckpoints) == 0 && len(view.HumanGrants) == 0 {
		var hasDraft bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM public.project_policy_drafts
			 WHERE project_id=$1::uuid)`, projectID).Scan(&hasDraft); err != nil {
			return ProjectTopologyView{}, fmt.Errorf("assembly: topology draft probe: %w", err)
		}
		if !hasDraft {
			return ProjectTopologyView{}, pgx.ErrNoRows
		}
	}
	return view, nil
}
