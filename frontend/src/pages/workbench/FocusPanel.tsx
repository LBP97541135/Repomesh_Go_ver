/** 右栏·焦点详情（方案 A）：点哪显示哪。
 *
 *  - 规划步骤：详情卡（关键词/候选/三档/批次）+ 人工门按钮（批准分档、确认物化）
 *  - Manager：主会话完整时间线（需求→规划→下发，即回看入口）
 *  - 任务：任务元信息 + 该任务协作房间的消息时序流
 *  - 输入框：由页面配置（发消息进当前会话 / 回答追问），真端点在页面接
 *  - PR 列车：交付期由页面塞到顶部插槽
 *
 *  消息渲染：只有真实房间消息才渲染成聊天气泡（契约 Q4 硬约束），角色由
 *  actor_id 前缀推导（agent_<role>[_<name>]），推导不出按系统条目样式。 */

import { useEffect, useRef, useState, type ReactNode } from "react";
import { IconCheck, IconClock, IconRun, IconSend } from "./treeIcons";
import type { DiscoveryProducer, DiscoveryView } from "../../api/contract";
import type { PlanTaskItem } from "../../api/taskTree";
import type { ConversationMessage } from "../../api/conversations";
import type { FocusEntry, StepState } from "./treeModel";
import { SupervisionPolicyCard, type PolicyDraftState } from "../../components/SupervisionPolicyCard";

/** 消息作者 → 角色显示。先看 authorKind（user 是人），服务侧 agent 再按
 *  roster 命名约定（agent_<role>[_<name>]）推导；都推不出按系统条目样式。 */
function actorOf(authorKind: string, actorId: string): { label: string; role: string; cls: string; letter: string } {
  if (authorKind === "user") return { label: "你", role: "用户", cls: "u", letter: "你" };
  const parts = actorId.split("_");
  if (parts[0] === "agent" && parts.length >= 2) {
    const role = parts[1].charAt(0).toUpperCase() + parts[1].slice(1);
    const name = parts.slice(2).join("_") || role;
    const cls = parts[1] === "manager" ? "m" : parts[1].startsWith("leader") ? "l" : "w";
    return { label: name, role, cls, letter: cls === "m" ? "M" : cls === "l" ? "L" : "W" };
  }
  return { label: actorId, role: "系统", cls: "s", letter: "·" };
}

/** 产出者一行：谁产的、用哪把技能、哪个 run。
 *
 *  规划期的结论由角色 agent 产出，界面必须说清是谁 —— 审计要的就是这个：
 *  结论不再是一个匿名 JSON。老快照没有 producer（后端自己算的那批），如实不显示。 */
function ProducerLine({ producer }: { producer?: DiscoveryProducer }) {
  if (!producer) return null;
  return (
    <p className="mt-1.5 text-[10.5px] leading-[1.7] text-[var(--tree-faint)]">
      产出：{producer.role} · 技能 {producer.skill_id} · run {producer.run_id.slice(0, 14)}
    </p>
  );
}

const AVA_CLS: Record<string, string> = {
  m: "bg-[var(--tree-acc)]",
  l: "bg-[var(--tree-role-l)]",
  w: "bg-olive",
  u: "bg-[#b8860b]",
  s: "bg-[#a3a3a0]",
};

const RPILL_CLS: Record<string, string> = {
  m: "bg-[var(--tree-acc)]/12 text-[var(--tree-acc)]",
  l: "bg-[var(--tree-role-l)]/12 text-[var(--tree-role-l)]",
  w: "bg-olive/12 text-olive",
  u: "bg-amber-well text-amber",
  s: "bg-[var(--tree-zone)] text-[var(--tree-sub)]",
};

function hhmm(at: string): string {
  return at.slice(11, 16);
}

/** 消息时间线（MGR / 任务共用）。轮询刷新只在用户本就贴底时跟到底——
 *  上翻读历史时不抢滚动。 */
