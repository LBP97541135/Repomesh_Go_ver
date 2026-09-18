import { useMemo, useState } from "react";
import { ChevronDown } from "lucide-react";
import { fetchAgentTeamsWorkflow, type AtTask } from "../api/agentteams";
import { errText } from "../display";
/** AgentTeams 执行进度面板(验收定稿的原型形态:白底极简、单行节点、
 *  点击浮层看详情)。数据源 = 后端适配器只读透传的上游 workflow,
 *  组件不做任何状态派生 —— 上游状态值域原样上色,未知状态按待安排灰渲染。
 *
 *  Controller 未配置(后端 503)或 project id 未填时,面板保持折叠不空转;
 *  project id / team 记忆在 localStorage,换页不丢。 */

const STATUS_COLOR: Record<string, string> = {
  completed: "#16a34a",
  "in-progress": "#d97706",
  submitted: "#d97706",
  assigned: "#9ca3af",
  planned: "#9ca3af",
  cancelled: "#9ca3af",
};
const colorOf = (status: string) => STATUS_COLOR[status] ?? "#9ca3af";

const ICON_CHECK = '<svg width="8" height="8" viewBox="0 0 16 16" fill="none"><path d="M3 8.5 6.5 12 13 4.5" stroke="#16a34a" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"/></svg>';
const ICON_RUN = '<svg width="8" height="8" viewBox="0 0 16 16" fill="#d97706"><circle cx="8" cy="8" r="3.2"/></svg>';
const ICON_DIM = '<svg width="8" height="8" viewBox="0 0 16 16" fill="none"><circle cx="8" cy="8" r="5.2" stroke="#9ca3af" stroke-width="2"/></svg>';
const iconOf = (status: string) =>
  status === "completed" ? ICON_CHECK : status === "in-progress" || status === "submitted" ? ICON_RUN : ICON_DIM;

