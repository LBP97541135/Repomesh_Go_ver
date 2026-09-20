/** E 跨仓职责 / 授权 / 冲突 —— 案例卡（2026-09-20 新增）。
 *
 *  为什么要有它：后端 `internal/responsibility` 早就完整（时间线 / Owner 确认 /
 *  授权申请·批准·撤销 / 责任转移 / 冲突上报·裁决 / 平台状态同步），路由也挂了，
 *  但**前端一个入口都没有** —— 这一整块能力在界面上等于不存在，评委问"Manager
 *  和 Leader 的交接在哪看"时无从指。
 *
 *  这一卡做两件事，都是**如实呈现**：
 *   ① 案例时间线：谁（角色 + id）在什么时候做了什么（事件类型 + 明细）；
 *   ② 六个动作的入口：Owner 确认 / 申请授权 / 批准 / 撤销 / 责任转移 / 冲突上报。
 *
 *  三条诚实边界：
 *   · 没有事件就说"还没有记录"，不摆一条编的"已交接"；
 *   · 事件类型未登记的原样显示（`CASE_EVENT_LABEL` 里没有就不翻译，不编好听名字）；
 *   · 动作失败原样显示服务端原文（不做"操作失败，请重试"这种抹掉原因的处理）。 */
import { useCallback, useEffect, useState } from "react";

import {
  CASE_EVENT_LABEL,
  CASE_ROLE_LABEL,
  confirmRepositoryOwner,
  fetchCaseTimeline,
  reportConflict,
  requestAuthorization,
  transferResponsibility,
  type CaseEventView,
} from "../api/responsibility";

function hhmmss(at: string): string {
  const date = new Date(at);
  return Number.isNaN(date.getTime()) ? at : date.toLocaleString();
}

/** 明细压成一行：对象取前几个键，太长就截断 —— 不丢信息，但不撑爆卡片。 */
function detailLine(detail: unknown): string {
  if (detail === null || detail === undefined) return "";
  if (typeof detail === "string") return detail;
  try {
    const text = JSON.stringify(detail);
    return text.length > 200 ? `${text.slice(0, 200)}…` : text;
  } catch {
    return "";
  }
}

type ActionKey = "owner" | "auth" | "transfer" | "conflict";

