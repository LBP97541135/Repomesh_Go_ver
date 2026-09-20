import { useCallback, useEffect, useState } from "react";
import type { AgentRole, ConsoleAgentView } from "../api/contract";
import { createAgent, deleteAgent, listAgents, updateAgentProfile, type AgentRosterRow } from "../api/agents";
import {
  bindSkillVersion,
  listAgentSkillBindings,
  listSkills,
  listSkillVersions,
  unbindSkill,
  type SkillSummary,
  type SkillVersion,
} from "../api/skills";
import { fetchConsoleAgents } from "../api/grid";
import { resolveProjectId } from "../api/issues";
import { runtimeDisplay, shortId, type RuntimePhase } from "../display";
import { ErrorPanel, LoadingLine } from "../components/StatusBlocks";
import { useRuntimeRows } from "./useRuntimeRows";

/** 智能体花名册（CONS-44 / 契约 v0.2 §4.3）。
 *
 *  **与原型的三处有意偏离，都是诚实数据要求的**：
 *   1. 原型的状态列写「在岗 / 执行中 / 休眠」——**醒睡态没有数据源**。`awake` 恒 null
 *      （`DesiredRuntimeState` 是我们下发的期望态，不是观测态，拿它冒充即编造）。
 *      本页状态列 = `status` 的启用态（active|disabled，agent_directory 持久化）
 *      + `active_task_count` 派生的在途任务数，两者都是真事实；
 *   2. 原型的「时长」列（3h12m / 22m）**无源**：Controller 响应里没有任何时间字段，
 *      `uptime_seconds` 恒 null。列保留但整列显「未接入」，并在页脚写明补齐路径——
 *      删掉这列会让缺口消失于无形，填上假数字则更糟；
 *   3. 原型的运行时列直接写 `claude-code`；真实情况是 `runtime_kind` 只在探测通时
 *      才有值，不可达/未配置时没有——按 §4.4 三态呈现。确认由 Bridge 接入的成员
 *      （`kind: "external"`）连探通了也没有：平台核实的是「容器不归 Controller 管」，
 *      它跑的是哪个 CLI 平台不观测，所以这一格只写 External，不写任何 CLI 名字。 */

const ROLE_LABEL: Record<AgentRole, string> = {
  // 2026-09-20 用户更正命名：总领导叫 **Manager**，仓库领导叫 **Leader**。
  organization_leader: "Manager（总领导）",
  repository_leader: "Leader（仓库领导）",
  worker: "worker",
};

