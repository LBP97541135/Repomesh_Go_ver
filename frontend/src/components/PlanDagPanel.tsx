import { Star as IconFocus } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import type { PlanGraphEdgeView, RepositoryPlanView, TaskDisplayStatus } from "../api/contract";
import type { DagExecutionView } from "../types";
import { unverifiedMarkerLabel } from "../display";
import { UnverifiedMarker } from "./AgentVerificationBlock";

/** 图形化 DAG 面板（批次 C-2）。数据源是既有端点
 *  `GET /issues/{id}/repositories/{repo}/plan`（契约 v0.2 §5.4/§5.5），与 RoomView
 *  文本版计划纸面同源——本面是它的图形化呈现。
 *
 *  2026-09-08 视觉定稿（与用户逐项确认）：
 *   - **紧凑单行胶囊节点**（高 26px：状态色点 + 名称）；状态全名/任务数/未验证数
 *     全部进 hover，节点上只留最小留痕（未解析、锚点 ◆）；
 *   - **等比缩放适配**：整图按容器宽度缩放（只缩不放），典型 3~5 批次一屏放下，
 *     超大图才出滚动；
 *   - **hover 高亮上下游**：悬停节点时它的依赖边与下游边加亮加粗、直连邻居保持，
 *     无关节点与边淡出——多仓依赖关系一眼可读；
 *   - **配色走主题令牌**：白卡 + 发丝线 + 微阴影（浅色即白浮卡观感；深色自动是
 *     深棕面板）。
 *
 *  2026-09-18 换皮（照主线 dag-plan-progress.html 原型，主线 fdea94c4 + d2843311）：
 *   - 节点由 26px 单行胶囊换成 **48px 白卡**（左缘 3px 色条 + 名称/批次两行），
 *     批次列之间加虚线层分隔，批次标题回到常规字号；
 *   - 高亮边改琥珀实线，未选中边改 `--color-line-strong`；
 *   - 「进行中」由 `--color-status-running` 统一改读 `--color-amber`，浅色下不再
 *     是信息蓝——与主线逐字同款；两仓的主题色值本来就一致，**不是**本仓特调。
 *
 *  2026-09-20 补齐原型差的三件（用户裁定"要 dag-plan-progress 这张板"）：
 *   - **规划五步链条放在这张板上**：`steps` 由工作台传入（name+state），
 *     画成一排与执行节点同形状的小卡（白卡 + 左缘色条 + 态），置于画布上方。
 *     步骤态不在本面重算——唯一推导在 workbench/treeModel，否则同一件事两个真相。
 *   - **点击出详情卡**（原型最标志性的交互）：此前面上只有 hover 的原生 title，
 *     理由是"主线未落地、本面不自行加"。现在按原型补：点节点，右上角浮出详情卡
 *     （批次 / 执行态 / 本轮任务 / 上游依赖 / 下游任务 / 未验证 / 失败理由），
 *     全部来自读面；原型里的 artifact 与执行者这两份读面没有，**如实不显示**。
 *   - 配色清残：`bg-white` 改回 `--tree-card`，层间虚线注释里的 `#eef0f3` 与实现
 *     统一到 `--color-line`——原型那套字面色值只活在注释里，深浅主题都要成立。
 *
 *  页脚方法论自述（粒度/边来源/锚点/未验证长文/着色来源/投影边界）按用户裁决退役；
 *  连线契约加注（粗线 + hover 接口与约定）保留。
 *
 *  红线：不派生任何状态——`display_status` 是读模型 §5.1 算好的，本面只把字面值
 *  上色并原样印进 hover；配色全部取 `index.css` 既有令牌，不新增颜色语义。 */

/* ── 泳道几何（单位 px，SVG 与 HTML 覆盖层共用同一套坐标） ───────────────── */
/* 2026-09-18 换皮（dag-plan-progress.html 原型同款比例）：
   大节点白卡 + 左色条 + 虚线层分隔 + 选中高亮边（2026-09-20 起：点选出详情卡） */
const NODE_H = 48;
const NODE_W2 = 200; // 原型 232，面板宽 880 下 4 列用 200 更合适
const COL_GAP = 56;
const ROW_GAP = 18;
const PAD = 28;
const HEAD_H = 20;

