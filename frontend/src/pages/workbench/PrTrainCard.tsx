/** 交付序列卡:最后一轮 PR 提交的合并确认。
 *
 *  产品语义:测试团队测试完毕且 Manager 判定无误后,进入合并时本卡作为
 *  一条**带按钮的系统消息**出现在对话流里(AssistantFlow 惯例:需要人的
 *  地方就地成为消息);确认后按仓库依赖顺序执行合并——被依赖者先合。
 *
 *  数据两态:
 *  - 缺省(不传 cars)= 夹具展示;
 *  - 传 `cars`(含 changeSetId)时,每节车厢轮询 merge-gate 真状态
 *    (pushed/pr/ciPassed/reviewed 四门),状态文案由门禁布尔推导。
 *  后端 change_sets 列表端点落地前,csId 需由冻结流程(build 后的
 *  freezeChangeSet)提供给本卡。 */

import { useEffect, useState } from "react";
import { getMergeGate, type MergeGate } from "../../api/scm";

type CarState = "done" | "run" | "wait";

const FIXTURE_CARS: Array<{ repo: string; pr: string; state: CarState; by: string }> = [
  { repo: "ts-common", pr: "PR !12 · cs-003", state: "done", by: "Leader·陆吾" },
  { repo: "saleor-core", pr: "PR !14 · cs-001", state: "run", by: "Worker·597869" },
  { repo: "saleor-dashboard", pr: "待提交", state: "wait", by: "Leader·顾盼" },
];

const AVA: Record<string, string> = {
  "Leader·陆吾": "L",
  "Worker·597869": "W",
  "Leader·顾盼": "L",
};

const SKIN: Record<CarState, { chip: string; text: string }> = {
  done: { chip: "border-olive/50 bg-olive/10 text-olive", text: "已合并" },
  run: {
    chip: "border-[rgba(94,106,210,.4)] bg-[rgba(94,106,210,.07)] text-[var(--tree-acc)]",
    text: "进行中",
  },
  wait: { chip: "border-[#e0dfdc] bg-[#fafafa] text-[var(--tree-faint)]", text: "等待前序合并" },
};

export interface TrainCarSpec {
  repo: string;
  /** 有 id 的车厢轮询 merge-gate 真状态;无 id = 待提交 */
  changeSetId?: string;
  /** 车厢上的 PR 标签(如 "PR !14");live 下缺省回退显示 changeSetId */
  pr?: string;
  /** change_sets.status === 'merged' 派生;「已合并」不来自门禁(门禁不含合并事实) */
  merged?: boolean;
  by?: string;
}

/** 四门布尔 → 状态文案。Go 的 merge-gate 语义:open = 四门全过(可合并放行);
 *  「已合并」不是门禁的输入,由 change_sets.status 派生(见 TrainCarSpec.merged)。 */
function gateText(g: MergeGate): string {
  const pending: string[] = [];
  if (!g.pushed) pending.push("未推送");
  if (!g.pr) pending.push("未开 PR");
  if (!g.ciPassed) pending.push("CI 未过");
  if (!g.reviewed) pending.push("未评审");
  return pending.length ? `待 ${pending.join(" · ")}` : "门禁全通过 · 待合并";
}

