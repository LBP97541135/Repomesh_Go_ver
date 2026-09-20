import { useEffect, useMemo, useRef, useState, type PointerEvent as ReactPointerEvent, type ReactNode } from "react";
import { ChevronLeft, FileText, PanelLeftOpen, X } from "lucide-react";
import { PrTrainCard, type TrainCarSpec } from "./PrTrainCard";
import { DispatchTree } from "./DispatchTree";
import { FocusPanel } from "./FocusPanel";
import { deriveStepStates, STEP_LABELS } from "./treeModel";
import type { FocusEntry } from "./treeModel";
import { IconBolt, IconUser } from "./treeIcons";
import type { DagExecutionView } from "../../types";
import type { DiscoveryView, IssueDetailView, TaskDisplayStatus } from "../../api/contract";
import {
  composeRequirementText,
  parseRequirementDocument,
  resolveProjectId,
  type CreateIssueRequest,
} from "../../api/issues";
import { fetchIssueDetail, fetchMainRoomConversation } from "../../api/rooms";
import { listConversationMessages, submitMessage, type ConversationMessage } from "../../api/conversations";
import { listPlanTasks, type PlanTaskItem } from "../../api/taskTree";
import { fetchTestEvidence, type TestEvidenceView } from "../../api/testEvidence";
import { appendIssueRepository } from "../../api/issueScope";
import { getPlan, interruptPlan, listPlanRevisions, type InterruptOutcomeView, type PlanRevisionView } from "../../api/plans";
import {
  buildDeliveryManifest,
  getLatestDeliveryManifest,
  type DeliveryManifestView,
} from "../../api/deliveryManifest";
import { approveTask, rejectTask } from "../../api/tasks";
import {
  fetchDiscovery,
  materializeDiscovery,
  newIdempotencyKey,
  submitDiscoveryApproval,
  triggerAnalysis,
  triggerCandidates,
  triggerClassification,
  triggerPlan,
} from "../../api/discovery";
import { resolveGovernanceAgent, type GovernanceAgent } from "../../api/decisions";
import { listChangeSets, mergeChangeSet } from "../../api/scm";
import { resolveDataSourceMode } from "../../api/source";
import { allProjectRepositories } from "../../api/projects";
import { allCreationOptions, type CreationOptions } from "../../api/projectIssues";
import { selectCandidates, supplementCheck, confirmSupplements } from "../../api/discoverySelection";
import { Modal } from "../../components/Modal";
import { subscribeEvents } from "../../api/events";
import { autoTrigger } from "./autoTrigger";
import { useIssueFlowState } from "./useIssueFlowState";
import { PlanDagCapsule } from "../../components/PlanDagCapsule";
import { SupervisionPolicyDialog } from "../../components/SupervisionPolicyDialog";
import { AIChatInput } from "../../components/ui/ai-chat-input";
import { errText } from "../../display";
import { ApiError } from "../../api/client";

/** 工作台（方案 A「树贯穿一生」· 2026-09-17 用户确认原型 dispatch-tree-lifecycle.html）。
 *
 *  一个 issue = 一棵树：新会话是欢迎式输入；既有会话左侧是下发任务树
 *  （规划期 ①-⑤ 步骤 + 两个人工门，物化后换代为 Leader/任务/测试组），
 *  右侧是焦点详情（步骤卡与门操作 / Manager 主会话 / 任务房间消息流）。
 *  顶部四点链路条（规划/执行/审核/交付）是唯一进度条，数据驱动。
 *
 *  数据节奏：详情 + 发现链 + 任务树 + 当前会话消息每 5s 静默轮询；
 *  处理员自动推进（发现链哪步待开始就自动触发哪步）沿用既有回路，
 *  人审门（分档审批、物化确认）永远由人操作。 */

const DOC_ACCEPT = ".txt,.md,.docx,.pdf,.odt,.rtf";
const POLL_MS = 5000;

/** 创建阻断原因 code → 人话（2026-09-20）。
 *
 *  后端给的是机器 code（`blockingReasons`），原样打出来等于让人拿代号去猜；
 *  而这里恰恰是「App 没装好」最容易被撞见的地方——用户原话就是别让它以一句
 *  报错的形式冒出来。**只做措辞映射**：能不能提交仍由 `options.canSubmit` 决定，
 *  这里不重算任何判定，认不出的 code 原样显示（不许吞）。 */
const BLOCKING_LABEL: Record<string, string> = {
  NO_AVAILABLE_REPOSITORIES: "没有可选的仓库——项目里的仓还没接入，或 GitHub App 授权不足",
  APP_AUTHORIZATION_UNCONFIRMED: "GitHub App 未覆盖这个仓库",
  CONFIGURATION_NOT_READY: "执行配置未完成",
};

/** App 相关的那两个 code：撞上它们时补一个去处（仓库页顶部有就地引导与直链）。
 *  ALL_REPOSITORIES_UNCOVERED 之类的新 code 不在这里硬编码——认不出就只给文案。 */
const APP_RELATED_BLOCKERS = new Set(["NO_AVAILABLE_REPOSITORIES", "APP_AUTHORIZATION_UNCONFIRMED"]);

/** 发现链步号 → 触发端点的幂等键前缀（与发现链四步触发同一套键位）。 */
const STEP_KEY_BY_STEP = {
  1: "analysis",
  2: "candidates",
  3: "classification",
  4: "plan",
} as const;

/** 需求文本里「用户手打的话」与「附件文档解析全文」的分界（U+2063 不可见分隔符）。
 *  定义已上移到 `../../api/issues`（它是需求文本这个线上字段的形状，读写两侧
 *  必须同一份）——这里只留这条指向。 */

/** 当前焦点的会话 id：MGR/步骤 → 主会话；任务 → 该任务协作房间；测试组 → 无。
 *
 *  测试组**没有自己的会话**：它的读面是测试证据（`GET /issues/{id}/tests`），
 *  不是消息流。这里必须显式返回 null——否则它会落到下面的兜底、拿到**主会话**的
 *  id，于是右栏在测试房间里弹出一个输入框，人往里写的话会发到 Manager 房间去。 */
function entryConvIdOf(
  entry: FocusEntry | null,
  detail: IssueDetailView | null,
  taskById: Map<string, PlanTaskItem>,
): string | null {
  if (!detail || entry === null) return null;
  // 测试组（证据读面，没有会话）与阶段历史（只读回看）都不该有输入框。
  if (entry.kind === "tests" || entry.kind === "stage") return null;
  if (entry.kind === "task") return taskById.get(entry.taskId)?.conversationId ?? null;
  return detail.source?.conversationId ?? null;
}

/** 分栏拖拽手柄：拖动改右栏宽度，双击复位。
 *
 *  2026-09-20 用户要求："聊天框、信息框、测试框的大小和比例都不能调整，需要有
 *  个动态调整的能力"。此前右栏宽度写死在 FocusPanel 的根元素上（`w-[400px]`），
 *  界面上没有任何调整入口。
 *
 *  两个实现取舍：
 *   · 用 **pointer 事件**而不是 mouse 事件：pointer 天然覆盖鼠标/触控/笔，而且
 *     `setPointerCapture` 之后即使指针移出手柄（快速拖动时必然发生）也能继续收到
 *     事件 —— 只用 mousemove 会在移出元素那一刻丢事件，手感是"拖着拖着断了"。
 *   · 拖动期间给 body 加 `col-resize` + `user-select: none`：否则一路拖过去会把
 *     页面文字选中，光标也会在进入其它元素时变回箭头。
 *  命中区 6px、视觉只有 1px 中线：不为了好看让人瞄不准。 */
function PanelResizeHandle({
  width,
  setWidth,
  min,
  max,
}: {
  width: number;
  setWidth: (next: number) => void;
  min: number;
  max: number;
}) {
  const dragging = useRef(false);
  const startX = useRef(0);
  const startWidth = useRef(0);
  const onPointerDown = (event: ReactPointerEvent<HTMLDivElement>) => {
    dragging.current = true;
    startX.current = event.clientX;
    startWidth.current = width;
    event.currentTarget.setPointerCapture(event.pointerId);
    document.body.style.cursor = "col-resize";
    document.body.style.userSelect = "none";
  };
  const onPointerMove = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (!dragging.current) return;
    // 右栏在右边：指针往左移 = 右栏变宽。
    const next = startWidth.current - (event.clientX - startX.current);
    setWidth(Math.min(max, Math.max(min, next)));
  };
  const finish = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (!dragging.current) return;
    dragging.current = false;
    try {
      event.currentTarget.releasePointerCapture(event.pointerId);
    } catch {
      /* 指针已经释放过了，忽略 */
    }
    document.body.style.cursor = "";
    document.body.style.userSelect = "";
  };
  return (
    <div
      role="separator"
      aria-orientation="vertical"
      aria-label="拖动调整详情面板宽度"
      title="拖动调整宽度（双击复位到 400px）"
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={finish}
      onPointerCancel={finish}
      onDoubleClick={() => setWidth(400)}
      className="group relative w-1.5 flex-none cursor-col-resize"
    >
      <span className="absolute inset-y-0 left-1/2 w-px -translate-x-1/2 bg-line transition-colors group-hover:bg-[var(--tree-acc)]" />
    </div>
  );
}

