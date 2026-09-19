package humancontrol

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"repomesh.local/repomesh/internal/testdb"
)

// 真实 Postgres：监管策略草稿的读写、定死与作用域解析。
//
// 这条面此前**整个不存在**（前端一直在打 /projects/{id}/policy-draft，Go 后端
// 从未注册过它），所以三档与卡点永远存不下来 —— 线上实测 agent_teams 唯一一行
// required_checkpoints=[]、review_requests 0 行，而人工审核台读的正是那些卡点。
func TestPostgresPolicyDraftLifecycle(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	owner, other := seedPolicyAccount(t, pool, 771001), seedPolicyAccount(t, pool, 771002)
	projectID := seedPolicyProject(t, pool, owner)
	otherProject := seedPolicyProject(t, pool, other)
	service := New(pool)

	// ① 没设过 = 404（pgx.ErrNoRows），不是 200 空对象：
	//    「没人决定过」与「有人决定了不设卡点」是两件事。
	if _, err := service.PolicyDraft(ctx, projectID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("未设定时应答 ErrNoRows（→404），得到 %v", err)
	}

	// ② 写一份 supervised：一个卡点 + 一条授权。
	view, err := service.PutPolicyDraft(ctx, owner, projectID, PolicyDraftCommand{
		ExecutionMode:       "supervised",
		RequiredCheckpoints: []string{"repository_scope", "delivery"},
		HumanGrants: []PolicyGrant{{
			HumanPrincipalID: owner, Role: "project_supervisor",
			CodeAccess: "read", ControlActions: []string{"approve_checkpoint"},
		}},
	})
	if err != nil {
		t.Fatalf("PutPolicyDraft: %v", err)
	}
	if view.ExecutionMode != "supervised" || len(view.RequiredCheckpoints) != 2 {
		t.Fatalf("回读不符：%+v", view)
	}
	if view.CreatedBy != owner {
		t.Fatalf("created_by 应是设它的那个账号，得到 %q", view.CreatedBy)
	}
	// ③ 幂等读回：同一份草稿再读一次内容不变（整份覆盖写，没有第二份）。
	again, err := service.PolicyDraft(ctx, projectID)
	if err != nil {
		t.Fatalf("PolicyDraft: %v", err)
	}
	if len(again.HumanGrants) != 1 || again.HumanGrants[0].HumanPrincipalID != owner {
		t.Fatalf("授权回读不符：%+v", again.HumanGrants)
	}

	// ④ 物化盖章后改不动（409）。这句话此前只是前端卡片上的文案，
	//    后端并没有任何东西阻止改。
	if err := service.FreezePolicyDraft(ctx, projectID); err != nil {
		t.Fatalf("FreezePolicyDraft: %v", err)
	}
	frozen, err := service.PolicyFrozen(ctx, projectID)
	if err != nil || !frozen {
		t.Fatalf("盖章后应报告已定死，得到 %v / %v", frozen, err)
	}
	_, err = service.PutPolicyDraft(ctx, owner, projectID, PolicyDraftCommand{ExecutionMode: "auto"})
	if !errors.Is(err, ErrPolicyFrozen) {
		t.Fatalf("定死后改写应返回 ErrPolicyFrozen（→409），得到 %v", err)
	}
	if err := service.DeletePolicyDraft(ctx, projectID); !errors.Is(err, ErrPolicyFrozen) {
		t.Fatalf("定死后撤回同样应被拒（撤回等于悄悄降强度），得到 %v", err)
	}

	// ⑤ 作用域解析：项目 id 与 issue id 两种都认；别人的项目一律 404。
	resolved, err := service.ResolveProjectScope(ctx, owner, projectID)
	if err != nil || resolved != projectID {
		t.Fatalf("项目 id 应解析到自己，得到 %q / %v", resolved, err)
	}
	if _, err := service.ResolveProjectScope(ctx, owner, otherProject); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("别人的项目应 404，得到 %v", err)
	}
	// 垃圾输入必须回「不是你的」而不是 uuid 的 22P02（那会变成 500）。
	if _, err := service.ResolveProjectScope(ctx, owner, "not-a-uuid"); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("非法 id 应 404，得到 %v", err)
	}

	// ⑥ 授权指向不存在的账号 = 422 原文（把审核权交给一个没人能登录的身份）。
	_, err = service.PutPolicyDraft(ctx, other, otherProject, PolicyDraftCommand{
		ExecutionMode:       "supervised",
		RequiredCheckpoints: []string{"execution"},
		HumanGrants: []PolicyGrant{{
			HumanPrincipalID: "acct-does-not-exist", Role: "project_supervisor",
			CodeAccess: "read", ControlActions: []string{"approve_checkpoint"},
		}},
	})
	var violation *PolicyViolation
	if !errors.As(err, &violation) || violation.Message != "human grant account does not exist" {
		t.Fatalf("应回 422 原文，得到 %v", err)
	}

	// ⑦ 撤回：成功 204；没得撤时 404 而不是一句轻快的 204。
	if err := service.DeletePolicyDraft(ctx, otherProject); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("没得撤时应答 ErrNoRows（→404），得到 %v", err)
	}
	if _, err := service.PutPolicyDraft(ctx, other, otherProject, PolicyDraftCommand{
		ExecutionMode:       "supervised",
		RequiredCheckpoints: []string{"execution"},
		HumanGrants: []PolicyGrant{{
			HumanPrincipalID: other, Role: "project_supervisor",
			CodeAccess: "read", ControlActions: []string{"approve_checkpoint"},
		}},
	}); err != nil {
		t.Fatalf("第二次写入: %v", err)
	}
	if err := service.DeletePolicyDraft(ctx, otherProject); err != nil {
		t.Fatalf("撤回应成功，得到 %v", err)
	}
	if _, err := service.PolicyDraft(ctx, otherProject); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("撤回后应回到未设定，得到 %v", err)
	}
}

