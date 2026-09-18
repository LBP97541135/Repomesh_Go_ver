package discovery

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// RepositoryPlan serves the contract §5.4 repo-granularity plan paper as-built:
// the plan snapshot this issue's materialization receipt points at, with batch
// entries (repository names) resolved to catalog ids where possible. Unresolved
// names keep the node with a null repository_id — dropping nodes would break
// batch layout (§5.4 勘正). No plan / never materialized → pgx.ErrNoRows (the
// web layer maps it to 404; the capsule treats that as "no plan snapshot yet").
func (s *Service) RepositoryPlan(ctx context.Context, issueID, repositoryID string) (map[string]any, error) {
	var materialization []byte
	err := s.pool.QueryRow(ctx,
		`SELECT materialization FROM repomesh_issues.issue_discoveries WHERE issue_id=$1`, issueID).
		Scan(&materialization)
	if err != nil {
		return nil, err
	}
	var receipt struct {
		PlanID *string `json:"plan_id"`
	}
	if err := json.Unmarshal(materialization, &receipt); err != nil || receipt.PlanID == nil {
		// 收据在但没起来的计划没有 id 可指(§8.3 失败态恒 null)——同按无快照
		return nil, pgx.ErrNoRows
	}

	var versionText string
	var batchesRaw, dagRaw []byte
	err = s.pool.QueryRow(ctx,
		`SELECT plan_version, execution_batches, task_dag FROM public.plans WHERE id=$1::uuid`,
		*receipt.PlanID).Scan(&versionText, &batchesRaw, &dagRaw)
	if err != nil {
		return nil, err
	}
	var batches [][]string
	if err := json.Unmarshal(batchesRaw, &batches); err != nil {
		return nil, err
	}
	var dag map[string][]string
	_ = json.Unmarshal(dagRaw, &dag)

	// 批次里存的是仓库名;名字对目录(id)的解析尽力而为,解析不到节点保留 id=null
	nameToID := map[string]string{}
	rows, err := s.pool.Query(ctx, `SELECT id::text, name FROM repomesh_projects.repositories`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		if rows.Scan(&id, &name) == nil {
			nameToID[name] = id
		}
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}

	nodes := []map[string]any{}
	edges := []map[string]any{}
	for batchIndex, batch := range batches {
		for _, name := range batch {
			id, resolved := nameToID[name]
			nodes = append(nodes, map[string]any{
				"repository_id": idOrNull(resolved, id),
				"name":          name,
				"batch_index":   batchIndex,
				"is_focus":      resolved && id == repositoryID,
			})
		}
	}
	for from, tos := range dag {
		fromID, ok := nameToID[from]
		if !ok {
			continue // 解析不到的边丢弃(§5.4:丢弃只进日志,不进图)
		}
		for _, to := range tos {
			toID, ok := nameToID[to]
			if !ok {
				continue
			}
			edges = append(edges, map[string]any{"from_repository_id": fromID, "to_repository_id": toID})
		}
	}

	version := 1
	if v, err := strconv.Atoi(strings.TrimPrefix(versionText, "v")); err == nil {
		version = v
	}
	return map[string]any{
		"issue_id":      issueID,
		"repository_id": repositoryID,
		"plan_version":  version,
		"dag": map[string]any{
			"nodes":       nodes,
			"edges":       edges,
			"granularity": "repository",
			"edge_source": "task_dag.depends_on",
		},
		"execution_batches":    batches,
		"spec":                 nil,
		"engineering_contract": nil,
	}, nil
}

func idOrNull(resolved bool, id string) any {
	if !resolved {
		return nil
	}
	return id
}
