package repositoryteams

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"repomesh.local/repomesh/internal/agentteams"
	"repomesh.local/repomesh/internal/testdb"
)

// 0057 的回归锁：团队按 **(项目, 仓库)** 归属，不再是「全局一个仓库一支队」。
//
// 病是什么：业务链是 账号 → 项目 → issue → 仓库，同一份仓库允许挂到多个项目。
// 而 `public.repository_teams` 的主键只有 `repository_id`（扫描侧 id），语义是
// 「全局一个仓库一支队」。于是同一账号下的第二个项目挂同一个仓时：
//   - `EnsureForRepository` 的 EXISTS 判定只看仓库 → 判「已有队」→ **静默跳过**，
//     第二个项目**一支队都没有**，而且不报错（不是"共用一支"）；
//   - `repository_team_workers` 也只按仓库挂、`resource_name` 全局唯一；
//   - `remotePrefix()` 只喂仓库 id → 两个项目送给远端的**队名完全相同**，互相覆盖。
//
// 这些用例都跑真实 PostgreSQL（迁移 0057 会由 testdb.Open 施加），
// 远端 AgentTeams 用一个本进程的假控制面替身（Client.BaseURL 可注入）。

// fakeController 是最小可用的 AgentTeams 控制面替身。
//
// 只需要两种响应：Worker 状态查询返回一个 phase（Get/Change 会读它），其余写请求返回 2xx
// （controllerResultError 把 2xx 当成功）。不模拟远端语义 —— 这里要验的是**本地这一层
// 的键与命名**，远端的真实行为不是本用例的范围。
func fakeController(t *testing.T) *agentteams.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/status") {
			_, _ = w.Write([]byte(`{"status":{"phase":"Running"}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(server.Close)
	return &agentteams.Client{BaseURL: server.URL, Token: "test-token"}
}

// seedScanRepository 植入**扫描侧**仓库行。
//
// 两个 id 空间必须分清：`repository_teams.repository_id` 存的是扫描侧 id
// （Create 按它查仓库名），而 `project_repositories.repository_id` 是项目侧 `repo_…`。
// testdb.SeedProject 只建项目侧，所以扫描侧这一行要自己补。
// URL 必须能与项目侧「按 URL 对齐」相认（repoTeamResolutionQuery 的规则）。
func seedScanRepository(t *testing.T, pool *pgxpool.Pool, organizationID, scanID, name, url string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO repomesh_scan.repositories (id, name, url, organization_id)
		VALUES ($1, $2, $3, $4::uuid)`, scanID, name, url, organizationID); err != nil {
		t.Fatalf("植入扫描侧仓库失败：%v", err)
	}
}

// sharedRepositoryFixture 造出「同一账号（组织）下两个项目挂同一个仓库」的场景，
// 并返回两边各自的**项目侧**仓库 id 与那个共享的**扫描侧** id。
func sharedRepositoryFixture(t *testing.T, pool *pgxpool.Pool) (projectA, projectB, projectRepoA, projectRepoB, scanID string) {
	t.Helper()
	const (
		organizationID = "7f0a1c2d-1111-4111-8111-7f0a1c2d1111"
		projectAID     = "aaaa1111-2222-4222-8222-aaaa11112222"
		projectBID     = "bbbb1111-3333-4333-8333-bbbb11113333"
		fullName       = "acme/shared"
	)
	scanID = "3f9c1e7a5b2d4c6e8f0a1b3c5d7e9f01"

	fixtureA := testdb.SeedProject(t, pool, projectAID, organizationID, fullName)
	fixtureB := testdb.SeedProject(t, pool, projectBID, organizationID, fullName)

	projectRepoA = fixtureA.Repositories[fullName]
	projectRepoB = fixtureB.Repositories[fullName]
	if projectRepoA == "" || projectRepoA != projectRepoB {
		t.Fatalf("夹具前提不成立：两个项目必须挂同一个项目侧仓库行（A=%q B=%q）",
			projectRepoA, projectRepoB)
	}
	seedScanRepository(t, pool, organizationID, scanID, fullName, "https://github.com/"+fullName)
	return projectAID, projectBID, projectRepoA, projectRepoB, scanID
}

