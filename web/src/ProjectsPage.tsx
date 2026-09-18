import { useEffect, useState } from "react";
import type { FormEvent } from "react";
import type { ApiError } from "./api";
import type { AuthenticatedPageProps } from "./projectPages";
import { projectErrorMessage, readProjects } from "./projectApi";
import type { ProjectSummary } from "./projectApi";
import { useRequestGate } from "./projectRequests";
import { ProjectShell } from "./ProjectShell";
import { Observation, useRetryDelay } from "./shared";

type ListState =
  | { kind: "loading"; items: ProjectSummary[] }
  | { kind: "ready"; items: ProjectSummary[]; nextCursor: string | null }
  | { kind: "error"; items: ProjectSummary[]; error: ApiError };

export function ProjectsPage(props: AuthenticatedPageProps) {
  const [search, setSearch] = useState("");
  const [query, setQuery] = useState("");
  const [cursor, setCursor] = useState<string | null>(null);
  const [revision, setRevision] = useState(0);
  const [state, setState] = useState<ListState>({ kind: "loading", items: [] });
  const gate = useRequestGate({ actor: props.session.user.id, sessionGeneration: props.auth.generation, route: "/projects", query: `${query}\u0000${cursor ?? ""}\u0000${revision}` });
  const retryDelay = useRetryDelay(state.kind === "error" ? state.error.retryAt : null);

  useEffect(() => {
    const ticket = gate.begin();
    setState((before) => ({ kind: "loading", items: cursor === null ? [] : before.items }));
    void readProjects({ query, cursor, signal: ticket.signal }).then((result) => {
      if (!gate.accepts(ticket)) return;
      if (result.kind === "error") {
        if (result.status === 401) {
          setState({ kind: "loading", items: [] });
          props.auth.unauthorized(ticket.sessionGeneration);
          return;
        }
        if (result.code === "INVALID_CURSOR" || result.code === "CURSOR_EXPIRED") {
          if (cursor !== null) {
            setCursor(null);
            setState({ kind: "loading", items: [] });
            return;
          }
        }
        setState((before) => ({ kind: "error", items: before.items, error: result }));
        return;
      }
      setState((before) => ({ kind: "ready", items: cursor === null ? result.value.items : [...before.items, ...result.value.items], nextCursor: result.value.nextCursor }));
    });
    return () => gate.invalidate();
  }, [cursor, gate, props.auth, query, revision]);

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (Array.from(search).length > 200 || retryDelay > 0) return;
    setCursor(null);
    setQuery(search);
    setRevision((before) => before + 1);
  }

  return <ProjectShell {...props} title="项目">
    <div className="page-heading"><div><p className="eyebrow">项目管理</p><h1>项目</h1><p className="muted">查看当前账号创建的项目，或保存一个待配置项目。</p></div><button className="primary compact" onClick={() => props.navigate("/projects/new")}>新建项目</button></div>
    <section className="panel">
      <div className="section-heading"><div><h2>项目列表</h2><p className="small muted">按稳定项目身份排序，搜索只匹配名称。</p></div><button disabled={state.kind === "loading" || retryDelay > 0} onClick={() => { setCursor(null); setRevision((before) => before + 1); }}>刷新</button></div>
      <form className="search-form" role="search" onSubmit={submit}><label className="sr-only" htmlFor="project-search">搜索项目名称</label><input id="project-search" type="search" value={search} placeholder="搜索项目名称" onChange={(event) => setSearch(event.target.value)} /><button type="submit" disabled={Array.from(search).length > 200 || retryDelay > 0}>搜索</button></form>
      {state.kind === "loading" && state.items.length === 0 && <div className="empty-state" role="status"><span className="spinner" aria-hidden="true" /><p>正在读取项目…</p></div>}
      {state.kind === "error" && <div className="repository-error"><p className="notice" role="alert">{projectErrorMessage(state.error)}</p><p className="small muted">读取失败不等于项目列表为空。</p><button onClick={() => setRevision((before) => before + 1)}>重新读取项目</button></div>}
      {state.kind === "ready" && state.items.length === 0 && <div className="empty-state"><p>{state.nextCursor !== null ? "这一页没有项目，仍可继续加载" : query ? "没有匹配的项目" : "还没有项目"}</p>{state.nextCursor === null && <button className="primary compact" onClick={() => props.navigate("/projects/new")}>保存第一个项目</button>}</div>}
      {state.items.length > 0 && <ul className="project-list">{state.items.map((project) => <li key={project.id}><a href={`/projects/${encodeURIComponent(project.id)}`} onClick={(event) => { event.preventDefault(); props.navigate(`/projects/${encodeURIComponent(project.id)}`); }}><strong>{project.name}</strong><span>创建于 <Observation at={project.createdAt} /></span></a></li>)}</ul>}
      {state.kind === "ready" && state.nextCursor !== null && <button className="load-more" onClick={() => setCursor(state.nextCursor)}>加载更多项目</button>}
      {state.kind === "loading" && state.items.length > 0 && <p className="small muted" role="status">正在加载下一页…</p>}
    </section>
  </ProjectShell>;
}
