import { useState } from "react";
import type { RepositoryCard } from "../api/repositories";
import { checkScope, submitScope, suggestScope, type ScopeSuggestion } from "../api/scope";
import { errText } from "../display";

/** 范围圈定面板（乙·第 2 步新增）：需求文本 + 勾选仓库 → 提交。
 *
 *  提交即产生一条 step=confirmation 的决策单（api-design.md §3.I 生产者语义），
 *  所以 `requirement` 是必填的链根，不是备注。建议按钮走 scope-assist（后端
 *  开关关闭时 503，如实回显错误，不假装没有建议）。 */
export function ScopePanel({
  repos,
  onToast,
}: {
  repos: RepositoryCard[];
  onToast: (text: string) => void;
}) {
  const [requirement, setRequirement] = useState("");
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [suggestions, setSuggestions] = useState<ScopeSuggestion[] | null>(null);
  const [busy, setBusy] = useState<"" | "suggest" | "submit">("");
  const [error, setError] = useState<string | null>(null);
  const [lastSubmitted, setLastSubmitted] = useState<string | null>(null);

  const toggle = (id: string) =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  const suggest = async () => {
    if (!requirement.trim()) {
      setError("先写需求文本再获取建议");
      return;
    }
    setBusy("suggest");
    setError(null);
    try {
      const res = await suggestScope(requirement.trim());
      setSuggestions(res.suggestions ?? []);
    } catch (err) {
      setError(errText(err));
    } finally {
      setBusy("");
    }
  };

  const submit = async () => {
    if (!requirement.trim()) {
      setError("先写需求文本");
      return;
    }
    if (selected.size === 0) {
      setError("至少勾选一个仓库");
      return;
    }
    setBusy("submit");
    setError(null);
    try {
      const ids = [...selected];
      const check = await checkScope(ids);
      const unknown = (check as { unknownRepositoryIds?: string[] }).unknownRepositoryIds ?? [];
      if (unknown.length > 0) {
        setError(`以下仓库未登记：${unknown.join("、")}`);
        return;
      }
      await submitScope({
        requirement: requirement.trim(),
        repositoryIds: ids,
        idempotencyKey: crypto.randomUUID(),
      });
      onToast(`已圈定 ${ids.length} 个仓库 · 决策单已记录`);
      setRequirement("");
      setSelected(new Set());
      setSuggestions(null);
      setLastSubmitted(requirement.trim());
    } catch (err) {
      setError(errText(err));
    } finally {
      setBusy("");
    }
  };

  return (
    <div className="mt-3 rounded-hard border border-line bg-panel px-4 py-3">
      <div className="flex items-baseline gap-3">
        <h2 className="text-[13px] font-semibold text-cream">范围圈定</h2>
        <span className="text-[11px] text-tx3">需求 + 仓库 → 一条决策单（历史决策的起点）</span>
      </div>
      <textarea
        className="mt-2 w-full rounded-hard border border-line bg-ink-deep px-2.5 py-2 font-mono text-[12px] text-tx placeholder:text-tx3"
        rows={2}
        placeholder="需求原文（例：为价格服务补充折扣计算的仓库）"
        value={requirement}
        onChange={(e) => setRequirement(e.target.value)}
      />
      <div className="mt-2 grid gap-1">
        {repos.length === 0 && <span className="text-[11px] text-tx3">还没有仓库——先添加并完成扫描</span>}
        {repos.map((repo) => (
          <label key={repo.id} className="flex cursor-pointer items-center gap-2 text-[11.5px] text-tx2">
            <input
              type="checkbox"
              checked={selected.has(repo.id)}
              onChange={() => toggle(repo.id)}
              className="accent-amber"
            />
            <span className="font-mono text-tx2">{repo.name}</span>
            <span className="text-tx3">{repo.url}</span>
          </label>
        ))}
      </div>
      {suggestions !== null && (
        <div className="mt-2 rounded-hard border border-line bg-ink-deep px-2.5 py-2">
          {suggestions.length === 0 ? (
            <span className="text-[11px] text-tx3">没有关键词命中的建议——手动勾选即可</span>
          ) : (
            suggestions.map((s) => (
              <button
                key={s.repositoryId}
                className="mr-1.5 mt-1 inline-block rounded-hard border border-line px-2 py-px text-[11px] text-tx2 hover:border-amber hover:text-amber-hi"
                title={`${s.rationale} · 分数 ${s.score.toFixed(2)}`}
                onClick={() => toggle(s.repositoryId)}
              >
                + {s.name} · {s.score.toFixed(2)}
              </button>
            ))
          )}
        </div>
      )}
      {lastSubmitted && (
        <p className="mt-2 text-[11px] text-tx3">
          已为「{lastSubmitted}」记录范围确认。补充仓库后用同一需求再次提交 = 范围修订（决策链 v+1），无需重规划。
        </p>
      )}
      {error && <p className="mt-2 text-[11.5px] text-amber">{error}</p>}
      <div className="mt-2 flex gap-2">
        <button
          className="rounded-hard border border-line px-2.5 py-[3px] text-[11.5px] text-tx2 hover:border-amber hover:text-amber-hi disabled:opacity-50"
          disabled={busy !== ""}
          onClick={suggest}
        >
          {busy === "suggest" ? "建议中…" : "获取建议"}
        </button>
        <button
          className="rounded-hard border border-amber px-2.5 py-[3px] text-[11.5px] text-amber-hi hover:bg-amber hover:text-ink-deep disabled:opacity-50"
          disabled={busy !== ""}
          onClick={submit}
        >
          {busy === "submit" ? "提交中…" : "提交圈定"}
        </button>
      </div>
    </div>
  );
}
