/** Go 后端适配层公共通道（唯一 fetch 出口，各域文件组合使用）。
 *  契约唯一来源：Go 仓库 `docs/current/api-design.md`（接口总册）。
 *
 *  与旧 Python 通道（client.ts/auth.ts 的 `/api/v1` + Bearer）的差别：
 *  - 前缀 `/api`，无版本段（2026-09-16 裁定）；
 *  - 认证只靠 httpOnly 会话 cookie `__Host-repomesh-session`（GitHub OAuth
 *    登录后种下，同源请求自动携带——不需要也不允许任何 Authorization 头）；
 *  - 写操作（POST/PUT/PATCH/DELETE）要求 `X-CSRF-Token` 头，令牌来自
 *    `GET /api/session` 的 `csrfToken` 字段，登录后 `setCsrfToken` 注入。
 *
 *  开发期经 vite 代理同源（vite.config.ts server.proxy → Go 后端 127.0.0.1:8080），
 *  生产由 Go 服务同源托管构建产物——两种模式都没有跨域，无需 credentials 特判。
 *
 *  能力边界（防漂移）：本文件不含任何业务端点；端点一律写在各域文件里，
 *  每域一个文件，禁止往 client.ts / 本文件加业务方法。 */

const BASE = "/api";

let csrfToken = "";

/** 登录后由 session 拉取方注入；未注入时写请求不带该头（后端 403 ORIGIN_REJECTED /
 *  CSRF 校验会拒绝——这是预期信号，说明还没走登录）。 */
export function setCsrfToken(token: string): void {
  csrfToken = token;
}

export function getCsrfToken(): string {
  return csrfToken;
}

export class ApiError extends Error {
  readonly status: number;
  readonly url: string;
  /** Go 后端错误体为 {"detail": "<明文>"}（api-design.md §2.4 as-built）。 */
  readonly detail: unknown;

  constructor(status: number, url: string, message: string, detail: unknown) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.url = url;
    this.detail = detail;
  }
}

async function errorFromResponse(
  res: Response,
  method: string,
  path: string,
): Promise<{ message: string; detail: unknown }> {
  const raw = await res.text().catch(() => "");
  let detail: unknown = raw;
  try {
    const parsed = JSON.parse(raw) as { detail?: unknown };
    if (parsed.detail !== undefined) detail = parsed.detail;
  } catch {
    /* 非 JSON 体，原样展示 */
  }
  const text = typeof detail === "string" ? detail : JSON.stringify(detail);
  return {
    message: `${method} ${path} → HTTP ${res.status}${text ? ` · ${text.slice(0, 200)}` : ""}`,
    detail,
  };
}

/** path 不含 `/api` 前缀（如 `/decision-chains/similar?...`）。
 *  extraHeaders：域级额外头（如创建接口的 `Idempotency-Key`），叠加在公共头之上。 */
export async function apiRequest<T>(
  method: "GET" | "POST" | "PUT" | "PATCH" | "DELETE",
  path: string,
  body?: unknown,
  extraHeaders?: Record<string, string>,
): Promise<T> {
  const url = `${BASE}${path}`;
  const headers: Record<string, string> = { Accept: "application/json", ...extraHeaders };
  if (body !== undefined) headers["Content-Type"] = "application/json";
  const isWrite = method !== "GET";
  if (isWrite && csrfToken) headers["X-CSRF-Token"] = csrfToken;

  let res: Response;
  try {
    res = await fetch(url, {
      method,
      headers,
      credentials: "same-origin",
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  } catch (cause) {
    throw new ApiError(0, url, `无法连接 ${url}：${cause instanceof Error ? cause.message : String(cause)}`, null);
  }
  if (!res.ok) {
    const err = await errorFromResponse(res, method, path);
    throw new ApiError(res.status, url, err.message, err.detail);
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}
