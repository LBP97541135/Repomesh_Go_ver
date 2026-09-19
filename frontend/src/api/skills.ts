/** 技能治理域（Go: internal/skills/http.go，21 个端点）。
 *
 *  领域词汇唯一来源 internal/skills/contracts.go：
 *   · 五态生命周期 draft → evaluating → canary → promoted → rolled_back
 *     （allowedTransitions 是硬约束，越级会被后端 409 skill_transition_refused 拒）
 *   · AB 盲评双臂 with / without，结果 pass / fail
 *   · 分级审批：worker 的 skill 由 manager 审、manager 的由 leader 审、leader 的由人工审
 *   · 角色统一叫法：leader（总）/ manager（仓库）/ worker
 *
 *  形状按 as-built 收敛，读面同时兼容 {items}/{skills}/裸数组三种包装先例。 */
import { apiRequest } from "./http";

export interface SkillSummary {
  id: string;
  name: string;
  scenario: string;
  target_agent_role: "leader" | "manager" | "worker";
  created_by: string;
  created_at: string;
}

export type SkillStatus = "draft" | "evaluating" | "canary" | "promoted" | "rolled_back";

export interface SkillVersion {
  id: string;
  skill_id: string;
  version: string;
  status: SkillStatus;
  content?: string;
  created_by?: string;
  created_at?: string;
  [key: string]: unknown;
}

/** 兼容三种包装先例，收不到就如实给空数组。 */
function unwrap<T>(raw: unknown, keys: string[]): T[] {
  if (Array.isArray(raw)) return raw as T[];
  const wrapper = raw as Record<string, unknown> | null;
  for (const key of keys) {
    const value = wrapper?.[key];
    if (Array.isArray(value)) return value as T[];
  }
  return [];
}

/** GET /api/skills —— 技能目录（2026-09-19 新增；此前只能按名字查版本，无法枚举）。 */
export async function listSkills(): Promise<SkillSummary[]> {
  return unwrap<SkillSummary>(await apiRequest<unknown>("GET", "/skills"), ["skills", "items"]);
}

/** GET /api/skills/versions?skill_id= —— 某技能的全部版本（含状态）。 */
export async function listSkillVersions(skillId: string): Promise<SkillVersion[]> {
  return unwrap<SkillVersion>(
    await apiRequest<unknown>("GET", `/skills/versions?skill_id=${encodeURIComponent(skillId)}`),
    ["versions", "items"],
  );
}

export async function listSkillBindings(): Promise<Array<Record<string, unknown>>> {
  return unwrap<Record<string, unknown>>(
    await apiRequest<unknown>("GET", "/skills/bindings"),
    ["bindings", "items"],
  );
}

export async function listMcpPolicies(): Promise<Array<Record<string, unknown>>> {
  return unwrap<Record<string, unknown>>(
    await apiRequest<unknown>("GET", "/skills/mcp-policies"),
    ["policies", "items"],
  );
}

/** 生命周期推进。后端按 allowedTransitions 校验，越级 409 原样上抛。 */
export type SkillAction = "evaluate" | "canary" | "promote" | "rollback" | "release";

export function advanceSkillVersion(
  versionId: string,
  action: SkillAction,
  body?: unknown,
): Promise<unknown> {
  return apiRequest(
    "POST",
    `/skills/versions/${encodeURIComponent(versionId)}/${action}`,
    body ?? {},
  );
}

/** POST /api/skills —— 注册新技能（name + scenario + target_agent_role 必填）。 */
export function registerSkill(input: {
  name: string;
  scenario: string;
  target_agent_role: "leader" | "manager" | "worker";
}): Promise<SkillSummary> {
  return apiRequest<SkillSummary>("POST", "/skills", input);
}

/** POST /api/skills/versions —— 给技能登记一个新版本。 */
export function registerSkillVersion(input: {
  skill_id: string;
  version: string;
  content: string;
}): Promise<SkillVersion> {
  return apiRequest<SkillVersion>("POST", "/skills/versions", input);
}

/** GET /api/skills/bindings?agent_id= —— 某智能体当前生效的技能绑定。
 *  后端要求 agent_id 必填（缺则 400）。 */
export async function listAgentSkillBindings(
  agentId: string,
): Promise<Array<Record<string, unknown>>> {
  return unwrap<Record<string, unknown>>(
    await apiRequest<unknown>("GET", `/skills/bindings?agent_id=${encodeURIComponent(agentId)}`),
    ["bindings", "items"],
  );
}

/** POST /api/skills/bindings —— 把一个**技能版本**绑到某个智能体上。
 *  三字段全必填（后端校验）；version_id 是技能版本 id（不是技能 id），
 *  source 用 contracts.go 的枚举：approval_release | revision_auto。 */
export function bindSkillVersion(input: {
  agent_id: string;
  version_id: string;
  source: string;
}): Promise<unknown> {
  return apiRequest("POST", "/skills/bindings", input);
}

/** DELETE /api/skills/bindings/{id} —— 解绑。 */
export function unbindSkill(bindingId: string): Promise<void> {
  return apiRequest<void>("DELETE", `/skills/bindings/${encodeURIComponent(bindingId)}`);
}
