package discovery

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// validateRepositories also protects the membership rows until the plan or
// tasks commit. A historical candidate or a user adjustment is not authority.
func validateRepositories(ctx context.Context, tx pgx.Tx, st *State, names []string) error {
	rows, err := tx.Query(ctx, `SELECT r.owner || '/' || r.name
		FROM repomesh_issues.issue_repository_scope scope
		JOIN repomesh_projects.project_repositories pr
		  ON pr.project_id=scope.project_id AND pr.repository_id=scope.repository_id
		JOIN repomesh_projects.repositories r ON r.id=pr.repository_id
		WHERE scope.project_id=$1 AND scope.issue_id=$2
		FOR KEY SHARE OF scope, pr`, st.ProjectID, st.IssueID)
	if err != nil {
		return err
	}
	allowed := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		allowed[name] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, name := range names {
		if !allowed[name] || seen[name] {
			return fmt.Errorf("%w: 仓库不在此 Issue 已确认的项目范围内，请重新确认仓库范围", ErrConflict)
		}
		seen[name] = true
	}
	return nil
}

func selectedTierNames(tiers []any) ([]string, error) {
	names := []string{}
	for _, value := range tiers {
		tier, ok := value.(map[string]any)
		if !ok {
			return nil, ErrConflict
		}
		name, _ := tier["repository"].(string)
		switch tier["tier"] {
		case "required", "maybe":
			names = append(names, name)
		case "excluded":
		default:
			return nil, ErrConflict
		}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("%w：请把至少一个仓库调整为「必需」或「可能」后再确认", ErrNoRepositories)
	}
	return names, nil
}