const nodeX = (col: number) => PAD + col * (NODE_W2 + COL_GAP);
const nodeY = (row: number) => PAD + HEAD_H + row * (NODE_H + ROW_GAP);

/** 节点的稳定标识＝`name + batch_index`。**不能用 `repository_id`**：契约 §5.4
 *  勘正后它可为 null（catalog 无此名 / issue 域外重名歧义），多个未解析节点会
 *  塌到同一个 key 上。hover 邻接也按它寻址。 */
const nodeKey = (node: { name: string; batch_index: number }) => `${node.batch_index}:${node.name}`;

/** 边来源的中文措辞（迁移 4）。三者是**不同性质的事实**，不合并：
 *  `scan` 是从代码里扫出来的依赖，`llm` 是集成时模型判定的，`tm` 是人工批次
 *  顺序反推的。一条边可信到什么程度，取决于它是哪一种。 */
const EDGE_SOURCE_LABEL: Record<string, string> = {
  scan: "扫描（代码依赖）",
  llm: "集成模型判定",
  tm: "人工批次顺序派生",
};

interface Placed {
  node: RepositoryPlanView["dag"]["nodes"][number];
  col: number;
  row: number;
}

/* ── 执行态皮肤（C-4）─────────────────────────────────────────────────────── */

/** **展示皮肤，不是状态映射。** 状态映射唯一实现在读模型（契约 v0.1 §5.1，后端
 *  7 态 → 展示 6 态）；本表只把已经给出的 6 个字面值分到皮肤上，不参与任何判定。
 *  Record 收窄到契约枚举：读模型将来多出第 7 个展示态时，这里缺项即编译错误。
 *
 *  分桶（设计定稿 ③）：橄榄 = 已交付 / 琥珀 = 进行中 / 弱灰 = 等待 / 赭红 = 失败。
 *  （2026-09-18 换皮：「进行中」不再单读 `--color-status-running`，与主线统一走
 *  `--color-amber`，浅色下因此不再是信息蓝——见文件头换皮说明。）
 *  白卡只在左缘留一条 3px 色条，余下是细描边加一层极浅底色，重心在色条上——
 *  满色块在白底上糊成一团。 */
const EXEC_SKIN: Record<TaskDisplayStatus, string> = {
  succeeded: "border-olive/40 bg-[color-mix(in_oklab,var(--color-olive)_8%,var(--color-panel))]",
  running: "border-amber/40 bg-amber-well",
  repairing: "border-amber/40 bg-amber-well",
  pending: "border-line bg-panel-2",
  blocked: "border-line bg-panel-2",
  failed: "border-salmon/40 bg-[color-mix(in_oklab,var(--color-salmon)_6%,white)]",
};

const STATUS_DOT: Record<TaskDisplayStatus, string> = {
  succeeded: "bg-olive",
  running: "bg-amber",
  repairing: "bg-amber",
  pending: "bg-tx3",
  blocked: "bg-tx3",
  failed: "bg-salmon",
};

/** 按 `batch_index` 分列。列取自节点自身而非 `execution_batches`——两者是同一份
 *  投影（服务端遍历 execution_batches 生成节点，batch_index 就是那个下标）。
 *  列号用**批次值排序后的名次**，即便某个 batch_index 空缺也不会留出空列。 */
function layout(nodes: RepositoryPlanView["dag"]["nodes"]): { placed: Placed[]; batches: number[]; rows: number } {
  const batches = [...new Set(nodes.map((n) => n.batch_index))].sort((a, b) => a - b);
  const filled = new Map<number, number>();
  const placed = nodes.map((node) => {
    const col = batches.indexOf(node.batch_index);
    const row = filled.get(col) ?? 0;
    filled.set(col, row + 1);
    return { node, col, row };
  });
  return { placed, batches, rows: Math.max(1, ...filled.values()) };
}

