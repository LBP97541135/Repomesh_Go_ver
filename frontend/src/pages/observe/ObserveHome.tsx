import { useEffect } from "react";
import type { ObserveSection } from "../../routes";
import { observeWorkbenchURL } from "../../runtimeConfig";

/** Observation now opens the local workbench directly, including old deep links. */
export function ObserveHome({ section = null }: { section?: ObserveSection | null }) {
  let url = "";
  let error = "";
  try {
    url = observeWorkbenchURL(section === "trace" || section === "logs" ? "traces" : "overview");
  } catch (reason) {
    error = reason instanceof Error ? reason.message : "本地工作台地址无效";
  }
  useEffect(() => {
    if (url) window.location.replace(url);
  }, [url]);
  return (
    <section className="max-w-[720px] rounded-hard border border-line bg-panel p-6">
      <h1 className="text-[16px] font-semibold text-cream">本地观测工作台</h1>
      {error ? <p role="alert" className="mt-3 text-[12px] text-salmon">{error}</p> : <>
        <p className="mt-3 text-[12px] text-tx2">正在打开本机的 Trace、证据与评测工作台…</p>
        <a className="mt-4 inline-block text-[12px] text-amber hover:text-amber-hi" href={url}>直接打开工作台 →</a>
      </>}
    </section>
  );
}
