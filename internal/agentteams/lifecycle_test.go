package agentteams

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// 选仓门 C1(spec §3.3):团队在仓库接入项目时以**休眠 worker** 预建,选进范围时
// 唤醒。这里锁住两条上游语义(httpstub,不碰真控制面):
//
//   - 建休眠 worker 必须是**一步**:POST /api/v1/workers 的请求体原生带
//     "state":"Sleeping"(上游 CreateWorkerRequest.State / types.go 的
//     `State *string`),绝不是"建完再 sleep"——那会先真起一个 runtime 再停它;
//   - ensure-ready **只对 Sleeping/Stopped 生效**(上游 lifecycle_handler.go:
//     其它相位它什么都不做、原样返回当前相位),判定没生效就必须回退 POST wake
//     (wake 是无条件把 spec.state 置 Running 的强动作)。

// capturedRequest 记录一次进来的请求(方法/路径/解码后的 JSON 体)。
type capturedRequest struct {
	Method string
	Path   string
	Body   map[string]any
}

// requestRecorder 是会记下每条请求的假控制器,按序存进切片。
type requestRecorder struct {
	mu       sync.Mutex
	requests []capturedRequest
}

func (r *requestRecorder) record(method, path string, body []byte) {
	captured := capturedRequest{Method: method, Path: path}
	if len(body) > 0 {
		_ = json.Unmarshal(body, &captured.Body)
	}
	r.mu.Lock()
	r.requests = append(r.requests, captured)
	r.mu.Unlock()
}

func (r *requestRecorder) snapshot() []capturedRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]capturedRequest(nil), r.requests...)
}

