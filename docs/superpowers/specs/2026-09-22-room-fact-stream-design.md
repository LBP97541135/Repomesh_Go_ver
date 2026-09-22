# 房间即叙事：平台事实发进房间，右侧面板改成房间流

- 日期：2026-09-22
- 状态：待评审
- 范围：跨模块（`discovery` / `coordinator` / `host-executor` / `roomnotice` / `web` / 前端工作台）

## 1. 目标

把**平台已经知道的事实**（派工、任务结果、测试证据、门事件、审批、物化）作为**消息发进对应的 AgentTeams 房间**，并让工作台右侧显示这些房间的流：

1. 右侧面板 = **Manager → Leader** 的对话流（派工与门事件）。
2. 点进 Leader = **Leader ↔ Worker** 的房间流（任务下发、结果、证据）。
3. 之前以卡片呈现的信息，改成这些流里的消息。

**非目标**（本版不做）：

- 不做 agent 之间的自然语言闲聊注入。我们只发**平台事实**；agent 自己在房间里说什么，原样透传显示。
- 不做消息编辑/删除/回复线程（Matrix 侧能力够，但界面这一版不需要）。
- 不改 AgentTeams 的 room 生命周期（建房、加人仍归既有路径）。

## 2. 现状（读准了再动）

| 事实 | 现状 |
|---|---|
| 表 | `public.repository_teams`：`team_room_id`（Leader+Worker 团队房）、`leader_dm_room_id`（Leader 直聊房）、`project_id`、`repository_id`（扫描侧）、`leader_id`、`leader_resource_name` |
| 投递口 | `internal/roomnotice`：`Notify(ctx, issueID, txnID, body)` → `deliver` → `roomForIssue(issueID)`，**只投团队房**，且一个 issue 只挑一间（`ORDER BY s.repository_id`）。幂等靠 `txnID`（Matrix 事务 id），异步 goroutine + 20s 封顶，失败只记一行 stderr |
| 房间读面 | `GET /api/issues/{id}/rooms` → `GetIssueRooms`，返回 `{main, leaders[]}`，每项带 `roomId/availability/canEnter/repositoryId`；`GET /api/issues/{id}/rooms/{roomId}/messages` 读 Matrix，鉴权复用 `roomBelongsToIssue` |
| 前端 | `RoomViewContainer` + `RoomView` 已能渲染一间房（轮询、不可见跳过、失败保留旧内容）；挂在 `#/issues/{id}/rooms/{roomId}`。工作台右侧走的是 `fetchMainRoomConversation` + 一堆卡片 |
| 缺口 | ① 投递口只知道团队房，**不认识 leader_dm_room_id**；② 读面 `leaders[]` 只给团队房，**没给 Leader DM 房**（前端注释里已经写明"等它给出来再区分，不猜"）；③ 事实的写点散在协调器/执行器/discovery 里，各自知道自己的事，没人往房间写 |

## 3. 契约：事件 → 房间 → 幂等键

**唯一出口**：所有事实都经过 `roomnotice`，不新造第二个"讲故事"的循环。理由：房间时间线必须与台账同源，否则两套叙事对不上，人不知道该信哪个。

| 事件 | 房间 | 触发写点 | 幂等键 |
|---|---|---|---|
| Manager 派工（某仓的任务下发） | 该仓 **团队房** | coordinator 派 DAG 任务时 | `task:<taskID>:dispatch` |
| Leader 收到任务（Manager→Leader） | 该仓 **Leader DM 房** | 同上（同一事务后） | `task:<taskID>:assigned` |
| 任务结果（开发/测试 agent 结束） | 该仓 **团队房** | host-executor `MarkAgentExited` 之后 / coordinator 收 run 时 | `run:<runID>:exited` |
| 测试证据（单点/节点级/跨仓） | 该仓 **团队房** | 测试证据落库后 | `evidence:<evidenceID>` |
| 门事件（选仓门开、查漏有漏、计划完成/失败） | 该仓 **Leader DM 房** | discovery 门写路径（已在发，扩到 DM 房与按仓选房） | `gate:<issueID>:<event>` |
| ③ 分档审批结果、⑤ 物化结果 | 该仓 **Leader DM 房** | discovery 审批/物化写路径 | `approval:<issueID>:<evidenceVersion>` / `materialize:<issueID>:<planID>` |
| 计划变更（重排/新版本） | 该仓 **Leader DM 房** | 计划落库后 | `plan:<issueID>:v<revision>` |

