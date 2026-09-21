import type { PlanTaskItem } from "../../api/taskTree";
import type { TestEvidenceItem, TestEvidenceView } from "../../api/testEvidence";

/**
 * 测试排期：**要跑什么、按什么顺序**，以及跑到哪了。
 *
 * 2026-09-20 用户反馈："测试组全程不展示任何规划和测试排期"。查下来是读面只有
 * **已发生的记录**（public.test_evidence 的三种 kind），没有"计划要跑什么"这一层。
 *
 * 这里的排期**不是另编一份**，是从**计划本身**推出来的（计划就是排期）：
 *   · 每条任务 → 一轮单点验收（任务跑完开发、过了经理门，就派测试 agent）；
 *   · 每个 DAG 节点（仓库）→ 一轮本仓库集成验证（该仓全部任务过了之后）；
 *   · 计划跨多个仓库时 → 再加一轮跨仓联调 + 回归。
 * 每一行状态**只用真实记录**判定：记录里有 → 已跑（通过 / 未过）；没有 → 待跑，
 * 并写清它在等什么。
 *
 * 2026-09-21 抽到这里：中间那棵树的「测试组」也要显示同一份排期（用户要求
 * "参考上面的 manager 主脑的显示"）。**推导只有这一份** —— 两处各写一套的话，
 * 迟早一边加了跨仓轮次、另一边没有，同一条计划在两屏排出两种样子。
 *
 * 返回 null = 推不出（计划还没物化）。调用方如实说"推不出"，不要摆空列表充数。
 */
export type TestScheduleRow = {
  key: string;
  stage: string;
  what: string;
  state: "done" | "failed" | "todo";
  note: string;
};

export function deriveTestSchedule(
  tasks: PlanTaskItem[] | null,
  view: TestEvidenceView | null,
): TestScheduleRow[] | null {
  if (tasks === null || tasks.length === 0) return null;
  const items = view?.items ?? [];
  const firstOf = (pred: (i: TestEvidenceItem) => boolean) => items.find(pred) ?? null;
  const repos: string[] = [];
  for (const t of tasks) {
    if (t.repositoryId && !repos.includes(t.repositoryId)) repos.push(t.repositoryId);
  }

  const rows: TestScheduleRow[] = [];
  for (const t of tasks) {
    const rec = firstOf((i) => i.kind === "task_single_point" && i.task_id === t.id);
    rows.push({
      key: `sp-${t.id}`,
      stage: `批次${t.batchNo ?? "—"}`,
      what: `单点验收 · ${t.title || t.taskUid || t.id}`,
      state: rec ? (rec.passed ? "done" : "failed") : "todo",
      note: rec ? (rec.summary || rec.command || "已产出记录") : `等 ${t.workerLabel || "执行者"} 跑完这条任务`,
    });
  }
  for (const repo of repos) {
    const rec = firstOf((i) => i.kind === "repo_integration" && i.repository_id === repo);
    rows.push({
      key: `ri-${repo}`,
      stage: "节点级",
      what: `仓库集成验证 · ${repo}`,
      state: rec ? (rec.passed ? "done" : "failed") : "todo",
      note: rec ? (rec.summary || "已产出记录") : "等该仓全部任务过了经理门",
    });
  }
  if (repos.length > 1) {
    const rec = firstOf((i) => i.kind === "cross_repo_regression");
    rows.push({
      key: "xr",
      stage: "跨仓",
      what: "跨仓库联调 + 回归",
      state: rec ? (rec.passed ? "done" : "failed") : "todo",
      note: rec ? (rec.summary || "已产出记录") : "等各仓节点集成都过了之后",
    });
  }
  return rows;
}

/** 排期行的状态 → 徽章样式与文案（两处共用，免得同一状态在两屏是两种颜色）。 */
export const TEST_SCHEDULE_TONE: Record<TestScheduleRow["state"], string> = {
  done: "border-olive/40 bg-olive-well text-olive",
  failed: "border-salmon/40 bg-salmon-well text-salmon",
  todo: "border-line bg-[var(--tree-zone)] text-[var(--tree-sub)]",
};

export const TEST_SCHEDULE_LABEL: Record<TestScheduleRow["state"], string> = {
  done: "已通过",
  failed: "未过",
  todo: "待跑",
};
