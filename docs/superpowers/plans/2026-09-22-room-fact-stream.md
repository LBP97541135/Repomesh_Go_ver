# 房间即叙事（平台事实发进房间）实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把平台事实（派工、任务结果、门事件、审批、物化）作为消息发进对应的 AgentTeams 房间，并让工作台右侧面板显示这些房间的流（Manager→Leader / Leader↔Worker）。

**Architecture:** 复用既有的 `internal/roomnotice` 单一投递口，新增"按公开目标投递"（`NotifyRoom`）：绑定仓库 → 该仓的房，issue 级 → 该仓名下全部房。读面把 Leader DM 房暴露出来；工作台右侧按左树选中节点在三条流之间切换；原来的信息卡片退役，信息由房间消息承载。投递始终在写库成功之后异步进行，且不参与事务——房间不可用不该回滚台账。

**Tech Stack:** Go（pgx / 标准库）、PostgreSQL、React + TypeScript（Vite）、AgentTeams（Matrix）房间。

**Spec:** `docs/superpowers/specs/2026-09-22-room-fact-stream-design.md`

## Global Constraints

- 领域层不依赖 FastAPI/HTTP 客户端/数据库/vendor SDK；应用服务依赖端口，基础设施实现端口（`AGENTS.md`）。
- **投递不参与事务、不能让业务链路失败**：`Notify`/`NotifyRoom` 无返回值，goroutine + 20s 封顶，失败只记一行 stderr（`internal/roomnotice/roomnotice.go` 现有三条规矩，别丢）。
- **幂等键**：`txnID` 是 Matrix 事务 id，同一逻辑事件重投只认第一条。issue 级事件投多间房时，每间房的键**必须不同**（带仓库后缀），否则第二间房会被上游当重复吞掉。
- **选房标识统一用项目侧 `repo_…` id**（各写点都有；0057 起 `repository_teams` 键是 `(project_id, 扫描侧 id)`，必须走 URL 对齐解析，直接 join 永远命不中）。
- **房不存在就跳过**：`team_room_id` / `leader_dm_room_id` 为空时不投、不建占位房、不假装发过。
- 迁移号唯一（`TestEmbeddedMigrationsLoad` 会拦重号）；本计划**不含迁移**。
- 提交前跑 `ruff check .`（Python 侧，本项目无改动）+ `go build ./...` + 相关包 `go test`；前端跑 `npx tsc -b` 与 `npx vite build`。
- 本地无 Docker 时真库用例会跳过；真库验证在服务器上跑（`REPOMESH_TEST_DATABASE_URL`，每个用例建 `repomesh_b02_it_*` 临时库）。

---

## 文件结构

> **本计划只做 P1**（spec §8）。**测试证据进房**与**计划变更进房**是 P2，不在本计划的任何任务里；P3 的结构化事实（`com.repomesh.fact`）也不做。别在本计划里顺手加。

| 文件 | 责任 | 改动 |
|---|---|---|
| `internal/roomnotice/roomnotice.go` | 事实投递口：选房 + 发送 | 新增 `RoomTarget`/`RoomKind`/`NotifyRoom`/`roomTargetsFor`，既有 `Notify` 不动 |
| `internal/roomnotice/roomnotice_test.go` | 选房与幂等键 | 新增用例 |
| `internal/issues/query.go` | 房间读面投影 | `issueRoom`/`RoomObservation`/`RepositoryRoom` 加 `LeaderDMRoomID`；`loadIssueRooms` 多查一列；`GetIssueRooms` 填字段 |
| `internal/web/issue_queries.go` | 房间越权判定 | `roomBelongsToIssue` 认 DM 房 |
| `internal/web/issue_queries_test.go` | 越权用例 | 新增 |
| `internal/discovery/plan.go`、`gate.go` 的既有 `rooms.Notify` 调用点 | 门/审批/物化事实 | 改成 `NotifyRoom(… Kind=RoomLeaderDM)` |
| `cmd/repomesh-coordinator/ledger.go` | 派工与收 run | 新增两处 `NotifyRoom`（派工→团队房；同事务后→DM 房） |
| `frontend/src/api/rooms.ts` | 读面客户端 | `goRoomsToViews` 认识 `leaderDmRoomId` |
| `frontend/src/pages/workbench/roomStreamModel.ts` | 选流映射（纯函数） | 新建 |
| `frontend/src/pages/workbench/FocusPanel.tsx` | 右侧面板 | 挂房间流、门控件钉顶、卡片退役 |
| `frontend/src/data/roomStream.ts` | 回放夹具 | 新建（三条流的可验证状态） |

