/** 智能体名册(Go:GET /api/agents,2026-09-17 落地,internal/assembly Roster)。
 *
 *  权限=成员:只返回会话所属组织的智能体池子。过滤参数见 §3.3
 *  (role=leader|manager|worker|db-test-team / repositoryId / status)。 */
import { apiRequest } from "./http";

export interface AgentRosterRow {
  id: string;
  organizationId: string;
  /** 编制的业务归属（迁移 0053）。为 null = 组织级角色（治理 leader、规划 agent），
   *  它们不参与自动编制。**新建智能体要用它**——组织不再决定人建在哪里。 */
  projectId: string | null;
  role: string;
  parentAgentId: string | null;
  repositoryId: string | null;
  responsibilityPaths: string[];
  resourceRef: Record<string, unknown>;
  singletonKey: string | null;
  status: string;
  prompt: string;
  cliKind: string;
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

/** POST /api/agents —— 手动新建一个智能体（2026-09-19 新增）。
 *
 *  与「仓库页建团」的区别：建团是按仓库自动编制一队人（1 leader + 1 manager + N worker）；
 *  这里是**显式指名**的一个成员，不参与自动编制。后端仍写 singleton_key
 *  （role:repo:name），同名重放不会建出第二个人。
 *
 *  **作用域是项目，不是组织**（迁移 0053）：service 用 projectId 反查组织戳写进
 *  organization_id，所以这里不再传 organizationId。传一个调用方自己猜的组织 id
 *  已被后端移除——业务链是 账号 → 项目 → issue → 仓库，组织只回答「这是哪个账号的数据」。
 *
 *  写路由要求：会话 + CSRF（apiRequest 会带 X-CSRF-Token）+ Origin 严格相等
 *  （浏览器对同源 POST 会自动带 Origin）。 */
export function createAgent(input: {
  projectId: string;
  role: "leader" | "manager" | "worker";
  repositoryId: string;
  name: string;
}): Promise<{ id: string }> {
  return apiRequest<{ id: string }>("POST", "/agents", input);
}

/** DELETE /api/agents/{id} —— 删除一个智能体（硬删，花名册与归属直接少一行）。
 *  后端显式把 DELETE 也当写操作校验（Origin + CSRF），不会绕过。 */
export function deleteAgent(agentId: string): Promise<void> {
  return apiRequest<void>("DELETE", `/agents/${encodeURIComponent(agentId)}`);
}

/** PATCH /api/agents/{id} —— 设置智能体的预设提示词与 CLI 工具（迁移 0035，A 方案）。
 *
 *  **智能体级**配置：cliKind 传空串 = 未指定，运行时沿用项目级
 *  `repomesh_projects.agent_settings.agent_kind` 与部署默认——三层优先级
 *  智能体 > 项目 > 部署。提示词上限 20000 字符（后端校验）。 */
export function updateAgentProfile(
  agentId: string,
  input: { prompt: string; cliKind: string },
): Promise<void> {
  return apiRequest<void>("PATCH", `/agents/${encodeURIComponent(agentId)}`, input);
}
