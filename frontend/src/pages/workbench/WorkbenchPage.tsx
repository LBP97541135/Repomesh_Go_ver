import { useEffect, useRef, useState, type ReactNode } from "react";
import { ChevronLeft, FileText, X } from "lucide-react";
import { PrTrainCard, type TrainCarSpec } from "./PrTrainCard";
import { DispatchTree } from "./DispatchTree";
import { FocusPanel } from "./FocusPanel";
import { deriveStepStates } from "./treeModel";
import type { FocusEntry } from "./treeModel";
import { IconBolt, IconUser } from "./treeIcons";
import type { DiscoveryView, IssueDetailView } from "../../api/contract";
import { parseRequirementDocument, type CreateIssueRequest } from "../../api/issues";
import { fetchIssueDetail } from "../../api/rooms";
import { listConversationMessages, submitMessage, type ConversationMessage } from "../../api/conversations";
import { listPlanTasks, type PlanTaskItem } from "../../api/taskTree";
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
import { listChangeSets } from "../../api/scm";
import { resolveDataSourceMode } from "../../api/source";
import { allProjectRepositories } from "../../api/projects";
import { allCreationOptions, type CreationOptions } from "../../api/projectIssues";
import { autoTrigger } from "./autoTrigger";
import { useIssueFlowState } from "./useIssueFlowState";
import { PlanDagCapsule } from "../../components/PlanDagCapsule";
import { SupervisionPolicyDialog } from "../../components/SupervisionPolicyDialog";
import { AIChatInput } from "../../components/ui/ai-chat-input";
import { errText } from "../../display";

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

/** 发现链步号 → 触发端点的幂等键前缀（与发现链四步触发同一套键位）。 */
const STEP_KEY_BY_STEP = {
  1: "analysis",
  2: "candidates",
  3: "classification",
  4: "plan",
} as const;

/** 需求文本里「用户手打的话」与「附件文档解析全文」的分界（U+2063 不可见分隔符）。
 *  契约里文档解析文本只能随 requirement_text 交给规划，但不进聊天气泡——
 *  用户没打字就一个字都不替他展示。 */
const DOC_SENTINEL = "\u2063";
function composeRequirementText(typed: string, documentText: string): string {
  return typed ? `${typed}\n\n${DOC_SENTINEL}\n${documentText}` : `${DOC_SENTINEL}\n${documentText}`;
}

