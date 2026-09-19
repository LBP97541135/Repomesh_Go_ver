package web

import (
	"encoding/json"
	"testing"
	"time"

	"repomesh.local/repomesh/internal/discovery"
)

func eventNames(events []streamEvent) []string {
	names := make([]string, len(events))
	for i, event := range events {
		names[i] = event.name
	}
	return names
}

func equals(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// 首拍必须发 discovery_step(订户立刻拿到最新读投影),但空健康表不发声。
// 这也是 prev==nil 的空表守护:修过一次 nil 解引用,别再回来。
func TestEventsFromFirstPoll(t *testing.T) {
	snap := &issueEventSnapshot{
		view:      map[string]any{"issue_id": "i-1", "step": 1},
		viewSig:   "v1",
		health:    []map[string]any{},
		healthSig: "h0",
	}
	got := snap.eventsFrom(nil)
	if !equals(eventNames(got), []string{"discovery_step"}) {
		t.Fatalf("first poll events = %v, want [discovery_step]", eventNames(got))
	}
}

// 指纹全未变 → 一帧都不发(差量流的基本契约)。
func TestEventsFromUnchangedSendsNothing(t *testing.T) {
	snap := &issueEventSnapshot{
		view:    map[string]any{"issue_id": "i-1"},
		viewSig: "v1", taskSig: "t1", healthSig: "h1",
		task:   map[string]any{"task_id": "t"},
		health: []map[string]any{{"step": 1, "kind": "failed", "message": "x"}},
	}
	if got := snap.eventsFrom(snap); len(got) != 0 {
		t.Fatalf("unchanged snapshot emitted %v, want none", eventNames(got))
	}
}

// 任务与健康同拍变化 → 三型齐发;信号清空也发一帧空表,前端才能撤告警。
func TestEventsFromDiffEmitsAllThree(t *testing.T) {
	prev := &issueEventSnapshot{
		view: map[string]any{"issue_id": "i-1"}, viewSig: "v1",
		health: []map[string]any{{"step": 1, "kind": "failed", "message": "x"}}, healthSig: "h1",
	}
	snap := &issueEventSnapshot{
		view: map[string]any{"issue_id": "i-1"}, viewSig: "v2",
		task: map[string]any{"task_id": "t"}, taskSig: "t1",
		health: []map[string]any{}, healthSig: "h2",
	}
	got := snap.eventsFrom(prev)
	if !equals(eventNames(got), []string{"discovery_step", "task_status", "worker_health"}) {
		t.Fatalf("diff events = %v, want all three", eventNames(got))
	}
	if data, ok := got[2].data.(map[string]any); !ok || len(data["signals"].([]map[string]any)) != 0 {
		t.Fatalf("cleared worker_health must carry an empty signals list, got %#v", got[2].data)
	}
}

// task_status 只在真有可报任务时发:receipt 优先(与 TaskView 同口径,
// replayed 归一为 succeeded),其次 running_task_id;两者皆无 → 静默。
func TestTaskStatusOf(t *testing.T) {
	view := map[string]any{"issue_id": "i-1", "step": 2, "running_task_id": nil}
	state := &discovery.State{
		IssueID: "i-1",
		Idempotency: map[string]any{
			"k-1": map[string]any{"status": "replayed"},
		},
	}
	task, sig := taskStatusOf(state, view, "k-1")
	if task == nil || task["status"] != "succeeded" || sig != "k-1|succeeded" {
		t.Fatalf("receipt task = %#v sig=%q, want succeeded", task, sig)
	}

	running := "run-9"
	view["running_task_id"] = &running
	task, sig = taskStatusOf(state, view, "")
	if task == nil || task["task_id"] != "run-9" || task["status"] != "running" || sig != "run-9|running" {
		t.Fatalf("running task = %#v sig=%q, want run-9/running", task, sig)
	}

	view["running_task_id"] = nil
	if task, _ = taskStatusOf(state, view, ""); task != nil {
		t.Fatalf("nothing to report must yield nil task, got %#v", task)
	}
}

// 健康信号与 stalls.go 同语义:错误步块 = failed;分档已过而计划迟到 = stalled。
func TestHealthSignalsOf(t *testing.T) {
	old := time.Now().Add(-20 * time.Minute)
	state := &discovery.State{
		Analysis:       map[string]any{"error": map[string]any{"message": ""}},
		Classification: map[string]any{"summary": "ok"},
		Approval:       map[string]any{"state": "approved"},
		UpdatedAt:      old,
	}
	view := map[string]any{"step": 4}
	signals := healthSignalsOf(state, view)
	if len(signals) != 2 {
		t.Fatalf("signals = %#v, want failed + stalled", signals)
	}
	raw, err := json.Marshal(signals)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"kind":"failed","message":"步骤执行失败(未给出原因)","step":1},{"kind":"stalled","message":"分档已批准但计划超过 15 分钟未生成,执行者疑似中断","step":4}]`
	if string(raw) != want {
		t.Fatalf("signals json = %s, want %s", raw, want)
	}
}
