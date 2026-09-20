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

/** 2026-09-20 统一装修：按钮与状态标签全部换成全站既有写法（仓库页/审核台同款），
 *  不再各画一套描边。 */
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
        <div className="rounded-hard border border-line bg-panel">
          {skills === null && <p className="px-3 py-3 text-[12px] text-tx3">正在读取…</p>}
          {skills !== null && skills.length === 0 && (
            <p className="px-3 py-3 text-[12px] text-tx3">还没有技能。</p>
          )}
          {(skills ?? []).map((skill) => {
            const active = selected?.id === skill.id;
            return (
              <button
                key={skill.id}
                className={`block w-full border-b border-line px-3 py-2 text-left last:border-b-0 ${
                  active ? "bg-amber/10" : "hover:bg-well"
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
                <span className="mt-[2px] block truncate text-[11px] text-tx2">
                  {skill.scenario} · {skill.target_agent_role}
                </span>
              </button>
            );
          })}
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
                <button className={`mt-2 ${chip}`} onClick={() => setShowContent(!showContent)}>
                  {showContent ? "收起原文" : "查看原文"}
                </button>
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
