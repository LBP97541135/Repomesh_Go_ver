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
  // ⚠️ 判定一律以「**产物在不在**」为准，**不要**拿 `d.step === N` 当条件。
  //
  // 2026-09-20 线上实测（人工参与的 issue 在 ① 之后整条链停死）：后端的
  // `discovery.deriveStep` 返回的是「**已经完成**的那一步」——只有 candidates
  // 在场时才返回 2。于是「candidates 还没产出」与「step 已经是 2」**永远不可能
  // 同时成立**，② 的 `d.step === 2 && d.candidates === null` 恒假 → 那个
  // 「待人选择」的门从来没显示过 → 人没有可点的东西、驱动器在人工参与下又不
  // 越门 → ② 永远"等待前序"。同族的 `d.step === 3 && d.classification === null`
  // （③）、`d.step === 4 && plan 为空`（④）也一起改掉。
  // ① 需求分析：分析不充分且还有待澄清问题时 → 停在「待人答」门
  //    （2026-09-20 移植主线 5743fbc2：此前只要 analysis 块在场就标 done，
  //     用户看不出自己需要补充回答，右栏也没有立即弹出追问）
  //
  //    2026-09-20 修正「门粘住」：`sufficient` / `questions` 是**上一次分析的历史
  //    快照**，链路往后走之后不会被清空。只判这两个字段的话，只要当初那次分析
  //    被判不充分，① 就永久显示「待人审」——哪怕 ②③④ 都跑完了（线上实测：
  //    iss_bfd2f80fff1692db9259 的 ③ 已完成、④ 已跑，① 还挂着待人审）。
  //    补上「链路还没走过 ①」这个条件：② 一旦产出候选块，说明人已经就 ① 做过
  //    决定（回答了追问，或强制继续），这个门就不该再挡。
  const a = d.analysis;
  const analysisPending =
    a !== null && !a.sufficient && (a.questions?.length ?? 0) > 0 && d.candidates === null;
  states[0] =
    a !== null
      ? a.error
        ? "failed"
        : analysisPending
          ? "gate"
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
  const cand = d.candidates;
  states[1] =
    cand !== null
      ? cand.error
        ? "failed"
        : "done"
      : states[0] === "done"
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
          : states[1] === "done"
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
  // 2026-09-20 补：读面现在**真的**输出 `plan` 块了（`State.View()` 加了这一项），
  // 所以「plan 在场」与「deriveStep 返回 (4,done)」两条都算数。
  const planned = (d.plan ?? null) !== null || (d.step === 4 && d.step_state === "done");
  // ④ 在**人工参与**模式下也是一道门（2026-09-20 用户反馈："生成计划……没有人工
  // 确认项，而且也不展示"）。此前它只由前端驱动器自动开火，人既看不到"该我点了"
  // 也点不了。现在：分档批准后停在「待人审」，等人点「生成计划」才派发；
  // 派发出去（running_task_id 非空 / step_state=running）就如实显示进行中。
  const planRunning = d.step === 4 && (d.step_state === "running" || d.running_task_id !== null);
  states[3] = planned
    ? "done"
    : states[2] === "done"
      ? planRunning
        ? "run"
        : d.step === 4 && d.step_state === "failed"
          ? "failed"
          : hitl
            ? "gate"
            : "wait"
      : "wait";
  // ⑤ 物化确认（人工门）。
  //  2026-09-20 补：物化**失败**此前落进 else 分支显示"未开始"（planned 为假时），
  //  人看不到"它失败了、可以重试"。收据在场就按收据的 status 如实说。
  const matStatus = d.materialization?.status ?? null;
  states[4] =
    matStatus === "materialized"
      ? "done"
      : matStatus === "failed"
        ? "failed"
        : matStatus !== null
          ? "run"
          : planned
            ? "gate"
            : "wait";
  return states;
}
