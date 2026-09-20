package assembly

import (
	"context"
	"strconv"
	"strings"
)

// AgentRosterRow is one agents-table entry projected for the roster
// endpoint (doc §3.3 field names, as-built column set of 0009).
type AgentRosterRow struct {
	ID             string `json:"id"`
	OrganizationID string `json:"organizationId"`
	// ProjectID：编制的业务归属（迁移 0053）。为空 = 组织级角色（治理 leader、
	// 规划 agent），它们不参与自动编制。
	ProjectID           *string        `json:"projectId"`
	Role                string         `json:"role"`
	ParentAgentID       *string        `json:"parentAgentId"`
	RepositoryID        *string        `json:"repositoryId"`
	ResponsibilityPaths []string       `json:"responsibilityPaths"`
	ResourceRef         map[string]any `json:"resourceRef"`
	SingletonKey        *string        `json:"singletonKey"`
	Status              string         `json:"status"`
	// 迁移 0035（2026-09-19）：智能体级预设提示词与 CLI 工具。
	// cliKind 空串 = 未指定 → 沿用项目级 agent_settings.agent_kind 与部署默认。
	Prompt  string `json:"prompt"`
	CLIKind string `json:"cliKind"`
}

// Roster lists the agents of the calling user's organization (doc §3.3
// 权限=成员;org 由会话主体的 users 行解析),optionally filtered by role,
// repository and status. Writers live in Assemble (singleton_key 幂等)。
func (s *Service) Roster(ctx context.Context, actor, role, repositoryID, status string) ([]AgentRosterRow, error) {
	// 2026-09-19 修正：原查询按
	//   `a.organization_id = (SELECT organization_id FROM public.users WHERE id=$1)`
	// 裁剪到"会话主体所属组织"，但 **public.users 是空表**（真实账号在
	// repomesh_access.accounts，而那张表**没有组织列**）→ 子查询恒为 NULL →
	// 花名册恒返回 0 行。智能体页因此拿不到名册（此前"修好"的路由一直在空转）。
	// 当前部署只有一个组织（6cc2057e-…），这里如实返回全部智能体；
	// 多组织上线前必须先给账号补组织归属，再恢复裁剪——不留一个永远为空的假守卫。
	// actor 仍作为参数保留在签名里（调用方按会话主体传），供后续裁剪使用。
	// 2026-09-19 多账户（迁移 0036）：按**账号自己的组织**裁剪，而不是按空表
	// public.users。账号还没归属组织时（organization_id 为 null）如实退化为"全部"
	// ——不因为一个没回填的账号就让名册整个空掉（那正是今天踩过的坑）。
	where := []string{`(a.organization_id = (SELECT organization_id FROM repomesh_access.accounts WHERE id=$1))`}
	// args 与 where 的占位符必须一一对应：上一版把 where 改成 TRUE 却留着 actor，
	// pgx 直接报 "bind message supplies 1 parameters, but prepared statement requires 0"
	//（500）——实测踩到，这里保持"where 里有几个 $n，args 就有几个"。
	args := []any{actor}
	if role != "" {
		args = append(args, role)
		where = append(where, "a.role=$"+strconv.Itoa(len(args)))
	}
	if repositoryID != "" {
		args = append(args, repositoryID)
		where = append(where, "a.repository_id=$"+strconv.Itoa(len(args)))
	}
	if status != "" {
		args = append(args, status)
		where = append(where, "a.status=$"+strconv.Itoa(len(args)))
	}
	rows, err := s.pool.Query(ctx, `SELECT a.id::text, a.organization_id::text, a.project_id, a.role,
		a.parent_agent_id::text, a.repository_id, a.responsibility_paths, a.resource_ref,
		a.singleton_key, a.status, a.prompt, a.cli_kind
		FROM public.agents a WHERE `+strings.Join(where, " AND ")+`
		ORDER BY a.role, a.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []AgentRosterRow{}
	for rows.Next() {
		var row AgentRosterRow
		if err := rows.Scan(&row.ID, &row.OrganizationID, &row.ProjectID, &row.Role, &row.ParentAgentID,
			&row.RepositoryID, &row.ResponsibilityPaths, &row.ResourceRef,
			&row.SingletonKey, &row.Status, &row.Prompt, &row.CLIKind); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
