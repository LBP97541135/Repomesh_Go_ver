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

/** 把任意错误响应体读成**一句话**。
 *
 *  2026-09-21 线上实测的 bug 就在这里：`dispatchGate` 此前不看状态码，直接把响应体
 *  `as HealthGateResult` 强转。而 401/503 时后端写的是**接入层错误信封**
 *  `{"error":{"code":"RESULT_UNCONFIRMED","message":"…"}}` —— 于是卡片上的
 *  `result.error` 是个**对象**，渲染出来就是「当前状态 未知 — [object Object]」：
 *  真正的原因（会话过期 / 服务端暂时不可用）被一个 toString 吃掉了。
 *
 *  认三种形状：信封（`error` 是对象或字符串）、`detail`（AgentTeams 未配置那条
 *  路由用的）、`message`。都不认就返回 null，由调用方回落到状态码 —— 不猜。 */
function readErrorText(body: unknown): string | null {
  if (typeof body === "string" && body.trim() !== "") return body.trim();
  if (body === null || typeof body !== "object") return null;
  const record = body as Record<string, unknown>;
  const inner = record.error;
  if (typeof inner === "string" && inner.trim() !== "") return inner.trim();
  if (inner !== null && typeof inner === "object") {
    const envelope = inner as Record<string, unknown>;
    const code = typeof envelope.code === "string" ? envelope.code : "";
    const message = typeof envelope.message === "string" ? envelope.message : "";
    const joined = [code, message].filter((part) => part !== "").join(" · ");
    if (joined !== "") return joined;
  }
  for (const key of ["detail", "message"] as const) {
    const value = record[key];
    if (typeof value === "string" && value.trim() !== "") return value.trim();
  }
  return null;
}

/** POST /api/agentteams/dispatch-gate — 派单前检查 Worker 是否可接。
 *  200 = 可派；409 = blocked（**带完整结果**，后端 writeJSON(409, result)）；503 = 未配置。
 *
 *  非 2xx 一律翻译成一条 `blocked` 结果，而不是把信封原样透给界面：这张卡是给
 *  人看的，人需要知道「为什么不可用」，不是需要看到一个对象的 toString。 */
export async function dispatchGate(workerName: string): Promise<HealthGateResult> {
  const res = await fetch("/api/agentteams/dispatch-gate", {
    method: "POST",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ worker: workerName }),
  });
  const body: unknown = await res.json().catch(() => null);
  // 409 是「可派发但需要先处理」，后端给的是**完整结果**，直接用它（含真实 phase）。
  if (res.status === 409 && body !== null && typeof body === "object" && "action" in (body as object)) {
    return body as HealthGateResult;
  }
  if (res.ok) {
    return body as HealthGateResult;
  }
  return {
    worker: workerName,
    phase: "",
    action: "blocked",
    recovered: false,
    durationMs: 0,
    error: readErrorText(body) ?? `${res.status} ${res.statusText || "请求失败"}`,
  };
}