---

## Phase 1：roomnotice 契约（只加不接线）

### Task 1: `NotifyRoom` 与选房

**Files:**
- Modify: `internal/roomnotice/roomnotice.go`
- Test: `internal/roomnotice/roomnotice_test.go`

**Interfaces:**
- Consumes: 既有 `Notifier.deliver(ctx, roomID, txnID, body)`（把 issue 换成 roomID 的那层薄封装，本任务里抽出来）。
- Produces:
  - `type RoomKind int`；`const ( RoomTeam RoomKind = iota; RoomLeaderDM )`
  - `type RoomTarget struct { IssueID string; RepositoryID string; Kind RoomKind }`（`RepositoryID` 为项目侧 `repo_…`；空 = issue 级）
  - `func (n *Notifier) NotifyRoom(ctx context.Context, target RoomTarget, txnID, body string)`
  - 内部：`func (n *Notifier) roomsForTarget(ctx context.Context, target RoomTarget) ([]string, error)`

- [ ] **Step 1: 写失败的用例（选房规则）**

`internal/roomnotice/roomnotice_test.go` 追加（真库，沿用 `testdb.Open`；房号用假值即可，本任务只验**选房**，不验发送）：

```go
// 选房规则：绑定仓库 → 只投该仓；issue 级 → 该仓名下全部仓；房为空 → 跳过。
func TestRoomsForTargetSelectsRooms(t *testing.T) {
	pool := testdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	// 造一个 issue，范围里两个仓；只给其中一个仓种团队房与 DM 房。
	issueID, teamRepo, plainRepo := seedRoomFixture(t, ctx, pool)
	n := New(pool, nil) // 只测选房，不需要 matrix

	rooms, err := n.roomsForTarget(ctx, RoomTarget{IssueID: issueID, RepositoryID: teamRepo, Kind: RoomTeam})
	if err != nil {
		t.Fatal(err)
	}
	if len(rooms) != 1 || rooms[0] != "!team-room:local" {
		t.Fatalf("绑定仓库应只命中该仓的团队房，got %v", rooms)
	}

	dm, err := n.roomsForTarget(ctx, RoomTarget{IssueID: issueID, RepositoryID: teamRepo, Kind: RoomLeaderDM})
	if err != nil {
		t.Fatal(err)
	}
	if len(dm) != 1 || dm[0] != "!dm-room:local" {
		t.Fatalf("绑定仓库应命中该仓的 Leader DM 房，got %v", dm)
	}

	all, err := n.roomsForTarget(ctx, RoomTarget{IssueID: issueID, Kind: RoomTeam})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("issue 级只应含有房的仓（另一个仓没房），got %v", all)
	}

	none, err := n.roomsForTarget(ctx, RoomTarget{IssueID: issueID, RepositoryID: plainRepo, Kind: RoomTeam})
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Fatalf("没房的仓要跳过，got %v", none)
	}
}
```

`seedRoomFixture` 需要：一个 project + account（`testdb.SeedProject`）、两个仓库（`SeedProject` 已建 `repomesh_projects.repositories` + `project_repositories`）、issue 范围两行（`repomesh_issues.issue_repository_scope`）、`repomesh_scan.repositories` 两行（URL 与 `r.host/owner/name` 对齐，供 URL 对齐解析命中）、`public.repository_teams` 一行（`team_room_id='!team-room:local'`、`leader_dm_room_id='!dm-room:local'`）。

