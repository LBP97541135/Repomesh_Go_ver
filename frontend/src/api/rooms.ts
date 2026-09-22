/** issue 详情 / 房间数据源：live | replay，开关沿用 `resolveDataSourceMode()`。
 *  live 打契约 v0.2 §3 / §5.1 / §5.2 / §5.4；replay 走本地夹具。两侧同一契约类型。 */
import type {
  DeliveryEventKind,
  DeliveryEventsPage,
  IssueDetailView,
  PlanGraphEdgeView,
  RepositoryPlanView,
  RoomListItemView,
  RoomListResponse,
  RoomStreamItemView,
  RoomStreamPage,
} from "./contract";
import type { RepositoryEnv } from "../types";
import { defaultClient, type GoIssueDetail, type MatrixRoomMessageView } from "./client";
import { resolveDataSourceMode } from "./source";
import { resolveProjectId } from "./issues";
import { shortId } from "../display";
import { repositoryEnvFromAggregate } from "../viewmodel";
import type { ConversationMessage } from "./conversations";
import {
  ISSUE_DETAIL_FIXTURE_DEFAULT,
  deliveryAggregateFixture,
  issueDetailFixture,
  issueDetailFixtures,
  issueRoomsFixtures,
  repositoryPlanFixture,
  roomStreamFixtures,
  roundEventsFixture,
} from "../data/issueDetail";

/** 回放形态选择：`?issue=<name>`，取值见 data/issueDetail.ts 的夹具表。
 *  **自检开关**（同 discovery.ts 的 `?discovery=`），live 模式下完全不参与取数。
 *  名字打错时不静默回落到默认形态——那会让人以为自己在看 A 其实在看 B。 */
function replayIssueName(): string {
  const name = new URLSearchParams(window.location.search).get("issue");
  if (name && !issueDetailFixtures[name]) {
    throw new Error(`回放夹具没有 issue 形态「${name}」。可选：${Object.keys(issueDetailFixtures).join(" / ")}`);
  }
  return name ?? ISSUE_DETAIL_FIXTURE_DEFAULT;
}

function replayIssueDetail(): IssueDetailView {
  return issueDetailFixtures[replayIssueName()];
}

/** 房间流单页条数：种子每房间 0-5 条，取 50 足够；真实规模由 next_cursor 续读。 */
export const ROOM_STREAM_LIMIT = 50;

/** 轮询间隔（§5.3：v0.2 的刷新机制是前端轮询，SSE 另立项）。
 *  5 秒取自原型标注；页面不可见时跳过一轮，后台标签页不空转打后端。 */
export const ROOM_POLL_MS = 5000;

/** 事件时间线单页条数。取小页是有意的：环境窗是窄栏，小页让「加载后续」的
 *  游标衔接在演示中看得见（CONS-14 的既有取值）。 */
export const ROOM_EVENTS_LIMIT = 6;

const EMPTY_STREAM: RoomStreamPage = { items: [], next_cursor: null };

export async function fetchIssueDetail(issueId: string, projectId?: string): Promise<IssueDetailView> {
  if (resolveDataSourceMode() === "replay") {
    if (issueId !== issueDetailFixture.issue_id) throw new Error(`replay 夹具未覆盖 issue ${shortId(issueId)}`);
    return replayIssueDetail();
  }
  const pid = projectId ?? (await resolveProjectId());
  if (!pid) throw new Error("没有可用项目，无法读取 issue 详情");
  // Go as-built 详情是创建契约 §7 的最小快照（internal/issues/query.go）：
  // 没有 rounds/phase/teams/拓扑读模型。这里把它适配成契约视图的诚实空态，
  // 否则 WorkbenchPage 读 detail.rounds.length 直接 TypeError（整页白屏）。
  const raw = await defaultClient().getIssueDetail(issueId, pid);
  return goIssueDetailToView(raw);
}

/** Go 最小快照 → 契约视图。缺读模型的字段一律取「无轮次、未建团」的基线，
 *  不编造进度；阶段推进（物化后进执行段）由 WorkbenchPage 按发现链收据推导。 */