function AgentRow({
  agent,
  phase,
  onOpenIssue,
  onDelete,
  onManage,
}: {
  agent: ConsoleAgentView;
  phase: RuntimePhase;
  onOpenIssue: (issueId: string) => void;
  onDelete: (agentId: string) => void;
  onManage: (agentId: string) => void;
}) {
  const rt = agent.runtime;
  const d = runtimeDisplay(phase, rt);
  const disabled = agent.status === "disabled";
  // runtime_kind 只在探测通时可得；否则复用三态措辞，不留空也不编
  const runtimeKind = rt !== null && rt.reachable ? rt.runtime_kind : null;

  return (
    <tr className={`border-b border-panel ${disabled ? "text-tx2" : ""}`}>
      <td className="py-2 pr-3 align-top">
        {/* 列表只给人能读的东西：资源名 + 角色中文。原始 id 一律收进 title，
            不占版面（2026-09-19 用户意见：不要过多 id）。 */}
        <div className="text-[12.5px] text-tx" title={`agent ${agent.agent_id}`}>
          {agent.agentteams_resource_name}
        </div>
        <div className="text-[10.5px] text-tx2">{ROLE_LABEL[agent.role] ?? agent.role}</div>
      </td>

      <td className="py-2 pr-3 align-top">
        <span className="flex items-center gap-1.5 text-[11.5px]">
          <i className={`size-1.5 rounded-full ${disabled ? "bg-tx3" : "bg-olive"}`} />
          {disabled ? "已停用" : "已启用"}
        </span>
        {agent.active_task_count > 0 && (
          <div className="mt-px text-[10.5px] text-bluegray">{agent.active_task_count} 个在途任务</div>
        )}
      </td>

      <td className="py-2 pr-3 align-top text-[11.5px] text-tx2">
        <div>{agent.repository_name ?? (agent.role === "organization_leader" ? "组织级" : "未关联仓库")}</div>
        {/* 2026-09-19：这里原本读 `issue_id`，而后端把那列填的是**团队 id**
            （console 查询里 `t.id::text` 被选了两遍，已修）。归属的真身是
            `team_id`（拓扑反查；未驻扎任何团队时为 null）。 */}
        {agent.team_id ? (
          <div className="mt-px font-mono text-[10.5px] text-tx2" title={`团队 ${agent.team_id}`}>
            团队 #{shortId(agent.team_id)}
          </div>
        ) : agent.issue_id ? (
          <button
            className="mt-px font-mono text-[10.5px] text-tx2 hover:text-amber-hi"
            title={`issue ${agent.issue_id}`}
            onClick={() => onOpenIssue(agent.issue_id!)}
          >
            #{shortId(agent.issue_id)}
          </button>
        ) : (
          <div className="mt-px text-[10.5px] text-tx3">未驻扎团队</div>
        )}
      </td>

      <td className="py-2 pr-3 align-top">
        {/* external 不与降级三态同色：那三种是「没取到观测值」，它是**核实过的
            事实**，涂成同一档中性会让人以为运行时列又没读到东西 */}
        <div
          className={`font-mono text-[11.5px] ${d.kind === "external" ? "text-kraft" : "text-tx2"}`}
          title={d.hint}
        >
          {runtimeKind ?? d.label}
        </div>
        {runtimeKind && <div className="text-[10.5px] text-bluegray">{d.label}</div>}
      </td>

      {/* §4.4：uptime_seconds 恒 null，整列如实为「未接入」 */}
      <td className="py-2 text-right align-top text-[11.5px] text-tx3">未接入</td>
      {/* 2026-09-19 写面：删除。此前只加在「名册回退表」里，而那张表只在运行时
          探测失败时才渲染——正常运行走的是本表，所以删除入口看不见。 */}
      <td className="py-2 pl-3 text-right align-top">
        <button
          className="mr-2 rounded-hard border border-line-strong px-2 py-[2px] text-[11px] text-cream hover:bg-amber/10"
          onClick={() => onManage(agent.agent_id)}
        >
          管理
        </button>
        <button
          className="rounded-hard border border-line px-2 py-[2px] text-[11px] text-salmon hover:bg-salmon/10"
          onClick={() => onDelete(agent.agent_id)}
        >
          删除
        </button>
      </td>
    </tr>
  );
}

/** `embedded`：收进设置页「智能体」分类时为 true——去掉页面级宽度约束与大标题，
 *  分类标题由设置页提供（与 LocalCliPage 同一范式）。 */