- [ ] **Step 2: 跑用例确认它失败**

Run: `go test ./internal/roomnotice/ -run TestRoomsForTargetSelectsRooms -v`
Expected: FAIL —— `n.roomsForTarget undefined`（本地无 Docker 时是 SKIP，那就上服务器跑：见 Global Constraints）

- [ ] **Step 3: 实现选房与投递**

在 `roomnotice.go` 里加（`roomsForTarget` 是 `roomForIssue` 的推广，SQL 结构一致，只多一个房间列与可选仓库过滤）：

```go
type RoomKind int

const (
	RoomTeam RoomKind = iota
	RoomLeaderDM
)

// RoomTarget 选房：RepositoryID 为空表示 issue 级（投该 issue 名下全部有房的仓）。
type RoomTarget struct {
	IssueID      string
	RepositoryID string // 项目侧 repo_… id
	Kind         RoomKind
}

func (k RoomKind) column() string {
	if k == RoomLeaderDM {
		return "leader_dm_room_id"
	}
	return "team_room_id"
}

// roomsForTarget 返回要投的房间号（顺序稳定：按 s.repository_id）。
// 空的房号一律跳过——房还没建/还没回读到时不当成错误，也不投。
func (n *Notifier) roomsForTarget(ctx context.Context, target RoomTarget) ([]string, error) {
	column := target.Kind.column()
	query := `
		SELECT COALESCE(NULLIF(t.` + column + `, ''), '')
		FROM repomesh_issues.issue_repository_scope s
		JOIN repomesh_projects.project_repositories pr
		  ON pr.project_id = s.project_id AND pr.repository_id = s.repository_id
		JOIN repomesh_projects.repositories r ON r.id = pr.repository_id
		JOIN repomesh_projects.projects p ON p.id = pr.project_id
		JOIN repomesh_access.accounts a ON a.id = p.owner
		LEFT JOIN LATERAL (
		  SELECT scan.id FROM repomesh_scan.repositories scan
		  WHERE scan.organization_id = a.organization_id
		    AND lower(regexp_replace(rtrim(scan.url, '/'), '\.git$', '')) IN
		      (lower('https://' || r.host || '/' || r.owner || '/' || r.name),
		       lower('http://' || r.host || '/' || r.owner || '/' || r.name),
		       lower('git@' || r.host || ':' || r.owner || '/' || r.name))
		  ORDER BY scan.profiled_at DESC, scan.id LIMIT 1
		) sc ON true
		JOIN public.repository_teams t
		  ON t.project_id = s.project_id AND t.repository_id = sc.id
		WHERE s.issue_id = $1 AND ($2 = '' OR s.repository_id = $2)
		ORDER BY s.repository_id`
	rows, err := n.pool.Query(ctx, query, target.IssueID, target.RepositoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	rooms := []string{}
	for rows.Next() {
		var roomID string
		if err := rows.Scan(&roomID); err != nil {
			return nil, err
		}
		if roomID != "" {
			rooms = append(rooms, roomID)
		}
	}
	return rooms, rows.Err()
}

// NotifyRoom 与 Notify 同一套规矩（异步、封顶、失败只记一行），区别只有选房。
// 多间房时每间的幂等键都不同——同一个键投第二间会被上游当重复吞掉。
func (n *Notifier) NotifyRoom(ctx context.Context, target RoomTarget, txnID, body string) {
	if n == nil || n.matrix == nil || n.pool == nil {
		return
	}
	detached := context.WithoutCancel(ctx)
	go func() {
		sendCtx, cancel := context.WithTimeout(detached, 20*time.Second)
		defer cancel()
		rooms, err := n.roomsForTarget(sendCtx, target)
		if err != nil {
			fmt.Fprintf(os.Stderr, "roomnotice: room lookup failed issue=%s repo=%s: %v\n",
				target.IssueID, target.RepositoryID, err)
			return
		}
		for _, roomID := range rooms {
			key := txnID
			if len(rooms) > 1 {
				key = txnID + ":" + roomID
			}
			if _, err := n.matrix.SendMessage(sendCtx, roomID, key, body); err != nil {
				fmt.Fprintf(os.Stderr, "roomnotice: send failed room=%s: %v\n", roomID, err)
			}
		}
	}()
}
```