// TestCreateWorkerSleepingPostsStateSleeping:建休眠 worker 的请求体必须是
// 恰好 {"name":...,"state":"Sleeping"} 一次 POST —— 一次请求正是"不是建完再
// sleep"的判定:若实现先 CreateWorker(默认 Running)再补一发 /sleep,这里会看到
// 两条请求而立刻红。
func TestCreateWorkerSleepingPostsStateSleeping(t *testing.T) {
	recorder := &requestRecorder{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		if r.ContentLength > 0 {
			_, _ = r.Body.Read(body)
		}
		recorder.record(r.Method, r.URL.Path, body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	data, status, err := (&Client{BaseURL: upstream.URL, Token: "sa-token"}).
		CreateWorkerSleeping(context.Background(), "repomesh-r-abc-leader")
	if err != nil {
		t.Fatalf("CreateWorkerSleeping: %v", err)
	}
	if status != http.StatusCreated {
		t.Fatalf("status=%d; want 201", status)
	}
	if len(data) != 2 { // `{}` —— 只证明拿到了响应体,不约束内容
		t.Fatalf("data=%q", data)
	}

	requests := recorder.snapshot()
	if len(requests) != 1 {
		t.Fatalf("建休眠 worker 必须恰好一次请求,实得 %d 次:%+v", len(requests), requests)
	}
	first := requests[0]
	if first.Method != http.MethodPost || first.Path != "/api/v1/workers" {
		t.Fatalf("请求形状不对:%s %s;want POST /api/v1/workers", first.Method, first.Path)
	}
	if first.Body["name"] != "repomesh-r-abc-leader" {
		t.Fatalf("name=%v;want repomesh-r-abc-leader", first.Body["name"])
	}
	if first.Body["state"] != "Sleeping" {
		t.Fatalf("state=%v;want Sleeping(上游建 worker 原生支持,一步建出休眠)", first.Body["state"])
	}
	if len(first.Body) != 2 {
		t.Fatalf("请求体应只含 name 与 state 两个字段,实得:%v", first.Body)
	}
}

// ensureReadyScript 描述假控制器对 ensure-ready / wake 的两次回应。
type ensureReadyScript struct {
	ensureStatus int    // POST ensure-ready 的 HTTP 状态码
	ensureBody   string // POST ensure-ready 的响应体(上游回 {name, phase})
	wakeStatus   int    // POST wake 的 HTTP 状态码
	wakeBody     string // POST wake 的响应体
}

// runEnsureReadyOrWake 起一个按脚本回应的假控制器,跑一次 EnsureReadyOrWake。
func runEnsureReadyOrWake(t *testing.T, script ensureReadyScript) (string, error, *requestRecorder) {
	t.Helper()
	recorder := &requestRecorder{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		if r.ContentLength > 0 {
			_, _ = r.Body.Read(body)
		}
		recorder.record(r.Method, r.URL.Path, body)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/workers/w-1/ensure-ready":
			w.WriteHeader(script.ensureStatus)
			_, _ = w.Write([]byte(script.ensureBody))
		case r.URL.Path == "/api/v1/workers/w-1/wake":
			w.WriteHeader(script.wakeStatus)
			_, _ = w.Write([]byte(script.wakeBody))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(upstream.Close)

	phase, err := (&Client{BaseURL: upstream.URL}).
		EnsureReadyOrWake(context.Background(), "w-1")
	return phase, err, recorder
}

func ensureReadyPaths(recorder *requestRecorder) []string {
	paths := []string{}
	for _, request := range recorder.snapshot() {
		paths = append(paths, request.Path)
	}
	return paths
}

func contains(haystack []string, needle string) bool {
	for _, item := range haystack {
		if item == needle {
			return true
		}
	}
	return false
}

// worker 不在 Sleeping/Stopped 时,上游 ensure-ready 是**空操作**(原样返回当前
// 相位)——这时必须回退 POST wake。
func TestEnsureReadyOrWakeFallsBackWhenEnsureIsNoOp(t *testing.T) {
	phase, err, recorder := runEnsureReadyOrWake(t, ensureReadyScript{
		ensureStatus: http.StatusOK,
		ensureBody:   `{"name":"w-1","phase":"Pending"}`,
		wakeStatus:   http.StatusOK,
		wakeBody:     `{"name":"w-1","phase":"Running"}`,
	})
	if err != nil {
		t.Fatalf("EnsureReadyOrWake: %v", err)
	}
	if phase != "Running" {
		t.Fatalf("phase=%q;want Running(回退 wake 后拿到的)", phase)
	}
	paths := ensureReadyPaths(recorder)
	if !contains(paths, "/api/v1/workers/w-1/ensure-ready") {
		t.Fatalf("应先打 ensure-ready,实际请求:%v", paths)
	}
	if !contains(paths, "/api/v1/workers/w-1/wake") {
		t.Fatalf("ensure-ready 空操作后必须回退 wake,实际请求:%v", paths)
	}
}

// 上游 409(backend ErrConflict)同样说明 ensure-ready 没吃下去,也要回退 wake。
func TestEnsureReadyOrWakeFallsBackOnConflict(t *testing.T) {
	phase, err, recorder := runEnsureReadyOrWake(t, ensureReadyScript{
		ensureStatus: http.StatusConflict,
		ensureBody:   `{"error":"worker backend conflict"}`,
		wakeStatus:   http.StatusOK,
		wakeBody:     `{"name":"w-1","phase":"Running"}`,
	})
	if err != nil {
		t.Fatalf("EnsureReadyOrWake: %v", err)
	}
	if phase != "Running" {
		t.Fatalf("phase=%q;want Running", phase)
	}
	if !contains(ensureReadyPaths(recorder), "/api/v1/workers/w-1/wake") {
		t.Fatalf("409 后必须回退 wake,实际请求:%v", ensureReadyPaths(recorder))
	}
}

// ensure-ready 生效(相位已是 Running/Ready)时**不得**再打 wake —— wake 是
// 无条件强动作,多余的一次会给控制面添无谓的 reconcile。
func TestEnsureReadyOrWakeSkipsWakeWhenEnsureWorked(t *testing.T) {
	for _, effective := range []string{"Running", "Ready"} {
		t.Run(effective, func(t *testing.T) {
			phase, err, recorder := runEnsureReadyOrWake(t, ensureReadyScript{
				ensureStatus: http.StatusOK,
				ensureBody:   `{"name":"w-1","phase":"` + effective + `"}`,
				wakeStatus:   http.StatusOK,
				wakeBody:     `{"name":"w-1","phase":"Running"}`,
			})
			if err != nil {
				t.Fatalf("EnsureReadyOrWake: %v", err)
			}
			if phase != effective {
				t.Fatalf("phase=%q;want %q", phase, effective)
			}
			paths := ensureReadyPaths(recorder)
			if contains(paths, "/api/v1/workers/w-1/wake") {
				t.Fatalf("ensure-ready 已生效就不该再 wake,实际请求:%v", paths)
			}
			if len(paths) != 1 {
				t.Fatalf("应只发一次 ensure-ready,实际请求:%v", paths)
			}
		})
	}
}

// 两条路都不通时必须如实报错 —— 调用方(coordinator 派活)要把失败写进 run 的
// 失败原因,不能拿到一个假装成功的空相位。
func TestEnsureReadyOrWakeErrorsWhenBothPathsFail(t *testing.T) {
	phase, err, _ := runEnsureReadyOrWake(t, ensureReadyScript{
		ensureStatus: http.StatusServiceUnavailable,
		wakeStatus:   http.StatusInternalServerError,
	})
	if err == nil {
		t.Fatalf("ensure-ready 503 + wake 500 必须返回错误,实得 phase=%q", phase)
	}
}
