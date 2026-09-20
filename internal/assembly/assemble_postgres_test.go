package assembly

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"repomesh.local/repomesh/internal/testdb"
)

// 迁移 0053 的回归锁：编制的幂等键是 (project_id, singleton_key)，
// **不再**是 (organization_id, singleton_key)。
//
// 反例（0053 之前）：同一账号下的两个项目挂同一个仓库，第二次编制会命中第一次
// 建出的 leader/manager/worker —— 两个项目共享同一批人（"串"）。组织那道墙只挡得住
// 别的账号，挡不住同一账号下的第二个项目：组织与账号 1:1（0037），它不是一个
// 项目边界。
func TestAssembleKeepsSharedRepositoryIsolatedPerProject(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const (
		organizationID = "cccccccc-3333-4333-8333-cccccccccccc"
		projectA       = "aaaaaaaa-1111-4111-8111-aaaaaaaaaaaa"
		projectB       = "bbbbbbbb-2222-4222-8222-bbbbbbbbbbbb"
		sharedRepo     = "acme/shared"
	)
	// 同一个组织（= 同一个账号空间）下的两个项目，挂同一个仓库。
	fixtureA := testdb.SeedProject(t, pool, projectA, organizationID, sharedRepo)
	fixtureB := testdb.SeedProject(t, pool, projectB, organizationID, sharedRepo)
	repositoryID := fixtureA.Repositories[sharedRepo]
	if repositoryID == "" || repositoryID != fixtureB.Repositories[sharedRepo] {
		t.Fatalf("夹具前提不成立：两个项目必须挂同一个仓库行（A=%q B=%q）",
			repositoryID, fixtureB.Repositories[sharedRepo])
	}

	service := New(pool, nil, nil)
	assemble := func(projectID string) AssemblyResult {
		t.Helper()
		result, err := service.Assemble(ctx, AssemblyCommand{
			ProjectID:      projectID,
			Repositories:   []string{repositoryID},
			WorkersPerRepo: 2,
		})
		if err != nil {
			t.Fatalf("项目 %s 编制失败：%v", projectID, err)
		}
		return result
	}

	resultA, resultB := assemble(projectA), assemble(projectB)

	// 1) 两套人是**不同**的人，而不是共享同一个人。
	if resultA.LeaderAgentID == resultB.LeaderAgentID {
		t.Fatalf("两个项目共用同一个项目总领导 %s —— 项目级隔离失效", resultA.LeaderAgentID)
	}
	if len(resultA.Managers) != 1 || len(resultB.Managers) != 1 ||
		resultA.Managers[0] == resultB.Managers[0] {
		t.Fatalf("两个项目共用同一个仓库经理：A=%v B=%v", resultA.Managers, resultB.Managers)
	}
	if len(resultA.Workers) != 2 || len(resultB.Workers) != 2 {
		t.Fatalf("工人数不对：A=%v B=%v", resultA.Workers, resultB.Workers)
	}
	for index := range resultA.Workers {
		if resultA.Workers[index] == resultB.Workers[index] {
			t.Fatalf("两个项目共用同一个工人 %s", resultA.Workers[index])
		}
	}

	// 2) 每套人都确实挂在**自己**的项目上，且组织列仍写着冗余租户戳。
	rows, err := pool.Query(ctx, `SELECT project_id::text, organization_id::text, role,
			COALESCE(singleton_key,'')
		 FROM public.agents ORDER BY project_id, role, singleton_key`)
	if err != nil {
		t.Fatalf("回读编制失败：%v", err)
	}
	defer rows.Close()
	projectAgentCount := map[string]int{}
	perProjectKeys := map[string][]string{}
	for rows.Next() {
		var projectID, orgID, role, singletonKey string
		if err := rows.Scan(&projectID, &orgID, &role, &singletonKey); err != nil {
			t.Fatalf("回读编制扫描失败：%v", err)
		}
		if orgID != organizationID {
			t.Fatalf("组织戳写错了：agent(project=%s role=%s) 的 organization_id=%s，期望 %s",
				projectID, role, orgID, organizationID)
		}
		if strings.Contains(singletonKey, organizationID) {
			t.Fatalf("singleton_key 里还带着组织前缀：%q（0053 之后应为 角色:仓库:名字）", singletonKey)
		}
		projectAgentCount[projectID]++
		perProjectKeys[projectID] = append(perProjectKeys[projectID], singletonKey)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("回读编制失败：%v", err)
	}
	// 一个项目 = 1 总领导 + 1 经理 + 2 工人。
	for _, projectID := range []string{projectA, projectB} {
		if got := projectAgentCount[projectID]; got != 4 {
			t.Fatalf("项目 %s 应有 4 名编制成员，实得 %d：%v", projectID, got, perProjectKeys[projectID])
		}
	}
	if len(projectAgentCount) != 2 {
		t.Fatalf("agents 应只含这两个项目的成员，实得项目数 %d：%v", len(projectAgentCount), projectAgentCount)
	}
	// 项目总领导的名字由项目派生，两个项目因此天然不同名。
	leaderKeyA := "leader::leader-" + projectA[len(projectA)-12:]
	leaderKeyB := "leader::leader-" + projectB[len(projectB)-12:]
	if !slices.Contains(perProjectKeys[projectA], leaderKeyA) {
		t.Errorf("项目 A 的键里应有 %q，实得 %v", leaderKeyA, perProjectKeys[projectA])
	}
	if !slices.Contains(perProjectKeys[projectB], leaderKeyB) {
		t.Errorf("项目 B 的键里应有 %q，实得 %v", leaderKeyB, perProjectKeys[projectB])
	}

	// 3) 一仓一队：两个项目各有一支行，不共用同一行。
	var teamCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM public.agent_teams
		 WHERE project_id IN ($1::uuid,$2::uuid)`, projectA, projectB).Scan(&teamCount); err != nil {
		t.Fatalf("回读团队失败：%v", err)
	}
	if teamCount != 2 {
		t.Fatalf("两个项目应各有一支仓库团队（共 2 支），实得 %d", teamCount)
	}

	// 4) 重复编制保持幂等：同名重放不新增人，也不换 id。
	replay := assemble(projectA)
	if replay.LeaderAgentID != resultA.LeaderAgentID ||
		replay.Managers[0] != resultA.Managers[0] ||
		replay.Workers[0] != resultA.Workers[0] {
		t.Fatalf("项目 A 重复编制换了人：首次=%+v 重放=%+v", resultA, replay)
	}
	var total int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM public.agents`).Scan(&total); err != nil {
		t.Fatalf("回读编制总数失败：%v", err)
	}
	if total != 8 {
		t.Fatalf("重复编制不应新增人，期望 8 行，实得 %d", total)
	}
}

