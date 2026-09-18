import { useEffect, useState } from "react";
import type { FormEvent } from "react";
import type { ApiError, Session } from "./api";
import { errorMessage } from "./api";
import { listProviders, saveProvider } from "./modelApi";
import type { ModelInput, ProviderPage, SaveBody } from "./modelApi";
import { persistRecoveryIndex, persistSessionRecovery, recoveryPath, shouldOpenSaveRecovery } from "./modelRecovery";
import type { SessionController } from "./session";
import { ErrorNotice, Mark } from "./shared";

type PageState = { kind: "loading" } | { kind: "ready"; page: ProviderPage } | { kind: "unavailable"; error: ApiError };

function newId(): string {
  const bytes = new Uint8Array(16);
  crypto.getRandomValues(bytes);
  bytes[6] = bytes[6] & 15 | 64;
  bytes[8] = bytes[8] & 63 | 128;
  const hex = Array.from(bytes, (value) => value.toString(16).padStart(2, "0")).join("");
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

function emptyModel(): ModelInput {
  return { id: null, modelId: "", displayName: null, contextWindow: 128000, maxOutputTokens: 4096, reasoning: false, vision: false };
}

export function ModelSettingsPage({ session, auth, navigate }: { session: Session; auth: SessionController; navigate: (path: string) => void }) {
  const [page, setPage] = useState<PageState>({ kind: "loading" });
  const [name, setName] = useState("");
  const [baseUrl, setBaseUrl] = useState("");
  const [secret, setSecret] = useState("");
  const [models, setModels] = useState<ModelInput[]>([emptyModel()]);
  const [sending, setSending] = useState(false);
  const [saveError, setSaveError] = useState<ApiError | null>(null);
  const { currentGeneration, isCurrent, unauthorized } = auth;

  useEffect(() => {
    const expected = currentGeneration();
    const controller = new AbortController();
    void listProviders({}, controller.signal).then((response) => {
      if (controller.signal.aborted || !isCurrent(expected)) return;
      if (response.kind === "error") {
        if (response.status === 401) unauthorized(expected);
        setPage({ kind: "unavailable", error: response });
      } else setPage({ kind: "ready", page: response.value });
    });
    return () => controller.abort();
  }, [currentGeneration, isCurrent, unauthorized]);

  async function onSave(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (sending) return;
    const saveId = newId();
    const body: SaveBody = {
      providerId: null, expectedRevision: null, name, baseUrl, apiFormat: "openai_chat_completions",
      models, secret: { mode: "replace", value: secret },
    };
    persistRecoveryIndex({ kind: "provider_save", actor: session.user.id, id: saveId, createdAt: new Date().toISOString() });
    persistSessionRecovery({
      locator: { kind: "provider_save", actor: session.user.id, id: saveId },
      snapshot: { providerId: null, expectedRevision: null, name, baseUrl, apiFormat: "openai_chat_completions", models, secretMode: "replace" },
    });
    setSecret("");
    setSaveError(null);
    setSending(true);
    const expected = currentGeneration();
    const response = await saveProvider({ actor: session.user.id, csrfToken: session.csrfToken }, saveId, body, new AbortController().signal);
    body.secret = { mode: "replace", value: "" };
    if (!isCurrent(expected)) return;
    if (response.kind === "error" && response.status === 401) {
      unauthorized(expected);
      setSending(false);
      return;
    }
    if (shouldOpenSaveRecovery(response)) {
      navigate(recoveryPath(saveId));
      return;
    }
    if (response.kind === "error") setSaveError(response);
    setSending(false);
  }

  return <div className="workspace">
    <header className="workspace-header"><a className="brand" href="/" onClick={(event) => { event.preventDefault(); navigate("/"); }}><Mark /><span>RepoMesh</span></a><span className="header-divider">/</span><span className="muted">模型设置</span><button className="signout" onClick={() => void auth.logout()}>退出登录</button></header>
    <main className="workspace-main">
      <div className="page-heading"><div><p className="eyebrow">供应商</p><h1>模型连接</h1><p className="muted">保存自己的供应商完整快照。Key 发出后立即从本页清除，不会写入浏览器存储。</p></div></div>
      {page.kind === "loading" && <p>正在读取供应商…</p>}
      {page.kind === "unavailable" && <ErrorNotice error={page.error} />}
      {page.kind === "ready" && page.page.items.length === 0 && <p className="muted">还没有已保存的供应商。</p>}
      {page.kind === "ready" && page.page.items.length > 0 && <ul className="repository-list">{page.page.items.map((item) => <li key={item.id}><strong>{item.name}</strong><span className="small muted">{item.modelCount} 个模型</span></li>)}</ul>}
      <form className="stack" onSubmit={(event) => void onSave(event)}>
        <label>名称<input value={name} onChange={(event) => setName(event.target.value)} required maxLength={100} /></label>
        <label>Base URL<input value={baseUrl} onChange={(event) => setBaseUrl(event.target.value)} required maxLength={2048} placeholder="https://gateway.example.invalid/v1" /></label>
        <label>API Key<input type="password" value={secret} onChange={(event) => setSecret(event.target.value)} required autoComplete="off" /></label>
        {models.map((model, index) => <fieldset key={index}>
          <legend>模型 {index + 1}</legend>
          <label>modelId<input value={model.modelId} onChange={(event) => setModels((current) => current.map((item, itemIndex) => itemIndex === index ? { ...item, modelId: event.target.value } : item))} required /></label>
          <label>显示名<input value={model.displayName ?? ""} onChange={(event) => setModels((current) => current.map((item, itemIndex) => itemIndex === index ? { ...item, displayName: event.target.value || null } : item))} /></label>
        </fieldset>)}
        <button type="button" className="outlined" onClick={() => setModels((current) => current.length >= 50 ? current : [...current, emptyModel()])}>增加模型</button>
        {saveError && <p className="notice" role="alert">{modelErrorMessage(saveError)}</p>}
        <p className="small muted">保存后会打开原操作页。即使存储被禁用，也请复制该链接。校验未通过时留在本页，密钥需要重新输入。</p>
        <button className="primary" type="submit" disabled={sending}>保存供应商</button>
      </form>
    </main>
  </div>;
}

export function modelErrorMessage(error: ApiError): string {
  switch (error.code) {
    case "MODEL_SAVE_NOT_FOUND": return "还看不到这次保存结果。请继续查询原操作，或明确终结。";
    case "MODEL_SAVE_CLOSED": return "原保存已终结，未写入新配置。请用新的保存操作继续。";
    case "MODEL_SAVE_RESULT_REMOVED": return "这次保存结果已清理，不能复用原操作。";
    case "IDEMPOTENCY_CONFLICT": return "原保存与这次输入不一致，已保留原结果。";
    case "VALIDATION_FAILED": return "这次保存没有受理。请检查名称、地址和模型后重试。密钥需要重新输入。";
    default: return errorMessage(error);
  }
}
