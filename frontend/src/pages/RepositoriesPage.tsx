import { useEffect, useRef, useState } from "react";
import { allProjectRepositories, updateProject, type ProjectUpdateInput } from "../api/projects";
import { allPages } from "../api/pagination";
import { listRepositoryCandidates, type RepositoryCandidate } from "../api/repositories";
import { errText } from "../display";

/** Membership is the project API; the account catalog appears only in the add form. */
export function RepositoriesPage({ projectId, projectName, onNewIssue }: { projectId: string; projectName: string; onNewIssue: () => void }) {
  const [page, setPage] = useState<Awaited<ReturnType<typeof allProjectRepositories>> | null>(null);
  const [candidates, setCandidates] = useState<RepositoryCandidate[] | null>(null);
  const [selected, setSelected] = useState<string[]>([]);
  const [adding, setAdding] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [candidateError, setCandidateError] = useState<string | null>(null);
  const [reload, setReload] = useState(0);
  const attempt = useRef<{ input: ProjectUpdateInput; key: string } | null>(null);
  useEffect(() => {
    let cancelled = false;
    setError(null); setPage(null);
    allProjectRepositories(projectId).then(p => { if (!cancelled) setPage(p); }).catch(e => { if (!cancelled) setError(errText(e)); });
    return () => { cancelled = true; };
  }, [projectId, reload]);
  useEffect(() => {
    if (!adding) return;
    let cancelled = false;
    setCandidates(null); setCandidateError(null);
    allPages(cursor => listRepositoryCandidates({ cursor, limit: 100 })).then(items => { if (!cancelled) setCandidates(items); }).catch(e => { if (!cancelled) setCandidateError(errText(e)); });
    return () => { cancelled = true; };
  }, [adding, reload]);
  const save = async () => {
    if (!page || busy || !selected.length) return;
    attempt.current ??= { key: crypto.randomUUID(), input: { expectedProjectRevision: page.projectRevision, repositoryIdsToAdd: [...selected].sort() } };
    setBusy(true); setError(null);
    try {
      await updateProject(projectId, attempt.current.input, attempt.current.key);
      attempt.current = null; setSelected([]); setAdding(false); setReload(n => n + 1);
    } catch (e) {
      setError(errText(e));
      // Definitive optimistic-lock rejection: refresh before a new user selection.
      const status = (e as { status?: number }).status;
      if (status && status >= 400 && status < 500 && status !== 408 && status !== 429) {
        attempt.current = null;
        if (status === 409) { setSelected([]); setPage(null); }
      }
    } finally { setBusy(false); }
  };
  const button = "rounded-hard border border-line-strong px-3 py-2 text-xs disabled:opacity-50";
  const joined = new Set(page?.items.map(r => r.id));
  return <div className="max-w-[880px] space-y-5 py-5">
    <div className="flex items-center gap-4"><h1 className="mr-auto text-lg text-cream">{projectName} · 项目仓库</h1><button className={button} onClick={() => setAdding(!adding)} disabled={busy}>接入仓库</button><button className={button} disabled={!page?.items.length || busy} onClick={onNewIssue}>创建 Issue</button></div>
    <p className="text-sm text-tx2">本页仅显示当前项目已接入的仓库。接入新仓库不会扩大已有 Issue 的工作范围。</p>
    {error && <p role="alert" className="text-sm text-salmon-hi">{error} <button onClick={() => setReload(n => n + 1)}>重新加载</button></p>}
    {!page && !error && <p>正在读取项目仓库…</p>}
    {page?.items.length === 0 && <p className="text-sm">项目已保存，尚未接入仓库。请先接入仓库，再创建 Issue。</p>}
    {!!page?.restrictedRepositoryCount && <p className="text-sm text-salmon-hi">有 {page.restrictedRepositoryCount} 个已接入仓库当前不可访问。</p>}
    <div className="space-y-2">{page?.items.map(r => <div key={r.id} className="rounded-hard border border-line bg-panel p-4 text-sm"><span>{r.displayName}</span><span className="ml-4 text-xs text-tx2">{r.appCapability.status === "allowed" ? "App 工作授权就绪" : "App 工作授权不足"}</span></div>)}</div>
    {adding && <section className="space-y-3 rounded-hard border border-line bg-panel p-5"><h2 className="text-sm">从账号可参与的仓库中选择并接入</h2>
      {candidateError && <p role="alert" className="text-salmon-hi">候选加载失败：{candidateError} <button onClick={() => setReload(n => n + 1)}>重试</button></p>}
      {!candidates && !candidateError && <p>正在读取候选仓库…</p>}
      {candidates?.length === 0 && <p>暂无候选，请检查 GitHub 连接和仓库参与权限。</p>}
      <div className="max-h-80 space-y-2 overflow-auto">{candidates?.map(r => <label key={r.id} className="flex items-center gap-2 text-sm"><input type="checkbox" checked={joined.has(r.id) || selected.includes(r.id)} disabled={joined.has(r.id) || r.userParticipation.status !== "allowed" || busy || attempt.current !== null} onChange={e => setSelected(prev => e.target.checked ? [...prev, r.id] : prev.filter(id => id !== r.id))} />{r.displayName}<span className="text-xs text-tx3">{joined.has(r.id) ? "已接入" : r.appCapability.status !== "allowed" ? "App 工作授权不足" : "可接入"}</span></label>)}</div>
      <button className={button} disabled={!page || !selected.length || selected.length > 100 || busy} onClick={() => void save()}>{busy ? "保存中…" : attempt.current ? "重试本次接入" : selected.length > 100 ? "一次最多接入 100 个仓库" : `接入所选 ${selected.length} 个仓库`}</button>
    </section>}
  </div>;
}
