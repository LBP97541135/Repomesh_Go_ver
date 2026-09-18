import type { ApiError, RepositoryPage, Result } from "./api";
import { parseCapability, parseRepository, request } from "./api";
import { identifier, normalizeProjectOperationKey, object, text, timestamp } from "./values";

export { parseErrorDiagnostics } from "./api";

export type Capability = RepositoryPage["items"][number]["userParticipation"];
export type RepositoryItem = RepositoryPage["items"][number];
export type ProfileKind = "model" | "execution";
export type ProfileSelection =
  | { readonly mode: "inherit"; readonly id?: never }
  | { readonly mode: "reference"; readonly id: string };
export type ConfigurationSelection = {
  readonly modelProfile: ProfileSelection;
  readonly executionProfile: ProfileSelection;
};
export type ProjectConfiguration = ConfigurationSelection & {
  effective: {
    configurationRevision: string;
    modelProfileId: string | null;
    executionProfileId: string | null;
    workerConcurrency: number | null;
    budgetPolicyId: string | null;
    timeLimitPolicyId: string | null;
    verificationGroupEnabled: boolean | null;
  };
  checks: Capability;
};
export type ProjectSummary = { id: string; name: string; projectRevision: string; createdAt: string };
export type ProjectView = ProjectSummary & {
  purpose: string;
  configuration: ProjectConfiguration;
  actions: { canEdit: boolean; canCreateIssue: boolean };
  creationReadiness: { status: "ready" | "restricted" | "unknown"; reasonCodes: string[]; observedAt: string | null };
};
export type ProjectList = { items: ProjectSummary[]; nextCursor: string | null };
export type ProjectRepositoryPage = { items: RepositoryItem[]; nextCursor: string | null; projectRevision: string; restrictedRepositoryCount: number };
export type ProfilePage = { items: { id: string; name: string; availability: Capability }[]; nextCursor: string | null; defaultProfileId: string | null };
export type ProjectCreateInput = { readonly name: string; readonly purpose: string; readonly repositoryIds: readonly string[]; readonly configuration?: ConfigurationSelection };
export type ProjectUpdateFields = { readonly name?: string; readonly purpose?: string; readonly repositoryIdsToAdd?: readonly string[]; readonly configuration?: ConfigurationSelection };
export type ProjectUpdateInput = { readonly expectedProjectRevision: string } & (
  | (ProjectUpdateFields & { readonly name: string })
  | (ProjectUpdateFields & { readonly purpose: string })
  | (ProjectUpdateFields & { readonly repositoryIdsToAdd: readonly string[] })
  | (ProjectUpdateFields & { readonly configuration: ConfigurationSelection })
);
export type ProjectCreationReceipt = { projectCreationId: string; status: "committed"; projectId: string; projectRevision: string; createdAt: string; links: { project: string; operation: string } };
export type ProjectUpdateReceipt = { updateId: string; status: "committed"; projectId: string; projectRevision: string; updatedAt: string; links: { project: string; operation: string } };
export type ProjectFieldError = { field: string; code: string };
export type ProjectErrorCode =
  | "INVALID_JSON" | "VALIDATION_FAILED" | "REQUEST_TOO_LARGE"
  | "AUTHENTICATION_REQUIRED" | "PROJECT_CREATE_NOT_ALLOWED" | "PROJECT_UPDATE_NOT_ALLOWED"
  | "RESOURCE_NOT_FOUND" | "INVALID_IDEMPOTENCY_KEY" | "IDEMPOTENCY_CONFLICT"
  | "PROJECT_REVISION_CONFLICT" | "PROJECT_CONTEXT_CHANGED"
  | "INVALID_CURSOR" | "CURSOR_EXPIRED"
  | "PROJECT_CREATION_NOT_FOUND" | "PROJECT_UPDATE_NOT_FOUND"
  | "PROJECT_CREATION_RESULT_REMOVED" | "PROJECT_UPDATE_RESULT_REMOVED"
  | "RATE_LIMITED" | "AUTHORIZATION_UNCONFIRMED" | "RESULT_UNCONFIRMED";
export type ApiErrorDiagnostics = { fieldErrors?: readonly ProjectFieldError[]; requestId?: string };

function integer(value: unknown, minimum = 0): number {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < minimum) throw new Error("Invalid integer");
  return value;
}

