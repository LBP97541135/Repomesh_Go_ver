import { useRef, useState } from "react";
import { ConfigurationFields } from "./ConfigurationFields";
import type { AuthenticatedPageProps } from "./projectPages";
import { clearSelectionDisclosure, createInput } from "./projectDrafts";
import type { RepositorySelection } from "./projectDrafts";
import { createProject, projectErrorMessage } from "./projectApi";
import type { ConfigurationSelection, ProjectFieldError } from "./projectApi";
import { clearAllOperationInputs, operationStorageKey, prepareOperation, projectRecoveryPath } from "./projectRecovery";
import type { CreateIdentity, OperationState, PreparedOperation } from "./projectRecovery";
import { useRequestGate } from "./projectRequests";
import { ProjectOperationResult } from "./ProjectOperationResult";
import { ProjectShell } from "./ProjectShell";
import { RepositoryPicker } from "./RepositoryPicker";

const inheritedConfiguration: ConfigurationSelection = { modelProfile: { mode: "inherit" }, executionProfile: { mode: "inherit" } };

function fieldMessage(field: ProjectFieldError): string {
  if (field.field === "name") return field.code === "TOO_LONG" ? "项目名称最多 200 个 Unicode 字符。" : "请填写项目名称。";
  if (field.field === "purpose") return field.code === "TOO_LONG" ? "项目用途最多 20000 个 Unicode 字符。" : "请填写项目用途。";
  return field.code === "TOO_MANY" ? "一次最多选择 100 个仓库。" : "请至少选择一个仓库。";
}

