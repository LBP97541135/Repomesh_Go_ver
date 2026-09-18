import { useEffect, useState } from "react";
import type { ApiError } from "./api";
import type { ProjectRecoveryProps } from "./projectPages";
import { createProject, projectErrorMessage, readProjectCreation, readProjectUpdate, updateProject } from "./projectApi";
import { clearAllOperationInputs, clearOperationInputs, loadOperation, operationStorageKey } from "./projectRecovery";
import type { OperationState, RecoverableOperation } from "./projectRecovery";
import { useRequestGate } from "./projectRequests";
import { ProjectOperationResult } from "./ProjectOperationResult";
import { ProjectShell } from "./ProjectShell";

function recoverable(props: ProjectRecoveryProps): RecoverableOperation {
  const original = loadOperation(props.operation);
  if (props.operation.kind === "project_create") return { kind: "project_create", identity: props.operation, original: original?.kind === "project_create" ? original : null };
  return { kind: "project_update", identity: props.operation, original: original?.kind === "project_update" ? original : null };
}

export function ProjectOperationPage(props: ProjectRecoveryProps) {
  const [state, setState] = useState<OperationState>(() => ({ kind: "unknown", operation: recoverable(props), error: null }));
  const [busy, setBusy] = useState(false);
  const projectId = props.operation.kind === "project_update" ? props.operation.projectId : "";
  const gate = useRequestGate({ actor: props.session.user.id, sessionGeneration: props.auth.generation, route: props.operation.kind === "project_create" ? `/project-creations/${props.operation.key}` : `/projects/${props.operation.projectId}/updates/${props.operation.key}`, query: "" });

  async function query() {
    setBusy(true);
    const ticket = gate.begin();
    const result = props.operation.kind === "project_create"
      ? await readProjectCreation({ key: props.operation.key, signal: ticket.signal })
      : await readProjectUpdate({ projectId: props.operation.projectId, key: props.operation.key, signal: ticket.signal });
    if (!gate.accepts(ticket)) return;
    setBusy(false);
    if (result.kind === "ok") {
      if (props.operation.kind === "project_create" && "projectCreationId" in result.value) setState({ kind: "committed", result: { kind: "project_create", identity: props.operation, receipt: result.value } });
      else if (props.operation.kind === "project_update" && "updateId" in result.value) setState({ kind: "committed", result: { kind: "project_update", identity: props.operation, receipt: result.value } });
      return;
    }
    if (result.status === 401) {
      clearAllOperationInputs();
      props.auth.unauthorized(ticket.sessionGeneration);
      setState({ kind: "inaccessible" });
      return;
    }
    if (result.code === "PROJECT_CREATION_RESULT_REMOVED" || result.code === "PROJECT_UPDATE_RESULT_REMOVED") {
      setState({ kind: "removed", identity: props.operation });
      return;
    }
    if (result.code === "RESOURCE_NOT_FOUND") {
      if (props.operation.kind === "project_update") clearOperationInputs({ actor: props.session.user.id, projectId: props.operation.projectId });
      else try { sessionStorage.removeItem(operationStorageKey(props.operation)); } catch { /* Browser storage may be unavailable. */ }
      setState({ kind: "inaccessible" });
      return;
    }
    setState({ kind: "unknown", operation: recoverable(props), error: result });
  }

  useEffect(() => { void query(); }, [props.auth.generation, props.operation.key, props.operation.kind, props.operation.kind === "project_update" ? props.operation.projectId : ""]);

  async function retryOriginal(operation: RecoverableOperation) {
    if (operation.original === null || busy) return;
    setBusy(true);
    const ticket = gate.begin();
    const result = operation.kind === "project_create"
      ? await createProject({ key: operation.identity.key, input: operation.original.input, csrfToken: props.session.csrfToken, signal: ticket.signal })
      : await updateProject({ projectId: operation.identity.projectId, key: operation.identity.key, input: operation.original.input, csrfToken: props.session.csrfToken, signal: ticket.signal });
    if (!gate.accepts(ticket)) return;
    setBusy(false);
    if (result.kind === "ok") {
      if (operation.kind === "project_create" && "projectCreationId" in result.value) setState({ kind: "committed", result: { kind: "project_create", identity: operation.identity, receipt: result.value } });
      else if (operation.kind === "project_update" && "updateId" in result.value) setState({ kind: "committed", result: { kind: "project_update", identity: operation.identity, receipt: result.value } });
      return;
    }
    if (result.status === 401) {
      clearAllOperationInputs();
      props.auth.unauthorized(ticket.sessionGeneration);
      setState({ kind: "inaccessible" });
      return;
    }
    if (result.code === "PROJECT_CREATION_RESULT_REMOVED" || result.code === "PROJECT_UPDATE_RESULT_REMOVED") {
      setState({ kind: "removed", identity: operation.identity });
      return;
    }
    if (result.code === "RESOURCE_NOT_FOUND") {
      if (operation.kind === "project_update") clearOperationInputs({ actor: props.session.user.id, projectId: operation.identity.projectId });
      else try { sessionStorage.removeItem(operationStorageKey(operation.identity)); } catch { /* Browser storage may be unavailable. */ }
      setState({ kind: "inaccessible" });
      return;
    }
    const error: ApiError = result;
    setState({ kind: "unknown", operation, error });
  }

  const destinationProjectId = projectId || undefined;
  if (state.kind === "committed") return <ProjectShell {...props} title="原操作结果" projectId={state.result.receipt.projectId}><ProjectOperationResult {...props} result={state.result} /></ProjectShell>;
  return <ProjectShell {...props} title="原操作结果" projectId={destinationProjectId}>
    <section className="panel operation-page">
      <p className="eyebrow">原操作恢复</p><h1>{props.operation.kind === "project_create" ? "项目创建结果" : "项目更新结果"}</h1>
      <div className="attempt-details"><span>原操作编号</span><code>{props.operation.key}</code></div>
      {state.kind === "unknown" && <><p className="notice">{state.error ? projectErrorMessage(state.error) : "正在查询原操作。404 只表示当前尚未查到，不会生成新键。"}</p><div className="form-actions"><button className="primary compact" disabled={busy} onClick={() => void query()}>{busy ? "正在查询…" : "查询原结果"}</button>{state.operation.original && state.error?.code !== "PROJECT_UPDATE_NOT_ALLOWED" && <button disabled={busy} onClick={() => void retryOriginal(state.operation)}>使用原键和原输入明确重试</button>}<button disabled={busy} onClick={() => props.navigate(destinationProjectId ? `/projects/${encodeURIComponent(destinationProjectId)}` : "/projects")}>稍后处理</button></div>{state.operation.original === null && <p className="small muted">原输入已丢失，本页只会查询，不会从当前项目或名称猜测并重发。</p>}</>}
      {state.kind === "removed" && <><p className="notice">原结果已清理，不能重新执行或用旧键重建。</p><button onClick={() => props.navigate(destinationProjectId ? `/projects/${encodeURIComponent(destinationProjectId)}` : "/projects")}>查看当前项目列表</button></>}
      {state.kind === "inaccessible" && <><p className="notice">当前身份无法访问这次操作。原回执和本地索引不授予读取权限。</p><button onClick={() => props.navigate("/login")}>重新确认登录</button></>}
    </section>
  </ProjectShell>;
}
