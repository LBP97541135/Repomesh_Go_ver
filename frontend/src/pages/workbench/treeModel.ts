/** 下发任务树的推导逻辑（方案 A「树贯穿一生」）。
 *
 *  纯数据推导，与渲染分离（沿 streamModel 的边界惯例）：步骤态从发现链
 *  读投影推导，人工门（③分档审批 / ⑤物化确认）没过就停在「待人审」，
 *  不编造进度。 */

import type { DiscoveryView } from "../../api/contract";

/** 右栏焦点条目：Manager 主会话 / 规划步骤 / 任务。 */
export type FocusEntry =
  | { kind: "mgr" }
  | { kind: "step"; step: 1 | 2 | 3 | 4 | 5 }
  | { kind: "task"; taskId: string };

export type StepState = "done" | "run" | "gate" | "wait" | "failed";

export const STEP_LABELS = ["需求分析", "候选评分", "分档审批", "生成计划", "物化确认"] as const;

/** 规划步骤态：①②④跟 artifact 走（analysis/candidates/plan），③⑤是人工门
 *  （approval.state / materialization.status）。 */
export function deriveStepStates(d: DiscoveryView | null): StepState[] {
  const states: StepState[] = ["wait", "wait", "wait", "wait", "wait"];
  if (!d) return states;
  const running = d.step_state === "running";
  const failed = d.step_state === "failed";
  // ① 需求分析
  states[0] =
    d.analysis !== null
      ? d.analysis?.error
        ? "failed"
        : "done"
      : d.step === 1
        ? running
          ? "run"
          : failed
            ? "failed"
            : "wait"
        : "wait";
  // ② 候选评分
  states[1] =
    d.candidates !== null
      ? d.candidates?.error
        ? "failed"
        : "done"
      : d.step === 2 && states[0] === "done"
        ? running
          ? "run"
          : failed
            ? "failed"
            : "wait"
        : "wait";
  // ③ 分档审批（人工门）
  states[2] =
    d.approval?.state === "approved"
      ? "done"
      : d.classification !== null
        ? "gate"
        : d.step === 3 && states[1] === "done"
          ? running
            ? "run"
            : failed
              ? "failed"
              : "wait"
          : "wait";
  // ④ 生成计划（物化在场也算——计划的 artifact 已消费）
  const planned = d.plan !== null || d.materialization !== null;
  states[3] = planned
    ? "done"
    : states[2] === "done"
      ? d.step === 4
        ? running
          ? "run"
          : failed
            ? "failed"
            : "wait"
        : "wait"
      : "wait";
  // ⑤ 物化确认（人工门）
  states[4] =
    d.materialization?.status === "materialized" ? "done" : planned ? "gate" : "wait";
  return states;
}
