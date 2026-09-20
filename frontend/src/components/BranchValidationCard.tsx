/** 评委建议① 的**可演示入口**：数据库分支验证（2026-09-20 新增）。
 *
 *  为什么要有它：`internal/branchvalidation` 早就实现完整（provider 端口、
 *  PolarDB 适配器、业务数据基线克隆、逐条留迁移结果、清理失败可重试），
 *  路由也挂了 —— 但**没有任何界面能触发它**，所以线上 `database_branch_validations`
 *  至今 0 行。评委要看"数据库相关的 Coding 用 Polar Agentic Database Branch"时，
 *  一个能点、能看结果的入口比一段代码更有说服力。
 *
 *  三条诚实边界：
 *   · 基线库**必填**：空库测不出历史数据/约束冲突/新旧版本兼容 —— 后端会拒，
 *     前端也不替它编一个默认值；
 *   · 结果里**provider 是谁就写谁**（local-postgres 不会冒充 polardb-agentic-branch）；
 *   · 逐条迁移结果原样列出（失败那条带错误原文），不做"验证通过"这种笼统话。
 */
import { useState } from "react";

import {
  retryBranchCleanup,
  startBranchValidation,
  type BranchValidationRunView,
} from "../api/branchValidation";

const PROVIDER_LABEL: Record<string, string> = {
  "local-postgres": "本地 PostgreSQL（同一实例内 TEMPLATE 克隆）",
  "polardb-agentic-branch": "PolarDB Agentic Branch",
};

export function BranchValidationCard({
  projectId,
  repoOptions,
}: {
  projectId: string | null;
  repoOptions: Array<{ id: string; name: string }>;
}) {
  const [repo, setRepo] = useState("");
  const [baseline, setBaseline] = useState("");
  const [migrations, setMigrations] = useState("");
  const [busy, setBusy] = useState(false);
  const [run, setRun] = useState<BranchValidationRunView | null>(null);
  const [error, setError] = useState<string | null>(null);

  if (!projectId) return null;

  const input =
    "w-full rounded-[7px] border border-[var(--tree-line)] bg-[var(--tree-card)] px-2 py-1 text-[11.5px] text-[var(--tree-ink)] placeholder:text-[var(--tree-faint)]";

  const submit = () => {
    setBusy(true);
    setError(null);
    setRun(null);
    startBranchValidation(projectId, {
      IdempotencyKey: `console-branch-validation-${crypto.randomUUID()}`,
      RepositoryID: repo,
      SourceDatabaseRef: baseline,
      // 一行一条语句：空行丢掉，避免把空白当迁移执行
      Migrations: migrations
        .split("\n")
        .map((line) => line.trim())
        .filter((line) => line !== ""),
    })
      .then((view) => setRun(view))
      .catch((err: unknown) => setError(err instanceof Error ? err.message : String(err)))
      .finally(() => setBusy(false));
  };

  const retry = () => {
    if (!run) return;
    setBusy(true);
    setError(null);
    retryBranchCleanup(projectId, run.id)
      .then((view) => setRun(view))
      .catch((err: unknown) => setError(err instanceof Error ? err.message : String(err)))
      .finally(() => setBusy(false));
  };

  return (
    <div className="border-t border-dashed border-[var(--tree-hairline)] px-4 py-3">
      <span className="microlabel">数据库分支验证</span>
      <p className="mt-1 text-[10.5px] leading-[1.7] text-[var(--tree-faint)]">
        为**一个候选**开一条独立数据库分支：从**业务数据基线库**克隆（不是空库），
        逐条执行迁移并留结果，跑完清理。同一候选的多个服务共用一条分支，不同候选互不干扰；
        分支数据不进生产。证据里会写清是在哪个 provider 上跑的。
      </p>

      <div className="mt-2 flex flex-col gap-1.5 rounded-[7px] border border-[var(--tree-hairline)] p-2">
        <select className={input} value={repo} onChange={(e) => setRepo(e.target.value)}>
          <option value="">（选择仓库）</option>
          {repoOptions.map((r) => (
            <option key={r.id} value={r.id}>
              {r.name}
            </option>
          ))}
        </select>
        <input
          className={input}
          placeholder="业务数据基线库名（必填 —— 空库测不出历史数据与约束冲突）"
          value={baseline}
          onChange={(e) => setBaseline(e.target.value)}
        />
        <textarea
          className={`${input} min-h-[64px] font-mono`}
          placeholder={"迁移语句，一行一条：\nALTER TABLE orders ADD COLUMN free_shipping boolean NOT NULL DEFAULT false;"}
          value={migrations}
          onChange={(e) => setMigrations(e.target.value)}
        />
        <button
          type="button"
          disabled={busy || repo === "" || baseline === ""}
          className="self-start rounded-[7px] bg-[var(--tree-acc)] px-3 py-1 text-[11.5px] font-semibold text-white disabled:opacity-50"
          onClick={submit}
        >
          {busy ? "执行中…" : "跑一次分支验证"}
        </button>
      </div>

      {error && (
        <p className="mt-2 rounded-[7px] border border-salmon/40 bg-salmon-well px-2.5 py-1.5 text-[11px] text-salmon">
          {error}
        </p>
      )}

      {run && (
        <div className="mt-2 rounded-[7px] border border-[var(--tree-hairline)] bg-[var(--tree-card)] p-2.5">
          <div className="flex flex-wrap items-center gap-1.5">
            <span
              className={`rounded-[5px] border px-1.5 py-px text-[10px] ${
                run.status === "passed"
                  ? "border-olive/40 bg-olive-well text-olive"
                  : run.status === "failed"
                    ? "border-salmon/40 bg-salmon-well text-salmon"
                    : "border-line text-[var(--tree-sub)]"
              }`}
            >
              {run.status}
            </span>
            <span className="text-[11px] text-[var(--tree-ink)]">
              {PROVIDER_LABEL[run.provider] ?? run.provider}
            </span>
            {run.branchRef && (
              <span className="font-mono text-[10.5px] text-[var(--tree-faint)]">分支 {run.branchRef}</span>
            )}
            {run.cleanupPending && (
              <span className="rounded-[5px] border border-amber/40 bg-amber-well px-1.5 py-px text-[10px] text-amber">
                分支未清理（可重试）
              </span>
            )}
          </div>
          {run.failureCode && <p className="mt-1 text-[11px] text-salmon">{run.failureCode}</p>}
          {(run.migrationResults ?? []).length > 0 && (
            <div className="mt-1.5 flex flex-col gap-0.5">
              {(run.migrationResults ?? []).map((result, index) => (
                <div key={index} className="text-[10.5px] leading-[1.7]">
                  <span className={result.ok ? "text-olive" : "text-salmon"}>{result.ok ? "✓" : "✗"}</span>{" "}
                  <span className="break-all font-mono text-[var(--tree-sub)]">{result.statement}</span>
                  {result.error && <span className="block pl-3 text-salmon">{result.error}</span>}
                </div>
              ))}
            </div>
          )}
          {run.cleanupPending && (
            <button
              type="button"
              disabled={busy}
              className="mt-1.5 rounded-[7px] border border-amber/60 bg-amber-well px-2.5 py-1 text-[11px] font-semibold text-amber disabled:opacity-50"
              onClick={retry}
            >
              {busy ? "重试中…" : "重试清理"}
            </button>
          )}
        </div>
      )}
    </div>
  );
}