// 核心用例：同一份仓库挂到两个项目 → **两支队**，远端名与人员都不相同。
//
// 反例（0057 之前）：`repository_teams` 的主键只有 `repository_id`，
// 第二个项目的 Create 会撞主键；就算绕过去，`remotePrefix(repositoryID)`
// 也会算出同一个远端队名，两支队在远端互相覆盖。
func TestCreateKeepsSharedRepositoryTeamsIsolatedPerProject(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	projectA, projectB, _, _, scanID := sharedRepositoryFixture(t, pool)
	service := New(pool, fakeController(t))

	snapshotA, err := service.Create(ctx, projectA, scanID, 2)
	if err != nil {
		t.Fatalf("项目 A 建队失败：%v", err)
	}
	snapshotB, err := service.Create(ctx, projectB, scanID, 2)
	if err != nil {
		t.Fatalf("项目 B 建队失败（同一仓库挂第二个项目时正是这里静默失败）：%v", err)
	}

	// 1) 两支行都落在同一个 repository_id 上 —— 这正是旧主键挡不住、也分不开的地方。
	rows, err := pool.Query(ctx, `
		SELECT project_id::text, agentteams_team_name, leader_id::text, leader_resource_name
		FROM public.repository_teams WHERE repository_id = $1`, scanID)
	if err != nil {
		t.Fatalf("回读团队失败：%v", err)
	}
	defer rows.Close()
	type teamRow struct{ projectID, teamName, leaderID, leaderResource string }
	var teams []teamRow
	for rows.Next() {
		var row teamRow
		if err := rows.Scan(&row.projectID, &row.teamName, &row.leaderID, &row.leaderResource); err != nil {
			t.Fatalf("回读团队扫描失败：%v", err)
		}
		teams = append(teams, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("回读团队失败：%v", err)
	}
	if len(teams) != 2 {
		t.Fatalf("同一仓库挂两个项目应有两支队，实得 %d 支：%+v", len(teams), teams)
	}
	if teams[0].projectID == teams[1].projectID {
		t.Fatalf("两支队必须分属不同项目，实得都是 %s", teams[0].projectID)
	}

	// 2) 远端队名必须不同 —— 否则两支队在 AgentTeams 上会互相覆盖。
	if teams[0].teamName == teams[1].teamName {
		t.Fatalf("两个项目算出了同一个远端队名 %q —— 远端会互相覆盖", teams[0].teamName)
	}
	if teams[0].leaderResource == teams[1].leaderResource {
		t.Fatalf("两个项目共用同一个 leader 资源名 %q", teams[0].leaderResource)
	}
	if teams[0].leaderID == teams[1].leaderID {
		t.Fatalf("两个项目共用同一个 leader id %q", teams[0].leaderID)
	}

	// 3) 人员是两批不同的人（编制层 0054 已按项目隔离，这一层要跟上）。
	if snapshotA.Leader.ID == snapshotB.Leader.ID {
		t.Fatalf("两个项目共用同一个 Leader：%s", snapshotA.Leader.ID)
	}
	if len(snapshotA.Workers) != 2 || len(snapshotB.Workers) != 2 {
		t.Fatalf("工人数不对：A=%d B=%d", len(snapshotA.Workers), len(snapshotB.Workers))
	}
	workersA := map[string]bool{}
	for _, worker := range snapshotA.Workers {
		workersA[worker.ID] = true
	}
	for _, worker := range snapshotB.Workers {
		if workersA[worker.ID] {
			t.Fatalf("两个项目共用同一个工人 %s", worker.ID)
		}
	}

	// 4) 子表的唯一性也跟着按项目分：4 名 worker 各自一行，资源名互不相同。
	var workerCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(DISTINCT resource_name) FROM public.repository_team_workers
		WHERE repository_id = $1 AND status = 'active'`, scanID).Scan(&workerCount); err != nil {
		t.Fatalf("回读 worker 失败：%v", err)
	}
	if workerCount != 4 {
		t.Fatalf("两个项目应各有 2 名互不重名的 worker（共 4），实得 %d", workerCount)
	}
}

// 反向证伪：**第二个项目必须真的建出队**。
//
// 这正是用户报的那个 bug 的准确形态 —— 0057 之前 `EnsureForRepository` 的
// EXISTS 判定是 `WHERE repository_id = $1`（不含项目），第二个项目走到这里判定
// 「已有队」直接 `return false, nil`：不建、不报错、页面上就是"没有队"。
// 把那一行改回只按仓库，本用例立刻红。
func TestEnsureForRepositoryBuildsTeamForSecondProject(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	projectA, projectB, projectRepoA, projectRepoB, scanID := sharedRepositoryFixture(t, pool)
	service := New(pool, fakeController(t))

	createdA, err := service.EnsureForRepository(ctx, projectA, projectRepoA, 1)
	if err != nil {
		t.Fatalf("项目 A 建队失败：%v", err)
	}
	if !createdA {
		t.Fatal("项目 A 第一次接入应该真的建出一支队")
	}

	// 关键断言：仓库已经被 A 用过了，B 仍然要能建出自己的那一支。
	createdB, err := service.EnsureForRepository(ctx, projectB, projectRepoB, 1)
	if err != nil {
		t.Fatalf("项目 B 建队失败：%v", err)
	}
	if !createdB {
		t.Fatal("项目 B 建不出队 —— 幂等判定又退回「按仓库」了（这就是被修掉的那个 bug）")
	}

	// 幂等：同项目重复调用不再新建。
	againA, err := service.EnsureForRepository(ctx, projectA, projectRepoA, 1)
	if err != nil {
		t.Fatalf("项目 A 重复建队失败：%v", err)
	}
	if againA {
		t.Fatal("同一项目重复调用不该再建一支队")
	}

	var teams int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM public.repository_teams WHERE repository_id = $1`, scanID).Scan(&teams); err != nil {
		t.Fatalf("回读团队数失败：%v", err)
	}
	if teams != 2 {
		t.Fatalf("期望恰好 2 支（每项目一支），实得 %d", teams)
	}
}

