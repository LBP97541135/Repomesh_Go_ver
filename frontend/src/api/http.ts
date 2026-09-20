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

/** 等令牌的写请求（见下面 `waitForCsrfToken` 的注释）。 */
let csrfWaiters: Array<(token: string) => void> = [];

/** 登录后由 session 拉取方注入；未注入时写请求不带该头（后端 403 ORIGIN_REJECTED /
 *  CSRF 校验会拒绝——这是预期信号，说明还没走登录）。 */
export function setCsrfToken(token: string): void {
  csrfToken = token;
  const waiters = csrfWaiters;
  csrfWaiters = [];
  for (const resolve of waiters) resolve(token);
}

export function getCsrfToken(): string {
  return csrfToken;
}

/** 等 CSRF 令牌就绪（**只给写请求用**）。
 *
 *  2026-09-21 线上实测：页面加载瞬间会有若干并发 POST（工作台引导、健康探测、
 *  物化状态刷新）抢在 `GET /api/session` 返回之前发出去，于是全被后端
 *  403 CSRF_REJECTED —— 15 分钟内成簇 20+ 条。这些请求**不是用户操作失败**，
 *  是时序问题：令牌晚到几百毫秒而已。
 *
 *  所以写请求不再"没令牌就裸发"，而是**等一小会儿**：令牌一到立刻发；超时
 *  （默认 3s）仍没有就照旧裸发 —— 没登录时本来也该 403，这条不改变那种情形的结论，
 *  只是不再把"登录引导竞态"混进同一堆 403 里。
 */
export function waitForCsrfToken(timeoutMs = 3000): Promise<string> {
  if (csrfToken) return Promise.resolve(csrfToken);
  return new Promise((resolve) => {
    const timer = setTimeout(() => {
      csrfWaiters = csrfWaiters.filter((waiter) => waiter !== onToken);
      resolve("");
    }, timeoutMs);
    const onToken = (token: string) => {
      clearTimeout(timer);
      resolve(token);
    };
    csrfWaiters.push(onToken);
  });
}

/** 读后端自报版本：`GET /healthz`（**不在 /api 下**，无鉴权，返回
 *  `{"status":…,"version":"<commit sha>"}`）。
 *
 *  为什么要它：部署页要能回答"线上跑的是哪个 commit"。前端自己那份
 *  `__APP_VERSION__` 是**构建期**注入的，两者可能不一致（前端产物没换、后端换了，
 *  或反过来）—— 分开显示才看得出这种不一致。 */
export async function fetchBackendVersion(): Promise<string | null> {
  try {
    const res = await fetch("/healthz", { credentials: "same-origin" });
    if (!res.ok) return null;
    const body = (await res.json()) as { version?: unknown };
    return typeof body.version === "string" && body.version !== "" ? body.version : null;
  } catch {
    return null;
  }
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
  // 网关类状态（502/503/504）几乎总是**代理层**回的，不是应用回的：
  // 线上实测 3220 条 502 全部落在部署窗口内（nginx 在 `systemctl stop` 到
  // `start` 之间没有上游）。此时 body 是 nginx 的 HTML 错误页，把它截 200 字
  // 拼进提示，人看到的就是一坨 `<html><head><title>502 Bad Gateway…`。
  // 那既不好读、也没告诉人该做什么。这里换成人话 + 下一步动作。
  if (res.status === 502 || res.status === 503 || res.status === 504) {
    const hint =
      res.status === 503
        ? "服务端暂时不可用"
        : "服务暂时不可达（网关没拿到上游应答）";
    return {
      message:
        `${method} ${path} → HTTP ${res.status} · ${hint}。` +
        "通常是正在部署/重启（窗口约 5 秒），稍后重试即可；" +
        "若持续超过 1 分钟仍不通，那不是部署窗口，请看服务器上的 repomesh-web 服务状态。",
      detail: { gateway: res.status },
    };
  }
  let detail: unknown = raw;
  try {
    const parsed = JSON.parse(raw) as { detail?: unknown; error?: unknown };
    if (parsed.detail !== undefined) detail = parsed.detail;
    else if (parsed.error !== undefined) {
      // 接入层错误信封 `{"error":{"code","message"}}` —— **message 才是人能看懂的那句话**。
      //
      // 2026-09-21 用户实测：合并失败时 toast 显示
      //   `POST …/merge → HTTP 503 · {"error":{"code":"RESULT_UNCONFIRMED",…}}`
      // 而服务端明明写好了「合并闸门未开（push=… PR=… CI=… 评审=…）」——
      // 那句话在信封的 message 里，被 JSON.stringify 埋了。用户看到的是一坨机器字段，
      // 看不到"缺哪一门"。这里把 message 提出来（附上 code 便于对日志），
      // 认不出形状才回落成原来的整段 JSON。
      const envelope = parsed.error;
      if (typeof envelope === "string") {
        detail = envelope;
      } else if (envelope !== null && typeof envelope === "object") {
        const record = envelope as { code?: unknown; message?: unknown };
        const code = typeof record.code === "string" ? record.code : "";
        const message = typeof record.message === "string" ? record.message : "";
        if (message !== "") detail = code !== "" ? `${message}（${code}）` : message;
        else if (code !== "") detail = code;
      }
    }
  } catch {
    // 非 JSON 体：**HTML 不原样展示**（多半是代理错误页，见上）。
    // 其它文本照旧 —— 有些端点的错误就是纯文本。
    if (/^\s*<(!doctype|html)/i.test(raw)) {
      detail = "（服务端返回了 HTML 错误页，已省略原文）";
    }
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
  // 写请求**等令牌**再发（见 waitForCsrfToken 的注释）：此前是"有就带、没有就裸发"，
  // 页面加载瞬间的并发 POST 会集体撞 403 CSRF_REJECTED。
  if (isWrite) {
    const token = await waitForCsrfToken();
    if (token) headers["X-CSRF-Token"] = token;
  }

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
