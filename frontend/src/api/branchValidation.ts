/** 评委建议①：数据库相关的 Coding 用 **Polar Agentic Database Branch**。
 *
 *  `internal/branchvalidation` 早已实现（provider 端口 + PolarDB 适配器 + Local
 *  兜底、逐条留迁移结果、清理失败记 cleanup_pending 可重试），路由也挂了
 *  （`internal/web/pipeline_routes2.go`），但**前端一个入口都没有** ——
 *  线上 `database_branch_validations` 至今 0 行，就是因为没有任何地方能触发它。
 *  这个文件与配套的 BranchValidationCard 就是补那一段，让这条链路**可演示**。
 *
 *  字段名逐字对齐后端：请求体走 `StartCommand`（Go 字段名，无 json tag），
 *  响应体走 `RunView`（camelCase）。两边形状不一样是既成事实，照抄不臆改。 */
import { apiRequest } from "./http";

export interface MigrationResultView {
  statement: string;
  ok: boolean;
  error?: string;
}

/** `RunView`（camelCase）。 */
export interface BranchValidationRunView {
  id: string;
  repositoryId: string;
  candidateSha: string;
  /** local-postgres | polardb-agentic-branch —— 证据上写清是在哪个 provider 上跑的 */
  provider: string;
  branchRef?: string;
  /** provisioning | validating | passed | failed */
  status: string;
  failureCode?: string;
  cleanupPending: boolean;
  migrationResults: MigrationResultView[] | null;
}

/** `StartCommand`：Go 字段名（后端没有 json tag）。 */
export interface StartBranchValidationInput {
  IdempotencyKey: string;
  RepositoryID: string;
  TaskID?: string;
  CandidateSHA?: string;
  /** **业务数据基线库**的名字 —— 空库不算验证（后端会明确拒绝）。 */
  SourceDatabaseRef: string;
  Migrations: string[];
}

/** POST /api/projects/{pid}/branch-validations */
export function startBranchValidation(
  projectId: string,
  input: StartBranchValidationInput,
): Promise<BranchValidationRunView> {
  return apiRequest<BranchValidationRunView>(
    "POST",
    `/projects/${encodeURIComponent(projectId)}/branch-validations`,
    input,
  );
}

/** GET /api/projects/{pid}/branch-validations/{runId} */
export function getBranchValidation(projectId: string, runId: string): Promise<BranchValidationRunView> {
  return apiRequest<BranchValidationRunView>(
    "GET",
    `/projects/${encodeURIComponent(projectId)}/branch-validations/${encodeURIComponent(runId)}`,
  );
}

/** POST …/retry-cleanup —— 清理失败后的重试（C3：不留资源遗留）。 */
export function retryBranchCleanup(projectId: string, runId: string): Promise<BranchValidationRunView> {
  return apiRequest<BranchValidationRunView>(
    "POST",
    `/projects/${encodeURIComponent(projectId)}/branch-validations/${encodeURIComponent(runId)}/retry-cleanup`,
  );
}
