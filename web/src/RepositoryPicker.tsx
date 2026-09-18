import { useEffect, useRef, useState } from "react";
import type { FormEvent } from "react";
import { readRepositories } from "./api";
import type { ApiError, RepositoryPage } from "./api";
import type { RepositoryPickerProps } from "./projectPages";
import { clearSelectionDisclosure, expireSelection } from "./projectDrafts";
import { useRequestGate } from "./projectRequests";
import { projectErrorMessage } from "./projectApi";
import { Observation, useRetryDelay } from "./shared";

type PickerPage = { kind: "loading" } | { kind: "ready"; value: RepositoryPage } | { kind: "error"; error: ApiError };

export function RepositoryPicker({ session, auth, selection, onChange, onDone, maximumSelection }: RepositoryPickerProps) {
  const [search, setSearch] = useState("");
  const [query, setQuery] = useState("");
  const [cursor, setCursor] = useState<string | null>(null);
  const [pages, setPages] = useState<RepositoryPage[]>([]);
  const [state, setState] = useState<PickerPage>({ kind: "loading" });
  const [revision, setRevision] = useState(0);
  const selectionRef = useRef(selection);
  const onChangeRef = useRef(onChange);
  selectionRef.current = selection;
  onChangeRef.current = onChange;
  const unauthorized = auth.unauthorized;
  const gate = useRequestGate({ actor: session.user.id, sessionGeneration: auth.generation, route: window.location.pathname, query: `${query}\u0000${cursor ?? ""}\u0000${revision}` });
  const retryDelay = useRetryDelay(state.kind === "error" ? state.error.retryAt : null);

  useEffect(() => {
    const expire = () => {
      const before = selectionRef.current;
      const next = expireSelection(before, Date.now());
      if (Array.from(before.entries()).some(([id, item]) => item.kind !== next.get(id)?.kind)) onChangeRef.current(next);
    };
    expire();
    const timer = window.setInterval(expire, 1000);
    return () => window.clearInterval(timer);
  }, []);

  useEffect(() => {
    const ticket = gate.begin();
    setState({ kind: "loading" });
    void readRepositories({ query, cursor, signal: ticket.signal }).then((result) => {
      if (!gate.accepts(ticket)) return;
      if (result.kind === "error") {
        if (result.status === 401) {
          onChangeRef.current(clearSelectionDisclosure(selectionRef.current));
          setPages([]);
          unauthorized(ticket.sessionGeneration);
          return;
        }
        setState({ kind: "error", error: result });
        return;
      }
      setState({ kind: "ready", value: result.value });
      setPages((current) => cursor === null ? [result.value] : [...current, result.value]);
    });
    return () => gate.invalidate();
  }, [cursor, gate, query, revision, unauthorized]);

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (Array.from(search).length > 200 || retryDelay > 0) return;
    setPages([]);
    setCursor(null);
    setQuery(search);
  }

  function toggle(repository: RepositoryPage["items"][number]) {
    const current = new Map(expireSelection(selection, Date.now()));
    if (current.has(repository.id)) current.delete(repository.id);
    else if (current.size < maximumSelection) current.set(repository.id, { kind: "confirmed", id: repository.id, repository });
    onChange(current);
  }

  const current = expireSelection(selection, Date.now());
  const confirmed = Array.from(current.values()).filter((item) => item.kind === "confirmed");
  const unconfirmedCount = current.size - confirmed.length;
  const items = pages.flatMap((page) => page.items).filter((item, index, all) => all.findIndex((candidate) => candidate.id === item.id) === index);
  const latest = pages.at(-1) ?? (state.kind === "ready" ? state.value : null);

  return <section className="picker" aria-labelledby="picker-title">
    <div className="section-heading"><div><h2 id="picker-title">选择仓库</h2><p className="small muted">只添加本次明确选择的仓库。搜索和翻页不会改变已选集合。</p></div></div>
    <div className="selection-summary">
      <h3>本次已选 <span className="pill">{current.size}/{maximumSelection}</span></h3>
      {confirmed.length === 0 && unconfirmedCount === 0 && <p className="small muted">还没有选择仓库。</p>}
      {confirmed.length > 0 && <ul className="selected-list">{confirmed.map((item) => <li key={item.id}><span>{item.repository.displayName}</span><button type="button" onClick={() => { const next = new Map(current); next.delete(item.id); onChange(next); }}>撤回</button></li>)}</ul>}
      {unconfirmedCount > 0 && <div className="unconfirmed-selection"><p>{unconfirmedCount} 项读取资格尚未核实，名称已隐藏。撤回后可继续保存其他修改。</p>{Array.from({ length: unconfirmedCount }, (_, index) => <button type="button" key={index} onClick={() => { const next = new Map(current); const target = Array.from(next.entries()).find((entry) => entry[1].kind === "unconfirmed"); if (target) next.delete(target[0]); onChange(next); }}>撤回一项未核实选择</button>)}</div>}
    </div>
    <form className="search-form" role="search" onSubmit={submit}><label className="sr-only" htmlFor="project-repository-search">搜索仓库候选</label><input id="project-repository-search" type="search" placeholder="搜索已发现的仓库" value={search} onChange={(event) => setSearch(event.target.value)} /><button type="submit" disabled={Array.from(search).length > 200 || retryDelay > 0}>搜索</button></form>
    {state.kind === "loading" && pages.length === 0 && <div className="empty-state" role="status"><span className="spinner" aria-hidden="true" /><p>正在读取仓库候选…</p></div>}
    {state.kind === "error" && <div className="repository-error"><p className="notice" role="alert">{projectErrorMessage(state.error)}</p><p className="small muted">读取失败不会把仍有效的已选仓库判为无权。</p><button type="button" disabled={retryDelay > 0} onClick={() => { if (state.error.code === "INVALID_CURSOR" || state.error.code === "CURSOR_EXPIRED") { setPages([]); setCursor(null); } setRevision((value) => value + 1); }}>重试读取</button></div>}
    {latest && <div className="coverage"><span className={latest.coverage.status === "complete" ? "pill good" : "pill amber"}>{latest.coverage.status === "complete" ? "当前发现范围完整" : latest.coverage.status === "partial" ? "部分发现" : "发现覆盖待确认"}</span><p>{latest.coverage.status === "complete" ? "当前发现批次已完整读取。" : "当前候选不代表账号的全部可参与仓库。"}</p></div>}
    {items.length === 0 && state.kind === "ready" && <div className="empty-state"><p>{latest?.nextCursor ? "本页没有可披露仓库，仍可继续加载" : query ? "当前发现结果没有匹配仓库" : "当前没有可展示的仓库候选"}</p></div>}
    {items.length > 0 && <ul className="repository-list selectable-list">{items.map((repository) => <li key={repository.id}><label><input type="checkbox" checked={current.has(repository.id)} disabled={!current.has(repository.id) && current.size >= maximumSelection} onChange={() => toggle(repository)} /><span className="repo-name"><strong>{repository.displayName}</strong><span className="small muted">读取资格已核实，<Observation at={repository.userParticipation.observedAt} /></span></span><span className={repository.appCapability.status === "allowed" ? "pill good" : "pill amber"}>{repository.appCapability.status === "allowed" ? "App 能力已核实" : repository.appCapability.status === "denied" ? "App 工作授权不足" : "App 能力待确认"}</span></label></li>)}</ul>}
    <div className="picker-actions"><button type="button" disabled={latest?.nextCursor === null || state.kind === "loading"} onClick={() => { if (latest?.nextCursor) setCursor(latest.nextCursor); }}>加载更多</button><button type="button" className="primary compact" onClick={onDone}>完成选择</button></div>
  </section>;
}
