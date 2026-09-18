import { useEffect, useRef, useState } from "react";
import type { ApiError, Session } from "./api";
import { closeSave, getSave } from "./modelApi";
import type { SaveResult } from "./modelApi";
import { acceptsRead, beginRead, readSessionRecovery, reduceSave } from "./modelRecovery";
import type { OperationState, ReadTicket } from "./modelRecovery";
import { modelErrorMessage } from "./ModelSettingsPage";
import type { SessionController } from "./session";
import { ErrorNotice, Mark } from "./shared";

export function ModelSavePage({ session, auth, navigate, saveId }: { session: Session; auth: SessionController; navigate: (path: string) => void; saveId: string }) {
  const locator = { kind: "provider_save" as const, actor: session.user.id, id: saveId };
  const [state, setState] = useState<OperationState<SaveResult>>({ state: "checking", locator, generation: auth.generation });
  const ticket = useRef<ReadTicket>(beginRead(locator, auth.generation));
  const { currentGeneration, isCurrent, unauthorized } = auth;
  const snapshot = readSessionRecovery(session.user.id, saveId);

  useEffect(() => {
    const expected = currentGeneration();
    const currentTicket = beginRead(locator, expected);
    ticket.current = currentTicket;
    const controller = new AbortController();
    void getSave(saveId, controller.signal).then((response) => {
      if (controller.signal.aborted || !isCurrent(expected) || !acceptsRead(ticket.current, currentTicket, session.user.id)) return;
      if (response.kind === "error" && response.status === 401) unauthorized(expected);
      setState((current) => reduceSave(current, currentTicket, response));
    });
    return () => controller.abort();
  }, [saveId, currentGeneration, isCurrent, unauthorized, session.user.id]);

  async function onClose() {
    const expected = currentGeneration();
    const currentTicket = beginRead(locator, expected);
    ticket.current = currentTicket;
    const response = await closeSave({ actor: session.user.id, csrfToken: session.csrfToken }, saveId, new AbortController().signal);
    if (!isCurrent(expected) || !acceptsRead(ticket.current, currentTicket, session.user.id)) return;
    if (response.kind === "error" && response.status === 401) unauthorized(expected);
    setState((current) => reduceSave(current, currentTicket, response));
  }

  return <div className="workspace">
    <header className="workspace-header"><a className="brand" href="/" onClick={(event) => { event.preventDefault(); navigate("/"); }}><Mark /><span>RepoMesh</span></a><span className="header-divider">/</span><span className="muted">保存结果</span><button className="signout" onClick={() => void auth.logout()}>退出登录</button></header>
    <main className="workspace-main">
      <div className="page-heading"><div><p className="eyebrow">原保存</p><h1>查询这次模型保存</h1><p className="muted">未知时只查询原操作或明确终结。终结不会撤销已经保存的配置。</p></div></div>
      {snapshot && <p className="small muted">非秘密草稿仍在本会话：{snapshot.snapshot.name}</p>}
      {state.state === "checking" || state.state === "unconfirmed" || state.state === "in_flight" ? <p>正在查询原保存结果…</p> : null}
      {state.state === "confirmed" && state.result.outcome === "committed" && <p>已保存供应商 {state.result.providerId}，版本 {state.result.providerRevision}。</p>}
      {state.state === "confirmed" && state.result.outcome === "rejected" && <p className="notice" role="alert">{state.result.error.code}</p>}
      {state.state === "confirmed" && state.result.outcome === "closed_without_save" && <p>原操作已终结，未保存。</p>}
      {state.state === "removed" && <p>原记录已清理，不能复用这个保存键。</p>}
      {state.state === "reauthenticate" && <ErrorNotice error={{ kind: "error", status: 401, code: "AUTHENTICATION_REQUIRED", retryAt: null }} />}
      {(state.state === "checking" || state.state === "unconfirmed") && <div className="heading-actions">
        <button className="outlined" onClick={() => navigate(`/settings/model-saves/${saveId}`)}>继续查询</button>
        <button className="primary" onClick={() => { if (window.confirm("已经保存会返回原结果；尚未保存会阻止这次请求以后保存。它不会撤销已保存配置。")) void onClose(); }}>终结原保存</button>
        <button type="button" onClick={() => navigate("/settings/models")}>稍后处理</button>
      </div>}
      {state.state === "confirmed" && <button className="outlined" onClick={() => navigate("/settings/models")}>返回模型设置</button>}
    </main>
  </div>;
}

export function savePageError(error: ApiError): string {
  return modelErrorMessage(error);
}
