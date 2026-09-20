/** E 跨仓职责 / 授权 / 冲突：`internal/responsibility` 的读面与动作。
 *
 *  2026-09-20：**后端早已完整**（service.go 的 RecordEvent/Timeline/ConfirmOwner/
 *  RequestAuth/GrantAuth/RevokeAuth/Transfer/ReportConflict/ResolveConflict/
 *  SyncPlatformStatus，路由挂在 `internal/web/responsibility_routes.go`），
 *  但前端**一个入口都没有** —— 这一整个能力在界面上等于不存在。
 *  这个文件与配套的 ResponsibilityCaseCard 就是补那一段。
 *
 *  字段名**逐字对齐后端 json tag**（snake_case）。本仓已经因为
 *  "Go 写 snake_case、TS 写 camelCase" 白屏过一次（`logsMissing` 那次），
 *  所以这里不按前端习惯改名 —— 接口形状就是接口形状。 */
import { apiRequest } from "./http";

/** 案例事件：谁（actor_role/actor_id）在什么时候做了什么（event_kind）。 */
export interface CaseEventView {
  id: string;
  plan_id: string;
  issue_id: string | null;
  project_id: string;
  /** manager / leader / executor / human —— 三者交接就靠它看 */
  actor_role: string;
  actor_id: string;
  /** owner_confirmed / auth_requested / auth_granted / auth_revoked /
   *  responsibility_transferred / status_synced / opinion_conflict /
   *  conflict_resolved / escalation …（迁移 0052 的枚举） */
  event_kind: string;
  detail: unknown;
  created_at: string;
}

export interface OwnerConfirmationView {
  id: string;
  plan_id: string;
  repository_id: string;
  owner_github_id: string;
  confirmed_by: string;
  status: string;
  confirmed_at: string | null;
  created_at: string;
}

export interface AuthRequestView {
  id: string;
  plan_id: string;
  repository_id: string;
  requester_id: string;
  requester_role: string;
  scope: string;
  status: string;
  granted_by: string | null;
  granted_at: string | null;
  revoked_by: string | null;
  revoked_at: string | null;
  reason: string;
  created_at: string;
}

export interface TransferView {
  id: string;
  plan_id: string;
  from_role: string;
  from_id: string;
  to_role: string;
  to_id: string;
  reason: string;
  transferred_at: string;
}

export interface ConflictView {
  id: string;
  plan_id: string;
  conflict_type: string;
  discovered_by: string;
  discovered_by_role: string;
  resolved_by: string | null;
  resolved_by_role: string | null;
  resolution: string;
  status: string;
  resolved_at: string | null;
  created_at: string;
}

/** GET /api/projects/{pid}/plans/{planId}/case-timeline */
export function fetchCaseTimeline(projectId: string, planId: string): Promise<{ events: CaseEventView[] }> {
  return apiRequest<{ events: CaseEventView[] }>(
    "GET",
    `/projects/${encodeURIComponent(projectId)}/plans/${encodeURIComponent(planId)}/case-timeline`,
  );
}

/** POST …/owner-confirm —— 仓库 Owner 确认（人做）。 */
export function confirmRepositoryOwner(
  projectId: string,
  planId: string,
  input: { repository_id: string; owner_github_id: string; confirmed_by: string },
): Promise<OwnerConfirmationView> {
  return apiRequest<OwnerConfirmationView>(
    "POST",
    `/projects/${encodeURIComponent(projectId)}/plans/${encodeURIComponent(planId)}/owner-confirm`,
    input,
  );
}

/** POST …/auth-request —— 授权申请（Leader 提，等有权限的人批）。 */
export function requestAuthorization(
  projectId: string,
  planId: string,
  input: { repository_id: string; requester_id: string; requester_role: string; scope: string; reason: string },
): Promise<AuthRequestView> {
  return apiRequest<AuthRequestView>(
    "POST",
    `/projects/${encodeURIComponent(projectId)}/plans/${encodeURIComponent(planId)}/auth-request`,
    input,
  );
}

/** POST /api/authorization/{id}/grant */
export function grantAuthorization(id: string, grantedBy: string): Promise<AuthRequestView> {
  return apiRequest<AuthRequestView>("POST", `/authorization/${encodeURIComponent(id)}/grant`, { granted_by: grantedBy });
}

/** POST /api/authorization/{id}/revoke */
export function revokeAuthorization(id: string, revokedBy: string): Promise<AuthRequestView> {
  return apiRequest<AuthRequestView>("POST", `/authorization/${encodeURIComponent(id)}/revoke`, { revoked_by: revokedBy });
}

/** POST …/transfer —— 负责人不可用时的责任转移。 */
export function transferResponsibility(
  projectId: string,
  planId: string,
  input: { from_role: string; from_id: string; to_role: string; to_id: string; reason: string },
): Promise<TransferView> {
  return apiRequest<TransferView>(
    "POST",
    `/projects/${encodeURIComponent(projectId)}/plans/${encodeURIComponent(planId)}/transfer`,
    input,
  );
}

/** POST …/conflicts —— 上报一个跨仓冲突（等人裁决）。 */
export function reportConflict(
  projectId: string,
  planId: string,
  input: { conflict_type: string; discovered_by: string; discovered_by_role: string },
): Promise<ConflictView> {
  return apiRequest<ConflictView>(
    "POST",
    `/projects/${encodeURIComponent(projectId)}/plans/${encodeURIComponent(planId)}/conflicts`,
    input,
  );
}

/** POST /api/conflicts/{id}/resolve —— 冲突裁决（裁决人 + 结论）。 */
export function resolveConflict(
  id: string,
  input: { resolved_by: string; resolved_by_role: string; resolution: string },
): Promise<ConflictView> {
  return apiRequest<ConflictView>("POST", `/conflicts/${encodeURIComponent(id)}/resolve`, input);
}

/** 事件类型 → 中文标签。未登记的原样显示（不编一个更好听的名字）。 */
export const CASE_EVENT_LABEL: Record<string, string> = {
  owner_confirmed: "仓库 Owner 已确认",
  auth_requested: "授权申请",
  auth_granted: "授权已批准",
  auth_revoked: "授权已撤销",
  responsibility_transferred: "责任转移",
  status_synced: "平台状态同步",
  opinion_conflict: "意见冲突",
  conflict_resolved: "冲突已裁决",
  escalation: "升级",
};

/** 角色 → 中文（Manager 总领导 / Leader 仓库领导 / 执行）。 */
export const CASE_ROLE_LABEL: Record<string, string> = {
  manager: "Manager · 总领导",
  leader: "Leader · 仓库领导",
  executor: "执行角色",
  human: "人",
};
