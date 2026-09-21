/** 项目域（Go：4.1 projects CRUD + 建项/更新回执读面 + 执行配置档案）。
 *
 *  端点与形状唯一来源：`docs/current/api-design.md` §4.1 与附录 C as-built；
 *  json 字段为 Go json tag 原样（internal/projects/types.go）。写操作要求
 *  **恰好一个** `Idempotency-Key` 头（缺失/重复即 400 INVALID_IDEMPOTENCY_KEY），
 *  且 Content-Type 必须是 application/json（415 否则）。 */
import type { RepositoryCandidate } from "./repositories";
import { allPages } from "./pagination";
import { apiRequest } from "./http";

const KEY_HEADER = (key: string) => ({ "Idempotency-Key": key });

/** 配置取舍（internal/projects ProfileChoice）：`mode` 为继承/显式选择的模式串，
 *  显式时带 `id`。 */
export interface ProfileChoice {
  mode: string;
  id?: string;
}

export interface ConfigurationChoice {
  modelProfile: ProfileChoice;
  executionProfile: ProfileChoice;
}

/** POST /api/projects 请求体（CreateInput，as-built）。 */
export interface ProjectCreateInput {
  name: string;
  purpose: string;
  repositoryIds: string[];
  /** 可省略:后端缺省继承(modelProfile/executionProfile 均 inherit)。 */
  configuration?: ConfigurationChoice;
}

/** PATCH /api/projects/{projectId} 请求体（UpdateInput）：字段级可空 = 不改。 */
export interface ProjectUpdateInput {
  expectedProjectRevision: string;
  name?: string;
  purpose?: string;
  repositoryIdsToAdd?: string[];
  /** 用**仓库 URL** 指名要接入的仓（2026-09-20）。
   *
   *  `repositoryIdsToAdd` 只吃 `repo_<20 位 GitHub 数字 id>`，而那个 id 只有**发现面**
   *  给得出来；仓库页列的是**扫描目录**，id 是 32 位随机 hex——拿它去调必得 422
   *  （线上实测：点「接入本项目」四次，后端四次 422 VALIDATION_FAILED）。
   *  扫描目录本来就有全 URL，所以走这个字段：后端按 owner/name 取数字 id、
   *  登记进项目注册表，再走同一条接入路径。 */
  repositoryUrlsToAdd?: string[];
  configuration?: ConfigurationChoice;
}

export interface ProjectLinks {
  project: string;
  operation: string;
}

/** 建项回执（CreationReceipt）：`GET /api/project-creations/{id}` 同形状。 */
export interface ProjectCreationReceipt {
  projectCreationId: string;
  status: string;
  projectId: string;
  projectRevision: string;
  createdAt: string;
  links: ProjectLinks;
}

/** 更新回执（UpdateReceipt）：`GET /api/projects/{id}/updates/{updateId}` 同形状。 */
export interface ProjectUpdateReceipt {
  updateId: string;
  status: string;
  projectId: string;
  projectRevision: string;
  updatedAt: string;
  links: ProjectLinks;
}

/** GET /api/projects 列表行（ProjectListItem，字段待页面接入时回填）。 */
export interface ProjectListItem {
  id: string;
  name: string;
  [key: string]: unknown;
}

export interface ProjectPage {
  items: ProjectListItem[];
  nextCursor: string | null;
}

/** GET /api/configuration-profiles 行（ProfileItem）。 */
export interface ConfigurationProfileItem {
  id: string;
  name: string;
  availability: string;
}

export interface ConfigurationProfilePage {
  items: ConfigurationProfileItem[];
  nextCursor: string | null;
  defaultProfileId: string | null;
}

/** GET /api/projects/{projectId} 详情（ProjectView；configuration 内层
 *  effective/checks/fixedSummary/quotaObservation 待页面接入时回填）。 */
export interface ProjectView {
  id: string;
  name: string;
  purpose: string;
  projectRevision: string;
  createdAt: string;
  configuration: {
    modelProfile: ProfileChoice;
    executionProfile: ProfileChoice;
    effective: Record<string, unknown>;
    checks: unknown;
    fixedSummary: Record<string, unknown>;
    quotaObservation: Record<string, unknown>;
  };
  actions: { canEdit: boolean; canCreateIssue: boolean };
  creationReadiness: { status: string } & Record<string, unknown>;
}

export interface PageQuery {
  q?: string;
  cursor?: string;
  /** 1..100，后端缺省 50。 */
  limit?: number;
}

