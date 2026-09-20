import { useEffect, useState } from "react";
import {
  advanceSkillVersion,
  createSnapshot,
  fetchSkillContent,
  listMcpPolicies,
  listSkillBindings,
  listSnapshots,
  listSkillVersions,
  listSkills,
  listEvalRuns,
  registerSkillVersion,
  type EvalRun,
  seedBindingsByRole,
  type SkillAction,
  type SkillStatus,
  type SkillSummary,
  type SkillVersion,
} from "../api/skills";
import { errText } from "../display";

/** 技能管理页（2026-09-19）。后端 internal/skills 的 21 个端点早已就绪，
 *  这里把最常用的三类接上：技能目录 → 版本生命周期 → 绑定 / MCP 策略。
 *
 *  生命周期按后端 allowedTransitions 走，**不自己发明下一步**：
 *    draft → evaluating → canary → { promoted | rolled_back }；promoted → rolled_back。
 *  越级的按钮根本不渲染（渲染了也只会拿到 409）。 */

const STATUS_LABEL: Record<SkillStatus, string> = {
  draft: "草稿",
  evaluating: "评估中",
  canary: "金丝雀",
  promoted: "已晋升",
  rolled_back: "已回滚",
};

/** 按功能板块分组 + 每个技能一句话介绍（2026-09-20 用户裁定）。
 *
 *  分组与文案是**前端展示层**的静态映射：数据源（internal/skills/presets.go 的
 *  15 个种子）没有"板块"这个字段，按交付流程归四类；未命中映射的（用户自己接入
 *  的技能）落「其它」，介绍退回 scenario。名字对不上时也只是少个分组，不会丢条目。 */
type SkillGroup = { heading: string; skills: string[] };

/** 用户可改的分组：默认 = 上面的功能板块；改过就存本机（localStorage）。
 *  ponytail: 只存本机、不跨账号共享；要共享/进库时把这份数据挪到后端一张表即可。 */
const GROUPS_KEY = "repomesh.skillGroups.v1";
const UNGROUPED = "未分组";

/** 分组可折叠（与仓库页同款交互）：记住收起状态，只记本机。 */
const COLLAPSED_KEY = "repomesh.skillGroups.collapsed.v1";

function readCollapsed(): string[] {
  try {
    const raw = window.localStorage.getItem(COLLAPSED_KEY);
    return raw ? (JSON.parse(raw) as string[]) : [];
  } catch {
    return [];
  }
}

function loadGroups(): SkillGroup[] {
  try {
    const raw = window.localStorage.getItem(GROUPS_KEY);
    const parsed = raw ? (JSON.parse(raw) as SkillGroup[]) : null;
    if (Array.isArray(parsed) && parsed.every((g) => g && typeof g.heading === "string" && Array.isArray(g.skills))) {
      return parsed;
    }
  } catch {
    /* 坏数据当没存过 */
  }
  return SKILL_GROUPS;
}

/** 默认分组：按交付流程归四类（数据源没有"板块"字段，见上方说明）。 */
const SKILL_GROUPS: SkillGroup[] = [
  {
    heading: "立项与规划 · Leader",
    skills: ["project-intake", "cross-repo-planning", "delivery-governance"],
  },
  {
    heading: "管理与把关 · Manager",
    skills: ["repository-spec-authoring", "task-decomposition", "code-review", "test-review", "worker-dispatch", "worker-result-evaluation"],
  },
  {
    heading: "执行与自验 · Worker",
    skills: ["task-execution", "self-test", "blocker-reporting"],
  },
  {
    heading: "进阶实践 · 可选",
    skills: ["tdd", "cross-repo-test", "integration-run"],
  },
];

const SKILL_INTRO: Record<string, string> = {
  "project-intake": "接下需求，明确目标与仓库范围，开出这个项目",
  "cross-repo-planning": "跨多个仓库排规划，定先后与依赖",
  "delivery-governance": "盯整条交付链的门禁与节奏，异常时叫停",
  "repository-spec-authoring": "给仓库写规格说明，让后续任务有据可依",
  "task-decomposition": "把规划拆成可执行的小任务",
  "code-review": "评审代码改动，把关质量与规范",
  "test-review": "评审测试用例与证据是否充分",
  "worker-dispatch": "把任务派给合适的执行者",
  "worker-result-evaluation": "验收执行结果，决定通过还是打回",
  "task-execution": "按任务说明改代码、提交产物",
  "self-test": "交付前先自测，别把问题甩给下游",
  "blocker-reporting": "卡住时及时上报原因与所需支援",
  tdd: "先写测试再写实现的开发方式",
  "cross-repo-test": "多仓改动一起联测",
  "integration-run": "把整条链路跑起来做集成验证",
};
const chip =
  "flex-none rounded-hard border border-line px-2.5 py-[3px] text-[11.5px] text-tx2 hover:border-amber hover:text-amber-hi disabled:opacity-50";