**选房规则**：事件若绑定仓库 → 投该仓库的房；不绑定仓库（issue 级事件）→ 投该 issue 名下**全部**仓库房（每仓一条，幂等键带 repo 后缀），保证每间房的时间线自洽。房不存在（`team_room_id`/`leader_dm_room_id` 为空）→ 跳过并记一行，**不建占位房**。

**消息形状**（v1 只用文本，不引入结构化 payload）：

```
[派工] ts-notification-service · 修复通用邮件接口的契约与错误语义
负责人：repomesh-r-…-leader
任务：task_…（批次 1）
```

- 首行是 `[类型] 主语 · 摘要`，类型集合固定：`派工 / 任务结果 / 测试证据 / 门 / 审批 / 物化 / 计划`。前端按它给气泡上色，**不解析正文**。
- 正文只写**台账里有的事实**：谁、什么、结果、指向（任务号/证据号/退出码）。不写推测、不写鼓励语。
- 长度上限 4KB（Matrix 事件够用，超出截断并在末行注明）。

升级路径（不在本版）：把类型与结构化字段放进 Matrix 事件的自定义键（`com.repomesh.fact`），前端按结构渲染富气泡。现在不做，因为读面目前只回 `m.room.message` 的 `body`。

## 4. 后端改动

### 4.1 `internal/roomnotice`：加"按公开目标投递"

保持 `Notify(issueID, txnID, body)` 不动（既有调用零改动），新增：

```go
// RoomTarget 选房：绑定仓库时投该仓的房；RepositoryID 为空表示 issue 级（投全部仓房）。
type RoomTarget struct {
    IssueID      string
    RepositoryID string // 项目侧 repo_… id
    Kind         RoomKind // RoomTeam | RoomLeaderDM
}
func (n *Notifier) NotifyRoom(ctx context.Context, target RoomTarget, txnID, body string)
```

`roomTargetsForIssue` 复用现有 `repositoryteams.RepoTeamResolutionQuery` 的解析（项目侧 `repo_…` → 扫描侧 id），按 `(project_id, 扫描侧 repository_id)` 查 `repository_teams` 取 `team_room_id`/`leader_dm_room_id`。

### 4.2 写点接线

| 写点 | 位置（现状） | 改动 |
|---|---|---|
| 门事件、审批、物化 | discovery 写路径（已在调 `rooms.Notify`） | 改成 `NotifyRoom(target, …)`，`Kind=RoomLeaderDM`，带仓库 |
| 派工 / 收 run | coordinator（`ledger.go` 派工与回收） | 新增两处调用：派工 → `RoomTeam`，同事务后 → `RoomLeaderDM` |
| run 结束 | host-executor / coordinator 收 run | 按 run 的 `task_package_ref` 找到 `(issue, repository)` → `RoomTeam` |
| 测试证据 | 证据落库处 | `RoomTeam` |

**约束**：所有调用都必须是**写库成功之后**的投递，且不得让投递失败影响主流程（保持现有 `Notify` 的异步语义）。投递不参与事务——矩阵不可用不该回滚台账。

### 4.3 读面：把 Leader DM 房暴露出来

`GetIssueRooms` 现在对每个 leader 只回团队房。改成把两间房都回出来，形状向后兼容：

```json
{ "main": {...},
  "leaders": [ { "repositoryId": "...", "roomId": "<team>", "leaderDmRoomId": "<dm>",
                 "availability": "ready", "canEnter": true } ] }
```

`roomBelongsToIssue` 必须同时认这两间房（否则消息端点会把 DM 房判成 404）。

## 5. 前端改动

`FocusPanel` 右侧按左树选中节点决定渲染哪条流：

| 选中 | 右侧显示 |
|---|---|
| Manager / 总览 | Manager 会话（现状保留） |
| 某仓库任务 / Leader | 该仓 **Leader DM 房**流（Manager→Leader 派工、门、审批） |
| 流里点 Leader，或左树 Leader 节点 | 该仓 **团队房**流（Leader↔Worker），带返回 |

