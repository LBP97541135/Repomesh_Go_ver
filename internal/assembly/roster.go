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
}

// Roster lists the agents of the calling user's organization (doc §3.3
// 权限=成员;org 由会话主体的 users 行解析),optionally filtered by role,
// repository and status. Writers live in Assemble (singleton_key 幂等)。
func (s *Service) Roster(ctx context.Context, actor, role, repositoryID, status string) ([]AgentRosterRow, error) {
	where := []string{"a.organization_id = (SELECT organization_id FROM public.users WHERE id=$1)"}
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
	rows, err := s.pool.Query(ctx, `SELECT a.id::text, a.organization_id::text, a.role,
		a.parent_agent_id::text, a.repository_id, a.responsibility_paths, a.resource_ref,
		a.singleton_key, a.status
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
			&row.SingletonKey, &row.Status); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
