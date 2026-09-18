/** 项目域（Go：4.1 projects CRUD + 建项/更新回执读面 + 执行配置档案）。
 *
 *  端点与形状唯一来源：`docs/current/api-design.md` §4.1 与附录 C as-built；
 *  json 字段为 Go json tag 原样（internal/projects/types.go）。写操作要求
 *  **恰好一个** `Idempotency-Key` 头（缺失/重复即 400 INVALID_IDEMPOTENCY_KEY），
 *  且 Content-Type 必须是 application/json（415 否则）。 */
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

/** GET /api/projects/{projectId}/repositories — 项目仓库（一项目一仓 as-built）。 */
export function listProjectRepositories(
  projectId: string,
  query?: PageQuery,
): Promise<{ items: unknown[]; nextCursor: string | null; projectRevision: string; restrictedRepositoryCount: number }> {
  return apiRequest("GET", `/projects/${encodeURIComponent(projectId)}/repositories${pageQuery(query)}`);
}

/** GET /api/configuration-profiles — 执行配置档案目录。 */
export function listConfigurationProfiles(query?: PageQuery): Promise<ConfigurationProfilePage> {
  return apiRequest<ConfigurationProfilePage>("GET", `/configuration-profiles${pageQuery(query)}`);
}
