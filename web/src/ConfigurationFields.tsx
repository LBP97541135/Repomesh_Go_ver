import { useEffect, useState } from "react";
import type { ApiError } from "./api";
import type { ConfigurationFieldsProps } from "./projectPages";
import { projectErrorMessage, readConfigurationProfiles } from "./projectApi";
import type { ProfileKind, ProfilePage, ProfileSelection } from "./projectApi";
import { useRequestGate } from "./projectRequests";

type ProfilesState = { kind: "loading"; items: ProfilePage["items"] } | { kind: "ready"; page: ProfilePage; items: ProfilePage["items"] } | { kind: "error"; error: ApiError; items: ProfilePage["items"] };

export function ConfigurationFields({ session, auth, value, onChange, disabled }: ConfigurationFieldsProps) {
  const [model, setModel] = useState<ProfilesState>({ kind: "loading", items: [] });
  const [execution, setExecution] = useState<ProfilesState>({ kind: "loading", items: [] });
  const [modelCursor, setModelCursor] = useState<string | null>(null);
  const [executionCursor, setExecutionCursor] = useState<string | null>(null);
  const [revision, setRevision] = useState(0);
  const modelGate = useRequestGate({ actor: session.user.id, sessionGeneration: auth.generation, route: window.location.pathname, query: `model:${modelCursor ?? ""}:${revision}` });
  const executionGate = useRequestGate({ actor: session.user.id, sessionGeneration: auth.generation, route: window.location.pathname, query: `execution:${executionCursor ?? ""}:${revision}` });

  useEffect(() => {
    const ticket = modelGate.begin();
    setModel((before) => ({ kind: "loading", items: modelCursor === null ? [] : before.items }));
    void readConfigurationProfiles({ kind: "model", cursor: modelCursor, signal: ticket.signal }).then((result) => {
      if (!modelGate.accepts(ticket)) return;
      if (result.kind === "error") {
        if (result.status === 401) {
          setModel({ kind: "loading", items: [] });
          auth.unauthorized(ticket.sessionGeneration);
          return;
        }
        if ((result.code === "INVALID_CURSOR" || result.code === "CURSOR_EXPIRED") && modelCursor !== null) {
          setModel({ kind: "loading", items: [] });
          setModelCursor(null);
          return;
        }
        setModel((before) => ({ kind: "error", error: result, items: before.items }));
      } else setModel((before) => ({ kind: "ready", page: result.value, items: modelCursor === null ? result.value.items : [...before.items, ...result.value.items] }));
    });
    return () => modelGate.invalidate();
  }, [auth, modelCursor, modelGate, revision]);

  useEffect(() => {
    const ticket = executionGate.begin();
    setExecution((before) => ({ kind: "loading", items: executionCursor === null ? [] : before.items }));
    void readConfigurationProfiles({ kind: "execution", cursor: executionCursor, signal: ticket.signal }).then((result) => {
      if (!executionGate.accepts(ticket)) return;
      if (result.kind === "error") {
        if (result.status === 401) {
          setExecution({ kind: "loading", items: [] });
          auth.unauthorized(ticket.sessionGeneration);
          return;
        }
        if ((result.code === "INVALID_CURSOR" || result.code === "CURSOR_EXPIRED") && executionCursor !== null) {
          setExecution({ kind: "loading", items: [] });
          setExecutionCursor(null);
          return;
        }
        setExecution((before) => ({ kind: "error", error: result, items: before.items }));
      } else setExecution((before) => ({ kind: "ready", page: result.value, items: executionCursor === null ? result.value.items : [...before.items, ...result.value.items] }));
    });
    return () => executionGate.invalidate();
  }, [auth, executionCursor, executionGate, revision]);

  function replace(kind: ProfileKind, selection: ProfileSelection) {
    onChange(kind === "model" ? { ...value, modelProfile: selection } : { ...value, executionProfile: selection });
  }

  const fields: { kind: ProfileKind; label: string; state: ProfilesState; selection: ProfileSelection; loadMore: () => void }[] = [
    { kind: "model", label: "模型配置", state: model, selection: value.modelProfile, loadMore: () => { if (model.kind === "ready") setModelCursor(model.page.nextCursor); } },
    { kind: "execution", label: "执行配置", state: execution, selection: value.executionProfile, loadMore: () => { if (execution.kind === "ready") setExecutionCursor(execution.page.nextCursor); } },
  ];

  return <div className="configuration-fields">{fields.map((field) => <fieldset key={field.kind} disabled={disabled}>
    <legend>{field.label}</legend>
    <label className="choice-row"><input type="radio" name={`${field.kind}-profile`} checked={field.selection.mode === "inherit"} onChange={() => replace(field.kind, { mode: "inherit" })} /><span>继承当前可用默认配置</span></label>
    {field.state.items.map((profile) => <label className="choice-row" key={profile.id}><input type="radio" name={`${field.kind}-profile`} checked={field.selection.mode === "reference" && field.selection.id === profile.id} onChange={() => replace(field.kind, { mode: "reference", id: profile.id })} /><span><strong>{profile.name}</strong><small>{profile.availability.status === "allowed" ? "当前可引用" : profile.availability.status === "denied" ? "已知配置暂不可用，可保存为待修复引用" : "配置可用性待确认"}</small></span></label>)}
    {field.state.kind === "loading" && field.state.items.length === 0 && <p className="small muted" role="status">正在读取{field.label}候选…</p>}
    {field.state.kind === "ready" && field.state.items.length === 0 && <p className="small muted">当前没有可引用的{field.label}。继承项仍可保存为待配置。</p>}
    {field.state.kind === "error" && <div><p className="notice" role="alert">{projectErrorMessage(field.state.error)}</p><button type="button" onClick={() => setRevision((before) => before + 1)}>重试读取{field.label}</button></div>}
    {field.state.kind === "ready" && field.state.page.nextCursor !== null && <button type="button" onClick={field.loadMore}>加载更多{field.label}</button>}
  </fieldset>)}</div>;
}
