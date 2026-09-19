/** 身份客户端（Go：A 板块 GitHub OAuth + 会话）。
 *
 *  会话是 httpOnly cookie `__Host-repomesh-session`（internal/web/auth.go），
 *  同源请求自动携带，前端不持有也不存储任何 token。
 *
 *  **2026-09-16 裁定（api-design.md 附录 E 第 8 条结案）**：登录只走 GitHub
 *  OAuth——本地用户名密码体系（login/bootstrap/accounts/createAccount）在 Go
 *  后端**没有落点**，下列旧方法保留仅为让调用方编译通过，运行时会 404；
 *  相关页面（SetupWizard/LocalAccountsPanel）由页面改造批次替换为 GitHub 登录。
 *
 *  写操作（含 logout）要求 `X-CSRF-Token`，令牌来自 `GET /api/session` 的
 *  `csrfToken`，`session()` 会顺手注入 `api/http.ts` 的令牌槽。 */

import { getCsrfToken, setCsrfToken } from "./http";

export interface Account {
  id: string;
  username: string;
  display_name: string;
  is_admin: boolean;
  active: boolean;
}

export interface AccountCreateRequest {
  username: string;
  password: string;
  display_name: string;
  is_admin?: boolean;
}

export class AuthError extends Error {
  readonly status: number;
  constructor(status: number, message: string) {
    super(message);
    this.name = "AuthError";
    this.status = status;
  }
}

const BASE = import.meta.env.VITE_API_BASE ?? "";