export function WorkbenchPage({
  projectId,
  projectName,
  issueId,
  onCreateIssue,
  onBack,
  onToast,
}: {
  projectId: string;
  projectName: string;
  /** null = 新会话；否则为既有 issue 的 id */
  issueId: string | null;
  onCreateIssue: (
    input: CreateIssueRequest,
    idempotencyKey: string,
  ) => Promise<{ issue_id: string }>;
  /** 顶栏「‹ 议题列表」：回 issue 列表（外壳负责路由）。新会话态不渲染。 */
  onBack?: () => void;
  onToast: (text: string) => void;
}) {
  const isNew = issueId === null;

  const [detail, setDetail] = useState<IssueDetailView | null>(null);
  const [loading, setLoading] = useState(!isNew);
  const [error, setError] = useState<string | null>(null);
  const [reload, setReload] = useState(0);
  const [activeEntry, setActiveEntry] = useState<FocusEntry | null>(null);
  /** 右栏可折叠(2026-09-20 移植主线 0c7a54a1):收起后左树铺满,窄条上一个展开钮。 */
  const [focusOpen, setFocusOpen] = useState(true);
  /** 右栏宽度（可拖拽分栏）。
   *
   *  2026-09-20 用户要求："聊天框、信息框、测试框的大小和比例都不能调整，需要有
   *  个动态调整的能力"。此前右栏是写死的 `w-[400px] flex-none`（FocusPanel 根元素），
   *  没有任何调整入口。
   *  宽度存 localStorage：这是"我的界面偏好"，换 issue 不该重置、刷新也该留着。
   *  取值时校验范围 —— 存进去过一个畸形值（比如手改 localStorage）时回落到缺省，
   *  而不是把界面撑成一条缝。 */
  const PANEL_MIN_WIDTH = 320;
  const PANEL_MAX_WIDTH = 900;
  /** 会话栏保底宽度。
   *
   *  2026-09-21 用户报「有时候会出现会话遮挡」。实测根因不是浮层盖住，而是
   *  **上限写死 900** 撞上可用宽度：1280 视口减去 236 侧栏只剩 1044，右栏拉到
   *  900 时左栏被 `flex-1 min-w-0` 压到 ~138px —— 会话栏被挤成一条缝，看起来
   *  就是"被盖住了"。上限改成按容器实测宽度算，左栏永远留得住 420px。 */
  const CONVERSATION_MIN_WIDTH = 420;
  const ROW_HANDLE_WIDTH = 6;
  const [panelWidth, setPanelWidth] = useState<number>(() => {
    const saved = Number(window.localStorage.getItem("repomesh.panel-width"));
    return Number.isFinite(saved) && saved >= 320 && saved <= 900 ? saved : 400;
  });
  /** 左树+手柄+右栏这一行的实测宽度：右栏上限从它算，而不是从常量算。 */
  const rowRef = useRef<HTMLDivElement | null>(null);
  const [rowWidth, setRowWidth] = useState(0);
  useEffect(() => {
    const el = rowRef.current;
    if (!el) return;
    const measure = () => setRowWidth(el.clientWidth);
    measure();
    if (typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(measure);
    observer.observe(el);
    return () => observer.disconnect();
  }, [detail]);
  const panelMaxWidth =
    rowWidth > 0
      ? Math.max(PANEL_MIN_WIDTH, rowWidth - CONVERSATION_MIN_WIDTH - ROW_HANDLE_WIDTH)
      : PANEL_MAX_WIDTH;
  // 窗口变小 / 侧栏展开后，先前存下的宽度可能已经超过新上限 —— 拉回来，
  // 而不是让会话栏继续被挤（"我的界面偏好"要留，但不能以压垮会话为代价）。
  useEffect(() => {
    setPanelWidth((prev) => Math.min(prev, panelMaxWidth));
  }, [panelMaxWidth]);
  useEffect(() => {
    window.localStorage.setItem("repomesh.panel-width", String(panelWidth));
  }, [panelWidth]);
  /** 静默轮询与首载的界线：换 issue 才整页 loading，轮询只换数据不闪屏
   *  （树与右栏每 5s 卸载重挂正是「一闪一闪」的根源，旧工作台同款保护）。 */
  const loadedIssueRef = useRef<string | null>(null);
  /** 这个 issue **不属于当前项目**（后端 404）。
   *
   *  2026-09-21 用户实测：在 e2e 项目下打开 sealor 的 issue 链接，页面显示
   *  「该功能在当前服务端版本尚未就绪（404）——请稍后重试或联系部署者升级」，
   *  而且**每 5 秒重试一次**，日志里刷成 404 风暴。两句都是假的：
   *    · 不是"服务端版本没就绪"，是这个 issue 在**别的项目**里（真话要能指导动作：
   *      切项目，而不是等升级）；
   *    · 重试没有意义 —— 项目没切之前，同一条请求永远 404。轮询必须停。 */
  const [foreignIssue, setForeignIssue] = useState(false);

  useEffect(() => {
    if (isNew) {
      loadedIssueRef.current = null;
      setDetail(null);
      setLoading(false);
      setError(null);
      setForeignIssue(false);
      setActiveEntry(null);
      return;
    }
    const firstVisit = loadedIssueRef.current !== issueId;
    let cancelled = false;
    if (firstVisit) {
      loadedIssueRef.current = issueId;
      setLoading(true);
      setError(null);
      setForeignIssue(false);
      // 首次进入 issue：右栏自动跳 Manager 主房间对话（2026-09-18 用户裁决；
      // 2026-09-20 LBP 线搬运时漏掉，右栏只剩「点击左侧步骤 / 任务行」空态）。
      setActiveEntry({ kind: "mgr" });
    }
    fetchIssueDetail(issueId, projectId)
      .then((d) => {
        if (cancelled) return;
        setDetail(d);
        setLoading(false);
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        // 404 单独说人话：这一条不是"取数失败"，是"你打开的是别的项目的 issue"。
        if (err instanceof ApiError && err.status === 404) {
          setForeignIssue(true);
          setError(
            `这个 issue 不在当前项目（${projectName ?? projectId}）里 —— 它属于别的项目。` +
              `请先在左上角切换到它所属的项目再打开；在切换之前，这里不会重试（重试必然还是 404）。`,
          );
          setLoading(false);
          return;
        }
        setError(errText(err));
        setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [projectId, issueId, isNew, reload]);

  useEffect(() => {
    // 跨项目 404 时停轮询：同一条请求在项目切换之前永远是 404，每 5 秒打一次
    // 只是把真故障淹进噪音里（线上实测 15 分钟 16 条）。
    if (isNew || foreignIssue) return;
    const timer = window.setInterval(() => setReload((n) => n + 1), POLL_MS);
    return () => window.clearInterval(timer);
  }, [isNew, foreignIssue]);

  // ── 发现链读投影（树的数据源）：SSE 主通道 + 首拍，5s 大轮询兜底 ──
  const [discovery, setDiscovery] = useState<DiscoveryView | null>(null);
  const discoveryIssueKey = detail?.issue_id ?? null;
  useEffect(() => {
    if (isNew || discoveryIssueKey === null) {
      setDiscovery(null);
      return;
    }
    let cancelled = false;
    // 依赖按 issue 标识收敛：detail 每 5s 轮询换新身份，若按对象进依赖，
    // 订阅会被反复重建——树每几秒重连一次就是闪的另一半。
    const refresh = () =>
      fetchDiscovery(discoveryIssueKey)
        .then((view) => !cancelled && setDiscovery(view))
        .catch(() => undefined);
    refresh(); // 首拍：SSE 连上前树上先有数据
    // SSE 主通道（2026-09-20 并入主线 Phase 4）：discovery_step 事件直接带最新
    // 读投影；task_status 触发右栏同拍刷新；重连后的 hello 补断线间隙。
    // 原先的 2.5s 轮询撤除——服务端每 2s 快拍推差量，比轮询更及时也更省。
    const unsubscribe = subscribeEvents(discoveryIssueKey, {
      onDiscoveryStep: (view) => {
        if (!cancelled) setDiscovery(view);
      },
      onTaskStatus: () => {
        if (!cancelled) setReload((n) => n + 1);
      },
      onWorkerHealth: () => undefined, // 失败步/疑似中断由观测告警面消费，工作台不弹
      onHello: () => {
        if (cancelled) return;
        setReload((n) => n + 1);
        refresh();
      },
    });
    return () => {
      cancelled = true;
      unsubscribe();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isNew, discoveryIssueKey]);

  // ── 治理决策主体（人工门与自动推进的「谁在操作」） ──
  // ── 测试团队的记录（task 单点 / DAG 节点集成 / 跨仓库联调回归）──
  //  2026-09-20：此前树上那一行「测试组 · db-test」是写死的文案，永远显示
  //  「等待上游开发任务全部完成」，而这些记录其实一直在产生（只以一个退出码的
  //  形式存在）。这里与发现链同一节拍轮询，让那一行说真话。
  const [testEvidence, setTestEvidence] = useState<TestEvidenceView | null>(null);
  useEffect(() => {
    if (isNew || discoveryIssueKey === null) {
      setTestEvidence(null);
      return;
    }
    let cancelled = false;
    const tick = () =>
      fetchTestEvidence(discoveryIssueKey)
        .then((view) => !cancelled && setTestEvidence(view))
        .catch(() => undefined);
    tick();
    const timer = window.setInterval(tick, 5000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [isNew, discoveryIssueKey, reload]);

  const [principal, setPrincipal] = useState<GovernanceAgent | null>(null);
  const principalOrgKey = detail?.organization_id ?? null;
  useEffect(() => {
    // 2026-09-19 修正：此前 `principalOrgKey === null` 时直接返回，导致 issue 没有
    // organization_id 时治理主体永远解析不出来——而驱动器要求 principal 非空，
    // 于是「处理员自动推进」整条链一次都不开火（建完 issue 后永远停在①等待前序，
    // 库里连 issue_discoveries 行都不会有）。resolveGovernanceAgent 本身**就是**
    // 按可空设计的（其注释：issue 的 organization_id 可能为 null，那时不加组织筛选），
    // 所以这里只需挡住 isNew，把 null 原样交给它。
    if (isNew) return;
    let cancelled = false;
    resolveGovernanceAgent(principalOrgKey)
      .then((agent) => !cancelled && setPrincipal(agent))
      .catch(() => !cancelled && setPrincipal(null));
    return () => {
      cancelled = true;
    };
  }, [isNew, principalOrgKey]);

  // ── 处理员自动推进：发现链哪步「待开始」就自动触发哪步。
  //    分档门（③审）与物化门（⑤审）不在此列——读模型在门没过前不会把步进器
  //    走到下一步，自动触发天然越不过人审门。 ──
  useEffect(() => {
    if (resolveDataSourceMode() === "replay") return;
    if (!discovery || !detail || !principal) return;
    // 人工参与：③ 的**分档计算**不是人审门（人审门是它的**审批**）。候选一到就该跑，
    // 但协调器只管 ai 模式的 issue，前端驱动器此前又只在 step_state==="idle" 时开火
    // —— 而候选落库后 step_state 已经是 "done"，于是人工参与的链路在 ② 之后又停死
    // 一次（线上实测：② 选完「让 AI 推断」，③ 永远"等待前序"）。
    // 这里按**产物缺什么**补这一下，与 ② 的选择门（treeModel 里改成按 candidates
    // 判定）配对：人选完 → 候选落库 → 这一步把分档算出来 → 停在 ③ 的人审门。
    if (issueHitl === "hitl" && discovery.candidates !== null && discovery.classification === null) {
      const key = `${discovery.issue_id}:classification`;
      if (!autoTrigger.has(key)) {
        autoTrigger.set(key, newIdempotencyKey("classification"));
        triggerClassification(detail.issue_id, {
          created_by_agent_id: principal.agentId,
          idempotency_key: autoTrigger.get(key)!,
        })
          .then(() => setReload((n) => n + 1))
          .catch((err: unknown) => setStepError({ step: 3, message: errText(err) }));
      }
      return;
    }
    if (discovery.step_state !== "idle" || discovery.running_task_id !== null) return;
    // ④ 的 idle 有两义：分档未过（等人）或分档已过（该跑计划）——只有后者开火
    if (discovery.step === 4 && discovery.approval?.state !== "approved") return;
    // 人工参与：② 是「待人选择」的门，驱动器不越门（人选完自己会触发链路）
    if (discovery.step === 2 && issueHitl !== "ai") return;
    // 人工参与：④ 生成计划也是门（2026-09-20）。此前这里不挡 —— 自动托管照旧
    // 替人把计划生成了，"生成计划"这一步在人工参与模式下根本没有确认项。
    // 现在等人点右栏/聊天里的「生成计划」按钮，与 ③⑤ 同一套写回路。
    if (discovery.step === 4 && issueHitl !== "ai") return;
    // 空范围不开火：分档把必需+可能全排空时 Plan 端点必 409，白打一轮
    if (discovery.step === 4) {
      const picked =
        (discovery.classification?.required?.length ?? 0) +
        (discovery.classification?.maybe?.length ?? 0);
      if (picked === 0) return;
    }
    const key = `${discovery.issue_id}:${discovery.step}`;
    if (autoTrigger.has(key)) return;
    autoTrigger.set(key, newIdempotencyKey(STEP_KEY_BY_STEP[discovery.step]));
    const payload = {
      created_by_agent_id: principal.agentId,
      idempotency_key: autoTrigger.get(key)!,
    };
    const fire =
      discovery.step === 1
        ? triggerAnalysis(detail.issue_id, payload)
        : discovery.step === 2
          ? triggerCandidates(detail.issue_id, payload)
          : discovery.step === 3
            ? triggerClassification(detail.issue_id, payload)
            : triggerPlan(detail.issue_id, payload);
    fire
      .then(() => {
        driverFailures.current.delete(key);
        setStepError(null);
        setReload((n) => n + 1);
      })
      .catch((err: unknown) => {
        // 2026-09-20 修：此前是 `catch(() => autoTrigger.delete(key))` —— 静默吞掉原因，
        // 下一轮 5s 轮询又开火，用户只看到「卡住」，什么都看不到。
        // 现在连撞三次就把原因摆到右栏并停止自动重发（改完再点「重试这一步」）。
        const attempts = (driverFailures.current.get(key) ?? 0) + 1;
        driverFailures.current.set(key, attempts);
        if (attempts >= 3) {
          setStepError({ step: discovery.step, message: errText(err) });
          return;
        }
        autoTrigger.delete(key);
      });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [discovery, principal, detail?.issue_id]);

  // ── 任务树（物化后）：plans/{planId}/tasks ──
  const planId = discovery?.materialization?.plan_id ?? null;
  const materialized = discovery?.materialization?.status === "materialized";
  const [tasks, setTasks] = useState<PlanTaskItem[] | null>(null);
  useEffect(() => {
    if (!planId || !materialized) {
      setTasks(null);
      return;
    }
    let cancelled = false;
    Promise.resolve(projectId)
      .then((pid) => (pid ? listPlanTasks(pid, planId) : null))
      .then((items) => !cancelled && setTasks(items))
      .catch(() => {
        if (!cancelled) setTasks(null);
      });
    return () => {
      cancelled = true;
    };
  }, [projectId, planId, materialized, reload]);

  // ── 计划换代状态：plans/{planId}（planVersion + replanState） ──
  //
  //  收集窗（replanState=deprecated）是**人工打断判定"影响计划"之后**开的，
  //  界面必须能看见它：否则用户点了打断、看到"已受理"，却不知道计划是否真的在动。
  const [planState, setPlanState] = useState<{ planVersion: string; replanState: string } | null>(null);
  useEffect(() => {
    if (!planId) {
      setPlanState(null);
      return;
    }
    let cancelled = false;
    Promise.resolve(projectId)
      .then((pid) => (pid ? getPlan(pid, planId) : null))
      .then((plan) => {
        if (cancelled || !plan) return;
        const version = typeof plan.planVersion === "string" ? plan.planVersion : "";
        const state = typeof plan.replanState === "string" ? plan.replanState : "";
        setPlanState({ planVersion: version || "—", replanState: state });
      })
      .catch(() => {
        if (!cancelled) setPlanState(null);
      });
    return () => {
      cancelled = true;
    };
  }, [projectId, planId, reload]);

  // ── 计划换代历史（A3）：plans/{planId}/revisions ──
  //
  //  这条历史一直落在 public.plans.revisions 里，但此前**只有落库没有读面**：
  //  计划换过几版、每版为什么换、增删了哪些仓库，界面上看不到。
  const [planRevisions, setPlanRevisions] = useState<PlanRevisionView[] | null>(null);
  useEffect(() => {
    if (!planId) {
      setPlanRevisions(null);
      return;
    }
    let cancelled = false;
    Promise.resolve(projectId)
      .then((pid) => (pid ? listPlanRevisions(pid, planId) : null))
      .then((page) => {
        if (cancelled) return;
        setPlanRevisions(page?.items ?? []);
      })
      .catch(() => {
        if (!cancelled) setPlanRevisions(null);
      });
    return () => {
      cancelled = true;
    };
  }, [projectId, planId, reload]);

  // ── 跨仓交付的一致版本清单（评委建议②）──
  //
  //  从一次交付展开完整版本清单：各仓提交/分支/PR、数据库迁移与基线、测试证据，
  //  以及失败发生在哪个仓库、哪个阶段。"还没有清单"由 api 层折成 null（不是错误）。
  //
  //  2026-09-20：这里原先把 `reload`（每 5 秒 +1 的轮询计数器）也当依赖，于是
  //  工作台开着的时候每 5 秒都去取一次清单 —— 而没有清单时后端回 404，服务端
  //  日志 35 分钟刷 444 条"请求失败"。清单只由本页的"生成清单"按钮创建（它自己
  //  会把结果写进 state），所以按 issue/物化状态取一次就够，不需要跟着轮询走。
  const [deliveryManifest, setDeliveryManifest] = useState<DeliveryManifestView | null>(null);
  useEffect(() => {
    if (!detail || !materialized) {
      setDeliveryManifest(null);
      return;
    }
    let cancelled = false;
    Promise.resolve(projectId)
      .then((pid) => (pid ? getLatestDeliveryManifest(pid, detail.issue_id) : null))
      .then((manifest) => {
        if (!cancelled) setDeliveryManifest(manifest);
      })
      .catch(() => {
        // 真失败按"没有清单"显示：卡片自己会给"生成清单"入口。
        if (!cancelled) setDeliveryManifest(null);
      });
    return () => {
      cancelled = true;
    };
  }, [projectId, detail?.issue_id, materialized]);

  // ── 仓库显示名（详情卡与步骤卡用） ──
  const [repoNameById, setRepoNameById] = useState<Record<string, string>>({});
  const repoListKey = detail?.issue_id ?? null;
  useEffect(() => {
    if (repoListKey === null) return;
    let cancelled = false;
    allProjectRepositories(projectId)
      .then((repos) => {
        if (cancelled) return;
        const names: Record<string, string> = {};
        for (const r of repos.items) names[r.id] = r.displayName;
        setRepoNameById(names);
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, [projectId, repoListKey]);
  void repoNameById;

  // ── 右栏焦点与会话消息 ──
  const [entryMessages, setEntryMessages] = useState<ConversationMessage[] | null>(null);
  const taskById = new Map((tasks ?? []).map((t) => [t.id, t]));
  const entryConvId = entryConvIdOf(activeEntry, detail, taskById);
  useEffect(() => {
    if (entryConvId === null) {
      setEntryMessages(null);
      return;
    }
    let cancelled = false;
    Promise.resolve(projectId)
      .then((pid) => (pid ? listConversationMessages(pid, entryConvId, { limit: 50 }) : null))
      .then((page) => {
        if (cancelled || !page) return;
        setEntryMessages(page.items);
      })
      .catch(() => {
        if (!cancelled) setEntryMessages([]);
      });
    return () => {
      cancelled = true;
    };
  }, [projectId, entryConvId, reload]);
  // ── Manager 房间的真实消息（AgentTeams 团队房，5s 轮询与房间页同拍）──
  // null = 该 issue 还没有可进的房：回落到上面的会话时间线，不显示成"房间空"。
  const [roomMessages, setRoomMessages] = useState<ConversationMessage[] | null>(null);
  useEffect(() => {
    if (resolveDataSourceMode() === "replay" || !issueId) {
      setRoomMessages(null);
      return;
    }
    let cancelled = false;
    const load = () => {
      if (document.visibilityState !== "visible") return; // 后台标签页不空转打后端
      fetchMainRoomConversation(issueId, projectId ?? undefined)
        .then((items) => {
          if (!cancelled) setRoomMessages(items);
        })
        .catch(() => undefined);
    };
    load();
    const timer = window.setInterval(load, 5000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [projectId, issueId, reload]);


  const clarifyPending =
    !!discovery &&
    discovery.analysis !== null &&
    !discovery.analysis.sufficient &&
    discovery.analysis.questions.length > 0;

  // 追问待答且右栏没有选中条目时，自动跳到步 1（2026-09-20 移植主线 5743fbc2）：
  // ① 现在会停在「待人答」门态，但人不点左树就看不到追问与回答框 —— 自动选中
  // 让人一进页就能答。已经选着别处时不抢焦点（activeEntry === null 才动）。
  useEffect(() => {
    if (clarifyPending && activeEntry === null) {
      setActiveEntry({ kind: "step", step: 1 });
    }
  }, [clarifyPending, activeEntry]);

  // ── 右栏输入框：追问回答（规划期）或往当前会话发消息（真端点） ──
  const [sending, setSending] = useState(false);
  const handleEntrySend = (text: string) => {
    if (!detail || sending) return;
    setSending(true);
    const settle = () => setSending(false);
    if (clarifyPending && activeEntry?.kind === "step" && activeEntry.step <= 2 && discovery) {
      // 追问回答走发现链写回路,需要决策主体
      if (!principal) {
        settle();
        onToast("决策主体未接入，无法提交回答。");
        return;
      }
      autoTrigger.delete(`${detail.issue_id}:1`);
      triggerAnalysis(detail.issue_id, {
        created_by_agent_id: principal.agentId,
        idempotency_key: newIdempotencyKey("analysis"),
        answers: [{ question: discovery.analysis!.questions.join(" ／ "), answer: text }],
      })
        .then(() => {
          onToast("已回答，处理员继续分析");
          setReload((n) => n + 1);
        })
        .catch((err: unknown) => onToast(`提交回答失败：${errText(err)}`))
        .finally(settle);
      return;
    }
    if (!entryConvId) {
      settle();
      return;
    }
    Promise.resolve(projectId).then((pid) => {
      if (!pid) {
        settle();
        return;
      }
      submitMessage(pid, entryConvId, { body: text }, crypto.randomUUID())
        .then(() => setReload((n) => n + 1))
        .catch((err: unknown) => onToast(`发送失败：${errText(err)}`))
        .finally(settle);
    });
  };

  // ── 人工门（分档审批 / 生成计划 / 物化确认）──
  //
  //  2026-09-20：④ 生成计划也进这道门（用户反馈"生成计划没有人工确认项"）。
  //  人工参与模式下分档批准后停在 ④，等人点「生成计划」才派发规划 run。
  const [gateBusy, setGateBusy] = useState<"approveTiers" | "plan" | "materialize" | null>(null);
  const [gateError, setGateError] = useState<string | null>(null);
  // ── 候选分流（2026-09-18 用户裁定）：②在聊天室里选——人勾选（→③依赖图查漏）
  //    或 AI 推断（→③走既有分档审批门），人闸与模型闸各一道。
  const [selectionOpen, setSelectionOpen] = useState(false);
  const [selectionBusy, setSelectionBusy] = useState(false);
  const [pickedRepos, setPickedRepos] = useState<Record<string, boolean>>({});
  /** 每个门的连续失败次数：自动托管代行人工门时，失败够 3 次就停手并上屏。
   *
   *  2026-09-20 线上实测：此前没有这道闸 —— 自动托管在 discovery 每次 2.5s 轮询后
   *  重新开火，一条**永远过不去**的门（线上是"全部仓库都被排除"的分档审批，每次
   *  必 409）把 `POST /discovery/approval` 打成了 26632 次。与步进器的
   *  driverFailures 同一套规矩：连撞三次就把原因摆出来，不再静默重发。 */
  const gateFailures = useRef(0);
  /** 监管策略弹窗（迁移 5-1b）：草稿卡片上的「配置 / 修改」。 */
  const [policyOpen, setPolicyOpen] = useState(false);
  const handleGate = (action: "approveTiers" | "plan" | "materialize") => {
    if (!detail || !discovery) return;
    if (resolveDataSourceMode() === "replay") {
      onToast("回放模式不写后端：人工门需要 ?source=live 才能真实执行。");
      return;
    }
    if (!principal) {
      setGateError("决策主体未接入（花名册无活跃 Org Leader），无法提交。");
      return;
    }
    setGateBusy(action);
    setGateError(null);
    if (action === "plan") {
      // ④ 生成计划（人工参与模式的第三道门）：派发规划 run，产物由协调器收回来
      // 落进发现链（与自动托管走的是同一个写端点，人闸只是决定"谁按下这一下"）。
      triggerPlan(detail.issue_id, {
        created_by_agent_id: principal.agentId,
        idempotency_key: newIdempotencyKey("plan"),
      })
        .then(() => {
          setGateBusy(null);
          gateFailures.current = 0;
          onToast("已派发计划生成：Leader 产出计划后回到这里确认物化");
          setReload((n) => n + 1);
        })
        .catch((err: unknown) => {
          setGateBusy(null);
          gateFailures.current += 1;
          setGateError(errText(err));
        });
      return;
    }
    if (action === "approveTiers") {
      if (discovery.classification_evidence_version === null) {
        setGateBusy(null);
        setGateError("分档证据尚未生成，无法批准。");
        return;
      }
      submitDiscoveryApproval(detail.issue_id, {
        decided_by_agent_id: principal.agentId,
        idempotency_key: newIdempotencyKey("approval"),
        decision: "approved",
        reason: "",
        adjustments: [],
        evidence_version: discovery.classification_evidence_version,
      })
        .then(() => {
          setGateBusy(null);
          gateFailures.current = 0;
          onToast("分档已批准；处理员继续生成计划");
          setReload((n) => n + 1);
        })
        .catch((err: unknown) => {
          setGateBusy(null);
          gateFailures.current += 1;
          setGateError(errText(err));
        });
      return;
    }
    materializeDiscovery(detail.issue_id, {
      created_by_agent_id: principal.agentId,
      idempotency_key: newIdempotencyKey("materialize"),
    })
      .then(() => {
        setGateBusy(null);
        gateFailures.current = 0;
        onToast("物化完成：编制已组装、批次已下发——左侧树已换代");
        setActiveEntry(null);
        setReload((n) => n + 1);
      })
      .catch((err: unknown) => {
        setGateBusy(null);
        gateFailures.current += 1;
        setGateError(errText(err));
      });
  };

  // ── HITL 模式(既有会话):读建项入口存的选择;没存过默认「人工参与」——
  //    门等真人是最保守的缺省,不会替任何人做主。 ──
  // ── 候选分流写回路 ──
  const handleChooseManual = () => setSelectionOpen(true);
  const handleChooseAI = () => {
    if (!detail || !principal || selectionBusy) return;
    setSelectionBusy(true);
    triggerCandidates(detail.issue_id, {
      created_by_agent_id: principal.agentId,
      idempotency_key: newIdempotencyKey("candidates"),
    })
      .then(() => {
        onToast("AI 已推断候选，下一步人来审批分档");
        setReload((n) => n + 1);
      })
      .catch((err: unknown) => onToast(`AI 推断失败：${errText(err)}`))
      .finally(() => setSelectionBusy(false));
  };
  const handleSelectionSubmit = () => {
    if (!detail || !principal || selectionBusy) return;
    const repositoryIds = Object.keys(repoNameById).filter((id) => pickedRepos[id]);
    if (repositoryIds.length === 0) {
      onToast("至少勾选一个仓库");
      return;
    }
    setSelectionBusy(true);
    selectCandidates(detail.issue_id, {
      created_by_agent_id: principal.agentId,
      idempotency_key: newIdempotencyKey("selection"),
      repositoryIds,
    })
      .then((receipt) =>
        supplementCheck(detail.issue_id, {
          created_by_agent_id: principal.agentId,
          idempotency_key: newIdempotencyKey("supplement"),
        }).then((check) => ({ receipt, check })),
      )
      .then(({ check }) => {
        setSelectionOpen(false);
        setReload((n) => n + 1);
        onToast(
          check.supplement_state === "pending"
            ? `已按你的勾选定候选；依赖图查出 ${check.supplements.length} 个漏选，待确认`
            : "已按你的勾选定候选，依赖图核对无漏选",
        );
      })
      .catch((err: unknown) => onToast(`勾选提交失败：${errText(err)}`))
      .finally(() => setSelectionBusy(false));
  };
  const handleConfirmSupplements = (repositories: string[]) => {
    if (!detail || !principal || selectionBusy) return;
    setSelectionBusy(true);
    confirmSupplements(detail.issue_id, {
      created_by_agent_id: principal.agentId,
      idempotency_key: newIdempotencyKey("supplement-confirm"),
      repositories,
    })
      .then(() => {
        onToast(`已补入 ${repositories.length} 个仓库，分档完成`);
        setReload((n) => n + 1);
      })
      .catch((err: unknown) => onToast(`确认补充失败：${errText(err)}`))
      .finally(() => setSelectionBusy(false));
  };

  const [issueHitl, setIssueHitl] = useState<"ai" | "hitl">("hitl");
  // 2026-09-20：模式改读**服务端事实**（issue 详情里的 hitlMode，0053 落列）。
  //
  // 此前它只活在浏览器 sessionStorage 里（`repomesh.hitl-mode.<issue>`），而协调器的
  // 自动托管循环在 Go 侧看不到这个值 —— 于是"人工参与"模式下 ③ 分档审批与 ⑤ 物化确认
  // 照样被自动代行，人审门形同不存在；换台机器/换个浏览器，模式还会凭空回到默认。
  // 现在服务端是唯一权威：建项时写入，读面返回，协调器按它停门。
  const issueKey = detail?.issue_id ?? null;
  useEffect(() => {
    if (issueKey === null) return;
    setIssueHitl(detail?.hitlMode === "ai" ? "ai" : "hitl");
    // 观测告警「去处理」的手递手：ObserveAlerts 写 pending-entry=mgr，
    // 从这里读走并清掉，直接打开 Manager 房间。搬运时只留了写的半边，
    // 读的这半边丢了——告警点了没反应。
    try {
      const pending = window.sessionStorage.getItem(`repomesh.pending-entry.${issueKey}`);
      if (pending === "mgr") {
        setActiveEntry({ kind: "mgr" });
        window.sessionStorage.removeItem(`repomesh.pending-entry.${issueKey}`);
      }
    } catch {
      /* 存不进就只在这台浏览器本次会话里失效 */
    }
  }, [issueKey, detail?.hitlMode]);

  // 自动托管:处理员代行人审门(分档审批 → 物化确认),用与真人门同一套写回路;
  // 失败如实落进右栏 gateError,不静默重试。
  useEffect(() => {
    if (resolveDataSourceMode() === "replay") return;
    if (issueHitl !== "ai" || !detail || !discovery || !principal || gateBusy) return;
    // 连撞三次就停手：门一直过不去（例如分档把全部仓库都排除了），再自动重发
    // 只是把同一个 409 打成上万次，原因早已摆在右栏。改完点「重试」再继续。
    if (gateFailures.current >= 3) return;
    if (discovery.classification !== null && discovery.approval?.state !== "approved") {
      // 必需+可能全空 = 模型没选出任何仓：人必须圈定范围，自动托管不代行
      const picked =
        (discovery.classification?.required?.length ?? 0) + (discovery.classification?.maybe?.length ?? 0);
      if (picked > 0) {
        handleGate("approveTiers");
        return;
      }
    }
    if (discovery.integration !== null && discovery.materialization === null) {
      handleGate("materialize");
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [issueHitl, discovery, principal, gateBusy]);

  /** 推进失败的原因（右栏显示）。step 用于只在对应步骤上显示。 */
  const [stepError, setStepError] = useState<{ step: number; message: string } | null>(null);
  /** 每个 (issue,step) 的连续失败次数：够了就停手并上屏，不再静默重发。 */
  const driverFailures = useRef<Map<string, number>>(new Map());
  // 步骤往前走了就把失败痕迹清掉：那是上一轮的事，留着只会误导。
  useEffect(() => {
    setStepError(null);
    driverFailures.current.clear();
  }, [detail?.issue_id, discovery?.step]);

  /** 「忽略追问，强制继续」：需求文本偏短时给用户的另一条路（后端记 forced_continue）。 */
  const handleForceContinue = () => {
    if (!detail || !principal) {
      onToast("决策主体未接入，无法继续。");
      return;
    }
    const key = `${detail.issue_id}:1`;
    autoTrigger.delete(key);
    driverFailures.current.delete(key);
    triggerAnalysis(detail.issue_id, {
      created_by_agent_id: principal.agentId,
      idempotency_key: newIdempotencyKey("analysis"),
      force_continue: true,
    })
      .then(() => {
        setStepError(null);
        onToast("已忽略追问，处理员继续下一步");
        setReload((n) => n + 1);
      })
      .catch((err: unknown) => onToast(`强制继续失败：${errText(err)}`));
  };

  /** ③ 执行中人工打断：把**一个人确认过的**仓库追加进本次 Issue 的仓库范围。
   *
   *  2026-09-20：后端此前没有追加路径（范围只在建 issue 时写入），界面上也没有入口。
   *  这里只做"人确认后追加"这一步；服务端的 409/404 原样上抛，不做自动重试。 */
  const handleAppendRepository = async (repositoryId: string) => {
    if (!detail) throw new Error("issue 还没加载完");
    const pid = await resolveProjectId();
    if (!pid) throw new Error("没有可用项目，无法追加仓库");
    await appendIssueRepository(pid, detail.issue_id, repositoryId);
    onToast("已追加进本次 Issue 的仓库范围");
    setReload((n) => n + 1);
  };

  /** ③ 执行中人工打断：提交一个人点名的仓库，由后端判定它是否影响当前计划。
   *
   *  判定结果**原样回给界面**（ready / affectsPlan / affectedSet / replanQueued），
   *  不粉饰：ready=false 就是"还没就绪、这次没判定"，affectsPlan=true 就是
   *  "收集窗已开、重排 v2 已登记"。前端不替后端猜结论。 */
  const handleInterruptPlan = async (repository: string, note: string): Promise<InterruptOutcomeView> => {
    if (!planId) throw new Error("还没有计划可打断（物化之后才有）");
    const pid = await resolveProjectId();
    if (!pid) throw new Error("没有可用项目，无法打断");
    const outcome = await interruptPlan(pid, planId, { repository, note });
    onToast(
      outcome.affectsPlan
        ? `判定影响当前计划：收集窗已开${outcome.replanQueued ? "，重排 v2 已登记" : "（重排未登记）"}`
        : outcome.ready
          ? "判定：与当前计划无耦合，暂定未重排"
          : "仓库扫描尚未就绪：本次没有判定，稍后可再来一次",
    );
    setReload((n) => n + 1);
    return outcome;
  };

  /** 生成一份交付版本清单快照（幂等键由前端持有，重放返回同一份）。 */
  const handleBuildManifest = async () => {
    if (!detail) throw new Error("issue 还没加载完");
    const pid = await resolveProjectId();
    if (!pid) throw new Error("没有可用项目，无法生成清单");
    const manifest = await buildDeliveryManifest(pid, detail.issue_id, {
      planId: planId ?? undefined,
      idempotencyKey: newIdempotencyKey("manifest"),
    });
    setDeliveryManifest(manifest);
    onToast(
      manifest.status === "consistent"
        ? "清单已生成：各仓代码/数据库/测试三面一致"
        : `清单已生成：${manifest.failureSummary ?? "存在未达成的仓库"}`,
    );
  };
  /** 经理门：blocked 任务的通过/驳回（审核段唯一的写动作）。 */
  const handleDecideTask = async (taskId: string, decision: "approve" | "reject", reason: string) => {
    const projectId = await resolveProjectId();
    if (!projectId) throw new Error("没有可用项目，无法提交审批");
    if (decision === "approve") {
      await approveTask(projectId, taskId, reason);
    } else {
      await rejectTask(projectId, taskId, reason);
    }
    setReload((n) => n + 1);
  };

  /** 交付段收口：按列车顺序（= 任务顺序）逐个合并 PR。
   *
   *  此前这里是 `onToast("已确认合并：…（演示）")` —— 点下去什么都没发生，
   *  而界面上写着"已确认合并"。现在真的调合并端点，失败逐条如实报（哪个仓库、
   *  什么原因），不静默跳过。 */
  const handleConfirmMerge = async () => {
    if (!trainCars) return;
    const cars = trainCars.filter((car) => car.changeSetId && !car.merged);
    if (cars.length === 0) {
      onToast("没有可合并的车厢：要么还没开 PR，要么都已经合并过");
      return;
    }
    const projectId = await resolveProjectId();
    if (!projectId) {
      onToast("没有可用项目，无法合并");
      return;
    }
    let merged = 0;
    const failures: string[] = [];
    for (const car of cars) {
      try {
        await mergeChangeSet(projectId, car.changeSetId!);
        merged += 1;
      } catch (err) {
        failures.push(`${car.repo}：${errText(err)}`);
      }
    }
    setReload((n) => n + 1);
    onToast(
      failures.length === 0
        ? `已按顺序合并 ${merged} 个 PR`
        : `合并 ${merged} 个；${failures.length} 个失败：${failures.join("；")}`,
    );
  };

  const handleRetryStep = (step: 1 | 2 | 3 | 4) => {
    if (!detail || !principal) {
      onToast("决策主体未接入，无法重试。");
      return;
    }
    const key = `${detail.issue_id}:${step}`;
    autoTrigger.set(key, newIdempotencyKey(STEP_KEY_BY_STEP[step]));
    const payload = {
      created_by_agent_id: principal.agentId,
      idempotency_key: autoTrigger.get(key)!,
    };
    const fire =
      step === 1
        ? triggerAnalysis(detail.issue_id, payload)
        : step === 2
          ? triggerCandidates(detail.issue_id, payload)
          : step === 3
            ? triggerClassification(detail.issue_id, payload)
            : triggerPlan(detail.issue_id, payload);
    fire
      .then(() => {
        setStepError(null);
        driverFailures.current.clear();
        setReload((n) => n + 1);
      })
      .catch((err: unknown) => {
        autoTrigger.delete(key);
        onToast(`重试失败：${errText(err)}`);
      });
  };

  // ── 新会话:输入与附件(发送即 createIssue) ──
  /** HITL 模式(入口选择,2026-09-17):ai = 自动托管(处理员代行人审门),hitl = 门等真人。
   *  2026-09-20 起这是**服务端事实**：随建项写进 issue（0053 的 hitl_mode 列），
   *  读面返回、协调器按它停门 —— 不再只存在这台浏览器的 sessionStorage 里。 */
  const [hitlMode, setHitlMode] = useState<"ai" | "hitl">("ai");
  const [draft, setDraft] = useState("");
  const [attachment, setAttachment] = useState<{ filename: string; text: string } | null>(null);
  const [docDragging, setDocDragging] = useState(false);
  const [creating, setCreating] = useState(false);
  const [parsingDocument, setParsingDocument] = useState(false);
  const attempt = useRef<{ input: CreateIssueRequest; key: string } | null>(null);
  const [options, setOptions] = useState<CreationOptions | null>(null);
  const [optionsError, setOptionsError] = useState<string | null>(null);
  const [optionsReload, setOptionsReload] = useState(0);
  const [selectedRepos, setSelectedRepos] = useState<string[]>([]);
  useEffect(() => {
    if (!isNew || resolveDataSourceMode() === "replay") return;
    let cancelled = false;
    setOptions(null); setOptionsError(null); setSelectedRepos([]);
    allCreationOptions(projectId).then(value => { if (!cancelled) setOptions(value); }).catch(e => { if (!cancelled) setOptionsError(errText(e)); });
    return () => { cancelled = true; };
  }, [projectId, isNew, optionsReload]);
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const dragDepth = useRef(0);

  const handleDraftChange = (text: string) => {
    if (attempt.current || creating) return;
    setDraft(text);
  };

  const handleCreateSend = () => {
    const typed = draft.trim();
    if (creating || parsingDocument || !options?.canSubmit || !selectedRepos.length || selectedRepos.length > 100) return;
    if (!typed && !attachment) return;
    const text = attachment ? composeRequirementText(typed, attachment.text) : typed;
    setCreating(true);
    attempt.current ??= { key: crypto.randomUUID(), input: { projectId, requirementText: text, repositoryIds: [...selectedRepos].sort(), expectedCreationContextRevision: options.creationContextRevision, hitlMode } };
    onCreateIssue(attempt.current.input, attempt.current.key)
      .then(() => {
        setDraft("");
        setAttachment(null);
        attempt.current = null;
      })
       .catch((err: unknown) => {
        onToast(`创建失败：${errText(err)}。重试将沿用原提交内容。`);
        const status = (err as { status?: number }).status;
        if (status && status >= 400 && status < 500 && status !== 408 && status !== 429) {
          attempt.current = null;
          setOptionsReload(n => n + 1);
        }
      })
      .finally(() => setCreating(false));
  };

  const handlePickDocument = (file: File | undefined) => {
    if (!file || creating || attempt.current) return;
    setParsingDocument(true);
    parseRequirementDocument(file)
      .then((parsed) => {
        setAttachment({ filename: parsed.filename, text: parsed.text });
        attempt.current = null;
        if (parsed.truncated) onToast(`文档较长，已截断为前 ${parsed.chars} 字`);
      })
      .catch((err: unknown) => onToast(`文档解析失败：${errText(err)}`))
      .finally(() => {
        setParsingDocument(false);
        if (fileInputRef.current) fileInputRef.current.value = "";
      });
  };

  const hour = new Date().getHours();
  const greeting = hour < 12 ? "上午好" : hour < 18 ? "下午好" : "晚上好";

  // ── 四点链路条（唯一进度条，数据驱动）+ DAG 胶囊/执行面板的数据 ──
  const flow = useIssueFlowState(projectId, issueId ?? "", planId, detail, reload);
  const stepStates = deriveStepStates(discovery, issueHitl === "hitl");
  const doneSteps = stepStates.filter((s) => s === "done").length;
  /** 规划五步链条（喂给 DAG 计划板）。标签用 treeModel 的唯一那份，不在这里另抄一遍。 */
  const stepChain = STEP_LABELS.map((label, i) => ({ label, state: stepStates[i] as string }));
  const allTasksDone = !!tasks && tasks.length > 0 && tasks.every((t) => t.status === "done");
  /** 等经理批的任务数（审核段的唯一信号）。 */
  const blockedTasks = tasks?.filter((t) => t.status === "blocked").length ?? 0;
  const STAGES = ["规划", "执行", "审核", "交付"] as const;
  const stageState = (i: number): "done" | "now" | "todo" => {
    if (i === 0) return materialized ? "done" : "now";
    if (i === 1) return materialized ? (allTasksDone ? "done" : "now") : "todo";
    // 审核段（2026-09-20 点亮）：任务跑完会被置 blocked 等经理批 —— 有 blocked
    // 就是"现在轮到你"，全部 done 才算过。此前这里写死 todo（"读面待迁移"），
    // 于是任务明明堆在 blocked 上，界面上看不出该有人做事。
    if (i === 2) return blockedTasks > 0 ? "now" : allTasksDone && tasks !== null && tasks.length > 0 ? "done" : "todo";
    // 交付以「全部任务完成、PR 列车在场」为准。
    if (i === 3) return allTasksDone ? "now" : "todo";
    return "todo";
  };
  /** 链路当前节点（一个词）。
   *  2026-09-20 移植主线 9e1dee3d：顶栏那条四点链路条收编进 DAG 胶囊——
   *  顶栏只留标题，进度收成这一个词；规划期还说清五步走到第几步。
   *  历史入口不丢：点这个词开那一段的阶段历史（见胶囊的 onOpenStage）。 */
  const stageNowIdx = STAGES.findIndex((_, i) => stageState(i) === "now");
  const stageIdx = stageNowIdx >= 0 ? stageNowIdx : STAGES.length - 1;
  const stageLabel = !materialized ? `${STAGES[0]} ${doneSteps}/5` : STAGES[stageIdx];

  /** DAG 的执行态着色输入（2026-09-20 接线）。
   *  此前这里恒传 `execution={null}`，于是 DAG 图**画得出来但颜色是死的**——
   *  已交付/进行中/失败三类节点全长一样，看不出哪个仓跑到哪了。
   *  数据源是任务树读面（`plans/{id}/tasks`），按仓归拢：
   *  一个仓的任务展示态一致就是那个态，**不一致记 null**（读模型说的就是"这一仓
   *  里有不同态"，界面不替它选一个）。
   *  未验证/blocker/失败理由三面任务树读面不提供，如实留空——planDagPanel 对空
   *  值按 0 呈现，不会编出"0 条 blocker"这种话。 */
  const dagExecution = useMemo<DagExecutionView | null>(() => {
    if (!materialized || tasks === null) return null;
    const byTaskStatus: Record<string, TaskDisplayStatus> = {
      done: "succeeded",
      running: "running",
      failed: "failed",
      blocked: "blocked",
      assigned: "pending",
      pending: "pending",
    };
    const counts: Record<string, Record<TaskDisplayStatus, number>> = {};
    for (const t of tasks) {
      const repo = t.repositoryId;
      if (!repo) continue;
      const status = byTaskStatus[t.status] ?? "pending";
      counts[repo] = counts[repo] ?? { pending: 0, running: 0, repairing: 0, blocked: 0, succeeded: 0, failed: 0 };
      counts[repo][status] += 1;
    }
    const byRepository: Record<string, TaskDisplayStatus | null> = {};
    const taskCountByRepository: Record<string, number> = {};
    const unverifiedCountByRepository: Record<string, number> = {};
    const blockerCountByRepository: Record<string, number> = {};
    const failureReasonsByRepository: Record<string, string[]> = {};
    for (const [repo, buckets] of Object.entries(counts)) {
      const total = Object.values(buckets).reduce((a, b) => a + b, 0);
      const present = (Object.keys(buckets) as TaskDisplayStatus[]).filter((s) => buckets[s] > 0);
      byRepository[repo] = present.length === 1 ? present[0] : null;
      taskCountByRepository[repo] = total;
      unverifiedCountByRepository[repo] = 0;
      blockerCountByRepository[repo] = 0;
      failureReasonsByRepository[repo] = [];
    }
    return {
      byRepository,
      taskCountByRepository,
      unverifiedCountByRepository,
      blockerCountByRepository,
      failureReasonsByRepository,
      roundLabel: flow.planState.status === "ready" ? `计划 ${flow.planState.plan.plan_version}` : "本轮",
    };
  }, [materialized, tasks, flow.planState]);

  // ── 交付期:PR 列车按任务顺序组装车厢——有 PR 的 change-set 挂真门禁轮询,
  //    没开 PR 的任务坐「待提交」车厢。车厢读面按 reload 同拍刷新。 ──
  const [trainCars, setTrainCars] = useState<TrainCarSpec[] | null>(null);
  const [trainPid, setTrainPid] = useState<string | null>(null);
  const trainKey = allTasksDone && tasks ? tasks.map((t) => t.id).join(",") : null;
  useEffect(() => {
    if (!trainKey || !tasks) {
      setTrainCars(null);
      return;
    }
    let cancelled = false;
    Promise.resolve(projectId).then((pid) => {
      if (!pid) return null;
      return listChangeSets(pid, trainKey.split(",")).then((items) => ({ pid, items }));
    }).then((res) => {
      if (cancelled || !res) return;
      setTrainPid(res.pid);
      const byTask = new Map(res.items.filter((cs) => cs.taskId).map((cs) => [cs.taskId!, cs]));
      const cars: TrainCarSpec[] = tasks.map((t) => {
        const cs = byTask.get(t.id);
        const repo = repoNameById[t.repositoryId ?? ""] ?? t.title;
        // 同任务行：装配期显示名 → 真实执行者 → 最后才写「待指派」。
        const by = t.workerLabel || t.assignee || t.leaderLabel || "待指派";
        return cs?.prUrl
          ? {
              repo,
              changeSetId: cs.id,
              pr: `PR !${(cs.prUrl.match(/(\d+)\/?$/) ?? [])[1] ?? ""}`,
              merged: cs.status === "merged",
              by,
            }
          : { repo, merged: cs?.status === "merged", by };
      });
      setTrainCars(cars);
    }).catch(() => {
      if (!cancelled) setTrainCars(null);
    });
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [projectId, trainKey, reload]);

  // ── 查看交付序列（2026-09-20 移植主线 9f206b0a）：右栏那张合并卡只是指针 ——
  //    点一下把左栏列车滚进视野并高亮一档；合并确认在列车上做（那里能看到每节
  //    车厢的门禁与顺序），聊天卡不再假装自己能合并。 ──
  const trainWrapRef = useRef<HTMLDivElement | null>(null);
  const [trainSpot, setTrainSpot] = useState(false);
  const handleViewTrain = () => {
    setTrainSpot(true);
    trainWrapRef.current?.scrollIntoView({ behavior: "smooth", block: "end" });
    window.setTimeout(() => setTrainSpot(false), 2000);
  };
  /** 右栏焦点正停在「交付序列」（顶栏交付段点开的历史）时，列车持续高亮。 */
  const viewingDelivery = activeEntry?.kind === "stage" && activeEntry.stage === 3;

  const train: ReactNode =
    allTasksDone && trainCars !== null ? (
      <div ref={trainWrapRef} className="train-in flex-none px-4 pb-4 pt-1">
        <PrTrainCard
          cars={trainCars}
          projectId={trainPid ?? undefined}
          // 2026-09-20 线上实测：这里此前还挂着「（演示）」那条 toast —— 上面
          // handleConfirmMerge 已经改成真调合并端点，但按钮接的是这一行，于是
          // 点「确认合并」只弹一句话、一个 PR 都不会合。改成真合并。
          onConfirm={() => void handleConfirmMerge()}
          spotlight={trainSpot || viewingDelivery}
        />
      </div>
    ) : null;

  if (isNew) {
    return (
      <div
        className="relative flex h-full min-w-0 flex-1 flex-col items-center justify-center gap-5 px-6 pb-24"
        onDragEnter={(e) => {
          e.preventDefault();
          dragDepth.current += 1;
          setDocDragging(true);
        }}
        onDragOver={(e) => e.preventDefault()}
        onDragLeave={() => {
          dragDepth.current -= 1;
          if (dragDepth.current <= 0) setDocDragging(false);
        }}
        onDrop={(e) => {
          e.preventDefault();
          dragDepth.current = 0;
          setDocDragging(false);
          handlePickDocument(e.dataTransfer.files?.[0]);
        }}
      >
        {docDragging && (
          <div className="absolute inset-4 z-10 grid place-items-center rounded-[12px] border-2 border-dashed border-amber bg-panel/80">
            <p className="text-[13px] text-tx2">松开以解析需求文档</p>
          </div>
        )}
        <div className="flex flex-col items-center gap-2 text-center">
          <h1 className="text-[19px] font-medium text-cream">{greeting}，要规划什么需求？</h1>
          <p className="text-[12px] text-tx2">项目：{projectName} · 选择本次 Issue 的工作仓库后提交</p>
          {/* 需求模板下载（2026-09-20 移植主线 13abb29c）：模板按发现链真正校验的
              四个维度（业务场景/行为描述/变更类型/技术约束）排版，词面命中覆盖标记，
              照它填就能一次过分析、少走追问轮次。 */}
          <button
            type="button"
            className="mt-1 flex items-center gap-1.5 rounded-hard border border-line px-3 py-1 text-[11.5px] text-tx2 transition-colors hover:border-amber hover:text-tx"
            title="下载需求模板（.md）——按四维度填写，减少追问轮次"
            onClick={() => {
              const tmpl = [
                "# 需求标题（一句话概括核心目标）",
                "",
                "## 业务场景",
                "谁在什么场景下遇到什么问题？（用户/场景/业务/客户）",
                "",
                "## 行为描述",
                "系统应该支持什么行为？期望的功能/接口是什么？（应该/需要/支持/实现）",
                "",
                "## 变更类型",
                "这是新增功能、修改现有逻辑、修复 bug、重构还是迁移？（新增/修改/重构/修复/迁移）",
                "",
                "## 技术约束（可选，缺少不算不足）",
                "性能/安全/兼容性/依赖/协议方面有什么要求？",
                "",
                "---",
                "以上四个维度都写清楚，需求分析直接通过，不需要追问。",
                "技术约束为可选维度，缺少不影响分析通过。",
              ].join("\n");
              const blob = new Blob([tmpl], { type: "text/markdown;charset=utf-8" });
              const url = URL.createObjectURL(blob);
              const a = document.createElement("a");
              a.href = url;
              a.download = "需求模板.md";
              a.click();
              URL.revokeObjectURL(url);
            }}
          >
            <FileText size={13} strokeWidth={1.75} />
            下载需求模板
          </button>
        </div>
        <section className="w-full max-w-[720px] rounded-hard border border-line bg-panel p-4 text-sm">
          <h2>本次 Issue 的仓库范围（已选 {selectedRepos.length} 个）</h2>
          {optionsError && <p role="alert" className="text-salmon-hi">{optionsError} <button onClick={() => setOptionsReload(n => n + 1)}>重试</button></p>}
          {!options && !optionsError && <p className="mt-2 text-tx2">{resolveDataSourceMode() === "replay" ? "回放模式不能创建 Issue" : "正在读取创建条件…"}</p>}
          {/* 阻断原因：机器 code 映射成人话，App 相关时补一个「去哪补」的入口。
              判定逻辑一个字没动——`canSubmit` 仍由后端算。 */}
          {options && !options.canSubmit && (
            <div className="mt-2 text-salmon-hi">
              <p>
                暂不能创建：
                {options.blockingReasons?.length
                  ? options.blockingReasons.map((code) => BLOCKING_LABEL[code] ?? code).join("；")
                  : "创建条件未就绪"}
                。请先完成项目仓库接入、工作授权和执行配置。
              </p>
              {options.blockingReasons?.some((code) => APP_RELATED_BLOCKERS.has(code)) && (
                <a
                  className="mt-1 inline-block text-[11.5px] text-amber-hi underline-offset-2 hover:underline"
                  href="#/repositories"
                >
                  去安装 GitHub App / 检查仓库授权 →
                </a>
              )}
            </div>
          )}
          <div className="mt-3 max-h-48 space-y-2 overflow-auto">{options?.repositories.map(r => <label key={r.repositoryId} className="flex items-center gap-2"><input type="checkbox" disabled={!r.selectable || creating || attempt.current !== null} checked={selectedRepos.includes(r.repositoryId)} onChange={e => setSelectedRepos(prev => e.target.checked ? [...prev, r.repositoryId] : prev.filter(id => id !== r.repositoryId))} />{r.displayName}<span className="text-xs text-tx3">{r.reasons.join("、")}</span></label>)}</div>
          <a className="mt-3 inline-block text-xs text-amber-hi" href="#/repositories">管理当前项目仓库</a>
          {attempt.current && <p className="mt-2 text-xs text-tx2">提交内容已固定，重试会查询或完成同一次创建。</p>}
        </section>
        {/* HITL 入口选择（2026-09-17 用户裁定:从建项处选,不再等物化）:
            自动托管 = 处理员代行人审门; 人工参与 = 分档审批/物化确认/PR 合并等真人。 */}
        <div className="flex flex-col items-center gap-1.5">
          <div className="flex rounded-hard border border-line bg-well p-0.5">
            {(
              [
                { key: "ai", label: "自动托管", Icon: IconBolt },
                { key: "hitl", label: "人工参与审计", Icon: IconUser },
              ] as const
            ).map((opt) => (
              <button
                key={opt.key}
                type="button"
                className={`flex items-center gap-1.5 rounded-hard px-3 py-1 text-[11.5px] transition-colors ${
                  hitlMode === opt.key ? "bg-amber font-bold text-on-amber" : "text-tx2 hover:text-tx"
                }`}
                onClick={() => setHitlMode(opt.key)}
              >
                <opt.Icon size={13} />
                {opt.label}
              </button>
            ))}
          </div>
          <p className="max-w-[420px] text-center text-[10.5px] leading-[1.6] text-tx3">
            {hitlMode === "ai"
              ? "处理员自动通过分档审批、生成计划与物化确认,全程不停顿"
              : "人工把守:分档审批 · 生成计划 · 物化确认 · PR 合并确认(策略卡点:范围/规格/执行/验证/交付/异常)"}
          </p>
        </div>
        <div className="w-full max-w-[720px]">
          <AIChatInput
            value={draft}
            onValueChange={handleDraftChange}
            onSend={handleCreateSend}
            sending={creating}
            placeholder="输入需求 —— 发送即创建 issue 并开始规划（Ctrl ⏎ 发送）"
            onAttach={() => fileInputRef.current?.click()}
            attachTitle="上传需求文档 · 支持 .txt / .md / .docx / .pdf / .odt / .rtf"
            attachDisabled={creating || parsingDocument || attempt.current !== null}
            // 2026-09-20（用户："需求不写仓库为什么就不行？"）：**不选仓库也能发**。
            // 没点名仓库时，候选评分那一步会退到本项目全部仓库目录，由 Manager
            // （总领导）自己发现该改哪些仓。
            sendDisabled={parsingDocument || selectedRepos.length > 100 || !options?.canSubmit || (draft.trim() === "" && attachment === null)}
            attachment={
              attachment ? (
                <div className="flex items-center gap-2 border-t border-line px-3 py-1.5">
                  <FileText size={13} className="flex-none text-tx2" />
                  <span className="min-w-0 truncate font-mono text-[11px] text-tx2" title={attachment.filename}>
                    {attachment.filename}
                  </span>
                  <button
                    type="button"
                    className="ml-auto flex-none text-[11px] text-tx3 hover:text-salmon"
                    title="移除附件"
                    disabled={creating || attempt.current !== null}
                    onClick={() => setAttachment(null)}
                  >
                    <X size={12} />
                  </button>
                </div>
              ) : null
            }
          />
        </div>
        <input
          ref={fileInputRef}
          type="file"
          accept={DOC_ACCEPT}
          className="hidden"
          onChange={(e) => handlePickDocument(e.target.files?.[0])}
        />
      </div>
    );
  }

  // ── 既有会话：顶栏四点条 + 左树右详情 ──
  const taskEntry = activeEntry?.kind === "task" ? (taskById.get(activeEntry.taskId) ?? null) : null;

  return (
    <div className="flex h-full min-w-0 flex-1 flex-col">
      {/* 顶栏：返回 + 四点链路条（唯一进度条） */}
      <div className="flex-none border-b border-line bg-ink px-6">
        <div className="flex h-12 items-center gap-3">
          {onBack && (
            <button
              className="flex flex-none items-center gap-0.5 rounded-hard border border-line px-2 py-0.5 text-[10.5px] text-tx2 hover:border-amber hover:text-amber-hi"
              onClick={onBack}
              title="返回会话列表"
            >
              <ChevronLeft size={12} strokeWidth={2} />
              issue 列表
            </button>
          )}
          {/* 2026-09-20 移植主线 9e1dee3d：顶栏那条四点链路条（规划/执行/审核/交付）
              收编进右侧 DAG 胶囊，顶栏只留标题。原来那四个圆点是**可点**的入口，
              所以链路节点在胶囊里仍然是可点的——点它开那一段的阶段历史，
              历史入口不随条一起消失。 */}
          {detail ? (
            <h1 className="min-w-0 truncate text-[13px] font-medium text-tx">{detail.title}</h1>
          ) : (
            <span className="text-[11.5px] text-tx3">…</span>
          )}
          {/* HITL 模式徽标:建项入口选的,自动托管=处理员代行人审门 */}
          {detail && !isNew && (
            <span
              className="ml-1 flex flex-none items-center gap-1 rounded-hard border border-line px-1.5 py-px text-[9.5px] text-tx2"
              title={issueHitl === "ai" ? "自动托管:分档审批、生成计划与物化确认由处理员代行" : "人工参与:分档审批 · 生成计划 · 物化确认 · PR 合并由人确认"}
            >
              {issueHitl === "ai" ? <IconBolt size={10} /> : <IconUser size={10} />}
              {issueHitl === "ai" ? "自动托管" : "人工参与"}
            </span>
          )}
          {/* 计划 DAG 胶囊（原顶栏组件，重构时误删，此番归位）。
              2026-09-20：链路当前节点收编进来（顶栏四点条撤走），execution 接线。 */}
          <div className="ml-auto flex items-center gap-2">
            <PlanDagCapsule
              state={flow.planState}
              execution={dagExecution}
              resetKey={issueId ?? "new"}
              stageLabel={stageLabel}
              onOpenStage={() => setActiveEntry({ kind: "stage", stage: stageIdx as 0 | 1 | 2 | 3 })}
              steps={stepChain}
            />
          </div>
        </div>
      </div>

      {/* 候选分流：人勾选弹层（项目目录仓库） */}
      <Modal
        open={selectionOpen}
        onClose={() => setSelectionOpen(false)}
        className="m-auto w-[min(460px,92vw)] rounded-[3px] border border-line-strong bg-panel p-0 text-tx shadow-pop"
      >
        <div className="border-b border-line px-4 py-2.5">
          <p className="text-[12.5px] font-semibold text-tx">勾选需求涉及的仓库</p>
          <p className="mt-0.5 text-[11px] text-tx2">从项目目录里选；漏了的之后还有依赖图兜底查漏</p>
        </div>
        <div className="flex max-h-[50vh] flex-col gap-1 overflow-y-auto px-4 py-3">
          {Object.entries(repoNameById).map(([id, name]) => (
            <label key={id} className="flex cursor-pointer items-center gap-2.5 rounded-hard border border-line bg-ink px-3 py-2 text-[12px] transition-colors hover:border-amber hover:bg-[var(--tree-zone)]">
              <input
                type="checkbox"
                checked={!!pickedRepos[id]}
                onChange={() => setPickedRepos((prev) => ({ ...prev, [id]: !prev[id] }))}
                className="size-4 accent-amber"
              />
              <span className="font-mono">{name}</span>
            </label>
          ))}
        </div>
        <div className="flex justify-end gap-2 border-t border-line px-4 py-2.5">
          <button type="button" className="rounded-hard border border-line px-3 py-1 text-[11.5px] text-tx2 hover:border-amber" onClick={() => setSelectionOpen(false)}>
            取消
          </button>
          <button
            type="button"
            disabled={selectionBusy}
            onClick={handleSelectionSubmit}
            className="rounded-hard bg-amber px-3.5 py-1 text-[11.5px] font-bold text-on-amber hover:bg-amber-hi disabled:opacity-50"
          >
            {selectionBusy ? "提交中…" : "确认勾选"}
          </button>
        </div>
      </Modal>

      {/* 主体：左树右详情 */}
      {loading && <div className="flex-1 bg-[var(--tree-bg)] px-6 py-4 text-[12px] text-[var(--tree-faint)]">会话加载中…</div>}
      {!loading && error && (
        <div className="flex-1 bg-[var(--tree-bg)] px-6 py-4">
          <p className="rounded-[9px] border border-salmon/40 bg-salmon-well px-3 py-2 text-[12px] text-salmon">{error}</p>
          {/* 跨项目 404 不给「重试」：项目没切之前重试必然还是 404，摆一个点了必失败
              的按钮比不摆更糟。改给一个真能走出去的动作（回列表，再从左上角切项目）。 */}
          {foreignIssue ? (
            <button
              className="mt-2 rounded-[7px] border border-[var(--tree-line)] bg-[var(--tree-card)] px-3 py-1 text-[11.5px] text-[var(--tree-ink)] hover:border-[var(--tree-acc)]"
              onClick={() => onBack?.()}
            >
              ‹ 返回 issue 列表（再从左上角切换项目）
            </button>
          ) : (
            <button
              className="mt-2 rounded-[7px] border border-[var(--tree-line)] bg-[var(--tree-card)] px-3 py-1 text-[11.5px] text-[var(--tree-ink)] hover:border-[var(--tree-acc)]"
              onClick={() => setReload((n) => n + 1)}
            >
              重试
            </button>
          )}
        </div>
      )}
      {!loading && !error && detail && (
        <div ref={rowRef} className="flex min-h-0 flex-1">
          <div className="flex min-w-0 flex-1 flex-col">
            <div className="min-h-0 flex-1">
              <DispatchTree
                title={detail.title}
                discovery={discovery}
                tasks={tasks}
                materialized={materialized}
                activeEntry={activeEntry}
                onOpen={setActiveEntry}
                testEvidence={testEvidence}
                hitl={issueHitl === "hitl"}
              />
            </div>
            {/* PR 交付列车:交付环节到达时从左栏底部弹入,不占聊天房间 */}
            {train}
          </div>
          {focusOpen ? (
            <>
            {/* 可拖拽分栏：拖动改右栏宽度（2026-09-20 用户要求"大小和比例都不能
                调整"）。手柄在左树与右栏之间，命中区 6px。 */}
            <PanelResizeHandle
              width={panelWidth}
              setWidth={setPanelWidth}
              min={PANEL_MIN_WIDTH}
              max={panelMaxWidth}
            />
            <FocusPanel
              entry={activeEntry}
              discovery={discovery}
              requirementText={detail.requirement_text ?? discovery?.requirement_text ?? ""}
              testEvidence={testEvidence}
              tasks={tasks}
              trainCars={trainCars}
              scopeRepoIds={(detail.repositories ?? []).map((r) => r.repository_id)}
              projectId={projectId}
              planId={planId}
              actorId={principal?.agentId ?? ""}
              repoOptions={Object.entries(repoNameById).map(([id, name]) => ({ id, name }))}
              onAppendRepository={handleAppendRepository}
              planState={planState}
              onInterruptPlan={handleInterruptPlan}
            planRevisions={planRevisions}
            deliveryManifest={deliveryManifest}
            onBuildManifest={handleBuildManifest}
              stepStates={stepStates}
              task={taskEntry}
              messages={entryMessages}
              roomMessages={roomMessages}
              onGate={handleGate}
              gateBusy={gateBusy}
              gateError={gateError}
              onRetryStep={handleRetryStep}
              stepError={stepError}
              onForceContinue={handleForceContinue}
              onDecideTask={handleDecideTask}
              policyCard={flow.policyCard}
              onConfigurePolicy={() => setPolicyOpen(true)}
              onRetryPolicy={flow.reloadPolicy}
              mergePending={allTasksDone && trainCars !== null && issueHitl === "hitl"}
              // 2026-09-20 移植主线 9f206b0a：合并卡改做「查看交付序列」指针。
              // onConfirmMerge 这个 prop 撤掉了，但能力没少 —— 列车卡自己的
              // 「确认合并」才是真入口（那里看得见每节车厢的门禁与顺序）。
              onViewTrain={handleViewTrain}
              onCollapse={() => setFocusOpen(false)}
              onBackToManager={() => setActiveEntry({ kind: "mgr" })}
              onChooseManual={handleChooseManual}
              onChooseAI={handleChooseAI}
              onConfirmSupplements={handleConfirmSupplements}
              selectionBusy={selectionBusy}
              input={
                activeEntry === null
                  ? null
                  : clarifyPending && activeEntry.kind === "step" && activeEntry.step <= 2
                    ? {
                        placeholder: "回答处理员的追问 —— 发送后它会带着你的补充继续分析（Enter 发送）",
                        sending,
                        onSend: handleEntrySend,
                      }
                    : entryConvId
                      ? {
                          placeholder:
                            activeEntry.kind === "mgr"
                              ? "发消息到主会话…（Enter 发送）"
                              : "发消息到该任务房间…（Enter 发送）",
                          sending,
                          onSend: handleEntrySend,
                        }
                      : null
              }
              width={panelWidth}
            />
            </>
          ) : (
            /* 收起态：一条 36px 窄条，左树铺满；点展开钮复原右栏
               （2026-09-20 移植主线 0c7a54a1，贴合 LBP 的左树 + 右详情分栏）。 */
            <div className="flex w-[36px] flex-none flex-col items-center border-l border-[var(--tree-hairline)] bg-[var(--tree-zone)] py-3">
              <button
                className="grid size-7 place-items-center rounded-hard text-[var(--tree-faint)] transition-colors hover:bg-[var(--tree-card)] hover:text-[var(--tree-ink)]"
                title="展开详情面板"
                onClick={() => setFocusOpen(true)}
              >
                <PanelLeftOpen size={14} strokeWidth={1.5} />
              </button>
            </div>
          )}
        </div>
      )}

      {/* 监管策略弹窗（迁移 5-1b）：草稿卡片上的「配置 / 修改」打开它。
          两个入参都取**已取到的事实**：生效分档来自发现读投影（弹窗用它把仓库
          下拉限定在计划内的仓库上，配出一条物化时必被拒的授权是界面失职），
          任务数来自计划集成计数（还没生成计划时是 null —— 那时 T 未知，
          代价预告如实说「每个任务各 1 次」，不拿 0 冒充）。 */}
      {detail && (
        <SupervisionPolicyDialog
          open={policyOpen}
          projectId={projectId}
          issueTitle={detail.title}
          effectiveTiers={discovery?.effective_tiers ?? []}
          taskCount={discovery?.integration?.task_dag_count ?? null}
          onClose={() => setPolicyOpen(false)}
          onSaved={() => {
            // 保存/撤回后重取草稿，让卡片显示的是服务端真正存下的那一份。
            flow.reloadPolicy();
            setPolicyOpen(false);
          }}
        />
      )}
    </div>
  );
}
