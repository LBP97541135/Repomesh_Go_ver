/** issue 列表数据源：live | replay，开关沿用 `resolveDataSourceMode()`
 *  （URL `?source=live|replay` > `VITE_DATA_SOURCE` > 默认 replay）。
 *
 *  live 打契约 v0.2 §2 的 `GET /issues`；replay 走本地夹具。两侧返回**同一个契约类型**，
 *  页面无分支。 */
import type { IssueListResponse, ParsedDocumentView } from "./contract";
import { defaultClient } from "./client";
import { createIssueCreation } from "./projectIssues";
import { readActiveProject } from "./activeProject";
import { apiRequest } from "./http";

/** 需求正文里「用户手打的话」与「附件文档解析全文」的分界（U+2063 不可见分隔符）。
 *  契约里文档解析文本只能随 requirement_text 交给规划，但不进聊天气泡——
 *  用户没打字就一个字都不替他展示。
 *
 *  2026-09-20 从 WorkbenchPage 移到 API 层：它描述的是**需求文本这个线上字段的
 *  形状**，读的一方（开场消息、建项标题）和写的一方（composeRequirementText）
 *  都该拿到同一份定义，不然就是各猜各的。 */
export const DOC_SENTINEL = "\u2063";

export function composeRequirementText(typed: string, documentText: string): string {
  return typed ? `${typed}\n\n${DOC_SENTINEL}\n${documentText}` : `${DOC_SENTINEL}\n${documentText}`;
}

/** 把需求正文拆回两半。没有分隔符 = 全是手打的话（老数据、纯打字提交都走这条）。 */
export function splitRequirement(raw: string): { typed: string; document: string } {
  const at = raw.indexOf(DOC_SENTINEL);
  if (at < 0) return { typed: raw.trim(), document: "" };
  return {
    typed: raw.slice(0, at).replace(/\u2063/g, "").trim(),
    document: raw.slice(at + DOC_SENTINEL.length).replace(/\u2063/g, "").trim(),
  };
}

