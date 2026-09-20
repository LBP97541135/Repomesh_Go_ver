import { useEffect, useState } from "react";
import type { ObserveSection } from "../../routes";
import { observeWorkbenchURL } from "../../runtimeConfig";

/** 观测面：**先探再跳**本机工作台（2026-09-20 全量走查修的）。
 *
 *  此前这里是无条件 `window.location.replace(url)`：本机工作台（默认
 *  `http://127.0.0.1:18090/`）没在跑时，浏览器直接落到自己的
 *  `ERR_CONNECTION_REFUSED` 错误页 —— 用户**连回退都没有**，只看到"无法访问此站点"。
 *
 *  改法与本机启动器那条同一套：先探测（`no-cors` 探活：连得上就 resolve 成 opaque
 *  响应，连不上就 reject），**连得上才跳**；连不上就留在控制台里如实说清楚 ——
 *  地址是什么、为什么连不上、怎么起、以及"控制台其余部分不受影响"。
 *
 *  这条不改产品取向（观测面到底该不该内嵌，仍由用户决定）：它只是把"跳到死页"
 *  换成"说清发生了什么"。 */
export function ObserveHome({ section = null }: { section?: ObserveSection | null }) {
  let url = "";
  let error = "";
  try {
    url = observeWorkbenchURL(section === "trace" || section === "logs" ? "traces" : "overview");
  } catch (reason) {
    error = reason instanceof Error ? reason.message : "本地工作台地址无效";
  }
  const [unreachable, setUnreachable] = useState(false);
  const [probing, setProbing] = useState(true);
  useEffect(() => {
    if (!url) {
      setProbing(false);
      return;
    }
    let cancelled = false;
    // no-cors 探活：不读响应内容，只要"连得上"这一个事实。
    fetch(url, { mode: "no-cors" })
      .then(() => {
        if (!cancelled) window.location.replace(url);
      })
      .catch(() => {
        if (!cancelled) {
          setUnreachable(true);
          setProbing(false);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [url]);
  return (
    <section className="max-w-[720px] rounded-hard border border-line bg-panel p-6">
      <h1 className="text-[16px] font-semibold text-cream">本地观测工作台</h1>
      {error ? (
        <p role="alert" className="mt-3 text-[12px] text-salmon">{error}</p>
      ) : unreachable ? (
        <>
          <p className="mt-3 text-[12px] leading-[1.8] text-tx2">
            本机观测工作台**没有应答**，所以没有跳过去 —— 跳过去只会看到浏览器的
            "无法访问此站点"。它是**操作者自己机器上的另一个进程**（默认监听
            <span className="font-mono"> {url.replace(/#.*$/, "")}</span>），
            没启动时这个地址上什么都没有。
          </p>
          <p className="mt-2 text-[12px] leading-[1.8] text-tx3">
            控制台的其余部分**不受影响**：项目、issue、交付、决策链、技能这些都在服务端，
            照常可用。要在这里看 Trace / 证据 / 评测，需要先把本机工作台起起来。
          </p>
          <a className="mt-4 inline-block text-[12px] text-amber hover:text-amber-hi" href={url}>
            我确定它已经在跑，直接打开 →
          </a>
        </>
      ) : (
        <>
          <p className="mt-3 text-[12px] text-tx2">
            {probing ? "正在探测本机工作台…" : "正在打开本机的 Trace、证据与评测工作台…"}
          </p>
          <a className="mt-4 inline-block text-[12px] text-amber hover:text-amber-hi" href={url}>
            直接打开工作台 →
          </a>
        </>
      )}
    </section>
  );
}