export function parseProfileSelection(value: unknown): ProfileSelection {
  const data = object(value);
  if (data.mode === "inherit" && Object.keys(data).length === 1) return { mode: "inherit" };
  if (data.mode === "reference" && Object.keys(data).length === 2) return { mode: "reference", id: identifier(data.id) };
  throw new Error("Invalid profile selection");
}

export function parseConfigurationSelection(value: unknown): ConfigurationSelection {
  const data = object(value);
  if (Object.keys(data).length !== 2) throw new Error("Invalid configuration selection");
  return { modelProfile: parseProfileSelection(data.modelProfile), executionProfile: parseProfileSelection(data.executionProfile) };
}

function parseConfiguration(value: unknown): ProjectConfiguration {
  const data = object(value);
  const selection = parseConfigurationSelection({ modelProfile: data.modelProfile, executionProfile: data.executionProfile });
  const effective = object(data.effective);
  if (typeof effective.verificationGroupEnabled !== "boolean" && effective.verificationGroupEnabled !== null) throw new Error("Invalid verification setting");
  return {
    ...selection,
    effective: {
      configurationRevision: text(effective.configurationRevision, 128),
      modelProfileId: effective.modelProfileId === null ? null : identifier(effective.modelProfileId),
      executionProfileId: effective.executionProfileId === null ? null : identifier(effective.executionProfileId),
      workerConcurrency: effective.workerConcurrency === null ? null : integer(effective.workerConcurrency, 1),
      budgetPolicyId: effective.budgetPolicyId === null ? null : identifier(effective.budgetPolicyId),
      timeLimitPolicyId: effective.timeLimitPolicyId === null ? null : identifier(effective.timeLimitPolicyId),
      verificationGroupEnabled: effective.verificationGroupEnabled,
    },
    checks: parseCapability(data.checks),
  };
}

function summary(value: unknown): ProjectSummary {
  const data = object(value);
  return { id: identifier(data.id), name: text(data.name, 200), projectRevision: text(data.projectRevision, 128), createdAt: timestamp(data.createdAt) };
}

export function parseProject(value: unknown): ProjectView {
  const data = object(value);
  const base = summary(value);
  const actions = object(data.actions);
  const readiness = object(data.creationReadiness);
  if (readiness.status !== "ready" && readiness.status !== "restricted" && readiness.status !== "unknown") throw new Error("Invalid readiness");
  if (typeof actions.canEdit !== "boolean" || typeof actions.canCreateIssue !== "boolean") throw new Error("Invalid actions");
  const readinessObservation = parseCapability({ status: "allowed", reasonCodes: readiness.reasonCodes, observedAt: readiness.observedAt });
  return {
    ...base,
    purpose: text(data.purpose, 20000),
    configuration: parseConfiguration(data.configuration),
    actions: { canEdit: actions.canEdit, canCreateIssue: actions.canCreateIssue },
    creationReadiness: { status: readiness.status, reasonCodes: readinessObservation.reasonCodes, observedAt: readinessObservation.observedAt },
  };
}

export function parseProjects(value: unknown): ProjectList {
  const data = object(value);
  if (!Array.isArray(data.items) || data.items.length > 100) throw new Error("Invalid projects");
  return { items: data.items.map(summary), nextCursor: data.nextCursor === null ? null : text(data.nextCursor, 8192) };
}

export function parseProjectRepositories(value: unknown): ProjectRepositoryPage {
  const data = object(value);
  if (!Array.isArray(data.items) || data.items.length > 100) throw new Error("Invalid repositories");
  return { items: data.items.map(parseRepository), nextCursor: data.nextCursor === null ? null : text(data.nextCursor, 8192), projectRevision: text(data.projectRevision, 128), restrictedRepositoryCount: integer(data.restrictedRepositoryCount) };
}

export function parseProfiles(value: unknown): ProfilePage {
  const data = object(value);
  if (!Array.isArray(data.items) || data.items.length > 100) throw new Error("Invalid profiles");
  const items = data.items.map((value) => {
    const item = object(value);
    return { id: identifier(item.id), name: text(item.name, 512), availability: parseCapability(item.availability) };
  });
  return { items, nextCursor: data.nextCursor === null ? null : text(data.nextCursor, 8192), defaultProfileId: data.defaultProfileId === null ? null : identifier(data.defaultProfileId) };
}