const primary =
  "flex-none rounded-hard bg-amber px-3 py-[6px] text-[12px] font-extrabold text-on-amber hover:bg-amber-hi disabled:opacity-60";
/** 状态一律走 .pill 族（原先每种状态各写一个圆角色块）。 */
const STATUS_PILL: Record<SkillStatus, string> = {
  draft: "pill pill-meta",
  evaluating: "pill pill-run",
  canary: "pill pill-gate",
  promoted: "pill pill-done",
  rolled_back: "pill pill-fail",
};

const NEXT_ACTIONS: Record<SkillStatus, Array<{ action: SkillAction; label: string }>> = {
  draft: [{ action: "evaluate", label: "送评估" }],
  evaluating: [{ action: "canary", label: "进金丝雀" }],
  canary: [
    { action: "promote", label: "晋升" },
    { action: "rollback", label: "回滚" },
  ],
  promoted: [{ action: "rollback", label: "回滚" }],
  rolled_back: [],
};

/** `embedded`：收进设置页「技能」分类时为 true——去掉页面级宽度、大内边距与外框，
 *  分类标题由设置页提供（与 LocalCliPage 同一范式）。 */
export function SkillsPage({ onToast, embedded = false }: { onToast: (text: string) => void; embedded?: boolean }) {
  const [skills, setSkills] = useState<SkillSummary[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [selected, setSelected] = useState<SkillSummary | null>(null);
  const [versions, setVersions] = useState<SkillVersion[] | null>(null);
  const [bindings, setBindings] = useState<Array<Record<string, unknown>>>([]);
  const [policies, setPolicies] = useState<Array<Record<string, unknown>>>([]);
  const [busy, setBusy] = useState(false);
  const [content, setContent] = useState<{ content: string; version: string; hash: string } | null>(null);
  const [showContent, setShowContent] = useState(false);
  const [snapshots, setSnapshots] = useState<Array<Record<string, unknown>>>([]);
  const [evalRuns, setEvalRuns] = useState<EvalRun[] | null>(null);
  /** 「登记新版本」表单（2026-09-20 补）。
   *
   *  为什么需要它：技能的生命周期是 draft → evaluating → canary → promoted，
   *  但界面上**只有"推进已有版本"的动作**（送评估/进金丝雀/晋升/回滚），
   *  没有"登记新版本"—— 于是线上 15 个技能全是 promoted、一个 draft 都没有，
   *  `evaluation_runs` 至今 0 行：**A/B 评估这条路在界面上根本走不到**。
   *  后端 `POST /api/skills/versions` 与前端 `registerSkillVersion` 早就有了，
   *  缺的就是这个入口。 */
  const [showNewVersion, setShowNewVersion] = useState(false);
  const [newVersion, setNewVersion] = useState("");
  const [newContent, setNewContent] = useState("");

  useEffect(() => {
    listSkills()
      .then(setSkills)
      .catch((err: unknown) => {
        setSkills([]);
        setError(errText(err));
      });
    listSkillBindings().then(setBindings).catch(() => undefined);
    listMcpPolicies().then(setPolicies).catch(() => undefined);
    listSnapshots().then(setSnapshots).catch(() => undefined);
  }, []);

  useEffect(() => {
    if (!selected) {
      setVersions(null);
      return;
    }
    setVersions(null);
    listSkillVersions(selected.id)
      .then(setVersions)
      .catch(() => setVersions([]));
    setContent(null);
    fetchSkillContent(selected.name)
      .then(setContent)
      .catch(() => setContent(null));
  }, [selected]);

  // 每次选中一个版本时，加载该版本的评估历史。
  const loadEvalRuns = async (versionId: string) => {
    setEvalRuns(null);
    try {
      setEvalRuns(await listEvalRuns(versionId));
    } catch {
      setEvalRuns([]);
    }
  };

  /** 登记一个新版本（状态为 draft）—— 之后才走得到"送评估 → A/B 评估"。 */
  const submitNewVersion = async () => {
    if (!selected || newVersion.trim() === "" || newContent.trim() === "") return;
    setBusy(true);
    try {
      await registerSkillVersion({
        skill_id: selected.id,
        version: newVersion.trim(),
        content: newContent,
      });
      onToast(`已登记版本 ${newVersion.trim()}（draft）—— 现在可以"送评估"了`);
      setShowNewVersion(false);
      setNewVersion("");
      setNewContent("");
      // 立刻重取版本列表：新 draft 要出现在列表里，否则人以为没生效。
      setVersions(await listSkillVersions(selected.id));
    } catch (err) {
      onToast(`登记失败：${errText(err)}`);
    } finally {
      setBusy(false);
    }
  };

  const doSeedBindings = async () => {
    setBusy(true);
    try {
      const r = await seedBindingsByRole();
      onToast(`已创建 ${r.seeded} 条种子绑定`);
      setBindings(await listSkillBindings());
    } catch (err) {
      onToast(`创建绑定失败：${errText(err)}`);
    } finally {
      setBusy(false);
    }
  };

  const doCreateSnapshot = async () => {
    setBusy(true);
    try {
      await createSnapshot();
      onToast("快照已创建");
      setSnapshots(await listSnapshots());
    } catch (err) {
      onToast(`快照创建失败：${errText(err)}`);
    } finally {
      setBusy(false);
    }
  };

  const act = async (version: SkillVersion, action: SkillAction, label: string) => {
    setBusy(true);
    try {
      await advanceSkillVersion(version.id, action);
      onToast(`${version.version} 已${label}`);
      if (selected) setVersions(await listSkillVersions(selected.id));
    } catch (err) {
      onToast(`${label}失败：${errText(err)}`);
    } finally {
      setBusy(false);
    }
  };

  // 左列分组：种子按功能板块，其余（自己接入的）落「其它」。
  // 分组可增删改（2026-09-20）：默认=功能板块，改过存本机。
  const [groups, setGroups] = useState<SkillGroup[]>(loadGroups);
  const [editing, setEditing] = useState(false);
  const [newHeading, setNewHeading] = useState("");
  const [collapsed, setCollapsed] = useState<Set<string>>(() => new Set(readCollapsed()));
  const toggle = (heading: string) =>
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(heading)) next.delete(heading);
      else next.add(heading);
      try {
        window.localStorage.setItem(COLLAPSED_KEY, JSON.stringify([...next]));
      } catch {
        /* 隐私模式等存不上：只丢记忆，不影响本次 */
      }
      return next;
    });
  const persist = (next: SkillGroup[]) => {
    setGroups(next);
    try {
      window.localStorage.setItem(GROUPS_KEY, JSON.stringify(next));
    } catch {
      /* 存不上就只影响本页本次 */
    }
  };
  const moveTo = (name: string, heading: string) =>
    persist(
      groups.map((g) => ({
        heading: g.heading,
        skills: heading === g.heading ? [...g.skills.filter((n) => n !== name), name] : g.skills.filter((n) => n !== name),
      })),
    );

  // 左列分组：按用户分组渲染，没分进去的落「未分组」，谁都不会丢。
  const groupedSkills = groups.map((g) => ({
    heading: g.heading,
    items: (skills ?? []).filter((s) => g.skills.includes(s.name)),
  }));
  const otherSkills = (skills ?? []).filter((s) => !groups.some((g) => g.skills.includes(s.name)));
  if (otherSkills.length > 0 || editing) groupedSkills.push({ heading: UNGROUPED, items: otherSkills });

  return (
    <div className={embedded ? "" : "max-w-[860px]"}>
      {/* 2026-09-20 用户裁定：标题行只留标题——"N 个"与生命周期说明这行字去掉。 */}
      {!embedded && (
        <div className="mb-3 flex items-baseline border-b border-line pb-2.5">
          <h1 className="text-[16px] font-semibold text-cream">技能</h1>
        </div>
      )}

      {error && (
        <p className="mb-4 rounded-hard border border-salmon/40 bg-salmon-well px-3 py-2 text-[11.5px] text-salmon">
          技能目录加载失败：{error}
        </p>
      )}

      <div className="grid grid-cols-[minmax(220px,280px)_1fr] gap-5">
        {/* 左：技能目录 */}
        {/* 2026-09-20 用户裁定：去掉最外面的大框——分组头自带分隔线，不需要再套一层盒子。 */}
        <div>
          {skills === null && <p className="px-3 py-3 text-[12px] text-tx3">正在读取…</p>}
          {skills !== null && skills.length === 0 && (
            <p className="px-3 py-3 text-[12px] text-tx3">还没有技能。</p>
          )}
          {/* 分组管理：查（默认视图）/ 增删改（管理态），改完即存本机。 */}
          <div className="flex items-center gap-1.5 border-b border-line px-3 py-2">
            <button className={chip} onClick={() => setEditing((v) => !v)}>
              {editing ? "完成" : "管理分组"}
            </button>
            {editing && (
              <>
                <button
                  className={chip}
                  onClick={() => {
                    persist(SKILL_GROUPS);
                    setNewHeading("");
                  }}
                >
                  恢复默认
                </button>
                <input
                  className="min-w-0 flex-1 rounded-hard border border-line bg-ink px-2 py-[3px] text-[11.5px] text-tx placeholder:text-tx3 focus:border-amber focus:outline-none"
                  placeholder="新分组名"
                  value={newHeading}
                  onChange={(e) => setNewHeading(e.target.value)}
                />
                <button
                  className={chip}
                  disabled={!newHeading.trim()}
                  onClick={() => {
                    persist([...groups, { heading: newHeading.trim(), skills: [] }]);
                    setNewHeading("");
                  }}
                >
                  + 建组
                </button>
              </>
            )}
          </div>
          {groupedSkills.map((group, index) => (
            <div key={`${group.heading}-${index}`}>
              {editing && group.heading !== UNGROUPED ? (
                <div className="microlabel flex w-full items-center gap-2 px-3 py-1.5">
                  <input
                    className="min-w-0 flex-1 rounded-hard border border-line bg-ink px-1.5 py-[2px] text-[11px] text-tx focus:border-amber focus:outline-none"
                    value={group.heading}
                    onChange={(e) =>
                      persist(groups.map((g) => (g.heading === group.heading ? { ...g, heading: e.target.value } : g)))
                    }
                  />
                  <button
                    className="text-[11px] text-salmon hover:text-salmon-hi"
                    title="删掉这个分组（里面的技能会回到「未分组」，不会被删）"
                    onClick={() => persist(groups.filter((g) => g.heading !== group.heading))}
                  >
                    删组
                  </button>
                </div>
              ) : (
                /* 仓库页同款折叠头：箭头 + 名字，仅此而已（用户裁定：不加数字）。 */
                <button
                  className="flex w-full items-center gap-2 border-b border-line px-3 py-1.5 text-left hover:text-tx"
                  onClick={() => toggle(group.heading)}
                  aria-expanded={!collapsed.has(group.heading)}
                >
                  <span className="text-[10px] text-tx3">{collapsed.has(group.heading) ? "▶" : "▼"}</span>
                  <span className="text-[12.5px] font-semibold text-cream">{group.heading}</span>
                </button>
              )}
              {(!collapsed.has(group.heading) || editing) && group.items.map((skill) => {
                const active = selected?.id === skill.id;
                return (
                  <button
                    key={skill.id}
                    className={`block w-full border-b border-line px-3 py-2 text-left hover:bg-well ${
                      active ? "bg-amber/10" : ""
                    }`}
                    onClick={() => setSelected(skill)}
                  >
                    <span className="flex items-center gap-2">
                      <span className="min-w-0 flex-1 truncate font-mono text-[12px] text-tx">{skill.name}</span>
                      {/* 系统自带（`created_by=system-seed`，库里 organization_id 为空 = 全局
                          种子技能）：必须标出来。2026-09-20 用户实测把它当成了"别人的技能"，
                          根子就是界面上分不清"系统自带的"和"某人接入的"。 */}
                      {skill.created_by === "system-seed" && <span className="pill pill-meta flex-none">内置</span>}
                    </span>
                    {/* 一句话介绍；自己接入的没有文案，退回场景与角色。 */}
                    <span className="mt-[2px] block truncate text-[11px] text-tx2">
                      {SKILL_INTRO[skill.name] ?? `${skill.scenario} · ${skill.target_agent_role}`}
                    </span>
                    {editing && (
                      <select
                        className="mt-1 rounded-hard border border-line bg-ink px-1.5 py-[2px] text-[11px] text-tx2 focus:border-amber focus:outline-none"
                        value={group.heading}
                        onChange={(e) => moveTo(skill.name, e.target.value === UNGROUPED ? "" : e.target.value)}
                      >
                        {group.heading === UNGROUPED && <option>{UNGROUPED}</option>}
                        {groups.map((g) => (
                          <option key={g.heading}>{g.heading}</option>
                        ))}
                      </select>
                    )}
                  </button>
                );
              })}
            </div>
          ))}
        </div>

        {/* 右：选中技能的版本与动作 */}
        <div className="min-w-0">
          {selected === null ? (
            <div className="rounded-hard border border-line bg-panel px-4 py-6 text-[12.5px] text-tx2">
              选左侧一个技能，查看它的版本与可推进的动作。
            </div>
          ) : (
            <>
              <div className="mb-3 rounded-hard border border-line bg-panel px-4 py-3">
                <div className="font-mono text-[13px] text-cream">{selected.name}</div>
                <div className="mt-0.5 text-[11.5px] text-tx2">
                  {selected.scenario} · 目标角色 {selected.target_agent_role} ·{" "}
                  {selected.created_by === "system-seed"
                    ? "系统内置（对所有账号可见）"
                    : `由 ${selected.created_by} 建立`}
                </div>
                <div className="mt-2 flex flex-wrap gap-2">
                  <button className={chip} onClick={() => setShowContent(!showContent)}>
                    {showContent ? "收起原文" : "查看原文"}
                  </button>
                  {/* 登记新版本：A/B 评估的唯一入口（没有 draft 就永远评不了）。 */}
                  <button
                    className={chip}
                    onClick={() => {
                      setShowNewVersion(!showNewVersion);
                      if (!showNewVersion) {
                        setNewContent(content?.content ?? "");
                        setNewVersion("");
                      }
                    }}
                  >
                    {showNewVersion ? "取消登记" : "登记新版本"}
                  </button>
                </div>
                {showNewVersion && (
                  <div className="mt-2 flex flex-col gap-1.5 rounded-hard border border-line bg-well px-3 py-2">
                    <p className="text-[11px] leading-[1.7] text-tx2">
                      登记出来的版本状态是 <span className="font-mono">draft</span>，
                      之后才能「送评估 → 进金丝雀 → 晋升」。正文默认带出当前版本，
                      改过再提交才有 A/B 可比性。
                    </p>
                    <input
                      className="rounded-hard border border-line bg-panel px-2 py-1 font-mono text-[12px] text-tx"
                      placeholder="版本号，如 1.1.0"
                      value={newVersion}
                      onChange={(e) => setNewVersion(e.target.value)}
                    />
                    <textarea
                      className="min-h-[120px] rounded-hard border border-line bg-panel px-2 py-1 font-mono text-[11px] text-tx"
                      placeholder="技能正文（SKILL.md 内容）"
                      value={newContent}
                      onChange={(e) => setNewContent(e.target.value)}
                    />
                    <button
                      className={`self-start ${primary}`}
                      disabled={busy || newVersion.trim() === "" || newContent.trim() === ""}
                      onClick={() => void submitNewVersion()}
                    >
                      {busy ? "提交中…" : "登记为 draft"}
                    </button>
                  </div>
                )}
                {showContent && content && (
                  <div className="mt-2 rounded-hard border border-line bg-well px-3 py-2">
                    <div className="text-[10.5px] text-tx3">
                      版本 {content.version} · {content.hash.slice(0, 20)}…
                    </div>
                    <pre className="mt-1 max-h-[300px] overflow-auto whitespace-pre-wrap font-mono text-[11px] text-tx">
                      {content.content}
                    </pre>
                  </div>
                )}
                {showContent && !content && (
                  <p className="mt-2 text-[11.5px] text-tx3">没有可用的 promoted/canary 版本。</p>
                )}
              </div>

              {versions === null && <p className="text-[12.5px] text-tx3">正在读取版本…</p>}
              {versions !== null && versions.length === 0 && (
                <p className="text-[12.5px] text-tx3">这个技能还没有版本。</p>
              )}
              <div className="space-y-2">
                {(versions ?? []).map((version) => (
                  <div
                    key={version.id}
                    title={`版本 ${version.id}`}
                    className="flex items-center justify-between gap-4 rounded-hard border border-line bg-panel px-4 py-3"
                  >
                    <div className="min-w-0">
                      <div className="flex items-center gap-2">
                        <span className="font-mono text-[12.5px] text-tx">{version.version}</span>
                        <span className={STATUS_PILL[version.status] ?? "pill pill-meta"}>
                          {STATUS_LABEL[version.status] ?? version.status}
                        </span>
                      </div>
                      {/* 2026-09-19 用户意见：不要过多 id。版本 uuid 收进 title，
                          列表只留版本号与状态（要看 id 悬停即可）。 */}
                    </div>
                    <button className={chip} onClick={() => void loadEvalRuns(version.id)}>
                      评估历史
                    </button>
                    <div className="flex flex-none gap-2">
                      {(NEXT_ACTIONS[version.status] ?? []).map((item) => (
                        <button
                          key={item.action}
                          className={primary}
                          disabled={busy}
                          onClick={() => void act(version, item.action, item.label)}
                        >
                          {item.label}
                        </button>
                      ))}
                      {(NEXT_ACTIONS[version.status] ?? []).length === 0 && (
                        <span className="text-[11.5px] text-tx3">已到终态，无后续动作</span>
                      )}
                    </div>
                  </div>
                ))}
              </div>

              {/* A/B 评估历史（点了"评估历史"按钮后展示） */}
              {evalRuns !== null && (
                <div className="mt-3 rounded-hard border border-line bg-panel px-4 py-3">
                  <div className="text-[12px] font-medium text-cream">A/B 评估历史</div>
                  {evalRuns.length === 0 ? (
                    <p className="mt-1 text-[11.5px] text-tx3">这个版本还没有评估记录。</p>
                  ) : (
                    <div className="mt-2 space-y-1">
                      {evalRuns.map((run) => (
                        <div key={run.id} className="flex items-center gap-3 text-[11.5px]">
                          <span className={run.result === "pass" ? "pill pill-done" : "pill pill-fail"}>
                            {run.result === "pass" ? "PASS" : "FAIL"}
                          </span>
                          <span className="text-tx2">{run.arm === "with" ? "有技能" : "无技能"}</span>
                          <span className="font-mono text-tx3">{run.blinded_label}</span>
                          <span className="ml-auto text-tx3">{new Date(run.run_at).toLocaleString()}</span>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              )}
            </>
          )}

          {/* 绑定与 MCP 策略：技能治理的另外两面，先如实显示条数 */}
          <div className="mt-5 grid grid-cols-2 gap-3">
            <div className="rounded-hard border border-line bg-panel px-4 py-3">
              <div className="text-[12.5px] text-cream">技能绑定</div>
              <div className="mt-1 text-[11.5px] text-tx2">{bindings.length} 条</div>
              <button
                className={`mt-2 ${chip}`}
                disabled={busy}
                onClick={() => void doSeedBindings()}
              >
                按角色批量建绑定
              </button>
            </div>
            <div className="rounded-hard border border-line bg-panel px-4 py-3">
              <div className="text-[12.5px] text-cream">MCP 调用策略</div>
              <div className="mt-1 text-[11.5px] text-tx2">{policies.length} 条</div>
            </div>
          </div>

          {/* 技能快照 */}
          <div className="mt-5 rounded-hard border border-line bg-panel px-4 py-3">
            <div className="flex items-center justify-between">
              <div className="text-[12.5px] text-cream">技能快照</div>
              <button
                className={chip}
                disabled={busy}
                onClick={() => void doCreateSnapshot()}
              >
                创建快照
              </button>
            </div>
            <div className="mt-1 text-[11.5px] text-tx2">{snapshots.length} 个（运行中的任务保持启动时的版本集）</div>
            {snapshots.length > 0 && (
              <div className="mt-2 space-y-1">
                {snapshots.slice(0, 5).map((snap, i) => {
                  const s = snap as { id?: string; superseded_at?: string; versions?: Array<{ skill_id?: string; version?: string }> };
                  return (
                    <div key={s.id ?? i} className="text-[11px] text-tx2">
                      <span className="font-mono">{s.id?.slice(0, 8)}…</span>
                      {" · "}
                      {s.versions?.length ?? 0} 个版本
                      {" · "}
                      {s.superseded_at ? "已取代" : "活跃"}
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
