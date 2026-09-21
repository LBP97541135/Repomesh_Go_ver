package skill

import (
	"context"
	"strings"
	"testing"
	"time"

	"repomesh.local/repomesh/internal/testdb"
)

// TestPostgresCleanGateIsVersionScoped 钉住用户 2026-09-21 报的「怎么一直在评估中」。
//
// 修复前 CleanGateOK 是 **skill 级**：跨该技能名下**所有版本**的历史，要求
// ≥1 条 pass 且 **0 条 fail**。于是一条历史失败会把所有版本一起锁死 ——
// 用户看到的正是 1.0.0 已晋升、1.1.0 / 1.2.0 却永远出不来，而且
// `allowedTransitions` 里 evaluating→evaluating 不允许、也没有回 draft 的路，
// 所以**没有任何恢复手段**（连以后新登记的版本也会被同一条历史失败连坐）。
//
// 这条用例钉两件事，缺一不可：
//  1. 旧版本的 fail **不锁**新版本（作用域收到版本之后，新版本按自己的证据判）；
//  2. 本版本自己的 fail **仍然锁**它自己 —— 不能为了让用户过而把门拆掉。
func TestPostgresCleanGateIsVersionScoped(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	store := &Store{Pool: pool}
	svc := &Service{Store: store}

	// 每次跑用不同的技能名，避免与同一测试库里的其它数据串味。
	skillName := "clean-gate-scope-" + time.Now().Format("150405.000000000")
	sk, err := store.RegisterSkill(ctx, "", skillName, "闸门作用域", "leader", "fixture")
	if err != nil {
		t.Fatalf("RegisterSkill: %v", err)
	}

	oldVersion, err := store.RegisterVersion(ctx, sk.ID, "1.0.0", "old content", "fixture")
	if err != nil {
		t.Fatalf("RegisterVersion(1.0.0): %v", err)
	}
	newVersion, err := store.RegisterVersion(ctx, sk.ID, "1.1.0", "new content", "fixture")
	if err != nil {
		t.Fatalf("RegisterVersion(1.1.0): %v", err)
	}

	// 两个版本都进"评估中"（run 必须落在这个时间点之后才进窗口）。
	for _, id := range []string{oldVersion.ID, newVersion.ID} {
		if _, err := store.Transition(ctx, id, StatusEvaluating); err != nil {
			t.Fatalf("Transition(evaluating): %v", err)
		}
	}
	// 旧版本记一条带技能臂的 **fail**（就是用户历史里那条 FAIL 有技能 3.0）。
	recordRun(t, ctx, store, oldVersion.ID, ArmWith, ResultFail)
	// 对照组（不带技能）的 fail 是必然的，不该计入 —— 顺手也写一条，确保它不影响判定。
	recordRun(t, ctx, store, oldVersion.ID, ArmWithout, ResultFail)
	// 新版本记一条带技能臂的 **pass**。
	recordRun(t, ctx, store, newVersion.ID, ArmWith, ResultPass)
	recordRun(t, ctx, store, newVersion.ID, ArmWithout, ResultPass)

	// ① 旧版本的 fail 不连坐新版本。
	pass, fail, err := store.CleanGate(ctx, newVersion.ID)
	if err != nil {
		t.Fatalf("CleanGate(1.1.0): %v", err)
	}
	if pass < 1 || fail != 0 {
		t.Fatalf("旧版本的 fail 连坐了新版本：CleanGate(1.1.0) pass=%d fail=%d —— "+
			"这正是用户报的「怎么一直在评估中」", pass, fail)
	}
	if _, err := svc.EnterCanary(ctx, newVersion.ID); err != nil {
		t.Fatalf("新版本应当能进灰度验证，却被拦下：%v", err)
	}

	// ② 本版本自己的 fail 仍然锁它自己（门没有被拆掉）。
	pass, fail, err = store.CleanGate(ctx, oldVersion.ID)
	if err != nil {
		t.Fatalf("CleanGate(1.0.0): %v", err)
	}
	if fail == 0 {
		t.Fatalf("本版本自己的 with-arm fail 必须计入：CleanGate(1.0.0) pass=%d fail=%d", pass, fail)
	}
	_, err = svc.EnterCanary(ctx, oldVersion.ID)
	if err == nil {
		t.Fatal("带 with-arm 失败的版本不该能进灰度验证")
	}
	if !strings.Contains(err.Error(), "1.0.0") {
		t.Errorf("拒绝理由必须点名**版本号**（此前只给 skill 的 uuid，人看不出是哪个版本）：%v", err)
	}
}

func recordRun(t *testing.T, ctx context.Context, store *Store, versionID, arm, result string) {
	t.Helper()
	if _, err := store.Pool.Exec(ctx, `
		INSERT INTO public.skill_evaluation_runs(version_id, question_id, arm, blinded_label, result)
		VALUES ($1::uuid, gen_random_uuid(), $2, $3, $4)`,
		versionID, arm, "blind-fixture", result); err != nil {
		t.Fatalf("insert eval run (%s/%s): %v", arm, result, err)
	}
}