function receiptLinks(value: unknown): { project: string; operation: string } {
  const links = object(value);
  return { project: text(links.project, 1024), operation: text(links.operation, 1024) };
}

export function parseProjectCreation(value: unknown): ProjectCreationReceipt {
  const data = object(value);
  if (data.status !== "committed") throw new Error("Invalid receipt status");
  if (typeof data.projectCreationId !== "string") throw new Error("Invalid operation identity");
  const projectCreationId = normalizeProjectOperationKey(data.projectCreationId);
  if (projectCreationId === null) throw new Error("Invalid operation identity");
  const projectId = identifier(data.projectId);
  const links = receiptLinks(data.links);
  if (links.project !== `/api/projects/${projectId}` || links.operation !== `/api/project-creations/${projectCreationId}`) throw new Error("Invalid receipt links");
  return { projectCreationId, status: "committed", projectId, projectRevision: text(data.projectRevision, 128), createdAt: timestamp(data.createdAt), links };
}

export function parseProjectUpdate(value: unknown): ProjectUpdateReceipt {
  const data = object(value);
  if (data.status !== "committed") throw new Error("Invalid receipt status");
  if (typeof data.updateId !== "string") throw new Error("Invalid operation identity");
  const updateId = normalizeProjectOperationKey(data.updateId);
  if (updateId === null) throw new Error("Invalid operation identity");
  const projectId = identifier(data.projectId);
  const links = receiptLinks(data.links);
  if (links.project !== `/api/projects/${projectId}` || links.operation !== `/api/projects/${projectId}/updates/${updateId}`) throw new Error("Invalid receipt links");
  return { updateId, status: "committed", projectId, projectRevision: text(data.projectRevision, 128), updatedAt: timestamp(data.updatedAt), links };
}

function checkCreation(result: Result<ProjectCreationReceipt>, key: string): Result<ProjectCreationReceipt> {
  const normalized = normalizeProjectOperationKey(key);
  return normalized === null || result.kind === "ok" && result.value.projectCreationId !== normalized ? { kind: "error", status: 200, code: "INVALID_RESPONSE", retryAt: null } : result;
}

function checkUpdate(result: Result<ProjectUpdateReceipt>, projectId: string, key: string): Result<ProjectUpdateReceipt> {
  const normalized = normalizeProjectOperationKey(key);
  return normalized === null || result.kind === "ok" && (result.value.projectId !== projectId || result.value.updateId !== normalized) ? { kind: "error", status: 200, code: "INVALID_RESPONSE", retryAt: null } : result;
}

export function readProjects(args: { query: string; cursor: string | null; limit?: number; signal?: AbortSignal }): Promise<Result<ProjectList>> {
  const params = new URLSearchParams({ q: args.query, limit: String(args.limit ?? 50) });
  if (args.cursor !== null) params.set("cursor", args.cursor);
  return request({ path: `/api/projects?${params}`, parse: parseProjects, signal: args.signal });
}

export function readProject(args: { projectId: string; signal?: AbortSignal }): Promise<Result<ProjectView>> {
  return request({ path: `/api/projects/${encodeURIComponent(args.projectId)}`, parse: parseProject, signal: args.signal });
}

export function readProjectRepositories(args: { projectId: string; cursor: string | null; limit?: number; signal?: AbortSignal }): Promise<Result<ProjectRepositoryPage>> {
  const params = new URLSearchParams({ limit: String(args.limit ?? 50) });
  if (args.cursor !== null) params.set("cursor", args.cursor);
  return request({ path: `/api/projects/${encodeURIComponent(args.projectId)}/repositories?${params}`, parse: parseProjectRepositories, signal: args.signal });
}

export function readConfigurationProfiles(args: { kind: ProfileKind; cursor: string | null; limit?: number; signal?: AbortSignal }): Promise<Result<ProfilePage>> {
  const params = new URLSearchParams({ kind: args.kind, limit: String(args.limit ?? 50) });
  if (args.cursor !== null) params.set("cursor", args.cursor);
  return request({ path: `/api/configuration-profiles?${params}`, parse: parseProfiles, signal: args.signal });
}

