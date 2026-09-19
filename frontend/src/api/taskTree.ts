/** 任务树读面（Go as-built）：`GET /api/projects/{projectId}/plans/{planId}/tasks`。
 *
 *  下发任务树的行数据。planId 来自发现链物化收据（materialization.plan_id）；
 *  批次/协作会话/执行者显示名是 0029 的展示列——真实物化写入端未填时为空，
 *  消费方按缺省呈现（批次「—」、执行者「待指派」），不编造。 */
import { apiRequest } from "./http";

export interface PlanTaskItem {
  id: string;
  taskUid?: string;
  title: string;
  repositoryId?: string;
  status: string;
  batchNo?: number;
  conversationId?: string;
  leaderLabel?: string;
  workerLabel?: string;
  /** 真实执行者（最近一条开发 run 的 agent_kind），由后端任务树读面带出。
   *  workerLabel 是装配期的显示名快照、物化写入端不填；没有它时显示这个，
   *  两个都没有才写「待指派」。 */
  assignee?: string;
}

export interface PlanTasksPage {
  items: PlanTaskItem[];
}

export function listPlanTasks(projectId: string, planId: string): Promise<PlanTaskItem[]> {
  return apiRequest<PlanTasksPage>(
    "GET",
    `/projects/${encodeURIComponent(projectId)}/plans/${encodeURIComponent(planId)}/tasks`,
  ).then((page) => page.items);
}
