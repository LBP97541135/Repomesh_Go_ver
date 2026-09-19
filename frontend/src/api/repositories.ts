/** 仓库网格域（Go：B 板块 repositories + D 板块 scan-jobs）。
 *
 *  **按域拆分的样板文件**：每域一个文件、走 `apiRequest`、禁止往 client.ts 加
 *  方法——其他域（scope/issues/skills…）照本文件的模式写。
 *  端点与形状唯一来源：`docs/current/api-design.md` §3.B/§3.D 与附录 C。
 *  标注「形状待核」的响应类型是按实现粗读写的，接页面时以后端 handler 为准回填。 */
import { apiRequest } from "./http";

/** `repomesh_scan.repositories` 行的浏览器投影（api-design.md §3.B；总册 D 板块）。
 *  JSON 字段为 Go json tag；`metadata`（AutoCard+observedCalls）暂未暴露。 */
export interface RepositoryCard {
  id: string;
  name: string;
  url: string;
  description: string;
  topics: string[];
  languages: string[];
  fingerprint?: string;
  profiledAt?: string;
  scanStatus?: string;
}

/** GET /api/scan/repositories — 已扫描仓库目录（后端返回卡片数组本身，
 *  形状已核 internal/scan/http.go handleRepositoryList）。
 *
 *  2026-09-19 修正：此前打的是 `/repositories`，而那个路径现在归
 *  internal/web/auth.go 的 GitHub 发现处理器所有，返回的是
 *  access.RepositoryPage **对象**（{items,nextCursor}）。前端拿对象当数组用，
 *  仓库页在 useMemo 里直接 `TypeError: n.forEach is not a function` 白屏。
 *  scan 目录在 as-built 里已挪到 /api/scan/repositories，这里跟过去。 */
export function listRepositories(): Promise<RepositoryCard[]> {
  return apiRequest<RepositoryCard[]>("GET", "/scan/repositories");
}

export interface UrlIdentification {
  url: string;
  /** `single_repo` / `group` / `unknown`（总册 §2.4 枚举）。 */
  url_type: "single_repo" | "group" | "unknown";
  /** Go 原始值可能是 unsupported/unknown——adapter 层收敛为契约的三值。 */
  platform: string;
  /** 单仓时 = 将注册的仓库名；group / unknown 时缺省。 */
  repository_name?: string;
}

/** GET /api/repositories/url-type?url= — 离线 URL 徽标（D-2）。 */
export function getUrlType(url: string): Promise<UrlIdentification> {
  return apiRequest<UrlIdentification>("GET", `/repositories/url-type?url=${encodeURIComponent(url)}`);
}

export interface ScanJobCreateRequest {
  /** `organization`（组织扫描）或 `repository`（单仓扫描）。 */
  kind: "organization" | "repository";
  url: string;
}

/** Go 扫描作业出参（internal/scan/jobs.go 的 json tag 原样）。 */
export interface ScanJobView {
  id: string;
  kind: "organization" | "repository";
  url: string;
  status: "running" | "succeeded" | "failed";
  total: number;
  scanned: number;
  lastScannedRepository?: string;
  registered: number;
  skipped: number;
  failed?: number;
  error?: string;
  startedAt: string;
  finishedAt?: string;
}

/** POST /api/scan-jobs — 提交扫描作业（替代 Python 的 scan-org/scan-repo 两条）。 */
export function createScanJob(req: ScanJobCreateRequest): Promise<ScanJobView> {
  return apiRequest<ScanJobView>("POST", "/scan-jobs", req);
}

/** GET /api/scan-jobs/{id} — 轮询作业状态（前端每 2s 一次直到终态）。 */
export function getScanJob(id: string): Promise<ScanJobView> {
  return apiRequest<ScanJobView>("GET", `/scan-jobs/${encodeURIComponent(id)}`);
}

export interface ScopeAssistSetting {
  feature: string;
  enabled: boolean;
}

/** GET /api/settings/scope-assist — 范围辅助开关（关 = 仅手动选仓）。 */
export function getScopeAssist(): Promise<ScopeAssistSetting> {
  return apiRequest<ScopeAssistSetting>("GET", "/settings/scope-assist");
}

/** PUT /api/settings/scope-assist — 切换范围辅助（写：会话 + Origin + CSRF）。 */
export function putScopeAssist(enabled: boolean): Promise<ScopeAssistSetting> {
  return apiRequest<ScopeAssistSetting>("PUT", "/settings/scope-assist", { enabled });
}

/** GET /api/repositories/candidates 候选行（access.RepositoryItem）。 */
export interface RepositoryCandidate {
  id: string;
  displayName: string;
  userParticipation: string;
  appCapability: string;
}

export interface RepositoryCandidatePage {
  items: RepositoryCandidate[];
  nextCursor: string | null;
  coverage: { status: string; reasonCodes: string[]; observedAt: string | null };
}

/** GET /api/repositories/candidates — B02 可参与仓库候选（会话必需；q/cursor/limit）。 */
export function listRepositoryCandidates(query?: { q?: string; cursor?: string; limit?: number }): Promise<RepositoryCandidatePage> {
  const params = new URLSearchParams();
  if (query?.q) params.set("q", query.q);
  if (query?.cursor) params.set("cursor", query.cursor);
  if (query?.limit !== undefined) params.set("limit", String(query.limit));
  const qs = params.toString();
  return apiRequest<RepositoryCandidatePage>("GET", `/repositories/candidates${qs ? `?${qs}` : ""}`);
}

/** GET /api/repositories/dependents 行：依赖 X 的仓库边（B11 重规划 Step 4b 输入）。 */
export interface RepositoryDependent {
  repository: string;
  mechanisms: string[];
  confirmed: boolean;
}

/** GET /api/repositories/dependents?name= — 反向依赖查询（未知仓库 404）。 */
export function getRepositoryDependents(name: string): Promise<{ target: string; dependents: RepositoryDependent[] }> {
  return apiRequest("GET", `/repositories/dependents?name=${encodeURIComponent(name)}`);
}
