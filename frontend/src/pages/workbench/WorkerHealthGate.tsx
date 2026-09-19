/** Worker 健康门（Phase 2，2026-09-18）：任务行选中时检查执行 Worker 的
 *  运行时状态；blocked 时显示恢复卡（ensure-ready 重试入口）。
 *
 *  数据流：FocusPanel 传入 workerName → dispatchGate() → HealthGateResult
 *  - action === "none" 且 phase === "Running" → 绿色"运行中"徽章
 *  - action === "ensure-ready" 且 recovered → 琥珀"已从休眠恢复"
 *  - action === "blocked" → 红色恢复卡 + 重试按钮 */

import { useCallback, useEffect, useState } from "react";
import { dispatchGate, phaseLabel, type HealthGateResult } from "../../api/agentteamsHealth";
import { IconCheck, IconClock, IconUser } from "./treeIcons";

export function WorkerHealthGate({ workerName }: { workerName: string | null }) {
  const [result, setResult] = useState<HealthGateResult | null>(null);
  const [checking, setChecking] = useState(false);

  const check = useCallback(() => {
    if (!workerName) return;
    setChecking(true);
    dispatchGate(workerName)
      .then(setResult)
      .catch((err: unknown) =>
        setResult({
          worker: workerName,
          phase: "",
          action: "blocked",
          recovered: false,
          durationMs: 0,
          error: String(err),
        }),
      )
      .finally(() => setChecking(false));
  }, [workerName]);

  useEffect(() => {
    setResult(null);
    check();
  }, [check]);

  if (!workerName) return null;
  if (checking && !result) return <p className="px-4 py-2 text-[11px] text-[var(--tree-faint)]">Worker 健康检查中…</p>;
  if (!result) return null;

  // 运行中：小绿条
  if (result.action === "none" && result.phase === "Running") {
    return (
      <div className="mx-4 mt-2 flex items-center gap-2 rounded-hard border border-[#16a34a]/30 bg-[#16a34a]/5 px-3 py-1.5">
        <IconCheck size={12} className="text-[#16a34a]" />
        <span className="text-[11px] text-[var(--tree-sub)]">
          Worker <b className="text-[var(--tree-ink)]">{workerName}</b> 运行中
        </span>
      </div>
    );
  }

  // 已恢复：琥珀条
  if (result.recovered) {
    return (
      <div className="mx-4 mt-2 flex items-center gap-2 rounded-hard border border-amber/30 bg-amber-well px-3 py-1.5">
        <IconClock size={12} className="text-amber" />
        <span className="text-[11px] text-[var(--tree-sub)]">
          Worker 已从{phaseLabel(result.phase) || "异常"}恢复 ({result.durationMs}ms)
        </span>
      </div>
    );
  }

  // Blocked：恢复卡
  return (
    <div className="mx-4 mt-2 rounded-hard border border-salmon/40 bg-salmon-well p-3">
      <div className="flex items-center gap-2">
        <IconUser size={13} className="text-salmon" />
        <span className="text-[12px] font-semibold text-salmon">Worker 不可用</span>
      </div>
      <p className="mt-1 text-[11px] leading-[1.6] text-[var(--tree-sub)]">
        {workerName} 当前状态 <b>{phaseLabel(result.phase) || "未知"}</b>
        {result.error ? ` — ${result.error}` : ""}
      </p>
      <button
        type="button"
        className="mt-2 rounded-hard border border-salmon/50 bg-transparent px-3 py-1 text-[11px] font-semibold text-salmon hover:bg-salmon/10 disabled:opacity-50"
        disabled={checking}
        onClick={check}
      >
        {checking ? "检查中…" : "重新检查 / 恢复"}
      </button>
    </div>
  );
}
