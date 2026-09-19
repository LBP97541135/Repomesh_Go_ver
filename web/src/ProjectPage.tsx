import { useEffect, useState } from "react";
import type { ApiError } from "./api";
import type { ProjectPageProps } from "./projectPages";
import { projectErrorMessage, readProject, readProjectRepositories } from "./projectApi";
import type { ProjectRepositoryPage, ProjectView, RepositoryItem } from "./projectApi";
import { clearOperationInputs } from "./projectRecovery";
import { useProjectSnapshot, useRequestGate } from "./projectRequests";
import { ProjectShell } from "./ProjectShell";
import { Observation } from "./shared";

type RepositoryState =
  | { kind: "idle" }
  | { kind: "loading"; items: RepositoryItem[]; restricted: number }
  | { kind: "ready"; items: RepositoryItem[]; page: ProjectRepositoryPage }
  | { kind: "error"; items: RepositoryItem[]; restricted: number; error: ApiError };

function selectionLabel(selection: { mode: "inherit" } | { mode: "reference"; id: string }): string {
  return selection.mode === "inherit" ? "继承默认" : `引用 ${selection.id}`;
}

export function ProjectPage(props: ProjectPageProps) {
  const context = { actor: props.session.user.id, sessionGeneration: props.auth.generation, route: `/projects/${props.projectId}`, query: "" };
  const snapshot = useProjectSnapshot({ context, auth: props.auth, load: (signal) => readProject({ projectId: props.projectId, signal }), onInaccessible: () => clearOperationInputs({ actor: props.session.user.id, projectId: props.projectId }) });
  const [cursor, setCursor] = useState<string | null>(null);
  const [revision, setRevision] = useState(0);
  const [repositories, setRepositories] = useState<RepositoryState>({ kind: "idle" });
  const [readable, setReadable] = useState<ProjectView | null>(null);
  const [inaccessible, setInaccessible] = useState(false);
  const project = inaccessible ? null : snapshot.state.kind === "ready"
    ? snapshot.state.value
    : snapshot.state.kind === "error" && snapshot.state.error.code === "PROJECT_UPDATE_NOT_ALLOWED" && readable !== null
      ? { ...readable, actions: { ...readable.actions, canEdit: false } }
      : null;
  const projectRevision = project?.projectRevision ?? readable?.projectRevision ?? "";
  const repositoryGate = useRequestGate({ ...context, route: `${context.route}/repositories`, query: `${projectRevision}\u0000${cursor ?? ""}\u0000${revision}` });

  useEffect(() => {
    if (inaccessible) return;
    if (snapshot.state.kind === "ready") setReadable(snapshot.state.value);
    else if (snapshot.state.kind === "inaccessible") setReadable(null);
  }, [inaccessible, snapshot.state]);

  useEffect(() => {
    const timer = window.setInterval(() => setRepositories((current) => {
      if (current.kind === "idle") return current;
      const items = current.items.filter((item) => {
        const observed = Date.parse(item.userParticipation.observedAt ?? "");
        return Number.isFinite(observed) && observed <= Date.now() && Date.now() - observed <= 60_000;
      });
      if (current.kind === "ready") return { ...current, items };
      return { ...current, items };
    }), 1000);
    return () => window.clearInterval(timer);
  }, []);

  useEffect(() => {
    if (snapshot.state.kind !== "ready") {
      if (readable !== null && (snapshot.state.kind === "loading" || snapshot.state.kind === "error" && snapshot.state.error.code === "PROJECT_UPDATE_NOT_ALLOWED")) return;
      repositoryGate.invalidate();
      setRepositories({ kind: "idle" });
      return;
    }
    const expectedRevision = snapshot.state.value.projectRevision;
    const ticket = repositoryGate.begin();
    setRepositories((before) => ({ kind: "loading", items: cursor === null || before.kind === "idle" ? [] : before.items, restricted: before.kind === "idle" ? 0 : "restricted" in before ? before.restricted : before.page.restrictedRepositoryCount }));
    void readProjectRepositories({ projectId: props.projectId, cursor, signal: ticket.signal }).then((result) => {
      if (!repositoryGate.accepts(ticket)) return;
      if (result.kind === "error") {
        if (result.status === 401) {
          setRepositories({ kind: "idle" });
          props.auth.unauthorized(ticket.sessionGeneration);
          return;
        }
        if (result.code === "RESOURCE_NOT_FOUND") {
          clearOperationInputs({ actor: props.session.user.id, projectId: props.projectId });
          repositoryGate.invalidate();
          setRepositories({ kind: "idle" });
          setReadable(null);
          setInaccessible(true);
          return;
        }
        if (result.code === "PROJECT_CONTEXT_CHANGED" || result.code === "INVALID_CURSOR" || result.code === "CURSOR_EXPIRED") {
          setCursor(null);
          setRepositories({ kind: "idle" });
          void snapshot.refresh();
          return;
        }
        setRepositories((before) => ({ kind: "error", items: before.kind === "idle" ? [] : before.items, restricted: before.kind === "ready" ? before.page.restrictedRepositoryCount : before.kind === "idle" ? 0 : before.restricted, error: result }));
        return;
      }
      if (result.value.projectRevision !== expectedRevision) {
        setCursor(null);
        setRepositories({ kind: "idle" });
        void snapshot.refresh();
        return;
      }
      setRepositories((before) => ({ kind: "ready", items: cursor === null || before.kind === "idle" ? result.value.items : [...before.items, ...result.value.items], page: result.value }));
    });
    return () => repositoryGate.invalidate();
  }, [cursor, props.auth, props.projectId, props.session.user.id, readable, repositoryGate, revision, snapshot.refresh, snapshot.state]);

  return <ProjectShell {...props} title="项目概览" projectId={props.projectId}>
    {snapshot.state.kind === "loading" && project === null && <div className="empty-state" role="status"><span className="spinner" aria-hidden="true" /><p>正在读取当前项目…</p></div>}
    {snapshot.state.kind === "error" && project === null && <div className="repository-error"><p className="notice" role="alert">{projectErrorMessage(snapshot.state.error)}</p><button onClick={() => void snapshot.refresh()}>重新读取项目</button></div>}
    {(snapshot.state.kind === "inaccessible" || inaccessible) && <div className="empty-state"><p>当前无法访问该项目。</p><button onClick={() => props.navigate("/projects")}>返回项目列表</button></div>}
    {snapshot.state.kind === "error" && project !== null && <p className="notice" role="alert">{projectErrorMessage(snapshot.state.error)}</p>}
    {project !== null && <>
      <div className="page-heading"><div><p className="eyebrow">项目概览</p><h1>{project.name}</h1><p className="muted">创建于 <Observation at={project.createdAt} /></p></div>{project.actions.canEdit ? <button className="primary compact" onClick={() => props.navigate(`/projects/${encodeURIComponent(props.projectId)}/settings`)}>项目设置</button> : <span className="pill amber">只读</span>}<button className="primary compact" onClick={() => { window.location.href = "/"; }}>发起 Issue（交付控制台）</button></div>
      <section className="panel"><h2>项目用途</h2><p className="preserve-lines">{project.purpose}</p></section>
      <section className="panel"><div className="section-heading"><div><h2>已保存仓库</h2><p className="small muted">只显示本次可披露的仓库。未披露项仍保留在项目范围。</p></div><button disabled={repositories.kind === "loading"} onClick={() => { setCursor(null); setRevision((value) => value + 1); }}>刷新</button></div>
        {repositories.kind === "idle" || repositories.kind === "loading" && repositories.items.length === 0 ? <div className="empty-state" role="status"><span className="spinner" aria-hidden="true" /><p>正在核实已保存仓库…</p></div> : null}
        {repositories.kind === "error" && <div><p className="notice" role="alert">{projectErrorMessage(repositories.error)}</p><button onClick={() => setRevision((value) => value + 1)}>重新读取仓库</button></div>}
        {repositories.kind === "ready" && repositories.items.length === 0 && repositories.page.restrictedRepositoryCount === 0 && repositories.page.nextCursor === null && <div className="empty-state"><p>当前项目没有可展示的仓库。</p></div>}
        {repositories.kind !== "idle" && repositories.items.length > 0 && <ul className="repository-list">{repositories.items.map((repository) => <li key={repository.id}><span className="repo-name"><strong>{repository.displayName}</strong><span className="small muted">读取资格已核实</span></span><span className={repository.appCapability.status === "allowed" ? "pill good" : "pill amber"}>{repository.appCapability.status === "allowed" ? "App 能力已核实" : "App 能力未就绪"}</span></li>)}</ul>}
        {repositories.kind === "ready" && repositories.page.restrictedRepositoryCount > 0 && <p className="notice">另有 {repositories.page.restrictedRepositoryCount} 个已保存仓库本次未披露。无法据此判断为已失权。</p>}
        {repositories.kind === "ready" && repositories.page.nextCursor !== null && <button className="load-more" onClick={() => setCursor(repositories.page.nextCursor)}>加载更多已保存仓库</button>}
      </section>
      <section className="panel"><h2>固定配置引用</h2><dl className="summary-list"><div><dt>模型</dt><dd>{selectionLabel(project.configuration.modelProfile)}</dd></div><div><dt>执行</dt><dd>{selectionLabel(project.configuration.executionProfile)}</dd></div><div><dt>并发</dt><dd>{project.configuration.effective.workerConcurrency ?? "未解析"}</dd></div><div><dt>验证小组</dt><dd>{project.configuration.effective.verificationGroupEnabled === null ? "未解析" : project.configuration.effective.verificationGroupEnabled ? "启用" : "关闭"}</dd></div></dl><p className={project.configuration.checks.status === "allowed" ? "status-copy good-copy" : "status-copy amber-copy"}>{project.configuration.checks.status === "allowed" ? "当前配置检查通过，但不表示模型调用或运行已接通。" : project.configuration.checks.status === "denied" ? "项目已保存，必要配置尚未满足。" : "项目已保存，配置可用性待确认。"}</p></section>
    </>}
  </ProjectShell>;
}
