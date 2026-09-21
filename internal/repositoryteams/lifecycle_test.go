package repositoryteams

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"repomesh.local/repomesh/internal/agentteams"
	"repomesh.local/repomesh/internal/testdb"
)

// 选仓门 C2(spec §3.3):团队在仓库**接入项目**时预建(单仓/批量都建),worker
// 一律 Sleeping;选进 issue 范围时唤醒(fire-and-forget)。这里锁三件事:
//
//   - 建即 Sleeping:远端收到的建 worker 请求体必须带 "state":"Sleeping";
//   - 批量接入逐仓串行、间隔可配(线上 2s),单仓失败记 WARN 不阻断其余;
//   - 唤醒对全队成员先 ensure-ready,没生效回退 wake,且绝不占调用方时间。
//
// 第一个用例不需要数据库(任何环境都跑);其余是真实 PostgreSQL 集成用例
// (testdb.Open,未配 REPOMESH_TEST_DATABASE_URL 时按仓库惯例跳过)。

// ---- 假控制器 ----

// lifecycleRequest 是假控制器记下的一条请求。
type lifecycleRequest struct {
	Method string
	Path   string
	Body   map[string]any
}

// lifecycleController 是可脚本化的 AgentTeams 替身:默认对一切写请求回 2xx、
// 对 worker 状态查询回 Running;可以指定"哪些 worker 名字的**创建**要失败"
// (验单仓失败不阻断),也可以脚本化 ensure-ready / wake 的回应(验回退语义)。
type lifecycleController struct {
	mu         sync.Mutex
	requests   []lifecycleRequest
	failCreate map[string]bool // worker 名字 → 创建时回 503

	ensureStatus int
	ensureBody   string
	wakeStatus   int
	wakeBody     string

	// 每条 ensure-ready 请求先进 park 频道、再等 release(验 fire-and-forget)。
	park   chan struct{}
	release chan struct{}
}

func newLifecycleController() *lifecycleController {
	return &lifecycleController{
		failCreate:   map[string]bool{},
		ensureStatus: http.StatusOK,
		ensureBody:   `{"name":"","phase":"Running"}`,
		wakeStatus:   http.StatusOK,
		wakeBody:     `{"name":"","phase":"Running"}`,
	}
}

