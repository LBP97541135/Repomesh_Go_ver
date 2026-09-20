package main

import (
	"os"
	"strings"

	"repomesh.local/repomesh/internal/branchvalidation"
)

// coordinatorBranchProvider 选环境回收用的 provider —— 与 web 侧同一套约定：
//
//   - 配了 REPOMESH_POLAR_BRANCH_DSN → PolarDB（PostgreSQL 兼容集群，从业务数据基线库开分支）；
//   - 否则 → 本地 PostgreSQL（同一实例 TEMPLATE 克隆）。
//
// 两边必须选到**同一个** provider，否则协调器会拿着本地 DSN 去删 PolarDB 上的分支名
// （删不掉、一直留在待清理集合里）。所以这段逻辑与 cmd/repomesh-web 的 branchProvider()
// 保持逐字一致，改一处要同时改另一处。
func coordinatorBranchProvider() branchvalidation.BranchProvider {
	if dsn := strings.TrimSpace(os.Getenv("REPOMESH_POLAR_BRANCH_DSN")); dsn != "" {
		return &branchvalidation.PolarProvider{
			BranchDSN:        dsn,
			BaselineDatabase: strings.TrimSpace(os.Getenv("REPOMESH_POLAR_BASELINE_DB")),
			ClusterID:        strings.TrimSpace(os.Getenv("REPOMESH_POLAR_CLUSTER_ID")),
		}
	}
	return &branchvalidation.LocalProvider{AdminDSN: os.Getenv("REPOMESH_DATABASE_URL")}
}
