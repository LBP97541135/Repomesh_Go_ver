/** 人工审核台（`/review-requests` 与项目检查点决策，human_control 面）。
 *
 *  **落在这里而不是 `client.ts`**：这几个端点认本地账号会话，与读模型的动作 token
 *  是两套凭据（见 `api/auth.ts` 顶部为什么不能混用）。决策要记在**谁**名下，
 *  这正是共享 token 换不来的东西——后端把 `actor.id` 直接写进决策行，前端连
 *  `human_principal_id` 这个字段都不用传。
 *
 *  **可见范围按「项目 + 用户」两层隔离**（2026-09-20）：队列只装**当前项目**的单，
 *  且只有项目 owner 或管理员读得到（后端 `List` 带 projectId 时校验归属，否则 404）。
 *  此前是「管理员看全表、非管理员看 assignee=自己」—— 前者是一次跨账号的全库拉取，
 *  后者会因为生产者漏填 assignee 而让单子对**所有人**隐身（连项目属主也看不见）。 */
import { sessionRequest } from "./auth";

/** 六个检查点（后端 `ProjectCheckpoint`）。 */
export type ProjectCheckpoint =
  | "repository_scope"
  | "specification"
  | "execution"
  | "validation"
  | "delivery"
  | "exception_escalation";

/** 审核请求四态（后端 `HumanReviewStatus`）。 */
export type HumanReviewStatus = "pending" | "approved" | "rejected" | "changes_requested";

/** 决策三种（后端 `CheckpointDecisionKind`）。**比状态少一个**：没有 `pending`，
 *  因为决策一旦记下就不是待办。 */
export type CheckpointDecisionKind = "approved" | "rejected" | "changes_requested";

/** 项目控制动作（后端 `HumanControlAction`）。审核台只用得到暂停/恢复/取消三种；
 *  其余四个（`view_decisions` / `approve_checkpoint` / `request_changes` /
 *  `edit_specification`）是授权语义里的**权限名**，不是可发起的动作，故不在此
 *  列举为按钮。完整七项的类型见 `api/humanControl.ts` 的 `HumanControlAction`。 */
export type ProjectControlAction = "pause_project" | "resume_project" | "cancel_project";

export interface HumanReviewRequestView {
  id: string;
  project_id: string;
  checkpoint: ProjectCheckpoint;
  /** 决策必须钉在它实际看到的那份证据上；后端据此判定漂移 */
  evidence_version: string;
  title: string;
  summary: string;
  status: HumanReviewStatus;
  repository_id: string | null;
  requested_by_agent_id: string | null;
  resolved_by_human_id: string | null;
  /** 这条待审**从哪来的**（后端 request_content.origin）。
   *
   *  `discovery` = 发现链的人工步骤（③ 分档审批 / ⑤ 物化确认）在 issue 页面上的
   *  镜像登记：**流水线在 issue 那边推进**，在这个队列里按按钮推不动它。所以界面
   *  对这类条目不给决策按钮，而是给一个指回 issue 的入口 —— 给一个按了没反应的
   *  按钮，比不给按钮更糟。
   *  `pipeline`（或空）= 审核台自己的卡点，在这里决策。 */
  origin: string;
  /** origin=discovery 时的出处 issue；否则空串。 */
  issue_id: string;
  created_at: string;
  updated_at: string;
}

export interface CheckpointDecisionView {
  id: string;
  review_request_id: string;
  project_id: string;
  checkpoint: ProjectCheckpoint;
  human_principal_id: string;
  decision: CheckpointDecisionKind;
  reason: string;
  repository_id: string | null;
  evidence_version: string;
  decided_at: string;
}

/** 待办列表。**必须带 projectId**——这个队列按项目隔离（2026-09-20）。
 *
 *  后端在带 `projectId` 时要求调用者是该项目的 owner，或管理员
 *  （ADR-0022 兜底）；两者都不是一律 404。不带 projectId 只会回**自己名下项目**
 *  的单，所以「切个项目就换个队列」是这条端点本来的语义，不是界面在本地过滤。
 *
 *  `status` 省略 = 全部状态（含已决的），传 `pending` = 只看待办。 */
export function fetchReviewRequests(
  projectId: string,
  status?: HumanReviewStatus,
): Promise<HumanReviewRequestView[]> {
  const params = new URLSearchParams({ projectId });
  if (status) params.set("status", status);
  return sessionRequest<HumanReviewRequestView[]>(`/review-requests?${params.toString()}`);
}

/** 记一条检查点决策。
 *
 *  **决策人不由前端指定**：后端取会话账号写入 `human_principal_id`。这是恢复登录门
 *  换来的东西——共享动作 token 下这一列只能是「某个人」。
 *
 *  409 = 该检查点已有决策或证据已漂移；403 = 该账号在这个项目里没有决策权。
 *  两者 detail 原文上抛，各有各的下一步。 */
export function recordCheckpointDecision(
  projectId: string,
  payload: { review_request_id: string; decision: CheckpointDecisionKind; reason: string },
): Promise<CheckpointDecisionView> {
  return sessionRequest<CheckpointDecisionView>(
    `/projects/${encodeURIComponent(projectId)}/checkpoint-decisions`,
    { method: "POST", body: JSON.stringify(payload) },
  );
}

/** 暂停 / 恢复 / 取消一个项目。403 = 该账号没有这项控制权。 */
export function controlProject(
  projectId: string,
  action: ProjectControlAction,
): Promise<unknown> {
  return sessionRequest<unknown>(`/projects/${encodeURIComponent(projectId)}/control`, {
    method: "POST",
    body: JSON.stringify({ action }),
  });
}

/** 待办的 SSE 流（`event: review-requests`，每 2s 比对、变了才推）。
 *
 *  **按项目订阅**：流与一次性取数共用同一条隔离规则，所以 `projectId` 是必填的
 *  —— 换个项目就得换一条流，调用方负责在项目变化时重订阅（见 ConsoleShell）。
 *
 *  **不带 Authorization 头**——EventSource 本来也不支持自定义头，而这一面认 cookie，
 *  正好合得上。`withCredentials` 只有跨源才需要；控制台走同源 dev proxy，
 *  同源请求默认就带 cookie。
 *
 *  返回取消函数。调用方负责在卸载时调用它，否则连接会一直挂着。 */
export function subscribeReviewRequests(
  projectId: string,
  onData: (rows: HumanReviewRequestView[]) => void,
  onError: () => void,
): () => void {
  const base = import.meta.env.VITE_API_BASE ?? "";
  const source = new EventSource(
    `${base}/api/v1/review-requests/events?projectId=${encodeURIComponent(projectId)}`,
    { withCredentials: true },
  );
  source.addEventListener("review-requests", (event) => {
    try {
      onData(JSON.parse((event as MessageEvent<string>).data) as HumanReviewRequestView[]);
    } catch {
      // 解析不了就当这一帧没来过：SSE 会继续推下一帧，
      // 为一帧坏数据把整条流判死不值得。
    }
  });
  source.onerror = onError;
  return () => source.close();
}