function pageQuery(query?: PageQuery): string {
  if (!query) return "";
  const params = new URLSearchParams();
  if (query.q) params.set("q", query.q);
  if (query.cursor) params.set("cursor", query.cursor);
  if (query.limit !== undefined) params.set("limit", String(query.limit));
  const qs = params.toString();
  return qs ? `?${qs}` : "";
}

/** GET /api/projects — 项目列表（成员可见全集，服务端按主体裁权）。 */
export function listProjects(query?: PageQuery): Promise<ProjectPage> {
  return apiRequest<ProjectPage>("GET", `/projects${pageQuery(query)}`);
}

/** POST /api/projects — 建项。201 首建 / 200 同键重放。 */
export function createProject(
  input: ProjectCreateInput,
  idempotencyKey: string,
): Promise<ProjectCreationReceipt> {
  return apiRequest<ProjectCreationReceipt>("POST", "/projects", input, KEY_HEADER(idempotencyKey));
}

/** 归档/还原回执（Go：ProjectArchiveReceipt）。Archived=true = 项目此刻在归档态。 */
export interface ProjectArchiveReceipt {
  projectId: string;
  name: string;
  archived: boolean;
  removedAt: string | null;
}

/** POST /api/projects/{projectId}/archive — 归档（软删除：removed_at 墓碑，数据保留）。 */
export function archiveProject(projectId: string): Promise<ProjectArchiveReceipt> {
  return apiRequest<ProjectArchiveReceipt>(
    "POST",
    `/projects/${encodeURIComponent(projectId)}/archive`,
  );
}

/** POST /api/projects/{projectId}/restore — 还原已归档项目（摘掉墓碑）。 */
export function restoreProject(projectId: string): Promise<ProjectArchiveReceipt> {
  return apiRequest<ProjectArchiveReceipt>(
    "POST",
    `/projects/${encodeURIComponent(projectId)}/restore`,
  );
}

/** 已归档项目行（Go：ArchivedProjectItem）。 */
export interface ArchivedProjectItem {
  id: string;
  name: string;
  createdAt: string;
  removedAt: string | null;
}

export interface ArchivedProjectPage {
  items: ArchivedProjectItem[];
}

/** GET /api/projects/archived — 已归档项目列表（不分页，封顶 200，最近归档的排前）。 */
export function listArchivedProjects(): Promise<ArchivedProjectPage> {
  return apiRequest<ArchivedProjectPage>("GET", "/projects/archived");
}

/** GET /api/project-creations/{projectCreationId} — 建项回执查询。 */
export function getProjectCreation(projectCreationId: string): Promise<ProjectCreationReceipt> {
  return apiRequest<ProjectCreationReceipt>(
    "GET",
    `/project-creations/${encodeURIComponent(projectCreationId)}`,
  );
}

/** GET /api/projects/{projectId} — 项目详情。 */
export function getProject(projectId: string): Promise<ProjectView> {
  return apiRequest<ProjectView>("GET", `/projects/${encodeURIComponent(projectId)}`);
}

/** PATCH /api/projects/{projectId} — 乐观锁更新（expectedProjectRevision 不符即 409）。 */
export function updateProject(
  projectId: string,
  input: ProjectUpdateInput,
  idempotencyKey: string,
): Promise<ProjectUpdateReceipt> {
  return apiRequest<ProjectUpdateReceipt>(
    "PATCH",
    `/projects/${encodeURIComponent(projectId)}`,
    input,
    KEY_HEADER(idempotencyKey),
  );
}

/** GET /api/projects/{projectId}/updates/{updateId} — 更新回执查询。 */
export function getProjectUpdate(projectId: string, updateId: string): Promise<ProjectUpdateReceipt> {
  return apiRequest<ProjectUpdateReceipt>(
    "GET",
    `/projects/${encodeURIComponent(projectId)}/updates/${encodeURIComponent(updateId)}`,
  );
}

/** GET /api/projects/{projectId}/repositories — 项目已接入仓库。 */
export function listProjectRepositories(
  projectId: string,
  query?: PageQuery,
): Promise<{ items: RepositoryCandidate[]; nextCursor: string | null; projectRevision: string; restrictedRepositoryCount: number }> {
  return apiRequest("GET", `/projects/${encodeURIComponent(projectId)}/repositories${pageQuery(query)}`);
}

