import { useEffect, useRef, useState } from "react";
import { ConfigurationFields } from "./ConfigurationFields";
import type { ApiError } from "./api";
import type { ProjectPageProps } from "./projectPages";
import { updateInput } from "./projectDrafts";
import type { ConfigurationEdit, RepositorySelection } from "./projectDrafts";
import { projectErrorMessage, readProject, readProjectRepositories, updateProject } from "./projectApi";
import type { ProjectFieldError, ProjectRepositoryPage, ProjectView, RepositoryItem } from "./projectApi";
import { clearAllOperationInputs, clearOperationInputs, prepareOperation, projectRecoveryPath } from "./projectRecovery";
import type { OperationState, PreparedOperation, UpdateIdentity } from "./projectRecovery";
import { useProjectSnapshot, useRequestGate } from "./projectRequests";
import { ProjectOperationResult } from "./ProjectOperationResult";
import { ProjectShell } from "./ProjectShell";
import { RepositoryPicker } from "./RepositoryPicker";

type DraftState = { base: ProjectView; name: string; purpose: string; additions: RepositorySelection; configuration: ConfigurationEdit };
type SavedState = { kind: "loading"; items: RepositoryItem[] } | { kind: "ready"; items: RepositoryItem[]; page: ProjectRepositoryPage } | { kind: "error"; items: RepositoryItem[]; error: ApiError };

function localFieldMessage(field: ProjectFieldError): string {
  if (field.code === "TOO_LONG") return field.field === "name" ? "项目名称最多 200 个 Unicode 字符。" : "项目用途最多 20000 个 Unicode 字符。";
  return field.field === "repositoryIdsToAdd" ? "一次最多明确添加 100 个仓库。" : "名称和用途不能只含空白。";
}

