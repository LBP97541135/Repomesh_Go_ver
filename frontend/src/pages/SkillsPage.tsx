import { useEffect, useState } from "react";
import {
  advanceSkillVersion,
  listMcpPolicies,
  listSkillBindings,
  listSkillVersions,
  listSkills,
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

  useEffect(() => {
    listSkills()
      .then(setSkills)
      .catch((err: unknown) => {
        setSkills([]);
        setError(errText(err));
      });
    listSkillBindings().then(setBindings).catch(() => undefined);
    listMcpPolicies().then(setPolicies).catch(() => undefined);
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
  }, [selected]);

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
    <div className={embedded ? "" : "mx-auto max-w-[1040px] px-8 py-10"}>
      <div className={`${embedded ? "mb-3 border-b border-line pb-2.5 " : "mb-6 "}flex flex-wrap items-baseline gap-3`}>
        {!embedded && <h1 className="text-[16px] font-semibold text-cream">技能</h1>}
        {skills && <span className="text-[11.5px] text-tx2">{skills.length} 个</span>}
        <span className="text-[11.5px] text-tx3">
          生命周期：草稿 → 评估中 → 金丝雀 → 晋升 / 回滚
        </span>
      </div>

      {error && (
        <p className="mb-4 rounded-hard border border-salmon-deep bg-salmon-well px-3 py-2 text-[12px] text-salmon-hi">
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
                <span className="block truncate font-mono text-[12px] text-tx">{skill.name}</span>
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
                  {selected.scenario} · 目标角色 {selected.target_agent_role} · 由 {selected.created_by} 建立
                </div>
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
                        <span className="rounded-full bg-amber/20 px-2 py-[1px] text-[10.5px] text-amber-hi">
                          {STATUS_LABEL[version.status] ?? version.status}
                        </span>
                      </div>
                      {/* 2026-09-19 用户意见：不要过多 id。版本 uuid 收进 title，
                          列表只留版本号与状态（要看 id 悬停即可）。 */}
                    </div>
                    <div className="flex flex-none gap-2">
                      {(NEXT_ACTIONS[version.status] ?? []).map((item) => (
                        <button
                          key={item.action}
                          className="rounded-hard border border-line-strong px-3 py-[5px] text-[12px] text-cream hover:bg-amber/10 disabled:opacity-50"
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
            </>
          )}

          {/* 绑定与 MCP 策略：技能治理的另外两面，先如实显示条数 */}
          <div className="mt-5 grid grid-cols-2 gap-3">
            <div className="rounded-hard border border-line bg-panel px-4 py-3">
              <div className="text-[12.5px] text-cream">技能绑定</div>
              <div className="mt-1 text-[11.5px] text-tx2">{bindings.length} 条</div>
            </div>
            <div className="rounded-hard border border-line bg-panel px-4 py-3">
              <div className="text-[12.5px] text-cream">MCP 调用策略</div>
              <div className="mt-1 text-[11.5px] text-tx2">{policies.length} 条</div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
