/** 管线扩展域(Go P1 additions):验证 / 规格 / 接口文档 / 发布。
 *
 *  端点与形状唯一来源:`internal/web/pipeline_routes2.go`、`pipeline_extensions.go`、
 *  `joint_validation.go` 与各 internal 域包的 json tag(逐字对齐,含大小写)。
 *  全部挂在 registerProjectRoute 下:会话 cookie + CSRF + Origin 校验。
 *  branch-validation 的 StartCommand 无 json tag,走 json 大小写不敏感匹配,
 *  前端一律发 camelCase。 */

import { apiRequest } from "./http";

/* ════════════ 验证域:数据库分支验证 + 联合验证 ════════════ */

/** POST /api/projects/{projectId}/branch-validations 请求体
 *  (branchvalidation.StartCommand,json 无 tag → 大小写不敏感匹配)。 */
export interface BranchValidationStartInput {
  organizationId: string;
  repositoryId: string;
  taskId: string;
  candidateSha: string;
  sourceDatabaseRef: string;
  /** 幂等键由调用方持有,重试沿用同键。 */
  idempotencyKey?: string;
  migrations?: string[];
}

export interface BranchValidationRun {
  id: string;
  repositoryId: string;
  candidateSha: string;
  provider: string;
  branchRef?: string;
  status: string;
  failureCode?: string;
  cleanupPending: boolean;
  migrationResults: Array<{ statement: string; ok: boolean; error?: string }>;
  [key: string]: unknown;
}

/** POST /api/projects/{projectId}/branch-validations — 发起数据库分支验证(201)。 */
export function startBranchValidation(
  projectId: string,
  input: BranchValidationStartInput,
): Promise<BranchValidationRun> {
  return apiRequest<BranchValidationRun>(
    "POST",
    `/projects/${encodeURIComponent(projectId)}/branch-validations`,
    input,
  );
}

/** GET /api/projects/{projectId}/branch-validations/{runId} — 验证运行详情。 */
export function getBranchValidation(
  projectId: string,
  runId: string,
): Promise<BranchValidationRun> {
  return apiRequest<BranchValidationRun>(
    "GET",
    `/projects/${encodeURIComponent(projectId)}/branch-validations/${encodeURIComponent(runId)}`,
  );
}

/** POST /api/projects/{projectId}/branch-validations/{runId}/retry-cleanup — 重试清理。 */
export function retryBranchValidationCleanup(
  projectId: string,
  runId: string,
): Promise<BranchValidationRun> {
  return apiRequest<BranchValidationRun>(
    "POST",
    `/projects/${encodeURIComponent(projectId)}/branch-validations/${encodeURIComponent(runId)}/retry-cleanup`,
  );
}

/** 联合验证条款(接口文档里的一条约定)。 */
export interface ContractClause {
  field: string;
  expected: string;
}

export interface JointValidationResult {
  verdict: "consistent" | "inconsistent";
  clauses: Array<{ field: string; expected: string; found?: string; ok: boolean }>;
}

/** POST /api/projects/{projectId}/joint-validation — 联合验证:
 *  机械比对上游任务交付摘要与接口文档条款(智能体结论不得翻转数据矛盾)。 */
export function runJointValidation(
  projectId: string,
  input: {
    planId: string;
    upstreamTaskIds: string[];
    clauses: ContractClause[];
  },
): Promise<JointValidationResult> {
  return apiRequest<JointValidationResult>(
    "POST",
    `/projects/${encodeURIComponent(projectId)}/joint-validation`,
    input,
  );
}

/* ════════════ 规格域:spec 生命周期 ════════════ */

export interface SpecView {
  id: string;
  version: number;
  title: string;
  content: string;
  /** draft | approved */
  state: string;
  authorId: string;
  [key: string]: unknown;
}

/** POST /api/projects/{projectId}/specs — 新建规格(201,authorId 取会话主体)。 */
export function createSpec(
  projectId: string,
  input: { repository: string; title: string; content: string; supersedes?: string },
): Promise<SpecView> {
  return apiRequest<SpecView>("POST", `/projects/${encodeURIComponent(projectId)}/specs`, {
    projectId: "",
    repository: input.repository,
    title: input.title,
    content: input.content,
    ...(input.supersedes ? { supersedes: input.supersedes } : {}),
  });
}

