package discovery

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// decodePaperBatches 是 execution_batches 的 as-built 容错读取（计划纸这一侧）。
//
// 规范形状是 [][]string（每批一串仓库名）。2026-09-20 线上实测物化写出过
// [{"batch_no":1,"tasks":[{"repository":"owner/name"}]}]，读面按 [][]string 解不开，
// §5.4 计划纸与 GET /plans/{id}/tasks 一起 503。这里只认「批次 → 仓库名」，
// 认不出的条目跳过 —— 不编造批次划分。
func decodePaperBatches(raw []byte) ([][]string, error) {
	if len(raw) == 0 {
		return [][]string{}, nil
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, err
	}
	type richBatch struct {
		BatchNo int `json:"batch_no"`
		Tasks   []struct {
			Repository string `json:"repository"`
		} `json:"tasks"`
	}
	out := [][]string{}
	rich := []richBatch{}
	usedRich := false
	for _, entry := range entries {
		var names []string
		if err := json.Unmarshal(entry, &names); err == nil {
			out = append(out, names)
			continue
		}
		var batch richBatch
		if err := json.Unmarshal(entry, &batch); err != nil {
			continue
		}
		usedRich = true
		rich = append(rich, batch)
	}
	if usedRich {
		sort.SliceStable(rich, func(i, j int) bool { return rich[i].BatchNo < rich[j].BatchNo })
		for _, batch := range rich {
			repos := []string{}
			for _, task := range batch.Tasks {
				if strings.TrimSpace(task.Repository) != "" {
					repos = append(repos, task.Repository)
				}
			}
			if len(repos) > 0 {
				out = append(out, repos)
			}
		}
	}
	return out, nil
}

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
		`SELECT plan_version, execution_batches, task_dag FROM public.plans WHERE id=$1::uuid AND issue_id=$2`,
		*receipt.PlanID, issueID).Scan(&versionText, &batchesRaw, &dagRaw)
	if err != nil {
		return nil, err
	}
	var batches [][]string
	if err := json.Unmarshal(batchesRaw, &batches); err != nil {
		// 2026-09-20：execution_batches 的 as-built 容错（同 internal/tasks 的
		// decodePlanBatches）。物化曾经写出 [{"batch_no":1,"tasks":[{repository}]}]，
		// 按 [][]string 解不开 —— 这里只认「批次 → 仓库名」这一层语义。
		tolerant, tolerantErr := decodePaperBatches(batchesRaw)
		if tolerantErr != nil {
			return nil, err
		}
		batches = tolerant
	}
	var dag map[string][]string
	_ = json.Unmarshal(dagRaw, &dag)

	// 批次里存的是仓库名;名字对目录(id)的解析尽力而为,解析不到节点保留 id=null
	nameToID := map[string]string{}
	rows, err := s.pool.Query(ctx, `SELECT r.id, r.owner || '/' || r.name
		FROM repomesh_issues.issue_repository_scope scope
		JOIN repomesh_projects.repositories r ON r.id=scope.repository_id
		WHERE scope.issue_id=$1`, issueID)
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
	found := false
	for _, id := range nameToID {
		found = found || id == repositoryID
	}
	if !found {
		return nil, pgx.ErrNoRows
	}

	nodes := []map[string]any{}
	edges := []map[string]any{}
	for batchIndex, batch := range batches {
		for _, name := range batch {
			id, resolved := nameToID[name]
			if !resolved {
				return nil, ErrConflict
			}
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