function goIssueDetailToView(d: GoIssueDetail): IssueDetailView {
  return {
    issue_id: d.id,
    issue_key: null,
    organization_id: null,
    title: d.title,
    requirement_text: d.description,
    document_filename: null,
    // 服务端事实，**必须透出来**：0053 起人审门模式由服务端说了算（建项时写入，
    // 协调器按它停门）。转换函数此前把 `d.hitlMode` 丢掉了，于是工作台永远读到
    // undefined、一律按最保守的 hitl 处理 —— 服务端设成 ai 也照样等人。
    // 缺省/老数据仍按 hitl（与 client.ts 的 GoIssueDetail 注释同一口径）。
    hitlMode: d.hitlMode === "ai" ? "ai" : "hitl",
    state: "open",
    phase: "plan",
    phase_note: "",
    round_count: 0,
    active_round_id: null,
    latest_round_id: null,
    pending_decision_count: 0,
    pending_planning: true,
    // 2026-09-20 建项不选仓：范围改由「选仓门」确认，门确认前 Go 详情可能不带
    // repositoryIds（字段缺省）——不 `?? []` 的话这里直接 TypeError，整页白屏。
    repository_count: (d.repositoryIds ?? []).length,
    team_count: 0,
    plan_version: "",
    operational_status: "active",
    execution_mode: null,
    opened_by_agent_id: null,
    opened_by_name: null,
    opened_at: d.createdAt,
    updated_at: d.createdAt,
    archived: false,
    archived_at: null,
    source: d.source,
    rounds: [],
    repositories: (d.repositoryIds ?? []).map((id) => ({
      repository_id: id,
      // 详情只给 id；显示名由工作台用控制台仓库清单回填（WorkbenchPage）
      name: shortId(id),
      team_id: null,
      role_in_issue: null,
    })),
    teams: [],
    contract: null,
    human_grants: [],
    required_checkpoints: [],
    // 这两个标量 Go 详情读面没有；徽标消费方以发现链专用端点为准（契约 v0.4 §3.3）
    discovery_step: 1,
    discovery_state: "idle",
  };
}

/** §5.1：未建团的 issue 返回空清单且 HTTP 200——空态不是错误，调用方渲染空态。 */
export async function fetchRooms(issueId: string, projectId?: string): Promise<RoomListItemView[]> {
  if (resolveDataSourceMode() === "replay") {
    if (issueId !== issueDetailFixture.issue_id) return [];
    // 每个形态各带自己的房间清单（夹具世界要自洽）：未物化形态没有拓扑就没有团队，
    // 半执行形态建了拓扑但房间没跟上——两者都是 0 房间，却是两回事，不由
    // `repositories.length` 一条规则倒推（那会让半执行形态摆出三仓六房间）。
    return issueRoomsFixtures[replayIssueName()] ?? [];
  }
  const pid = projectId ?? (await resolveProjectId());
  if (!pid) return [];
  const res = await defaultClient().listRooms(issueId, pid);
  // 两种形状都收：Go as-built 是 {issueId, main, leaders[]}，Python 是 {rooms:[…]}。
  //
  // 判据用**有没有 main/leaders**，不是 `rooms` 在不在：`[]` 在 JS 里是真值，
  // 将来后端哪天带上一个空的 rooms 字段，按 `rooms` 优先就会把真实的
  // main/leaders 整个丢掉、静默退回空清单。
  const go = res as GoRoomsView;
  if (go.main || go.leaders) return goRoomsToViews(go, issueId);
  return (res as Partial<RoomListResponse>).rooms ?? [];
}

/** Go as-built 的房间读面（契约 §7）。`availability` 只有 "ready" 时 roomId 才非空。 */
interface GoRoomObservation {
  conversationId?: string;
  availability?: string;
  roomId?: string | null;
  canEnter?: boolean;
  repositoryId?: string;
  repositoryName?: string;
  /** 该仓的 Leader 直聊房（Manager→Leader）。读面带出来才推 leader_dm 那条。 */
  leaderDmRoomId?: string | null;
}

interface GoRoomsView {
  issueId?: string;
  main?: GoRoomObservation;
  leaders?: GoRoomObservation[];
}

/** Go 的 {main, leaders[]} → 契约的 RoomListItemView[]。
 *
 *  只收**真有房**的条目：availability 不是 "ready" 或 roomId 为空的一律不要。
 *  后端在那两种情形下本来就什么都没承诺（unavailable/NOT_ASSOCIATED），
 *  界面摆一间进不去的房比空态更误导。
 *
 *  `message_count` / `last_message` 这一版读面不提供，如实留 0 / null ——
 *  这不是"房间是空的"，是"这项没读"。界面不得把它渲染成"还没有消息"。
 *  （查过了：这两个字段目前没有任何组件渲染，只在夹具里出现。）
 *
 *  `repository_name` 照实传：传 null 的话界面会显示「catalog 未收录」——
 *  一句我们占不了的说法，名字拿不到就说拿不到。 */