`Notify` 保持原样（既有调用零改动）；`deliver`/`roomForIssue` 也保留（`Notify` 还在用）。

- [ ] **Step 4: 跑用例确认通过**

Run: `go test ./internal/roomnotice/ -run TestRoomsForTargetSelectsRooms -v` → PASS
Run: `go build ./...` → 无输出

- [ ] **Step 5: 提交**

```bash
git add internal/roomnotice/roomnotice.go internal/roomnotice/roomnotice_test.go
git commit -m "feat(roomnotice): 按公开目标投递 —— 团队房 / Leader DM 房，issue 级投全部房"
```

---

## Phase 2：读面暴露 Leader DM 房

### Task 2: `GetIssueRooms` 带回 `leaderDmRoomId`

**Files:**
- Modify: `internal/issues/query.go:506-520`（类型）、`:600-640`（SQL）、`:559-590`（投影）
- Modify: `internal/web/issue_queries.go:161-172`（越权判定）
- Test: `internal/web/issue_queries_test.go`

**Interfaces:**
- Consumes: 无。
- Produces: JSON 多一个字段 `leaders[].leaderDmRoomId`（`*string`，可空）；`roomBelongsToIssue` 同时认团队房与 DM 房。

- [ ] **Step 1: 写失败的用例**

`internal/web/issue_queries_test.go` 追加（真库；沿用该文件既有夹具风格）：

```go
// DM 房也是这个 issue 的房：消息端点必须认它，否则 Leader 房的流一律 404。
func TestRoomBelongsToIssueAcceptsLeaderDMRoom(t *testing.T) {
	dm := "!dm:local"
	team := "!team:local"
	view := issues.RoomsView{
		IssueID: "iss_1",
		Leaders: []any{issues.RepositoryRoom{
			RepositoryID:   "repo_1",
			RoomID:         &team,
			LeaderDMRoomID: &dm,
			Availability:   "ready",
		}},
	}
	if !roomBelongsToIssue(view, dm) {
		t.Fatal("Leader DM 房应被认作属于该 issue")
	}
	if !roomBelongsToIssue(view, team) {
		t.Fatal("团队房仍应被认作属于该 issue")
	}
	if roomBelongsToIssue(view, "!someone-else:local") {
		t.Fatal("别的房不能被认下")
	}
}
```

- [ ] **Step 2: 跑用例确认失败**

Run: `go test ./internal/web/ -run TestRoomBelongsToIssueAcceptsLeaderDMRoom -v`
Expected: FAIL —— `unknown field LeaderDMRoomID`

- [ ] **Step 3: 加字段与查询列**

`query.go` 类型：`RepositoryRoom` 与 `RoomObservation` 各加

```go
	// LeaderDMRoomID 是这间仓的 Leader 直聊房（Manager→Leader）。可空：房还没建
	// 或还没回读到就为空，界面按"没有这间房"处理，不编房号。
	LeaderDMRoomID *string `json:"leaderDmRoomId,omitempty"`
```

`issueRoom` 加 `leaderDMRoomID string`；`loadIssueRooms` 的 SQL 多取一列：

```sql
SELECT s.repository_id, COALESCE(repo.name, ''), COALESCE(NULLIF(t.team_room_id, ''), ''),
       COALESCE(NULLIF(t.leader_dm_room_id, ''), '')
```

Scan 到 `&room.leaderDMRoomID`；投影处（`RepositoryRoom` 与 index==0 的 `RoomObservation` 两处）填：

```go
	var dm *string
	if room.leaderDMRoomID != "" {
		dm = &room.leaderDMRoomID
	}
```

保留既有过滤（`room.roomID != ""` 才收）——两间房是建团队时一起回读的，为空的 DM 房不代表要单列一个只有 DM 房的仓；这条规则写进 `loadIssueRooms` 的注释。

