import { useEffect, useState } from "react";
import { readObservationModel, saveObservationModel, testObservationModel, type ObservationModelConfig, type ObservationPurpose } from "../api/observationModels";
import { ApiError } from "../api/http";
import { errText } from "../display";
import { CredentialField, credentialInputClass } from "./setup/CredentialField";

const purposes = {
  jev: { title: "Jev · Rubric 评分", name: "Jev", model: "jev-1.13.0", endpoint: "https://api.typesafe.ai/v1/systemone", description: "契约、产物与证据质量评分。分数不代替业务验收。" },
  deepseek: { title: "DeepSeek · 观测分析", name: "DeepSeek", model: "deepseek-flash", endpoint: "https://api.deepseek.com/chat/completions", description: "样本聚类、失败归因和 AI 初标。人工确认单独保留。" },
};
const buttonClass = "rounded-hard border border-line px-3 py-2 text-[12px] text-tx2 hover:border-amber hover:text-amber-hi disabled:opacity-50";

function settingsError(reason: unknown): string {
  if (reason instanceof ApiError) {
    if (reason.status === 403) return "需要管理员权限及有效会话才能维护这台机器的观测模型。";
    if (reason.status === 503) return "本地观测工作台暂不可用，配置没有得到确认。启动工作台后点击重新读取。";
    if (reason.status === 502) return "模型服务连接或响应失败。请检查已保存的 Key 与网络后重试。";
    if (reason.status === 400 || reason.status === 409) return "配置未被接受。请检查模型名，并在首次配置或更换供应商时填写对应的 API Key。";
  }
  return errText(reason);
}

function ObservationModelForm({ purpose }: { purpose: ObservationPurpose }) {
  const info = purposes[purpose];
  const [saved, setSaved] = useState<ObservationModelConfig | null>(null);
  const [model, setModel] = useState(info.model);
  const [apiKey, setApiKey] = useState("");
  const [available, setAvailable] = useState<string[]>([]);
  const [busy, setBusy] = useState<"load" | "save" | "test" | null>("load");
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");

  useEffect(() => {
    let active = true;
    readObservationModel(purpose).then(value => {
      if (!active) return;
      setSaved(value);
      setModel(value.model.startsWith(purpose === "jev" ? "jev-" : "deepseek-") ? value.model : info.model);
    }).catch(reason => { if (active) setError(settingsError(reason)); })
      .finally(() => { if (active) setBusy(null); });
    return () => { active = false; };
  }, [purpose, info.model]);

  const dirty = !!apiKey || (!!saved && model.trim() !== saved.model);
  async function reload() {
    setBusy("load"); setError(""); setMessage(""); setAvailable([]); setApiKey("");
    try { const value = await readObservationModel(purpose); setSaved(value); setModel(value.model.startsWith(purpose === "jev" ? "jev-" : "deepseek-") ? value.model : info.model); }
    catch (reason) { setSaved(null); setError(settingsError(reason)); }
    finally { setBusy(null); }
  }
  async function save(event: React.FormEvent) {
    event.preventDefault(); setBusy("save"); setError(""); setMessage("");
    try {
      const value = await saveObservationModel(purpose, model.trim(), apiKey.trim());
      setSaved(value); setModel(value.model); setApiKey(""); setAvailable([]);
      setMessage("配置已保存，下一次评分或分析使用此模型。连接尚未测试。");
    } catch (reason) { setError(settingsError(reason)); }
    finally { setBusy(null); }
  }
  async function test() {
    setBusy("test"); setError(""); setMessage("");
    try {
      const result = await testObservationModel(purpose);
      setAvailable(result.models);
      setMessage(result.ok ? result.message : "Key 连接验证成功，但所选模型不在返回列表中。请从模型输入框选择可用模型并保存。");
    } catch (reason) { setAvailable([]); setError(settingsError(reason)); }
    finally { setBusy(null); }
  }

  return <form onSubmit={save} aria-label={info.title} className="rounded-hard border border-line bg-panel p-5">
    <div className="flex flex-wrap items-center justify-between gap-2">
      <h3 className="text-[13px] font-semibold text-cream">{info.title}</h3>
      <span className="text-[11px] text-tx3">{busy === "load" ? "读取中…" : !saved ? "配置状态未知" : saved.configured ? `已保存 · ${saved.model}` : "未配置 Key"}</span>
    </div>
    <p className="mt-2 text-[11.5px] leading-relaxed text-tx2">{info.description}</p>
    <fieldset disabled={busy !== null || saved === null}>
      <CredentialField mode="editable" label={`${info.name} 模型`} note="可输入模型名；读取可用模型后，可从输入框的候选列表选择。">
        <input aria-label={`${info.name} 模型`} className={credentialInputClass} value={model} onChange={e => { setModel(e.target.value); setMessage(""); }} list={`${purpose}-available-models`} required autoComplete="off" maxLength={72} />
        <datalist id={`${purpose}-available-models`}>{[...new Set([info.model, ...(saved?.model ? [saved.model] : []), ...available])].map(name => <option key={name} value={name} />)}</datalist>
      </CredentialField>
      <CredentialField mode="editable" label={`${info.name} API Key`} note={saved?.configured ? "留空保留已保存的 Key；填写新值会替换。页面不会读取明文 Key。" : "首次配置请填写对应供应商的 API Key。"}>
        <input aria-label={`${info.name} API Key`} className={credentialInputClass} type="password" autoComplete="new-password" value={apiKey} onChange={e => { setApiKey(e.target.value); setMessage(""); }} required={!saved?.configured} maxLength={4096} />
      </CredentialField>
      <p className="mt-2 break-all text-[10.5px] text-tx3">API：{saved?.endpoint || info.endpoint}</p>
      {dirty && <p className="mt-2 text-[11px] text-amber">有未保存修改；连接测试使用已保存配置。</p>}
      <div className="mt-4 flex flex-wrap gap-2">
        <button type="submit" className="rounded-hard bg-amber px-4 py-2 text-[12px] font-bold text-paper-ink disabled:opacity-50">{busy === "save" ? "保存中…" : `保存 ${info.name} 配置`}</button>
        <button type="button" className={buttonClass} disabled={!saved?.configured || dirty} onClick={() => void test()}>{busy === "test" ? "读取中…" : "测试连接 / 读取可用模型"}</button>
      </div>
    </fieldset>
    <button type="button" className={`${buttonClass} mt-3`} disabled={busy !== null} onClick={() => void reload()}>重新读取配置</button>
    {available.length > 0 && <p className="mt-3 break-words text-[11px] text-tx2">供应商返回：{available.join("、")}</p>}
    {error && <p role="alert" className="mt-3 text-[11.5px] text-salmon">{error}</p>}
    {message && <p role="status" className="mt-3 text-[11.5px] text-olive">{message}</p>}
  </form>;
}

export function ObservationModelSettings({ isAdmin }: { isAdmin: boolean }) {
  if (!isAdmin) return <p className="mt-4 text-[12px] text-tx2">Jev 评分与 DeepSeek 观测分析使用这台机器的共享配置，由管理员在此维护。</p>;
  return <div className="mt-5 grid gap-4"><ObservationModelForm purpose="jev" /><ObservationModelForm purpose="deepseek" /></div>;
}
