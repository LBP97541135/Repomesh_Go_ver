interface RepoMeshRuntimeConfig {
  apiToken?: string;
  observeWorkbenchUrl?: string;
}

function runtimeConfig(): RepoMeshRuntimeConfig {
  return (
    window as typeof window & { __REPOMESH_CONFIG__?: RepoMeshRuntimeConfig }
  ).__REPOMESH_CONFIG__ ?? {};
}

export function browserApiToken(): string {
  return runtimeConfig().apiToken ?? import.meta.env.VITE_API_TOKEN ?? "";
}

/** 观测工作台地址。两种形态：**同源路径**（部署形态，默认 `/observe/`）或
 *  **回环绝对地址**（本地开发直接开工作台时用）。
 *
 *  2026-09-21：工作台从「操作者自己机器上的进程」改成「服务器回环上的服务 + 控制台
 *  同源反代」。默认值因此从 `http://127.0.0.1:18090/` 换成同源 `/observe/` —— 旧默认
 *  指向访问者自己的 127.0.0.1，而服务器上什么都没有，于是「观测」永远只显示连不上
 *  （用户报的"每次打开都是本地启动，但我本地啥也没有"）。
 *
 *  回环绝对地址仍然接受（本地开发），且**只认回环** —— 这条安全性质一字未改。
 *  observation model 的凭据始终留在工作台里，前端只拿公开地址。 */
export function observeWorkbenchURL(page: "overview" | "traces" | "settings" = "overview"): string {
  const configured = runtimeConfig().observeWorkbenchUrl
    ?? import.meta.env.VITE_OBSERVE_WORKBENCH_URL
    ?? "/observe/";
  // 同源路径：控制台自己把 /observe/* 反代到**服务器回环**上的工作台。
  if (configured.startsWith("/")) {
    const sameOrigin = new URL(configured, window.location.origin);
    if (sameOrigin.origin !== window.location.origin) {
      throw new Error("观测工作台的同源路径不能指向别的 origin");
    }
    sameOrigin.hash = page;
    return sameOrigin.href;
  }
  // 回环绝对地址（本地开发）：校验原样保留。
  const url = new URL(configured);
  if (!["http:", "https:"].includes(url.protocol) || url.username || url.password
    || !["localhost", "127.0.0.1", "[::1]"].includes(url.hostname)
    || url.pathname !== "/" || url.search) {
    throw new Error("本地工作台地址须为回环 HTTP(S) 地址，例如 http://127.0.0.1:18090/");
  }
  url.hash = page;
  return url.href;
}