function goRoomsToViews(view: GoRoomsView, issueId: string): RoomListItemView[] {
  const rooms: RoomListItemView[] = [];
  const push = (observation: GoRoomObservation | undefined): void => {
    const roomId = observation?.roomId ?? null;
    if (!roomId || observation?.availability !== "ready") return;
    const base = {
      issue_id: issueId,
      team_id: "",
      repository_id: observation?.repositoryId ?? "",
      repository_name: observation?.repositoryName ?? null,
      members: [],
      last_message: null,
      message_count: 0,
      live: false,
      conversation_id: observation?.conversationId ?? null,
    };
    rooms.push({ ...base, room_id: roomId, kind: "team_room" });
    // Leader DM 房是**另一间房**（Manager→Leader 的派工与门事件），不是团队房的
    // 别名：按既有契约（`kind` 由自己标出，见 §7 与 data/issueDetail.ts 的夹具）
    // 单独推一条。读面没给这个房号就不推 —— 不编一间进不去的房。
    const dmRoomId = observation?.leaderDmRoomId ?? null;
    if (dmRoomId) {
      rooms.push({ ...base, room_id: dmRoomId, kind: "leader_dm" });
    }
  };
  push(view.main);
  for (const leader of view.leaders ?? []) push(leader);
  return rooms;
}

/** 房间消息流。live 打 `GET /api/issues/{issueId}/rooms/{roomId}/messages`：
 *  房间在 AgentTeams 的 homeserver 上，由后端代理读取——浏览器不直连 Matrix，
 *  后端也不把凭据下发。replay 仍按 roomId 走夹具。
 *
 *  这一版后端一次给完（上限 200 条），没有游标，所以 `next_cursor` 恒为 null。 */
export async function fetchRoomStream(
  issueId: string,
  roomId: string,
): Promise<RoomStreamPage> {
  if (resolveDataSourceMode() === "replay") {
    return roomStreamFixtures[roomId] ?? EMPTY_STREAM;
  }
  const pid = await resolveProjectId();
  if (!pid) return EMPTY_STREAM;
  const page = await defaultClient().getIssueRoomMessages(issueId, roomId, pid, {
    limit: ROOM_STREAM_LIMIT,
  });
  return {
    next_cursor: null,
    items: page.messages.map((m) => matrixMessageToStreamItem(m, roomId)),
  };
}

/** 该 issue 主房间（第一间真有房的仓库团队房）的消息，映射成会话消息形状 ——
 *  工作台右栏的 Manager 房间直接用它渲染，和原有时间线同一个组件。
 *
 *  返回 null = 还没有可进的房（仓库没建队 / 房间号没回读到）。这**不是**"房间空"，
 *  调用方据此回落到原时间线，不能显示成"还没有消息"。
 *  actorId 取 Matrix 用户名的本地段（@admin:server → admin）：域后缀对界面没信息量。 */
export async function fetchMainRoomConversation(
  issueId: string,
  projectId?: string,
): Promise<ConversationMessage[] | null> {
  const rooms = await fetchRooms(issueId, projectId);
  const main = rooms[0];
  if (!main) return null;
  const pid = projectId ?? (await resolveProjectId());
  if (!pid) return null;
  const page = await defaultClient().getIssueRoomMessages(issueId, main.room_id, pid, {
    limit: ROOM_STREAM_LIMIT,
  });
  return page.messages.map((m, index) => ({
    id: m.eventId,
    sequence: index + 1,
    authorKind: "",
    actorId: m.sender.split(":")[0].replace(/^@/, "") || "repomesh",
    body: m.body,
    createdAt: m.at,
  }));
}

/** 任意一间**属于该 issue 的房**的消息，形状与 fetchMainRoomConversation 完全一致 ——
 *  右栏按选中节点切流（Leader 分组 → 团队房；任务 → Leader DM 房）时用它。
 *
 *  与主房那条的区别只有"取哪间"：那条自己从房间清单挑第一间，这条由调用方给房号
 *  （房号来自清单，所以仍在 issue 边界内）。 */
export async function fetchRoomConversation(
  issueId: string,
  roomId: string,
  projectId?: string,
): Promise<ConversationMessage[] | null> {
  if (roomId === "") return null;
  const pid = projectId ?? (await resolveProjectId());
  if (!pid) return null;
  const page = await defaultClient().getIssueRoomMessages(issueId, roomId, pid, {
    limit: ROOM_STREAM_LIMIT,
  });
  return page.messages.map((m, index) => ({
    id: m.eventId,
    sequence: index + 1,
    authorKind: "",
    actorId: m.sender.split(":")[0].replace(/^@/, "") || "repomesh",
    body: m.body,
    createdAt: m.at,
  }));
}