function MessageTimeline({ messages }: { messages: ConversationMessage[] }) {
  const ref = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 120;
    if (nearBottom) el.scrollTop = el.scrollHeight;
  }, [messages]);
  return (
    <div ref={ref} className="flex flex-col gap-3 overflow-y-auto px-4 py-3.5">
      {messages.length === 0 && (
        <p className="pl-[31px] text-[10.5px] text-[var(--tree-faint)]">这个房间还没有消息。</p>
      )}
      {messages.map((m) => {
        const actor = actorOf(m.authorKind, m.actorId);
        return (
          <div key={m.id} className="flex gap-2.5">
            <span className={`grid h-[22px] w-[22px] flex-none place-items-center rounded-full text-[8.5px] font-bold text-white ${AVA_CLS[actor.cls]}`}>
              {actor.letter}
            </span>
            <div className="min-w-0 flex-1">
              <div className="mb-0.5 flex items-center gap-1.5">
                <span className="text-[11px] font-semibold text-[var(--tree-ink)]">{actor.label}</span>
                <span className={`rounded-[5px] px-1.5 py-px text-[9.5px] ${RPILL_CLS[actor.cls]}`}>{actor.role}</span>
                <span className="ml-auto font-mono text-[10px] text-[var(--tree-faint)]">{hhmm(m.createdAt)}</span>
              </div>
              <div className="whitespace-pre-wrap break-words rounded-lg border border-[var(--tree-hairline)] bg-[var(--tree-card)] px-2.5 py-1.5 text-[11.5px] leading-[1.65] text-[var(--tree-ink)] shadow-[0_1px_2px_rgba(15,15,15,.03)]">
                {m.body}
              </div>
            </div>
          </div>
        );
      })}
    </div>
  );
}

export interface FocusPanelProps {
  entry: FocusEntry | null;
  discovery: DiscoveryView | null;
  stepStates: StepState[];
  task: PlanTaskItem | null;
  /** 当前 entry 的会话消息（MGR=主会话 / 任务=该任务房间）；null=加载中或不可用 */
  messages: ConversationMessage[] | null;
  /** 人工门动作（页面持写回路） */
  onGate: (action: "approveTiers" | "materialize") => void;
  gateBusy: "approveTiers" | "materialize" | null;
  gateError: string | null;
  /** 失败步重试 */
  onRetryStep: (step: 1 | 2 | 3 | 4) => void;
  /** 这一步推进失败的原因（驱动器不再静默重试，把原因摆到界面上）。 */
  stepError: { step: number; message: string } | null;
  /** 「忽略追问，强制继续」：需求信息偏少时给用户的另一条路。 */
  onForceContinue: () => void;
  /** 经理门（审核段）：blocked 任务的通过/驳回。 */
  onDecideTask: (taskId: string, decision: "approve" | "reject", reason: string) => Promise<void>;
  /** 监管策略草稿卡片的合成态（由页面的拓扑态 + 草稿态合成，见 useIssueFlowState） */
  policyCard: PolicyDraftState;
  onConfigurePolicy: () => void;
  onRetryPolicy: () => void;
  /** 输入框配置；null=不渲染输入 */
  input: { placeholder: string; sending: boolean; disabled?: boolean; onSend: (text: string) => void } | null;
  /** 交付期到达且人工参与:合并确认也作为一条带按钮的消息出现在 Manager 房间 */
  mergePending?: boolean;
  onConfirmMerge?: () => void;
}

