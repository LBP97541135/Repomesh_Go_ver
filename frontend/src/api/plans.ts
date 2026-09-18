/** 计划与执行管线域(Go:§4.3 规划 + M1-M9 pipeline 接入,2026-09-17 落地)。
 *
 *  端点与形状唯一来源:`internal/web/pipeline_routes.go`、`handoff_routes.go`
 *  与 api-design.md §4.3。写操作走会话 cookie + CSRF + Origin 校验
 *  (registerProjectRoute 惯例),**不需要**幂等键(后端此处未要求)。
 *  Plan 出参形状待页面接入时按 handler 实测回填(ponytail: 先窄类型不臆测)。 */
import { apiRequest } from "./http";

/** POST /api/projects/{projectId}/plans 请求体(pipeline_routes.go as-built):
 *  requirementKey 为链聚合键;批次为任务 id 分组的有序数组;dag 为任务依赖。 */
export interface PlanCreateInput {
  requirementText: string;
  requirementKey: string;
  batches: string[][];
  dag: Record<string, string[]>;
}

/** 计划出参(粗类型:tasks.CreatePlan 的 Plan,字段待页面接入时回填)。 */
export type PlanView = Record<string, unknown>;

/** POST /api/projects/{projectId}/plans — 生成计划(201,返回计划出参)。 */
export function createPlan(
  projectId: string,
  input: PlanCreateInput,
): Promise<PlanView> {
  return apiRequest<PlanView>("POST", `/projects/${encodeURIComponent(projectId)}/plans`, input);
}

/** GET /api/projects/{projectId}/plans/{planId} — 计划详情。 */
export function getPlan(projectId: string, planId: string): Promise<PlanView> {
  return apiRequest<PlanView>(
    "GET",
    `/projects/${encodeURIComponent(projectId)}/plans/${encodeURIComponent(planId)}`,
  );
}

/** POST /api/projects/{projectId}/plans/{planId}/interrupt — 中断执行。 */
export function interruptPlan(
  projectId: string,
  planId: string,
  reason: string,
): Promise<{ status: string }> {
  return apiRequest<{ status: string }>(
    "POST",
    `/projects/${encodeURIComponent(projectId)}/plans/${encodeURIComponent(planId)}/interrupt`,
    { reason },
  );
}

/** 交接单(§5.4 数据库测试交接):创建入参(handoff.CreateCommand)。 */
export interface HandoffCreateInput {
  repositoryId: string;
  summary: string;
  deliverables: string[];
}

/** 交接单出参(handoff.HandoffView,字段待页面接入时回填)。 */
export interface HandoffView {
  id: string;
  planId: string;
  repositoryId: string;
  authorId: string;
  summary: string;
  deliverables: string[];
  createdAt: string;
  [key: string]: unknown;
}

/** POST /api/projects/{projectId}/plans/{planId}/handoffs — 创建交接单(201)。 */
export function createHandoff(
  projectId: string,
  planId: string,
  input: HandoffCreateInput,
): Promise<HandoffView> {
  return apiRequest<HandoffView>(
    "POST",
    `/projects/${encodeURIComponent(projectId)}/plans/${encodeURIComponent(planId)}/handoffs`,
    input,
  );
}

/** GET /api/projects/{projectId}/plans/{planId}/handoffs — 交接单列表。 */
export function listHandoffs(
  projectId: string,
  planId: string,
): Promise<{ items: HandoffView[] }> {
  return apiRequest(
    "GET",
    `/projects/${encodeURIComponent(projectId)}/plans/${encodeURIComponent(planId)}/handoffs`,
  );
}

/** 编制组装(POST /api/organizations/{orgId}/assembly):按仓库列表生成
 *  Manager→Leader→Workers 的编制,下发链的组织来源。 */
export function assembleOrganization(
  orgId: string,
  input: { repositories: string[]; workersPerRepo: number; leaderName: string },
): Promise<Record<string, unknown>> {
  return apiRequest<Record<string, unknown>>(
    "POST",
    `/organizations/${encodeURIComponent(orgId)}/assembly`,
    input,
  );
}
