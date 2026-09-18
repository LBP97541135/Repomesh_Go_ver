import type { BusinessNextPage, Destination } from "./routes";
import { parseBusinessNextPage } from "./routes";
import { identifier, isUuid, object, text, timestamp } from "./values";

export type Purpose = "login" | "reconnect";

export type Session = {
  user: { id: string; displayName: string };
  githubConnection: {
    status: "connected" | "missing" | "unknown";
    observedAt: string | null;
  };
  csrfToken: string;
};

export type Attempt = {
  attemptId: string;
  purpose: Purpose;
  state: "pending" | "unknown" | "confirmed" | "cancelled" | "rejected" | "expired" | "superseded";
  reasonCode: "USER_CANCELLED" | "ACCOUNT_MISMATCH" | "BINDING_INVALID" | "EXCHANGE_UNCONFIRMED" | "SESSION_NOT_CONFIRMED" | "ATTEMPT_EXPIRED" | "NEWER_ATTEMPT" | null;
  observedAt: string | null;
  connection: { committedRevision: string; isCurrent: boolean } | null;
  nextPage: BusinessNextPage | null;
};

export type Capability = {
  status: "allowed" | "denied" | "unknown";
  reasonCodes: string[];
  observedAt: string | null;
};

export type RepositoryPage = {
  items: {
    id: string;
    displayName: string;
    userParticipation: Capability;
    appCapability: Capability;
  }[];
  nextCursor: string | null;
  coverage: {
    status: "complete" | "partial" | "unknown";
    reasonCodes: string[];
    observedAt: string | null;
  };
};

export type ApiError = {
  kind: "error";
  status: number;
  code: string;
  retryAt: number | null;
  fieldErrors?: readonly { field: string; code: string }[];
  requestId?: string;
};
export type Result<T> = { kind: "ok"; value: T } | ApiError;

export type RequestOptions<T> = {
  path: string;
  parse: (value: unknown) => T;
  signal?: AbortSignal;
  timeoutMs?: number;
  successStatuses?: readonly number[];
} & (
  | { method?: "GET"; body?: never; csrfToken?: never; key?: never }
  | { method?: "POST" | "PATCH"; body: object; csrfToken?: string; key?: string }
);

export type AuthorizationStartResult = {
  attemptId: string;
  authorizationUrl: string;
  expiresAt: string;
  resultPage: string;
};

function string(value: unknown, max = 1024): string {
  if (typeof value !== "string" || value.length === 0 || value.length > max) {
    throw new Error("Invalid string");
  }
  return value;
}

function member<const T extends string>(value: unknown, values: readonly T[]): T {
  for (const item of values) if (value === item) return item;
  throw new Error("Invalid choice");
}

function strings(value: unknown): string[] {
  if (!Array.isArray(value) || value.length > 100) throw new Error("Invalid list");
  return value.map((item: unknown) => text(item, 128));
}

export { isUuid as isAttemptId } from "./values";

function attemptId(value: unknown): string {
  if (!isUuid(value)) throw new Error("Invalid attempt ID");
  return value;
}

function parseSession(value: unknown): Session {
  const data = object(value);
  const user = object(data.user);
  const connection = object(data.githubConnection);
  return {
    user: { id: string(user.id, 128), displayName: string(user.displayName, 200) },
    githubConnection: {
      status: member(connection.status, ["connected", "missing", "unknown"]),
      observedAt: connection.observedAt === null ? null : timestamp(connection.observedAt),
    },
    csrfToken: string(data.csrfToken, 4096),
  };
}

function parseStart(value: unknown): AuthorizationStartResult {
  const data = object(value);
  const id = attemptId(data.attemptId);
  const authorizationUrl = string(data.authorizationUrl, 8192);
  const url = new URL(authorizationUrl);
  if (url.origin !== "https://github.com" || url.pathname !== "/login/oauth/authorize" || url.username || url.password || url.hash) {
    throw new Error("Invalid authorization URL");
  }
  if (data.resultPage !== `/auth/result/${id}`) throw new Error("Invalid result route");
  const expiresAt = timestamp(data.expiresAt);
  return { attemptId: id, authorizationUrl, expiresAt, resultPage: data.resultPage };
}

