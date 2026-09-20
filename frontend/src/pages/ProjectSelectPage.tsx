import { useRef, useState } from "react";
import { createProject, type ProjectCreateInput, type ProjectListItem } from "../api/projects";
import { errText } from "../display";

/** 项目管理（侧栏「项目」，也是没有当前项目时的落地页）。
 *
 *  2026-09-20 统一装修：这一页此前停在旧尺寸——`h1 text-lg`、正文 `text-sm`（14px）、
 *  卡片 `p-4/p-5`、按钮 `py-2`，整页比仓库页高出一大截，和旁边几个页面不像同一个产品。
 *  现在标题、卡片、输入框、按钮、当前项目标记全部换成全站既有写法（见 `RepositoriesPage`
 *  与 `AddRepositoryCard`），并把外壳已经给过的页边距让出来（外壳是 `px-8 pt-5 pb-10`，
 *  这里不再叠 `py-6`）。
 *
 *  **行为一行没改**：仍是「建项目 → 选仓库 → 建 Issue」三步，两个动作按钮、幂等键
 *  重试策略、禁用条件都与改版前一致。 */

/** 小尺寸次要按钮：仓库页同款。 */
const chip =
  "flex-none rounded-hard border border-line px-2.5 py-[3px] text-[11.5px] text-tx2 hover:border-amber hover:text-amber-hi disabled:opacity-50";
/** 主按钮：与「添加仓库」里的确认键同款。 */
const primary =
  "rounded-hard bg-amber px-3.5 py-[6px] text-[12px] font-extrabold text-on-amber hover:bg-amber-hi disabled:opacity-60";
/** 输入框：与「添加仓库」里那个地址输入同款（等宽字体是给仓库地址用的，这里不用）。 */
const field =
  "mt-1 block w-full resize-none rounded-hard border border-line bg-ink px-2.5 py-[6px] text-[12px] text-tx placeholder:text-tx3 focus:border-amber focus:outline-none disabled:opacity-60";

export function ProjectSelectPage({ projects, activeProjectId, error, onRetry, onSelect }: {
  projects: ProjectListItem[] | null;
  activeProjectId: string | null;
  error: string | null;
  onRetry: () => void;
  onSelect: (id: string, destination: "repositories" | "issues") => void;
}) {
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState("");
  const [purpose, setPurpose] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);
  const attempt = useRef<{ key: string; input: ProjectCreateInput } | null>(null);
  const submit = async () => {
    if (busy || !name.trim()) return;
    attempt.current ??= { key: crypto.randomUUID(), input: { name: name.trim(), purpose: purpose.trim() || name.trim(), repositoryIds: [] } };
    setBusy(true);
    setFailure(null);
    try {
      const receipt = await createProject(attempt.current.input, attempt.current.key);
      onSelect(receipt.projectId, "repositories");
    } catch (error) {
      setFailure(errText(error));
      const status = (error as { status?: number }).status;
      if (status && status >= 400 && status < 500 && status !== 408 && status !== 429) attempt.current = null;
    }
    finally { setBusy(false); }
  };
  return (
    <div className="max-w-[860px]">
      <div className="flex items-baseline gap-3 border-b border-line pb-3">
        <h1 className="text-[16px] font-semibold text-cream">项目</h1>
        {projects && <span className="text-[11.5px] text-tx2">{projects.length} 个</span>}
        {/* 三步走是这一页唯一需要先讲清的事，放在标题行里，不另起一段占高度。 */}
        <span className="hidden text-[11px] text-tx3 sm:inline">新建 → 接入仓库 → 建 Issue</span>
        <button className={`ml-auto ${chip}`} onClick={() => setCreating(!creating)}>
          {creating ? "收起" : "+ 新建项目"}
        </button>
      </div>

      {creating && (
        <section className="mt-3 rounded-hard border border-line bg-panel px-4 py-3">
          <div className="flex items-baseline gap-2">
            <span className="text-[12.5px] text-cream">新建项目</span>
            <span className="text-[11px] text-tx3">先落基本信息，保存后直接进入仓库接入</span>
          </div>
          {/* 两块并排（窄屏堆叠、宽屏一行）——旧的竖排两个大字段是这一页高度的主要来源。 */}
          <div className="mt-2.5 grid gap-2.5 sm:grid-cols-[minmax(0,1fr)_minmax(0,1.6fr)]">
            <label className="block">
              <span className="text-[11.5px] text-tx2">项目名称</span>
              <input className={field} value={name} disabled={busy || attempt.current !== null} onChange={e => setName(e.target.value)} />
            </label>
            <label className="block">
              <span className="text-[11.5px] text-tx2">项目用途</span>
              {/* 仍是 textarea（用途可能写多行），只是收到一行高、不许拖拽。 */}
              <textarea rows={1} className={field} value={purpose} disabled={busy || attempt.current !== null} onChange={e => setPurpose(e.target.value)} />
            </label>
          </div>
          {failure && (
            <p role="alert" className="mt-2.5 rounded-hard border border-salmon/40 bg-salmon-well px-2.5 py-1.5 text-[11.5px] text-salmon">
              {failure}。可重试本次保存；成功后进入仓库接入。
            </p>
          )}
          <div className="mt-2.5 flex items-center gap-2.5">
            <button className={primary} disabled={busy || !name.trim()} onClick={() => void submit()}>
              {busy ? "保存中…" : attempt.current ? "重试保存" : "保存项目，继续选择仓库"}
            </button>
            <span className="text-[11px] text-tx3">保存后会直接跳到这个项目的仓库页</span>
          </div>
        </section>
      )}

      {error && (
        <p role="alert" className="mt-3 rounded-hard border border-salmon/40 bg-salmon-well px-3 py-2 text-[11.5px] text-salmon">
          项目加载失败：{error}
          <button className="ml-2 underline underline-offset-2 hover:text-salmon-hi" onClick={onRetry}>重试</button>
        </p>
      )}
      {projects === null && !error && <p className="mt-3 text-[12.5px] text-tx3">正在读取项目…</p>}
      {projects?.length === 0 && (
        <p className="mt-3 rounded-hard border border-amber/40 bg-amber-well px-3 py-2 text-[11.5px] text-amber">
          还没有项目。点右上角「+ 新建项目」，保存后就能接入仓库、建 Issue。
        </p>
      )}

      <div className="mt-3 space-y-2">
        {projects?.map(p => (
          <section
            key={p.id}
            className="flex flex-wrap items-center gap-x-2.5 gap-y-1.5 rounded-hard border border-line bg-panel px-4 py-3 transition-colors hover:border-line-strong"
          >
            <span className="min-w-0 flex-1 truncate text-[12.5px] text-tx" title={p.id}>{p.name}</span>
            {/* 「当前项目」用全站的 .pill 族，跟仓库页的「已接入本项目」同一套观感。 */}
            {p.id === activeProjectId && <span className="pill pill-done">当前项目</span>}
            <span className="flex flex-none items-center gap-1.5">
              <button className={chip} onClick={() => onSelect(p.id, "repositories")}>管理仓库</button>
              <button className={chip} onClick={() => onSelect(p.id, "issues")}>进入 Issue</button>
            </span>
          </section>
        ))}
      </div>
    </div>
  );
}