export function FocusPanel({
  entry,
  discovery,
  stepStates,
  task,
  messages,
  onGate,
  gateBusy,
  gateError,
  onRetryStep,
  stepError,
  onForceContinue,
  onDecideTask,
  policyCard,
  onConfigurePolicy,
  onRetryPolicy,
  input,
  mergePending = false,
  onConfirmMerge,
}: FocusPanelProps) {
  const body = (() => {
    if (entry === null) {
      return (
        <div className="flex flex-1 items-center justify-center px-6 text-center text-[11.5px] leading-[1.8] text-[var(--tree-faint)]">
          点击左侧步骤 / 任务行，这里显示详情、人工门操作与消息时序流
        </div>
      );
    }
    if (entry.kind === "step") return <StepDetail step={entry.step} state={stepStates[entry.step - 1]} discovery={discovery} onGate={onGate} gateBusy={gateBusy} gateError={gateError} onRetryStep={onRetryStep} stepError={stepError} onForceContinue={onForceContinue} policyCard={policyCard} onConfigurePolicy={onConfigurePolicy} onRetryPolicy={onRetryPolicy} messages={messages} />;
    if (entry.kind === "task") {
      return (
        <div className="flex min-h-0 flex-1 flex-col">
          {task === null ? (
            <p className="px-4 py-3 text-[11px] text-[var(--tree-faint)]">任务行数据未取到，不摆假详情。</p>
          ) : messages === null ? (
            <div className="flex flex-1 items-center justify-center text-[11px] text-[var(--tree-faint)]">消息流加载中…</div>
          ) : (
            <MessageTimeline messages={messages} />
          )}
          {/* 经理门（审核段）：agent 跑完把任务置 blocked 等经理批 —— 此前后端有
              approve/reject 端点、前端也有调用封装，但**界面上没有任何入口**，
              于是任务永远停在 blocked，整条链看起来"卡住"。 */}
          {task !== null && task.status === "blocked" && (
            <TaskGate task={task} onDecide={onDecideTask} />
          )}
          <PlanHistory discovery={discovery} />
        </div>
      );
    }
    // MGR：主会话完整时间线（回看入口）+ 待人审计项就地成为消息
    return (
      <div className="flex min-h-0 flex-1 flex-col">
        {messages === null ? (
          <div className="flex flex-1 items-center justify-center text-[11px] text-[var(--tree-faint)]">主会话加载中…</div>
        ) : (
          <MessageTimeline messages={messages} />
        )}
        <PlanHistory discovery={discovery} />
        <GateStack
          stepStates={stepStates}
          mergePending={mergePending}
          onGate={onGate}
          onConfirmMerge={onConfirmMerge}
          gateBusy={gateBusy}
          gateError={gateError}
        />
      </div>
    );
  })();

  const header = (() => {
    if (entry === null) return { title: "选择条目查看详情", role: "—", batch: "—", cls: "s" };
    if (entry.kind === "step") {
      const titles = ["① 需求分析", "② 候选评分", "③ 分档审批", "④ 生成计划", "⑤ 物化确认"];
      return { title: titles[entry.step - 1], role: "Manager", batch: `规划 · 步骤 ${entry.step}`, cls: "m" };
    }
    if (entry.kind === "task") {
      return {
        title: task ? `${task.taskUid ?? ""} ${task.title}`.trim() : "任务详情",
        role: task?.leaderLabel ?? "待指派",
        batch: `批次${task?.batchNo ?? "—"}`,
        cls: "l",
      };
    }
    return { title: "Manager · 主会话时间线", role: "Manager", batch: "主会话", cls: "m" };
  })();

  return (
    <aside className="flex h-full w-[400px] flex-none flex-col border-l border-line bg-[var(--tree-bg)]">
      <div className="border-b border-[var(--tree-hairline)] px-4 pb-2.5 pt-3.5">
        <p className="text-[13px] font-semibold leading-[1.45] text-[var(--tree-ink)]">{header.title}</p>
        <div className="mt-2 flex flex-wrap items-center gap-1.5">
          <span className={`rounded-[5px] px-1.5 py-px text-[9.5px] ${RPILL_CLS[header.cls]}`}>{header.role}</span>
          <span className="rounded-[5px] bg-[var(--tree-zone)] px-1.5 py-px text-[10px] text-[var(--tree-sub)]">{header.batch}</span>
        </div>
      </div>
      {body}
      {input && <FocusInput {...input} />}
    </aside>
  );
}

/** 待人审计项:就地成为 Manager 房间里的带按钮消息(「需要人的地方成为消息」
 *  惯例)。分档审批 / 物化确认 / 合并确认,与树上步骤卡共用同一套写回路。 */
