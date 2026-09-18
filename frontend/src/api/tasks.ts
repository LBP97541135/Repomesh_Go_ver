/** 任务审批域(Go:pipeline_routes.go 的 approve/reject,as-built)。
 *
 *  审批人不由前端指定:后端取会话主体写入。approve 摘要可选;reject 原因必填
 *  (不做前端校验,后端 422 兜底——错误文案原样上抛)。 */
import { apiRequest } from "./http";

/** POST /api/projects/{projectId}/tasks/{taskId}/approve — 审批通过任务步。 */
export function approveTask(
  projectId: string,
  taskId: string,
  summary?: string,
): Promise<{ status: string }> {
  return apiRequest<{ status: string }>(
    "POST",
    `/projects/${encodeURIComponent(projectId)}/tasks/${encodeURIComponent(taskId)}/approve`,
    { summary: summary ?? "" },
  );
}

/** POST /api/projects/{projectId}/tasks/{taskId}/reject — 驳回任务步(需原因)。 */
export function rejectTask(
  projectId: string,
  taskId: string,
  reason: string,
): Promise<{ status: string }> {
  return apiRequest<{ status: string }>(
    "POST",
    `/projects/${encodeURIComponent(projectId)}/tasks/${encodeURIComponent(taskId)}/reject`,
    { reason },
  );
}
