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
import { defaultClient, type GoIssueDetail } from "./client";
import { resolveDataSourceMode } from "./source";
import { resolveProjectId } from "./issues";
import { shortId } from "../display";
import { repositoryEnvFromAggregate } from "../viewmodel";
import { listConversationMessages, type ConversationMessage } from "./conversations";
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
    state: "open",
    phase: "plan",
    phase_note: "",
    round_count: 0,
    active_round_id: null,
    latest_round_id: null,
    pending_decision_count: 0,
    pending_planning: true,
    repository_count: d.repositoryIds.length,
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
    repositories: d.repositoryIds.map((id) => ({
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
  // Go as-built 房间读面是 {main:{availability,roomId,…}} 基线：运行时观察器
  // 未接线，roomId 恒 null → 没有可进房间；Python 形状 {rooms:[…]} 落地前按空态。
  const res = (await defaultClient().listRooms(issueId, pid)) as Partial<RoomListResponse>;
  return res.rooms ?? [];
}

/** 房间消息流。live 走 Go as-built 的会话消息端点
 *  `GET /api/projects/{projectId}/conversations/{conversationId}/messages`——
 *  `/rooms/{roomId}/stream` 后端未实现;conversationId 由房间清单自带
 *  (Go 读面字段 conversationId)。replay 仍按 roomId 走夹具。 */
export async function fetchRoomStream(
  roomId: string,
  conversationId?: string | null,
  cursor?: string,
): Promise<RoomStreamPage> {
  if (resolveDataSourceMode() === "replay") {
    return roomStreamFixtures[roomId] ?? EMPTY_STREAM;
  }
  if (!conversationId) return EMPTY_STREAM;
  const pid = await resolveProjectId();
  if (!pid) return EMPTY_STREAM;
  const page = await listConversationMessages(pid, conversationId, {
    cursor,
    limit: ROOM_STREAM_LIMIT,
  });
  return {
    next_cursor: page.nextCursor,
    items: page.items.map((m) => conversationToStreamItem(m, roomId)),
  };
}

/** 会话消息 → 房间流条目(契约 §5.2 形状;Go 未存的任务/仓库指针留空)。 */
function conversationToStreamItem(m: ConversationMessage, roomId: string): RoomStreamItemView {
  return {
    at: m.createdAt,
    source: "message",
    room_id: roomId,
    message: {
      id: m.id,
      kind: "conversation_message",
      subject: "",
      body: m.body,
      sender_agent_id: m.actorId,
      sender_name: m.actorId,
      recipient_agent_id: "",
      recipient_name: null,
      repository_id: null,
      task_id: null,
      room_id: roomId,
    } as RoomStreamItemView["message"],
    text: m.body,
    repository_id: null,
    task_id: null,
    payload_ref: m.id,
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