function GateStack({
  stepStates,
  mergePending,
  onGate,
  onConfirmMerge,
  gateBusy,
  gateError,
}: {
  stepStates: StepState[];
  mergePending: boolean;
  onGate: (action: "approveTiers" | "materialize") => void;
  onConfirmMerge?: () => void;
  gateBusy: "approveTiers" | "materialize" | null;
  gateError: string | null;
}) {
  const cards: Array<{ key: string; title: string; desc: string; label: string; action: () => void; busy: boolean }> = [];
  if (stepStates[2] === "gate") {
    cards.push({
      key: "tiers",
      title: "分档审批 · 待人审",
      desc: "候选仓库已定档,批准后处理员继续生成计划",
      label: "批准分档",
      action: () => onGate("approveTiers"),
      busy: gateBusy === "approveTiers",
    });
  }
  if (stepStates[4] === "gate") {
    cards.push({
      key: "materialize",
      title: "物化确认 · 待人审",
      desc: "计划 v1 已就绪,确认后组建编制并下发批次1",
      label: "确认物化并开工",
      action: () => onGate("materialize"),
      busy: gateBusy === "materialize",
    });
  }
  if (mergePending) {
    cards.push({
      key: "merge",
      title: "交付序列 · 待确认合并",
      desc: "全部任务完成、门禁就绪,按依赖顺序执行合并",
      label: "确认合并",
      action: () => onConfirmMerge?.(),
      busy: false,
    });
  }
  if (cards.length === 0) return null;
  return (
    <div className="flex flex-col gap-2.5 border-t border-dashed border-[var(--tree-hairline)] px-4 py-3">
      {cards.map((c) => (
        <div key={c.key} className="flex gap-2.5">
          <span className="mt-0.5 grid h-[22px] w-[22px] flex-none place-items-center rounded-full bg-amber-well text-amber">
            <IconClock size={12} />
          </span>
          <div className="min-w-0 flex-1">
            <div className="mb-0.5 flex items-center gap-1.5">
              <span className="text-[11px] font-semibold text-[var(--tree-ink)]">{c.title}</span>
            </div>
            <div className="rounded-lg border border-[var(--tree-hairline)] bg-[var(--tree-card)] px-2.5 py-2">
              <p className="text-[11px] leading-[1.6] text-[var(--tree-sub)]">{c.desc}</p>
              <button
                type="button"
                disabled={c.busy}
                onClick={c.action}
                className="mt-1.5 rounded-[7px] border border-amber/40 bg-amber-well px-3 py-1 text-[11.5px] font-semibold text-amber hover:bg-amber-well/80 disabled:opacity-50"
              >
                {c.busy ? "提交中…" : c.label}
              </button>
            </div>
          </div>
        </div>
      ))}
      {gateError && (
        <p className="rounded-[7px] border border-salmon/40 bg-salmon-well px-2.5 py-1.5 text-[11px] text-salmon">{gateError}</p>
      )}
    </div>
  );
}

function FocusInput({  placeholder,
  sending,
  disabled,
  onSend,
}: {
  placeholder: string;
  sending: boolean;
  disabled?: boolean;
  onSend: (text: string) => void;
}) {
  const ref = useRef<HTMLTextAreaElement | null>(null);
  const send = () => {
    const el = ref.current;
    if (!el) return;
    const text = el.value.trim();
    if (!text || sending || disabled) return;
    onSend(text);
    el.value = "";
  };
  return (
    <div className="border-t border-[var(--tree-hairline)] p-2.5">
      <div className="flex items-center gap-2 rounded-[7px] border border-[var(--tree-line)] bg-[var(--tree-card)] px-3 py-1.5">
        <textarea
          ref={ref}
          rows={1}
          disabled={disabled || sending}
          placeholder={placeholder}
          className="max-h-24 min-w-0 flex-1 resize-none bg-transparent text-[11.5px] leading-[1.6] text-[var(--tree-ink)] outline-none placeholder:text-[var(--tree-faint)]"
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey) {
              e.preventDefault();
              send();
            }
          }}
        />
        <button
          type="button"
          className="grid flex-none place-items-center rounded text-[var(--tree-acc)] disabled:text-[#c9c9c5]"
          disabled={disabled || sending}
          onClick={send}
          title="发送（Enter）"
        >
          <IconSend size={14} />
        </button>
      </div>
    </div>
  );
}

/* ── 规划步骤详情卡 ── */

