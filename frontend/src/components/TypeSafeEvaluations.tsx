import { useEffect, useState } from "react";
import { fetchTypeSafeEvaluations, typeSafeMessage, type TypeSafeEvaluation } from "../api/typesafe";

const CHOICE = { supported: "证据支持", contradicted: "证据矛盾", insufficient: "证据不足" };
const STATUS = { pending: "判断中", completed: "已完成判断", failed: "调用失败", unknown: "结果未知", unavailable: "未调用" };

export function TypeSafeEvaluations({ projectId, issueId, purpose, taskId }: { projectId?: string | null; issueId?: string | null; purpose?: "test" | "code_review"; taskId?: string }) {
  return projectId && issueId ? <Records key={`${projectId}:${issueId}`} projectId={projectId} issueId={issueId} purpose={purpose} taskId={taskId} /> : null;
}
function Records({ projectId, issueId, purpose, taskId }: { projectId: string; issueId: string; purpose?: "test" | "code_review"; taskId?: string }) {
  const [items, setItems] = useState<TypeSafeEvaluation[] | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout>;
    const read = async () => {
      try {
        const value = await fetchTypeSafeEvaluations(projectId, issueId);
        if (!cancelled) { setItems(value.items); setError(""); }
      } catch (err) { if (!cancelled) setError(typeSafeMessage(err)); }
      if (!cancelled) timer = setTimeout(() => void read(), 10000);
    };
    void read();
    return () => { cancelled = true; clearTimeout(timer); };
  }, [projectId, issueId]);
  const visible = items?.filter(item => (!purpose || item.purpose === purpose) && (!taskId || item.taskId === taskId));
  return (
    // 2026-09-21 用户实测：这一段「会被拦住，展示不完整」。
    //
    // 根因是**嵌套滚动**：这张卡自己带了 `max-h-[440px] overflow-y-auto`，而它所在
    // 的房间容器（FocusPanel 的任务/主会话房间）本身已经 `overflow-y-auto`。
    // 于是内容被两层滚动条切成两段：外层滚到这张卡就停住，卡里的正文要再滚一次才看得到
    // —— 看起来就是"话被拦腰截断"。删掉内层的高度与滚动，交给房间那一层统一滚：
    // **一屏只留一条滚动条**。
    <section className="m-3 rounded-[8px] border border-[var(--tree-hairline)] bg-[var(--tree-card)] p-3 text-[11px]">
      <h3 className="font-medium text-[var(--tree-ink)]">Jev 辅助{purpose === "code_review" ? "代码审查" : purpose === "test" ? "测试验证" : "验证与审查"}</h3>
      <p className="mt-1 text-[var(--tree-faint)]">语义判断独立记录，不改变测试结果或经理审批。</p>
      {error && <p role="alert" className="mt-2 text-salmon">{error}</p>}
      {!items && !error && <p className="mt-2 text-[var(--tree-faint)]">读取中…</p>}
      {visible?.length === 0 && <p className="mt-2 text-[var(--tree-faint)]">尚无 Jev 调用记录。已启用时，请等待后续测试或审查运行。</p>}
      {visible?.map(item => (
        <article key={item.id} className="mt-3 border-t border-[var(--tree-hairline)] pt-2">
          <p className="text-[var(--tree-ink)]">{item.purpose === "code_review" ? "仓库负责人 · 代码审查" : "测试团队 · 证据核对"} · {STATUS[item.status]}</p>
          {item.errorCode && <p className="mt-1 text-salmon">{typeSafeMessage(item.errorCode)}</p>}
          {item.response && item.input.claims?.map(claim => {
            const answer = item.response?.answers[claim.id];
            return <div key={claim.id} className="mt-2 text-[var(--tree-sub)]"><p>{claim.text}</p>
              {answer && <p className={answer.choice === "contradicted" ? "text-salmon" : "text-[var(--tree-faint)]"}>{CHOICE[answer.choice]} · confidence {answer.confidence.toFixed(2)}<br />支持 {(answer.probabilities.supported * 100).toFixed(1)}% · 矛盾 {(answer.probabilities.contradicted * 100).toFixed(1)}% · 不足 {(answer.probabilities.insufficient * 100).toFixed(1)}%</p>}
            </div>;
          })}
          <details className="mt-2 text-[var(--tree-faint)]">
            <summary>来源与调用详情</summary>
            <p className="mt-1 break-all">运行：{item.runId} · 配置修订 {item.revision}</p>
            <p>模型：{item.response?.model ?? "未取得结果"} · {item.latencyMs} ms{item.response ? ` · 输入 ${item.response.usage.input_tokens} tokens` : ""}</p>
            <p className="break-all">Skill：{item.skillHash} · 模板 {item.templateVersion}</p>
            {item.input.commit && <p className="break-all">Agent 提供的提交：{item.input.commit}</p>}
            <p className="mt-1">以下材料由 Agent 提供，尚非独立核验的执行事实：</p>
            <pre className="mt-1 max-h-48 overflow-auto whitespace-pre-wrap break-all">{item.input.evidence || "无证据文本"}</pre>
          </details>
        </article>
      ))}
    </section>
  );
}
