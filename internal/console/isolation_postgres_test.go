package console

import (
	"context"
	"testing"
	"time"

	"repomesh.local/repomesh/internal/assembly"
	"repomesh.local/repomesh/internal/testdb"
)

// 账号隔离的**可见性**验证：A 账号只能看到自己空间里的东西。
//
// 2026-09-19 修复前：Organizations / Agents / Teams / Repositories 四个目录读面
// 全是**全库返回** —— 公有部署（一账号一空间）下，任何登录账号都能看到别人的
// 空间名、智能体、团队与仓库目录。这条测试把"看不见"钉死。
func TestPostgresConsoleSurfacesAreAccountScoped(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const (
		orgA = "aaaaaaaa-1111-4111-8111-aaaaaaaaaaaa"
		orgB = "bbbbbbbb-2222-4222-8222-bbbbbbbbbbbb"
		// public.agents.id 是 uuid 列，夹具必须用合法 UUID。
		agentA = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
		agentB = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture: %v\nSQL: %s", err, sql)
		}
	}
	exec(`INSERT INTO public.organizations (id, name) VALUES ($1, 'A 的空间'), ($2, 'B 的空间')`, orgA, orgB)
	exec(`INSERT INTO repomesh_access.accounts (id, github_id, display_name, organization_id) VALUES
		('acct-a', 880001, '账号A', $1),
		('acct-b', 880002, '账号B', $2)`, orgA, orgB)
	exec(`INSERT INTO public.agents (id, organization_id, role, resource_ref, status, prompt, cli_kind)
		VALUES ($3, $1, 'leader', '{"name":"A 的 leader"}'::jsonb, 'active', '', 'codex_cli'),
		       ($4, $2, 'leader', '{"name":"B 的 leader"}'::jsonb, 'active', '', 'codex_cli')`, orgA, orgB, agentA, agentB)

	service := New(pool)

	// ① 空间列表：只看到自己的那一个。
	orgsA, err := service.Organizations(ctx, "acct-a")
	if err != nil {
		t.Fatalf("Organizations(A): %v", err)
	}
	if len(orgsA.Organizations) != 1 || orgsA.Organizations[0].OrganizationID != orgA {
		t.Fatalf("A 只应看到自己的空间，得到 %+v", orgsA.Organizations)
	}
	orgsB, err := service.Organizations(ctx, "acct-b")
	if err != nil {
		t.Fatalf("Organizations(B): %v", err)
	}
	if len(orgsB.Organizations) != 1 || orgsB.Organizations[0].OrganizationID != orgB {
		t.Fatalf("B 只应看到自己的空间，得到 %+v", orgsB.Organizations)
	}

	// ② 智能体目录：看不到对方的人。
	agentsA, err := service.Agents(ctx, "acct-a", false)
	if err != nil {
		t.Fatalf("Agents(A): %v", err)
	}
	if len(agentsA.Agents) != 1 || agentsA.Agents[0].AgentID != agentA {
		t.Fatalf("A 只应看到自己的智能体，得到 %+v", agentsA.Agents)
	}

	// ③ 没有归属空间的账号：如实返回空，而不是退化成"全部"。
	exec(`INSERT INTO repomesh_access.accounts (id, github_id, display_name) VALUES ('acct-none', 880003, '未归属账号')`)
	orgsNone, err := service.Organizations(ctx, "acct-none")
	if err != nil {
		t.Fatalf("Organizations(未归属): %v", err)
	}
	if len(orgsNone.Organizations) != 0 {
		t.Fatalf("未归属账号不应看到任何空间，得到 %+v", orgsNone.Organizations)
	}
	agentsNone, err := service.Agents(ctx, "acct-none", false)
	if err != nil {
		t.Fatalf("Agents(未归属): %v", err)
	}
	if len(agentsNone.Agents) != 0 {
		t.Fatalf("未归属账号不应看到任何智能体（此前会退化成「全部」），得到 %+v", agentsNone.Agents)
	}
}

// 智能体名册（assembly.Roster）同样不得在"账号没归属空间"时退化为全部。
func TestPostgresRosterNeverDegradesToAll(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if _, err := pool.Exec(ctx, `INSERT INTO public.organizations (id, name) VALUES ('cccccccc-3333-4333-8333-cccccccccccc', 'C 的空间')`); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO public.agents (id, organization_id, role, resource_ref, status, prompt, cli_kind)
		VALUES ('cccccccc-cccc-4ccc-8ccc-cccccccccccc', 'cccccccc-3333-4333-8333-cccccccccccc', 'leader', '{}'::jsonb, 'active', '', 'codex_cli')`); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO repomesh_access.accounts (id, github_id, display_name) VALUES ('acct-orphan', 880004, '无空间账号')`); err != nil {
		t.Fatalf("fixture: %v", err)
	}

	roster, err := assembly.New(pool, nil, nil).Roster(ctx, "acct-orphan", "", "", "")
	if err != nil {
		t.Fatalf("Roster: %v", err)
	}
	if len(roster) != 0 {
		t.Fatalf("无空间账号的名册必须为空（此前 OR ... IS NULL 会退化成全部），得到 %d 条", len(roster))
	}
}