/** 经理门卡片：blocked 任务的通过 / 驳回（驳回必须给原因 —— 后端会拒空原因）。 */
function TaskGate({
  task,
  onDecide,
}: {
  task: PlanTaskItem;
  onDecide: (taskId: string, decision: "approve" | "reject", reason: string) => Promise<void>;
}) {
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState<"approve" | "reject" | null>(null);
  const [error, setError] = useState<string | null>(null);
  const decide = (decision: "approve" | "reject") => {
    setBusy(decision);
    setError(null);
    onDecide(task.id, decision, reason.trim())
      .then(() => setReason(""))
      .catch((err: unknown) => setError(err instanceof Error ? err.message : String(err)))
      .finally(() => setBusy(null));
  };
  return (
    <div className="border-t border-dashed border-[var(--tree-hairline)] px-4 py-3">
      <div className="microlabel pb-1.5">经理门 · 待人审</div>
      <p className="text-[11.5px] leading-[1.7] text-[var(--tree-sub)]">
        {task.workerLabel ?? "执行者"} 已跑完并把任务交回。通过则任务置为完成；驳回会带原因退回，可再次派发。
      </p>
      <input
        className="mt-2 w-full rounded-[7px] border border-[var(--tree-line)] bg-[var(--tree-card)] px-2.5 py-1.5 text-[11.5px] text-[var(--tree-ink)] placeholder:text-[var(--tree-faint)] focus:border-[var(--tree-acc)] focus:outline-none"
        placeholder="审批意见 / 驳回原因"
        value={reason}
        onChange={(e) => setReason(e.target.value)}
      />
      <div className="mt-2 flex gap-1.5">
        <button
          className="rounded-[7px] bg-[var(--tree-acc)] px-3 py-1 text-[11.5px] font-semibold text-white disabled:opacity-50"
          disabled={busy !== null}
          onClick={() => decide("approve")}
        >
          {busy === "approve" ? "提交中…" : "通过"}
        </button>
        <button
          className="rounded-[7px] border border-[var(--tree-line)] px-3 py-1 text-[11.5px] text-[var(--tree-ink)] hover:border-salmon hover:text-salmon disabled:opacity-50"
          disabled={busy !== null || reason.trim() === ""}
          onClick={() => decide("reject")}
        >
          {busy === "reject" ? "提交中…" : "驳回"}
        </button>
      </div>
      {error && <p className="mt-1.5 text-[11px] text-salmon">{error}</p>}
    </div>
  );
}

/** 规划记录（只读回看）：物化后左树换成任务视图，规划那 5 步与它们的产物
 *  就从界面上消失了 —— 用户实测的原话是"进入执行页面后看不到规划页面的记录"。
 *  这里把 5 步的结论与**产出者**并排摊开，任何一步都能追到是谁产的。
 *
 *  数据源就是 `discovery`（工作台仍在轮询它），所以这不是另一份真相。 */