func (c *lifecycleController) client(t *testing.T) *agentteams.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		if r.ContentLength > 0 {
			_, _ = r.Body.Read(body)
		}
		captured := lifecycleRequest{Method: r.Method, Path: r.URL.Path}
		if len(body) > 0 {
			_ = json.Unmarshal(body, &captured.Body)
		}
		c.mu.Lock()
		c.requests = append(c.requests, captured)
		c.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/workers":
			if name, _ := captured.Body["name"].(string); c.failCreate[name] {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"error":"controller overloaded"}`))
				return
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/ensure-ready"):
			if c.park != nil {
				c.park <- struct{}{}
				<-c.release
			}
			w.WriteHeader(c.ensureStatus)
			_, _ = w.Write([]byte(c.ensureBody))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/wake"):
			w.WriteHeader(c.wakeStatus)
			_, _ = w.Write([]byte(c.wakeBody))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/status"):
			_, _ = w.Write([]byte(`{"status":{"phase":"Running"}}`))
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(server.Close)
	return &agentteams.Client{BaseURL: server.URL, Token: "test-token"}
}

func (c *lifecycleController) snapshot() []lifecycleRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]lifecycleRequest(nil), c.requests...)
}

// workerCreates 挑出所有"建 worker"请求(POST /api/v1/workers)。
func (c *lifecycleController) workerCreates() []lifecycleRequest {
	creates := []lifecycleRequest{}
	for _, request := range c.snapshot() {
		if request.Method == http.MethodPost && request.Path == "/api/v1/workers" {
			creates = append(creates, request)
		}
	}
	return creates
}

func (c *lifecycleController) countPath(method, suffix string) int {
	count := 0
	for _, request := range c.snapshot() {
		if request.Method == method && strings.HasSuffix(request.Path, suffix) {
			count++
		}
	}
	return count
}

// ---- 用例 1:建即 Sleeping(无需数据库)----

// createRemoteWorker 是本服务建远端 worker 的唯一通道(建队、扩编都走它)——
// 换成休眠建之后,远端收到的请求体必须带 "state":"Sleeping",而且**只发一次**
// 请求(不是建完 Running 再补一发 /sleep)。
func TestCreateRemoteWorkerCreatesSleepingWorker(t *testing.T) {
	controller := newLifecycleController()
	service := &Service{client: controller.client(t)}

	if err := service.createRemoteWorker(context.Background(), "repomesh-r-x-leader"); err != nil {
		t.Fatalf("createRemoteWorker: %v", err)
	}

	creates := controller.workerCreates()
	if len(creates) != 1 {
		t.Fatalf("建 worker 应恰好一次请求,实得 %d 次:%+v", len(creates), creates)
	}
	if creates[0].Body["name"] != "repomesh-r-x-leader" {
		t.Fatalf("name=%v", creates[0].Body["name"])
	}
	if creates[0].Body["state"] != "Sleeping" {
		t.Fatalf("state=%v;want Sleeping(建完的 worker 一律休眠,选进范围时再唤醒)", creates[0].Body["state"])
	}
	if len(creates[0].Body) != 2 {
		t.Fatalf("请求体应只含 name 与 state,实得:%v", creates[0].Body)
	}
}

// ---- 夹具(数据库用例)----

// multiRepositoryFixture 造"一个项目接入 N 个仓库"的场景,返回**项目侧** id
// 列表与对应的**扫描侧** id 列表(两序一致)。扫描侧 URL 与项目侧按
// RepoTeamResolutionQuery 的规则对齐(https://github.com/owner/name)。
func multiRepositoryFixture(t *testing.T, pool *pgxpool.Pool, projectID string, fullNames ...string) (projectSide, scanSide []string) {
	t.Helper()
	const organizationID = "5e2b7c4d-4444-4444-8444-5e2b7c4d4444"
	fixture := testdb.SeedProject(t, pool, projectID, organizationID, fullNames...)
	for i, fullName := range fullNames {
		// 扫描侧 id:32 位十六进制文本,每个仓库一个互不相同的重复字节。
		scanID := strings.Repeat(string(rune('0'+i+1)), 32)
		if _, err := pool.Exec(context.Background(), `
			INSERT INTO repomesh_scan.repositories (id, name, url, organization_id)
			VALUES ($1, $2, $3, $4::uuid)`,
			scanID, fullName, "https://github.com/"+fullName, organizationID); err != nil {
			t.Fatalf("植入扫描侧仓库 %s 失败:%v", fullName, err)
		}
		projectSide = append(projectSide, fixture.Repositories[fullName])
		scanSide = append(scanSide, scanID)
	}
	return projectSide, scanSide
}

// shortenEnsureInterval 把逐仓间隔调到 1ms(默认 2s 是给线上控制面的喘息,
// 不是语义本身),用完恢复。
func shortenEnsureInterval(t *testing.T) {
	t.Helper()
	original := ensureInterval
	ensureInterval = time.Millisecond
	t.Cleanup(func() { ensureInterval = original })
}

// ---- 用例 2:批量接入 → 每仓一队,全 Sleeping ----

func TestEnsureForRepositoriesBuildsSleepingTeamsInBatch(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	shortenEnsureInterval(t)

	const projectID = "c1a21b3d-5555-4555-8555-c1a21b3d5555"
	projectSide, _ := multiRepositoryFixture(t, pool, projectID, "acme/alpha", "acme/beta", "acme/gamma")
	controller := newLifecycleController()
	service := New(pool, controller.client(t))

	outcomes := service.EnsureForRepositories(ctx, projectID, projectSide, 1)
	if len(outcomes) != 3 {
		t.Fatalf("3 个仓库应有 3 条结果,实得 %d:%+v", len(outcomes), outcomes)
	}
	for i, outcome := range outcomes {
		if outcome.RepositoryID != projectSide[i] {
			t.Fatalf("结果 %d 的仓库 id 对不上:%q;want %q", i, outcome.RepositoryID, projectSide[i])
		}
		if outcome.Err != nil {
			t.Fatalf("仓库 %s 建队失败:%v", outcome.RepositoryID, outcome.Err)
		}
		if !outcome.Created {
			t.Fatalf("仓库 %s 应建出一支队", outcome.RepositoryID)
		}
	}

	// 每队 leader + w-0001(默认每队 1 名执行者)→ 共 6 个 worker,
	// 每个请求体都必须带 state=Sleeping。
	creates := controller.workerCreates()
	if len(creates) != 6 {
		t.Fatalf("3 队 × (leader+w-0001) 应建 6 个 worker,实得 %d", len(creates))
	}
	for i, create := range creates {
		if create.Body["state"] != "Sleeping" {
			t.Fatalf("第 %d 个建 worker 请求 state=%v;want Sleeping(建完的 worker 一律休眠)", i, create.Body["state"])
		}
	}

	var teams int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM public.repository_teams WHERE project_id = $1`, projectID).Scan(&teams); err != nil {
		t.Fatalf("回读团队数失败:%v", err)
	}
	if teams != 3 {
		t.Fatalf("应恰好 3 支队,实得 %d", teams)
	}
}

