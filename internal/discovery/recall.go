package discovery

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"

	"repomesh.local/repomesh/internal/scan"
)

// repoCard 是召回阶段的一张仓库名片：登记信息 + **扫描生成的 AutoCard**。
//
// 2026-09-19 修正：此前候选阶段只读 description / topics / languages 三个人工
// 字段，扫描已经产出的 AutoCard（目录 / 依赖 / 构建身份 / 部署身份 / 最近提交 /
// 暴露的 API）完全没被使用——而 GOAI-infra-repomesh 的设计里，总 Manager 恰恰是
// 靠 AutoCard 做全局召回的。少了它，模型/关键词都只剩仓库名可比。
type repoCard struct {
	ID          string
	Name        string
	Description string
	Topics      []string
	Languages   []string
	AutoCard    *scan.AutoCard
	// InProject 标记该仓库是否已挂在本项目上：同分时优先。
	InProject bool
}

// loadRepoPool only reads repositories explicitly selected for this issue.
// Scan metadata must match the exact repository URL and the owner's space.
func (s *Service) loadRepoPool(ctx context.Context, tx pgx.Tx, projectID, issueID string) ([]repoCard, error) {
	const query = `
		SELECT r.id,
		       r.owner || '/' || r.name,
		       COALESCE(s.description, ''),
		       COALESCE(s.topics::text, '[]'),
		       COALESCE(s.languages::text, '[]'),
		       COALESCE(s.metadata::text, '{}'),
		       true AS in_project
		FROM repomesh_issues.issue_repository_scope scope
		JOIN repomesh_projects.project_repositories pr
		  ON pr.project_id=scope.project_id AND pr.repository_id=scope.repository_id
		JOIN repomesh_projects.repositories r ON r.id=pr.repository_id
		JOIN repomesh_projects.projects p ON p.id=pr.project_id
		JOIN repomesh_access.accounts a ON a.id=p.owner
		LEFT JOIN LATERAL (
		  SELECT scan.* FROM repomesh_scan.repositories scan
		  WHERE scan.organization_id=a.organization_id
		    AND lower(regexp_replace(rtrim(scan.url, '/'), '\.git$', '')) IN
		      (lower('https://' || r.host || '/' || r.owner || '/' || r.name),
		       lower('http://' || r.host || '/' || r.owner || '/' || r.name),
		       lower('git@' || r.host || ':' || r.owner || '/' || r.name))
		  ORDER BY scan.profiled_at DESC, scan.id LIMIT 1
		) s ON true
		WHERE scope.project_id=$1 AND scope.issue_id=$2
		ORDER BY r.id`
	rows, err := tx.Query(ctx, query, projectID, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cards := []repoCard{}
	for rows.Next() {
		var card repoCard
		var topicsRaw, languagesRaw, metadataRaw string
		if err := rows.Scan(&card.ID, &card.Name, &card.Description, &topicsRaw, &languagesRaw, &metadataRaw, &card.InProject); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(topicsRaw), &card.Topics)
		_ = json.Unmarshal([]byte(languagesRaw), &card.Languages)
		if strings.TrimSpace(metadataRaw) != "" && metadataRaw != "{}" {
			auto := &scan.AutoCard{}
			if err := json.Unmarshal([]byte(metadataRaw), auto); err == nil && autoHasContent(auto) {
				card.AutoCard = auto
			}
		}
		cards = append(cards, card)
	}
	return cards, rows.Err()
}

func autoHasContent(auto *scan.AutoCard) bool {
	if auto == nil {
		return false
	}
	return len(auto.TopDirs) > 0 || len(auto.Deps) > 0 || len(auto.Identities) > 0 ||
		len(auto.DeployIdentities) > 0 || len(auto.RecentCommits) > 0 || len(auto.ExposedAPIs) > 0
}

// cardText 把一张名片渲染成一段文本：LLM 与关键词路径共用同一份事实，
// 保证"模型看到的"和"关键词命中的"不是两套信息。
func cardText(card repoCard) string {
	parts := []string{"仓库 " + card.Name}
	if card.Description != "" {
		parts = append(parts, "描述："+card.Description)
	}
	if len(card.Topics) > 0 {
		parts = append(parts, "主题："+strings.Join(card.Topics, ", "))
	}
	if len(card.Languages) > 0 {
		parts = append(parts, "语言："+strings.Join(card.Languages, ", "))
	}
	if card.AutoCard != nil {
		auto := card.AutoCard
		if len(auto.TopDirs) > 0 {
			parts = append(parts, "目录："+strings.Join(cap(auto.TopDirs, 24), ", "))
		}
		if len(auto.Deps) > 0 {
			parts = append(parts, "依赖："+strings.Join(cap(auto.Deps, 24), ", "))
		}
		if len(auto.Identities) > 0 {
			parts = append(parts, "构建身份："+strings.Join(cap(auto.Identities, 8), ", "))
		}
		if len(auto.DeployIdentities) > 0 {
			parts = append(parts, "部署身份："+strings.Join(cap(auto.DeployIdentities, 8), ", "))
		}
		if len(auto.ExposedAPIs) > 0 {
			parts = append(parts, "暴露接口："+strings.Join(cap(auto.ExposedAPIs, 16), ", "))
		}
		if len(auto.RecentCommits) > 0 {
			parts = append(parts, "最近提交："+strings.Join(cap(auto.RecentCommits, 5), " | "))
		}
		if auto.LowSignal {
			parts = append(parts, "（扫描信号不足）")
		}
	}
	return strings.Join(parts, "；")
}

// haystack 是关键词路径的匹配面：把名片里所有可比较的文本拼在一起。
func haystack(card repoCard) string {
	return strings.ToLower(cardText(card))
}

func cap(values []string, limit int) []string {
	if len(values) <= limit {
		return values
	}
	return values[:limit]
}

// sortedNames 便于稳定输出。
func sortedNames(names []string) []string {
	out := append([]string(nil), names...)
	sort.Strings(out)
	return out
}

// toStrings 把状态文档里存的 []any 关键词转成 []string。
func toStrings(raw []any) []string {
	out := []string{}
	for _, item := range raw {
		if value, ok := item.(string); ok && value != "" {
			out = append(out, value)
		}
	}
	return out
}

// errorOrNil 让"没有错误"落成 JSON null（而不是空串），读面上一眼能分清。
func errorOrNil(message string) any {
	if message == "" {
		return nil
	}
	return message
}