function PlanHistory({ discovery }: { discovery: DiscoveryView | null }) {
  const [open, setOpen] = useState(false);
  if (!discovery) return null;
  const a = discovery.analysis;
  const c = discovery.candidates;
  const k = discovery.classification;
  const integration = discovery.integration;
  const mat = discovery.materialization;
  const rows: Array<{ step: string; body: ReactNode }> = [
    {
      step: "① 需求分析",
      body: a ? (
        <>
          <span>{(a.extracted_keywords ?? []).join(" · ") || "（无关键词）"}</span>
          <ProducerLine producer={a.producer} />
        </>
      ) : (
        <span>未跑</span>
      ),
    },
    {
      step: "② 候选评分",
      body: c ? (
        <>
          <span>{c.items.length} 个候选 · {(c.items[0]?.repository_name ?? "—")}</span>
          <ProducerLine producer={c.producer} />
        </>
      ) : (
        <span>未跑</span>
      ),
    },
    {
      step: "③ 分档审批",
      body: k ? (
        <span>
          必改 {k.required.length} · 可能 {k.maybe.length} · 排除 {k.excluded.length} ·{" "}
          {discovery.approval.state === "approved" ? "已批准" : "待批准"}
        </span>
      ) : (
        <span>未跑</span>
      ),
    },
    {
      step: "④ 生成计划",
      body: integration ? (
        <>
          <span>{integration.task_dag_count} 个任务</span>
          <ProducerLine producer={integration.producer} />
        </>
      ) : (
        <span>未跑</span>
      ),
    },
    {
      step: "⑤ 物化确认",
      body: mat ? <span>{mat.status}{mat.at ? ` · ${String(mat.at).slice(0, 16)}` : ""}</span> : <span>未物化</span>,
    },
  ];
  return (
    <div className="border-t border-dashed border-[var(--tree-hairline)] px-4 py-2.5">
      <button
        className="microlabel flex w-full items-center gap-1.5 text-left hover:text-[var(--tree-acc)]"
        onClick={() => setOpen((v) => !v)}
      >
        <span>{open ? "▾" : "▸"}</span> 规划记录（只读回看）
      </button>
      {open && (
        <div className="mt-1.5 flex flex-col gap-1.5">
          {rows.map((row) => (
            <div key={row.step} className="text-[11px] leading-[1.7] text-[var(--tree-sub)]">
              <span className="text-[var(--tree-ink)]">{row.step}</span>
              <span className="pl-1.5">{row.body}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function CardShell({ title, tone = "plain", children }: { title: string; tone?: "plain" | "done" | "gate"; children: ReactNode }) {
  const toneCls =
    tone === "done" ? "text-olive" : tone === "gate" ? "text-amber" : "text-[var(--tree-acc)]";
  return (
    <div className="rounded-[9px] border border-[var(--tree-hairline)] bg-[var(--tree-card)] p-3">
      <p className={`mb-1.5 flex items-center gap-1.5 text-[12px] font-semibold text-[var(--tree-ink)]`}>
        {tone === "done" ? <IconCheck size={12} className={toneCls} /> : tone === "gate" ? <IconClock size={12} className={toneCls} /> : <IconRun size={12} className={toneCls} />}
        {title}
      </p>
      {children}
    </div>
  );
}

function StepDetail({
  step,
  state,
  discovery,
  onGate,
  gateBusy,
  gateError,
  onRetryStep,
  stepError,
  onForceContinue,
  policyCard,
  onConfigurePolicy,
  onRetryPolicy,
  messages,
}: {
  step: 1 | 2 | 3 | 4 | 5;
  state: StepState;
  discovery: DiscoveryView | null;
  onGate: (action: "approveTiers" | "materialize") => void;
  gateBusy: "approveTiers" | "materialize" | null;
  gateError: string | null;
  onRetryStep: (step: 1 | 2 | 3 | 4) => void;
  stepError: { step: number; message: string } | null;
  onForceContinue: () => void;
  policyCard: PolicyDraftState;
  onConfigurePolicy: () => void;
  onRetryPolicy: () => void;
  messages: ConversationMessage[] | null;
}) {
  const wrap = (cards: ReactNode) => (
    <div className="flex flex-col gap-3 overflow-y-auto px-4 py-3.5">
      {state === "failed" && (
        <div className="rounded-[9px] border border-salmon/40 bg-salmon-well p-3">
          <p className="text-[11.5px] text-salmon">这一步执行失败。{discovery?.analysis?.error ? String(discovery.analysis.error) : ""}</p>
          <button
            className="mt-2 rounded-[7px] border border-[var(--tree-line)] bg-[var(--tree-card)] px-3 py-1 text-[11.5px] text-[var(--tree-ink)] hover:border-[var(--tree-acc)]"
            onClick={() => onRetryStep(step as 1 | 2 | 3 | 4)}
          >
            重试这一步
          </button>
        </div>
      )}
      {/* 驱动器推进失败的原因：此前是静默吞掉 + 每 5s 重发（用户什么都看不到）。 */}
      {stepError && stepError.step === step && (
        <div className="rounded-[9px] border border-salmon/40 bg-salmon-well p-3">
          <p className="text-[11.5px] leading-[1.7] text-salmon">这一步没能推进：{stepError.message}</p>
          <p className="mt-1 text-[11px] text-[var(--tree-sub)]">改完上面的东西再点「重试这一步」；如果原因写着「分析未通过」，也可以直接忽略追问继续。</p>
        </div>
      )}
      {cards}
      {messages !== null && messages.length > 0 && step <= 2 && (
        <div className="min-h-0">
          <p className="mb-1 text-[10px] tracking-widest text-[var(--tree-faint)]">分析期对话</p>
          <MessageTimeline messages={messages.slice(0, step === 1 ? 2 : 3)} />
        </div>
      )}
    </div>
  );

  if (step === 1) {
    const a = discovery?.analysis ?? null;
    return wrap(
      a ? (
        <CardShell title="需求已解析" tone="done">
          <div className="flex flex-wrap gap-1.5">
            {(a.extracted_keywords ?? []).map((k) => (
              <span key={k} className="rounded-full border border-[var(--tree-hairline)] bg-[var(--tree-zone)] px-2 py-px text-[10.5px] text-[var(--tree-sub)]">{k}</span>
            ))}
          </div>
          <div className="mt-2">
            {(a.dimensions ?? []).map((d) => (
              <p key={d.name} className="flex gap-1.5 py-px text-[11px] text-[var(--tree-sub)]">
                <span className={d.covered ? "text-olive" : "text-tx3"}>{d.covered ? "✓" : "○"}</span>
                <span>
                  <span className="text-[var(--tree-ink)]">{d.name}</span>：{d.note || (d.covered ? "已说清" : "词面上没提到")}
                </span>
              </p>
            ))}
            <p className="mt-1 text-[10.5px] leading-[1.7] text-[var(--tree-faint)]">
              ○ 只表示**这段话里没有出现**该维度的常见说法，不代表你没说清——判定按词面，真实需求常被漏判。
            </p>
            <ProducerLine producer={a.producer} />
          </div>

          {/* 需求确实太短（真·信息不足）时才追问，并给「忽略追问继续」这条出路。
              此前界面只有四个 ✕、没有追问列表、也没有继续的按钮 —— 用户走到这里
              就没有下一步了（实测：整条链卡在第一步）。 */}
          {!a.sufficient && (
            <div className="mt-2 rounded-[9px] border border-amber/40 bg-amber-well px-2.5 py-2">
              <p className="text-[11.5px] text-amber">
                需求文本偏短，处理员想先问几句 —— 也可以在右栏输入框回答，或直接忽略追问继续。
              </p>
              {(a.questions ?? []).length > 0 && (
                <ul className="mt-1 list-disc pl-4 text-[11px] leading-[1.7] text-[var(--tree-sub)]">
                  {(a.questions ?? []).map((q) => (
                    <li key={q}>{q}</li>
                  ))}
                </ul>
              )}
              <button
                className="mt-1.5 rounded-[7px] border border-amber/40 bg-amber-well px-2.5 py-[3px] text-[11.5px] font-semibold text-amber hover:bg-amber-well/80"
                onClick={onForceContinue}
              >
                忽略追问，强制继续
              </button>
            </div>
          )}
        </CardShell>
      ) : (
        <CardShell title="正在分析需求">{state === "wait" ? <p className="text-[11px] text-[var(--tree-sub)]">处理员会自动开始分析；也可以稍后手动触发。</p> : <p className="text-[11px] text-[var(--tree-sub)]">分析进行中…</p>}</CardShell>
      ),
    );
  }
  if (step === 2) {
    const c = discovery?.candidates ?? null;
    return wrap(
      c ? (
        <CardShell title={`候选仓库 ${c.items.length} 个`} tone="done">
          <ProducerLine producer={c.producer} />
          {c.items.map((it) => (
            <div key={it.repository_id} className="flex items-center gap-2 py-0.5 text-[11.5px]">
              <span className="font-mono text-[11px] text-[var(--tree-ink)]">{it.repository_name}</span>
              <span className="rounded-[5px] bg-[var(--tree-zone)] px-1.5 py-px text-[10px] text-[var(--tree-sub)]">{it.score.toFixed(2)}</span>
              {it.is_entry_point && <span className="rounded-[5px] border border-[var(--tree-acc)] bg-[var(--tree-acc)]/10 px-1.5 py-px text-[10px] text-[var(--tree-acc)]">入口仓</span>}
              <span className="ml-auto min-w-0 truncate text-[10.5px] text-[var(--tree-faint)]" title={it.rationale ?? ""}>{it.rationale ?? ""}</span>
            </div>
          ))}
        </CardShell>
      ) : (
        <CardShell title="候选评分">{state === "run" ? <p className="text-[11px] text-[var(--tree-sub)]">正在评估项目仓库目录中的候选…</p> : <p className="text-[11px] text-[var(--tree-sub)]">等待需求分析完成。</p>}</CardShell>
      ),
    );
  }
  if (step === 3) {
    const cls = discovery?.classification ?? null;
    const approved = discovery?.approval?.state === "approved";
    return wrap(
      cls ? (
        <CardShell title={approved ? "分档已确认" : "分档结果 · 待人审"} tone={approved ? "done" : "gate"}>
          {(["required", "maybe", "excluded"] as const).map((tier) => (
            <div key={tier} className="flex items-center gap-2 py-0.5 text-[11.5px]">
              <span className="w-8 flex-none text-[10.5px] text-[var(--tree-faint)]">{{ required: "必需", maybe: "可能", excluded: "排除" }[tier]}</span>
              <span className="text-[var(--tree-ink)]">{cls[tier].length > 0 ? cls[tier].map((r) => r.repository).join("、") : "无"}</span>
            </div>
          ))}
          {!approved && (
            <div className="mt-2.5 flex gap-2">
              <button
                className="rounded-[7px] border border-amber/40 bg-amber-well px-3.5 py-1.5 text-[12px] font-semibold text-amber hover:bg-amber-well disabled:opacity-50"
                disabled={gateBusy === "approveTiers"}
                onClick={() => onGate("approveTiers")}
              >
                {gateBusy === "approveTiers" ? "提交中…" : "批准分档"}
              </button>
            </div>
          )}
        </CardShell>
      ) : (
        <CardShell title="分档审批">{state === "run" ? <p className="text-[11px] text-[var(--tree-sub)]">正在生成候选分档…</p> : <p className="text-[11px] text-[var(--tree-sub)]">等待候选评分完成。</p>}</CardShell>
      ),
    );
  }
  if (step === 4) {
    const integration = discovery?.integration ?? null;
    return wrap(
      <CardShell title={integration ? `计划 v1 · ${integration.task_dag_count} 任务 ${integration.batch_count} 批` : "生成计划"} tone={integration ? "done" : "plain"}>
        <ProducerLine producer={integration?.producer} />
        {integration ? (
          <p className="text-[11px] leading-[1.7] text-[var(--tree-sub)]">
            批次明细的读面（计划纸面快照）在 Go 后端尚未迁移，批次划分以物化确认卡与任务树为准。
          </p>
        ) : state === "run" ? (
          <p className="text-[11px] text-[var(--tree-sub)]">正在生成计划…</p>
        ) : (
          <p className="text-[11px] text-[var(--tree-sub)]">等待分档审批通过后自动生成。</p>
        )}
      </CardShell>,
    );
  }
  // step 5
  const materialized = discovery?.materialization?.status === "materialized";
  return wrap(
    <>
      {/* 监管策略卡片（§3.1）：与物化按钮同级。**只在拓扑还不存在时**才有按钮
          —— 拓扑一落地档案就锁死了（§3.4），卡片态由 useIssueFlowState 合成。 */}
      {!materialized && (
        <SupervisionPolicyCard state={policyCard} onConfigure={onConfigurePolicy} onRetry={onRetryPolicy} />
      )}
      <CardShell title={materialized ? "已物化并开工" : "物化并开工"} tone={materialized ? "done" : "gate"}>
      {materialized ? (
        <p className="text-[11px] leading-[1.7] text-[var(--tree-sub)]">编制已组装、批次已下发——左侧树已换代为任务视图，点任务行查看各自房间的消息流。</p>
      ) : (
        <>
          <p className="pb-2 text-[10.5px] leading-[1.7] text-[var(--tree-faint)]">将按计划 v1 组建编制并下发批次1；确认后左侧树换代为任务视图。</p>
          <button
            className="rounded-[7px] bg-[var(--tree-acc)] px-3.5 py-1.5 text-[12px] font-semibold text-white hover:bg-[var(--tree-acc)] disabled:opacity-50"
            disabled={gateBusy === "materialize"}
            onClick={() => onGate("materialize")}
          >
            {gateBusy === "materialize" ? "物化中…" : "确认物化并开工"}
          </button>
        </>
      )}
        {gateError && <p className="mt-2 rounded-[7px] border border-salmon/40 bg-salmon-well px-2.5 py-1.5 text-[11px] text-salmon">{gateError}</p>}
      </CardShell>
    </>,
  );
}
