/** SCM 交付域(Go:§7.1 change_sets,scm_routes.go as-built)。
 *
 *  端点:冻结变更集 / 记录变更事件 / merge-gate 状态。**没有变更集列表端点**——
 *  调用方需持有 changeSetId(冻结返回值)才能查询门禁与追加事件。 */
import { apiRequest } from "./http";

export interface MergeGate {
  changeSetId: string;
  /** 已推送 / 已开 PR / CI 通过 / 已评审 —— 四道门;open = 门禁未关闭(未合并) */
  pushed: boolean;
  pr: boolean;
  ciPassed: boolean;
  reviewed: boolean;
  open: boolean;
}

/** POST /api/projects/{projectId}/change-sets — 冻结变更集(201,返回 changeSetId)。 */
export async function freezeChangeSet(
  projectId: string,
  input: { organizationId?: string; repositories: string[]; title?: string },
): Promise<{ changeSetId: string }> {
  return apiRequest<{ changeSetId: string }>(
    "POST",
    `/projects/${encodeURIComponent(projectId)}/change-sets`,
    input,
  );
}

/** POST /api/projects/{projectId}/change-sets/{changeSetId}/events — 记录变更事件。 */
export function recordChangeSetEvent(
  projectId: string,
  changeSetId: string,
  body: { kind: string; payload: string },
): Promise<{ status: string }> {
  return apiRequest<{ status: string }>(
    "POST",
    `/projects/${encodeURIComponent(projectId)}/change-sets/${encodeURIComponent(changeSetId)}/events`,
    body,
  );
}

/** GET /api/projects/{projectId}/change-sets/{changeSetId}/merge-gate — 门禁四门状态。 */
export function getMergeGate(
  projectId: string,
  changeSetId: string,
): Promise<MergeGate> {
  return apiRequest<MergeGate>(
    "GET",
    `/projects/${encodeURIComponent(projectId)}/change-sets/${encodeURIComponent(changeSetId)}/merge-gate`,
  );
}


export interface ChangeSetSummary {
  id: string;
  status: string;
  prUrl?: string;
  branch?: string;
  taskId?: string;
}

/** GET /api/projects/{projectId}/change-sets?taskIds=a,b — 交付列车车厢读面:
 *  按任务 id 列 change-sets(经 tasks.project_id 收敛项目域)。 */
export function listChangeSets(
  projectId: string,
  taskIds: string[],
): Promise<ChangeSetSummary[]> {
  return apiRequest<{ items: ChangeSetSummary[] }>(
    "GET",
    `/projects/${encodeURIComponent(projectId)}/change-sets?taskIds=${encodeURIComponent(taskIds.join(","))}`,
  ).then((page) => page.items);
}

/** POST /projects/{projectId}/change-sets/{changeSetId}/merge —— **真合并**。
 *
 *  这是整条链上唯一会真动用户仓库的动作。后端三道门（属于本项目 / 有 PR /
 *  合并闸门开着）一道都不省，闸门未开时**如实回缺哪几项**，不假装成功。
 *  幂等：已合并的直接返回成功，不重复调 GitHub。 */
export function mergeChangeSet(
  projectId: string,
  changeSetId: string,
): Promise<{ merged: boolean; pr: string; message: string }> {
  return apiRequest<{ merged: boolean; pr: string; message: string }>(
    "POST",
    `/projects/${encodeURIComponent(projectId)}/change-sets/${encodeURIComponent(changeSetId)}/merge`,
  );
}