export function CreateProjectPage(props: AuthenticatedPageProps) {
  const [step, setStep] = useState<"repositories" | "details">("repositories");
  const [selection, setSelection] = useState<RepositorySelection>(new Map());
  const [name, setName] = useState("");
  const [purpose, setPurpose] = useState("");
  const [configuration, setConfiguration] = useState<ConfigurationSelection>(inheritedConfiguration);
  const [fields, setFields] = useState<readonly ProjectFieldError[]>([]);
  const [operation, setOperation] = useState<OperationState>({ kind: "idle" });
  const operationLocked = operation.kind === "sending" || operation.kind === "unknown";
  const operationLockedRef = useRef(operationLocked);
  operationLockedRef.current = operationLocked;
  const writeGate = useRequestGate({ actor: props.session.user.id, sessionGeneration: props.auth.generation, route: "/projects/new", query: "" });

  function review() {
    if (operationLockedRef.current) return;
    const result = createInput({ name, purpose, selection, configuration }, Date.now());
    if (result.kind === "invalid") {
      setFields(result.fields);
      return;
    }
    if (result.kind === "authorization-unconfirmed") {
      setFields([{ field: "repositoryIds", code: "AUTHORIZATION_UNCONFIRMED" }]);
      return;
    }
    if (result.kind !== "valid") return;
    setFields([]);
    setOperation({ kind: "ready", preparation: prepareOperation({ actor: props.session.user.id, kind: "project_create", key: crypto.randomUUID(), input: result.input }) });
  }

  async function send(prepared: PreparedOperation) {
    if (prepared.kind !== "project_create" || operationLockedRef.current) return;
    const ticket = writeGate.begin();
    operationLockedRef.current = true;
    setOperation({ kind: "sending", operation: prepared });
    const response = await createProject({ key: prepared.operation.key, input: prepared.operation.input, csrfToken: props.session.csrfToken, signal: ticket.signal });
    if (!writeGate.accepts(ticket)) return;
    const identity: CreateIdentity = { actor: prepared.operation.actor, kind: "project_create", key: prepared.operation.key };
    if (response.kind === "ok") {
      setOperation({ kind: "committed", result: { kind: "project_create", identity, receipt: response.value } });
      return;
    }
    if (response.status === 401) {
      clearAllOperationInputs();
      props.auth.unauthorized(ticket.sessionGeneration);
      setOperation({ kind: "inaccessible" });
      return;
    }
    if (response.code === "RESOURCE_NOT_FOUND") {
      try { sessionStorage.removeItem(operationStorageKey(identity)); } catch { /* Browser storage may be unavailable. */ }
      setSelection(clearSelectionDisclosure(selection));
    }
    const uncertain = response.status === 0 || response.status >= 500 || response.code === "INVALID_RESPONSE" || response.code === "AUTHORIZATION_UNCONFIRMED" || response.code === "RESULT_UNCONFIRMED" || response.code === "IDEMPOTENCY_CONFLICT";
    setOperation(uncertain
      ? { kind: "unknown", operation: { kind: "project_create", identity, original: prepared.operation }, error: response }
      : { kind: "rejected", operation: prepared, error: response });
  }

  if (operation.kind === "committed") return <ProjectShell {...props} title="项目已保存" projectId={operation.result.receipt.projectId}><ProjectOperationResult {...props} result={operation.result} /></ProjectShell>;

  return <ProjectShell {...props} title="新建项目">
    <div className="page-heading"><div><p className="eyebrow">两步创建</p><h1>保存新项目</h1><p className="muted">先明确选择仓库，再填写项目资料和配置引用。保存不会创建 Issue 或启动运行。</p></div><span className="pill">{step === "repositories" ? "第 1 步，共 2 步" : "第 2 步，共 2 步"}</span></div>
    {step === "repositories" && !operationLocked && <RepositoryPicker {...props} selection={selection} onChange={(value) => { if (!operationLockedRef.current) setSelection(value); }} onDone={() => { if (operationLockedRef.current) return; if (selection.size > 0) setStep("details"); else setFields([{ field: "repositoryIds", code: "REQUIRED" }]); }} maximumSelection={100} />}
    {step === "repositories" && fields.map((field, index) => <p className="notice" role="alert" key={`${field.field}:${index}`}>{fieldMessage(field)}</p>)}
    {step === "details" && <section className="panel project-form" aria-labelledby="project-details-title">
      <div className="section-heading"><div><h2 id="project-details-title">项目资料</h2><p className="small muted">已选择 {selection.size} 个仓库。</p></div><button disabled={operationLocked} onClick={() => { if (!operationLockedRef.current) setStep("repositories"); }}>返回修改仓库</button></div>
      <label>项目名称<input disabled={operationLocked} value={name} onChange={(event) => { if (operationLockedRef.current) return; setName(event.target.value); setOperation({ kind: "idle" }); }} /></label>
      <label>项目用途<textarea disabled={operationLocked} value={purpose} rows={6} onChange={(event) => { if (operationLockedRef.current) return; setPurpose(event.target.value); setOperation({ kind: "idle" }); }} /></label>
      <section className="configuration-section"><h2>配置引用</h2><p className="small muted">模型和执行配置可以继承当前默认。缺少候选时仍可保存待配置项目。</p><ConfigurationFields {...props} value={configuration} onChange={(value) => { if (operationLockedRef.current) return; setConfiguration(value); setOperation({ kind: "idle" }); }} disabled={operationLocked} /></section>
      {fields.map((field, index) => <p className="notice" role="alert" key={`${field.field}:${index}`}>{field.code === "AUTHORIZATION_UNCONFIRMED" ? "所选仓库的读取资格已过期或未知。请返回选择器重新核实，或撤回未核实项。" : fieldMessage(field)}</p>)}
      {operation.kind === "idle" && <button className="primary compact" onClick={review}>查看保存摘要</button>}
      {operation.kind === "ready" && operation.preparation.prepared.kind === "project_create" && <div className="change-summary"><h2>保存摘要</h2><p>将保存项目资料、{operation.preparation.prepared.operation.input.repositoryIds.length} 个明确仓库及完整模型和执行配置引用。</p><p>配置未解析时项目保持待配置。不会建立 Issue、预算编辑或运行准备。</p>{operation.preparation.kind === "link-required" && <div className="notice"><p>浏览器无法可靠保存原输入。发送前请复制恢复链接；之后只能用原链接查询结果。</p><input id="create-recovery-path" className="recovery-path" readOnly value={`${window.location.origin}${operation.preparation.recoveryPath}`} onFocus={(event) => event.currentTarget.select()} /><button type="button" onClick={() => { const input = document.getElementById("create-recovery-path"); if (input instanceof HTMLInputElement) { input.select(); void navigator.clipboard?.writeText(input.value).catch(() => undefined); } }}>复制恢复链接</button></div>}<div className="form-actions"><button onClick={() => setOperation({ kind: "idle" })}>返回修改</button><button className="primary compact" onClick={() => void send(operation.preparation.prepared)}>{operation.preparation.kind === "link-required" ? "已保存链接，继续发送" : "确认保存项目"}</button></div></div>}
      {operation.kind === "sending" && <div className="empty-state" role="status"><span className="spinner" aria-hidden="true" /><p>正在保存项目，请勿重复提交…</p></div>}
      {operation.kind === "unknown" && operation.operation.kind === "project_create" && <div className="notice"><p>{operation.error ? projectErrorMessage(operation.error) : "提交结果仍待确认。"}</p><a href={projectRecoveryPath(operation.operation.identity)} onClick={(event) => { event.preventDefault(); props.navigate(projectRecoveryPath(operation.operation.identity)); }}>查询原创建结果</a><p className="small">不会生成新键或自动创建另一项目。</p></div>}
      {operation.kind === "rejected" && <div className="notice"><p>{projectErrorMessage(operation.error)}</p><button onClick={() => setOperation({ kind: "idle" })}>修改后重新核对</button></div>}
      {operation.kind === "inaccessible" && <p className="notice">当前身份已失效，项目输入已清理。</p>}
    </section>}
  </ProjectShell>;
}
