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

/** GET /api/skills/versions/{id}/evaluations —— 某版本的 A/B 评估历史。 */
export interface EvalRun {
  id: string;
  version_id: string;
  question_id: string;
  arm: "with" | "without";
  blinded_label: string;
  answer: Record<string, unknown>;
  judged_by: string | null;
  result: "pass" | "fail";
  run_at: string;
}

/** POST /api/skills/versions/{id}/ab-evaluation —— **跑一轮完整的 A/B 评估**。
 *
 *  与相邻两条的分工：`/evaluate` 只做状态迁移（draft→evaluating，即"送评估"）；
 *  `/evaluations` 记录**单条**结果（给外部评判方用）。此前只有这两条，
 *  于是"送评估"之后版本永远停在 evaluating —— 线上 `skill_evaluation_runs` 0 行。
 *  这条是**本仓自带的执行器**：把该技能全部测试题跑完、两臂都记上并返回结论。 */
export interface ABQuestionResult {
  question_id: string;
  question: string;
  with_label: string;
  without_label: string;
  /** 两臂各自的答案原文（已反盲归位）。 */
  with_answer: string;
  without_answer: string;
  with_score: number;
  without_score: number;
  with_result: string;
  without_result: string;
  /** 反盲后的胜方臂：with | without | tie。 */
  winner: string;
  /** 裁判给的一句话理由。 */
  rationale: string;
}

export interface ABEvaluationSummary {
  version_id: string;
  skill_id: string;
  /** 判定者身份。真盲评形如 `blind_llm_judge:tokendance.space:deepseek-v4.1-flash`；
   *  历史遗留的 `local_coverage_check` 是旧的本地覆盖度检查，**不是**盲评。 */
  judge: string;
  questions: ABQuestionResult[];
  with_pass: number;
  without_pass: number;
  /** 两臂的平均分（10 分制）与带技能臂的优势分差。 */
  with_mean_score: number;
  without_mean_score: number;
  win_margin: number;
  /** win / lose / tie —— 按事实给，不硬凑"通过"。 */
  verdict: string;
}

export function runABEvaluation(versionId: string): Promise<ABEvaluationSummary> {
  return apiRequest<ABEvaluationSummary>(
    "POST",
    `/skills/versions/${encodeURIComponent(versionId)}/ab-evaluation`,
  );
}

export async function listEvalRuns(versionId: string): Promise<EvalRun[]> {
  return unwrap<EvalRun>(
    await apiRequest<unknown>("GET", `/skills/versions/${encodeURIComponent(versionId)}/evaluations`),
    ["runs", "items"],
  );
}

/** GET /api/skills/content/{skill} —— 技能原文（当前 promoted/canary 版本的 SKILL.md）。
 *  返回纯文本；响应头 X-Skill-Version / X-Skill-Hash 携带版本与内容摘要。 */
export async function fetchSkillContent(skillName: string): Promise<{ content: string; version: string; hash: string }> {
  const res = await fetch(`/api/skills/content/${encodeURIComponent(skillName)}`);
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  return {
    content: await res.text(),
    version: res.headers.get("X-Skill-Version") ?? "",
    hash: res.headers.get("X-Skill-Hash") ?? "",
  };
}

/** POST /api/skills/bindings/seed-by-role —— 按角色批量建种子绑定。 */
export function seedBindingsByRole(): Promise<{ seeded: number }> {
  return apiRequest<{ seeded: number }>("POST", "/skills/bindings/seed-by-role", {});
}

/** POST /api/skills/snapshots —— 创建组织级技能快照。 */
export function createSnapshot(): Promise<Record<string, unknown>> {
  return apiRequest("POST", "/skills/snapshots", {});
}

/** GET /api/skills/snapshots —— 列出全部快照。 */
export async function listSnapshots(): Promise<Array<Record<string, unknown>>> {
  return unwrap<Record<string, unknown>>(
    await apiRequest<unknown>("GET", "/skills/snapshots"),
    ["snapshots", "items"],
  );
}
