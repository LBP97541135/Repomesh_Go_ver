/** 范围圈定域（Go：D 板块 scope 四条 + 决策链生产者接缝）。
 *
 *  ⚠ 提交圈定的 `requirement` 字段是历史决策的生产者（api-design.md §3.I
 *  生产者语义）：POST /api/scope 落一条 step=confirmation 决策单。前端必须
 *  把需求原文带上，丢了它决策链就没有数据。 */
import { apiRequest } from "./http";

export interface ScopeSuggestion {
  repositoryId: string;
  name: string;
  score: number;
  matchedTerms: string[];
  rationale: string;
}

export interface ScopeSuggestionsResponse {
  suggestions: ScopeSuggestion[];
  /** as-built：开关关闭时端点 503，detail 为明文。 */
}

/** POST /api/scope/suggestions — 辅助选仓建议（开关关 = 503，调用方回退手动）。 */
export function suggestScope(requirement: string, limit?: number): Promise<ScopeSuggestionsResponse> {
  return apiRequest<ScopeSuggestionsResponse>("POST", "/scope/suggestions", {
    requirement,
    ...(limit !== undefined ? { limit } : {}),
  });
}

export interface ScopeCheckResponse {
  /** 已知仓库 id；未知 id 以 detail/字段回显（形状待核 handler）。 */
  knownRepositoryIds: string[];
  unknownRepositoryIds: string[];
}

/** POST /api/scope/check — 提交前校验仓库名单。 */
export function checkScope(repositoryIds: string[]): Promise<ScopeCheckResponse> {
  return apiRequest<ScopeCheckResponse>("POST", "/scope/check", { repositoryIds });
}

export interface ScopeSubmitRequest {
  /** ⚠ 必带：历史决策的链根（需求原文快照）。 */
  requirement: string;
  repositoryIds: string[];
  /** 幂等键：同键重放返回原回执，不重复落决策单。 */
  idempotencyKey: string;
}

export interface ScopeSubmitResponse {
  confirmed: boolean;
  repositoryIds: string[];
  decidedAt: string;
}

/** POST /api/scope — 提交范围确认（写：会话 + Origin + CSRF）。
 *  后端把这次确认落成决策单（step=confirmation, actor=human）。 */
export function submitScope(req: ScopeSubmitRequest): Promise<ScopeSubmitResponse> {
  return apiRequest<ScopeSubmitResponse>("POST", "/scope", req);
}