function parseAttempt(value: unknown): Attempt {
  const data = object(value);
  const purpose = member(data.purpose, ["login", "reconnect"]);
  const state = member(data.state, ["pending", "unknown", "confirmed", "cancelled", "rejected", "expired", "superseded"]);
  const reasonCode = data.reasonCode === null ? null : member(data.reasonCode, ["USER_CANCELLED", "ACCOUNT_MISMATCH", "BINDING_INVALID", "EXCHANGE_UNCONFIRMED", "SESSION_NOT_CONFIRMED", "ATTEMPT_EXPIRED", "NEWER_ATTEMPT"]);
  let connection: Attempt["connection"] = null;
  if (data.connection !== null) {
    const parsed = object(data.connection);
    if (purpose !== "reconnect" || state !== "confirmed" || typeof parsed.isCurrent !== "boolean") throw new Error("Invalid connection receipt");
    connection = { committedRevision: string(parsed.committedRevision, 128), isCurrent: parsed.isCurrent };
  }
  const nextPage = parseBusinessNextPage(data.nextPage);
  return {
    attemptId: attemptId(data.attemptId), purpose, state, reasonCode,
    observedAt: data.observedAt === null ? null : timestamp(data.observedAt), connection, nextPage,
  };
}

export function parseCapability(value: unknown): Capability {
  const data = object(value);
  return {
    status: member(data.status, ["allowed", "denied", "unknown"]),
    reasonCodes: strings(data.reasonCodes), observedAt: data.observedAt === null ? null : timestamp(data.observedAt),
  };
}

export function parseRepository(value: unknown): RepositoryPage["items"][number] {
  const repo = object(value);
  const userParticipation = parseCapability(repo.userParticipation);
  if (userParticipation.status !== "allowed" || userParticipation.observedAt === null) throw new Error("Unconfirmed repository identity");
  return {
    id: identifier(repo.id), displayName: text(repo.displayName, 512),
    userParticipation, appCapability: parseCapability(repo.appCapability),
  };
}

function parseRepositories(value: unknown): RepositoryPage {
  const data = object(value);
  if (!Array.isArray(data.items) || data.items.length > 100) throw new Error("Invalid repositories");
  const items = data.items.map(parseRepository);
  const coverage = object(data.coverage);
  return {
    items, nextCursor: data.nextCursor === null ? null : text(data.nextCursor, 8192),
    coverage: {
      status: member(coverage.status, ["complete", "partial", "unknown"]),
      reasonCodes: strings(coverage.reasonCodes), observedAt: coverage.observedAt === null ? null : timestamp(coverage.observedAt),
    },
  };
}

function retryAt(response: Response): number | null {
  const header = response.headers.get("Retry-After");
  const fallback = response.status === 429 ? Date.now() + 30000 : null;
  if (!header) return fallback;
  if (/^\d+$/.test(header)) {
    const deadline = Date.now() + Number(header) * 1000;
    return Number.isSafeInteger(deadline) ? deadline : fallback;
  }
  const date = Date.parse(header);
  return Number.isFinite(date) ? Math.max(Date.now(), date) : fallback;
}

export function parseErrorDiagnostics(value: unknown): {
  fieldErrors?: readonly { field: string; code: string }[];
  requestId?: string;
} {
  const error = object(value);
  const parsed: { fieldErrors?: readonly { field: string; code: string }[]; requestId?: string } = {};
  if (error.fieldErrors !== undefined) {
    if (!Array.isArray(error.fieldErrors) || error.fieldErrors.length > 100) throw new Error("Invalid field errors");
    parsed.fieldErrors = error.fieldErrors.map((value: unknown) => {
      const item = object(value);
      return { field: text(item.field, 128), code: text(item.code, 128) };
    });
  }
  if (error.requestId !== undefined) {
    const requestId = text(error.requestId, 128);
    if (/[\u0000-\u001f\u007f]/u.test(requestId)) throw new Error("Invalid request ID");
    parsed.requestId = requestId;
  }
  return parsed;
}

