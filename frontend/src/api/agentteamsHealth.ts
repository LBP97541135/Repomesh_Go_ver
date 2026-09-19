/** AgentTeams 健康监控 API（Phase 1，2026-09-18）。
 *
 *  后端代理 AgentTeams Controller 的三个只读端点，浏览器不直连上游。
 *  - GET /api/agentteams/health → Controller 存活
 *  - GET /api/agentteams/status → 平台总览（Worker/Team/Human 计数）
 *  - GET /api/agentteams/workers/{name}/status → 单 Worker 运行时 phase */

export type WorkerPhase =
  | "Pending"
  | "Starting"
  | "Running"
  | "Stopping"
  | "Sleeping"
  | "Stopped"
  | "Failed";

export interface ControllerHealthView {
  status?: string;
  [key: string]: unknown;
}

export interface PlatformStatusView {
  kubeMode?: string;
  workers?: number;
  teams?: number;
  humans?: number;
  [key: string]: unknown;
}

export interface WorkerStatusView {
  phase?: WorkerPhase;
  name?: string;
  [key: string]: unknown;
}

/** Worker phase → 展示色（与左树任务行色点共用） */
export function phaseColor(phase: WorkerPhase | undefined): string {
  switch (phase) {
    case "Running":
      return "bg-[#16a34a]";
    case "Sleeping":
    case "Stopping":
    case "Stopped":
      return "bg-amber";
    case "Failed":
      return "bg-salmon";
    default:
      return "bg-tx3";
  }
}

/** Worker phase → 中文标签 */
export function phaseLabel(phase: WorkerPhase | undefined | ""): string {
  switch (phase) {
    case "Running": return "运行中";
    case "Starting": return "启动中";
    case "Sleeping": return "休眠";
    case "Stopping": return "停止中";
    case "Stopped": return "已停止";
    case "Failed": return "异常";
    default: return phase ?? "未知";
  }
}

async function fetchJSON<T>(url: string): Promise<T> {
  const res = await fetch(url, { credentials: "same-origin" });
  if (!res.ok) throw new Error(`${res.status} ${url}`);
  return res.json() as Promise<T>;
}

export function fetchControllerHealth(): Promise<ControllerHealthView> {
  return fetchJSON("/api/agentteams/health");
}

export function fetchPlatformStatus(): Promise<PlatformStatusView> {
  return fetchJSON("/api/agentteams/status");
}

export function fetchWorkerStatus(name: string): Promise<WorkerStatusView> {
  return fetchJSON(`/api/agentteams/workers/${encodeURIComponent(name)}/status`);
}

// ── Phase 2: 派单前健康门（dispatch gate）──

export interface HealthGateResult {
  worker: string;
  phase: WorkerPhase | "";
  action: "none" | "ensure-ready" | "blocked";
  recovered: boolean;
  durationMs: number;
  error?: string;
}

/** POST /api/agentteams/dispatch-gate — 派单前检查 Worker 是否可接。
 *  200 = 可派；409 = blocked（需展示恢复卡）；503 = AgentTeams 未配置。 */
export async function dispatchGate(workerName: string): Promise<HealthGateResult> {
  const res = await fetch("/api/agentteams/dispatch-gate", {
    method: "POST",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ worker: workerName }),
  });
  return res.json() as Promise<HealthGateResult>;
}
