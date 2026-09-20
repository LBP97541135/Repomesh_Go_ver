/** 一条任务名下**agent 的真实工作内容**：`internal/web/task_output_routes.go`。
 *
 *  为什么要有它：用户反复提过"右边的流式输出里，看不到 worker 具体的工作内容"、
 *  "我也看不到 worker 的内部工作记录"。根因是**没有数据源** ——
 *  `public.log_entries` 全仓没有生产者（线上实测 0 行），而真正的产出在磁盘上：
 *  `/opt/repomesh/workspaces/<attempt>/agent-stdout.log`（agent 自己写的
 *  变更说明 / 总结 / exit code 就在里面）。
 *
 *  连接点：`repomesh_execution.agent_runs.task_package_ref` 对 dev run 与 test run
 *  都**直接用 task id**，所以一个任务能同时拿到"开发跑的那次"和"测试跑的那次"。
 *
 *  诚实条款：`logsMissing` 列出**读不到**的那几份日志（工作区被清掉 / 还没落盘）。
 *  界面必须据此说"没有记录"，**不能**把空日志显示成"它什么都没干"。 */
import { apiRequest } from "./http";

export interface TaskRunOutputView {
  runId: string;
  agentKind: string;
  state: string;
  exitCode: number | null;
  workspace: string;
  repoFullName: string;
  startedAt: string;
  exitedAt: string;
  stdoutTail: string;
  stderrTail: string;
  /** 看到的是尾部（文件比请求的 tail 大）—— 界面要如实标注。 */
  stdoutTruncated: boolean;
  stderrTruncated: boolean;
  /** 读不到的日志文件名（不是"空内容"，是"没有这份记录"）。 */
  logsMissing: string[];
}

export interface TaskAgentOutputView {
  taskId: string;
  runs: TaskRunOutputView[];
  workspaceRoot: string;
}

/** GET /api/projects/{pid}/tasks/{tid}/agent-output — 这条任务的 agent 输出。
 *
 *  `tail` 是每份日志回多少字节的**尾部**（默认 64KB，服务端上限 256KB）。 */
export function fetchTaskAgentOutput(
  projectId: string,
  taskId: string,
  tail?: number,
): Promise<TaskAgentOutputView> {
  const query = tail ? `?tail=${encodeURIComponent(String(tail))}` : "";
  return apiRequest<TaskAgentOutputView>(
    "GET",
    `/projects/${encodeURIComponent(projectId)}/tasks/${encodeURIComponent(taskId)}/agent-output${query}`,
  );
}