export function AgentTeamsDagPanel({ issueId }: { issueId: string }) {
  const [open, setOpen] = useState(false);
  const [projectId, setProjectId] = useState(() => localStorage.getItem("at.project") ?? "");
  const [team, setTeam] = useState(() => localStorage.getItem("at.team") ?? "");
  const [tasks, setTasks] = useState<AtTask[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [selected, setSelected] = useState<string | null>(null);

  const load = async () => {
    if (!projectId.trim()) {
      setError("先填 AgentTeams 的 project id。");
      return;
    }
    setBusy(true);
    setError(null);
    localStorage.setItem("at.project", projectId.trim());
    localStorage.setItem("at.team", team.trim());
    try {
      setTasks(await fetchAgentTeamsWorkflow(projectId.trim(), team.trim() || undefined));
    } catch (err: unknown) {
      setTasks(null);
      setError(errText(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="mt-2 rounded-hard border border-line bg-panel">
      <button
        onClick={() => setOpen(!open)}
        className="flex w-full items-center gap-2 px-3 py-2 text-left text-[11.5px] text-tx2 hover:text-cream"
      >
        <ChevronDown size={12} className={open ? "rotate-180" : ""} />
        AgentTeams 执行进度({issueId.slice(0, 8)}…)
        <span className="ml-auto text-[10px] text-tx3">数据源:上游 workflow API 只读透传</span>
      </button>
      {open && (
        <div className="relative border-t border-line px-4 py-3">
          <div className="flex flex-wrap items-center gap-2 text-[12px]">
            <span className="text-tx3">project id</span>
            <input
              value={projectId}
              onChange={(e) => setProjectId(e.target.value)}
              className="w-56 rounded-md border border-line bg-base px-2 py-1 text-cream"
              placeholder="pricing-discount-20260916"
            />
            <span className="text-tx3">team(可选)</span>
            <input
              value={team}
              onChange={(e) => setTeam(e.target.value)}
              className="w-40 rounded-md border border-line bg-base px-2 py-1 text-cream"
            />
            <button onClick={load} disabled={busy} className="rounded-md border border-line px-3 py-1 text-cream disabled:opacity-50">
              {busy ? "加载中…" : "加载"}
            </button>
          </div>
          {error && <p className="mt-2 text-[12px] text-salmon-hi">{error}</p>}
          {tasks && <DagGraph tasks={tasks} selected={selected} onSelect={setSelected} />}
        </div>
      )}
    </div>
  );
}

function DagGraph({ tasks, selected, onSelect }: {
  tasks: AtTask[];
  selected: string | null;
  onSelect: (id: string | null) => void;
}) {
  const layout = useMemo(() => {
    const byId = new Map(tasks.map(t => [t.id, t]));
    const cache = new Map<string, number>();
    const layerOf = (t: AtTask): number => {
      const hit = cache.get(t.id);
      if (hit !== undefined) return hit;
      const value = t.deps.length ? Math.max(...t.deps.map(d => {
        const dep = byId.get(d);
        return dep ? layerOf(dep) + 1 : 0;
      })) : 0;
      cache.set(t.id, value);
      return value;
    };
    tasks.forEach(layerOf);
    const layers: AtTask[][] = [];
    tasks.forEach(t => (layers[layerOf(t)] ??= []).push(t));
    for (let i = 0; i < layers.length; i++) layers[i] ??= [];
    // 重心排序消交叉(前后扫四轮)
    const idx = new Map<string, { l: number; r: number }>();
    layers.forEach((col, l) => col.forEach((t, r) => idx.set(t.id, { l, r })));
    const bary = (t: AtTask, adj: number) => {
      const ids = t.deps.concat(tasks.filter(x => x.deps.includes(t.id)).map(x => x.id))
        .filter(id => idx.get(id)?.l === adj)
        .map(id => idx.get(id)!.r);
      return ids.length ? ids.reduce((a, b) => a + b, 0) / ids.length : idx.get(t.id)!.r;
    };
    for (let pass = 0; pass < 4; pass++) {
      for (let i = 1; i < layers.length; i++) {
        layers[i].forEach(t => idx.set(t.id, { l: i, r: layers[i].indexOf(t) }));
        layers[i].sort((a, b) => bary(a, i - 1) - bary(b, i - 1));
      }
      for (let i = layers.length - 2; i >= 0; i--) {
        layers[i].forEach(t => idx.set(t.id, { l: i, r: layers[i].indexOf(t) }));
        layers[i].sort((a, b) => bary(a, i + 1) - bary(b, i + 1));
      }
    }
    const NW = 232, NH = 48, GX = 76, GY = 18, PAD = 28;
    const width = PAD * 2 + layers.length * (NW + GX) - GX;
    const height = PAD * 2 + Math.max(...layers.map(l => l.length)) * (NH + GY) - GY;
    const pos = new Map<string, { x: number; y: number }>();
    layers.forEach((col, i) => col.forEach((t, j) => pos.set(t.id, { x: PAD + i * (NW + GX), y: PAD + j * (NH + GY) })));
    const edges: Array<{ from: string; to: string }> = [];
    tasks.forEach(t => t.deps.forEach(d => { if (byId.has(d)) edges.push({ from: d, to: t.id }); }));
    return { layers, pos, width, height, edges, isDone: (id: string) => byId.get(id)?.status === "completed" };
  }, [tasks]);

  const NW = 232, NH = 48;

  return (
    <div className="mt-3 rounded-hard border border-line bg-white p-4">
      <svg viewBox={`0 0 ${layout.width} ${layout.height}`} style={{ width: "100%", height: "auto" }}>
        {layout.edges.map(e => {
          const a = layout.pos.get(e.from)!, b = layout.pos.get(e.to)!;
          const x1 = a.x + NW, y1 = a.y + NH / 2, x2 = b.x, y2 = b.y + NH / 2;
          const mx = x1 + Math.abs(x2 - x1) * 0.55;
          const dim = selected && selected !== e.from && selected !== e.to;
          return <path key={`${e.from}-${e.to}`} d={`M${x1},${y1} C${mx},${y1} ${mx},${y2} ${x2},${y2}`} fill="none"
            stroke={selected === e.to ? "#d97706" : "#c4cddb"} strokeWidth={selected === e.to ? 2 : 1.5}
            opacity={dim ? 0.25 : 1} markerEnd="url(#at-arrow)" />;
        })}
        <defs>
          <marker id="at-arrow" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
            <path d="M0,0 L10,5 L0,10 z" fill="#c4cddb" />
          </marker>
        </defs>
        {tasks.map(t => {
          const p = layout.pos.get(t.id)!;
          const color = colorOf(t.status);
          const dim = selected && selected !== t.id;
          const ready = t.status === "planned" && t.deps.length > 0 && t.deps.every(d => tasks.find(x => x.id === d)?.status === "completed");
          return (
            <g key={t.id} data-id={t.id} style={{ cursor: "pointer", opacity: dim ? 0.45 : 1 }}
              onClick={() => onSelect(selected === t.id ? null : t.id)}>
              {ready && <rect x={p.x - 4} y={p.y - 4} width={NW + 8} height={NH + 8} rx={11} fill="#fffdf8" stroke="#d97706" strokeWidth={1.1} strokeDasharray="5 4" />}
              <rect x={p.x} y={p.y} width={NW} height={NH} rx={9} fill="#fff"
                stroke={selected === t.id ? "#d97706" : "#e5e7eb"} strokeWidth={selected === t.id ? 1.5 : 1} />
              <rect x={p.x} y={p.y + 6} width={3} height={NH - 12} rx={1.5} fill={color} />
              <g transform={`translate(${p.x + 12},${p.y + 13})`} dangerouslySetInnerHTML={{ __html: iconOf(t.status) }} />
              <text x={p.x + 25} y={p.y + 19} fontSize={12} fontWeight={600} fill={t.status === "cancelled" ? "#9ca3af" : "#1f2937"}
                textDecoration={t.status === "cancelled" ? "line-through" : "none"}>
                {t.title.length > 15 ? t.title.slice(0, 14) + "…" : t.title}
              </text>
              <text x={p.x + 25} y={p.y + 37} fontSize={10.5} fill="#6b7280">
                {t.deps.length ? `上游依赖 ${t.deps.length} 项` : "入口任务"}
              </text>
            </g>
          );
        })}
      </svg>
      <div className="mt-3 flex flex-wrap gap-4 border-t border-line pt-3 text-[11.5px] text-tx2">
        <span><i className="mr-1.5 inline-block h-2.5 w-2.5 rounded-sm" style={{ background: "#16a34a" }} />已完成 completed</span>
        <span><i className="mr-1.5 inline-block h-2.5 w-2.5 rounded-sm" style={{ background: "#d97706" }} />执行中 / 待验收 / 已派工</span>
        <span><i className="mr-1.5 inline-block h-2.5 w-2.5 rounded-sm" style={{ background: "#9ca3af" }} />待安排 / 已取消</span>
        <span><i className="mr-1.5 inline-block h-2.5 w-2.5 rounded-sm border border-dashed" style={{ borderColor: "#d97706" }} />ready_nodes 可开工</span>
      </div>
      {selected && (() => {
        const t = tasks.find(x => x.id === selected)!;
        return (
          <div className="absolute right-5 top-5 w-72 rounded-lg border border-line bg-white p-4 text-[12.5px] shadow-lg">
            <div className="mb-2 flex items-center justify-between">
              <span className="font-semibold" style={{ color: "#1f2937" }}>{t.title}</span>
              <button className="text-gray-400" onClick={() => onSelect(null)}>×</button>
            </div>
            <div style={{ display: "flex", gap: 8, fontSize: 12, color: "#6b7280", padding: "5px 0", borderBottom: "1px dashed #e5e7eb" }}>
              <em style={{ fontStyle: "normal", flexShrink: 0, width: 60, color: "#9ca3af" }}>任务 id</em>
              <b style={{ color: "#1f2937" }}>{t.id}</b>
            </div>
            <div style={{ display: "flex", gap: 8, fontSize: 12, color: "#6b7280", padding: "5px 0", borderBottom: "1px dashed #e5e7eb" }}>
              <em style={{ fontStyle: "normal", flexShrink: 0, width: 60, color: "#9ca3af" }}>状态</em>
              <b style={{ color: "#1f2937" }}>{t.status}</b>
            </div>
            <div style={{ display: "flex", gap: 8, fontSize: 12, color: "#6b7280", padding: "5px 0" }}>
              <em style={{ fontStyle: "normal", flexShrink: 0, width: 60, color: "#9ca3af" }}>上游依赖</em>
              <b style={{ color: "#1f2937" }}>{t.deps.join("、") || "无(入口任务)"}</b>
            </div>
          </div>
        );
      })()}
    </div>
  );
}
