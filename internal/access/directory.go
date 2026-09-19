package access

import (
	"context"
	"fmt"
)

// AccountRow 是账号目录的一行（前端 auth.ts 的 Account 逐字对应）。
//
// `username` 在 Go 侧取的是 github_id 的字符串形式：控制台里账号的身份就是
// GitHub 账号，而本地账号体系（用户名密码）在 Go 后端没有落点（api-design.md
// 附录 E 第 8 条）—— 如实回 GitHub id，不编一个看起来像用户名的东西。
type AccountRow struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	IsAdmin     bool   `json:"is_admin"`
	Active      bool   `json:"active"`
}

// AccountDirectory 返回该账号所在空间里的账号（监管策略「选人」下拉的数据源）。
//
// 隔离规则与 console 目录一致：**只回同一个空间里的账号**。
// organization 为空（账号还没归属空间）时**只回自己** —— 空组织不能退化成
// 「看到全部」，那是公有部署下最严重的一种越权。
func (s *Service) AccountDirectory(ctx context.Context, actor, organization string) ([]AccountRow, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, github_id::text, display_name, is_admin, NOT disabled
		 FROM repomesh_access.accounts
		 WHERE ($2 = '' AND id = $1)
		    OR ($2 <> '' AND organization_id::text = $2)
		 ORDER BY display_name, id`, actor, organization)
	if err != nil {
		return nil, fmt.Errorf("access: account directory: %w", err)
	}
	defer rows.Close()
	result := []AccountRow{}
	for rows.Next() {
		var row AccountRow
		if err := rows.Scan(&row.ID, &row.Username, &row.DisplayName, &row.IsAdmin, &row.Active); err != nil {
			return nil, fmt.Errorf("access: account directory scan: %w", err)
		}
		result = append(result, row)
	}
	return result, rows.Err()
}