function NodeBox({
  placed,
  execution,
  dimmed,
  selected,
  onHover,
  onSelect,
}: {
  placed: Placed;
  execution: DagExecutionView | null;
  dimmed: boolean;
  /** 被点选（详情卡正指着它）。与 hover 分开：hover 是掠过，点选是"就看这个"。 */
  selected: boolean;
  onHover: (key: string | null) => void;
  onSelect: (key: string | null) => void;
}) {
  const { node } = placed;
  const unresolved = node.repository_id === null;

  /** 本仓在本轮的执行态。三种「没有」互不相同，压成一个会撒谎：
   *   - `execution === null`：未物化 / 本轮聚合没取到——**无事实可着色**；
   *   - 本轮没有这个仓的任务（计数 0）：计划里有它，执行面还没有它；
   *   - 有多条任务且态不一致（值为 null）：读模型没有给出仓级结论，不挑一条充数。 */
  const taskCount = execution && node.repository_id ? (execution.taskCountByRepository[node.repository_id] ?? 0) : 0;
  const status = execution && node.repository_id ? (execution.byRepository[node.repository_id] ?? null) : null;
  const colored = !unresolved && status !== null;

  /** A-18：本仓有几条任务是 agent 自述「未验证」的——不换状态色，另加琥珀标记。 */
  const unverified =
    execution && node.repository_id
      ? (execution.unverifiedCountByRepository[node.repository_id] ?? 0)
      : 0;
  const blockerCount =
    execution && node.repository_id
      ? (execution.blockerCountByRepository[node.repository_id] ?? 0)
      : 0;
  /** A-18 第四面：失败理由（Runner 原文）。 */
  const failureReasons =
    execution && node.repository_id
      ? (execution.failureReasonsByRepository[node.repository_id] ?? [])
      : [];

  const skin = unresolved
    ? "border-dashed border-salmon/70 bg-[var(--tree-card)]"
    : colored
      ? EXEC_SKIN[status]
      : node.is_focus
        ? "border-amber/50 bg-amber-well"
        : "border-line bg-panel";

  const baseTitle = unresolved
    ? `${node.name}：catalog 中查无此仓库——名字未注册，或在本 issue 域外重名歧义（域内优先后仍无唯一解），服务端不猜。`
    : colored
      ? `${node.name} · 本轮任务展示态 ${status}（读模型 §5.1 算出的 display_status，界面只上色）`
      : node.name;

  const title = [
    baseTitle,
    !unresolved && execution && taskCount === 0 ? "本轮还没有这个仓的任务（计划内有它，执行面还没有它）。" : null,
    !unresolved && execution && taskCount > 1 && status === null ? `${taskCount} 条任务态不一致，读模型未给出仓级结论。` : null,
    unverified > 0
      ? `${unverifiedMarkerLabel(blockerCount)}：本仓 ${unverified} 条任务没有可核验的执行记录（agent 自述，契约 §5.4）。原话在「查看证据」里。`
      : null,
    ...failureReasons.map((reason) => `失败理由（Runner 原文）：${reason}`),
  ]
    .filter(Boolean)
    .join("\n");

  return (
    <div
      className={`absolute flex cursor-pointer items-center overflow-hidden rounded-[9px] border bg-panel transition-opacity ${skin} ${
        dimmed ? "opacity-45" : ""
      } ${selected ? "border-amber shadow-[0_0_0_2px_color-mix(in_oklab,var(--color-amber)_35%,transparent)]" : ""}`}
      style={{ left: nodeX(placed.col), top: nodeY(placed.row), width: NODE_W2, height: NODE_H }}
      title={title}
      onMouseEnter={() => onHover(nodeKey(node))}
      onMouseLeave={() => onHover(null)}
      onClick={() => onSelect(selected ? null : nodeKey(node))}
    >
      {/* 左色条：颜色即状态（原型同款 3px 色条 + 6px 缩进） */}
      <span
        className={`h-[36px] w-[3px] flex-none rounded-[1.5px] ${
          unresolved ? "bg-salmon" : colored ? STATUS_DOT[status] : "bg-tx3"
        }`}
        style={{ marginLeft: 0 }}
      />
      <div className="flex min-w-0 flex-1 flex-col gap-0.5 pl-2.5 pr-2">
        <span className={`truncate text-[12px] font-semibold leading-tight ${unresolved ? "text-salmon" : "text-[var(--tree-ink)]"}`}>
          {node.name}
          {node.is_focus && <IconFocus size={9} className="ml-1 text-amber" />}
        </span>
        <span className="truncate font-mono text-[10px] leading-tight text-tx3">
          {colored ? `${taskCount} 任务 · ${status}` : unresolved ? "未解析" : `批次 ${node.batch_index + 1}${node.is_focus ? " · 锚点" : ""}`}
        </span>
      </div>
      {/* A-18：与状态并排、不覆盖它——「跑成了」和「没验证」都是真的 */}
      {unverified > 0 && (
        <UnverifiedMarker compact blockerCount={blockerCount} title={`${unverified} 条任务未验证（agent 自述）`} />
      )}
    </div>
  );
}