`issue_queries.go` 的越权判定加一条：

```go
		if room.LeaderDMRoomID != nil && *room.LeaderDMRoomID == roomID {
			return true
		}
```

- [ ] **Step 4: 跑用例确认通过 + 读面回归**

Run: `go test ./internal/web/ -run 'TestRoomBelongsTo|Rooms' -v` → PASS
Run: `go test ./internal/issues/ ./internal/web/ -count=1`（真库在服务器上跑）→ ok

- [ ] **Step 5: 提交**

```bash
git add internal/issues/query.go internal/web/issue_queries.go internal/web/issue_queries_test.go
git commit -m "feat(rooms): 读面带回 leaderDmRoomId，房间鉴权认 Leader DM 房"
```

---

## Phase 3：写点接线

### Task 3: discovery 的门/审批/物化改投 Leader DM 房

**Files:**
- Modify: `internal/discovery/plan.go`（审批、物化处的 `rooms.Notify`）、`internal/discovery/gate.go`（门事件若有）、`internal/discovery/service.go`（`Issues.Rooms` 的装配类型）

**Interfaces:**
- Consumes: `roomnotice.RoomTarget`/`RoomKind`（Task 1）。
- Produces: discovery 侧事实进 DM 房；正文首行格式 `[门] …` / `[审批] …` / `[物化] …`。

- [ ] **Step 1: 写失败的用例**

Matrix 在测试环境里没有，所以把"组正文"抽成纯函数再测——投递本身不测（它是副作用，且已有 `Notify` 的老用例覆盖"投不出去不影响调用方"）：

```go
func TestFactBodyFormat(t *testing.T) {
	body := FactBody(FactApproval, "结算流程改造", "分档已批准（4 必需 / 8 可能）")
	if !strings.HasPrefix(body, "[审批] ") {
		t.Fatalf("首行必须是 [类型] 开头，got %q", body)
	}
	if !strings.Contains(body, "结算流程改造") {
		t.Fatal("正文要带主事实（issue 标题），否则房间里看不出说的是哪件事")
	}
}
```

- [ ] **Step 2: 跑用例确认失败**

Run: `go test ./internal/discovery/ -run TestFactBodyFormat -v` → FAIL（`FactBody` 未定义）

- [ ] **Step 3: 实现 `FactBody` 并改调用点**

新增 `internal/discovery/fact.go`：

```go
// 事实类型：首行标签，前端按它给气泡上色，不解析正文。
const (
	FactDispatch  = "派工"
	FactResult    = "任务结果"
	FactEvidence  = "测试证据"
	FactGate      = "门"
	FactApproval  = "审批"
	FactMaterialize = "物化"
	FactPlan      = "计划"
)

// FactBody 组一条事实消息：[类型] 主语 · 摘要 + 明细行。
// 只写台账里有的事实（谁/什么/结果/指向），不写推测与鼓励语。
func FactBody(kind, subject, summary string, lines ...string) string {
	head := "[" + kind + "] " + subject
	if summary != "" {
		head += " · " + summary
	}
	out := []string{head}
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	body := strings.Join(out, "\n")
	if len(body) > 4096 {
		body = body[:4096] + "\n…（超长截断）"
	}
	return body
}
```

调用点（`plan.go` 审批与物化处、`gate.go` 门事件处）把 `rooms.Notify(ctx, issueID, key, body)` 换成：

```go
	rooms.NotifyRoom(ctx, roomnotice.RoomTarget{
		IssueID: issueID, RepositoryID: repoID, Kind: roomnotice.RoomLeaderDM,
	}, key, FactBody(FactApproval, issueTitle, "分档已批准", "证据指纹："+evidenceVersion))
```

`repositoryID`：审批处用 `st.EffectiveTiers` 里第一个 required/maybe 的仓（没有就留空 → 走 issue 级投全部房）；物化处用 plan 的 `repositories[0]`。

- [ ] **Step 4: 跑用例 + 回归**

