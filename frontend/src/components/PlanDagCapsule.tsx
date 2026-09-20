import { useEffect, useRef, useState } from "react";
import { ChevronDown } from "lucide-react";
import type { DagExecutionView } from "../types";
import { ErrorBoundary } from "./ErrorBoundary";
import { StepChain, type PlanDagState } from "./PlanDagPanel";
import { AgentTeamsDagPanel } from "./AgentTeamsDagPanel";

/** 物化后的胶囊进度读数：N/M 仓已交付。数的是**仓**（byRepository 的归拢结论），
 *  不是任务——任务数在面板头部有，胶囊只给一眼可读的进度。态不一致（null）的仓
 *  不计入已交付：读模型没说交付了，界面不替它说。 */
function capsuleProgress(execution: DagExecutionView | null): string | null {
  if (!execution) return null;
  const repos = Object.keys(execution.taskCountByRepository);
  if (repos.length === 0) return null;
  const delivered = repos.filter((id) => execution.byRepository[id] === "succeeded").length;
  return `${delivered}/${repos.length} 仓已交付`;
}

/** 计划 DAG 的「任务清单胶囊」（参照 ZCode 顶部 todo 胶囊，2026-09-08 用户定稿）。
 *
 *  收起 = 一枚椭圆读数胶囊，钉在顶栏右侧（原「N 仓 · N 轮」的位置）：计划版本、
 *  节点/批次规模，物化后追加「N/M 仓已交付」进度；展开 = 从胶囊下方向左浮出的
 *  DAG 面板（等比缩放适配面板宽度）。点胶囊或点面板外任意处收起。
 *
 *  absent / 首次加载中胶囊整个不出现：没有图可看，摆一枚空胶囊就是「看得见的
 *  都属实」的反面。取用失败仍出现（鲑红点 + 面板内重试入口）——失败要能被看见。
 *
 *  渲染失败由区块级 ErrorBoundary 接住：只有这一块塌，对话流照常。 */