/** GET /api/projects/{projectId}/specs/current?repository= — 当前生效规格。 */
export function getCurrentSpec(projectId: string, repository?: string): Promise<SpecView> {
  const q = repository ? `?repository=${encodeURIComponent(repository)}` : "";
  return apiRequest<SpecView>("GET", `/projects/${encodeURIComponent(projectId)}/specs/current${q}`);
}

/** POST /api/projects/{projectId}/specs/{specId}/approve — 审批规格(200)。 */
export function approveSpec(projectId: string, specId: string): Promise<SpecView> {
  return apiRequest<SpecView>(
    "POST",
    `/projects/${encodeURIComponent(projectId)}/specs/${encodeURIComponent(specId)}/approve`,
  );
}

/* ════════════ 接口文档域:interface document 生命周期 ════════════ */

export interface InterfaceDocumentView {
  id: string;
  version: number;
  title: string;
  content: string;
  state: string;
  authorId: string;
  approvers: string[];
  approvedBy: string[];
  effectiveAt?: string;
  createdAt: string;
}

/** POST /api/projects/{projectId}/interface-documents — 新建接口文档版本
 *  (201;approvers = 全部须审批的 Manager,齐批后生效)。 */
export function createInterfaceDocument(
  projectId: string,
  input: { issueId?: string; title: string; content: string; approvers: string[] },
): Promise<InterfaceDocumentView> {
  return apiRequest<InterfaceDocumentView>(
    "POST",
    `/projects/${encodeURIComponent(projectId)}/interface-documents`,
    input,
  );
}

/** GET /api/projects/{projectId}/interface-documents/{docId} — 文档详情。 */
export function getInterfaceDocument(
  projectId: string,
  docId: string,
): Promise<InterfaceDocumentView> {
  return apiRequest<InterfaceDocumentView>(
    "GET",
    `/projects/${encodeURIComponent(projectId)}/interface-documents/${encodeURIComponent(docId)}`,
  );
}

/** POST /api/projects/{projectId}/interface-documents/{docId}/approve — 审批(200)。 */
export function approveInterfaceDocument(
  projectId: string,
  docId: string,
): Promise<InterfaceDocumentView> {
  return apiRequest<InterfaceDocumentView>(
    "POST",
    `/projects/${encodeURIComponent(projectId)}/interface-documents/${encodeURIComponent(docId)}/approve`,
  );
}

/* ════════════ 发布域:release push + gate ════════════ */

export interface ReleaseGateView {
  projectId: string;
  /** pending | released | rejected */
  state: string;
  decision?: string;
  summary?: string;
  decidedBy?: string;
}

/** GET /api/projects/{projectId}/release-gate — 发布门当前判定。 */
export function getReleaseGate(projectId: string): Promise<ReleaseGateView> {
  return apiRequest<ReleaseGateView>(
    "GET",
    `/projects/${encodeURIComponent(projectId)}/release-gate`,
  );
}

/** POST /api/projects/{projectId}/release-gate — 人工发布判定
 *  ({decision, summary};decidedBy 取会话主体)。 */
export function decideReleaseGate(
  projectId: string,
  input: { decision: string; summary: string },
): Promise<ReleaseGateView> {
  return apiRequest<ReleaseGateView>(
    "POST",
    `/projects/${encodeURIComponent(projectId)}/release-gate`,
    input,
  );
}

export interface ReleaseResult {
  branchRef: string;
  pullUrl: string;
}

/** POST /api/projects/{projectId}/releases — 推分支 + 建 PR(push 先行,PR 幂等)。
 *  ⚠ SCM token 取自平台凭据库,请求体不携带——前端只传工作区与分支信息。 */
export function createRelease(
  projectId: string,
  input: { workspace: string; branch: string; base: string; owner: string; repo: string; title: string; body?: string },
): Promise<ReleaseResult> {
  return apiRequest<ReleaseResult>(
    "POST",
    `/projects/${encodeURIComponent(projectId)}/releases`,
    input,
  );
}
