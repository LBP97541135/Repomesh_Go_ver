/** 会话消息域（Go：6.1 messages as-built —— 项目维度会话消息读写、提交回执、澄清读取）。
 *
 *  端点与形状唯一来源：`docs/current/api-design.md` §6.1 与附录 C as-built；
 *  json 字段为 Go json tag 原样（internal/messages）。提交要求**恰好一个**
 *  `Idempotency-Key` 头——后端直接把键当作 submissionId，同键重放返回 200 + 原回执。
 *  请求体只认 `body`（必填）与 `replyTo`（可选）两个键，未知键 422 UNKNOWN_FIELD。 */
import { apiRequest } from "./http";

const KEY_HEADER = (key: string) => ({ "Idempotency-Key": key });

/** POST messages 请求体（parseSubmitInput 白名单）。 */
export interface MessageSubmitInput {
  body: string;
  replyTo?: string;
}

/** 提交回执（SubmissionReceipt）：`GET .../message-submissions/{id}` 同形状。 */
export interface MessageSubmissionReceipt {
  submissionId: string;
  messageId: string;
  conversationId: string;
  sequence: number;
  submittedAt: string;
  replyTo?: string;
}

/** GET messages 列表行（messageRow）。 */
export interface ConversationMessage {
  id: string;
  sequence: number;
  authorKind: string;
  actorId: string;
  body: string;
  createdAt: string;
}

export interface ConversationMessagePage {
  items: ConversationMessage[];
  nextCursor: string | null;
}

/** 澄清读取（ClarificationView，internal/messages/clarify.go）。 */
export interface ClarificationView {
  id: string;
  requestId: string;
  state: string;
  revision: number;
  questionMessageId: string;
  answerMessageId: string | null;
  resolution: {
    resolutionId: string;
    inputKind: string;
    outcome: string;
    issueId: string | null;
    spans?: Array<{ messageId: string; start: number; end: number }>;
  } | null;
}

export interface PageQuery {
  cursor?: string;
  /** 1..100，后端缺省 50。 */
  limit?: number;
}

function pageQuery(query?: PageQuery): string {
  if (!query) return "";
  const params = new URLSearchParams();
  if (query.cursor) params.set("cursor", query.cursor);
  if (query.limit !== undefined) params.set("limit", String(query.limit));
  const qs = params.toString();
  return qs ? `?${qs}` : "";
}

/** POST /api/projects/{projectId}/conversations/{conversationId}/messages —
 *  201 首建 / 200 同键重放。 */
export function submitMessage(
  projectId: string,
  conversationId: string,
  input: MessageSubmitInput,
  idempotencyKey: string,
): Promise<MessageSubmissionReceipt> {
  return apiRequest<MessageSubmissionReceipt>(
    "POST",
    `/projects/${encodeURIComponent(projectId)}/conversations/${encodeURIComponent(conversationId)}/messages`,
    input,
    KEY_HEADER(idempotencyKey),
  );
}

/** GET /api/projects/{projectId}/conversations/{conversationId}/messages — 游标分页。 */
export function listConversationMessages(
  projectId: string,
  conversationId: string,
  query?: PageQuery,
): Promise<ConversationMessagePage> {
  return apiRequest<ConversationMessagePage>(
    "GET",
    `/projects/${encodeURIComponent(projectId)}/conversations/${encodeURIComponent(conversationId)}/messages${pageQuery(query)}`,
  );
}

/** GET /api/projects/{projectId}/message-submissions/{submissionId} — 提交回执读取。 */
export function getMessageSubmission(
  projectId: string,
  submissionId: string,
): Promise<MessageSubmissionReceipt> {
  return apiRequest<MessageSubmissionReceipt>(
    "GET",
    `/projects/${encodeURIComponent(projectId)}/message-submissions/${encodeURIComponent(submissionId)}`,
  );
}

/** GET /api/projects/{projectId}/logical-requests/{requestId}/clarification —
 *  澄清记录读取。 */
export function getClarification(
  projectId: string,
  requestId: string,
): Promise<ClarificationView> {
  return apiRequest<ClarificationView>(
    "GET",
    `/projects/${encodeURIComponent(projectId)}/logical-requests/${encodeURIComponent(requestId)}/clarification`,
  );
}