// 读与改都必须限定在**本项目**那一支上。
//
// 反例：路径/查询若不带项目，Get 与 Change 会命中"这个仓库的第一支队"，
// 于是给 A 加人会把 B 的编制也改了。
func TestGetAndChangeAreProjectScoped(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	projectA, projectB, _, _, scanID := sharedRepositoryFixture(t, pool)
	service := New(pool, fakeController(t))

	if _, err := service.Create(ctx, projectA, scanID, 1); err != nil {
		t.Fatalf("项目 A 建队失败：%v", err)
	}
	if _, err := service.Create(ctx, projectB, scanID, 1); err != nil {
		t.Fatalf("项目 B 建队失败：%v", err)
	}

	snapshotA, err := service.Get(ctx, projectA, scanID)
	if err != nil {
		t.Fatalf("读项目 A 的队失败：%v", err)
	}
	snapshotB, err := service.Get(ctx, projectB, scanID)
	if err != nil {
		t.Fatalf("读项目 B 的队失败：%v", err)
	}
	if snapshotA.Leader.ID == snapshotB.Leader.ID {
		t.Fatal("Get 没有按项目区分，两个项目读到了同一支队的 Leader")
	}

	// 给 A 扩到 3 人：B 必须原封不动。
	if _, err := service.Change(ctx, projectA, scanID, ChangeCommand{
		WorkerCount: 3, RosterRevision: snapshotA.RosterRevision,
	}); err != nil {
		t.Fatalf("项目 A 扩编失败：%v", err)
	}

	afterA, err := service.Get(ctx, projectA, scanID)
	if err != nil {
		t.Fatalf("扩编后读项目 A 失败：%v", err)
	}
	afterB, err := service.Get(ctx, projectB, scanID)
	if err != nil {
		t.Fatalf("扩编后读项目 B 失败：%v", err)
	}
	if len(afterA.Workers) != 3 {
		t.Fatalf("项目 A 应扩到 3 名 worker，实得 %d", len(afterA.Workers))
	}
	if len(afterB.Workers) != 1 {
		t.Fatalf("项目 B 的编制被连带改了：应仍为 1 名 worker，实得 %d", len(afterB.Workers))
	}
	if afterB.RosterRevision != snapshotB.RosterRevision {
		t.Fatalf("项目 B 的花名册版本不该变：%d → %d", snapshotB.RosterRevision, afterB.RosterRevision)
	}

	// Change 用的队名必须是**库里那一行**的队名（存量队名不含项目维度，重算会指错队）。
	var teamNameA string
	if err := pool.QueryRow(ctx, `
		SELECT agentteams_team_name FROM public.repository_teams
		WHERE project_id = $1::uuid AND repository_id = $2`, projectA, scanID).Scan(&teamNameA); err != nil {
		t.Fatalf("回读队名失败：%v", err)
	}
	if got, want := remotePrefix(projectA, scanID), teamNameA; got == want {
		t.Fatalf("本用例失去意义：新算的名字 %q 与存量队名相同，无法证明是「读库」而不是「重算」", got)
	}
}

// 锁住 schema 本身：主键是 (project_id, repository_id)，且同一个项目不能有第二支。
func TestRepositoryTeamPrimaryKeyIncludesProject(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	projectA, projectB, _, _, scanID := sharedRepositoryFixture(t, pool)
	service := New(pool, fakeController(t))
	for _, projectID := range []string{projectA, projectB} {
		if _, err := service.Create(ctx, projectID, scanID, 1); err != nil {
			t.Fatalf("项目 %s 建队失败：%v", projectID, err)
		}
	}

	// 同项目 + 同仓库再插一行：必须被主键挡住（幂等保护）。
	if _, err := pool.Exec(ctx, `
		INSERT INTO public.repository_teams
			(project_id, repository_id, agentteams_team_name, leader_id, leader_resource_name)
		VALUES ($1::uuid, $2, 'dup-team-name', gen_random_uuid(), 'dup-leader')`,
		projectA, scanID); err == nil {
		t.Fatal("同一 (项目, 仓库) 插第二行应撞主键，居然成功了")
	}

	// 反过来：同仓库换项目就该插得进去（这正是 0057 放开的）。
	if _, err := pool.Exec(ctx, `
		INSERT INTO public.repository_teams
			(project_id, repository_id, agentteams_team_name, leader_id, leader_resource_name)
		VALUES (gen_random_uuid(), $1, 'other-project-team', gen_random_uuid(), 'other-leader')`,
		scanID); err == nil {
		t.Fatal("外键应挡住不存在的项目（project_id 上有指向 repomesh_projects.projects 的外键）")
	}
}