Run: `go test ./internal/discovery/ -run TestFactBody -v` → PASS
Run: `go test ./internal/discovery/ -count=1`（服务器真库）→ ok

- [ ] **Step 5: 提交**

```bash
git add internal/discovery/fact.go internal/discovery/plan.go internal/discovery/gate.go internal/discovery/fact_test.go
git commit -m "feat(discovery): 门/审批/物化事实投进 Leader DM 房"
```

### Task 4: coordinator 派工与收 run 投团队房 + DM 房

**Files:**
- Modify: `cmd/repomesh-coordinator/ledger.go`（派工 SQL 增列 `scope.repository_id`；派工后两处投递；收 run 处一处投递）

**Interfaces:**
- Consumes: `roomnotice.NotifyRoom`（Task 1）、`discovery.FactBody`（Task 3）。
- Produces: 派工进团队房（`[派工]`）、指派进 DM 房（`[派工] … 负责人`）、run 结束进团队房（`[任务结果]`）。

- [ ] **Step 1: 写失败的用例**

`ledger_test.go` 是纯函数测试，投递是副作用，这里用**消息正文**做断言（同 Task 3 的 `FactBody`）：

```go
func TestDispatchFactBodiesNameTaskAndLeader(t *testing.T) {
	team := discovery.FactBody(discovery.FactDispatch, "ts-notification-service", "修复通用邮件接口",
		"任务：task_1（批次 1）", "负责人：repomesh-r-…-leader")
	if !strings.Contains(team, "负责人：") {
		t.Fatal("团队房那条要说清谁负责，否则 worker 看不出该找谁")
	}
	dm := discovery.FactBody(discovery.FactDispatch, "ts-notification-service", "收到任务 task_1", "批次 1")
	if !strings.Contains(dm, "task_1") {
		t.Fatal("DM 房那条要带任务号，Manager 才知道在说哪一条")
	}
}
```

- [ ] **Step 2: 跑用例确认失败**

Run: `go test ./cmd/repomesh-coordinator/ -run TestDispatchFactBodies -v` → FAIL（未定义或断言不过）

- [ ] **Step 3: 接线**

派工查询（`ledger.go:615`）的 SELECT 增 `scope.repository_id`，扫到 `repoID`；在**写库成功之后**（`tx.Commit` 之后）加两处投递：

```go
	d.rooms.NotifyRoom(ctx, roomnotice.RoomTarget{
		IssueID: issueID, RepositoryID: repoID, Kind: roomnotice.RoomTeam,
	}, "task:"+taskID+":dispatch", teamBody)
	d.rooms.NotifyRoom(ctx, roomnotice.RoomTarget{
		IssueID: issueID, RepositoryID: repoID, Kind: roomnotice.RoomLeaderDM,
	}, "task:"+taskID+":assigned", dmBody)
```

收 run 处（`rk`/回收分支）：按 `run.task_package_ref` 找到 task → `scope.repository_id` 与 `issue_id`，投团队房：

```go
	d.rooms.NotifyRoom(ctx, roomnotice.RoomTarget{
		IssueID: issueID, RepositoryID: repoID, Kind: roomnotice.RoomTeam,
	}, "run:"+runID+":exited", discovery.FactBody(discovery.FactResult, repoFullName,
		"开发 agent 结束", "退出码："+strconv.Itoa(exitCode), "任务："+taskID))
```

- [ ] **Step 4: 跑用例 + 回归**

Run: `go test ./cmd/repomesh-coordinator/ -count=1` → ok（该包多数用例是纯函数，不需要真库）

- [ ] **Step 5: 提交**

```bash
git add cmd/repomesh-coordinator/ledger.go cmd/repomesh-coordinator/ledger_test.go
git commit -m "feat(coordinator): 派工与任务结果分别投团队房 / Leader DM 房"
```

---

## Phase 4：前端

### Task 5: 选流映射（纯函数）+ 读面映射认识 DM 房

