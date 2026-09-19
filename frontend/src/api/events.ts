/** SSE 事件流(Phase 4):`GET /api/events/stream?issueId=…`。
 *
 *  服务端每 2s 快拍 `repomesh_issues` 读模型、只推差量(无 WAL/LISTEN-NOTIFY),
 *  事件三型:`discovery_step`(携带与 GET /issues/{id}/discovery 同形的最新读
 *  投影)、`task_status`(任务/回执状态变化)、`worker_health`(失败步与疑似
 *  中断,语义同观测告警面)。连接即 `hello`;之后每 30s 一条 `:keepalive`。
 *
 *  重连策略:原生 EventSource 自带重连但节奏不受控,这里 onerror 一律关掉,
 *  换自己的指数退避(1s → 2s → 4s → … 封顶 30s),`hello` 握手成功即归零。
 *  **每次成功连上都会收到 `hello`**——重连后的 hello 就是「断线间隙可能错过
 *  事件,请整页重取」的信号,调用方在 onHello 里做全量刷新即可。
 *
 *  认证与 http.ts 同源同 cookie(GET 无需 CSRF)。回放模式没有后端可订:
 *  返回空退订,页面数据源仍是本地夹具。 */
import type { DiscoveryView } from "./contract";
import { resolveDataSourceMode } from "./source";

export interface HelloEvent {
  issue_id: string;
  task_id?: string;
  poll_ms?: number;
}

export interface TaskStatusEvent {
  issue_id: string;
  task_id: string;
  status: string;
  step: number;
}

/** 与观测告警面 stalls 同语义:failed = 步块带错;stalled = 分档已过而计划迟未生成。 */
export interface WorkerHealthSignal {
  step: number;
  kind: "failed" | "stalled";
  message: string;
}

export interface WorkerHealthEvent {
  issue_id: string;
  signals: WorkerHealthSignal[];
}

export interface EventStreamHandlers {
  onHello?: (event: HelloEvent) => void;
  onDiscoveryStep?: (view: DiscoveryView) => void;
  onTaskStatus?: (event: TaskStatusEvent) => void;
  onWorkerHealth?: (event: WorkerHealthEvent) => void;
}

const RECONNECT_BASE_MS = 1000;
const RECONNECT_MAX_MS = 30000;

/** 帧坏了就整帧丢弃:半截 JSON 冒充读投影比缺一拍更糟(树会指错步)。 */
function parseFrame<T>(raw: string | null): T | null {
  if (raw === null || raw === "") return null;
  try {
    return JSON.parse(raw) as T;
  } catch {
    return null;
  }
}

/** 订阅一个 issue 的事件流;返回退订函数(组件卸载/换 issue 时必须调用)。 */
export function subscribeEvents(issueId: string, handlers: EventStreamHandlers): () => void {
  if (resolveDataSourceMode() === "replay") {
    return () => undefined;
  }
  let closed = false;
  let source: EventSource | null = null;
  let timer: number | null = null;
  let attempts = 0;

  const connect = (): void => {
    if (closed) return;
    const es = new EventSource(`/api/events/stream?issueId=${encodeURIComponent(issueId)}`);
    source = es;
    const on = (type: string, deliver: (data: string | null) => void): void => {
      es.addEventListener(type, (ev) => deliver((ev as MessageEvent).data));
    };
    on("hello", (data) => {
      attempts = 0; // 握手成功:退避归零
      const hello = parseFrame<HelloEvent>(data);
      if (hello) handlers.onHello?.(hello);
    });
    on("discovery_step", (data) => {
      const view = parseFrame<DiscoveryView>(data);
      if (view) handlers.onDiscoveryStep?.(view);
    });
    on("task_status", (data) => {
      const event = parseFrame<TaskStatusEvent>(data);
      if (event) handlers.onTaskStatus?.(event);
    });
    on("worker_health", (data) => {
      const event = parseFrame<WorkerHealthEvent>(data);
      if (event) handlers.onWorkerHealth?.(event);
    });
    es.onerror = () => {
      es.close(); // 自管重连:不用 EventSource 内建节奏
      if (source === es) source = null;
      if (closed) return;
      const delay = Math.min(RECONNECT_BASE_MS * 2 ** attempts, RECONNECT_MAX_MS);
      attempts += 1;
      timer = window.setTimeout(connect, delay);
    };
  };

  connect();
  return () => {
    closed = true;
    if (timer !== null) window.clearTimeout(timer);
    source?.close();
    source = null;
  };
}