- 复用 `fetchRoomStream` + `RoomView` 的消息渲染，不重写。
- **可点的门**（批准分档 / 补仓 / 重试）保持"消息带按钮"，沿用选仓门与 ③ 那条消息的做法；按钮调用的端点不变。

  ⚠ 说清楚一件事：**按钮不在 Matrix 消息里**，也做不到——Matrix 消息只承载文本。流里其实混着两类条目：

  | 类别 | 来源 | 是否可点 |
  |---|---|---|
  | 事实消息 | 房间里的真实消息（后端投递 + agent 自己说的话） | 否，纯文本气泡 |
  | 门控件 | **前端按 discovery 读模型就地渲染**（现状做法，`ChatRow/ChatCard`） | 是，带按钮 |

  v1 的排布规则：**门控件钉在流顶部**（"待你处理"），下面按时间显示房间消息；两者不按时间穿插。理由是门控件没有可靠的房间时间戳可比，硬穿插会出现"审批按钮跑到派工消息下面"这种误导。P2 若要实现穿插，先给门事件补一个真实时间戳来源，再排序。

- **卡片退役**：任务结果、测试证据、分支校验、责任案例这些卡片撤掉，信息改由房间消息承载。删卡片的同一提交里补上流里的**空态说明**（"这间房还没有消息 / 读不到房间"两种要分开说，不能混成"没有消息"）。
- 轮询沿用 `RoomViewContainer` 的既有约束（不可见跳过、上一轮未回不叠加、失败保留旧内容）。

## 6. 错误处理与降级

| 情形 | 行为 |
|---|---|
| 房不存在 / 未 join | 跳过投递，记一行；**不建房、不假装发过** |
| Matrix 不可用 | 投递失败只记日志，主流程照常；房间时间线缺一条，台账仍是事实源 |
| 读面读不到（超时/401） | 界面显示"读不到房间"并保留旧内容，**不显示成"还没有消息"** |
| 事件重复触发 | `txnID` 幂等（Matrix 认第一个） |

## 7. 测试

- **后端（真库）**：`NotifyRoom` 的选房（绑定仓库→该仓；issue 级→全部仓；房为空→跳过）+ 幂等键重复投递只落一条 + 消息正文含台账事实。
- **读面**：`GetIssueRooms` 回两间房；`roomBelongsToIssue` 认 DM 房；DM 房越权（别的 issue 的房）仍 404。
- **前端**：`tsc` + `vite build`；右侧面板按选中节点切流的映射（纯函数化后单测）。

## 8. 分期

1. **P1（本 spec 的实现范围）**：`NotifyRoom` + 三处写点（门/审批/物化 → DM 房；派工与 run 结束 → 团队房）+ 读面暴露 DM 房 + 右侧面板切流 + 卡片退役。
2. **P2**：测试证据、计划变更进房；其余卡片清干净。
3. **P3**：结构化事实（`com.repomesh.fact`）+ 富气泡。

**PR 顺序**（跨模块改动的边界约定：先定契约再共享实现）：

1. `internal/roomnotice` 的 `NotifyRoom` + 选房单测（含真库用例）——**只加不接线**，既有行为零变化。
2. 读面暴露 `leaderDmRoomId` + `roomBelongsToIssue` 认 DM 房 + 越权用例。
3. 三个写点接线（discovery 先，再 coordinator）。
4. 前端切流 + 卡片退役。

每一步都能单独发布、单独回滚；第 1、2 步上线后房间与读面就绪，但**界面还不看**，所以中途停下也不会出现半截叙事。

## 9. 未决 / 风险

- **多仓 issue 的 DM 房语义**：Manager 对每个仓都有一个 Leader，所以"Manager→Leader"是**每仓一条**。若产品上希望有一个总览房，需要另立房间——本版按现成的 `leader_dm_room_id` 走。
- **消息量与噪音**：47 个仓 × 每任务两条派工消息，房间会很长。若实测太吵，P2 用"批次聚合一条"降噪。
- **卡片退役的边界**：DAG 胶囊/任务树是**导航**，不退役；只有"信息展示"类卡片退役。这一条容易扩权，实现时以本节表格为准。
