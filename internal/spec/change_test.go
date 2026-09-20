package spec

import (
	"context"
	"crypto/rand"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"repomesh.local/repomesh/internal/testdb"
)

// fakeReviews 记下审核台的写面调用（组合根那侧才接 humancontrol）。
type fakeReviews struct {
	commands []ChangeReviewCommand
}

func (f *fakeReviews) RequestChangeReview(ctx context.Context, command ChangeReviewCommand) (string, error) {
	f.commands = append(f.commands, command)
	return "review-" + command.EvidenceVersion[:8], nil
}

func seedSpecProject(t *testing.T, pool *pgxpool.Pool) (projectID, owner string) {
	t.Helper()
	ctx := context.Background()
	stamp := time.Now().Format("150405.000000000")
	owner = "acct-spec-" + stamp
	if _, err := pool.Exec(ctx, `INSERT INTO repomesh_access.accounts (id, github_id, display_name)
		VALUES ($1, floor(random()*800000000)::bigint + 100000000, '规格夹具账号')`, owner); err != nil {
		t.Fatalf("播种账号: %v", err)
	}
	projectID = specUUID(t)
	organizationID := specUUID(t)
	configRevision := specUUID(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `INSERT INTO public.organizations (id, name) VALUES ($1,'规格夹具空间')`, organizationID); err != nil {
		t.Fatalf("播种空间: %v", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO repomesh_projects.projects
		 (id, owner, organization_id, name, purpose, revision, creation_context_revision, current_configuration_revision)
		 VALUES ($1,$2,$3,'规格夹具项目','规格变更回路的集成测试用项目',$4,$5,$6)`,
		projectID, owner, organizationID, specUUID(t), specUUID(t), configRevision); err != nil {
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
	return projectID, owner
}

// specUUID 生成 v4 形状的 uuid（projects.id / organizations.id 有 uuid 类型约束，
// spec 域自己的 newID 是 "前缀+20位十六进制"，不是 uuid —— 别拿它当 id 用）。
func specUUID(t *testing.T) string {
	t.Helper()
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		t.Fatalf("生成 uuid 失败: %v", err)
	}
	buffer[6] = (buffer[6] & 0x0f) | 0x40
	buffer[8] = (buffer[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", buffer[0:4], buffer[4:6], buffer[6:8], buffer[8:10], buffer[10:16])
}

func TestParseChangeRequestRejectsIncompleteArtifacts(t *testing.T) {
	cases := map[string]string{
		"空文件":       "",
		"不是 JSON":   "{",
		"缺仓库":       `{"title":"t","reason":"r","changes":"c"}`,
		"缺 changes": `{"repository":"owner/a","title":"t","reason":"r"}`,
		"缺 reason":  `{"repository":"owner/a","title":"t","changes":"c"}`,
	}
	for name, raw := range cases {
		if _, err := ParseChangeRequest([]byte(raw)); err == nil {
			t.Fatalf("%s：不合格产物必须被拒", name)
		}
	}
	ok, err := ParseChangeRequest([]byte("```json\n{\"repository\":\"owner/a\",\"title\":\"t\",\"reason\":\"r\",\"changes\":\"c\"}\n```"))
	if err != nil || ok.Repository != "owner/a" {
		t.Fatalf("合格产物被拒: %+v err=%v", ok, err)
	}
}

func TestSpecChangeRequestNeedsHumanApproval(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	projectID, owner := seedSpecProject(t, pool)
	reviews := &fakeReviews{}
	service := New(pool).WithReviews(reviews)

	request := SpecChangeRequest{
		Repository: "owner/a", Title: "规格 v1：免运费边界",
		Reason: "现有规格没写清免运费与小计的边界", Changes: "小计 ≥ 900 时运费为 0",
		Evidence: "线上复现：900 时仍收 12 元", RunID: "run_spec_change_1", TaskID: "task-1",
	}

	submission, err := service.SubmitChangeRequest(ctx, projectID, "agent_worker_1", owner, request)
	if err != nil {
		t.Fatalf("SubmitChangeRequest: %v", err)
	}
	if submission.ReviewID == "" || submission.Duplicate {
		t.Fatalf("提交结果 = %+v", submission)
	}
	if len(reviews.commands) != 1 || reviews.commands[0].EvidenceVersion != request.EvidenceVersion() {
		t.Fatalf("审核单没落：%+v", reviews.commands)
	}
	if reviews.commands[0].Origin != "spec-change-request" {
		t.Fatalf("审核单来源 = %q", reviews.commands[0].Origin)
	}

	// **提交阶段不产生规格** —— agent 不能自己改生效。
	current, err := service.Current(ctx, projectID, request.Repository)
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if current.State != "none" {
		t.Fatalf("提交阶段就产生了规格：%+v", current)
	}

	// 幂等重放：同一次请求不堆第二条待批。
	replay, err := service.SubmitChangeRequest(ctx, projectID, "agent_worker_1", owner, request)
	if err != nil || !replay.Duplicate {
		t.Fatalf("重放 = %+v err=%v", replay, err)
	}

	// 人批 → 以请求内容落下一版规格并批准。
	applied, err := service.ApproveChange(ctx, projectID, owner, submission.EvidenceVersion)
	if err != nil {
		t.Fatalf("ApproveChange: %v", err)
	}
	if applied.Version != 1 || applied.Repository != request.Repository {
		t.Fatalf("批准结果 = %+v", applied)
	}
	current, err = service.Current(ctx, projectID, request.Repository)
	if err != nil {
		t.Fatalf("Current after approve: %v", err)
	}
	if current.State != "approved" || current.Content != request.Changes || current.Version != 1 {
		t.Fatalf("升版后规格 = %+v", current)
	}

	// 第二份变更请求 → 升到 v2（版本轴真的在走）。
	second := request
	second.Changes = "小计 ≥ 900 时运费为 0，并在响应里返回 free_shipping_900"
	second.RunID = "run_spec_change_2"
	secondSubmission, err := service.SubmitChangeRequest(ctx, projectID, "agent_worker_1", owner, second)
	if err != nil {
		t.Fatalf("第二次提交: %v", err)
	}
	secondApplied, err := service.ApproveChange(ctx, projectID, owner, secondSubmission.EvidenceVersion)
	if err != nil {
		t.Fatalf("第二次批准: %v", err)
	}
	if secondApplied.Version != 2 {
		t.Fatalf("第二版号 = %d，应为 2", secondApplied.Version)
	}
	current, err = service.Current(ctx, projectID, request.Repository)
	if err != nil || current.Version != 2 || current.Content != second.Changes {
		t.Fatalf("v2 规格 = %+v err=%v", current, err)
	}

	// 批准之后同一请求不能重复应用（状态已 approved）。
	if _, err := service.ApproveChange(ctx, projectID, owner, submission.EvidenceVersion); err == nil {
		t.Fatal("已批的请求不该能再次应用")
	}
}