/** Matrix 房间消息 → 房间流条目(契约 §5.2 形状)。
 *
 *  这些是**真房间消息**，所以 `message` 非 null、可以渲染成聊天气泡
 *  （契约 §5.2：只有真实房间消息才可渲染成气泡）。Go 未存的任务/仓库指针留空。 */
function matrixMessageToStreamItem(m: MatrixRoomMessageView, roomId: string): RoomStreamItemView {
  return {
    at: m.at,
    source: "message",
    room_id: roomId,
    message: {
      id: m.eventId,
      kind: "conversation_message",
      subject: "",
      body: m.body,
      sender_agent_id: m.sender,
      sender_name: m.sender,
      recipient_agent_id: "",
      recipient_name: null,
      repository_id: null,
      task_id: null,
      room_id: roomId,
    } as RoomStreamItemView["message"],
    text: m.body,
    repository_id: null,
    task_id: null,
    payload_ref: m.eventId,
  };
}

export async function fetchRepositoryPlan(issueId: string, repositoryId: string): Promise<RepositoryPlanView> {
  if (resolveDataSourceMode() === "replay") return repositoryPlanFixture;
  return defaultClient().getRepositoryPlan(issueId, repositoryId);
}

/** 迁移 4：取该 issue 某版计划快照的边语义（使用物化收据给出的 plan_id）。
 *
 *  **2026-09-20：这条读面在 Go 后端不存在，所以不再发这个请求。**
 *
 *  它打的是 `GET /api/plans/{planId}/versions/{n}` —— Python 时代的接口，Go 侧
 *  从来没注册过（`/api/plans/...` 在 internal/web 里零命中）。于是工作台每 2.5 秒
 *  发一次、每次收一个 404：nginx 实测 90 秒 49 条，把真故障淹掉。
 *
 *  边语义（interface / agreement 注脚）本来就是**可选加注**：DAG 面靠 §5.4 就能
 *  画完整，拿不到就按"无语义"渲染（与 404 时的行为完全一致，界面没有任何变化）。
 *  等 Go 侧真的把计划快照读面做出来（数据在 public.plans.task_dag->'dag' 里，
 *  nodes/edges 都有），再把这条请求接回去。在那之前，不发一个注定 404 的请求。 */
export async function fetchPlanGraphEdges(
  _planId: string,
  _planVersion: number,
): Promise<PlanGraphEdgeView[] | null> {
  return null;
}

/** 该 issue 的当前轮次。环境窗与事件时间线都是**轮次粒度**的消费面，先解析一次
 *  轮次再各自取数——否则两个面各取一遍 issue 详情，同一个事实请求两次。
 *  纯草稿 issue（无轮次）返回 null，调用方按缺口呈现。 */
export async function fetchRoundId(issueId: string, projectId?: string): Promise<string | null> {
  if (resolveDataSourceMode() === "replay") {
    const fixture = replayIssueDetail();
    return fixture.active_round_id ?? fixture.latest_round_id;
  }
  const pid = projectId ?? (await resolveProjectId());
  if (!pid) return null;
  const detail = await fetchIssueDetail(issueId, pid);
  return detail.active_round_id ?? detail.latest_round_id;
}

/** 环境窗数据：v0.1 交付聚合是**轮次粒度**，环境窗是**单仓作用域**，所以取该轮次的
 *  聚合再切出本仓那一片。聚合取不到时返回 null，窗内显缺口而非假数字。 */
export async function fetchRepositoryEnv(
  roundId: string,
  repositoryId: string,
): Promise<RepositoryEnv | null> {
  if (resolveDataSourceMode() === "replay") {
    return repositoryEnvFromAggregate(deliveryAggregateFixture, repositoryId);
  }
  return repositoryEnvFromAggregate(await defaultClient().getDelivery(roundId), repositoryId);
}

/** 本轮事件时间线（§4.1）。`kind` 是**服务端**单值过滤（全量语义），
 *  `cursor` 不透明原样回传续读。仓库维度**没有服务端过滤**——§4.1 只定义了 kind，
 *  所以按仓的取舍只能在已加载的这一页里做，呈现时必须说明是当页语义（见 RoomView）。 */
export async function fetchRoundEvents(
  roundId: string,
  opts?: { cursor?: string; kind?: DeliveryEventKind },
): Promise<DeliveryEventsPage> {
  if (resolveDataSourceMode() === "replay") {
    const all = roundEventsFixture.items;
    return {
      items: opts?.kind ? all.filter((e) => e.kind === opts.kind) : all,
      next_cursor: null,
    };
  }
  return defaultClient().getEvents(roundId, { ...opts, limit: ROOM_EVENTS_LIMIT });
}
