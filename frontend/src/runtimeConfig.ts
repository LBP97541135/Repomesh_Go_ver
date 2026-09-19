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

/** Public address only; observation model credentials stay in the workbench. */
export function observeWorkbenchURL(page: "overview" | "traces" | "settings" = "overview"): string {
  const configured = runtimeConfig().observeWorkbenchUrl
    ?? import.meta.env.VITE_OBSERVE_WORKBENCH_URL
    ?? "http://127.0.0.1:18090/";
  const url = new URL(configured);
  if (!["http:", "https:"].includes(url.protocol) || url.username || url.password
    || !["localhost", "127.0.0.1", "[::1]"].includes(url.hostname)
    || url.pathname !== "/" || url.search) {
    throw new Error("本地工作台地址须为回环 HTTP(S) 地址，例如 http://127.0.0.1:18090/");
  }
  url.hash = page;
  return url.href;
}
