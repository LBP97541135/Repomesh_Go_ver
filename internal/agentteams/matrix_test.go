package agentteams

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// 房间消息只认 m.room.message，而且要从旧到新——Matrix 的 dir=b 是反的。
// 状态事件（m.room.create 等）不进对话流：空房间否则看起来像有人说过话。
func TestRoomMessagesKeepsOnlyMessagesOldestFirst(t *testing.T) {
	var gotPath, gotQuery, gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery, gotAuth = r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"rooms":{"join":{"!room:hs":{"timeline":{"events":[{"type":"m.room.message","event_id":"$old","sender":"@me:hs","origin_server_ts":1500,"content":{"body":"第一条"}},{"type":"m.room.create","event_id":"$c","sender":"@admin:hs","content":{}},{"type":"m.room.message","event_id":"$new","sender":"@mgr:hs","origin_server_ts":2000,"content":{"body":"第二条"}}]}}}}}`))
	}))
	defer upstream.Close()

	client := &MatrixClient{BaseURL: upstream.URL, Token: "mx-token", MasqueradeAs: "@admin:hs"}
	messages, err := client.RoomMessages(context.Background(), "!room:hs", 50)
	if err != nil {
		t.Fatalf("RoomMessages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("got %d messages; want 2 (state events must be dropped)", len(messages))
	}
	if messages[0].Body != "第一条" || messages[1].Body != "第二条" {
		t.Fatalf("order=%q,%q; want sync order (oldest first)", messages[0].Body, messages[1].Body)
	}
	if gotPath != "/_matrix/client/v3/sync" {
		t.Fatalf("path=%q; want /sync (Tuwunel 的 /messages 在这些房间只回一条,不可信)", gotPath)
	}
	if !strings.Contains(gotQuery, "timeout=0") || !strings.Contains(gotQuery, "rooms") {
		t.Fatalf("query=%q; want timeout+room filter", gotQuery)
	}
	if !strings.Contains(gotQuery, "user_id=@admin") && !strings.Contains(gotQuery, "user_id=%40admin") {
		t.Fatalf("query=%q; want masquerade user_id (appservice 裸身份不在房里)", gotQuery)
	}
	if gotAuth != "Bearer mx-token" {
		t.Fatalf("auth=%q", gotAuth)
	}
}

// 空房间不是错误：sync 里没有这间房（或 timeline 里没有消息）就返回空切片。
func TestRoomMessagesEmptyRoomIsNotAnError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"rooms":{"join":{"!room:hs":{"timeline":{"events":[{"type":"m.room.create","event_id":"$c","sender":"@a:hs","content":{}}]}}}}}`))
	}))
	defer upstream.Close()

	messages, err := (&MatrixClient{BaseURL: upstream.URL}).RoomMessages(context.Background(), "!empty:hs", 0)
	if err != nil {
		t.Fatalf("RoomMessages: %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("got %d messages; want 0", len(messages))
	}
}

// 读不到就说读不到，把上游状态码带出来：401（凭据不对）和 404（房间不存在）
// 是完全两种故障，笼统一句"读取失败"没法查。
func TestRoomMessagesSurfacesUpstreamFailure(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"errcode":"M_FORBIDDEN"}`))
	}))
	defer upstream.Close()

	_, err := (&MatrixClient{BaseURL: upstream.URL, Token: "stale"}).RoomMessages(context.Background(), "!room:hs", 10)
	if err == nil {
		t.Fatal("want error on 403")
	}
	if !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "M_FORBIDDEN") {
		t.Fatalf("err=%v; want status + upstream body", err)
	}
}

// 空 roomID 不发请求：宁可本地报错，也不要打出一个 /rooms//messages 让上游猜。
func TestRoomMessagesRejectsEmptyRoomID(t *testing.T) {
	called := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer upstream.Close()

	if _, err := (&MatrixClient{BaseURL: upstream.URL}).RoomMessages(context.Background(), "  ", 10); err == nil {
		t.Fatal("want error on empty room id")
	}
	if called {
		t.Fatal("empty room id must not reach the homeserver")
	}
}

// txnID 是幂等键：重试要打到同一个 URL，否则上游会当成两条新消息。
func TestSendMessageCarriesTransactionID(t *testing.T) {
	var gotPath string
	var gotBody map[string]string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"event_id":"$sent"}`))
	}))
	defer upstream.Close()

	eventID, err := (&MatrixClient{BaseURL: upstream.URL, Token: "t"}).
		SendMessage(context.Background(), "!room:hs", "run_dev_1", "开始分析需求…")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if eventID != "$sent" {
		t.Fatalf("eventID=%q", eventID)
	}
	if gotPath != "/_matrix/client/v3/rooms/!room:hs/send/m.room.message/run_dev_1" {
		t.Fatalf("path=%q", gotPath)
	}
	if gotBody["body"] != "开始分析需求…" || gotBody["msgtype"] != "m.text" {
		t.Fatalf("body=%v", gotBody)
	}
}

