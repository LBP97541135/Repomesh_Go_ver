/** 下发任务树（方案 A「树贯穿一生」· 2026-09-17 用户确认）。
 *
 *  左栏唯一内容：Manager 组常驻一生；规划期挂 ①-⑤ 步骤节点（两个人工门长在
 *  树上），物化后步骤收拢、Leader 分组与任务行长出。视觉语言与已确认原型
 *  dispatch-tree-lifecycle.html 一致（线性图标、双主题令牌）。
 *
 *  本组件只消费状态渲染；步骤态从发现链读投影推导（deriveStepStates），
 *  任务行来自 plans/{id}/tasks 读面，不自行编造进度。 */

import { useMemo, useState } from "react";
import type { DiscoveryView } from "../../api/contract";
import type { PlanTaskItem } from "../../api/taskTree";
import type { TestEvidenceView } from "../../api/testEvidence";
import { STEP_LABELS, deriveStepStates, type FocusEntry, type StepState } from "./treeModel";
import { IconChevron, IconClock, IconFlask, IconRun, IconCheck, IconUser } from "./treeIcons";

export type { FocusEntry, StepState };

const STEP_PILL: Record<StepState, { text: string; cls: string }> = {
  done: { text: "已完成", cls: "border-olive/50 bg-olive/10 text-olive" },
  run: { text: "进行中", cls: "border-[var(--tree-acc)] bg-[var(--tree-acc)]/10 text-[var(--tree-acc)]" },
  gate: { text: "待人审", cls: "border-amber/40 bg-amber-well text-amber" },
  failed: { text: "失败", cls: "border-salmon/40 bg-salmon-well text-salmon" },
  wait: { text: "等待前序", cls: "border-[var(--tree-hairline)] bg-[var(--tree-zone)] text-[var(--tree-faint)]" },
  choose: { text: "待人选", cls: "border-amber/40 bg-amber-well text-amber" },
  confirm: { text: "待人确认", cls: "border-amber/40 bg-amber-well text-amber" },
};

const TASK_PILL: Record<string, { text: string; cls: string }> = {
  done: { text: "已完成", cls: "border-olive/50 bg-olive/10 text-olive" },
  running: { text: "进行中", cls: "border-[var(--tree-acc)] bg-[var(--tree-acc)]/10 text-[var(--tree-acc)]" },
  pending: { text: "待下发", cls: "border-amber/40 bg-amber-well text-amber" },
  assigned: { text: "已指派", cls: "border-[var(--tree-hairline)] bg-[var(--tree-zone)] text-[var(--tree-sub)]" },
  blocked: { text: "受阻", cls: "border-salmon/40 bg-salmon-well text-salmon" },
};

function taskPill(status: string) {
  return TASK_PILL[status] ?? { text: status, cls: TASK_PILL.assigned.cls };
}

/** 任务行左端状态图标：已完成=圈勾 / 进行中=同心圆 / 待下发=时钟（原型同款）。 */
function TaskStatusIcon({ status }: { status: string }) {
  if (status === "done") return <IconCheck size={12} className="flex-none text-olive" />;
  if (status === "running" || status === "assigned")
    return <IconRun size={12} className={`flex-none text-[var(--tree-acc)] ${status === "running" ? "animate-pulse" : ""}`} />;
  if (status === "blocked") return <IconClock size={12} className="flex-none text-salmon" />;
  return <IconClock size={12} className="flex-none text-amber" />;
}

/** 状态 → 步骤行左端数字圈的配色。 */
function stepIconCls(state: StepState): string {
  switch (state) {
    case "done":
      return "border-olive/50 bg-olive/10 text-olive";
    case "run":
      return "border-[var(--tree-acc)] text-[var(--tree-acc)]";
    case "gate":
    case "failed":
      return "border-amber/60 text-amber";
    default:
      return "border-[var(--tree-line)] text-[var(--tree-faint)]";
  }
}