function parseErrorBody(value: unknown): Pick<ApiError, "code" | "fieldErrors" | "requestId"> {
  const envelope = object(value);
  const error = object(envelope.error);
  return { code: text(error.code, 128), ...parseErrorDiagnostics(error) };
}

export async function request<T>(options: RequestOptions<T>): Promise<Result<T>> {
  const { path, parse, signal, timeoutMs = 15000, successStatuses = [200] } = options;
  try {
    const headers = new Headers({ Accept: "application/json" });
    if (options.method === "POST" || options.method === "PATCH") {
      headers.set("Content-Type", "application/json");
      if (options.csrfToken !== undefined) headers.set("X-CSRF-Token", options.csrfToken);
      if (options.key !== undefined) headers.set("Idempotency-Key", options.key);
    }
    const timeout = AbortSignal.timeout(timeoutMs);
    const response = await fetch(path, {
      method: options.method ?? "GET", headers,
      body: options.method === "POST" || options.method === "PATCH" ? JSON.stringify(options.body) : undefined,
      credentials: "same-origin", cache: "no-store", redirect: "error",
      signal: signal === undefined ? timeout : AbortSignal.any([signal, timeout]),
    });
    if (!response.ok) {
      let diagnostics: Pick<ApiError, "code" | "fieldErrors" | "requestId"> = { code: "REQUEST_FAILED" };
      try {
        const raw: unknown = await response.json();
        diagnostics = parseErrorBody(raw);
      } catch { /* Untrusted error bodies never enter the page. */ }
      return { kind: "error", status: response.status, retryAt: retryAt(response), ...diagnostics };
    }
    if (!successStatuses.includes(response.status)) return { kind: "error", status: response.status, code: "INVALID_RESPONSE", retryAt: null };
    try {
      const raw: unknown = response.status === 204 ? null : await response.json();
      return { kind: "ok", value: parse(raw) };
    } catch (error: unknown) {
      if (error instanceof DOMException && error.name === "AbortError") {
        return { kind: "error", status: 0, code: "ABORTED", retryAt: null };
      }
      return { kind: "error", status: response.status, code: "INVALID_RESPONSE", retryAt: null };
    }
  } catch (error: unknown) {
    return { kind: "error", status: 0, code: error instanceof DOMException && error.name === "AbortError" ? "ABORTED" : "NETWORK_ERROR", retryAt: null };
  }
}

export const readSession = (signal?: AbortSignal) => request({ path: "/api/session", parse: parseSession, signal });
export const readAttempt = ({ id, signal }: { id: string; signal?: AbortSignal }) => request({ path: `/api/auth/attempts/${encodeURIComponent(id)}`, parse: parseAttempt, signal });
export const startAuthorization = ({ id, purpose, csrfToken, destination = { kind: "home" } }: { id: string; purpose: Purpose; csrfToken?: string; destination?: Destination }) => request({ path: `/api/auth/github/${purpose}`, method: "POST", body: { destination }, key: id, csrfToken, parse: parseStart, successStatuses: [200, 201] });
export const endSession = (csrfToken: string) => request({ path: "/api/auth/logout", method: "POST", body: {}, csrfToken, successStatuses: [204], parse: (value: unknown) => {
  if (value !== null) throw new Error("Invalid logout response");
  return null;
} });

const repositoryReads = new Map<string, Promise<Result<RepositoryPage>>>();