// 换 Matrix 凭据这条也走控制器；拿不到 token 就必须报错，不能返回空串让
// 调用方拿着空 token 去打 homeserver（那会变成一串难查的 401）。
func TestMatrixTokenRequiresAccessToken(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/credentials/matrix-token" {
			t.Errorf("path=%q", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	if _, _, err := (&Client{BaseURL: upstream.URL, Token: "sa"}).MatrixToken(context.Background()); err == nil {
		t.Fatal("want error when access_token is absent")
	}
}

func TestMatrixTokenReadsAccessToken(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"access_token":"syt_new"}`))
	}))
	defer upstream.Close()

	token, status, err := (&Client{BaseURL: upstream.URL, Token: "sa"}).MatrixToken(context.Background())
	if err != nil {
		t.Fatalf("MatrixToken: %v", err)
	}
	if status != http.StatusOK || token != "syt_new" {
		t.Fatalf("status=%d token=%q", status, token)
	}
}

// sessionFixture 造一对假上游：控制器负责发凭据，homeserver 负责读房间。
//
// 计数器走原子：并发用例会同时打这两个服务，普通 int 在 -race 下会直接报竞争。
type sessionFixture struct {
	controllerCalls atomic.Int64
	homeserverCalls atomic.Int64
	// tokenIssued 是控制器每次发出的凭据序号，用来判断"换没换"。
	tokenIssued  atomic.Int64
	rejectTokens map[string]int // token -> 该 token 撞上的 HTTP 状态（0 = 放行）
}

func (f *sessionFixture) servers(t *testing.T) (*Client, string) {
	t.Helper()
	controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.controllerCalls.Add(1)
		issued := f.tokenIssued.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"access_token":"tok-` + strconv.FormatInt(issued, 10) + `"}`))
	}))
	t.Cleanup(controller.Close)

	homeserver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.homeserverCalls.Add(1)
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if status := f.rejectTokens[token]; status != 0 {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"errcode":"M_UNKNOWN_TOKEN"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"rooms":{"join":{"!room:hs":{"timeline":{"events":[{"type":"m.room.message","event_id":"$m","sender":"@a:hs","origin_server_ts":1,"content":{"body":"ok"}}]}}}}}`))
	}))
	t.Cleanup(homeserver.Close)
	return &Client{BaseURL: controller.URL, Token: "sa"}, homeserver.URL
}

// 控制器给的凭据是**轮换**语义：每读一次房间就换一枚会让别处正在用的旧 token 失效。
// 所以正常路径只换一次，之后一直复用。
func TestMatrixSessionCachesTokenAcrossReads(t *testing.T) {
	fixture := &sessionFixture{rejectTokens: map[string]int{}}
	controller, homeserver := fixture.servers(t)
	session := &MatrixSession{Controller: controller, Homeserver: homeserver}

	for i := 0; i < 3; i++ {
		if _, err := session.RoomMessages(context.Background(), "!room:hs", 10); err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
	}
	if fixture.controllerCalls.Load() != 1 {
		t.Fatalf("controller calls=%d; want 1 (token must be reused, not rotated per read)", fixture.controllerCalls.Load())
	}
}

// 凭据过期是预期内的（上游注释：Workers/Managers 收到 401 时用它换新）。
// 第一枚被拒 → 换一枚重试 → 成功。
func TestMatrixSessionRefreshesTokenOn401AndRetriesOnce(t *testing.T) {
	fixture := &sessionFixture{rejectTokens: map[string]int{"tok-1": http.StatusUnauthorized}}
	controller, homeserver := fixture.servers(t)
	session := &MatrixSession{Controller: controller, Homeserver: homeserver}

	messages, err := session.RoomMessages(context.Background(), "!room:hs", 10)
	if err != nil {
		t.Fatalf("RoomMessages: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("got %d messages; want the retry to succeed", len(messages))
	}
	if fixture.controllerCalls.Load() != 2 {
		t.Fatalf("controller calls=%d; want 2 (initial + one refresh)", fixture.controllerCalls.Load())
	}
}

// 403 是"确实无权"，换多少枚凭据都一样 —— 不能重试，否则只是把故障拖长。
func TestMatrixSessionDoesNotRetryOn403(t *testing.T) {
	fixture := &sessionFixture{rejectTokens: map[string]int{"tok-1": http.StatusForbidden}}
	controller, homeserver := fixture.servers(t)
	session := &MatrixSession{Controller: controller, Homeserver: homeserver}

	if _, err := session.RoomMessages(context.Background(), "!room:hs", 10); err == nil {
		t.Fatal("want error on 403")
	}
	if fixture.controllerCalls.Load() != 1 {
		t.Fatalf("controller calls=%d; want 1 (403 must not trigger a token refresh)", fixture.controllerCalls.Load())
	}
}

// 换不到凭据就如实报错，不能拿空 token 去打 homeserver（那会变成一串难查的 401）。
func TestMatrixSessionFailsWhenControllerCannotIssueToken(t *testing.T) {
	controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer controller.Close()
	homeserver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("homeserver must not be reached without a token")
	}))
	defer homeserver.Close()

	session := &MatrixSession{Controller: &Client{BaseURL: controller.URL}, Homeserver: homeserver.URL}
	if _, err := session.RoomMessages(context.Background(), "!room:hs", 10); err == nil {
		t.Fatal("want error when the controller cannot issue a token")
	}
}

// 并发调用**不能各自去换凭据**。换 token 是轮换语义：第二个去换会把第一个刚拿到的
// 那枚作废，两边于是来回打成 401 —— 越换越坏。所以刷新必须串行、且后到的人复用
// 前一个刚换到的那枚。这条也用 -race 跑，顺带验证锁没写错（当年就写错过一次：
// withToken 里连着两次 Lock，sync.Mutex 不可重入，直接死锁）。
func TestMatrixSessionSharesOneRefreshUnderConcurrency(t *testing.T) {
	fixture := &sessionFixture{rejectTokens: map[string]int{}}
	controller, homeserver := fixture.servers(t)
	session := &MatrixSession{Controller: controller, Homeserver: homeserver}

	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			// 并发里不能用 Fatalf（它只在测试 goroutine 里合法）。
			if _, err := session.RoomMessages(context.Background(), "!room:hs", 10); err != nil {
				t.Errorf("concurrent read: %v", err)
			}
		}()
	}
	group.Wait()

	if calls := fixture.controllerCalls.Load(); calls != 1 {
		t.Fatalf("controller calls=%d; want 1 (concurrent reads must share one refresh, "+
			"each extra refresh invalidates the token the others just got)", calls)
	}
}

// 已经换过一次之后，另一个调用撞上 401 时必须**复用**那枚新的，而不是再换一枚。
func TestMatrixSessionReusesFreshlyRefreshedToken(t *testing.T) {
	fixture := &sessionFixture{rejectTokens: map[string]int{"tok-1": http.StatusUnauthorized}}
	controller, homeserver := fixture.servers(t)
	session := &MatrixSession{Controller: controller, Homeserver: homeserver}

	// 先把 tok-1 换掉（这次 401 触发一次刷新）。
	if _, err := session.RoomMessages(context.Background(), "!room:hs", 10); err != nil {
		t.Fatalf("first read: %v", err)
	}
	after := fixture.controllerCalls.Load()

	// 再来一次：凭据已经是 tok-2，稳过，不应再换。
	if _, err := session.RoomMessages(context.Background(), "!room:hs", 10); err != nil {
		t.Fatalf("second read: %v", err)
	}
	if calls := fixture.controllerCalls.Load(); calls != after {
		t.Fatalf("controller calls grew from %d to %d; want no extra refresh", after, calls)
	}
}

// 直配凭据这条路：配了 MATRIX_ACCESS_TOKEN 就完全不碰控制器（Controller 为 nil
// 也不报错），发送带 ?user_id= 代发身份。这是线上实测出的正路：admin 在控制器
// 那边没有凭据记录，走换凭据必 500。
func TestStaticTokenSkipsControllerAndMasquerades(t *testing.T) {
	var sendPath string
	homeserver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sendPath = r.URL.Path + "?" + r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"event_id":"$s"}`))
	}))
	defer homeserver.Close()

	t.Setenv("MATRIX_HOMESERVER_URL", homeserver.URL)
	t.Setenv("MATRIX_ACCESS_TOKEN", "syt_static")
	t.Setenv("REPOMESH_MATRIX_ACT_AS", "@admin:matrix-local.agentteams.io:18080")

	session := NewSessionFromEnv(nil) // 控制器就是 nil —— 不能因此走不下去
	if session == nil {
		t.Fatal("static token path must not require a controller")
	}
	if _, err := session.SendMessage(context.Background(), "!room:hs", "txn-1", "hi"); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if !strings.Contains(sendPath, "user_id=%40admin%3Amatrix-local") {
		t.Fatalf("send path missing masquerade: %q", sendPath)
	}
}

// 什么都没配 → nil，调用方如实报"没配"，不拿空凭据去打 homeserver。
func TestNewSessionFromEnvNilWhenUnconfigured(t *testing.T) {
	t.Setenv("MATRIX_HOMESERVER_URL", "")
	t.Setenv("MATRIX_ACCESS_TOKEN", "")
	if NewSessionFromEnv(&Client{BaseURL: "http://c:8090"}) != nil {
		t.Fatal("want nil when nothing is configured")
	}
}