/** GET /api/configuration-profiles — 配置档案目录（model / execution 两类）。
 *
 *  2026-09-22：**kind 改成必填参数**。此前这个函数签名里没有 kind，而
 *  ``pageQuery`` 也不认它 —— 也就是说**调用方根本没有办法按 kind 查**，
 *  发出去的永远是裸 ``/configuration-profiles``。后端 ``ParseProfileQuery``
 *  要求 kind，所以这条读面此前**不可能被正确调用过**（与它零调用互为印证：
 *  没人用，所以没人发现它调不对）。
 *
 *  kind 走 query 参数而不是路径段（与后端的读面形状一致），
 *  但与 ``setDefaultConfigurationProfile`` 的路径段形态**刻意不同**：
 *  写操作把身份放路径里便于路由与鉴权，读操作的筛选条件放 query 里。
 */
export function listConfigurationProfiles(
  kind: "model" | "execution",
  query?: PageQuery,
): Promise<ConfigurationProfilePage> {
  const extra = pageQuery(query);
  const suffix = extra ? `&${extra.slice(1)}` : "";
  return apiRequest<ConfigurationProfilePage>(
    "GET",
    `/configuration-profiles?kind=${encodeURIComponent(kind)}${suffix}`,
  );
}

/** PUT /api/configuration-profiles/{kind}/default —— 把某个档案设成**默认**。
 *
 *  2026-09-22 补：这是那条缺失链路的前端一半。
 *
 *  在此之前 `repomesh_projects.defaults` 只有导入路径能写、**没有任何 HTTP 入口**，
 *  而 `model` 那一侧连写函数都不存在 —— 线上实测 catmem 有一个完整可用的模型档案
 *  （deepseek，enabled），却**没有任何办法把它设成默认**，于是他的项目走 `inherit`
 *  解析到空，建 issue 被「执行配置未完成」**永久**阻断，而且他自己解不开。
 *
 *  **版本号不传**：默认永远指向档案的 current_version（服务端定），
 *  否则会出现"默认指向一个已经不是当前的版本"，与档案页显示的不一致。
 *
 *  kind 走**路径段**而不是请求体 —— 它是资源身份的一部分（model 与 execution 是
 *  两类档案），放路径里路由与鉴权一眼可见。
 *
 *  失败语义（服务端）：422 = kind 不在闭集里或 profileId 为空；
 *  404 = 档案不存在 / 不是你的 / 未启用 / 当前版本行缺失（**不区分**，不泄露存在性）。 */
export function setDefaultConfigurationProfile(
  kind: "model" | "execution",
  profileId: string,
): Promise<{ kind: string; profileId: string; status: string }> {
  return apiRequest("PUT", `/configuration-profiles/${encodeURIComponent(kind)}/default`, {
    profileId,
  });
}

/** POST /api/configuration-profiles/execution —— 建一条**执行档案**（含 v1 版本）。
 *
 *  2026-09-22 补：执行档案此前**只能由导入路径**写入，没有任何 HTTP/UI 入口 ——
 *  线上实测 catmem **一个执行档案都没有**，于是他的项目走 `inherit` 解析不到执行侧，
 *  建 issue 被「执行配置未完成」永久阻断，而且他自己解不开。
 *
 *  **政策不用传**：预算政策与时限政策是**全局**的（`request_policy_versions` /
 *  `time_policy_versions` 都没有 owner 列），服务端直接引用现成版本。
 *  服务端取不到政策时回 409 `POLICY_NOT_CONFIGURED` —— 如实说"没配政策"，
 *  而不是建一个没有约束的空壳档案。
 *
 *  422 = 名字为空/超长，或 workerConcurrency 不在 1..16。 */
export function registerExecutionProfile(
  name: string,
  workerConcurrency: number,
): Promise<{ kind: string; profileId: string; version: string }> {
  return apiRequest("POST", "/configuration-profiles/execution", { name, workerConcurrency });
}

export function listAllProjects(): Promise<ProjectListItem[]> {
  return allPages((cursor) => listProjects({ cursor, limit: 100 }));
}
export async function allProjectRepositories(projectId: string) {
  let revision: string | undefined;
  let restricted = 0;
  const items = await allPages(async (cursor) => {
    const page = await listProjectRepositories(projectId, { cursor, limit: 100 });
    if (revision && revision !== page.projectRevision) throw new Error("项目已更新，请重新加载仓库。");
    revision = page.projectRevision;
    restricted = page.restrictedRepositoryCount;
    return page;
  });
  return { items, projectRevision: revision!, restrictedRepositoryCount: restricted };
}