export function PlanDagCapsule({
  state,
  execution,
  resetKey,
  stageLabel,
  onOpenStage,
  steps,
}: {
  state: PlanDagState;
  /** C-4 执行态着色与胶囊进度读数的输入；null = 尚未物化。 */
  execution: DagExecutionView | null;
  /** 换 issue 即复位（收起 + 错误边界复位），不把上一单的错误挂到这一单头上。 */
  resetKey: string;
  /** 当前链路节点（规划/执行/审核/交付）。
   *  2026-09-20 移植主线 9e1dee3d：顶栏那条四点链路条收编进胶囊——收起态只显示
   *  当前节点这一个词，点开看 DAG 方案图。**计划快照未就绪时胶囊也渲染**（只显
   *  节点、不可展开）：链路走到哪本身就是有价值的信息，不该因为没有计划图而消失。 */
  stageLabel?: string;
  /** 点链路节点那一个词 → 打开那一段的阶段历史。不传就只当读数、不可点。 */
  onOpenStage?: () => void;
  /** 规划五步链条（name+state）。2026-09-20 用户裁定：五步放在 DAG 计划板上。
   *  这里只做透传——步骤态的唯一推导在 workbench/treeModel，本组件不重算。 */
  steps?: Array<{ label: string; state: string }>;
}) {
  const [open, setOpen] = useState(false);
  const wrapRef = useRef<HTMLDivElement | null>(null);

  // 换 issue 即收起：A 单展开着切到 B 单，悬着的面板内容已换血，收起最诚实
  useEffect(() => setOpen(false), [resetKey]);

  // 点外部收起（与吸底输入框同一套 mousedown 监听）+ Esc 收起。
  //
  // 2026-09-21 用户报「有时候会出现会话遮挡」：展开态是**视口居中的浮层**
  // （fixed left-1/2 top-1/2，78vw），它是设计定稿的"看一张图"，但它确实压在
  // 会话中间。既然压着，就必须有明确的、不用猜的退出方式 —— 此前只有"点面板外"
  // 一条路，用户按 Esc 没反应只能以为卡死。这里补 Esc，并在面板右上角给一枚
  // 显式的关闭按钮（点外部收起仍在，两条路都通）。
  useEffect(() => {
    if (!open) return;
    const onDown = (event: MouseEvent) => {
      if (wrapRef.current && !wrapRef.current.contains(event.target as Node)) setOpen(false);
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  // 计划快照没就绪（absent / 首载中）也要能点开：胶囊里现在放的是 AgentTeams 的
  // 任务级 DAG（9.16 原型，2026-09-20 用户裁定），它不依赖 RepoMesh 计划快照——
  // 此前 absent 时整个禁用，用户的 issue 没生成计划就"点不开胶囊"，DAG 无从看起。
  const noPlan = state.status === "absent" || state.status === "loading";
  if (noPlan && !stageLabel) return null;

  const progress = capsuleProgress(execution);

  return (
    <div ref={wrapRef} className="relative z-20">
      {/* 两枚控件共用一个胶囊外壳：左边点开/收起 DAG 方案图，右边那一个词（链路当前
          节点）点开它那一段的历史。
          拆成两枚而不是嵌套 <button>，是因为按钮不能嵌按钮；而阶段历史这一面是
          2026-09-20 LBP 才做的（点哪段看哪段发生过什么），顶栏那条四点条拆掉以后
          它是唯一入口，不能跟着一起消失。 */}
      <div
        className={`flex items-center gap-2 rounded-full border bg-panel px-3 py-1 font-mono text-[10.5px] shadow-card transition-colors ${
          open ? "border-amber/60" : "border-line"
        }`}
      >
        <button
          type="button"
          onClick={() => setOpen((v) => !v)}
          className="flex items-center gap-2 transition-colors hover:text-tx"
          title={open ? "收起任务 DAG" : "展开任务 DAG"}
        >
          <span
            className={`size-1.5 flex-none rounded-full ${state.status === "error" ? "bg-salmon" : stageLabel ? "bg-amber" : "bg-olive"}`}
          />
          {state.status === "ready" ? (
            <span className="text-tx3">
              v{state.plan.plan_version} · {state.plan.dag.nodes.length} 节点 · {state.plan.execution_batches.length} 批次
              {progress ? ` · ${progress}` : ""}
            </span>
          ) : stageLabel ? null : (
            <span className="text-tx3">任务 DAG</span>
          )}
          <span className="flex-none text-tx3"><ChevronDown size={12} strokeWidth={1.5} className={open ? "rotate-180" : ""} /></span>
        </button>
        {/* 链路当前节点：收编自顶栏那条四点条（2026-09-20 移植主线 9e1dee3d）。 */}
        {stageLabel && onOpenStage && (
          <>
            <span className="h-3 w-px flex-none bg-line" />
            <button
              type="button"
              onClick={() => onOpenStage()}
              className="flex-none text-tx transition-colors hover:text-amber-hi"
              title={`查看「${stageLabel}」这一段发生过什么`}
            >
              {stageLabel}
            </button>
          </>
        )}
        {stageLabel && !onOpenStage && <span className="flex-none text-tx">{stageLabel}</span>}
      </div>

      {open && (
        // 设计定稿(2026-09-08):880px 容器是等比缩放的基准,不按节点数缩水。
        // 2026-09-18(主线 d2843311/38c45556 移植):锚定改视口正中——DAG 是整张
        // 方案图,居中读比钉在右上角更像「看一张图」;宽度 72vw→78vw 让 4~5 个
        // 批次列排得下。阴影维持 shadow-float(主线中间态试过 shadow-pop 又调回)。
        // 面板仍是 wrapRef 的 DOM 子节点,点外收起的 contains 判定不受影响。
        <div className="fixed left-1/2 top-1/2 z-30 w-[min(880px,78vw)] -translate-x-1/2 -translate-y-1/2 rounded-hard border border-line bg-panel text-tx shadow-float">
          {/* 显式关闭：浮层压在会话上，退出方式必须看得见（见上面 Esc 那段注释）。 */}
          <div className="flex items-center justify-between border-b border-line px-3 py-1.5">
            <span className="font-mono text-[10.5px] text-tx3">任务 DAG · 方案图</span>
            <button
              type="button"
              onClick={() => setOpen(false)}
              className="flex-none rounded-hard px-1.5 text-[12px] text-tx3 transition-colors hover:text-tx"
              title="收起（Esc）"
              aria-label="收起任务 DAG"
            >
              ✕
            </button>
          </div>
          <div className="max-h-[min(68vh,560px)] overflow-y-auto p-2">
            <ErrorBoundary block="计划 DAG" resetKey={resetKey}>
              {/* 2026-09-20 用户裁定：胶囊里放 9.16 原型（dag-plan-progress.html）的任务级
                  DAG——AgentTeamsDagPanel 就是照原型写的（分层布局/白卡左色条/ready
                  虚线框/点击详情），此前被回退后一直没人引用。五步链条留在板头。 */}
              {steps && steps.length > 0 && <StepChain steps={steps} />}
              <AgentTeamsDagPanel embedded issueId={resetKey} />
            </ErrorBoundary>
          </div>
        </div>
      )}
    </div>
  );
}