/** 当前焦点的会话 id：MGR/步骤 → 主会话；任务 → 该任务协作房间。 */
function entryConvIdOf(
  entry: FocusEntry | null,
  detail: IssueDetailView | null,
  taskById: Map<string, PlanTaskItem>,
): string | null {
  if (!detail || entry === null) return null;
  if (entry.kind === "task") return taskById.get(entry.taskId)?.conversationId ?? null;
  return detail.source?.conversationId ?? null;
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
  /** 静默轮询与首载的界线：换 issue 才整页 loading，轮询只换数据不闪屏
   *  （树与右栏每 5s 卸载重挂正是「一闪一闪」的根源，旧工作台同款保护）。 */
  const loadedIssueRef = useRef<string | null>(null);

  useEffect(() => {
    if (isNew) {
      loadedIssueRef.current = null;
      setDetail(null);
      setLoading(false);
      setError(null);
      setActiveEntry(null);
      return;
    }
    const firstVisit = loadedIssueRef.current !== issueId;
    let cancelled = false;
    if (firstVisit) {
      loadedIssueRef.current = issueId;
      setLoading(true);
      setError(null);
    }
    fetchIssueDetail(issueId, projectId)
      .then((d) => {
        if (cancelled) return;
        setDetail(d);
        setLoading(false);
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setError(errText(err));
        setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [projectId, issueId, isNew, reload]);

  useEffect(() => {
    if (isNew) return;
    const timer = window.setInterval(() => setReload((n) => n + 1), POLL_MS);
    return () => window.clearInterval(timer);
  }, [isNew]);

  // ── 发现链读投影（树的数据源）：快拍 + 2.5s 轮询 ──
  const [discovery, setDiscovery] = useState<DiscoveryView | null>(null);
  const discoveryIssueKey = detail?.issue_id ?? null;
  useEffect(() => {
    if (isNew || discoveryIssueKey === null) {
      setDiscovery(null);
      return;
    }
    let cancelled = false;
    // 依赖按 issue 标识收敛：detail 每 5s 轮询换新身份，若按对象进依赖，
    // 计时器会被反复重建、tick 连环补发——树每几秒重取一次就是闪的另一半。
    const tick = () =>
      fetchDiscovery(discoveryIssueKey)
        .then((view) => !cancelled && setDiscovery(view))
        .catch(() => undefined);
    tick();
    const timer = window.setInterval(tick, 2500);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [isNew, discoveryIssueKey, reload]);

  // ── 治理决策主体（人工门与自动推进的「谁在操作」） ──
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
    if (discovery.step_state !== "idle" || discovery.running_task_id !== null) return;
    // ④ 的 idle 有两义：分档未过（等人）或分档已过（该跑计划）——只有后者开火
    if (discovery.step === 4 && discovery.approval?.state !== "approved") return;
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

  const clarifyPending =
    !!discovery &&
    discovery.analysis !== null &&
    !discovery.analysis.sufficient &&
    discovery.analysis.questions.length > 0;

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

  // ── 人工门（分档审批 / 物化确认）──
  const [gateBusy, setGateBusy] = useState<"approveTiers" | "materialize" | null>(null);
  const [gateError, setGateError] = useState<string | null>(null);
  /** 监管策略弹窗（迁移 5-1b）：草稿卡片上的「配置 / 修改」。 */
  const [policyOpen, setPolicyOpen] = useState(false);
  const handleGate = (action: "approveTiers" | "materialize") => {
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
          onToast("分档已批准；处理员继续生成计划");
          setReload((n) => n + 1);
        })
        .catch((err: unknown) => {
          setGateBusy(null);
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
        onToast("物化完成：编制已组装、批次已下发——左侧树已换代");
        setActiveEntry(null);
        setReload((n) => n + 1);
      })
      .catch((err: unknown) => {
        setGateBusy(null);
        setGateError(errText(err));
      });
  };

  // ── HITL 模式(既有会话):读建项入口存的选择;没存过默认「人工参与」——
  //    门等真人是最保守的缺省,不会替任何人做主。 ──
  const [issueHitl, setIssueHitl] = useState<"ai" | "hitl">("hitl");
  const issueKey = detail?.issue_id ?? null;
  useEffect(() => {
    if (issueKey === null) return;
    try {
      const stored = window.sessionStorage.getItem(hitlKey(issueKey));
      setIssueHitl(stored === "ai" ? "ai" : "hitl");
    } catch {
      setIssueHitl("hitl");
    }
  }, [issueKey]);

  // 自动托管:处理员代行人审门(分档审批 → 物化确认),用与真人门同一套写回路;
  // 失败如实落进右栏 gateError,不静默重试。
  useEffect(() => {
    if (resolveDataSourceMode() === "replay") return;
    if (issueHitl !== "ai" || !detail || !discovery || !principal || gateBusy) return;
    if (discovery.classification !== null && discovery.approval?.state !== "approved") {
      handleGate("approveTiers");
      return;
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
   *  随 issue 存 sessionStorage——这是这台浏览器这次战役的选择,不是平台数据。 */
  const hitlKey = (id: string) => `repomesh.hitl-mode.${id}`;
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
    attempt.current ??= { key: crypto.randomUUID(), input: { projectId, requirementText: text, repositoryIds: [...selectedRepos].sort(), expectedCreationContextRevision: options.creationContextRevision } };
    onCreateIssue(attempt.current.input, attempt.current.key)
      .then((created) => {
        try {
          window.sessionStorage.setItem(hitlKey(created.issue_id), hitlMode);
        } catch {
          /* 存不进就只在本次会话内生效 */
        }
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
  const stepStates = deriveStepStates(discovery);
  const doneSteps = stepStates.filter((s) => s === "done").length;
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
        const by = t.workerLabel ?? t.leaderLabel ?? "待指派";
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

  const train: ReactNode =
    allTasksDone && trainCars !== null ? (
      <div className="train-in flex-none px-4 pb-4 pt-1">
        <PrTrainCard
          cars={trainCars}
          projectId={trainPid ?? undefined}
          onConfirm={() => onToast("已确认合并：按仓库依赖顺序执行（演示）")}
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
        </div>
        <section className="w-full max-w-[720px] rounded-hard border border-line bg-panel p-4 text-sm">
          <h2>本次 Issue 的仓库范围（已选 {selectedRepos.length} 个）</h2>
          {optionsError && <p role="alert" className="text-salmon-hi">{optionsError} <button onClick={() => setOptionsReload(n => n + 1)}>重试</button></p>}
          {!options && !optionsError && <p className="mt-2 text-tx2">{resolveDataSourceMode() === "replay" ? "回放模式不能创建 Issue" : "正在读取创建条件…"}</p>}
          {options && !options.canSubmit && <p className="mt-2 text-salmon-hi">暂不能创建：{options.blockingReasons?.join("、")}。请先完成项目仓库接入、工作授权和执行配置。</p>}
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
              ? "处理员自动通过分档审批与物化确认,全程不停顿"
              : "人工把守:分档审批 · 物化确认 · PR 合并确认(策略卡点:范围/规格/执行/验证/交付/异常)"}
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
            sendDisabled={parsingDocument || selectedRepos.length > 100 || !options?.canSubmit || selectedRepos.length === 0 || (draft.trim() === "" && attachment === null)}
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
          <span className="eyebrow">流程</span>
          {detail ? (
            <div className="flex items-center gap-1.5">
              {STAGES.map((title, i) => {
                const st = stageState(i);
                return (
                  <span
                    key={title}
                    className="flex items-center gap-1.5"
                    title={
                      i === 2
                        ? blockedTasks > 0
                          ? `${blockedTasks} 个任务等你批（点任务行审批）`
                          : undefined
                        : i === 3
                          ? "交付读面：PR 列车在场即为到达"
                          : undefined
                    }
                  >
                    {i > 0 && <span className={`h-px w-4 ${stageState(i - 1) !== "todo" ? "bg-line-strong" : "bg-line"}`} />}
                    <span
                      className={`grid size-[18px] place-items-center rounded-full border-[1.5px] text-[10px] font-bold ${
                        st === "now"
                          ? "border-amber bg-amber text-on-amber"
                          : st === "done"
                            ? "border-olive bg-olive text-on-amber"
                            : "border-line-strong text-tx3"
                      }`}
                    >
                      {st === "done" ? "✓" : st === "now" ? "●" : i + 1}
                    </span>
                    <span className={`text-[12px] ${st === "now" ? "font-medium text-tx" : "text-tx3"}`}>
                      {title}
                      {i === 0 && !materialized ? ` ${doneSteps}/5` : ""}
                    </span>
                  </span>
                );
              })}
            </div>
          ) : (
            <span className="text-[11.5px] text-tx3">…</span>
          )}
          {/* HITL 模式徽标:建项入口选的,自动托管=处理员代行人审门 */}
          {detail && !isNew && (
            <span
              className="ml-1 flex flex-none items-center gap-1 rounded-hard border border-line px-1.5 py-px text-[9.5px] text-tx2"
              title={issueHitl === "ai" ? "自动托管:分档审批与物化确认由处理员代行" : "人工参与:分档审批 · 物化确认 · PR 合并由人确认"}
            >
              {issueHitl === "ai" ? <IconBolt size={10} /> : <IconUser size={10} />}
              {issueHitl === "ai" ? "自动托管" : "人工参与"}
            </span>
          )}
          {/* 计划 DAG 胶囊（原顶栏组件，重构时误删，此番归位） */}
          <div className="ml-auto flex items-center gap-2">
            <PlanDagCapsule
              state={flow.planState}
              execution={null}
              onRetry={flow.reloadPlan}
              resetKey={issueId ?? "new"}
            />
          </div>
        </div>
      </div>

      {/* 主体：左树右详情 */}
      {loading && <div className="flex-1 bg-[var(--tree-bg)] px-6 py-4 text-[12px] text-[var(--tree-faint)]">会话加载中…</div>}
      {!loading && error && (
        <div className="flex-1 bg-[var(--tree-bg)] px-6 py-4">
          <p className="rounded-[9px] border border-salmon/40 bg-salmon-well px-3 py-2 text-[12px] text-salmon">{error}</p>
          <button
            className="mt-2 rounded-[7px] border border-[var(--tree-line)] bg-[var(--tree-card)] px-3 py-1 text-[11.5px] text-[var(--tree-ink)] hover:border-[var(--tree-acc)]"
            onClick={() => setReload((n) => n + 1)}
          >
            重试
          </button>
        </div>
      )}
      {!loading && !error && detail && (
        <div className="flex min-h-0 flex-1">
          <div className="flex min-w-0 flex-1 flex-col">
            <div className="min-h-0 flex-1">
              <DispatchTree
                title={detail.title}
                discovery={discovery}
                tasks={tasks}
                materialized={materialized}
                activeEntry={activeEntry}
                onOpen={setActiveEntry}
              />
            </div>
            {/* PR 交付列车:交付环节到达时从左栏底部弹入,不占聊天房间 */}
            {train}
          </div>
          <FocusPanel
            entry={activeEntry}
            discovery={discovery}
            stepStates={stepStates}
            task={taskEntry}
            messages={entryMessages}
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
            onConfirmMerge={() => onToast("已确认合并：按仓库依赖顺序执行（演示）")}
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
          />
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