// RequiredCheckpointsFor / RequiresCheckpoint 是执行面读策略的入口：
// 没设过草稿时必须是**空集合**（= 还没人决定过，默认全自动），不是一次失败。
func TestPostgresPolicyCheckpointReads(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	owner := seedPolicyAccount(t, pool, 772001)
	projectID := seedPolicyProject(t, pool, owner)
	service := New(pool)

	checkpoints, err := service.RequiredCheckpointsFor(ctx, projectID)
	if err != nil {
		t.Fatalf("没设过时不该报错：%v", err)
	}
	if len(checkpoints) != 0 {
		t.Fatalf("没设过时应为空集合，得到 %+v", checkpoints)
	}
	if required, err := service.RequiresCheckpoint(ctx, projectID, "delivery"); err != nil || required {
		t.Fatalf("没设过时不该要求人工，得到 %v / %v", required, err)
	}

	if _, err := service.PutPolicyDraft(ctx, owner, projectID, PolicyDraftCommand{
		ExecutionMode:       "supervised",
		RequiredCheckpoints: []string{"delivery"},
		HumanGrants: []PolicyGrant{{
			HumanPrincipalID: owner, Role: "project_supervisor",
			CodeAccess: "read", ControlActions: []string{"approve_checkpoint"},
		}},
	}); err != nil {
		t.Fatalf("PutPolicyDraft: %v", err)
	}
	if required, err := service.RequiresCheckpoint(ctx, projectID, "delivery"); err != nil || !required {
		t.Fatalf("设了 delivery 就该要求人工，得到 %v / %v", required, err)
	}
	if required, err := service.RequiresCheckpoint(ctx, projectID, "validation"); err != nil || required {
		t.Fatalf("没设 validation 就不该要求人工，得到 %v / %v", required, err)
	}
}

// ---- fixture ----

func seedPolicyAccount(t *testing.T, pool *pgxpool.Pool, githubID int64) string {
	t.Helper()
	ctx := context.Background()
	accountID := "acct-policy-" + time.Now().Format("150405.000000000")
	if _, err := pool.Exec(ctx, `INSERT INTO repomesh_access.accounts (id, github_id, display_name)
		 VALUES ($1,$2,'策略夹具账号')`, accountID, githubID); err != nil {
		t.Fatalf("播种账号: %v", err)
	}
	return accountID
}

// seedPolicyProject 播一个最小可用的 A-suite 项目。
// project_current_configuration_fk 是延迟约束，所以项目与它的配置修订必须同事务写。
func seedPolicyProject(t *testing.T, pool *pgxpool.Pool, owner string) string {
	t.Helper()
	ctx := context.Background()
	projectID := policyUUID(t)
	configRevision := policyUUID(t)
	// organization_id 自 0030 起是 NOT NULL：项目必须落在某个空间里。
	// 夹具自建一个空间，而不是借"第一个组织"——借来的空间会让这条测试
	// 依赖库里的既有数据，单独跑就会挂。
	organizationID := policyUUID(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `INSERT INTO public.organizations (id, name) VALUES ($1,'策略夹具空间')`,
		organizationID); err != nil {
		t.Fatalf("播种空间: %v", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_projects.projects
		 (id, owner, organization_id, name, purpose, revision, creation_context_revision, current_configuration_revision)
		 VALUES ($1,$2,$3,'策略夹具项目','监管策略草稿的集成测试用项目',$4,$5,$6)`,
		projectID, owner, organizationID, policyUUID(t), policyUUID(t), configRevision); err != nil {
		t.Fatalf("播种项目: %v", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_projects.configuration_revisions
		 (project_id, revision, fixed, created_by, created_at)
		 VALUES ($1,$2,'{"fixture":true}'::jsonb,$3,clock_timestamp())`,
		projectID, configRevision, owner); err != nil {
		t.Fatalf("播种配置修订: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return projectID
}

// policyUUID 生成 v4 形状的 uuid（projects.id 有正则检查）。
func policyUUID(t *testing.T) string {
	t.Helper()
	id := newID()
	if id == "" {
		t.Fatal("生成 uuid 失败")
	}
	return id
}