// 拓扑读面的字段改名（organization_leader_id → project_leader_id）也要锁住：
// 它现在按**项目**查总领导。此前按组织查，一个组织下的第二个项目会读到别人的领导。
func TestProjectTopologyReportsProjectScopedLeader(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const (
		organizationID = "dddddddd-4444-4444-8444-dddddddddddd"
		projectID      = "eeeeeeee-5555-4555-8555-eeeeeeeeeeee"
		fullName       = "acme/topology"
	)
	fixture := testdb.SeedProject(t, pool, projectID, organizationID, fullName)
	repositoryID := fixture.Repositories[fullName]
	if repositoryID == "" {
		t.Fatalf("夹具没建出仓库 %s", fullName)
	}

	service := New(pool, nil, nil)
	result, err := service.Assemble(ctx, AssemblyCommand{
		ProjectID:      projectID,
		Repositories:   []string{repositoryID},
		WorkersPerRepo: 1,
	})
	if err != nil {
		t.Fatalf("编制失败：%v", err)
	}
	view, err := service.ProjectTopology(ctx, projectID)
	if err != nil {
		t.Fatalf("读项目拓扑失败：%v", err)
	}
	if view.ProjectLeaderID != result.LeaderAgentID {
		t.Fatalf("拓扑里的总领导应为项目级编制出的 %s，实得 %q",
			result.LeaderAgentID, view.ProjectLeaderID)
	}
	if len(view.RepositoryTeams) != 1 {
		t.Fatalf("应有一支仓库团队，实得 %d", len(view.RepositoryTeams))
	}
	if view.RepositoryTeams[0].LeaderAgentID != result.LeaderAgentID {
		t.Fatalf("仓库团队的 leader 应为 %s，实得 %s",
			result.LeaderAgentID, view.RepositoryTeams[0].LeaderAgentID)
	}
}

