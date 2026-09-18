/** 智能体名册(Go:GET /api/agents,2026-09-17 落地,internal/assembly Roster)。
 *
 *  权限=成员:只返回会话所属组织的智能体池子。过滤参数见 §3.3
 *  (role=leader|manager|worker|db-test-team / repositoryId / status)。 */
import { apiRequest } from "./http";

export interface AgentRosterRow {
  id: string;
  organizationId: string;
  role: string;
  parentAgentId: string | null;
  repositoryId: string | null;
  responsibilityPaths: string[];
  resourceRef: Record<string, unknown>;
  singletonKey: string | null;
  status: string;
}

export function listAgents(filter?: {
  role?: string;
  repositoryId?: string;
  status?: string;
}): Promise<AgentRosterRow[]> {
  const params = new URLSearchParams();
  if (filter?.role) params.set("role", filter.role);
  if (filter?.repositoryId) params.set("repositoryId", filter.repositoryId);
  if (filter?.status) params.set("status", filter.status);
  const qs = params.toString();
  return apiRequest<AgentRosterRow[]>("GET", `/agents${qs ? `?${qs}` : ""}`);
}
