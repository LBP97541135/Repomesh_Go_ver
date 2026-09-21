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

/** 只接受字符串的错误文案。
 *
 *  类型上 `HealthGateResult.error` 是 `string | undefined`，但**运行时不是**：
 *  后端在 401/503 时写的是接入层错误信封（`{"error":{"code","message"}}`），
 *  取数侧曾经把它原样透上来（见 api/agentteamsHealth.ts 里那段注释）。
 *  取数侧已经改成翻译成人话；这里是渲染层的最后一道防线 —— 非字符串一律当
 *  「没有文案」，宁可少一句话，也不吐一个 `[object Object]` 给人看。 */
function errorText(value: unknown): string {
  return typeof value === "string" ? value : "";
}

/** 一个名字是不是 **AgentTeams 的 worker id**。
 *
 *  2026-09-21 线上实测（用户报「Worker 不可用 · 读不到 — worker status returned 404」）：
 *  任务行给这张卡的是**展示标签**（`W1`、`W2`…），而它拿这个名字去查 AgentTeams
 *  Controller 的 `/workers/{name}/status` —— **必然 404**，界面于是渲染成
 *  「Worker 不可用」，看着像执行环境坏了。
 *
 *  实际是**类别错误**：RepoMesh 的编制标签（W1/L1）不是 Controller 的 worker id。
 *  真实 id 形如 `wrk_accept_codex`（部署文档写明：executor 与 coordinator dispatch
 *  必须同为 `wrk_*`）。不是这个形状，就说明**这个项目根本不经过 Controller 派工** ——
 *  那这张卡就不该出现（不是"坏了"，是"这件事在这里不存在"）。
 *
 *  宁可整卡不渲染，也不把一个必然 404 的查询渲染成故障：后者会让人去查一个不存在的问题。
 */
function isAgentTeamsWorkerId(name: string): boolean {
  return name.startsWith("wrk_");
}

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
  if (!isAgentTeamsWorkerId(workerName)) return null;
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
        {/* 2026-09-21：这里此前直接把 result.error 插进文案，而接入层错误信封
            （{"error":{"code","message"}}）会让它变成对象 → 页面显示 [object Object]，
            真正的原因被吃掉。取数侧已改成翻译成人话，这里再加一道：**只渲染字符串**，
            非字符串一律不渲染（宁可少一句话，也不吐一个 toString）。 */}
        {workerName} 当前状态{" "}
        <b>{phaseLabel(result.phase) || (errorText(result.error) ? "读不到" : "未知")}</b>
        {errorText(result.error) ? ` — ${errorText(result.error)}` : ""}
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
