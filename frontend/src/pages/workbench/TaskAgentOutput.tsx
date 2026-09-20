/** 「这条任务到底干了什么」—— agent 的真实输出（右栏任务详情用）。
 *
 *  用户反复提过："右边的流式输出里，看不到 worker 具体的工作内容"、
 *  "我也看不到 worker 的内部工作记录"。这个面板把工作区里
 *  `agent-stdout.log` / `agent-stderr.log` 的**尾部**摊开，按 run 分组
 *  （一个任务通常有两条：开发跑的那次 + 测试跑的那次）。
 *
 *  两条诚实条款（都来自读面的事实，不是猜的）：
 *   · `logsMissing` 非空 → 明说"这份日志没有记录"（工作区被清掉 / 还没落盘），
 *     **不**把空内容显示成"它什么都没干"；
 *   · `*Truncated` 为真 → 明说"这是尾部"，免得人以为看到了全过程。 */
import { useEffect, useState } from "react";

import { fetchTaskAgentOutput, type TaskAgentOutputView, type TaskRunOutputView } from "../../api/taskOutput";

function RunCard({ run }: { run: TaskRunOutputView }) {
  const [showStderr, setShowStderr] = useState(false);
  const failed = run.exitCode !== null && run.exitCode !== 0;
  return (
    <div className="rounded-[9px] border border-[var(--tree-hairline)] bg-[var(--tree-card)]">
      <div className="flex flex-wrap items-center gap-1.5 border-b border-[var(--tree-hairline)] px-2.5 py-1.5">
        <span className="text-[11.5px] font-medium text-[var(--tree-ink)]">{run.agentKind === "review_agent" ? "仓库负责人 · 辅助审查" : run.agentKind || "agent"}</span>
        <span className={`rounded-[5px] px-1.5 py-px text-[10px] ${failed ? "bg-salmon-well text-salmon" : "bg-[var(--tree-zone)] text-[var(--tree-sub)]"}`}>
          {run.state}
          {run.exitCode !== null ? ` · exit ${run.exitCode}` : ""}
        </span>
        {run.repoFullName && (
          <span className="font-mono text-[10.5px] text-[var(--tree-faint)]">{run.repoFullName}</span>
        )}
        <span className="ml-auto text-[10px] text-[var(--tree-faint)]">
          {run.startedAt}{run.exitedAt ? ` → ${run.exitedAt}` : ""}
        </span>
      </div>

      {run.logsMissing.length > 0 && (
        <p className="px-2.5 pt-1.5 text-[10.5px] text-amber">
          没有这份记录：{run.logsMissing.join("、")}（工作区可能已被清理，或日志还没落盘）——不是"它什么都没干"。
        </p>
      )}

      {run.stdoutTail !== "" && (
        <div className="px-2.5 pt-1.5">
          <div className="flex items-center gap-1.5">
            <span className="microlabel">agent 输出</span>
            {run.stdoutTruncated && <span className="text-[10px] text-[var(--tree-faint)]">（只看尾部）</span>}
          </div>
          <pre className="mt-1 max-h-[360px] overflow-auto whitespace-pre-wrap break-words rounded-[7px] bg-[var(--tree-zone)] px-2 py-1.5 font-mono text-[10.5px] leading-[1.65] text-[var(--tree-ink)]">
            {run.stdoutTail}
          </pre>
        </div>
      )}

      {run.stderrTail !== "" && (
        <div className="px-2.5 py-1.5">
          <button
            type="button"
            className="text-[10.5px] text-[var(--tree-sub)] hover:text-[var(--tree-ink)]"
            onClick={() => setShowStderr((v) => !v)}
          >
            {showStderr ? "▾" : "▸"} 标准错误{run.stderrTruncated ? "（尾部）" : ""}
          </button>
          {showStderr && (
            <pre className="mt-1 max-h-[240px] overflow-auto whitespace-pre-wrap break-words rounded-[7px] bg-[var(--tree-zone)] px-2 py-1.5 font-mono text-[10.5px] leading-[1.65] text-[var(--tree-sub)]">
              {run.stderrTail}
            </pre>
          )}
        </div>
      )}

      {run.stdoutTail === "" && run.stderrTail === "" && run.logsMissing.length === 0 && (
        <p className="px-2.5 py-1.5 text-[10.5px] text-[var(--tree-faint)]">
          这次 run 的日志是**空的**（进程起来了但没写任何输出）。
        </p>
      )}
    </div>
  );
}

export function TaskAgentOutput({ projectId, taskId }: { projectId: string | null; taskId: string }) {
  const [view, setView] = useState<TaskAgentOutputView | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [reload, setReload] = useState(0);

  useEffect(() => {
    if (!projectId || !taskId) return;
    let cancelled = false;
    fetchTaskAgentOutput(projectId, taskId)
      .then((v) => {
        if (!cancelled) {
          setView(v);
          setError(null);
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(err instanceof Error ? err.message : String(err));
      });
    return () => {
      cancelled = true;
    };
  }, [projectId, taskId, reload]);

  if (!projectId) return null;
  return (
    <div className="border-t border-dashed border-[var(--tree-hairline)] px-4 py-3">
      <div className="flex items-center gap-2">
        <span className="microlabel">worker 工作内容</span>
        <button
          type="button"
          className="ml-auto text-[10.5px] text-[var(--tree-sub)] hover:text-[var(--tree-ink)]"
          onClick={() => setReload((n) => n + 1)}
        >
          刷新
        </button>
      </div>
      {error !== null ? (
        <p className="mt-1.5 rounded-[7px] border border-salmon/40 bg-salmon-well px-2.5 py-1.5 text-[11px] text-salmon">
          读不到 agent 输出：{error}
        </p>
      ) : view === null ? (
        <p className="mt-1.5 text-[11px] text-[var(--tree-faint)]">读取中…</p>
      ) : view.runs.length === 0 ? (
        <p className="mt-1.5 text-[11px] text-[var(--tree-faint)]">
          这条任务还没有 agent run（还没派发，或者派发台账里没有它的记录）。
        </p>
      ) : (
        <div className="mt-1.5 flex flex-col gap-2">
          {view.runs.map((run) => (
            <RunCard key={run.runId} run={run} />
          ))}
        </div>
      )}
    </div>
  );
}