export function PrTrainCard({
  onConfirm,
  cars,
  projectId,
  className = "",
}: {
  onConfirm?: () => void;
  /** live 车厢(可选):提供 changeSetId 的车厢轮询真门禁;缺省 = 夹具三节 */
  cars?: TrainCarSpec[];
  projectId?: string;
  className?: string;
}) {
  const live = cars !== undefined;
  const list: Array<{ repo: string; changeSetId?: string; pr?: string; state?: CarState; by?: string }> =
    cars ?? FIXTURE_CARS.map((c) => ({ ...c }));
  const [gates, setGates] = useState<Record<string, MergeGate>>({});

  useEffect(() => {
    if (!projectId || !cars) return;
    let cancelled = false;
    cars.forEach((c) => {
      if (!c.changeSetId) return;
      getMergeGate(projectId, c.changeSetId)
        .then((g) => {
          if (!cancelled) setGates((prev) => ({ ...prev, [c.changeSetId!]: g }));
        })
        .catch(() => {
          /* 门禁拉取失败不拖垮整卡:车厢保持「状态未知」 */
        });
    });
    return () => {
      cancelled = true;
    };
  }, [projectId, cars]);

  return (
    <div className={`rounded-xl border border-[var(--tree-hairline)] bg-[var(--tree-card)] p-3 shadow-[0_1px_3px_rgba(15,15,15,.05)] ${className}`}>
      {/* 卡头:合并确认语境 */}
      <div className="flex items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          <span className="rounded-full border border-olive/50 bg-olive/10 px-2 py-0.5 text-[10px] font-medium text-olive">
            测试已通过
          </span>
          <span className="rounded-full border border-[#e0dfdc] bg-[#fafafa] px-2 py-0.5 text-[10px] text-[var(--tree-faint)]">
            Manager 已确认
          </span>
        </div>
        <span className="text-[10px] text-[var(--tree-faint)]">交付序列 · 按仓库依赖顺序合并</span>
      </div>

      {/* 列车:三节,箭头表达顺序 */}
      <div className="mt-2.5 flex items-stretch">
        {list.map((car, i) => {
          const gate = car.changeSetId && projectId ? gates[car.changeSetId as string] : undefined;
          const state: CarState = live
            ? car.merged
              ? "done"
              : car.changeSetId
                ? "run"
                : "wait"
            : (car as { state?: CarState }).state ?? "wait";
          const text = live
            ? car.merged
              ? "已合并"
              : car.changeSetId
                ? gate
                  ? gateText(gate)
                  : "门禁状态未知"
                : "尚未开 PR · 待提交"
            : (car as { pr?: string }).pr ?? "";
          const skin = state === "done" ? SKIN.done : state === "run" ? SKIN.run : SKIN.wait;
          return (
            <div key={car.repo} className="flex min-w-[118px] flex-1 items-stretch">
              <div
                className={`min-w-[118px] flex-1 rounded-lg border bg-[var(--tree-card)] px-2.5 py-2 ${
                  state === "run" ? "border-[rgba(94,106,210,.45)] shadow-[0_0_0_3px_rgba(94,106,210,.07)]" : "border-[var(--tree-hairline)]"
                }`}
              >
                <div className="text-[12.5px] font-semibold text-[#37352f]">{car.repo}</div>
                <div className="mt-0.5 font-mono text-[10px] text-[var(--tree-faint)]">
                  {live ? car.pr ?? car.changeSetId : car.pr}
                </div>
                <span className={`mt-1.5 inline-flex items-center gap-1.5 rounded-md border px-2 py-0.5 text-[10.5px] ${skin.chip}`}>
                  {state === "run" && (
                    <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-[var(--tree-acc)]" />
                  )}
                  {skin.text}
                </span>
                <div className="mt-1.5 flex items-center gap-1.5 text-[10.5px] text-[#787774]">
                  <span className="grid h-[15px] w-[15px] place-items-center rounded-full bg-[linear-gradient(135deg,#4ade80,#16a34a)] text-[7px] font-bold text-white">
                    {car.by ? AVA[car.by] ?? car.by[0] : "—"}
                  </span>
                  {car.by}
                </div>
                <div className="mt-1.5 truncate text-[10px] text-[var(--tree-faint)]">{text}</div>
              </div>
              {i < list.length - 1 && (
                <span aria-hidden className="mx-3 self-center text-[13px] text-[#b9b9b5]">
                  →
                </span>
              )}
            </div>
          );
        })}
      </div>

      {/* 人工节点:确认合并(带按钮的消息) */}
      <div className="mt-2.5 flex items-center justify-between border-t border-dashed border-[var(--tree-hairline)] pt-2">
        <span className="text-[10.5px] text-[var(--tree-faint)]">
          前序已合并的仓库先合;确认后按顺序依次执行,乱序将被 merge gate 拒绝
        </span>
        <button
          onClick={onConfirm}
          className="rounded-lg bg-[var(--tree-acc)] px-3.5 py-1.5 text-[12px] font-medium text-white transition-colors hover:bg-[var(--tree-acc)]"
        >
          确认合并
        </button>
      </div>
    </div>
  );
}