**Files:**
- Create: `frontend/src/pages/workbench/roomStreamModel.ts`
- Modify: `frontend/src/api/rooms.ts`（`goRoomsToViews` 认 `leaderDmRoomId`）

**Interfaces:**
- Consumes: `FocusEntry`（`pages/workbench/treeModel.ts`：现有 `mgr | step | task | tests | stage`，本任务**新增 `leader`**）、`PlanTaskItem`（`api/taskTree.ts`：有 `repositoryId?`）、`RoomListItemView`（`api/contract.ts`）。
- Produces:
  - `treeModel.ts`：`FocusEntry` 增 `| { kind: "leader"; repositoryId: string }`
  - `roomStreamModel.ts`：`type StreamTarget = { kind: "manager" } | { kind: "team"; roomId: string } | { kind: "leader_dm"; roomId: string; repositoryId: string }`；`function streamTargetFor(entry, rooms, tasks): StreamTarget`

- [ ] **Step 1: 写实现与自查**

```ts
/** 右侧流的选择：选中节点 → 该看哪条流。
 *
 *  规则（spec §5）：Manager/总览、规划步骤 → Manager 会话；某仓任务 → 该仓的
 *  Leader DM 房（Manager→Leader）；Leader 分组 → 该仓团队房（Leader↔Worker）。
 *  房没回读到（room_id 为空 / 清单里没有这个仓）一律回落 Manager 会话 ——
 *  宁可显示一条已知的流，也不摆一间进不去的房。
 *
 *  task 条目本身不带仓库（FocusEntry 只有 taskId），所以要从任务列表反查
 *  PlanTaskItem.repositoryId —— 这是唯一能把任务落到仓上的来源。 */
export function streamTargetFor(
  entry: FocusEntry | null,
  rooms: RoomListItemView[],
  tasks: PlanTaskItem[] | null,
): StreamTarget {
  if (entry === null || entry.kind === "mgr" || entry.kind === "step" || entry.kind === "tests" || entry.kind === "stage") {
    return { kind: "manager" };
  }
  const repositoryId =
    entry.kind === "leader"
      ? entry.repositoryId
      : (tasks ?? []).find((t) => t.id === entry.taskId)?.repositoryId ?? "";
  if (repositoryId === "") return { kind: "manager" };
  const room = rooms.find((r) => r.repository_id === repositoryId);
  if (!room) return { kind: "manager" };
  if (entry.kind === "leader") {
    return room.room_id ? { kind: "team", roomId: room.room_id } : { kind: "manager" };
  }
  return room.leader_dm_room_id
    ? { kind: "leader_dm", roomId: room.leader_dm_room_id, repositoryId }
    : { kind: "manager" };
}
```

`api/rooms.ts`：`RoomListItemView` 加 `leader_dm_room_id: string | null`；`GoRoomObservation` 加 `leaderDmRoomId?: string | null`；`goRoomsToViews` 的 `push()` 里带上 `leader_dm_room_id: observation.leaderDmRoomId ?? null`。

- [ ] **Step 2: 类型检查**

Run: `cd frontend && npx tsc -b`
Expected: 只剩基线那 3 处 `SupervisionPolicySettings.tsx` 报错

- [ ] **Step 3: 提交**

```bash
git add frontend/src/pages/workbench/roomStreamModel.ts frontend/src/api/rooms.ts frontend/src/api/contract.ts
git commit -m "feat(workbench): 选流映射（选中节点 → Manager / Leader DM / 团队房）"
```

### Task 6: 右侧面板挂流、门控件钉顶、卡片退役

**Files:**
- Modify: `frontend/src/pages/workbench/FocusPanel.tsx`（右侧面板）
- Modify: `frontend/src/pages/workbench/WorkbenchPage.tsx`（把任务列表与房间清单传给 FocusPanel）
- Modify: `frontend/src/pages/workbench/DispatchTree.tsx:258`（Leader 分组头改成可点）