export function DispatchTree({
  title,
  discovery,
  tasks,
  materialized,
  activeEntry,
  onOpen,
  testEvidence,
}: {
  title: string;
  discovery: DiscoveryView | null;
  /** 物化后的任务行；null = 未物化或读面未取到 */
  tasks: PlanTaskItem[] | null;
  materialized: boolean;
  activeEntry: FocusEntry | null;
  onOpen: (entry: FocusEntry) => void;
  /** 测试团队的真实记录；null = 还没取到 */
  testEvidence: TestEvidenceView | null;
}) {
  const [openLeader, setOpenLeader] = useState<string | null>(null);
  const stepStates = deriveStepStates(discovery);
  const doneSteps = stepStates.filter((s) => s === "done").length;

  /** Leader 分组（物化后）：有 leaderLabel 的进组，没有的（待下发）直挂 Manager。 */
  const groups = useMemo(() => {
    const map = new Map<string, PlanTaskItem[]>();
    if (!tasks) return { grouped: [...map.entries()], unassigned: [] as PlanTaskItem[] };
    const unassigned: PlanTaskItem[] = [];
    for (const t of tasks) {
      if (!t.leaderLabel) unassigned.push(t);
      else map.set(t.leaderLabel, [...(map.get(t.leaderLabel) ?? []), t]);
    }
    return { grouped: [...map.entries()], unassigned };
  }, [tasks]);

  const done = tasks?.filter((t) => t.status === "done").length ?? 0;
  const running = tasks?.filter((t) => t.status === "running" || t.status === "assigned").length ?? 0;
  const pending = tasks?.filter((t) => t.status === "pending" || t.status === "blocked").length ?? 0;
  const total = tasks?.length ?? 0;
  const pct = total > 0 ? done / total : 0;

  /** 测试组那一行说的话：有记录就说记录（几条、几条没过），没有就说还没有。
   *  2026-09-20 前这里写死「等待上游开发任务全部完成」，而记录其实一直在产生。 */
  const testSummary = (() => {
    if (!testEvidence || testEvidence.items.length === 0) return null;
    // 「不适用」的记录（例如单仓库计划的跨仓库联调）不该被算成"通过" ——
    // 它是"不需要做"，与"做了且过了"是两回事。约定：summary 以「不适用」开头。
    const skipped = testEvidence.items.filter((i) => (i.summary ?? "").startsWith("不适用")).length;
    const passed = testEvidence.items.filter((i) => i.passed && !(i.summary ?? "").startsWith("不适用")).length;
    const failed = testEvidence.items.length - passed - skipped;
    const kinds = new Set(testEvidence.items.map((i) => i.kind));
    const parts = [`${testEvidence.items.length} 条记录`];
    parts.push(`${passed} 通过`);
    if (failed > 0) parts.push(`${failed} 未过`);
    if (skipped > 0) parts.push(`${skipped} 不适用`);
    if (kinds.has("repo_integration")) parts.push("含节点集成");
    if (kinds.has("cross_repo_regression")) parts.push("含跨仓库联调");
    return parts.join(" · ");
  })();

  const hl = "bg-[rgba(94,106,210,.055)] shadow-[inset_0_0_0_1px_rgba(94,106,210,.22)]";

  return (
    <div className="flex h-full min-w-0 flex-col overflow-y-auto border-r border-[var(--tree-hairline)] bg-[var(--tree-bg)] px-4 py-4">
      {/* 概览 */}
      <div className="flex items-center gap-3 px-0.5 pb-3 text-[11px] text-[var(--tree-faint)]">
        <span className="min-w-0 truncate text-[var(--tree-sub)]">{title}</span>
        <span className="text-[var(--tree-line)]">·</span>
        {materialized ? (
          <span>
            执行中 <b className="font-semibold text-[var(--tree-ink)]">{done}/{total}</b>
          </span>
        ) : (
          <span>
            规划中 <b className="font-semibold text-[var(--tree-ink)]">{doneSteps}/5</b>
          </span>
        )}
      </div>

      {/* Manager 组：常驻一生 */}
      <div
        className={`relative rounded-[9px] bg-[var(--tree-mgr)] p-2.5 ${activeEntry?.kind === "mgr" ? hl : ""}`}
      >
        {materialized && (total > 0 || groups.grouped.length > 0) && (
          <span
            className="absolute -bottom-2 right-3 z-[1] rounded-[6px] border border-[color-mix(in_oklab,var(--tree-acc)_45%,transparent)] bg-[var(--tree-bg)] px-2 py-px text-[9.5px] text-[var(--tree-acc)]"
            title="编制徽章：Manager 麾下的 Leader 与任务数（方案 D 色环徽章继承）"
          >
            下辖 {groups.grouped.length} Leader · {total} 任务
          </span>
        )}
        <button
          className="flex w-full items-center gap-2 text-left"
          onClick={() => onOpen({ kind: "mgr" })}
          title="查看主会话时间线（需求→规划→下发）"
        >
          <IconUser size={14} className="flex-none text-[var(--tree-acc)]" />
          <span className="text-[12.5px] font-semibold text-[var(--tree-ink)]">Manager · 主脑</span>
          <span className="ml-auto rounded-[5px] border border-[var(--tree-hairline)] bg-[var(--tree-card)] px-1.5 py-px text-[10px] text-[var(--tree-sub)]">
            {materialized ? "执行中" : "规划中"}
          </span>
        </button>
        <div className="flex items-center gap-2 px-0.5 pt-2 text-[11px] text-[var(--tree-sub)]">
          <span className="flex-none">{materialized ? `汇总进度:${total} 个任务` : "汇总进度:5 个规划步骤"}</span>
          <span className="h-1 flex-1 overflow-hidden rounded-full bg-[var(--tree-line)]">
            {materialized ? (
              <>
                <span className="float-left h-full bg-olive" style={{ width: `${pct * 100}%` }} />
                <span className="float-left h-full bg-[var(--tree-acc)]" style={{ width: `${(running / Math.max(total, 1)) * 100}%` }} />
              </>
            ) : (
              <span className="float-left h-full bg-olive" style={{ width: `${(doneSteps / 5) * 100}%` }} />
            )}
          </span>
          <span className="flex-none">
            {materialized
              ? `${done} 已完成 / ${running} 进行 / ${pending} 待下发`
              : `${doneSteps} 完成 / ${stepStates.filter((s) => s === "run").length} 进行 / ${stepStates.filter((s) => s === "gate").length} 待人审`}
          </span>
        </div>

        {/* 规划步骤（物化前） */}
        {!materialized && (
          <div className="mt-1.5 flex flex-col gap-px pl-1">
            {STEP_LABELS.map((label, i) => {
              const st = stepStates[i];
              const pill = STEP_PILL[st];
              const active = activeEntry?.kind === "step" && activeEntry.step === i + 1;
              return (
                <div key={label}>
                  <button
                    className={`flex w-full items-center gap-2 rounded-[7px] px-2 py-1.5 text-left hover:bg-[rgba(94,106,210,.05)] ${active ? hl : ""}`}
                    onClick={() => onOpen({ kind: "step", step: (i + 1) as 1 | 2 | 3 | 4 | 5 })}
                  >
                    <span
                      className={`grid h-5 w-5 flex-none place-items-center rounded-full border-[1.5px] bg-[var(--tree-card)] text-[10px] font-bold ${stepIconCls(st)} ${st === "run" ? "animate-pulse" : ""}`}
                    >
                      {st === "done" ? "✓" : i + 1}
                    </span>
                    <span className={`text-[12px] ${st === "done" || st === "run" ? "font-medium text-[var(--tree-ink)]" : "text-[var(--tree-sub)]"}`}>
                      {String(i + 1).replace("1", "①").replace("2", "②").replace("3", "③").replace("4", "④").replace("5", "⑤")} {label}
                    </span>
                    <span className="ml-auto" />
                    <span className={`rounded-[5px] border px-1.5 py-px text-[10px] ${pill.cls}`}>{pill.text}</span>
                  </button>
                  {i < 4 && (
                    <span className={`ml-[19px] block h-1.5 w-px ${stepStates[i] === "done" ? "border-l border-olive/50" : "bg-[var(--tree-line)]"}`} />
                  )}
                </div>
              );
            })}
          </div>
        )}

        {/* 物化后：待下发任务直挂 Manager（批次2 等） */}
        {materialized &&
          groups.unassigned.map((t) => (
            <TaskRow key={t.id} task={t} active={activeEntry?.kind === "task" && activeEntry.taskId === t.id} hl={hl} onOpen={onOpen} />
          ))}
      </div>

      {/* 物化后：Leader 分组（互斥展开） */}
      {materialized &&
        groups.grouped.map(([leader, items]) => {
          const expanded = openLeader === leader;
          const activeHere = activeEntry?.kind === "task" && items.some((t) => t.id === activeEntry.taskId);
          return (
            <div key={leader}>
              <button
                className={`mt-0.5 flex w-full items-center gap-2 rounded-[7px] px-2 py-1.5 text-left hover:bg-[var(--tree-zone)] ${activeHere ? hl : ""}`}
                onClick={() => setOpenLeader(expanded ? null : leader)}
              >
                <span
                  className={`size-2 flex-none rounded-full border-2 transition-shadow ${
                    activeHere || expanded
                      ? "border-[var(--tree-role-l)] shadow-[0_0_0_2px_color-mix(in_oklab,var(--tree-acc)_45%,transparent)]"
                      : "border-[var(--tree-role-l)]/70 shadow-[0_0_0_2px_color-mix(in_oklab,var(--tree-acc)_22%,transparent)]"
                  }`}
                  title="Leader 色环 · Manager 光环：同属 Manager 麾下"
                />
                <span className="text-[12.5px] font-medium text-[var(--tree-ink)]">{leader}</span>
                <span className="ml-auto text-[11px] text-[var(--tree-faint)]">({items.length} 任务)</span>
                <IconChevron size={11} className={`flex-none text-[var(--tree-faint)] transition-transform ${expanded ? "rotate-90" : ""}`} />
              </button>
              <div className={`grid transition-all duration-300 ${expanded ? "grid-rows-[1fr] opacity-100" : "grid-rows-[0fr] opacity-0"}`}>
                <div className="overflow-hidden">
                  <div className="flex flex-col gap-px py-0.5 pl-8 pr-1.5">
                    {items.map((t) => (
                      <TaskRow key={t.id} task={t} active={activeEntry?.kind === "task" && activeEntry.taskId === t.id} hl={hl} onOpen={onOpen} />
                    ))}
                  </div>
                </div>
              </div>
            </div>
          );
        })}
      {materialized && tasks !== null && tasks.length === 0 && (
        <p className="mt-3 px-1 text-[11px] text-[var(--tree-faint)]">该计划还没有任务行（物化写入端未落批次数据时不摆假进度）。</p>
      )}

      {/* 测试组：常驻一生 */}
      <div className="mt-4 border-t border-[var(--tree-hairline)] pt-3">
        <button
          className={`flex w-full items-center gap-2 rounded-[7px] px-2 py-1.5 text-left hover:bg-[var(--tree-zone)] ${activeEntry?.kind === "tests" ? hl : ""}`}
          title={testSummary ?? "测试任务与结果"}
          onClick={() => onOpen({ kind: "tests" })}
        >
          <IconFlask size={13} className="flex-none text-amber" />
          <span className="text-[12.5px] font-medium text-[var(--tree-ink)]">测试组 · db-test</span>
          <span className="ml-auto text-[11px] text-[var(--tree-faint)]">
            {testSummary ?? (materialized ? "还没有记录" : "等待上游规划完成")}
          </span>
        </button>
      </div>
    </div>
  );
}

