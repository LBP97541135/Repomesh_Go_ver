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

/** 执行中人工打断的判定结果(tasks.InterruptOutcome,pipeline_routes.go as-built)。
 *  ready=false:新仓库的扫描还没就绪,判定未做(可稍后再来);affectsPlan=true:
 *  该仓库与当前计划有耦合 —— 收集窗已开,重排 v2 的派发意图已登记(replanQueued)。 */
export interface InterruptOutcomeView {
  nodeId: string;
  onboarded: boolean;
  ready: boolean;
  affectsPlan: boolean;
  affectedSet?: string[];
  replanQueued: boolean;
}

/** POST /api/projects/{projectId}/plans/{planId}/interrupt — 执行中人工打断:
 *  提交一个**人点名**的仓库 X,后端据此落打断决策单、触发 onboarding、判定它是否
 *  影响当前计划。入参必须是 { repository, note } —— 此前这里是 { reason },与后端
 *  对不上(会以 422 REPOSITORY_REQUIRED 拒绝),而且全仓没有调用方,是死代码。 */
export function interruptPlan(
  projectId: string,
  planId: string,
  input: { repository: string; note: string },
): Promise<InterruptOutcomeView> {
  return apiRequest<InterruptOutcomeView>(
    "POST",
    `/projects/${encodeURIComponent(projectId)}/plans/${encodeURIComponent(planId)}/interrupt`,
    input,
  );
}

/** 计划换代历史的一条(tasks.PlanRevision,pipeline_routes.go as-built)。
 *  每一次全量快照替换都在 public.plans.revisions 里:换了哪一版、谁触发的、
 *  增删了哪些仓库、创建/取代了多少任务。 */
export interface PlanRevisionView {
  revision: number;
  baseVersion: string;
  resultVersion: string;
  actor: string;
  reason: string;
  addedRepositories: string[];
  removedRepositories: string[];
  createdTasks: number;
  supersededTasks: number;
  /** 触发本轮重排的那一跳(人工打断/升级梯的决策单 id)。 */
  upstreamRef?: string;
  idempotencyKey: string;
}

/** GET /api/projects/{projectId}/plans/{planId}/revisions — 计划换代历史。
 *  这条历史此前只有落库没有读面:计划换过几版、每版为什么换,界面上看不到。 */
export function listPlanRevisions(
  projectId: string,
  planId: string,
): Promise<{ items: PlanRevisionView[] }> {
  return apiRequest<{ items: PlanRevisionView[] }>(
    "GET",
    `/projects/${encodeURIComponent(projectId)}/plans/${encodeURIComponent(planId)}/revisions`,
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

// 2026-09-20（迁移 0053）删掉了这里的 `assembleOrganization(orgId, …)`：它打的是
// `POST /api/organizations/{orgId}/assembly`，而**那条后端路由已经不存在**
// ——组织退出主业务，编制的作用域是项目（`internal/web/pipeline_routes.go` 里那条
// 注册被整条移除，`internal/web/topology_routes.go` 的
// `POST /api/projects/{projectId}/topologies` 是唯一入口）。
// 保留一个指向已删路由的客户端函数，正是当年「仓库页建团」静默打不通的那种漂移；
// 项目级客户端留在 `api/humanControl.ts` 的 `createTopology`，此处不再另立一份。