**Interfaces:**
- Consumes: `streamTargetFor`（Task 5）、`fetchRoomStream`/`fetchRooms`（`api/rooms.ts`）、**既有 `RoomViewContainer`**（`pages/RoomViewContainer.tsx`，已经负责轮询/空态/错误态，别重写）。
- Produces: 右侧面板三条流；门控件钉流顶部且**不按时间穿插**；Leader 分组头可点进团队房。

- [ ] **Step 1: 加回放夹具（可验证的状态）**

`data/roomStream.ts` 导出三条流的夹具（`manager` / `leader_dm` / `team` 各一屏消息，含 `[派工]/[任务结果]/[门]` 三种类型），沿用 `data/discovery.ts` 的 `?source=replay` 惯例。

- [ ] **Step 2: Leader 分组头可点**

`DispatchTree.tsx` 第 258 行的分组渲染里，组头那行加 `onClick={() => onOpen({ kind: "leader", repositoryId: items[0]?.repositoryId ?? "" })}`，并给它可点的样式（与既有 task 行一致的 hover/pointer）。`repositoryId` 为空时点击不生效（`streamTargetFor` 会回落 Manager，界面不会跳到空房）。

- [ ] **Step 3: 挂流 + 钉门**

`FocusPanel` 右侧按 `streamTargetFor(activeEntry, rooms, tasks)` 渲染：

```tsx
{target.kind === "manager" ? (
  /* 现状：Manager 会话 + 门控件 */
) : (
  <>
    {/* 门控件钉顶：v1 不按时间穿插（门没有可靠的房间时间戳，硬穿插会出现
        「审批按钮跑到派工消息下面」这种误导 —— spec §5） */}
    {gateControls}
    <RoomViewContainer
      issueId={issueId}
      roomId={target.roomId}
      onBack={() => setActiveEntry({ kind: "mgr" })}
      onToast={onToast}
    />
  </>
)}
```

`rooms` 来自既有的 `fetchRooms(issueId)`；失败时空数组 → `streamTargetFor` 回落 Manager 会话。`tasks` 用面板已持有的任务列表（`WorkbenchPage` 已经在取，透传即可）。

- [ ] **Step 4: 卡片退役**

撤掉右栏里"信息展示"类卡片（任务结果、测试证据、分支校验、责任案例），信息由房间消息承载；**保留** DAG 胶囊/任务树（导航）。空态**沿用 `RoomViewContainer` 的既有措辞**（它已经区分"读不到"与"没有消息"），只在需要时补文案——不要在这里另写一套。

- [ ] **Step 5: 构建 + 回放自检**

Run: `cd frontend && npx tsc -b && npx vite build`
Expected: tsc 只剩 3 处基线报错；vite 退出 0

回放自检：`?source=replay` 下逐个切三条流，确认消息按类型上色、门控件在顶部、两种空态措辞分得开。

- [ ] **Step 6: 提交**

```bash
git add frontend/src/pages/workbench/FocusPanel.tsx frontend/src/pages/workbench/WorkbenchPage.tsx frontend/src/pages/workbench/DispatchTree.tsx frontend/src/data/roomStream.ts
git commit -m "feat(workbench): 右侧改成房间流，Leader 分组可下钻，信息卡片退役"
```

---

## 收尾验证（全部任务完成后）

- [ ] `go build ./...` 无输出；`go test ./internal/roomnotice/ ./internal/issues/ ./internal/web/ ./internal/discovery/ ./cmd/repomesh-coordinator/ -count=1`（真库在服务器跑）全绿
- [ ] `cd frontend && npx tsc -b`（仅基线 3 处）+ `npx vite build` 退出 0
- [ ] 真机走一遍：建项 → ① 分析 → ③ 分档 → 物化 → 打开工作台，确认
      ① 右栏能切到 Leader DM 流并看到 `[派工]/[审批]`；
      ② 下钻 Leader 能看到团队房的 `[派工]/[任务结果]`；
      ③ 房读不到时显示"读不到房间"而不是"还没有消息"
- [ ] 用浏览器直接读一次房间（Matrix `/sync`），确认消息确实落到两间房而不是只落了库里