export function ProjectSettingsPage(props: ProjectPageProps) {
  const context = { actor: props.session.user.id, sessionGeneration: props.auth.generation, route: `/projects/${props.projectId}/settings`, query: "" };
  const snapshot = useProjectSnapshot({ context, auth: props.auth, load: (signal) => readProject({ projectId: props.projectId, signal }), onInaccessible: () => clearOperationInputs({ actor: props.session.user.id, projectId: props.projectId }) });
  const [draft, setDraft] = useState<DraftState | null>(null);
  const [pickerOpen, setPickerOpen] = useState(false);
  const [saved, setSaved] = useState<SavedState>({ kind: "loading", items: [] });
  const [savedCursor, setSavedCursor] = useState<string | null>(null);
  const [savedRevision, setSavedRevision] = useState(0);
  const [operation, setOperation] = useState<OperationState>({ kind: "idle" });
  const operationLocked = operation.kind === "sending" || operation.kind === "unknown";
  const operationLockedRef = useRef(operationLocked);
  operationLockedRef.current = operationLocked;
  const [fields, setFields] = useState<readonly ProjectFieldError[]>([]);
  const [conflictCurrent, setConflictCurrent] = useState<ProjectView | null>(null);
  const displayedRevision = draft?.base.projectRevision ?? "";
  const savedGate = useRequestGate({ ...context, route: `${context.route}/repositories`, query: `${displayedRevision}\u0000${savedCursor ?? ""}\u0000${savedRevision}` });
  const writeGate = useRequestGate({ ...context, route: `${context.route}/write`, query: "" });
  const conflictGate = useRequestGate({ ...context, route: `${context.route}/conflict`, query: "" });

  function clearInaccessible() {
    clearOperationInputs({ actor: props.session.user.id, projectId: props.projectId });
    setDraft(null);
    setConflictCurrent(null);
    setPickerOpen(false);
    setSavedCursor(null);
    setSaved({ kind: "loading", items: [] });
    setFields([]);
    setOperation({ kind: "inaccessible" });
  }

  useEffect(() => {
    const timer = window.setInterval(() => setSaved((current) => ({ ...current, items: current.items.filter((item) => {
      const observed = Date.parse(item.userParticipation.observedAt ?? "");
      return Number.isFinite(observed) && observed <= Date.now() && Date.now() - observed <= 60_000;
    }) })), 1000);
    return () => window.clearInterval(timer);
  }, []);

  useEffect(() => {
    if (snapshot.state.kind === "inaccessible") {
      setDraft(null);
      setPickerOpen(false);
      setSaved({ kind: "loading", items: [] });
      return;
    }
    if (snapshot.state.kind === "error" && snapshot.state.error.code === "PROJECT_UPDATE_NOT_ALLOWED") {
      setDraft((current) => current === null ? null : { ...current, base: { ...current.base, actions: { ...current.base.actions, canEdit: false } } });
      return;
    }
    if (snapshot.state.kind !== "ready" || operation.kind === "inaccessible") return;
    if (draft === null) setDraft({ base: snapshot.state.value, name: snapshot.state.value.name, purpose: snapshot.state.value.purpose, additions: new Map(), configuration: { kind: "preserve" } });
    else if (snapshot.state.value.projectRevision !== draft.base.projectRevision) setConflictCurrent(snapshot.state.value);
  }, [draft, operation.kind, snapshot.state]);

  useEffect(() => {
    if (draft === null || conflictCurrent !== null || snapshot.state.kind !== "ready" || snapshot.state.value.projectRevision !== draft.base.projectRevision) return;
    const ticket = savedGate.begin();
    setSaved((before) => ({ kind: "loading", items: savedCursor === null ? [] : before.items }));
    void readProjectRepositories({ projectId: props.projectId, cursor: savedCursor, signal: ticket.signal }).then((result) => {
      if (!savedGate.accepts(ticket)) return;
      if (result.kind === "error") {
        if (result.status === 401) {
          setDraft(null);
          setPickerOpen(false);
          setSaved({ kind: "loading", items: [] });
          props.auth.unauthorized(ticket.sessionGeneration);
          return;
        }
        if (result.code === "RESOURCE_NOT_FOUND") {
          clearInaccessible();
          return;
        }
        if (result.code === "PROJECT_UPDATE_NOT_ALLOWED") setDraft((current) => current === null ? null : { ...current, base: { ...current.base, actions: { ...current.base.actions, canEdit: false } } });
        if (result.code === "PROJECT_CONTEXT_CHANGED" || result.code === "INVALID_CURSOR" || result.code === "CURSOR_EXPIRED") {
          setSavedCursor(null);
          setSaved({ kind: "loading", items: [] });
          void snapshot.refresh();
          return;
        }
        setSaved((before) => ({ kind: "error", items: before.items, error: result }));
        return;
      }
      if (result.value.projectRevision !== draft.base.projectRevision) {
        setSavedCursor(null);
        setSaved({ kind: "loading", items: [] });
        void snapshot.refresh();
        return;
      }
      setSaved((before) => ({ kind: "ready", items: savedCursor === null ? result.value.items : [...before.items, ...result.value.items], page: result.value }));
    });
    return () => savedGate.invalidate();
  }, [conflictCurrent, displayedRevision, props.auth, props.projectId, props.session.user.id, savedCursor, savedGate, savedRevision, snapshot.refresh, snapshot.state]);

  function review() {
    if (draft === null || operationLockedRef.current) return;
    const result = updateInput(draft, Date.now());
    if (result.kind === "invalid") {
      setFields(result.fields);
      return;
    }
    if (result.kind === "authorization-unconfirmed") {
      setFields([{ field: "repositoryIdsToAdd", code: "AUTHORIZATION_UNCONFIRMED" }]);
      return;
    }
    if (result.kind === "unchanged") return;
    setFields([]);
    setOperation({ kind: "ready", preparation: prepareOperation({ actor: props.session.user.id, kind: "project_update", projectId: props.projectId, key: crypto.randomUUID(), input: result.input }) });
  }

  async function send(prepared: PreparedOperation) {
    if (prepared.kind !== "project_update" || operationLockedRef.current) return;
    const ticket = writeGate.begin();
    operationLockedRef.current = true;
    setOperation({ kind: "sending", operation: prepared });
    const response = await updateProject({ projectId: props.projectId, key: prepared.operation.key, input: prepared.operation.input, csrfToken: props.session.csrfToken, signal: ticket.signal });
    if (!writeGate.accepts(ticket)) return;
    const identity: UpdateIdentity = { actor: prepared.operation.actor, kind: "project_update", projectId: prepared.operation.projectId, key: prepared.operation.key };
    if (response.kind === "ok") {
      setOperation({ kind: "committed", result: { kind: "project_update", identity, receipt: response.value } });
      return;
    }
    if (response.status === 401) {
      clearAllOperationInputs();
      props.auth.unauthorized(ticket.sessionGeneration);
      setOperation({ kind: "inaccessible" });
      return;
    }
    if (response.code === "RESOURCE_NOT_FOUND") {
      clearInaccessible();
      return;
    }
    if (response.code === "PROJECT_UPDATE_NOT_ALLOWED") setDraft((current) => current === null ? null : { ...current, base: { ...current.base, actions: { ...current.base.actions, canEdit: false } } });
    const uncertain = response.status === 0 || response.status >= 500 || response.code === "INVALID_RESPONSE" || response.code === "AUTHORIZATION_UNCONFIRMED" || response.code === "RESULT_UNCONFIRMED" || response.code === "IDEMPOTENCY_CONFLICT" || response.code === "PROJECT_UPDATE_NOT_FOUND";
    setOperation(uncertain ? { kind: "unknown", operation: { kind: "project_update", identity, original: prepared.operation }, error: response } : { kind: "rejected", operation: prepared, error: response });
  }

  async function readConflict() {
    const ticket = conflictGate.begin();
    const response = await readProject({ projectId: props.projectId, signal: ticket.signal });
    if (!conflictGate.accepts(ticket)) return;
    if (response.kind === "ok") setConflictCurrent(response.value);
    else if (response.status === 401) props.auth.unauthorized(ticket.sessionGeneration);
    else if (response.code === "RESOURCE_NOT_FOUND") clearInaccessible();
  }

  if (operation.kind === "committed") return <ProjectShell {...props} title="项目已更新" projectId={props.projectId}><ProjectOperationResult {...props} result={operation.result} /></ProjectShell>;

  return <ProjectShell {...props} title="项目设置" projectId={props.projectId}>
    {snapshot.state.kind === "loading" && draft === null && <div className="empty-state" role="status"><span className="spinner" aria-hidden="true" /><p>正在读取项目设置…</p></div>}
    {snapshot.state.kind === "error" && draft === null && <div><p className="notice" role="alert">{projectErrorMessage(snapshot.state.error)}</p><button onClick={() => void snapshot.refresh()}>重新读取项目</button></div>}
    {snapshot.state.kind === "inaccessible" && <div className="empty-state"><p>当前无法访问该项目。</p><button onClick={() => props.navigate("/projects")}>返回项目列表</button></div>}
    {operation.kind === "inaccessible" && draft === null && snapshot.state.kind !== "inaccessible" && <div className="empty-state"><p>当前身份无法继续访问该项目，项目输入已清理。</p><button onClick={() => props.navigate("/projects")}>返回项目列表</button></div>}
    {draft && snapshot.state.kind !== "inaccessible" && <>
      <div className="page-heading"><div><p className="eyebrow">项目设置</p><h1>{draft.base.name}</h1><p className="muted">按项目资料、已保存仓库、配置引用的顺序核对。</p></div>{draft.base.actions.canEdit ? <span className="pill good">可编辑</span> : <span className="pill amber">只读</span>}</div>
      {!draft.base.actions.canEdit && <p className="notice">当前仍可查看项目外壳，但没有编辑资格。</p>}
      {conflictCurrent && operation.kind === "idle" && <div className="notice conflict"><h3>项目已有更新</h3><p>当前名称：{conflictCurrent.name}</p><p>我的名称：{draft.name}</p><p>当前用途：{conflictCurrent.purpose}</p><p>我的用途：{draft.purpose}</p><button onClick={() => { setDraft({ ...draft, base: conflictCurrent }); setConflictCurrent(null); setSavedCursor(null); setOperation({ kind: "idle" }); }}>以当前修订为基础，重新核对</button></div>}
      <section className="panel project-form"><h2>1. 项目资料</h2><label>项目名称<input disabled={!draft.base.actions.canEdit || operationLocked} value={draft.name} onChange={(event) => { if (operationLockedRef.current) return; setDraft({ ...draft, name: event.target.value }); setOperation({ kind: "idle" }); }} /></label><label>项目用途<textarea rows={6} disabled={!draft.base.actions.canEdit || operationLocked} value={draft.purpose} onChange={(event) => { if (operationLockedRef.current) return; setDraft({ ...draft, purpose: event.target.value }); setOperation({ kind: "idle" }); }} /></label></section>
      <section className="panel"><div className="section-heading"><div><h2>2. 已保存仓库</h2><p className="small muted">原范围只读。本次添加集合与原范围分开。</p></div>{draft.base.actions.canEdit && <button disabled={operationLocked} onClick={() => { if (!operationLockedRef.current) setPickerOpen((value) => !value); }}>{operationLocked || !pickerOpen ? "添加仓库" : "收起选择器"}</button>}</div>
        {saved.kind === "loading" && saved.items.length === 0 && <p role="status">正在核实已保存仓库…</p>}
        {saved.kind === "error" && <div><p className="notice">{projectErrorMessage(saved.error)}</p><button onClick={() => setSavedRevision((value) => value + 1)}>重新读取</button></div>}
        {saved.items.length > 0 && <ul className="repository-list">{saved.items.map((repository) => <li key={repository.id}><span className="repo-name"><strong>{repository.displayName}</strong><span className="small muted">已保存，只读</span></span></li>)}</ul>}
        {saved.kind === "ready" && saved.items.length === 0 && saved.page.restrictedRepositoryCount === 0 && saved.page.nextCursor === null && <p className="small muted">当前没有可展示的已保存仓库。</p>}
        {saved.kind === "ready" && saved.page.restrictedRepositoryCount > 0 && <p className="notice">另有 {saved.page.restrictedRepositoryCount} 个已保存仓库本次未披露。保存设置不会删除它们。</p>}
        {saved.kind === "ready" && saved.page.nextCursor !== null && <button className="load-more" onClick={() => setSavedCursor(saved.page.nextCursor)}>加载更多已保存仓库</button>}
        {pickerOpen && !operationLocked && <RepositoryPicker {...props} selection={draft.additions} onChange={(additions) => { if (operationLockedRef.current) return; setDraft({ ...draft, additions }); setOperation({ kind: "idle" }); }} onDone={() => { if (!operationLockedRef.current) setPickerOpen(false); }} maximumSelection={100} />}
      </section>
      <section className="panel"><h2>3. 配置引用</h2><label className="toggle-row"><input type="checkbox" disabled={!draft.base.actions.canEdit || operationLocked} checked={draft.configuration.kind === "replace"} onChange={(event) => { if (operationLockedRef.current) return; setDraft({ ...draft, configuration: event.target.checked ? { kind: "replace", value: { modelProfile: draft.base.configuration.modelProfile, executionProfile: draft.base.configuration.executionProfile } } : { kind: "preserve" } }); setOperation({ kind: "idle" }); }} /><span><strong>本次更新配置引用</strong><small>只有明确勾选才发送完整模型和执行引用，并在保存时重新解析两项。</small></span></label>{draft.configuration.kind === "replace" && <ConfigurationFields {...props} value={draft.configuration.value} onChange={(value) => { if (operationLockedRef.current) return; setDraft({ ...draft, configuration: { kind: "replace", value } }); setOperation({ kind: "idle" }); }} disabled={!draft.base.actions.canEdit || operationLocked} />}</section>
      {fields.map((field, index) => <p className="notice" key={`${field.field}:${index}`}>{field.code === "AUTHORIZATION_UNCONFIRMED" ? "待添加仓库的读取资格未知。请重新核实或撤回未核实项。" : localFieldMessage(field)}</p>)}
      {operation.kind === "idle" && <button className="primary compact save-review" disabled={!draft.base.actions.canEdit || updateInput(draft, Date.now()).kind === "unchanged"} onClick={review}>查看本次改动</button>}
      {operation.kind === "ready" && <div className="change-summary panel"><h2>本次改动摘要</h2><ul>{"name" in operation.preparation.prepared.operation.input && <li>更新项目名称</li>}{"purpose" in operation.preparation.prepared.operation.input && <li>更新项目用途</li>}{"repositoryIdsToAdd" in operation.preparation.prepared.operation.input && <li>明确添加 {operation.preparation.prepared.operation.input.repositoryIdsToAdd?.length ?? 0} 个仓库，原范围完整保留</li>}{"configuration" in operation.preparation.prepared.operation.input && <li>完整重新解析模型和执行配置引用</li>}</ul>{operation.preparation.kind === "link-required" && <div className="notice"><p>浏览器无法保存原输入。发送前请复制恢复链接。</p><input id="update-recovery-path" className="recovery-path" readOnly value={`${window.location.origin}${operation.preparation.recoveryPath}`} onFocus={(event) => event.currentTarget.select()} /><button type="button" onClick={() => { const input = document.getElementById("update-recovery-path"); if (input instanceof HTMLInputElement) { input.select(); void navigator.clipboard?.writeText(input.value).catch(() => undefined); } }}>复制恢复链接</button></div>}<div className="form-actions"><button onClick={() => setOperation({ kind: "idle" })}>返回修改</button><button className="primary compact" onClick={() => void send(operation.preparation.prepared)}>{operation.preparation.kind === "link-required" ? "已保存链接，继续发送" : "确认保存修改"}</button></div></div>}
      {operation.kind === "sending" && <div className="empty-state" role="status"><span className="spinner" aria-hidden="true" /><p>正在保存修改，请勿重复提交…</p></div>}
      {operation.kind === "unknown" && <div className="notice"><p>{operation.error ? projectErrorMessage(operation.error) : "保存结果仍待确认。"}</p><a href={projectRecoveryPath(operation.operation.identity)} onClick={(event) => { event.preventDefault(); props.navigate(projectRecoveryPath(operation.operation.identity)); }}>查询原更新结果</a><p className="small">结果未知期间不能修改原输入或生成新键。</p></div>}
      {operation.kind === "rejected" && <div className="notice"><p>{projectErrorMessage(operation.error)}</p>{operation.error.code === "PROJECT_REVISION_CONFLICT" ? <><button onClick={() => void readConflict()}>读取当前值进行比较</button>{conflictCurrent && <div className="conflict"><h3>修订冲突</h3><p>当前名称：{conflictCurrent.name}</p><p>我的名称：{draft.name}</p><p>当前用途：{conflictCurrent.purpose}</p><p>我的用途：{draft.purpose}</p><button onClick={() => { setDraft({ ...draft, base: conflictCurrent }); setConflictCurrent(null); setOperation({ kind: "idle" }); }}>以当前修订为基础，重新核对</button></div>}</> : <button onClick={() => setOperation({ kind: "idle" })}>修改后重新核对</button>}</div>}
      {operation.kind === "inaccessible" && <p className="notice">当前身份已失效，项目输入已清理。</p>}
    </>}
  </ProjectShell>;
}
