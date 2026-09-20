import { useCallback, useRef, useState } from "react";
import {
  archiveProject,
  createProject,
  listArchivedProjects,
  restoreProject,
  type ArchivedProjectItem,
  type ProjectCreateInput,
  type ProjectListItem,
} from "../api/projects";
import { errText } from "../display";

/** 项目管理（侧栏「项目」，也是没有当前项目时的落地页）。
 *
 *  2026-09-20 统一装修：这一页此前停在旧尺寸——`h1 text-lg`、正文 `text-sm`（14px）、
 *  卡片 `p-4/p-5`、按钮 `py-2`，整页比仓库页高出一大截，和旁边几个页面不像同一个产品。
 *  现在标题、卡片、输入框、按钮、当前项目标记全部换成全站既有写法（见 `RepositoriesPage`
 *  与 `AddRepositoryCard`），并把外壳已经给过的页边距让出来（外壳是 `px-8 pt-5 pb-10`，
 *  这里不再叠 `py-6`）。
 *
 *  2026-09-20 加**归档**（软删除）：每个项目一枚「归档」chip，归档后项目从列表消失、
 *  数据一行不删；标题行的「已归档」展开归档区，可随时「还原」。归档的是当前项目时，
 *  外壳会在列表刷新后自动落回这一页（activeProjectId 校验不过即清空）。 */

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

  // 归档区：默认收起，第一次展开才拉取（归档量小，一次给全，后端封顶 200）。
  const [showArchived, setShowArchived] = useState(false);
  const [archived, setArchived] = useState<ArchivedProjectItem[] | null>(null);
  const [archivedLoading, setArchivedLoading] = useState(false);
  const [archivedError, setArchivedError] = useState<string | null>(null);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [actionFailure, setActionFailure] = useState<string | null>(null);

  const loadArchived = useCallback(() => {
    setArchivedLoading(true);
    setArchivedError(null);
    listArchivedProjects()
      .then(page => setArchived(page.items))
      .catch(e => setArchivedError(errText(e)))
      .finally(() => setArchivedLoading(false));
  }, []);

  const toggleArchived = () => {
    const next = !showArchived;
    setShowArchived(next);
    if (next) loadArchived();
  };

  const doArchive = async (id: string, projectName: string) => {
    if (busyId !== null) return;
    if (!window.confirm(`是否确认归档「${projectName}」？\n归档是软删除：项目从列表消失、数据保留，可随时还原。`)) return;
    setBusyId(id);
    setActionFailure(null);
    try {
      await archiveProject(id);
      onRetry();
      if (showArchived) loadArchived();
    } catch (error) {
      setActionFailure(errText(error));
    } finally {
      setBusyId(null);
    }
  };

  const doRestore = async (id: string) => {
    if (busyId !== null) return;
    setBusyId(id);
    setActionFailure(null);
    try {
      await restoreProject(id);
      onRetry();
      loadArchived();
    } catch (error) {
      setActionFailure(errText(error));
    } finally {
      setBusyId(null);
    }
  };

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
        <button className={chip} onClick={toggleArchived}>
          {showArchived ? "收起已归档" : archived?.length ? `已归档（${archived.length}）` : "已归档"}
        </button>
        <button className={`ml-auto ${chip}`} onClick={() => setCreating(!creating)}>
          {creating ? "收起" : "+ 新建项目"}
        </button>
      </div>

      {showArchived && (
        <section className="mt-3 rounded-hard border border-line bg-panel px-4 py-3">
          <div className="flex items-baseline gap-2">
            <span className="text-[12.5px] text-cream">已归档项目</span>
            <span className="text-[11px] text-tx3">软删除：数据保留，可随时还原</span>
          </div>
          {archivedLoading && <p className="mt-2 text-[12px] text-tx3">正在读取…</p>}
          {archivedError && (
            <p role="alert" className="mt-2 text-[11.5px] text-salmon">
              {archivedError}
              <button className="ml-2 underline underline-offset-2 hover:text-salmon-hi" onClick={loadArchived}>重试</button>
            </p>
          )}
          {archived?.length === 0 && <p className="mt-2 text-[12px] text-tx3">没有已归档的项目。</p>}
          <div className="mt-2 space-y-1.5">
            {archived?.map(p => (
              <div key={p.id} className="flex flex-wrap items-center gap-x-2.5 rounded-hard border border-line px-3 py-2">
                <span className="min-w-0 flex-1 truncate text-[12px] text-tx2">{p.name}</span>
                {p.removedAt && (
                  <span className="text-[11px] text-tx3" title={p.removedAt}>
                    归档于 {new Date(p.removedAt).toLocaleString()}
                  </span>
                )}
                <button className={chip} disabled={busyId !== null} onClick={() => void doRestore(p.id)}>还原</button>
              </div>
            ))}
          </div>
        </section>
      )}

      {actionFailure && (
        <p role="alert" className="mt-3 rounded-hard border border-salmon/40 bg-salmon-well px-3 py-2 text-[11.5px] text-salmon">
          {actionFailure}
        </p>
      )}

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
              <button className={chip} disabled={busyId !== null} onClick={() => void doArchive(p.id, p.name)}>归档</button>
            </span>
          </section>
        ))}
      </div>
    </div>
  );
}
