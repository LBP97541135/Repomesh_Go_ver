import { useRef, useState } from "react";
import { createProject, type ProjectCreateInput, type ProjectListItem } from "../api/projects";
import { errText } from "../display";

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
  const button = "rounded-hard border border-line-strong px-3 py-2 text-[12px] text-cream disabled:opacity-50";
  return <div className="mx-auto max-w-[880px] py-6">
    <div className="mb-6 flex items-center justify-between">
      <div><h1 className="text-lg text-cream">项目管理</h1><p className="mt-2 text-sm text-tx2">① 创建项目 → ② 接入仓库 → ③ 创建 Issue</p></div>
      <button className={button} onClick={() => setCreating(!creating)}>新建项目</button>
    </div>
    {creating && <section className="mb-6 space-y-4 rounded-hard border border-line bg-panel p-5">
      <h2 className="text-sm text-cream">第一步：保存项目基本信息</h2>
      <label className="block text-sm">项目名称<input className="mt-1 block w-full border border-line bg-well p-2" value={name} disabled={busy || attempt.current !== null} onChange={e => setName(e.target.value)} /></label>
      <label className="block text-sm">项目用途<textarea className="mt-1 block w-full border border-line bg-well p-2" value={purpose} disabled={busy || attempt.current !== null} onChange={e => setPurpose(e.target.value)} /></label>
      {failure && <p role="alert" className="text-sm text-salmon-hi">{failure}。可重试本次保存；成功后进入仓库接入。</p>}
      <button className={button} disabled={busy || !name.trim()} onClick={() => void submit()}>{busy ? "保存中…" : attempt.current ? "重试保存" : "保存项目，继续选择仓库"}</button>
    </section>}
    {error && <p role="alert" className="mb-4 text-salmon-hi">项目加载失败：{error} <button onClick={onRetry}>重试</button></p>}
    {projects === null && !error && <p>正在读取项目…</p>}
    {projects?.length === 0 && <p className="text-sm text-tx2">尚无项目，请先创建项目。</p>}
    <div className="space-y-3">{projects?.map(p => <section key={p.id} className="flex items-center gap-3 rounded-hard border border-line bg-panel p-4">
      <span className="mr-auto text-sm">{p.name}{p.id === activeProjectId ? " · 当前项目" : ""}</span>
      <button className={button} onClick={() => onSelect(p.id, "repositories")}>管理仓库</button>
      <button className={button} onClick={() => onSelect(p.id, "issues")}>进入 Issue</button>
    </section>)}</div>
  </div>;
}
