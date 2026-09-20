import { useEffect, useRef, useState } from "react";
import { checkTypeSafeConnection, fetchTypeSafeSettings, saveTypeSafeSettings, typeSafeMessage, type TypeSafeSettingsView } from "../api/typesafe";

export function TypeSafeSettings({ projectId, projectName }: { projectId: string | null; projectName?: string }) {
  return (
    <section className="mt-5 rounded-hard border border-line bg-panel p-5" aria-label="TypeSafe / Jev 配置">
      <h3 className="text-[13px] font-semibold text-cream">测试与代码审查 · TypeSafe / Jev</h3>
      {projectId ? <ProjectSettings key={projectId} projectId={projectId} projectName={projectName ?? projectId} /> :
        <p className="mt-2 text-[12px] text-tx2">请先选择项目，再配置它的 Jev Key 和启用开关。</p>}
    </section>
  );
}

function ProjectSettings({ projectId, projectName }: { projectId: string; projectName: string }) {
  const [view, setView] = useState<TypeSafeSettingsView | null>(null);
  const [enabled, setEnabled] = useState(false);
  const [key, setKey] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [reload, setReload] = useState(0);
  const mounted = useRef(false);
  useEffect(() => {
    mounted.current = true;
    let cancelled = false;
    fetchTypeSafeSettings(projectId).then(next => {
      if (!cancelled) { setView(next); setEnabled(next.enabled); setError(""); }
    }).catch(err => { if (!cancelled) setError(typeSafeMessage(err)); });
    return () => { cancelled = true; mounted.current = false; };
  }, [projectId, reload]);

  const act = async (action: "save" | "clear" | "check") => {
    if (!view || busy) return;
    setBusy(true); setError(""); setNotice("");
    try {
      const next = action === "check"
        ? await checkTypeSafeConnection(projectId, view.revision)
        : await saveTypeSafeSettings(projectId, {
          expectedRevision: view.revision, enabled: action === "clear" ? false : enabled, model: view.model,
          secret: action === "clear" ? { mode: "clear" } : key.trim() ? { mode: "replace", value: key.trim() } : { mode: "keep" },
        });
      if (mounted.current) {
        setView(next); setEnabled(next.enabled);
        setNotice(action === "check" ? typeSafeMessage(next.checkStatus) : action === "clear" ? "Key 已清除，Jev 已关闭。" : "配置已保存；后续运行按此开关调用 Jev。");
      }
    } catch (err) {
      if (mounted.current) setError(typeSafeMessage(err));
    } finally {
      if (mounted.current) { setKey(""); setBusy(false); }
    }
  };
  const button = "rounded-hard border border-line px-3 py-1.5 text-[12px] text-tx2 hover:border-amber disabled:opacity-50";
  return (
    <div className="mt-3 grid gap-3 text-[12px]">
      <p className="text-tx2">当前项目：{projectName}</p>
      <label className="flex items-center gap-2 text-cream">
        <input type="checkbox" checked={enabled} disabled={!view || busy} onChange={e => setEnabled(e.target.checked)} />
        启用 Jev 辅助测试与代码审查
      </label>
      <p className="text-[11.5px] leading-relaxed text-tx2">开启后，测试团队核对测试证据，新任务增加一次代码审查，供仓库负责人验收参考。将向 TypeSafe 发送所选证据文本；辅助判断不会自动批准任务或合并代码。</p>
      <label className="grid gap-1.5 text-tx2">TypeSafe API Key
        <input type="password" autoComplete="new-password" spellCheck={false} value={key} disabled={busy || !view}
          onChange={e => setKey(e.target.value)} placeholder={view?.configured ? "已保存；输入新 Key 可替换" : "输入 TypeSafe API Key"}
          className="w-full rounded-hard border border-line bg-well px-3 py-2 text-tx outline-none focus:border-amber" />
      </label>
      {view && <p className="text-[11px] text-tx3">{view.configured ? "Key 已加密保存" : "尚未保存 Key"} · {view.model} · {typeSafeMessage(view.checkStatus)}</p>}
      <div className="flex flex-wrap gap-2">
        <button className={button} disabled={!view || busy || (enabled && !view.configured && !key.trim())} onClick={() => void act("save")}>{busy ? "处理中…" : "保存配置"}</button>
        <button className={button} disabled={!view?.configured || busy || !!key} onClick={() => void act("check")}>检查连接</button>
        <button className={button} disabled={!view?.configured || busy} onClick={() => void act("clear")}>清除 Key 并关闭</button>
        <button className={button} disabled={busy} onClick={() => { setKey(""); setNotice(""); setReload(n => n + 1); }}>重新读取</button>
      </div>
      <p className="text-[11px] text-tx3">检查连接会使用已保存的 Key 调用一次合成样本推理。每条运行最多 8 次调用；关闭后停止新调用并保留历史记录。</p>
      {!view && !error && <p className="text-tx3">读取配置中…</p>}
      {notice && <p role="status" className="text-tx2">{notice}</p>}
      {error && <p role="alert" className="text-salmon">{error}</p>}
      {view && <details className="text-[11px] text-tx3"><summary>Skill 来源</summary><p className="mt-1 break-all">typesafe-ai/skills · {view.skillCommit}</p><p className="break-all">内容摘要：{view.skillHash}</p></details>}
    </div>
  );
}
