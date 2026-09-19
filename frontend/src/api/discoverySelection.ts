/** 候选分流写路径（2026-09-18 用户裁定：第 2 步聊天室里人选 or AI 推断）。
 *
 *  人勾选 → supplement-check 按依赖图反查漏选 → supplements/confirm 人确认。
 *  与既有四个发现链触发端点同侪：幂等键随动作生成，回执不投影结果，
 *  写完一律重取发现链读投影。 */
import { apiRequest } from "./http";

export interface SelectionReceipt {
  step: number;
  status: string;
  repository_count?: number;
}

export interface SupplementCheckResult {
  supplement_state: "pending" | "none";
  supplements: Array<{ repository: string; repository_id?: string; via?: string; confidence?: number; mechanism?: string }>;
}

/** POST /api/issues/{id}/discovery/candidates/selection — 人勾选候选仓库。 */
export function selectCandidates(
  issueId: string,
  input: { created_by_agent_id: string; idempotency_key: string; repositoryIds: string[] },
): Promise<SelectionReceipt> {
  return apiRequest<SelectionReceipt>(
    "POST",
    `/issues/${encodeURIComponent(issueId)}/discovery/candidates/selection`,
    input,
  );
}

/** POST /api/issues/{id}/discovery/supplement-check — 依赖图查漏（人工勾选后）。 */
export function supplementCheck(
  issueId: string,
  input: { created_by_agent_id: string; idempotency_key: string },
): Promise<SupplementCheckResult> {
  return apiRequest<SupplementCheckResult>(
    "POST",
    `/issues/${encodeURIComponent(issueId)}/discovery/supplement-check`,
    input,
  );
}

/** POST /api/issues/{id}/discovery/supplements/confirm — 确认漏选清单。 */
export function confirmSupplements(
  issueId: string,
  input: { created_by_agent_id: string; idempotency_key: string; repositories: string[] },
): Promise<{ status: string; confirmed_count?: number }> {
  return apiRequest<{ status: string; confirmed_count?: number }>(
    "POST",
    `/issues/${encodeURIComponent(issueId)}/discovery/supplements/confirm`,
    input,
  );
}
