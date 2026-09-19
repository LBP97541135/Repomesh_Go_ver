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
  | { kind: "task"; taskId: string }
  // 测试组：task 单点验收 / DAG 节点集成 / 跨仓库联调回归的**真实记录**
  // （2026-09-20 前这里没有读面，树上那一行是写死的文案）。
  | { kind: "tests" }
  // 阶段历史：顶栏「流程」那四个阶段（规划/执行/审核/交付）点开后看的历史。
  // 2026-09-20：顶栏此前只是状态指示（不可点），人想看"这一段到底发生过什么"
  // 只能自己翻；现在点哪段就切到哪段的历史。
  | { kind: "stage"; stage: 0 | 1 | 2 | 3 };

export type StepState = "done" | "run" | "gate" | "wait" | "failed" | "choose" | "confirm";

export const STEP_LABELS = ["需求分析", "候选评分", "分档审批", "生成计划", "物化确认"] as const;

/** 规划步骤态：①②④跟 artifact 走（analysis/candidates/plan），③⑤是人工门
 *  （approval.state / materialization.status）。 */
export function deriveStepStates(d: DiscoveryView | null, hitl = false): StepState[] {
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
  // ② 候选评分（分流步，2026-09-18 用户裁定）：人工参与且分析已过 → 停在
  // 「待人选」，等聊天室里的选择；自动托管 → 照常自动推进。
  states[1] =
    d.candidates !== null
      ? d.candidates?.error
        ? "failed"
        : "done"
      : d.step === 2 && states[0] === "done"
        ? hitl
          ? "choose"
          : running
            ? "run"
            : failed
              ? "failed"
              : "wait"
        : "wait";
  // ③ 分档审批：漏选清单待人确认 > 人工审批门
  const supplementPending = d.classification?.supplement_state === "pending";
  states[2] =
    d.approval?.state === "approved"
      ? "done"
      : supplementPending
        ? "confirm"
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
  //
  // 2026-09-19 修正（用户实测："怎么先完成了 1 和 4"）：旧代码读 `d.plan`，而
  // 后端 `State.View()` **根本不返回 plan 字段**（只有 analysis / candidates /
  // classification / approval / integration / materialization）。于是 `d.plan`
  // 恒为 undefined、`undefined !== null` 恒为 true → **planned 恒真 → ④ 永远
  // "已完成"、⑤ 永远"待人审"**，与真实进度无关。
  // 改按后端自己的口径：`deriveStep` 只在 plan 或 materialization 在场时返回
  // (4,"done")，所以「step===4 且 step_state==="done"」才是计划已生成。
  const planned = d.step === 4 && d.step_state === "done";
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