// 上线升级路径的锁：0053 把库里既有行的键改写成**项目级口径**之后，现役 Assemble
// 必须认出它们就是同一批人，而不是再建一套。
//
// 这条之所以单列：迁移产出的键与 ensureAgent 算出的键只要差一个字符，部署后第一次
// 编制就会建出**第二个总领导**——那是不用数据修复就回不去的重复。
// 这里不去跑迁移（迁移本身的回填由 SQL 侧验证），而是把「迁移之后的形状」直接摆进
// 库里，再看 Assemble 认不认。
func TestAssembleReusesMigratedProjectScopedIdentities(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const (
		organizationID = "11111111-2222-4222-8222-111111111111"
		projectID      = "22222222-3333-4333-8333-222222222222"
		fullName       = "acme/migrated"
	)
	fixture := testdb.SeedProject(t, pool, projectID, organizationID, fullName)
	repositoryID := fixture.Repositories[fullName]
	if repositoryID == "" {
		t.Fatalf("夹具没建出仓库 %s", fullName)
	}
	// 短名口径与 assembly.shortName 一致：取末尾 12 位。刻意写成「被测代码之外」的
	// 独立表达式，这样当 shortName 被改动时这里会一起红，而不是跟着一起漂。
	tail := func(s string) string { return s[len(s)-12:] }

	type migratedIdentity struct {
		id, role, repositoryID, singletonKey string
	}
	migrated := []migratedIdentity{
		{"cccccccc-0001-4000-8000-00000000000a", "leader", "",
			"leader::leader-" + tail(projectID)},
		{"cccccccc-0002-4000-8000-00000000000b", "manager", repositoryID,
			"manager:" + repositoryID + ":mgr-" + tail(repositoryID)},
		{"cccccccc-0003-4000-8000-00000000000c", "worker", repositoryID,
			"worker:" + repositoryID + ":wrk-" + tail(repositoryID) + "-0"},
	}
	for _, row := range migrated {
		if _, err := pool.Exec(ctx, `INSERT INTO public.agents
				(id, organization_id, project_id, role, repository_id, singleton_key, resource_ref)
				VALUES ($1::uuid,$2::uuid,$3::text,$4,NULLIF($5,''),$6,'{}'::jsonb)`,
			row.id, organizationID, projectID, row.role, row.repositoryID, row.singletonKey); err != nil {
			t.Fatalf("写入「迁移后」的编制行失败：%v", err)
		}
	}

	service := New(pool, nil, nil)
	result, err := service.Assemble(ctx, AssemblyCommand{
		ProjectID:      projectID,
		Repositories:   []string{repositoryID},
		WorkersPerRepo: 1,
	})
	if err != nil {
		t.Fatalf("编制失败：%v", err)
	}
	if result.LeaderAgentID != migrated[0].id {
		t.Fatalf("总领导没被复用：库里已有 %s，Assemble 返回 %s —— 会建出第二个总领导",
			migrated[0].id, result.LeaderAgentID)
	}
	if len(result.Managers) != 1 || result.Managers[0] != migrated[1].id {
		t.Fatalf("仓库经理没被复用：库里已有 %s，Assemble 返回 %v",
			migrated[1].id, result.Managers)
	}
	if len(result.Workers) != 1 || result.Workers[0] != migrated[2].id {
		t.Fatalf("工人没被复用：库里已有 %s，Assemble 返回 %v",
			migrated[2].id, result.Workers)
	}
	var total int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM public.agents
		 WHERE project_id=$1::text`, projectID).Scan(&total); err != nil {
		t.Fatalf("回读编制总数失败：%v", err)
	}
	if total != 3 {
		t.Fatalf("Assemble 应复用既有 3 行、不新增，实得 %d 行", total)
	}
}

// 组织级角色（治理 leader 这类没有项目归属的键）不能被 0053 的键改写波及：
// 它们继续按组织幂等，插入/读取都不经过项目。
func TestEnsureAgentKeepsOrganizationScopedRowsIntact(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const organizationID = "ffffffff-6666-4666-8666-ffffffffffff"
	testdb.SeedProject(t, pool, "", organizationID, "acme/orgscope")

	// 模拟遗留的治理 leader：没有项目归属，键里没有冒号。
	const governanceKey = "governance-leader"
	if _, err := pool.Exec(ctx, `INSERT INTO public.agents
			(id, organization_id, role, singleton_key, resource_ref)
			VALUES (gen_random_uuid(), $1::uuid, 'organization_leader', $2, '{}'::jsonb)`,
		organizationID, governanceKey); err != nil {
		t.Fatalf("插入组织级 agent 失败：%v", err)
	}

	var projectID *string
	var key string
	if err := pool.QueryRow(ctx, `SELECT project_id, singleton_key FROM public.agents
		 WHERE role='organization_leader'`).Scan(&projectID, &key); err != nil {
		t.Fatalf("回读组织级 agent 失败：%v", err)
	}
	if projectID != nil {
		t.Fatalf("组织级 agent 不该有项目归属，实得 %q", *projectID)
	}
	if key != governanceKey {
		t.Fatalf("组织级 agent 的键不该被改写，期望 %q 实得 %q", governanceKey, key)
	}

	// 同一组织下再插一条同名键：应撞唯一索引（组织级幂等仍然生效）。
	if _, err := pool.Exec(ctx, `INSERT INTO public.agents
			(id, organization_id, role, singleton_key, resource_ref)
			VALUES (gen_random_uuid(), $1::uuid, 'organization_leader', $2, '{}'::jsonb)`,
		organizationID, governanceKey); err == nil {
		t.Fatal("组织级同名键应撞唯一索引，居然成功了")
	}
}
