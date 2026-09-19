/** Issue 域（Go：B06 创建 + B07 读面）——**全部挂在项目维度下**。
 *
 *  ⚠ 调用前提：页面必须先有 `projectId`（Go 的 issue 不挂组织，挂项目；
 *  api-design.md 附录 E 第 4 条）。壳层目前只有 organizationId——项目选择
 *  进壳之前，这些函数只供就绪页面调用。
 *
 *  幂等键走 `Idempotency-Key` **请求头**（恰好一个，400 否则）；
 *  字段契约唯一来源：docs/current/issue-page-create-api-contract.md（§3 请求、
 *  §4 创建响应、§7 详情与房间）。 */
import { allPages } from "./pagination";
import { apiRequest } from "./http";

/** 创建请求体（契约 §3,2026-09-17 后端 parseNewInput as-built）:
 *  expectedCreationContextRevision/title/description/repositoryIds 必填,
 *  acceptanceCriteria/repositoryAnalysisId/conversation 可选;未知字段 422。 */
export interface IssueCreationInput {
  expectedCreationContextRevision?: string;
  title?: string;
  description?: string;
  repositoryIds?: string[];
  acceptanceCriteria?: string[];
  repositoryAnalysisId?: string;
  conversation?: { mode: "new" } | { mode: "existing"; id: string };
  [key: string]: unknown;
}

/** 创建响应（后端 writeIssueCreation 信封，as-built）。 */
export interface IssueCreationReceipt {
  creationId: string;
  status: "committed";
  projectId: string;
  createdAt: string;
  source: { kind: string };
  issue: { id: string; number: unknown };
  mainChangeSet: { id: string };
  conversation: { id: string };
}

const KEY_HEADER = (key: string) => ({ "Idempotency-Key": key });

/** POST /api/projects/{projectId}/issue-creations — 201 首建 / 200 同键重放。 */
export function createIssueCreation(
  projectId: string,
  input: IssueCreationInput,
  idempotencyKey: string,
): Promise<IssueCreationReceipt> {
  return apiRequest<IssueCreationReceipt>(
    "POST",
    `/projects/${encodeURIComponent(projectId)}/issue-creations`,
    input,
    KEY_HEADER(idempotencyKey),
  );
}

/** GET /api/projects/{projectId}/issue-creations/{creationId} — 原操作回执查询。 */
export function getIssueCreation(projectId: string, creationId: string): Promise<IssueCreationReceipt> {
  return apiRequest<IssueCreationReceipt>(
    "GET",
    `/projects/${encodeURIComponent(projectId)}/issue-creations/${encodeURIComponent(creationId)}`,
  );
}

export interface PageQuery {
  cursor?: string;
  /** 1..100，缺省 50。 */
  limit?: number;
}

function pageQuery(query: PageQuery): string {
  const params = new URLSearchParams();
  if (query.cursor) params.set("cursor", query.cursor);
  if (query.limit !== undefined) params.set("limit", String(query.limit));
  const qs = params.toString();
  return qs ? `?${qs}` : "";
}

/** GET /api/projects/{projectId}/issue-creation-options — 创建条件（谁能建、缺什么）。 */
export function creationOptions(projectId: string, query?: PageQuery): Promise<CreationOptions> {
  return apiRequest<CreationOptions>(
    "GET",
    `/projects/${encodeURIComponent(projectId)}/issue-creation-options${pageQuery(query ?? {})}`,
  );
}

/** GET /api/projects/{projectId}/issue-conversations — 会话列表。 */
export function issueConversations(projectId: string, query?: PageQuery): Promise<unknown> {
  return apiRequest<unknown>(
    "GET",
    `/projects/${encodeURIComponent(projectId)}/issue-conversations${pageQuery(query ?? {})}`,
  );
}

/** GET /api/projects/{projectId}/issues — 项目维度 issue 列表。 */
export function listProjectIssues(projectId: string, query?: PageQuery): Promise<unknown> {
  return apiRequest<unknown>(
    "GET",
    `/projects/${encodeURIComponent(projectId)}/issues${pageQuery(query ?? {})}`,
  );
}

/** GET /api/issues/{issueId} — issue 详情（§7；字段以契约为准）。 */
export function getIssue(issueId: string): Promise<unknown> {
  return apiRequest<unknown>("GET", `/issues/${encodeURIComponent(issueId)}`);
}

/** GET /api/issues/{issueId}/rooms — 房间列表（§7；消息流本身未实现）。 */
export function issueRooms(issueId: string): Promise<unknown> {
  return apiRequest<unknown>("GET", `/issues/${encodeURIComponent(issueId)}/rooms`);
}

export interface CreationRepository {
  repositoryId: string;
  displayName: string;
  selectable: boolean;
  reasons: string[];
}
export interface CreationOptions {
  projectId: string;
  creationContextRevision: string;
  canSubmit: boolean;
  blockingReasons: string[] | null;
  repositories: CreationRepository[];
  nextCursor: string | null;
}
export async function allCreationOptions(projectId: string): Promise<CreationOptions> {
  let first: CreationOptions | undefined;
  const repositories = await allPages(async (cursor) => {
    const page = await creationOptions(projectId, { cursor, limit: 100 });
    if (first && first.creationContextRevision !== page.creationContextRevision) {
      throw new Error("项目创建条件已变化，请刷新后重新选择。");
    }
    first ??= page;
    return { items: page.repositories, nextCursor: page.nextCursor };
  });
  return { ...first!, repositories, nextCursor: null };
}