/** 会话通道：同源 cookie，不带任何 Authorization 头。 */
async function goRequest<T>(path: string, init?: RequestInit): Promise<T> {
  const url = `${BASE}${path}`;
  let res: Response;
  try {
    res = await fetch(url, {
      credentials: "include",
      ...init,
      headers: { "Content-Type": "application/json", ...init?.headers },
    });
  } catch (cause) {
    throw new AuthError(0, `无法连接身份服务：${cause instanceof Error ? cause.message : String(cause)}`);
  }
  if (!res.ok) {
    const raw = await res.text().catch(() => "");
    let detail = raw;
    try {
      const parsed = JSON.parse(raw) as { detail?: unknown; error?: { message?: string } };
      if (parsed.error?.message !== undefined) {
        detail = parsed.error.message;
      } else if (parsed.detail !== undefined) {
        detail = typeof parsed.detail === "string" ? parsed.detail : JSON.stringify(parsed.detail);
      }
    } catch {
      /* 非 JSON 体，原样展示 */
    }
    throw new AuthError(res.status, detail || `请求失败（HTTP ${res.status}）`);
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

/** Go `GET /api/session` 的响应形状（internal/web 的 auth 会话出参）。 */
export interface GoSession {
  user: { id: string; displayName: string; githubId: string; isAdmin?: boolean };
  csrfToken: string;
}

/** 当前会话；未登录时后端 401，调用方据 AuthError.status 分流到登录页。
 *  顺手把 csrfToken 注入写通道（api/http.ts）。 */
export async function fetchSession(): Promise<{ account: Account; csrfToken: string }> {
  const session = await goRequest<GoSession>("/api/session");
  setCsrfToken(session.csrfToken);
  return {
    csrfToken: session.csrfToken,
    account: {
      id: session.user.id,
      username: session.user.githubId,
      display_name: session.user.displayName,
      // 账号自己的管理员事实：服务端仍是写操作的权威（见仓库团队管理）
      is_admin: session.user.isAdmin === true,
      active: true,
    },
  };
}

/** 跳转 GitHub OAuth 登录入口（整页跳转，回调由后端接住并种会话 cookie）。 */
/** B02 已采用协议：POST 拿授权 URL（幂等键 + destination home）→ 整页跳转。
 *  GitHub 授权后回调由后端换令牌、种会话 cookie，并重定向到 destination。 */
export async function startGithubLogin(): Promise<void> {
  const result = await goRequest<{ authorizationUrl: string }>("/api/auth/github/login", {
    method: "POST",
    headers: { "Idempotency-Key": crypto.randomUUID() },
    body: JSON.stringify({ destination: { kind: "home" } }),
  });
  window.location.href = result.authorizationUrl;
}

/** 切换账号：已登录状态下换成另一个 GitHub 账号（整页跳转，同登录）。
 *
 *  与 startGithubLogin 的唯一差别是打 `/switch` 端点：`login` 在已有活跃会话时
 *  返回 409 SESSION_ALREADY_ACTIVE，必须先登出；`switch` 允许带会话发起，后端回调
 *  成功后会 `identity_generation+1` 并 revoke 该浏览器绑定上的全部旧会话，再种一条
 *  新会话——所以旧账号的 cookie 随即失效，不需要前端做任何清理。
 *
 *  该端点要求 CSRF（与 reconnect 同级），而 goRequest 不注入 CSRF，故显式带头。 */
export async function switchGithubAccount(): Promise<void> {
  const result = await goRequest<{ authorizationUrl: string }>("/api/auth/github/switch", {
    method: "POST",
    headers: {
      "Idempotency-Key": crypto.randomUUID(),
      "X-CSRF-Token": getCsrfToken(),
    },
    body: JSON.stringify({ destination: { kind: "home" } }),
  });
  window.location.href = result.authorizationUrl;
}

/** Go `GET /api/auth/attempts/{id}` 出参（access.AttemptResult，as-built）：
 *  OAuth 回跳页轮询登录尝试状态的读面。 */
export interface AuthAttemptResult {
  attemptId: string;
  purpose: string;
  state: string;
  reasonCode: string | null;
  observedAt: string | null;
  connection: Record<string, unknown> | null;
  nextPage: string | null;
}

/** GET /api/auth/attempts/{id} — 登录尝试状态（保留不改，2026-09-16 裁定）。 */
export function getAuthAttempt(id: string): Promise<AuthAttemptResult> {
  return goRequest<AuthAttemptResult>(`/api/auth/attempts/${encodeURIComponent(id)}`);
}

export const authApi = {
  /** 已采用：当前会话（Go `GET /api/session`，映射为旧 Account 形状）。 */
  me: async (): Promise<Account> => (await fetchSession()).account,

  /** 已采用：注销（Go `POST /api/auth/logout`）。 */
  logout: async (): Promise<void> => {
    await goRequest<void>("/api/auth/logout", { method: "POST" });
  },

  /** 跳转 GitHub OAuth 登录。 */
  startGithubLogin,

  /** 已登录时切换到另一个 GitHub 账号。 */
  switchGithubAccount,

  /** @deprecated Go 后端无本地账号体系（2026-09-16 裁定），运行时 404。 */
  login: (username: string, password: string) =>
    goRequest<{ account: Account }>("/api/auth/login", {
      method: "POST",
      body: JSON.stringify({ username, password }),
    }),

  /** @deprecated 同上。 */
  bootstrap: (username: string, password: string, displayName: string) =>
    goRequest<Account>("/api/auth/bootstrap", {
      method: "POST",
      body: JSON.stringify({ username, password, display_name: displayName }),
    }),

  /** @deprecated 同上。 */
  accounts: () => goRequest<Account[]>("/api/auth/accounts"),

  /** @deprecated 同上。 */
  createAccount: (payload: AccountCreateRequest) =>
    goRequest<Account>("/api/auth/accounts", {
      method: "POST",
      body: JSON.stringify(payload),
    }),
};

/** human_control 面的旧通道（reviewDesk/humanControl 仍引用，保持编译）。
 *  这些端点在 Go 后端尚无落点；对应板块迁移时统一切到 apiRequest。 */
export async function sessionRequest<T>(path: string, init?: RequestInit): Promise<T> {
  const url = `${BASE}/api/v1${path}`;
  let res: Response;
  try {
    res = await fetch(url, { credentials: "include", ...init });
  } catch (cause) {
    throw new AuthError(0, `无法连接身份服务：${cause instanceof Error ? cause.message : String(cause)}`);
  }
  if (!res.ok) {
    const raw = await res.text().catch(() => "");
    throw new AuthError(res.status, raw.slice(0, 200) || `请求失败（HTTP ${res.status}）`);
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}