export function AgentsPage({
  onOpenIssue,
  embedded = false,
}: {
  onOpenIssue: (issueId: string) => void;
  embedded?: boolean;
}) {
  // ── 写面（2026-09-19 新增）：新建 / 删除智能体 ──
  const [creating, setCreating] = useState(false);
  const [writeError, setWriteError] = useState<string | null>(null);
  const [busyWrite, setBusyWrite] = useState(false);
  const [form, setForm] = useState({ name: "", role: "worker", repositoryId: "" });
  // 配置草稿：按 agent id 存，未编辑时显示后端当前值（不预填假值）。
  const [drafts, setDrafts] = useState<Record<string, { prompt: string; cliKind: string }>>({});
  // 2026-09-19 用户意见：进入单个智能体的管理之前，不要一次展开全部可管理内容。
  const [managing, setManaging] = useState<string | null>(null);
  // 技能绑定（2026-09-19）：按 agent id 缓存当前生效的绑定 + 待绑定的版本 id。
  const [bindings, setBindings] = useState<Record<string, Array<Record<string, unknown>>>>({});
  const [bindDraft, setBindDraft] = useState<Record<string, { versionId: string; source: string }>>({});
  // 绑定用下拉（2026-09-19 用户意见：不要让人手填 uuid）：
  // 先选技能 → 再选它的版本。技能目录拉一次，版本按技能懒加载。
  const [skillOptions, setSkillOptions] = useState<SkillSummary[] | null>(null);
  const [skillPicked, setSkillPicked] = useState<Record<string, string>>({});
  const [versionOptions, setVersionOptions] = useState<Record<string, SkillVersion[]>>({});

  useEffect(() => {
    listSkills()
      .then(setSkillOptions)
      .catch(() => setSkillOptions([]));
  }, []);

  const pickSkill = async (agentId: string, skillId: string) => {
    setSkillPicked((prev) => ({ ...prev, [agentId]: skillId }));
    setBindDraft((prev) => ({ ...prev, [agentId]: { ...(prev[agentId] ?? { source: "approval_release" }), versionId: "" } }));
    if (skillId === "" || versionOptions[skillId]) return;
    try {
      const versions = await listSkillVersions(skillId);
      setVersionOptions((prev) => ({ ...prev, [skillId]: versions }));
    } catch {
      setVersionOptions((prev) => ({ ...prev, [skillId]: [] }));
    }
  };

  const loadBindings = async (agentId: string) => {
    try {
      const rows = await listAgentSkillBindings(agentId);
      setBindings((prev) => ({ ...prev, [agentId]: rows }));
    } catch {
      setBindings((prev) => ({ ...prev, [agentId]: [] }));
    }
  };

  const doBind = async (agentId: string) => {
    setWriteError(null);
    const draft = bindDraft[agentId] ?? { versionId: "", source: "approval_release" };
    if (draft.versionId.trim() === "") {
      setWriteError("请填写要绑定的技能版本 id（技能页可查到版本的 uuid）。");
      return;
    }
    try {
      await bindSkillVersion({
        agent_id: agentId,
        version_id: draft.versionId.trim(),
        source: draft.source,
      });
      setBindDraft({ ...bindDraft, [agentId]: { ...draft, versionId: "" } });
      await loadBindings(agentId);
    } catch (err) {
      setWriteError(err instanceof Error ? err.message : String(err));
    }
  };

  const doUnbind = async (agentId: string, bindingId: string) => {
    setWriteError(null);
    try {
      await unbindSkill(bindingId);
      await loadBindings(agentId);
    } catch (err) {
      setWriteError(err instanceof Error ? err.message : String(err));
    }
  };

  /** 保存智能体级配置（迁移 0035）。cliKind 空串 = 继承项目/部署默认。 */
  const saveProfile = async (agent: AgentRosterRow) => {
    setWriteError(null);
    const draft = drafts[agent.id] ?? { prompt: agent.prompt ?? "", cliKind: agent.cliKind ?? "" };
    try {
      await updateAgentProfile(agent.id, { prompt: draft.prompt, cliKind: draft.cliKind });
      setWriteError(null);
      retry();
    } catch (err) {
      setWriteError(err instanceof Error ? err.message : String(err));
    }
  };

  /** 新建：**项目作用域**（迁移 0053）。此前从名册第一行取 organizationId——
   *  那是"组织是编制作用域"的遗留，取到的是列表里任意一个人所属的账号空间，
   *  与用户正在看的项目无关，甚至可能不是他自己选的那个项目。现在取壳层
   *  **当前选定**的项目（`resolveProjectId()`）；没选就没有作用域，如实报错。 */
  const submitAgent = async () => {
    setWriteError(null);
    if (form.name.trim() === "") {
      setWriteError("请填写智能体名称。");
      return;
    }
    const projectId = await resolveProjectId();
    if (!projectId) {
      setWriteError("没有选定的项目——请先在「项目」页建立或选择项目，再新建智能体。");
      return;
    }
    setBusyWrite(true);
    try {
      await createAgent({
        projectId,
        role: form.role as "leader" | "manager" | "worker",
        repositoryId: form.repositoryId.trim(),
        name: form.name.trim(),
      });
      setCreating(false);
      setForm({ ...form, name: "" });
      retry();
    } catch (err) {
      setWriteError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusyWrite(false);
    }
  };

  const removeAgent = async (agent: AgentRosterRow) => {
    setWriteError(null);
    try {
      await deleteAgent(agent.id);
      retry();
    } catch (err) {
      setWriteError(err instanceof Error ? err.message : String(err));
    }
  };

  /** 运行时表的删除：那行是 ConsoleAgentView，主键叫 agent_id（名册行才叫 id）。 */
  const removeAgentById = async (agentId: string) => {
    setWriteError(null);
    try {
      await deleteAgent(agentId);
      retry();
    } catch (err) {
      setWriteError(err instanceof Error ? err.message : String(err));
    }
  };
  const fetcher = useCallback((withRuntime: boolean) => fetchConsoleAgents(withRuntime), []);
  const { rows, error, phase, retry } = useRuntimeRows<ConsoleAgentView>(fetcher);

  /* 智能体名册（GET /api/agents，agent_directory 池子）——真实数据，非夹具。
   *
   * 2026-09-19：名册**不再只是** `/console/agents` 404 时的回退——它现在是
   * 智能体配置（预设提示词 / CLI 工具，迁移 0035）的数据源，所以无条件拉取。
   * 旧守卫是 `if (!error || !error.includes("404") || roster !== null) return;`，
   * 那是 console 面还没落地时写的回退逻辑；console 面落地后该条件恒为真，
   * **名册永远不会被拉取**，配置区因此永远不渲染（实测踩到）。 */
  const [roster, setRoster] = useState<AgentRosterRow[] | null>(null);
  useEffect(() => {
    let cancelled = false;
    listAgents()
      .then((rosterRows) => {
        if (cancelled) return;
        setRoster(rosterRows);
      })
      .catch(() => {
        if (cancelled) return;
        setRoster([]);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const enabled = rows?.filter((a) => a.status === "active").length ?? 0;
  const busy = rows?.filter((a) => a.active_task_count > 0).length ?? 0;

  return (
    <div className={embedded ? "" : "max-w-[860px]"}>
      <div className={`${embedded ? "" : "border-b border-line pb-3 "}flex flex-wrap items-baseline gap-3`}>
        {!embedded && <h1 className="text-[16px] font-semibold text-cream">智能体</h1>}
        {rows && (
          <span className="text-[11.5px] text-tx2">
            {rows.length} 个 · {enabled} 个已启用 · {busy} 个有在途任务
          </span>
        )}
        <button
          className="ml-auto rounded-hard border border-line px-2.5 py-[3px] text-[11.5px] text-tx2 hover:border-amber hover:text-amber-hi"
          onClick={() => {
            setCreating((v) => !v);
            setWriteError(null);
          }}
        >
          {creating ? "取消" : "+ 新建智能体"}
        </button>
      </div>

      {/* 单个智能体的管理视图：点列表里的「管理」才进来。
          列表页不再一次摊开全部可管理内容，也不再到处露原始 id。 */}
      {managing !== null && (() => {
        const agent = (roster ?? []).find((row) => row.id === managing);
        if (!agent) return null;
        // 友好名：singletonKey 是 role:repo:name（迁移 0053 去掉了组织前缀），取最后一段当名字。
        const displayName = (agent.singletonKey ?? agent.id).split(":").pop() ?? agent.id;
        const draft = drafts[agent.id] ?? { prompt: agent.prompt ?? "", cliKind: agent.cliKind ?? "" };
        return (
          <section className="mt-4 rounded-hard border border-line bg-panel px-4 py-4">
            <div className="flex items-center gap-2.5">
              <button
                className="rounded-hard border border-line px-2 py-[3px] text-[11.5px] text-tx2 hover:text-cream"
                onClick={() => setManaging(null)}
              >
                ← 返回列表
              </button>
              <span className="text-[13.5px] font-semibold text-cream">{displayName}</span>
              <span className="rounded-full bg-amber/20 px-2 py-[1px] text-[10.5px] text-amber-hi">
                {/* 2026-09-20 更正：manager = 总领导（组织级）、leader = 仓库领导。
                    此前这里两个映射是**反的** —— manager 显示成「仓库 leader」、
                    leader 显示成「组织 leader」。 */}
                {agent.role === "manager" ? "Manager（总领导）" : agent.role === "leader" ? "Leader（仓库领导）" : agent.role}
              </span>
            </div>
            <p className="mt-2 text-[11.5px] text-tx2">
              预设提示词与 CLI 工具是<b>智能体级</b>配置；CLI 留空表示继承项目级与部署默认。
            </p>
            <div className="mt-3">
                  <textarea
                    className="mt-2 w-full rounded-hard border border-line bg-well px-2.5 py-[6px] text-[12px] text-tx"
                    rows={2}
                    placeholder="预设提示词（留空 = 不设）"
                    value={draft.prompt}
                    onChange={(e) =>
                      setDrafts({ ...drafts, [agent.id]: { ...draft, prompt: e.target.value } })
                    }
                  />
                  <div className="mt-2 flex items-center gap-2">
                    <select
                      className="rounded-hard border border-line bg-well px-2.5 py-[5px] font-mono text-[12px] text-tx"
                      value={draft.cliKind}
                      onChange={(e) =>
                        setDrafts({ ...drafts, [agent.id]: { ...draft, cliKind: e.target.value } })
                      }
                    >
                      <option value="">继承（项目 / 部署默认）</option>
                      <option value="codex_cli">codex_cli</option>
                      <option value="claude_cli">claude_cli</option>
                      <option value="dsh">dsh（AgentTeams 原生）</option>
                    </select>
                    <button
                      className="rounded-hard border border-line-strong px-3 py-[5px] text-[12px] text-cream hover:bg-amber/10"
                      onClick={() => void saveProfile(agent)}
                    >
                      保存
                    </button>
                  </div>

                  {/* 技能绑定：agent_skill_bindings + /api/skills/bindings。
                      version_id 是**技能版本** id（技能页可查），source 用后端枚举。 */}
                  <div className="mt-3 border-t border-line pt-2">
                    <div className="flex items-center gap-2 text-[11.5px] text-tx2">
                      <span>技能绑定</span>
                      <span className="text-tx3">
                        {(bindings[agent.id] ?? []).length} 条
                      </span>
                      <button
                        className="ml-auto rounded-hard border border-line px-2 py-[2px] text-[11px] text-tx2 hover:text-cream"
                        onClick={() => void loadBindings(agent.id)}
                      >
                        刷新
                      </button>
                    </div>
                    {(bindings[agent.id] ?? []).map((binding, index) => {
                      const bindingId = String(binding.id ?? binding.binding_id ?? "");
                      return (
                        <div key={bindingId || index} className="mt-1 flex items-center gap-2 text-[11px]">
                          <span className="min-w-0 flex-1 truncate font-mono text-tx3">
                            {String(binding.version_id ?? binding.skill_version_id ?? bindingId)}
                          </span>
                          <span className="flex-none text-tx3">{String(binding.source ?? "")}</span>
                          {bindingId !== "" && (
                            <button
                              className="flex-none rounded-hard border border-line px-2 py-[1px] text-salmon hover:bg-salmon/10"
                              onClick={() => void doUnbind(agent.id, bindingId)}
                            >
                              解绑
                            </button>
                          )}
                        </div>
                      );
                    })}
                    <div className="mt-2 flex items-center gap-2">
                      {/* 两级下拉：先选技能，再选它的版本——不再让人手填 uuid。 */}
                      <select
                        className="min-w-0 flex-1 rounded-hard border border-line bg-well px-2 py-[5px] text-[11.5px] text-tx"
                        value={skillPicked[agent.id] ?? ""}
                        onChange={(e) => void pickSkill(agent.id, e.target.value)}
                      >
                        <option value="">选择技能…</option>
                        {(skillOptions ?? []).map((skill) => (
                          <option key={skill.id} value={skill.id}>
                            {skill.name}（{skill.scenario}）
                          </option>
                        ))}
                      </select>
                      <select
                        className="flex-none rounded-hard border border-line bg-well px-2 py-[5px] font-mono text-[11.5px] text-tx"
                        value={(bindDraft[agent.id] ?? { versionId: "" }).versionId}
                        onChange={(e) =>
                          setBindDraft({
                            ...bindDraft,
                            [agent.id]: {
                              ...(bindDraft[agent.id] ?? { source: "approval_release" }),
                              versionId: e.target.value,
                            },
                          })
                        }
                      >
                        <option value="">选择版本…</option>
                        {(versionOptions[skillPicked[agent.id] ?? ""] ?? []).map((version) => (
                          <option key={version.id} value={version.id}>
                            {version.version} · {version.status}
                          </option>
                        ))}
                      </select>
                      <select
                        className="flex-none rounded-hard border border-line bg-well px-2 py-[5px] font-mono text-[11.5px] text-tx"
                        value={(bindDraft[agent.id] ?? { source: "approval_release" }).source}
                        onChange={(e) =>
                          setBindDraft({
                            ...bindDraft,
                            [agent.id]: {
                              ...(bindDraft[agent.id] ?? { versionId: "" }),
                              source: e.target.value,
                            },
                          })
                        }
                      >
                        <option value="approval_release">approval_release</option>
                        <option value="revision_auto">revision_auto</option>
                      </select>
                      <button
                        className="flex-none rounded-hard border border-line-strong px-3 py-[5px] text-[11.5px] text-cream hover:bg-amber/10"
                        onClick={() => void doBind(agent.id)}
                      >
                        绑定
                      </button>
                    </div>
                  </div>
            </div>
          </section>
        );
      })()}

      {creating && (
        <section className="mt-3 rounded-hard border border-line bg-panel px-4 py-4">
          <h2 className="text-[13px] font-semibold text-cream">新建智能体</h2>
          <p className="mt-1 text-[11.5px] text-tx2">
            这里是显式指名的一个成员；「仓库页 → 建团」才是按仓库自动编制一队人。同名重放不会建出第二个人。
          </p>
          <div className="mt-3 grid grid-cols-3 gap-3">
            <label className="text-[12px] text-tx2">
              名称
              <input
                className="mt-1 w-full rounded-hard border border-line bg-well px-2.5 py-[5px] font-mono text-[12px] text-tx"
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                placeholder="例如：wrk-extra-0"
              />
            </label>
            <label className="text-[12px] text-tx2">
              角色
              <select
                className="mt-1 w-full rounded-hard border border-line bg-well px-2.5 py-[5px] text-[12px] text-tx"
                value={form.role}
                onChange={(e) => setForm({ ...form, role: e.target.value })}
              >
                <option value="worker">worker</option>
                <option value="manager">manager</option>
                <option value="leader">leader</option>
              </select>
            </label>
            <label className="text-[12px] text-tx2">
              绑定仓库 id（可空）
              <input
                className="mt-1 w-full rounded-hard border border-line bg-well px-2.5 py-[5px] font-mono text-[12px] text-tx"
                value={form.repositoryId}
                onChange={(e) => setForm({ ...form, repositoryId: e.target.value })}
                placeholder="仓库页卡片里的 id"
              />
            </label>
          </div>
          {writeError && (
            <p className="mt-3 rounded-hard border border-salmon-deep bg-salmon-well px-3 py-2 text-[11.5px] text-salmon-hi">
              {writeError}
            </p>
          )}
          <button
            className="login-cta mt-3 rounded-hard px-3 py-[6px] text-[12px] font-extrabold tracking-[0.04em] disabled:opacity-60"
            disabled={busyWrite}
            onClick={() => void submitAgent()}
          >
            {busyWrite ? "正在创建…" : "创建"}
          </button>
        </section>
      )}

      {error && roster !== null ? (
        <>
          <p className="mb-3 text-[11.5px] text-tx3">
            运行时探测(/console/agents)服务端未接入,以下为智能体名册(agent_directory 池子,真实数据)。
          </p>
          <table className="w-full">
            <thead>
              <tr className="border-b border-line text-left">
                <th className="pb-1.5 text-[10.5px] uppercase tracking-[0.12em] text-tx2">智能体</th>
                <th className="pb-1.5 text-[10.5px] uppercase tracking-[0.12em] text-tx2">角色</th>
                <th className="pb-1.5 text-[10.5px] uppercase tracking-[0.12em] text-tx2">状态</th>
                <th className="pb-1.5 text-[10.5px] uppercase tracking-[0.12em] text-tx2">绑定仓库</th>
                <th className="pb-1.5 text-right text-[10.5px] uppercase tracking-[0.12em] text-tx2">操作</th>
              </tr>
            </thead>
            <tbody>
              {roster.map((a) => (
                <tr key={a.id} className="border-b border-[#f1f1ef]">
                  <td className="py-2 font-mono text-[11px] text-tx">{a.singletonKey ?? a.id.slice(0, 8)}</td>
                  <td className="py-2 text-tx2">{a.role}</td>
                  <td className="py-2 text-tx2">{a.status}</td>
                  <td className="py-2 text-tx2">{a.repositoryId ?? "—"}</td>
                  <td className="py-2 text-right">
                    <button
                      className="rounded-hard border border-line px-2 py-[2px] text-[11px] text-salmon hover:bg-salmon/10"
                      onClick={() => void removeAgent(a)}
                    >
                      删除
                    </button>
                  </td>
                </tr>
              ))}
              {roster.length === 0 && (
                <tr><td colSpan={5} className="py-6 text-center text-tx3">名册为空——物化/组装后出现第一批智能体。</td></tr>
              )}
            </tbody>
          </table>
        </>
      ) : error ? (
        <ErrorPanel title="花名册加载失败" message={error} onRetry={retry} />
      ) : rows === null ? (
        <LoadingLine />
      ) : rows.length === 0 ? (
        <div className="py-8 text-center text-[12.5px] text-tx3">agent_directory 里还没有注册的智能体</div>
      ) : (
        <table className="mt-3 w-full">
          <thead>
            <tr className="border-b border-line text-left">
              <th className="pb-1.5 text-[10.5px] font-normal tracking-[0.12em] text-tx2 uppercase">智能体</th>
              <th className="pb-1.5 text-[10.5px] font-normal tracking-[0.12em] text-tx2 uppercase">状态</th>
              <th className="pb-1.5 text-[10.5px] font-normal tracking-[0.12em] text-tx2 uppercase">归属</th>
              <th className="pb-1.5 text-[10.5px] font-normal tracking-[0.12em] text-tx2 uppercase">运行时</th>
              <th
                className="pb-1.5 text-right text-[10.5px] font-normal tracking-[0.12em] text-tx2 uppercase"
                title="uptime_seconds 恒 null：Controller 响应里没有任何时间字段，与运行时是否可达无关"
              >
                时长
              </th>
              <th className="pb-1.5 text-right text-[10.5px] font-normal tracking-[0.12em] text-tx2 uppercase">操作</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((agent) => (
              <AgentRow
                key={agent.agent_id}
                agent={agent}
                phase={phase}
                onOpenIssue={onOpenIssue}
                onDelete={(agentId) => void removeAgentById(agentId)}
                onManage={setManaging}
              />
            ))}
          </tbody>
        </table>
      )}

    </div>
  );
}