/** 任务行：状态图标 + T 号 + 标题 + 执行者 chip + 批次/状态 pill。 */
function TaskRow({
  task,
  active,
  hl,
  onOpen,
}: {
  task: PlanTaskItem;
  active: boolean;
  hl: string;
  onOpen: (entry: FocusEntry) => void;
}) {
  const pill = taskPill(task.status);
  return (
    <button
      className={`flex w-full items-center gap-2 rounded-[7px] px-2 py-1.5 text-left hover:bg-[var(--tree-zone)] ${active ? hl : ""}`}
      onClick={() => onOpen({ kind: "task", taskId: task.id })}
    >
      <TaskStatusIcon status={task.status} />
      <span className="flex-none font-mono text-[10.5px] text-[var(--tree-faint)]">{task.taskUid ?? "—"}</span>
      <span className="min-w-0 flex-1 truncate text-[12px] font-medium text-[var(--tree-ink)]">{task.title}</span>
      <span className="flex-none rounded-[5px] border border-[var(--tree-hairline)] bg-[var(--tree-card)] px-1.5 py-px text-[10px] text-[var(--tree-faint)]">
        {/* 执行者：优先装配期写下的显示名；没有就显示**真的跑过这条任务的 agent**
            （后端任务树读面带出的 assignee）；两个都没有才写「待指派」——
            此前物化写入端不填 workerLabel，于是每条跑完的任务都显示「待指派」。 */}
        {task.workerLabel || task.assignee || "待指派"}
      </span>
      <span className="flex-none rounded-[5px] border border-[var(--tree-hairline)] bg-[var(--tree-zone)] px-1.5 py-px text-[10px] text-[var(--tree-sub)]">
        批次{task.batchNo ?? "—"}
      </span>
      <span className={`flex-none rounded-[5px] border px-1.5 py-px text-[10px] ${pill.cls}`}>{pill.text}</span>
    </button>
  );
}