function DagCanvas({
  dag,
  graphEdges,
  execution,
  selected,
  onSelect,
}: {
  dag: RepositoryPlanView["dag"];
  graphEdges: PlanGraphEdgeView[] | null;
  execution: DagExecutionView | null;
  selected: string | null;
  onSelect: (key: string | null) => void;
}) {
  const wrapRef = useRef<HTMLDivElement | null>(null);
  /** 容器实测宽度（等比缩放的基准）；null = 尚未量到，先按原尺寸画。 */
  const [availW, setAvailW] = useState<number | null>(null);
  /** hover 高亮的节点（nodeKey）。非 null 时：它的直连边加亮，无关节点/边淡出。 */
  const [hovered, setHovered] = useState<string | null>(null);

  // 容器宽度用 ResizeObserver 跟：侧栏开合、窗口缩放都会改它
  useEffect(() => {
    const el = wrapRef.current;
    if (!el) return;
    const update = () => setAvailW(el.clientWidth);
    update();
    const ro = new ResizeObserver(update);
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  const { placed, batches, rows } = layout(dag.nodes);
  const width = PAD * 2 + batches.length * NODE_W2 + Math.max(0, batches.length - 1) * COL_GAP;
  const gridBottom = PAD + HEAD_H + rows * NODE_H + Math.max(0, rows - 1) * ROW_GAP;

  /** 边按 `repository_id` 寻址。同一 id 理论上只出现在一个批次里；真出现重复时
   *  取先出现的那个位置——画一条到「其中一个」的线，好过整条边消失。 */
  const byId = new Map<string, Placed>();
  for (const p of placed) if (p.node.repository_id !== null && !byId.has(p.node.repository_id)) byId.set(p.node.repository_id, p);

  /** 名字 → 边语义。**只索引 confirmed 边**：candidate 是待确认的扫描边，
   *  没有进拓扑投影，拿它给一条已投影的连线加注就是把待定说成已定。 */
  const pairKey = (fromName: string, toName: string) => `${fromName}\n${toName}`;
  const semanticByPair = new Map<string, PlanGraphEdgeView>();
  for (const edge of graphEdges ?? []) {
    if (edge.status === "confirmed") semanticByPair.set(pairKey(edge.from, edge.to), edge);
  }
  const semanticsOf = (fromName: string, toName: string) =>
    semanticByPair.get(pairKey(fromName, toName)) ?? null;

  const resolved = dag.edges
    .map((edge) => ({ from: byId.get(edge.from_repository_id), to: byId.get(edge.to_repository_id) }))
    // 服务端保证两端已解析且都落在 nodes 内（§5.4），这里的判空只是不让任何
    // 意外形状把整面炸掉。
    .filter((e): e is { from: Placed; to: Placed } => Boolean(e.from && e.to));

  // hover 邻接：点亮 hover 节点的直连边与两端节点
  const litEdgeKeys = new Set<string>();
  const neighborKeys = new Set<string>();
  if (hovered !== null) {
    for (const e of resolved) {
      const fk = nodeKey(e.from.node);
      const tk = nodeKey(e.to.node);
      if (fk === hovered || tk === hovered) {
        litEdgeKeys.add(`${fk}->${tk}`);
        neighborKeys.add(fk);
        neighborKeys.add(tk);
      }
    }
  }

  /** **跨批次边要绕行**：跨越 ≥2 列的边如果直着画，会从中间那一列的节点身上穿过去，
   *  读起来就成了「A→中间仓→C」。这类边改走图底部的绕行道，多条时按槽位错开。 */
  const skipping = resolved.filter((e) => e.to.col - e.from.col > 1);
  const laneSlots = Math.min(skipping.length, 4);
  const laneY = (i: number) => gridBottom + 10 + (i % Math.max(1, laneSlots)) * 9;
  const height = (laneSlots > 0 ? laneY(laneSlots - 1) + 6 : gridBottom) + PAD;

  // 等比缩放适配：只缩不放（小图放大只会糊），超大图保留横向滚动
  const scale = availW === null || availW >= width ? 1 : availW / width;

  return (
    <div ref={wrapRef} className="w-full" style={{ height: Math.round(height * scale) }}>
      <div className="relative origin-top-left" style={{ width, height, transform: `scale(${scale})` }}>
        <svg className="absolute inset-0" width={width} height={height} aria-hidden>
          <defs>
            <marker
              id="plan-dag-arrow"
              markerWidth="7"
              markerHeight="7"
              refX="6"
              refY="3"
              orient="auto"
              markerUnits="userSpaceOnUse"
            >
              <path d="M0,0 L6,3 L0,6 Z" className="fill-tx2" />
            </marker>
          </defs>

          {/* 层间虚线分隔（照原型的 dash 4 6；颜色走当前主题的 `--color-line`，
              不用原型那支一次性色值 #eef0f3——深浅两套主题下都要成立） */}
          {batches.map((_, i) => {
            if (i === 0) return null;
            const x = PAD + i * (NODE_W2 + COL_GAP) - COL_GAP / 2;
            return (
              <line
                key={`sep-${i}`}
                x1={x}
                y1={14}
                x2={x}
                y2={height - 14}
                stroke="var(--color-line)"
                strokeWidth={1}
                strokeDasharray="4 6"
              />
            );
          })}

          {resolved.map(({ from, to }) => {
            // 边语义按**仓库名**匹配：graph_edges 两端存的是名字（与
            // execution_batches 同口径），而这里的连线按 id 寻址。名字是两者
            // 唯一的公共键。
            const semantic = semanticsOf(from.node.name, to.node.name);
            const x1 = nodeX(from.col) + NODE_W2;
            const y1 = nodeY(from.row) + NODE_H / 2;
            const x2 = nodeX(to.col);
            const y2 = nodeY(to.row) + NODE_H / 2;
            const ek = `${nodeKey(from.node)}->${nodeKey(to.node)}`;
            const lit = hovered !== null && litEdgeKeys.has(ek);
            const dim = hovered !== null && !lit;
            const span = to.col - from.col;
            const skipIndex = skipping.findIndex((e) => e.from === from && e.to === to);
            let d: string;
            if (span > 1) {
              // 绕行道：右出 → 下沉到底部车道 → 横穿 → 抬回目标左侧
              const lane = laneY(skipIndex < 0 ? 0 : skipIndex);
              d = `M ${x1} ${y1} C ${x1 + 20} ${y1}, ${x1 + 20} ${lane}, ${x1 + 40} ${lane} L ${x2 - 40} ${lane} C ${x2 - 20} ${lane}, ${x2 - 20} ${y2}, ${x2} ${y2}`;
            } else if (span > 0) {
              // 相邻批次：右出左入的横向贝塞尔
              d = `M ${x1} ${y1} C ${x1 + COL_GAP / 2} ${y1}, ${x2 - COL_GAP / 2} ${y2}, ${x2} ${y2}`;
            } else {
              // 同列或回指（execution_batches 的语义下不该出现）退化成竖直连线，
              // 不假装它是一条正常的层间边。
              d = `M ${nodeX(from.col) + NODE_W2 / 2} ${nodeY(from.row) + NODE_H} L ${nodeX(to.col) + NODE_W2 / 2} ${nodeY(to.row)}`;
            }
            return (
              <path
                key={ek}
                d={d}
                fill="none"
                // 带契约的边画实一点：它比一条纯执行顺序依赖多一份约定。
                // 只用粗细区分，不新增颜色语义。
                stroke={lit ? "var(--color-amber)" : "var(--color-line-strong)"}
                strokeWidth={semantic?.interface ? (lit ? 2.2 : 1.9) : lit ? 2 : 1.5}
                opacity={dim ? 0.18 : 1}
                markerEnd="url(#plan-dag-arrow)"
              >
                {semantic && (
                  // 原生 <title>：hover 出提示，且进可访问性树。
                  // 没有 interface 的边只报来源，不编一个契约名出来。
                  <title>
                    {[
                      `${from.node.name} → ${to.node.name}`,
                      semantic.interface ? `接口：${semantic.interface}` : null,
                      semantic.agreement ? `约定：${semantic.agreement}` : null,
                      `来源：${EDGE_SOURCE_LABEL[semantic.source] ?? semantic.source}`,
                    ]
                      .filter(Boolean)
                      .join("\n")}
                  </title>
                )}
              </path>
            );
          })}
        </svg>

        {batches.map((batch, col) => (
          <div
            key={batch}
            className="absolute text-[11px] text-tx3"
            style={{ left: nodeX(col), top: PAD - 4, width: NODE_W2 }}
          >
            批次 {batch + 1}
          </div>
        ))}

        {placed.map((p) => (
          <NodeBox
            key={nodeKey(p.node)}
            placed={p}
            execution={execution}
            dimmed={hovered !== null && !neighborKeys.has(nodeKey(p.node))}
            selected={selected === nodeKey(p.node)}
            onHover={setHovered}
            onSelect={onSelect}
          />
        ))}
      </div>
    </div>
  );
}

/** 图例行。**两套图例按有没有执行态事实切换**——未物化时摆一排执行态色点，等于
 *  给一张没有执行事实的图配一本用不上的色谱，读者会以为自己在看运行状态。 */
function Legend({ execution }: { execution: DagExecutionView | null }) {
  const dot = (cls: string, label: string) => (
    <span key={label} className="flex items-center gap-1">
      <i className={`inline-block size-2 rounded-full ${cls}`} />
      {label}
    </span>
  );

  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1 font-mono text-[10px] text-tx2">
      {execution ? (
        <>
          <span className="font-bold tracking-[0.1em] uppercase">执行态 · {execution.roundLabel}</span>
          {dot("bg-olive", "已交付 succeeded")}
          {dot("bg-[var(--color-status-running)]", "进行中 running / repairing")}
          {dot("bg-paper-dim", "等待 pending / blocked")}
          {dot("bg-salmon", "失败 failed")}
          {dot("border border-dashed border-salmon bg-transparent", "未解析（catalog 无此仓）")}
          {/* A-18：不是第五个执行态，是贴在任何一个态上的标记，所以图例里也另起一说 */}
          {dot("border border-amber bg-transparent", "未验证（标记，非状态）")}
        </>
      ) : (
        <>
          <span className="font-bold tracking-[0.1em] uppercase">计划结构</span>
          {dot("bg-olive", "已交付")}
          {dot("bg-amber", "进行中 / 修复中")}
          {dot("bg-tx3", "等待 / 受阻")}
          {dot("border border-dashed border-amber bg-amber-well", "锚点仓（可开工）")}
          {dot("border border-dashed border-salmon bg-transparent", "未解析")}
        </>
      )}
    </div>
  );
}

/** 规划五步链条（2026-09-20 用户裁定：**放在 DAG 计划板上**，不是放在顶栏）。
 *
 *  五步是 RepoMesh 自己的语义，不在上游 workflow、也不在计划快照里，所以由调用方
 *  （工作台）把 name+state 传进来，本面只负责画——不在这里重新推导一遍步骤态，
 *  否则同一件事会有两个真相。
 *
 *  `state` 用字符串收而不是 import 左树的 StepState：components 不该反向依赖
 *  pages/workbench。字面值与左树同一套（done/run/gate/choose/confirm/failed/wait）。 */
function stepSkin(state: string): { bar: string; text: string; label: string } {
  if (state === "done") return { bar: "bg-olive", text: "text-olive", label: "已完成" };
  if (state === "run") return { bar: "bg-amber", text: "text-amber", label: "进行中" };
  if (state === "failed") return { bar: "bg-salmon", text: "text-salmon", label: "失败" };
  if (state === "gate" || state === "choose" || state === "confirm")
    return { bar: "bg-amber", text: "text-amber", label: "待人审" };
  return { bar: "bg-tx3", text: "text-tx3", label: "未开始" };
}

function StepChain({ steps }: { steps: Array<{ label: string; state: string }> }) {
  return (
    <div className="mb-2 flex flex-wrap items-center gap-x-1.5 gap-y-1 border-b border-dashed border-line pb-2">
      <span className="mr-1 font-mono text-[10px] tracking-[0.1em] text-tx3 uppercase">规划</span>
      {steps.map((s, i) => {
        const skin = stepSkin(s.state);
        return (
          <span key={s.label} className="flex items-center gap-1.5">
            {i > 0 && <span className="text-[11px] text-tx3">→</span>}
            {/* 与执行节点同一套形状：白卡 + 左缘 3px 色条 + 名称 + 态——链条与图读起来是一张板上的东西 */}
            <span className="flex items-center gap-1.5 overflow-hidden rounded-[7px] border border-line bg-panel pr-2">
              <span className={`h-[22px] w-[3px] flex-none ${skin.bar}`} />
              <span className="py-1 text-[11.5px] text-[var(--tree-ink)]">{s.label}</span>
              <span className={`text-[10.5px] ${skin.text}`}>{skin.label}</span>
            </span>
          </span>
        );
      })}
    </div>
  );
}

/** 面板取数三态 + 「无计划快照」这一态。404 **不是错误**：issue 尚未生成计划
 *  就是这个形态，必须说出来而不是把区块藏掉或摆一张空图假装有计划。 */
export type PlanDagState =
  | { status: "loading" }
  | { status: "absent"; reason: string }
  | { status: "error"; message: string }
  | {
      status: "ready";
      plan: RepositoryPlanView;
      /** 迁移 4：该版快照的计划层边（含 interface/agreement）。**null = 没取到**
       *  （老快照 graph_edges 为空 / 端点 404 / 回放模式），此时连线照画、只是
       *  没有语义可标——这一层是给既有连线加注的，不是画图的前提。 */
      graphEdges: PlanGraphEdgeView[] | null;
    };

export function PlanDagPanel({
  state,
  execution,
  onRetry,
  steps,
}: {
  state: PlanDagState;
  /** C-4 执行态着色的输入。`null` = 尚未物化（无轮次）或本轮聚合没取到，
   *  此时节点维持结构三视觉——没有事实就不上色。 */
  execution: DagExecutionView | null;
  onRetry: () => void;
  /** 规划五步链条（工作台传进来的 name+state）。不传就不画——不自己编一份。 */
  steps?: Array<{ label: string; state: string }>;
}) {
  return (
    <>
      {/* 面板标题由承载方（PlanDagCapsule 胶囊）提供，这里只渲染状态与图本身 */}

      {state.status === "loading" && <p className="py-4 text-[12px] text-tx2">计划纸面加载中…</p>}

      {state.status === "absent" && (
        <div>
          {/* 计划还没有，但五步链条照样要看得见——它描述的正是"计划为什么还没有" */}
          {steps && steps.length > 0 && <StepChain steps={steps} />}
          <p className="text-[12px] text-tx3">{state.reason}</p>
        </div>
      )}

      {state.status === "error" && (
        <p className="text-[12px] text-salmon">
          计划纸面取用失败：{state.message}
          <button className="pl-2 text-tx2 underline hover:text-amber-hi" onClick={onRetry}>
            重试
          </button>
        </p>
      )}

      {state.status === "ready" && (
        <PlanDagSheet plan={state.plan} graphEdges={state.graphEdges} execution={execution} steps={steps} />
      )}
    </>
  );
}

function PlanDagSheet({
  plan,
  graphEdges,
  execution,
  steps,
}: {
  plan: RepositoryPlanView;
  graphEdges: PlanGraphEdgeView[] | null;
  execution: DagExecutionView | null;
  steps?: Array<{ label: string; state: string }>;
}) {
  /** 点选的节点（nodeKey）。原型里右侧浮出详情卡——这一面此前只有 hover 的原生
   *  title，原型最标志性的那个交互没落地（主线也没做）。这里补上。 */
  const [selected, setSelected] = useState<string | null>(null);

  /** 节点选中时能说的事实，全部来自读面：批次、本轮任务数、仓级展示态、
   *  未验证/blocker 数、失败理由。原型里的 artifact/执行者在这两份读面里没有，
   *  如实不显示——不编字段。 */
  const selectedNode = selected === null ? null : (plan.dag.nodes.find((n) => nodeKey(n) === selected) ?? null);
  const selectedRepoId = selectedNode?.repository_id ?? null;
  const selectedTaskCount =
    execution && selectedRepoId ? (execution.taskCountByRepository[selectedRepoId] ?? 0) : 0;
  const selectedStatus = execution && selectedRepoId ? (execution.byRepository[selectedRepoId] ?? null) : null;
  const selectedUnverified =
    execution && selectedRepoId ? (execution.unverifiedCountByRepository[selectedRepoId] ?? 0) : 0;
  const selectedBlockers =
    execution && selectedRepoId ? (execution.blockerCountByRepository[selectedRepoId] ?? 0) : 0;
  const selectedFailures =
    execution && selectedRepoId ? (execution.failureReasonsByRepository[selectedRepoId] ?? []) : [];
  /** 该节点在图上的上下游（按仓库名，与画线同一口径）。 */
  const upstream = selectedNode ? plan.dag.edges.filter((e) => e.to_repository_id === selectedNode.repository_id) : [];
  const downstream = selectedNode ? plan.dag.edges.filter((e) => e.from_repository_id === selectedNode.repository_id) : [];
  const nameOf = (id: string | null) => plan.dag.nodes.find((n) => n.repository_id === id)?.name ?? "—";

  return (
    <div className="relative rounded-hard border border-line bg-panel px-3 py-2 text-tx shadow-card">
      {/* 规划五步链条：放在这张板上（用户裁定），执行图跟在它下面 */}
      {steps && steps.length > 0 && <StepChain steps={steps} />}
      {plan.dag.nodes.length === 0 ? (
        // 快照在、批次为空：这是真实形态之一，说出来而不是画一张空画布
        <p className="py-2 font-mono text-[11.5px] text-tx3">
          本计划快照（v{plan.plan_version}）没有任何执行批次，无可绘制的节点。
        </p>
      ) : (
        <DagCanvas
          dag={plan.dag}
          graphEdges={graphEdges}
          execution={execution}
          selected={selected}
          onSelect={setSelected}
        />
      )}

      {plan.dag.nodes.length > 0 && (
        <div className="mt-1.5">
          <Legend execution={execution} />
        </div>
      )}

      {/* 详情卡（原型同款：浮在板右上角）。点空白/再点同一节点收起。 */}
      {selectedNode && (
        <div className="absolute right-2 top-2 z-10 w-72 rounded-[10px] border border-line bg-panel p-3 text-[11.5px] shadow-float">
          <div className="mb-1.5 flex items-start justify-between gap-2">
            <span className="min-w-0 flex-1 text-[12.5px] font-semibold text-[var(--tree-ink)]">{selectedNode.name}</span>
            <button className="flex-none text-tx3 hover:text-tx" onClick={() => setSelected(null)} title="关闭">
              ×
            </button>
          </div>
          {[
            ["批次", `第 ${selectedNode.batch_index + 1} 批${selectedNode.is_focus ? " · 锚点仓" : ""}`],
            [
              "执行态",
              selectedRepoId === null
                ? "未解析（catalog 无此仓）"
                : execution === null
                  ? "未物化，本轮无事实"
                  : selectedStatus ?? (selectedTaskCount === 0 ? "本轮无任务" : `${selectedTaskCount} 条任务态不一致`),
            ],
            ["本轮任务", execution === null ? "—" : `${selectedTaskCount} 条`],
            ["上游依赖", upstream.length > 0 ? upstream.map((e) => nameOf(e.from_repository_id)).join("、") : "无（入口）"],
            ["下游任务", downstream.length > 0 ? downstream.map((e) => nameOf(e.to_repository_id)).join("、") : "无"],
          ].map(([k, v]) => (
            <div key={k} className="flex gap-2 border-b border-dashed border-line py-1 last:border-b-0">
              <em className="w-[52px] flex-none not-italic text-tx3">{k}</em>
              <b className="min-w-0 flex-1 break-words font-medium text-[var(--tree-ink)]">{v}</b>
            </div>
          ))}
          {selectedUnverified > 0 && (
            <p className="mt-1.5 text-[11px] text-amber">
              {unverifiedMarkerLabel(selectedBlockers)}：{selectedUnverified} 条任务没有可核验的执行记录（agent 自述）
            </p>
          )}
          {selectedFailures.map((reason) => (
            <p key={reason} className="mt-1 break-words text-[11px] text-salmon">
              失败理由（Runner 原文）：{reason}
            </p>
          ))}
        </div>
      )}
    </div>
  );
}
