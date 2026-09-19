import { useEffect, useState } from "react";
import { AlertPanel } from "../../components/AlertPanel";
import { fetchDiscoveryStalls, type DiscoveryStall } from "../../api/discoveryStalls";
import { resolveProjectId } from "../../api/issues";
import { ObserveCrumb } from "./ObserveCrumb";

/** 观测 · 告警（#/observe/alerts）。
 *
 * 「在线监控与告警」场景的落地面：阈值规则管理 + 触发历史。数据源
 * `observability.alert_rules` + `alert_events`，规则按尾随窗口评估 llm_usage
 * 聚合指标。本页复用 AlertPanel（30s 轮询）；正在 firing 的告警横幅在门户页
 * 全局可见，不在此重复渲染。 */


/** 流程卡点：发现链域自报的卡点（失败步带原因原文 / 疑似中断）。
 *  恢复动作一律走各域既有端点（重试=发现链触发），本读面只发现不执行；
 *  「去处理」带着 pending-entry 手递手跳到该 issue 的 Manager 房间。 */
function DiscoveryStalls() {
  const [stalls, setStalls] = useState<DiscoveryStall[] | null>(null);
  useEffect(() => {
    let cancelled = false;
    const tick = () =>
      resolveProjectId()
        .then((pid) => (pid ? fetchDiscoveryStalls(pid) : []))
        .then((items) => !cancelled && setStalls(items))
        .catch(() => {
          if (!cancelled) setStalls([]);
        });
    tick();
    const timer = window.setInterval(tick, 30000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, []);
  const goManager = (issueId: string) => {
    try {
      window.sessionStorage.setItem(`repomesh.pending-entry.${issueId}`, "mgr");
    } catch {
      /* 存不进就直接进工作台，少一步自动开房 */
    }
    window.location.hash = `#/issues/${issueId}`;
  };
  return (
    <section className="mt-6">
      <h2 className="text-[13px] font-bold text-cream">流程卡点</h2>
      <p className="mt-1 text-[11.5px] leading-relaxed text-tx3">
        发现链域自报的卡点：失败步带原因原文，疑似中断=分档已批而计划迟迟未生成。
        点「去处理」跳到该 issue 的 Manager 房间，重试长在卡点卡上。
      </p>
      <div className="mt-3 flex flex-col gap-2">
        {stalls === null && <p className="text-[11.5px] text-tx3">读取中…</p>}
        {stalls !== null && stalls.length === 0 && (
          <p className="text-[11.5px] text-tx3">当前没有卡点——各发现链均在正常推进或等人审。</p>
        )}
        {stalls?.map((s) => (
          <div key={`${s.issueId}-${s.step}-${s.kind}`} className="flex items-center gap-3 rounded-hard border border-line bg-panel px-3.5 py-2.5">
            <span className={`flex-none rounded-hard border px-1.5 py-px font-mono text-[9.5px] ${s.kind === "failed" ? "border-salmon/50 bg-salmon-well text-salmon" : "border-amber/40 bg-amber-well text-amber"}`}>
              {s.kind === "failed" ? "失败" : "疑似中断"}
            </span>
            <div className="min-w-0 flex-1">
              <p className="truncate text-[12px] text-tx">
                <span className="font-mono text-[11px] text-tx2">TT-{String(s.issueNumber).padStart(3, "0")}</span>
                {" "}· {["", "需求分析", "候选评分", "分档审批", "生成计划", "物化确认"][s.step]}
                <span className="text-tx3"> — {s.title}</span>
              </p>
              <p className="truncate text-[11px] text-tx2" title={s.message}>{s.message}</p>
            </div>
            <button
              type="button"
              className="flex-none rounded-hard border border-line-strong px-2.5 py-1 text-[11px] text-tx hover:border-amber hover:text-amber-hi"
              onClick={() => goManager(s.issueId)}
              title="跳到该 issue 的工作台并打开 Manager 房间"
            >
              去处理 → Manager 房间
            </button>
          </div>
        ))}
      </div>
    </section>
  );
}

export function ObserveAlerts() {
  return (
    <div className="max-w-[860px]">
      <ObserveCrumb section="告警" />
      <p className="mt-4 text-[11.5px] leading-relaxed text-tx3">
        在线监控与告警：规则按尾随窗口评估 LLM 用量聚合指标（成功率 / 错误数 /
        P95 延迟 / 成本 / 调用数），违反 → 触发中（firing）、恢复 → 已恢复。
        后台任务每 60s 评估一轮，「立即评估」按钮手动补一轮；正在触发中的告警
        显示在观测门户顶部横幅。
      </p>
      <AlertPanel />
      <DiscoveryStalls />
    </div>
  );
}