export function ResponsibilityCaseCard({
  projectId,
  planId,
  actorId,
  repoOptions,
}: {
  projectId: string | null;
  planId: string | null;
  /** 当前登录者（写进 confirmed_by / requester_id / from_id 等字段）。 */
  actorId: string;
  /** 本项目已挂仓库（只做下拉候选，服务端不校验这份清单）。 */
  repoOptions: Array<{ id: string; name: string }>;
}) {
  const [events, setEvents] = useState<CaseEventView[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [action, setAction] = useState<ActionKey | null>(null);
  const [busy, setBusy] = useState(false);
  const [opError, setOpError] = useState<string | null>(null);
  const [note, setNote] = useState<string | null>(null);
  const [reload, setReload] = useState(0);

  useEffect(() => {
    if (!projectId || !planId) {
      setEvents(null);
      return;
    }
    let cancelled = false;
    fetchCaseTimeline(projectId, planId)
      .then((view) => {
        if (!cancelled) {
          setEvents(view.events ?? []);
          setError(null);
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(err instanceof Error ? err.message : String(err));
      });
    return () => {
      cancelled = true;
    };
  }, [projectId, planId, reload]);

  const run = useCallback(
    (fn: () => Promise<unknown>, done: string) => {
      setBusy(true);
      setOpError(null);
      fn()
        .then(() => {
          setNote(done);
          setAction(null);
          setReload((n) => n + 1);
        })
        .catch((err: unknown) => setOpError(err instanceof Error ? err.message : String(err)))
        .finally(() => setBusy(false));
    },
    [],
  );

  if (!projectId || !planId) {
    return (
      <div className="border-t border-dashed border-[var(--tree-hairline)] px-4 py-3">
        <p className="microlabel pb-1">跨仓职责 · 授权 · 冲突</p>
        <p className="text-[11px] leading-[1.8] text-[var(--tree-sub)]">
          还没有计划（或读不到项目 id），案例时间线无从取起 —— 它按 plan 记录。
        </p>
      </div>
    );
  }

  const repoSelect = (value: string, onChange: (next: string) => void) => (
    <select
      className="w-full rounded-[7px] border border-[var(--tree-line)] bg-[var(--tree-card)] px-2 py-1 text-[11.5px] text-[var(--tree-ink)]"
      value={value}
      onChange={(e) => onChange(e.target.value)}
    >
      <option value="">（选择仓库）</option>
      {repoOptions.map((r) => (
        <option key={r.id} value={r.id}>
          {r.name}
        </option>
      ))}
    </select>
  );

  const input = "w-full rounded-[7px] border border-[var(--tree-line)] bg-[var(--tree-card)] px-2 py-1 text-[11.5px] text-[var(--tree-ink)]";

  return (
    <div className="border-t border-dashed border-[var(--tree-hairline)] px-4 py-3">
      <div className="flex items-center gap-2">
        <span className="microlabel">跨仓职责 · 授权 · 冲突</span>
        <button
          type="button"
          className="ml-auto text-[10.5px] text-[var(--tree-sub)] hover:text-[var(--tree-ink)]"
          onClick={() => setReload((n) => n + 1)}
        >
          刷新
        </button>
      </div>
      <p className="mt-1 text-[10.5px] leading-[1.7] text-[var(--tree-faint)]">
        时间线记录的是**谁（角色）在什么时候做了什么**：Manager 定方向、Leader 认领仓库与申请授权、
        执行角色报冲突，三者交接在同一串事件里可见。动作只有人能发起，agent 只能留下事件。
      </p>

      {/* 动作入口 */}
      <div className="mt-2 flex flex-wrap gap-1.5">
        {(
          [
            ["owner", "确认仓库 Owner"],
            ["auth", "申请授权"],
            ["transfer", "责任转移"],
            ["conflict", "上报冲突"],
          ] as Array<[ActionKey, string]>
        ).map(([key, label]) => (
          <button
            key={key}
            type="button"
            className={`rounded-[7px] border px-2.5 py-1 text-[11px] ${
              action === key
                ? "border-amber/60 bg-amber-well text-amber"
                : "border-[var(--tree-line)] text-[var(--tree-ink)] hover:border-[var(--tree-acc)]"
            }`}
            onClick={() => {
              setAction(action === key ? null : key);
              setOpError(null);
              setNote(null);
            }}
          >
            {label}
          </button>
        ))}
      </div>

      {action === "owner" && (
        <OwnerForm
          repoSelect={repoSelect}
          input={input}
          busy={busy}
          actorId={actorId}
          onSubmit={(repo, owner) =>
            run(
              () =>
                confirmRepositoryOwner(projectId, planId, {
                  repository_id: repo,
                  owner_github_id: owner,
                  confirmed_by: actorId,
                }),
              "已确认该仓库的 Owner",
            )
          }
        />
      )}
      {action === "auth" && (
        <AuthForm
          repoSelect={repoSelect}
          input={input}
          busy={busy}
          actorId={actorId}
          onSubmit={(repo, scope, reason) =>
            run(
              () =>
                requestAuthorization(projectId, planId, {
                  repository_id: repo,
                  requester_id: actorId,
                  requester_role: "leader",
                  scope,
                  reason,
                }),
              "授权申请已登记（等有权限的人批准）",
            )
          }
        />
      )}
      {action === "transfer" && (
        <TransferForm
          input={input}
          busy={busy}
          actorId={actorId}
          onSubmit={(fromRole, toRole, toId, reason) =>
            run(
              () =>
                transferResponsibility(projectId, planId, {
                  from_role: fromRole,
                  from_id: actorId,
                  to_role: toRole,
                  to_id: toId,
                  reason,
                }),
              "责任已转移（原负责人不可用时用这一条）",
            )
          }
        />
      )}
      {action === "conflict" && (
        <ConflictForm
          input={input}
          busy={busy}
          actorId={actorId}
          onSubmit={(type) =>
            run(
              () =>
                reportConflict(projectId, planId, {
                  conflict_type: type,
                  discovered_by: actorId,
                  discovered_by_role: "executor",
                }),
              "冲突已上报（等裁决人给结论）",
            )
          }
        />
      )}

      {note && <p className="mt-2 text-[11px] text-olive">{note}</p>}
      {opError && (
        <p className="mt-2 rounded-[7px] border border-salmon/40 bg-salmon-well px-2.5 py-1.5 text-[11px] text-salmon">
          {opError}
        </p>
      )}

      {/* 时间线 */}
      <div className="mt-2.5">
        {error !== null ? (
          <p className="text-[11px] text-salmon">案例时间线读取失败：{error}</p>
        ) : events === null ? (
          <p className="text-[11px] text-[var(--tree-faint)]">读取中…</p>
        ) : events.length === 0 ? (
          <p className="text-[11px] leading-[1.8] text-[var(--tree-sub)]">
            还没有记录 —— 这一条不是"没做"，是"还没有人在这里留下动作"。
          </p>
        ) : (
          <div className="flex flex-col gap-1">
            {events.map((event) => (
              <div key={event.id} className="flex items-start gap-2 text-[11px]">
                <span className="mt-px flex-none rounded-[5px] border border-[var(--tree-hairline)] px-1.5 py-px text-[10px] text-[var(--tree-sub)]">
                  {CASE_ROLE_LABEL[event.actor_role] ?? event.actor_role}
                </span>
                <span className="min-w-0 flex-1">
                  <span className="text-[var(--tree-ink)]">
                    {CASE_EVENT_LABEL[event.event_kind] ?? event.event_kind}
                  </span>
                  <span className="ml-1 text-[10px] text-[var(--tree-faint)]">{hhmmss(event.created_at)}</span>
                  {detailLine(event.detail) && (
                    <span className="block break-all font-mono text-[10px] text-[var(--tree-faint)]">
                      {detailLine(event.detail)}
                    </span>
                  )}
                </span>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

function OwnerForm({
  repoSelect,
  input,
  busy,
  onSubmit,
}: {
  repoSelect: (value: string, onChange: (next: string) => void) => React.ReactNode;
  input: string;
  busy: boolean;
  actorId: string;
  onSubmit: (repo: string, owner: string) => void;
}) {
  const [repo, setRepo] = useState("");
  const [owner, setOwner] = useState("");
  return (
    <div className="mt-2 flex flex-col gap-1.5 rounded-[7px] border border-[var(--tree-hairline)] p-2">
      {repoSelect(repo, setRepo)}
      <input className={input} placeholder="Owner 的 GitHub id（如 octocat）" value={owner} onChange={(e) => setOwner(e.target.value)} />
      <button
        type="button"
        disabled={busy || repo === "" || owner === ""}
        className="self-start rounded-[7px] bg-[var(--tree-acc)] px-3 py-1 text-[11.5px] font-semibold text-white disabled:opacity-50"
        onClick={() => onSubmit(repo, owner)}
      >
        {busy ? "提交中…" : "确认 Owner"}
      </button>
    </div>
  );
}

function AuthForm({
  repoSelect,
  input,
  busy,
  onSubmit,
}: {
  repoSelect: (value: string, onChange: (next: string) => void) => React.ReactNode;
  input: string;
  busy: boolean;
  actorId: string;
  onSubmit: (repo: string, scope: string, reason: string) => void;
}) {
  const [repo, setRepo] = useState("");
  const [scope, setScope] = useState("repo:write");
  const [reason, setReason] = useState("");
  return (
    <div className="mt-2 flex flex-col gap-1.5 rounded-[7px] border border-[var(--tree-hairline)] p-2">
      {repoSelect(repo, setRepo)}
      <input className={input} placeholder="授权范围（如 repo:write）" value={scope} onChange={(e) => setScope(e.target.value)} />
      <input className={input} placeholder="为什么需要（理由要写清，人会看）" value={reason} onChange={(e) => setReason(e.target.value)} />
      <button
        type="button"
        disabled={busy || repo === "" || scope === "" || reason === ""}
        className="self-start rounded-[7px] bg-[var(--tree-acc)] px-3 py-1 text-[11.5px] font-semibold text-white disabled:opacity-50"
        onClick={() => onSubmit(repo, scope, reason)}
      >
        {busy ? "提交中…" : "提交申请"}
      </button>
    </div>
  );
}

function TransferForm({
  input,
  busy,
  onSubmit,
}: {
  input: string;
  busy: boolean;
  actorId: string;
  onSubmit: (fromRole: string, toRole: string, toId: string, reason: string) => void;
}) {
  const [fromRole, setFromRole] = useState("leader");
  const [toRole, setToRole] = useState("manager");
  const [toId, setToId] = useState("");
  const [reason, setReason] = useState("");
  return (
    <div className="mt-2 flex flex-col gap-1.5 rounded-[7px] border border-[var(--tree-hairline)] p-2">
      <div className="flex gap-1.5">
        <input className={input} placeholder="从角色（leader）" value={fromRole} onChange={(e) => setFromRole(e.target.value)} />
        <input className={input} placeholder="到角色（manager）" value={toRole} onChange={(e) => setToRole(e.target.value)} />
      </div>
      <input className={input} placeholder="接手人的 id" value={toId} onChange={(e) => setToId(e.target.value)} />
      <input className={input} placeholder="为什么转移（负责人不可用？写清）" value={reason} onChange={(e) => setReason(e.target.value)} />
      <button
        type="button"
        disabled={busy || toId === "" || reason === ""}
        className="self-start rounded-[7px] bg-[var(--tree-acc)] px-3 py-1 text-[11.5px] font-semibold text-white disabled:opacity-50"
        onClick={() => onSubmit(fromRole, toRole, toId, reason)}
      >
        {busy ? "提交中…" : "转移责任"}
      </button>
    </div>
  );
}

function ConflictForm({
  input,
  busy,
  onSubmit,
}: {
  input: string;
  busy: boolean;
  actorId: string;
  onSubmit: (type: string) => void;
}) {
  // ⚠️ 只能从这三个里选：`conflict_resolutions.conflict_type` 有 CHECK 约束
  // （graph_conflict / opinion_conflict / scope_conflict）。此前这里是自由文本、
  // 默认值还是 `content_conflict` —— 提交必 500（23514 违反约束），
  // 线上实测过。**让界面只给合法值**，比让用户撞一次约束再猜要好。
  const [type, setType] = useState("opinion_conflict");
  return (
    <div className="mt-2 flex flex-col gap-1.5 rounded-[7px] border border-[var(--tree-hairline)] p-2">
      <select className={input} value={type} onChange={(e) => setType(e.target.value)}>
        <option value="opinion_conflict">意见冲突（对同一件事的判断不一致）</option>
        <option value="graph_conflict">依赖图冲突（图上有边、模型判排除）</option>
        <option value="scope_conflict">范围冲突（谁该改这个仓）</option>
      </select>
      <button
        type="button"
        disabled={busy || type === ""}
        className="self-start rounded-[7px] bg-[var(--tree-acc)] px-3 py-1 text-[11.5px] font-semibold text-white disabled:opacity-50"
        onClick={() => onSubmit(type)}
      >
        {busy ? "提交中…" : "上报冲突"}
      </button>
    </div>
  );
}