// ---- 用例 3:单仓失败不阻断其余 ----

// 第二个仓库的 leader 创建被控制器拒绝(503):这一个仓的结果要带错误,但
// 第一、第三个仓照建 —— 接入不能被单个仓的 AgentTeams 配额/过载拖死。
func TestEnsureForRepositoriesContinuesAfterSingleFailure(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	shortenEnsureInterval(t)

	const projectID = "c2b32c4e-6666-4666-8666-c2b32c4e6666"
	projectSide, scanSide := multiRepositoryFixture(t, pool, projectID, "acme/one", "acme/two", "acme/three")
	controller := newLifecycleController()
	controller.failCreate[remotePrefix(projectID, scanSide[1])+"-leader"] = true
	service := New(pool, controller.client(t))

	outcomes := service.EnsureForRepositories(ctx, projectID, projectSide, 1)
	if len(outcomes) != 3 {
		t.Fatalf("3 个仓库应有 3 条结果(失败也要有结果),实得 %d", len(outcomes))
	}
	if outcomes[0].Err != nil || !outcomes[0].Created {
		t.Fatalf("第一仓不该被第二仓的失败阻断:%+v", outcomes[0])
	}
	if outcomes[1].Err == nil {
		t.Fatal("第二仓(leader 创建被拒)的结果必须带错误 —— 记 WARN 靠它")
	}
	if outcomes[1].Created {
		t.Fatal("第二仓没建成,不该谎报 Created")
	}
	if outcomes[2].Err != nil || !outcomes[2].Created {
		t.Fatalf("第三仓不该被第二仓的失败阻断:%+v", outcomes[2])
	}

	var teams int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM public.repository_teams WHERE project_id = $1`, projectID).Scan(&teams); err != nil {
		t.Fatalf("回读团队数失败:%v", err)
	}
	if teams != 2 {
		t.Fatalf("失败的仓不落库,应恰好 2 支队,实得 %d", teams)
	}
}

// ---- 用例 4:逐仓间隔(串行限速)----

func TestEnsureForRepositoriesSpacesRepositoriesByInterval(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	original := ensureInterval
	ensureInterval = 120 * time.Millisecond
	t.Cleanup(func() { ensureInterval = original })

	const projectID = "c3c43d5f-7777-4777-8777-c3c43d5f7777"
	projectSide, _ := multiRepositoryFixture(t, pool, projectID, "acme/pause1", "acme/pause2", "acme/pause3")
	service := New(pool, newLifecycleController().client(t))

	start := time.Now()
	outcomes := service.EnsureForRepositories(ctx, projectID, projectSide, 1)
	elapsed := time.Since(start)

	if len(outcomes) != 3 {
		t.Fatalf("3 个仓库应有 3 条结果,实得 %d", len(outcomes))
	}
	// 3 个仓 = 2 段间隔,每段至少 ensureInterval → 下界留 20% 余量。
	if minElapsed := 2 * 120 * time.Millisecond * 8 / 10; elapsed < minElapsed {
		t.Fatalf("逐仓串行应有间隔:3 仓耗时 %v,不足下界 %v(限速没生效)", elapsed, minElapsed)
	}
}

// ---- 用例 5:唤醒 —— 全队成员先 ensure-ready,没生效回退 wake ----

func TestWakeTeamWakesEveryMember(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const projectID = "c4d54e6a-8888-4888-8888-c4d54e6a8888"
	projectSide, scanSide := multiRepositoryFixture(t, pool, projectID, "acme/wake")
	controller := newLifecycleController()
	service := New(pool, controller.client(t))
	if _, err := service.Create(ctx, projectID, scanSide[0], 1); err != nil {
		t.Fatalf("建队失败:%v", err)
	}
	prefix := remotePrefix(projectID, scanSide[0])
	leader, worker := prefix+"-leader", prefix+"-w-0001"

	t.Run("ensure-ready 生效就不再 wake", func(t *testing.T) {
		controller.mu.Lock()
		controller.requests = nil
		controller.mu.Unlock()

		if err := service.wakeTeam(ctx, projectID, projectSide[0]); err != nil {
			t.Fatalf("wakeTeam: %v", err)
		}
		if got := controller.countPath(http.MethodPost, "/api/v1/workers/"+leader+"/ensure-ready"); got != 1 {
			t.Fatalf("leader(%s) 应恰好一次 ensure-ready,实得 %d 次", leader, got)
		}
		if got := controller.countPath(http.MethodPost, "/api/v1/workers/"+worker+"/ensure-ready"); got != 1 {
			t.Fatalf("w-0001(%s) 应恰好一次 ensure-ready,实得 %d 次", worker, got)
		}
		if got := controller.countPath(http.MethodPost, "/wake"); got != 0 {
			t.Fatalf("ensure-ready 已生效(Running)就不该再 wake,实得 %d 次", got)
		}
	})

	t.Run("ensure-ready 空操作时回退 wake", func(t *testing.T) {
		controller.mu.Lock()
		controller.requests = nil
		controller.ensureBody = `{"name":"","phase":"Pending"}`
		controller.mu.Unlock()

		if err := service.wakeTeam(ctx, projectID, projectSide[0]); err != nil {
			t.Fatalf("wakeTeam: %v", err)
		}
		if got := controller.countPath(http.MethodPost, "/wake"); got != 2 {
			t.Fatalf("两个成员都该回退 wake,实得 %d 次", got)
		}
	})

	t.Run("两条路都失败要如实报错", func(t *testing.T) {
		controller.mu.Lock()
		controller.requests = nil
		controller.ensureStatus = http.StatusServiceUnavailable
		controller.wakeStatus = http.StatusInternalServerError
		controller.mu.Unlock()

		if err := service.wakeTeam(ctx, projectID, projectSide[0]); err == nil {
			t.Fatal("ensure-ready 503 + wake 500 必须返回错误,不许假装唤醒成功")
		}
	})
}

// ---- 用例 6:唤醒是 fire-and-forget,不占调用方时间 ----

func TestWakeTeamsForRepositoriesReturnsImmediately(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const projectID = "c5e65f7b-9999-4999-8999-c5e65f7b9999"
	projectSide, scanSide := multiRepositoryFixture(t, pool, projectID, "acme/async")
	controller := newLifecycleController()
	service := New(pool, controller.client(t))
	if _, err := service.Create(ctx, projectID, scanSide[0], 1); err != nil {
		t.Fatalf("建队失败:%v", err)
	}

	// 控制器对每条 ensure-ready:先报"进来了",再原地等 release —— 唤醒请求
	// 只要开始处理,调用方就必须已经拿到返回。
	controller.mu.Lock()
	controller.requests = nil
	controller.park = make(chan struct{}, 4)
	controller.release = make(chan struct{})
	controller.mu.Unlock()

	returned := make(chan struct{})
	go func() {
		service.WakeTeamsForRepositories(ctx, projectID, projectSide[0])
		close(returned)
	}()

	select {
	case <-controller.park:
	case <-time.After(10 * time.Second):
		t.Fatal("后台唤醒迟迟没发出 ensure-ready(10s)")
	}
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("WakeTeamsForRepositories 阻塞了调用方 —— 唤醒必须 fire-and-forget")
	}

	close(controller.release)
	deadline := time.After(10 * time.Second)
	for {
		controller.mu.Lock()
		count := 0
		for _, request := range controller.requests {
			if request.Method == http.MethodPost && strings.HasSuffix(request.Path, "/ensure-ready") {
				count++
			}
		}
		controller.mu.Unlock()
		if count >= 2 {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("后台应把两个成员都唤醒,10s 后只见 %d 次 ensure-ready", count)
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// 用例 6 之外再钉一条:没配库/客户端时唤醒必须是纯 no-op(组合根的低频循环
// 也会碰到它,不能打远端)。
func TestWakeTeamsWithoutPoolOrClientIsNoOp(t *testing.T) {
	(&Service{}).WakeTeamsForRepositories(context.Background(), "p", "r") // 不 panic、不发请求
	if err := (&Service{}).wakeTeam(context.Background(), "p", "r"); err != nil && !errors.Is(err, ErrControllerUnavailable) {
		t.Fatalf("wakeTeam: %v", err)
	}
}