export async function createProject(args: { key: string; input: ProjectCreateInput; csrfToken: string; signal?: AbortSignal }): Promise<Result<ProjectCreationReceipt>> {
  const key = normalizeProjectOperationKey(args.key);
  if (key === null) return { kind: "error", status: 400, code: "INVALID_IDEMPOTENCY_KEY", retryAt: null };
  return checkCreation(await request({ path: "/api/projects", method: "POST", body: args.input, csrfToken: args.csrfToken, key, parse: parseProjectCreation, signal: args.signal, successStatuses: [200, 201] }), key);
}

export async function updateProject(args: { projectId: string; key: string; input: ProjectUpdateInput; csrfToken: string; signal?: AbortSignal }): Promise<Result<ProjectUpdateReceipt>> {
  const key = normalizeProjectOperationKey(args.key);
  if (key === null) return { kind: "error", status: 400, code: "INVALID_IDEMPOTENCY_KEY", retryAt: null };
  return checkUpdate(await request({ path: `/api/projects/${encodeURIComponent(args.projectId)}`, method: "PATCH", body: args.input, csrfToken: args.csrfToken, key, parse: parseProjectUpdate, signal: args.signal }), args.projectId, key);
}

export async function readProjectCreation(args: { key: string; signal?: AbortSignal }): Promise<Result<ProjectCreationReceipt>> {
  const key = normalizeProjectOperationKey(args.key);
  if (key === null) return { kind: "error", status: 400, code: "INVALID_IDEMPOTENCY_KEY", retryAt: null };
  return checkCreation(await request({ path: `/api/project-creations/${encodeURIComponent(key)}`, parse: parseProjectCreation, signal: args.signal }), key);
}

export async function readProjectUpdate(args: { projectId: string; key: string; signal?: AbortSignal }): Promise<Result<ProjectUpdateReceipt>> {
  const key = normalizeProjectOperationKey(args.key);
  if (key === null) return { kind: "error", status: 400, code: "INVALID_IDEMPOTENCY_KEY", retryAt: null };
  return checkUpdate(await request({ path: `/api/projects/${encodeURIComponent(args.projectId)}/updates/${encodeURIComponent(key)}`, parse: parseProjectUpdate, signal: args.signal }), args.projectId, key);
}

export function projectErrorMessage(error: ApiError): string {
  switch (error.code) {
    case "INVALID_JSON": return "请求内容格式无效，请检查后重试。";
    case "VALIDATION_FAILED": return "项目内容未通过校验，请检查标出的字段。";
    case "REQUEST_TOO_LARGE": return "项目内容超过允许大小，请缩短后重试。";
    case "AUTHENTICATION_REQUIRED": return "登录已失效，请重新登录后继续。";
    case "PROJECT_CREATE_NOT_ALLOWED": return "当前账号不能创建项目。";
    case "PROJECT_UPDATE_NOT_ALLOWED": return "当前项目只读，不能保存修改。";
    case "RESOURCE_NOT_FOUND": return "当前无法访问该项目或资源。";
    case "INVALID_IDEMPOTENCY_KEY": return "原操作编号无效，不能发送请求。";
    case "IDEMPOTENCY_CONFLICT": return "原操作对应另一份输入，请查询原结果。";
    case "PROJECT_REVISION_CONFLICT": return "项目已经更新，请读取当前值并核对修改。";
    case "PROJECT_CONTEXT_CHANGED": return "项目范围已经更新，请从第一页重新读取。";
    case "INVALID_CURSOR":
    case "CURSOR_EXPIRED": return "分页已失效，请从第一页重新读取。";
    case "PROJECT_CREATION_NOT_FOUND":
    case "PROJECT_UPDATE_NOT_FOUND": return "尚未查到原操作结果，结果仍待确认。";
    case "PROJECT_CREATION_RESULT_REMOVED":
    case "PROJECT_UPDATE_RESULT_REMOVED": return "原操作结果已清理，不能重新执行。";
    case "RATE_LIMITED": return "请求较频繁，请等待后再查询原操作。";
    case "AUTHORIZATION_UNCONFIRMED": return "当前权限核查结果未知，请稍后查询原操作。";
    case "RESULT_UNCONFIRMED": return "提交结果未知，请查询原操作。";
    case "INVALID_RESPONSE": return "服务器返回了无法验证的项目结果。";
    case "NETWORK_ERROR": return "暂时无法连接服务器，提交结果可能未知。";
    default: return error.status >= 500 ? "项目服务暂时不可用，请稍后重试。" : "暂时无法完成项目请求。";
  }
}
