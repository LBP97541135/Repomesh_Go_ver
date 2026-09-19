package assembly

import (
	"context"
	"strconv"
	"strings"
)

// AgentRosterRow is one agents-table entry projected for the roster
// endpoint (doc §3.3 field names, as-built column set of 0009).
type AgentRosterRow struct {
	ID                  string         `json:"id"`
	OrganizationID      string         `json:"organizationId"`
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
	where := []string{"TRUE"}
	// 注意：where 已不含任何占位符，**args 必须为空**——留着 actor 会让 pgx 报
	// "bind message supplies 1 parameters, but prepared statement requires 0"（500）。
	// 这是我上一版改 where 时的疏漏，实测踩到。
	args := []any{}
	_ = actor
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
	rows, err := s.pool.Query(ctx, `SELECT a.id::text, a.organization_id::text, a.role,
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
		if err := rows.Scan(&row.ID, &row.OrganizationID, &row.Role, &row.ParentAgentID,
			&row.RepositoryID, &row.ResponsibilityPaths, &row.ResourceRef,
			&row.SingletonKey, &row.Status, &row.Prompt, &row.CLIKind); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
