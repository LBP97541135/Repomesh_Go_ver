package discovery

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"repomesh.local/repomesh/internal/testdb"
)

// ---- 任务归属标签（2026-09-20 用户裁定）----

// Leader 归属仓库（`Leader · <仓库名>`），Worker 是编制内序号 W1、W2…。
//
// 主线把标签计算抽成了纯函数 taskLabels，LBP 的 plan.go 则是在 Materialize 里
// 内联成 leader_label/worker_label 两列直接写库——所以这里按 LBP 的真实实现，
// 走完整的「候选 → 分档 → 审批 → 计划 → 物化」链路，再回读 public.tasks 的两列，
// 而不是照搬主线的函数签名。
func TestMaterializeWritesOwnershipLabels(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	const issue = "iss_plan_labels"
	seedRecallFixture(t, ctx, pool, issue,
		"我需要新增一个用户满1000减200的活动",
		[]string{"checkout", "满减", "促销", "结算"})
	service := New(pool)

	if _, err := service.Candidates(ctx, issue, agentID, "labels-candidates", 20, nil); err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if _, err := service.Classification(ctx, issue, agentID, "labels-classify"); err != nil {
		t.Fatalf("Classification: %v", err)
	}
	version := evidenceVersion(t, ctx, pool, issue)
	if _, err := service.Approval(ctx, issue, agentID, "labels-approve", "approved", "确认", nil, version); err != nil {
		t.Fatalf("Approval: %v", err)
	}
	if _, err := service.Plan(ctx, issue, agentID, "labels-plan"); err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if _, err := service.Materialize(ctx, issue, agentID, "labels-materialize"); err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	planID, _ := readJSONColumn(t, ctx, pool, "plan", issue)["plan_id"].(string)
	if planID == "" {
		t.Fatal("计划里没有 plan_id")
	}
	rows, err := pool.Query(ctx,
		`SELECT repository_id, COALESCE(leader_label,''), COALESCE(worker_label,'')
		 FROM public.tasks WHERE plan_id=$1`, planID)
	if err != nil {
		t.Fatalf("读取任务失败: %v", err)
	}
	defer rows.Close()

	total := 0
	workers := map[string]bool{}
	for rows.Next() {
		var repo, leader, worker string
		if err := rows.Scan(&repo, &leader, &worker); err != nil {
			t.Fatalf("扫描任务失败: %v", err)
		}
		total++
		if want := "Leader · " + repo; leader != want {
			t.Fatalf("Leader 归属仓库的标签应为 %q，得到 %q", want, leader)
		}
		if !strings.HasPrefix(worker, "W") {
			t.Fatalf("Worker 标签应是编制内序号 W1、W2…，得到 %q", worker)
		}
		if workers[worker] {
			t.Fatalf("Worker 序号重复：%q", worker)
		}
		workers[worker] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if total == 0 {
		t.Fatal("物化没有写入任何任务")
	}
	for index := 1; index <= total; index++ {
		label := "W" + strconv.Itoa(index)
		if !workers[label] {
			t.Fatalf("Worker 序号应连续覆盖 W1…W%d，缺少 %q（得到 %v）", total, label, workers)
		}
	}
}

// ---- 关键词去噪（2026-09-18 主线修正）----

// 需求文档的模板字段/标题编号不是业务关键词：纯数字（编号/ID/年份）和
// dataset/train/ticket/数据集 这类结构词必须被挡在检索信号之外，正常业务词保留。
func TestExtractKeywordsDropsTemplateNoiseAndDigits(t *testing.T) {
	got := extractKeywords("需求概述 案例编号 数据集 dataset来源 001 2024 ticket train dataset 报价计算 满减活动 免运费")
	want := []string{"报价计算", "满减活动", "免运费"}
	if len(got) != len(want) {
		t.Fatalf("去噪结果应为 %v，得到 %v", want, got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("去噪结果应为 %v，得到 %v", want, got)
		}
	}
}

// 纯数字判定本身也要挡住编号/ID，同时不能误伤含数字的业务词。
func TestIsAllDigits(t *testing.T) {
	if !isAllDigits("001") || !isAllDigits("2024") {
		t.Fatal("纯数字串应判为数字")
	}
	if isAllDigits("v2") || isAllDigits("满6000") || isAllDigits("") {
		t.Fatal("非纯数字串不应判为数字")
	}
}