/** 文档正文里第一条 markdown 标题（去掉 `#`）。取不到就 null——不编一个标题。 */
export function documentTitleOf(document: string): string | null {
  const heading = document
    .split("\n")
    .map((l) => l.trim())
    .find((l) => /^#{1,6}\s+\S/.test(l));
  const value = heading?.replace(/^#{1,6}\s+/, "").trim();
  return value ? value.slice(0, 200) : null;
}

/** 建项标题。
 *
 *  2026-09-20 修：此前直接取「第一个非空行」，而附件场景下需求正文是
 *  `\u2063\n{doc}`——第一行只有一个不可见分隔符，且 **U+2063 不是空白字符，
 *  `trim()` 去不掉它**，于是整条 issue 的标题变成一个看不见的字符，列表里看着
 *  就是空标题（线上两条新 issue 的 title 实测就是 `\u2063`）。
 *  现在按「文档自己的标题 → 用户手打的第一行 → 文档第一行 → 兜底词」取，
 *  并显式剔除分隔符。 */
function firstLine(text: string): string {
  const { typed, document } = splitRequirement(text);
  const candidates = [
    documentTitleOf(document),
    typed.split("\n").find((l) => l.trim().length > 0),
    document.split("\n").find((l) => l.trim().length > 0),
  ];
  for (const candidate of candidates) {
    const value = (candidate ?? "").replace(/\u2063/g, "").trim();
    if (value.length > 0) return value.slice(0, 200);
  }
  return "需求";
}
import { resolveDataSourceMode, type DataSourceMode } from "./source";
import { issuesFixture } from "../data/issues";

/** 单页条数。联调种子仅四条，取 20 足够；真实规模下由 next_cursor 续读。 */
export const ISSUES_PAGE_LIMIT = 20;

/** Compatibility readers may use only the shell-validated explicit selection. */
export async function resolveProjectId(): Promise<string | null> {
  return readActiveProject();
}

export interface IssuesQuery {
  state: "open" | "closed";
  /** 当前明确选择的项目。 */
  projectId: string;
  /** Q2：工作区由前端持有并传参，服务端不猜。未选工作区时不传 = 全部。 */
  organizationId?: string;
  cursor?: string;
  /** v0.5：默认视图（与两个计数）排除已归档 issue；开关打开时置 true。 */
  includeArchived?: boolean;
}

function replayPage(q: IssuesQuery): IssueListResponse {
  const all = issuesFixture.issues;
  return {
    issues: all.filter((i) => i.state === q.state),
    open_count: all.filter((i) => i.state === "open").length,
    closed_count: all.filter((i) => i.state === "closed").length,
    // 夹具即全量，没有第二页——不给一个点了没反应的「加载更多」
    next_cursor: null,
  };
}

export function issuesSourceMode(): DataSourceMode {
  return resolveDataSourceMode();
}

export interface CreateIssueRequest {
  projectId: string;
  requirementText: string;
  /** 2026-09-20 起建项不选仓：缺省不发送（解析层仍收老客户端的值但忽略其内容，
   *  指纹算法不变）。留作可选只是让旧调用方/兼容路径不必改签名。 */
  repositoryIds?: string[];
  expectedCreationContextRevision: string;
  /** 人审门模式：ai = 自动托管，hitl = 门等真人（缺省 hitl，最保守）。 */
  hitlMode?: "ai" | "hitl";
}

/** All scope decisions belong to the caller; retries send this exact snapshot. */
export async function createIssue(input: CreateIssueRequest, idempotencyKey: string): Promise<CreatedIssueRef> {
  // 2026-09-20（用户："需求不写仓库为什么就不行？"）：仓库范围**不再必填**。
  // 需求里没点名仓库时，候选评分那一步会退到本项目的全部仓库目录，由 Manager
  // （总领导）自己去发现该改哪些仓。仍然必须有项目与创建上下文版本。
  if (!input.projectId || !input.expectedCreationContextRevision) {
    throw new Error("请先选择项目。");
  }
  const receipt = await createIssueCreation(input.projectId, {
    expectedCreationContextRevision: input.expectedCreationContextRevision,
    conversation: { mode: "new" },
    // 建项不选仓（2026-09-20）：没点名仓库就**不发这个键**——范围交给①之后的
    // 「选仓门」确认（submitScopeSelection）。老调用方传了仍照发（值后端只留痕）。
    ...(input.repositoryIds && input.repositoryIds.length > 0
      ? { repositoryIds: [...input.repositoryIds].sort() }
      : {}),
    title: firstLine(input.requirementText),
    description: input.requirementText,
    hitlMode: input.hitlMode ?? "hitl",
  }, idempotencyKey);
  return { issue_id: receipt.issue.id };
}

/** 选仓门批量确认（plan Task A3 契约，前端先行、后端并行实现）：
 *  `POST /api/projects/{pid}/issues/{iid}/scope/selection`。
 *
 *  门上的两条路——「我自己勾」（decidedBy=manual）与「让 AI 定」（decidedBy=ai）
 *  ——都走这一条：一次事务把确认的集合写成 issue 的仓库范围（repository_scope
 *  与 content_scope 双表 + 同一把 scope_revision）并关门。422 空/越界（1..100）、
 *  409 revision 不匹配或仓不在项目内，detail 原样上抛由调用方呈现。 */
export interface ScopeSelectionInput {
  /** 1..100、非空、去重（这里发出去前会再排一次序）。 */
  repositoryIds: string[];
  decidedBy: "manual" | "ai" | "timeout";
  idempotencyKey: string;
  expectedCreationContextRevision: string;
}

export interface ScopeSelectionReceipt {
  /** committed=范围已落地;replayed=同幂等键重放;ai_requested=「让 AI 定」已受理但
   *  建议还没生成(不写范围、不关门,后端随后生成并自动采纳)——见 spec 2026-09-20 修订。 */
  status: "committed" | "replayed" | "ai_requested";
  repositoryCount: number;
}

export function submitScopeSelection(
  projectId: string,
  issueId: string,
  input: ScopeSelectionInput,
): Promise<ScopeSelectionReceipt> {
  return apiRequest<ScopeSelectionReceipt>(
    "POST",
    `/projects/${encodeURIComponent(projectId)}/issues/${encodeURIComponent(issueId)}/scope/selection`,
    input,
  );
}

/** createIssue 的返回契约：只承诺 id（其余读模型字段由列表刷新提供）。 */
export interface CreatedIssueRef {
  issue_id: string;
}

export async function fetchIssues(q: IssuesQuery): Promise<IssueListResponse> {
  if (issuesSourceMode() === "replay") return replayPage(q);

  const projectId = q.projectId;
  if (!projectId) {
    // 没有任何可读项目：诚实空态（组织/项目批次未接时也走这里）
    return { issues: [], open_count: 0, closed_count: 0, next_cursor: null };
  }
  return defaultClient().listIssues({
    projectId,
    state: q.state,
    organizationId: q.organizationId,
    cursor: q.cursor,
    limit: ISSUES_PAGE_LIMIT,
    includeArchived: q.includeArchived,
  });
}

/** v0.5 §1：归档 issue（墓碑语义，不删除；幂等，重复归档返回同一 archived_at）。
 *  replay 夹具不可篡改（createIssue 同一条红线），调用方在页面层挡掉。 */
export async function archiveIssue(issueId: string): Promise<void> {
  await defaultClient().archiveIssue(issueId);
}

export interface IssuePurgeReceipt {
  snapshots: number;
  decision_chain_nodes: number;
  audit_events: number;
}

/** 彻底清除（2026-09-08 用户裁决）：**不可逆**——硬删除已归档 issue 的快照、
 *  决策链与审计事件（保留一条 IssuePurged 审计）。仅对已归档 issue 可用
 *  （后端 409 兜底）；replay 模式由调用方在页面层挡掉。 */
export async function purgeIssue(issueId: string): Promise<IssuePurgeReceipt> {
  return defaultClient().purgeIssue(issueId);
}

/** 需求文档真实上传（与 createIssue 同源鉴权）：把 .txt/.md/.docx/.pdf/.odt/.rtf
 *  解析成纯文本，由弹窗填入需求区继续编辑。回放模式同样可用——解析只读后端，
 *  不写任何数据，不篡改夹具世界。 */
export async function parseRequirementDocument(file: File): Promise<ParsedDocumentView> {
  return defaultClient().parseIssueDocument(file);
}