function settleWhenAborted<T>(flight: Promise<Result<T>>, signal: AbortSignal): Promise<Result<T>> {
  if (signal.aborted) return Promise.resolve({ kind: "error", status: 0, code: "ABORTED", retryAt: null });
  return new Promise((resolve) => {
    const onAbort = () => resolve({ kind: "error", status: 0, code: "ABORTED", retryAt: null });
    signal.addEventListener("abort", onAbort, { once: true });
    void flight.then((result) => {
      signal.removeEventListener("abort", onAbort);
      resolve(result);
    });
  });
}

export function readRepositories({ query, cursor, signal, refresh }: { query: string; cursor: string | null; signal?: AbortSignal; refresh?: boolean }): Promise<Result<RepositoryPage>> {
  const params = new URLSearchParams({ q: query, limit: "50" });
  if (cursor !== null) params.set("cursor", cursor);
  if (refresh) params.set("refresh", "1");
  const key = params.toString();
  let flight = repositoryReads.get(key);
  if (flight === undefined) {
    const tracked = request({ path: `/api/repositories?${key}`, parse: parseRepositories, timeoutMs: 60000 }).finally(() => {
      if (repositoryReads.get(key) === tracked) repositoryReads.delete(key);
    });
    repositoryReads.set(key, tracked);
    flight = tracked;
  }
  return signal === undefined ? flight : settleWhenAborted(flight, signal);
}

export function errorMessage(error: ApiError): string {
  switch (error.code) {
    case "AUTHENTICATION_REQUIRED": return "登录已失效，请重新登录后继续。";
    case "ORIGIN_REJECTED": return "当前页面来源无法通过验证，请从部署的 RepoMesh 地址重新打开。";
    case "CSRF_REJECTED": return "当前会话已变化，请检查当前登录状态后再操作。";
    case "RESOURCE_NOT_FOUND": return "无法读取这次结果。请回到发起授权的浏览器，或明确开始一次新登录。";
    case "ATTEMPT_IN_PROGRESS": return "这次授权已开始处理，请查询原尝试的结果。";
    case "SESSION_ALREADY_ACTIVE": return "当前已有登录。请检查当前账号，再选择重连或退出。";
    case "IDEMPOTENCY_CONFLICT": return "原尝试与这次请求不一致，已保留原尝试，请先查询结果。";
    case "AUTH_ATTEMPT_RESULT_REMOVED": return "这次授权结果已清理，无法取回。可以明确开始新的登录或重连。";
    case "VALIDATION_FAILED": return "登录请求未通过校验，请刷新页面后重试。";
    case "RATE_LIMITED": return "请求较频繁，请等待后再试。";
    case "RESULT_UNCONFIRMED": return "暂时无法确认这次结果，请继续查询原操作。";
    case "MODEL_SAVE_NOT_FOUND": return "还看不到这次保存结果。请继续查询原操作，或明确终结。";
    case "MODEL_SAVE_CLOSED": return "原保存已终结，未写入新配置。请用新的保存操作继续。";
    case "MODEL_SAVE_RESULT_REMOVED": return "这次保存结果已清理，不能复用原操作。";
    case "CURSOR_EXPIRED": return "发现批次或连接已更新，请从第一页重新读取。";
    case "INVALID_CURSOR": return "当前分页与搜索条件不一致，请从第一页重新读取。";
    case "INVALID_RESPONSE": return "服务器返回了无法验证的结果，暂时不能继续。";
    case "NETWORK_ERROR": return "暂时无法连接服务器。请检查网络后重试。";
    default:
      if (error.status === 401) return "登录已失效，请重新登录后继续。";
      if (error.status === 403) return "当前无法确认访问资格，请重新检查登录状态。";
      if (error.status === 429) return "请求较频繁，请等待后再试。";
      if (error.status === 503) return "认证或 GitHub 服务暂不可用，请稍后重试；如持续出现，请联系部署管理员检查配置。";
      return "暂时无法完成请求，请稍后重试。";
  }
}
