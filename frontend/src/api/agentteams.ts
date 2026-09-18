/** AgentTeams 执行进度数据源(graph-loop-design 既定路线):后端适配器只读透传
 *  上游 Controller 的 workflow 接口,浏览器不直连 Controller。
 *
 *  ⚠ 上游 workflow JSON 的字段名本会话无真实负载可核对(调研报告只锁了端点与
 *  状态值域),这里按 id/title/status/depends_on 做防御性归一:缺字段的任务如实
 *  丢弃,多个候选字段名取第一个存在的。首个真实负载到手后按实回填并删掉本注释。 */
import { apiRequest } from "./http";

export interface AtTask {
  id: string;
  title: string;
  status: string;
  deps: string[];
}

const pick = (o: Record<string, unknown>, keys: string[]): unknown => {
  for (const k of keys) if (o[k] !== undefined) return o[k];
  return undefined;
};

/** GET /api/agentteams/projects/{projectId}/workflow — 后端只读透传,
 *  这里归一成渲染所需的最小形状。多团队同名 project 时传 team 消歧(否则上游 409)。 */
export async function fetchAgentTeamsWorkflow(projectId: string, team?: string): Promise<AtTask[]> {
  const query = team ? `?team=${encodeURIComponent(team)}` : "";
  const raw = await apiRequest<unknown>(
    "GET",
    `/agentteams/projects/${encodeURIComponent(projectId)}/workflow${query}`,
  );
  const list = (Array.isArray(raw) ? raw : (raw as { tasks?: unknown; nodes?: unknown })?.tasks ??
    (raw as { nodes?: unknown })?.nodes) as unknown;
  if (!Array.isArray(list)) throw new Error("AgentTeams workflow 响应不含任务数组");
  const tasks: AtTask[] = [];
  for (const item of list as Array<Record<string, unknown>>) {
    const id = String(pick(item, ["id", "task_id", "name"]) ?? "");
    if (!id) continue;
    const rawDeps = pick(item, ["depends_on", "dependencies", "deps"]);
    tasks.push({
      id,
      title: String(pick(item, ["title", "name", "label"]) ?? id),
      status: String(pick(item, ["status", "state"]) ?? "planned").toLowerCase(),
      deps: Array.isArray(rawDeps) ? rawDeps.map(String) : [],
    });
  }
  return tasks;
}
