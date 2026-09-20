/** 右栏·焦点详情（方案 A）：点哪显示哪。
 *
 *  - 规划步骤：详情卡（关键词/候选/三档/批次）+ 人工门按钮（批准分档、确认物化）
 *  - Manager：主会话完整时间线（需求→规划→下发，即回看入口）
 *  - 任务：任务元信息 + 该任务协作房间的消息时序流
 *  - 输入框：由页面配置（发消息进当前会话 / 回答追问），真端点在页面接
 *  - PR 列车：交付期由页面塞到顶部插槽
 *
 *  消息渲染：只有真实房间消息才渲染成聊天气泡（契约 Q4 硬约束），角色由
 *  actor_id 前缀推导（agent_<role>[_<name>]），推导不出按系统条目样式。 */

import { X as IconX } from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { IconCheck, IconClock, IconHouse, IconRun, IconSend } from "./treeIcons";
import { WorkerHealthGate } from "./WorkerHealthGate";
import type { DiscoveryProducer, DiscoveryView } from "../../api/contract";
import type { PlanTaskItem } from "../../api/taskTree";
import type { ConversationMessage } from "../../api/conversations";
import { STEP_LABELS, type FocusEntry, type StepState } from "./treeModel";
import type { TestEvidenceItem, TestEvidenceView } from "../../api/testEvidence";
import type { TrainCarSpec } from "./PrTrainCard";
import type { InterruptOutcomeView, PlanRevisionView } from "../../api/plans";
import type { DeliveryManifestView } from "../../api/deliveryManifest";
import { documentTitleOf, splitRequirement } from "../../api/issues";
import { SupervisionPolicyCard, type PolicyDraftState } from "../../components/SupervisionPolicyCard";
import { TaskAgentOutput } from "./TaskAgentOutput";

/** 消息作者 → 角色显示。先看 authorKind（user 是人），服务侧 agent 再按
 *  roster 命名约定（agent_<role>[_<name>]）推导；都推不出按系统条目样式。 */
function actorOf(authorKind: string, actorId: string): { label: string; role: string; cls: string; letter: string } {
  if (authorKind === "user") return { label: "你", role: "用户", cls: "u", letter: "你" };
  const parts = actorId.split("_");
  if (parts[0] === "agent" && parts.length >= 2) {
    const role = parts[1].charAt(0).toUpperCase() + parts[1].slice(1);
    const name = parts.slice(2).join("_") || role;
    const cls = parts[1] === "manager" ? "m" : parts[1].startsWith("leader") ? "l" : "w";
    return { label: name, role, cls, letter: cls === "m" ? "M" : cls === "l" ? "L" : "W" };
  }
  return { label: actorId, role: "系统", cls: "s", letter: "·" };
}

/** 产出者一行：谁产的、用哪把技能、哪个 run。
 *
 *  规划期的结论由角色 agent 产出，界面必须说清是谁 —— 审计要的就是这个：
 *  结论不再是一个匿名 JSON。老快照没有 producer（后端自己算的那批），如实不显示。 */
function ProducerLine({ producer }: { producer?: DiscoveryProducer }) {
  if (!producer) return null;
  return (
    <p className="mt-1.5 text-[10.5px] leading-[1.7] text-[var(--tree-faint)]">
      产出：{producer.role} · 技能 {producer.skill_id} · run {producer.run_id.slice(0, 14)}
    </p>
  );
}

const AVA_CLS: Record<string, string> = {
  m: "bg-[var(--tree-acc)]",
  l: "bg-[var(--tree-role-l)]",
  w: "bg-olive",
  u: "bg-[#b8860b]",
  s: "bg-[#a3a3a0]",
};

const RPILL_CLS: Record<string, string> = {
  m: "bg-[var(--tree-acc)]/12 text-[var(--tree-acc)]",
  l: "bg-[var(--tree-role-l)]/12 text-[var(--tree-role-l)]",
  w: "bg-olive/12 text-olive",
  u: "bg-amber-well text-amber",
  s: "bg-[var(--tree-zone)] text-[var(--tree-sub)]",
};

function hhmm(at: string): string {
  // 2026-09-20 线上实测：这里原先直接对原始字符串做 slice(11,16)，而后端按
  // **UTC** 序列化 created_at（例如 03:01 CST 存成 19:01Z）—— 界面上每条消息的
  // 时间都比真实时间早 8 小时。解析成 Date 再按浏览器本地时区格式化；
  // 解析不出来（形状意外）才退回原来的切片，至少不会显示成空白。
  const parsed = new Date(at);
  if (!Number.isNaN(parsed.getTime())) {
    const two = (n: number) => String(n).padStart(2, "0");
    return `${two(parsed.getHours())}:${two(parsed.getMinutes())}`;
  }
  return at.slice(11, 16);
}

/** 消息时间线（MGR / 任务共用）。轮询刷新只在用户本就贴底时跟到底——
 *  上翻读历史时不抢滚动。 */
function MessageTimeline({ messages }: { messages: ConversationMessage[] }) {
  const ref = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 120;
    if (nearBottom) el.scrollTop = el.scrollHeight;
  }, [messages]);
  return (
    <div ref={ref} className="flex flex-col gap-3 overflow-y-auto px-4 py-3.5">
      {messages.length === 0 && (
        <p className="pl-[31px] text-[10.5px] text-[var(--tree-faint)]">这个房间还没有消息。</p>
      )}
      {messages.map((m) => {
        const actor = actorOf(m.authorKind, m.actorId);
        return (
          <div key={m.id} className="flex gap-2.5">
            <span className={`grid h-[22px] w-[22px] flex-none place-items-center rounded-full text-[8.5px] font-bold text-white ${AVA_CLS[actor.cls]}`}>
              {actor.letter}
            </span>
            <div className="min-w-0 flex-1">
              <div className="mb-0.5 flex items-center gap-1.5">
                <span className="text-[11px] font-medium text-[var(--tree-ink)]">{actor.label}</span>
                <span className={`rounded-[5px] px-1.5 py-px text-[9.5px] ${RPILL_CLS[actor.cls]}`}>{actor.role}</span>
                <span className="ml-auto font-mono text-[10px] text-[var(--tree-faint)]">{hhmm(m.createdAt)}</span>
              </div>
              <div className="whitespace-pre-wrap break-words rounded-lg border border-[var(--tree-hairline)] bg-[var(--tree-card)] px-2.5 py-1.5 text-[11.5px] leading-[1.65] text-[var(--tree-ink)] shadow-[0_1px_2px_rgba(15,15,15,.03)]">
                {m.body}
              </div>
            </div>
          </div>
        );
      })}
    </div>
  );
}

const KIND_LABEL: Record<string, string> = {
  task_single_point: "任务单点验收",
  repo_integration: "仓库集成验证",
  cross_repo_regression: "跨仓库联调 + 回归",
};

/** 测试排期：**要跑什么、按什么顺序**，以及跑到哪了。
 *
 *  2026-09-20 用户反馈："测试组全程不展示任何规划和测试排期"。查下来是读面只有
 *  **已发生的记录**（`public.test_evidence` 的三种 kind），没有"计划要跑什么"这一层，
 *  界面上自然只有事后清单。
 *
 *  这里的排期**不是另编一份**，是从**计划本身**推出来的（计划就是排期）：
 *   · 每条任务 → 一轮单点验收（任务跑完开发、过了经理门，就派测试 agent）；
 *   · 每个 DAG 节点（仓库）→ 一轮本仓库集成验证（该仓全部任务过了之后）；
 *   · 计划跨多个仓库时 → 再加一轮跨仓联调 + 回归。
 *  每一行状态**只用真实记录**判定：记录里有 → 已跑（通过 / 未过）；没有 → 待跑，
 *  并写清它在等什么。计划还没物化（tasks 为 null）时推不出排期，如实说推不出。 */
function TestSchedule({ tasks, view }: { tasks: PlanTaskItem[] | null; view: TestEvidenceView | null }) {
  if (tasks === null || tasks.length === 0) {
    return (
      <div className="border-b border-dashed border-[var(--tree-hairline)] px-4 py-3">
        <p className="text-[12px] font-medium text-[var(--tree-ink)]">测试排期</p>
        <p className="mt-1 text-[11px] leading-[1.8] text-[var(--tree-sub)]">
          计划还没物化，推不出排期 —— 排期是从任务 DAG 推出来的，不是另编一份。
        </p>
      </div>
    );
  }
  const items = view?.items ?? [];
  const firstOf = (pred: (i: TestEvidenceItem) => boolean) => items.find(pred) ?? null;
  const repos: string[] = [];
  for (const t of tasks) {
    if (t.repositoryId && !repos.includes(t.repositoryId)) repos.push(t.repositoryId);
  }

  type Row = { key: string; stage: string; what: string; state: "done" | "failed" | "todo"; note: string };
  const rows: Row[] = [];
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
    // "待跑"的**理由**要按事实说：如果该仓的任务都已经完成（经理门也过了），
    // 那它就不是"在等上游"，而是"上游到了、这一步还没有记录" —— 这两件事
    // 对排障完全不同（前者等着就行，后者得去看这条链路跑没跑）。
    const repoTasks = tasks.filter((t) => t.repositoryId === repo);
    const allDone = repoTasks.length > 0 && repoTasks.every((t) => t.status === "done");
    rows.push({
      key: `ri-${repo}`,
      stage: "节点级",
      what: `仓库集成验证 · ${repo}`,
      state: rec ? (rec.passed ? "done" : "failed") : "todo",
      note: rec
        ? rec.summary || "已产出记录"
        : allDone
          ? `该仓 ${repoTasks.length} 条任务都已过经理门，但**还没有**集成验证记录（这一步可能没跑）`
          : `等该仓剩余任务过经理门（${repoTasks.filter((t) => t.status !== "done").length}/${repoTasks.length} 条未完成）`,
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
  const done = rows.filter((r) => r.state === "done").length;
  const failed = rows.filter((r) => r.state === "failed").length;
  const tone: Record<Row["state"], string> = {
    done: "border-olive/40 bg-olive-well text-olive",
    failed: "border-salmon/40 bg-salmon-well text-salmon",
    todo: "border-line bg-[var(--tree-zone)] text-[var(--tree-sub)]",
  };
  const label: Record<Row["state"], string> = { done: "已通过", failed: "未过", todo: "待跑" };
  return (
    <div className="border-b border-dashed border-[var(--tree-hairline)] px-4 py-3">
      <div className="flex items-baseline gap-2">
        <span className="text-[12px] font-medium text-[var(--tree-ink)]">测试排期</span>
        <span className="text-[11px] text-[var(--tree-sub)]">
          计划 {rows.length} 轮 · 已通过 {done}
          {failed > 0 ? ` · 未过 ${failed}` : ""}
          {rows.length - done - failed > 0 ? ` · 待跑 ${rows.length - done - failed}` : ""}
        </span>
      </div>
      <p className="mt-1 text-[10.5px] leading-[1.7] text-[var(--tree-faint)]">
        排期从任务 DAG 推出（每条任务一轮单点验收 · 每个仓库一轮集成 · 跨仓时加一轮联调回归）；
        状态只按**真实记录**判定，没有记录就是"待跑"，不提前标通过。
      </p>
      <div className="mt-1.5 flex flex-col gap-1">
        {rows.map((r) => (
          <div key={r.key} className="flex items-start gap-2 text-[11px]">
            <span className="mt-px w-12 flex-none text-[10px] text-[var(--tree-faint)]">{r.stage}</span>
            <span className={`mt-px flex-none rounded-[5px] border px-1.5 py-px text-[10px] ${tone[r.state]}`}>
              {label[r.state]}
            </span>
            <span className="min-w-0 flex-1">
              <span className="text-[var(--tree-ink)]">{r.what}</span>
              <span className="block truncate text-[10.5px] text-[var(--tree-faint)]" title={r.note}>{r.note}</span>
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}

/** 测试组的记录详情：三种 kind 各一段，逐条列出脚本、命令、退出码与结论。
 *
 *  2026-09-20：这些事实此前只以一个退出码的形式躺在 scm_commands 里，界面上
 *  「测试组」永远是写死的一行文案。没有记录就说没有 —— 不摆一个假的通过。 */
function TestEvidenceDetail({ view }: { view: TestEvidenceView | null }) {
  if (view === null) {
    return (
      <div className="flex flex-1 items-center justify-center text-[11px] text-[var(--tree-faint)]">
        测试记录加载中…
      </div>
    );
  }
  if (view.items.length === 0) {
    return (
      <div className="flex flex-1 flex-col gap-2 px-4 py-3">
        <p className="text-[12px] font-medium text-[var(--tree-ink)]">测试组 · 还没有记录</p>
        <p className="text-[11px] leading-[1.8] text-[var(--tree-sub)]">
          每条任务跑完开发后会自动派一个测试 agent 做单点验收（它写测试脚本、跑、把结论写成证据文件）；
          计划的全部任务过了经理门之后，还会按仓库派节点级集成，跨仓库时再加一轮联调与回归。
          记录产生后会出现在这里。
        </p>
      </div>
    );
  }
  const order = ["task_single_point", "repo_integration", "cross_repo_regression"];
  const groups = order
    .map((kind) => ({ kind, items: view.items.filter((i) => i.kind === kind) }))
    .filter((g) => g.items.length > 0);
  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto px-4 py-3">
      <div className="flex items-baseline gap-2">
        <span className="text-[12px] font-medium text-[var(--tree-ink)]">测试组 · db-test</span>
        <span className="text-[11px] text-[var(--tree-sub)]">
          {view.items.length} 条记录 · {view.passed} 通过
          {view.failed > 0 ? ` · ${view.failed} 未过` : ""}
        </span>
      </div>
      {groups.map((group) => (
        <div key={group.kind} className="flex flex-col gap-1.5">
          <p className="text-[11px] font-medium text-[var(--tree-sub)]">
            {KIND_LABEL[group.kind] ?? group.kind}（{group.items.length}）
          </p>
          {group.items.map((item, index) => (
            <TestEvidenceRow key={`${group.kind}-${index}`} item={item} />
          ))}
        </div>
      ))}
    </div>
  );
}

/** 测试团队的消息流（2026-09-20 接上）。
 *
 *  测试组**没有自己的会话**——它的"说过什么"就是一条条落库的验证证据：
 *  单点验收 / 仓库集成 / 跨仓联调回归。所以这里不装假消息：**每条记录就是一条
 *  来自测试组的消息**，谁产出的、什么时候、跑了什么命令、退出码与结论，全部逐字
 *  来自读面（`GET /issues/{id}/tests`）。没有记录时整段不渲染——没发生的事不摆进度。
 *
 *  两处用它：测试组自己的房间（RM-TEST，主体就是这条流）与 Manager 主房间
 *  （跟在 StepStream 后面，让"测试组干了什么"进入房间流，不必自己切过去找）。 */
function TestStream({ view }: { view: TestEvidenceView | null }) {
  if (!view || view.items.length === 0) return null;
  // 按时间排：房间流是"发生的顺序"，不是按类别归拢的顺序。
  const items = [...view.items].sort((a, b) => a.created_at.localeCompare(b.created_at));
  return (
    <div className="flex flex-col gap-3 px-4 pb-1">
      {items.map((item, index) => (
        <div key={`${item.kind}-${index}`} className="flex gap-2.5">
          <span
            className="mt-0.5 grid h-[22px] w-[22px] flex-none place-items-center rounded-full bg-amber text-[8.5px] font-bold text-[#16130d]"
            title="测试组 · db-test"
          >
            T
          </span>
          <div className="min-w-0 flex-1">
            <div className="mb-0.5 flex items-center gap-1.5">
              <span className="text-[11px] font-medium text-[var(--tree-ink)]">测试组 · db-test</span>
              <span className="rounded-[5px] bg-amber-well px-1.5 py-px text-[9.5px] text-amber">
                {KIND_LABEL[item.kind] ?? item.kind}
              </span>
              <span className="text-[10px] text-[var(--tree-faint)]">{hhmm(item.created_at)}</span>
            </div>
            <TestEvidenceRow item={item} />
          </div>
        </div>
      ))}
    </div>
  );
}

function TestEvidenceRow({ item }: { item: TestEvidenceItem }) {
  const tone = item.passed
    ? "border-olive/40 bg-olive-well text-olive"
    : "border-salmon/40 bg-salmon-well text-salmon";
  return (
    <div className="rounded-[8px] border border-[var(--tree-hairline)] bg-[var(--tree-card)] px-2.5 py-2 transition-colors hover:bg-[var(--tree-zone)]">
      <div className="flex items-center gap-2">
        <span className={`flex-none rounded-[5px] border px-1.5 py-px text-[10px] ${tone}`}>
          {item.passed ? "通过" : "未过"}
        </span>
        <span className="min-w-0 flex-1 truncate text-[11.5px] text-[var(--tree-ink)]">
          {item.repository_id || item.task_id || "—"}
        </span>
        {item.exit_code !== undefined && (
          <span className="flex-none font-mono text-[10.5px] text-[var(--tree-faint)]">
            exit {item.exit_code}
          </span>
        )}
      </div>
      {item.command && (
        <p className="mt-1 break-all font-mono text-[10.5px] leading-[1.7] text-[var(--tree-sub)]">
          $ {item.command}
        </p>
      )}
      {item.script && (
        <p className="mt-0.5 break-all text-[10.5px] text-[var(--tree-faint)]">脚本：{item.script}</p>
      )}
      {item.summary && (
        <p className="mt-1 text-[11px] leading-[1.75] text-[var(--tree-sub)]">{item.summary}</p>
      )}
    </div>
  );
}


/** 阶段历史：顶栏「流程」四个阶段点开后的**只读回看**。
 *
 *  规划 = 五步 + 每步的产出者（谁产的、哪把技能、哪个 run）
 *  执行 = 任务行（真实执行者、批次、状态）+ 单点验收记录
 *  审核 = 经理门的决策与原因
 *  交付 = PR 列车每节车厢的 PR 与合并状态
 *
 *  只摆**已经真实发生**的事：没有记录就说没有。 */
/** 本次 Issue 的仓库范围 —— ③「执行中人工打断 / 动态引入新仓库」的**人工确认入口**。
 *
 *  2026-09-20：范围此前只在建 issue 时选定，界面上没有任何追加入口。后端现在有了
 *  追加端点（POST .../issues/{id}/scope/repositories），这里补上人确认的那一步 ——
 *  用户已裁定：**新仓库必须人工确认才生效**，不允许 agent 自己改范围。
 *
 *  候选只列**本项目已挂的**仓库：服务端也只收这种，未挂的会以 409
 *  REPOSITORY_NOT_IN_PROJECT 明确拒绝（挂仓库是另一个动作，不在这里替人做）。 */
function IssueScopeCard({
  scopeRepoIds,
  repoOptions,
  onAppend,
}: {
  scopeRepoIds: string[];
  repoOptions: Array<{ id: string; name: string }>;
  onAppend: (repositoryId: string) => Promise<void>;
}) {
  const [pick, setPick] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const nameOf = (id: string) => options.find((r) => r.id === id)?.name ?? id;
  // 防御性默认值：这个组件是"选中规划阶段才渲染"的，一旦 props 没到位就会把整个
  // 工作台打成白屏（2026-09-20 实测：TypeError: Cannot read properties of undefined
  // (reading 'filter')，点「规划」必崩）。宁可这块卡片少显示，也不能让整页崩。
  const scope = scopeRepoIds ?? [];
  const options = repoOptions ?? [];
  const candidates = options.filter((r) => !scope.includes(r.id));
  const submit = () => {
    if (pick === "" || busy) return;
    setBusy(true);
    setErr(null);
    onAppend(pick)
      .then(() => setPick(""))
      .catch((e: unknown) => setErr(e instanceof Error ? e.message : String(e)))
      .finally(() => setBusy(false));
  };
  return (
    <div className="rounded-[8px] border border-[var(--tree-hairline)] bg-[var(--tree-card)] px-2.5 py-2 transition-colors hover:bg-[var(--tree-zone)]">
      <p className="text-[11.5px] text-[var(--tree-ink)]">本次 Issue 的仓库范围（{scope.length}）</p>
      {scope.length === 0 && (
        <p className="mt-1 text-[10.5px] text-[var(--tree-faint)]">还没有仓库 —— 服务端按此范围校验一切改动。</p>
      )}
      {scope.map((id) => (
        <p key={id} className="mt-1 break-all font-mono text-[10.5px] text-[var(--tree-sub)]">{nameOf(id)}</p>
      ))}
      {candidates.length > 0 && (
        <div className="mt-2 flex items-center gap-2">
          <select
            className="min-w-0 flex-1 rounded-[6px] border border-[var(--tree-hairline)] bg-[var(--tree-bg)] px-1.5 py-1 text-[11px] text-[var(--tree-ink)]"
            value={pick}
            onChange={(e) => setPick(e.target.value)}
          >
            <option value="">追加一个仓库…</option>
            {candidates.map((r) => (
              <option key={r.id} value={r.id}>{r.name}</option>
            ))}
          </select>
          <button
            className="flex-none rounded-[6px] border border-[var(--tree-acc)] px-2 py-1 text-[11px] text-[var(--tree-acc)] disabled:opacity-50"
            disabled={busy || pick === ""}
            onClick={submit}
          >
            {busy ? "追加中…" : "追加"}
          </button>
        </div>
      )}
      {err !== null && <p className="mt-1 text-[10.5px] text-salmon">{err}</p>}
    </div>
  );
}
/** 计划换代 + ③ 执行中人工打断（动态引入新仓库）。
 *
 *  这条能力此前在界面上**完全没有入口**：/plans/{id}/interrupt 是个空壳，前端那句
 *  interruptPlan() 也是死代码（入参还写错成 { reason }）。用户裁定：新仓库必须人工
 *  确认才生效 —— 所以这里是人点名一个仓库 X，后端落打断决策单、触发 onboarding、
 *  判定 X 是否影响当前计划；判定"影响"时收集窗开启，重排 v2 的意图同时登记。
 *
 *  结果如实显示：ready=false（扫描还没就绪，判定没做）、affectsPlan=false（与当前
 *  计划无耦合，暂定备用）、affectsPlan=true + replanQueued（已登记重排，等 Leader 产 v2）。 */
/** 计划换代历史（A3）：v1→v2 的每一次全量快照替换。
 *
 *  这条历史一直落在 public.plans.revisions 里（含触发它的那一跳、增删的仓库、
 *  创建/取代的任务数），但此前**只有落库没有读面** —— 计划换过几版、每版为什么换，
 *  界面上看不到。空列表就说"还没有换代"，不摆假进度。 */
function PlanRevisionsCard({ revisions }: { revisions: PlanRevisionView[] | null }) {
  return (
    <div className="rounded-[8px] border border-[var(--tree-hairline)] bg-[var(--tree-card)] px-2.5 py-2">
      <p className="text-[11.5px] text-[var(--tree-ink)]">计划换代历史{revisions === null ? "" : `（${revisions.length}）`}</p>
      {revisions === null && (
        <p className="mt-1 text-[10.5px] text-[var(--tree-faint)]">还没有读到换代记录。</p>
      )}
      {revisions !== null && revisions.length === 0 && (
        <p className="mt-1 text-[10.5px] text-[var(--tree-faint)]">还没有换代 —— 计划仍是首版。</p>
      )}
      {(revisions ?? []).map((r) => (
        <div key={r.revision} className="mt-1.5 border-t border-[var(--tree-hairline)] pt-1.5 text-[10.5px] leading-[1.7] text-[var(--tree-sub)]">
          <p>{r.baseVersion} → {r.resultVersion} · 第 {r.revision} 次换代 · 触发者 {r.actor}</p>
          {r.reason !== "" && <p className="break-all">原因：{r.reason}</p>}
          {(r.addedRepositories ?? []).length > 0 && (
            <p className="break-all">新增仓库：{(r.addedRepositories ?? []).join("、")}</p>
          )}
          {(r.removedRepositories ?? []).length > 0 && (
            <p className="break-all">移出仓库：{(r.removedRepositories ?? []).join("、")}</p>
          )}
          <p>任务轴：新建 {r.createdTasks} · 取代 {r.supersededTasks}</p>
          {r.upstreamRef ? <p className="break-all">触发跳：{r.upstreamRef}</p> : null}
        </div>
      ))}
    </div>
  );
}

function PlanReplanCard({
  planState,
  onInterrupt,
}: {
  planState: { planVersion: string; replanState: string } | null;
  onInterrupt: (repository: string, note: string) => Promise<InterruptOutcomeView>;
}) {
  const [repo, setRepo] = useState("");
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [outcome, setOutcome] = useState<InterruptOutcomeView | null>(null);
  const submit = () => {
    if (busy || repo.trim() === "") return;
    setBusy(true);
    setErr(null);
    onInterrupt(repo.trim(), note.trim())
      .then((out) => { setOutcome(out); setNote(""); })
      .catch((e: unknown) => setErr(e instanceof Error ? e.message : String(e)))
      .finally(() => setBusy(false));
  };
  const windowOpen = planState?.replanState === "deprecated";
  return (
    <div className="rounded-[8px] border border-[var(--tree-hairline)] bg-[var(--tree-card)] px-2.5 py-2">
      <p className="text-[11.5px] text-[var(--tree-ink)]">
        计划换代{planState ? `（${planState.planVersion}${windowOpen ? " · 收集窗已开" : ""}）` : ""}
      </p>
      {!planState && (
        <p className="mt-1 text-[10.5px] text-[var(--tree-faint)]">还没有计划 —— 物化之后才有可打断的计划。</p>
      )}
      {planState && (
        <>
          <p className="mt-1 text-[10.5px] leading-[1.7] text-[var(--tree-faint)]">
            执行中要引入一个计划外的仓库？点名它，后端会判定它是否与当前计划耦合。
          </p>
          <input
            className="mt-1.5 w-full rounded-[6px] border border-[var(--tree-hairline)] bg-[var(--tree-bg)] px-1.5 py-1 font-mono text-[11px] text-[var(--tree-ink)]"
            placeholder="owner/name"
            value={repo}
            onChange={(e) => setRepo(e.target.value)}
          />
          <input
            className="mt-1 w-full rounded-[6px] border border-[var(--tree-hairline)] bg-[var(--tree-bg)] px-1.5 py-1 text-[11px] text-[var(--tree-ink)]"
            placeholder="为什么引入（写进决策链）"
            value={note}
            onChange={(e) => setNote(e.target.value)}
          />
          <button
            className="mt-1.5 rounded-[6px] border border-[var(--tree-acc)] px-2 py-1 text-[11px] text-[var(--tree-acc)] disabled:opacity-50"
            disabled={busy || repo.trim() === ""}
            onClick={submit}
          >
            {busy ? "判定中…" : "打断并判定"}
          </button>
        </>
      )}
      {outcome && (
        <div className="mt-1.5 border-t border-[var(--tree-hairline)] pt-1.5 text-[10.5px] leading-[1.7] text-[var(--tree-sub)]">
          <p>决策单 {outcome.nodeId}</p>
          <p>{outcome.onboarded ? "本次新注册并触发了扫描" : "仓库此前已在册"}</p>
          {!outcome.ready && <p>扫描尚未就绪：本次**没有判定**，就绪后可再来一次。</p>}
          {outcome.ready && !outcome.affectsPlan && <p>判定：与当前计划无耦合 —— 暂定，未重排。</p>}
          {outcome.ready && outcome.affectsPlan && (
            <>
              <p>判定：影响当前计划，收集窗已开。</p>
              <p className="break-all">受影响集合：{(outcome.affectedSet ?? []).join("、") || "—"}</p>
              <p>{outcome.replanQueued ? "重排 v2 已登记（等 Leader 产出）" : "重排未登记：后端没有接上重排端口"}</p>
            </>
          )}
        </div>
      )}
      {err !== null && <p className="mt-1 text-[10.5px] text-salmon">{err}</p>}
    </div>
  );
}

/** 跨仓交付的**一致版本清单**（评委建议②）。
 *
 *  从一次交付展开完整版本清单：需求、每个仓库的提交/分支/PR、数据库迁移版本与数据
 *  基线、分支与验证结论、测试证据，以及**失败发生在哪个仓库、哪个阶段** —— 而不是
 *  只看到"任务全部变绿"。没有清单就如实说没有（不摆假的），并给一个"生成清单"入口。 */
function DeliveryManifestCard({
  manifest,
  onBuild,
}: {
  manifest: DeliveryManifestView | null;
  onBuild: () => Promise<void>;
}) {
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const build = () => {
    if (busy) return;
    setBusy(true);
    setErr(null);
    onBuild()
      .catch((e: unknown) => setErr(e instanceof Error ? e.message : String(e)))
      .finally(() => setBusy(false));
  };
  return (
    <div className="rounded-[8px] border border-[var(--tree-hairline)] bg-[var(--tree-card)] px-2.5 py-2">
      <div className="flex items-center gap-2">
        <p className="min-w-0 flex-1 text-[11.5px] text-[var(--tree-ink)]">
          交付版本清单{manifest ? `（${manifest.planVersion} · ${manifest.status === "consistent" ? "一致" : "不一致"}）` : ""}
        </p>
        <button
          className="flex-none rounded-[6px] border border-[var(--tree-acc)] px-2 py-0.5 text-[10.5px] text-[var(--tree-acc)] disabled:opacity-50"
          disabled={busy}
          onClick={build}
        >
          {busy ? "生成中…" : "生成清单"}
        </button>
      </div>
      {!manifest && (
        <p className="mt-1 text-[10.5px] text-[var(--tree-faint)]">还没有清单 —— 点「生成清单」按当前事实落一份快照。</p>
      )}
      {manifest && manifest.failureSummary ? (
        <p className="mt-1 break-all text-[10.5px] text-salmon">{manifest.failureSummary}</p>
      ) : null}
      {(manifest?.entries ?? []).map((entry) => (
        <div key={entry.repositoryName} className="mt-1.5 border-t border-[var(--tree-hairline)] pt-1.5 text-[10.5px] leading-[1.7] text-[var(--tree-sub)]">
          <div className="flex items-center gap-2">
            <span className="min-w-0 flex-1 truncate">{entry.repositoryName}</span>
            <span className={entry.failureStage ? "text-salmon" : "text-olive"}>{entry.failureStage ? "未达成" : "已达成"}</span>
          </div>
          <p className="break-all">代码：{entry.commitSha ? entry.commitSha.slice(0, 10) : "无提交"}{entry.branchRef ? ` · ${entry.branchRef}` : ""}</p>
          {entry.pullRequestUrl ? <p className="break-all">PR：{entry.pullRequestUrl}</p> : <p>PR：未开</p>}
          <p>数据库：{entry.validationStatus}{entry.databaseProvider ? ` · ${entry.databaseProvider}` : ""}{entry.databaseBaseline ? ` · 基线 ${entry.databaseBaseline}` : ""}</p>
          {entry.migrations.length > 0 && <p className="break-all">迁移 {entry.migrations.length} 条：{entry.migrations[0]}</p>}
          {entry.testEvidence.length > 0 && (
            <p className="break-all">测试：{entry.testEvidence.map((item) => `${item.kind}${item.passed ? "通过" : "未过"}`).join(" / ")}</p>
          )}
          {entry.failureStage && <p className="break-all text-salmon">失败阶段 {entry.failureStage}：{entry.failureDetail ?? ""}</p>}
        </div>
      ))}
      {err !== null && <p className="mt-1 text-[10.5px] text-salmon">{err}</p>}
    </div>
  );
}

function StageHistory({
  stage,
  discovery,
  stepStates,
  tasks,
  testEvidence,
  trainCars,
  scopeRepoIds,
  projectId = null,
  repoOptions,
  onAppendRepository,
  planState,
  onInterruptPlan,
  planRevisions,
  deliveryManifest,
  onBuildManifest,
}: {
  stage: 0 | 1 | 2 | 3;
  discovery: DiscoveryView | null;
  stepStates: StepState[];
  tasks: PlanTaskItem[] | null;
  testEvidence: TestEvidenceView | null;
  trainCars: TrainCarSpec[] | null;
  /** 本次 Issue 当前的仓库范围（issue 详情的 repositoryIds） */
  scopeRepoIds: string[];
  /** 当前项目 id —— 「worker 工作内容」读面按 (projectId, taskId) 取，
   *  而那条读面在服务端按「项目 owner 或 admin」授权。 */
  projectId?: string | null;
  /** 本项目已挂的仓库（追加的候选只从这里来） */
  repoOptions: Array<{ id: string; name: string }>;
  /** 人确认追加一个仓库 */
  onAppendRepository: (repositoryId: string) => Promise<void>;
  /** 计划换代状态（GET /plans/{id}）：当前版本号与收集窗状态。null = 还没有计划。 */
  planState: { planVersion: string; replanState: string } | null;
  /** ③ 执行中人工打断：提交一个**人点名**的仓库，后端判定它是否影响当前计划。 */
  onInterruptPlan: (repository: string, note: string) => Promise<InterruptOutcomeView>;
  /** 计划换代历史（GET /plans/{id}/revisions）。null = 还没读到。 */
  planRevisions: PlanRevisionView[] | null;
  /** 跨仓交付的一致版本清单（null = 还没有清单）。 */
  deliveryManifest: DeliveryManifestView | null;
  /** 生成一份清单快照（幂等键由页面持有）。 */
  onBuildManifest: () => Promise<void>;
}) {
  // A1 提交带进来的范围追加 props：本组件暂未消费（构建阻塞项），先显式忽略。
  void scopeRepoIds;
  void repoOptions;
  void onAppendRepository;
  const stepState = (i: number): string => {
    const st = stepStates[i];
    return st === "done" ? "已完成" : st === "run" ? "进行中" : st === "gate" ? "待人审" : st === "failed" ? "失败" : "未开始";
  };
  const producerOf = (block: { producer?: DiscoveryProducer } | null | undefined): string => {
    const p = block?.producer;
    if (!p) return "";
    const bits = [p.role, p.skill_id, p.run_id].filter(Boolean);
    return bits.length > 0 ? `产出者：${bits.join(" · ")}` : "";
  };
  const empty = (what: string) => (
    <p className="px-4 py-3 text-[11px] leading-[1.8] text-[var(--tree-faint)]">
      {what}还没有记录 —— 只显示真实发生过的事，不摆假进度。
    </p>
  );

  if (stage === 0) {
    if (!discovery) return empty("规划");
    const blocks = [discovery.analysis, discovery.candidates, null, discovery.plan, null];
    return (
      <div className="flex min-h-0 flex-1 flex-col gap-1.5 overflow-y-auto px-4 py-3">
        <p className="text-[12px] font-medium text-[var(--tree-ink)]">规划 · 五步回看</p>
        {STEP_LABELS.map((label, i) => (
          <div key={label} className="rounded-[8px] border border-[var(--tree-hairline)] bg-[var(--tree-card)] px-2.5 py-2 transition-colors hover:bg-[var(--tree-zone)]">
            <div className="flex items-center gap-2">
              <span className="text-[11.5px] text-[var(--tree-ink)]">{i + 1}. {label}</span>
              <span className="ml-auto text-[10.5px] text-[var(--tree-sub)]">{stepState(i)}</span>
            </div>
            {producerOf(blocks[i] as { producer?: DiscoveryProducer } | null) && (
              <p className="mt-1 text-[10.5px] leading-[1.7] text-[var(--tree-faint)]">{producerOf(blocks[i] as { producer?: DiscoveryProducer } | null)}</p>
            )}
            {i === 2 && discovery.approval?.state === "approved" && (
              <p className="mt-1 text-[10.5px] text-[var(--tree-faint)]">已批准（证据版本 {discovery.classification_evidence_version ?? "—"}）</p>
            )}
            {i === 4 && discovery.materialization?.status === "materialized" && (
              <p className="mt-1 break-all text-[10.5px] text-[var(--tree-faint)]">已物化 · 计划 {discovery.materialization.plan_id ?? "—"}</p>
            )}
          </div>
        ))}
        <IssueScopeCard scopeRepoIds={scopeRepoIds} repoOptions={repoOptions} onAppend={onAppendRepository} />
        <PlanReplanCard planState={planState} onInterrupt={onInterruptPlan} />
        <PlanRevisionsCard revisions={planRevisions} />
      </div>
    );
  }

  if (stage === 1) {
    if (!tasks || tasks.length === 0) return empty("执行");
    return (
      <div className="flex min-h-0 flex-1 flex-col gap-2 overflow-y-auto px-4 py-3">
        <p className="text-[12px] font-medium text-[var(--tree-ink)]">执行 · 任务与验收</p>
        {tasks.map((t) => (
          <div key={t.id} className="rounded-[8px] border border-[var(--tree-hairline)] bg-[var(--tree-card)] px-2.5 py-2 transition-colors hover:bg-[var(--tree-zone)]">
            <div className="flex items-center gap-2">
              <span className="min-w-0 flex-1 truncate text-[11.5px] text-[var(--tree-ink)]">{t.title}</span>
              <span className="flex-none text-[10.5px] text-[var(--tree-sub)]">{t.status}</span>
            </div>
            <p className="mt-1 text-[10.5px] text-[var(--tree-faint)]">
              执行者 {t.workerLabel || t.assignee || "—"} · 批次 {t.batchNo ?? "—"}
            </p>
            {testEvidence?.items
              .filter((item) => item.task_id === t.id)
              .map((item, index) => (
                <p key={index} className={`mt-1 text-[10.5px] leading-[1.7] ${item.passed ? "text-olive" : "text-salmon"}`}>
                  单点验收 {item.passed ? "通过" : "未过"}：{item.summary || "—"}
                </p>
              ))}
          </div>
        ))}
      </div>
    );
  }

  if (stage === 2) {
    const decided = (tasks ?? []).filter((t) => t.resultSummary);
    if (decided.length === 0) {
      return empty("审核");
    }
    return (
      <div className="flex min-h-0 flex-1 flex-col gap-1.5 overflow-y-auto px-4 py-3">
        <p className="text-[12px] font-medium text-[var(--tree-ink)]">审核 · 经理门决策</p>
        {decided.map((t) => (
          <div key={t.id} className="rounded-[8px] border border-[var(--tree-hairline)] bg-[var(--tree-card)] px-2.5 py-2 transition-colors hover:bg-[var(--tree-zone)]">
            <p className="text-[11.5px] text-[var(--tree-ink)]">{t.title}</p>
            <p className="mt-1 text-[10.5px] leading-[1.7] text-[var(--tree-sub)]">
              {t.resultSummary}
            </p>
          </div>
        ))}
      </div>
    );
  }

  if (!trainCars || trainCars.length === 0) return empty("交付");
  return (
    <div className="flex min-h-0 flex-1 flex-col gap-1.5 overflow-y-auto px-4 py-3">
      <p className="text-[12px] font-medium text-[var(--tree-ink)]">交付 · PR 列车</p>
      {trainCars.map((car, index) => (
        <div key={index} className="rounded-[8px] border border-[var(--tree-hairline)] bg-[var(--tree-card)] px-2.5 py-2 transition-colors hover:bg-[var(--tree-zone)]">
          <div className="flex items-center gap-2">
            <span className="min-w-0 flex-1 truncate text-[11.5px] text-[var(--tree-ink)]">{car.repo}</span>
            <span className={`flex-none rounded-[5px] border px-1.5 py-px text-[10px] ${car.merged ? "border-olive/40 bg-olive-well text-olive" : "border-[var(--tree-hairline)] text-[var(--tree-sub)]"}`}>
              {car.merged ? "已合并" : car.pr ? "待合并" : "未开 PR"}
            </span>
          </div>
          <p className="mt-1 break-all text-[10.5px] text-[var(--tree-faint)]">
            {car.pr || "尚未开 PR"} · 执行者 {car.by || "—"}
          </p>
        </div>
      ))}
      <DeliveryManifestCard manifest={deliveryManifest} onBuild={onBuildManifest} />
    </div>
  );
}

export interface FocusPanelProps {
  entry: FocusEntry | null;
  discovery: DiscoveryView | null;
  /** 需求正文（issue 的 description，缺失时回退发现链的 requirement_text）。
   *  Manager 房间的开场消息用它——它描述的是"人提交了什么"，建项那一刻就有，
   *  所以**不能**挂在 `discovery.analysis` 上（那是分析跑完才有的东西）。 */
  requirementText: string;
  stepStates: StepState[];
  task: PlanTaskItem | null;
  /** 当前 entry 的会话消息（MGR=主会话 / 任务=该任务房间）；null=加载中或不可用 */
  messages: ConversationMessage[] | null;
  // Manager 房间的真实消息（AgentTeams）。null=还没有可进的房,回落到 messages。
  roomMessages: ConversationMessage[] | null;
  /** 人工门动作（页面持写回路） */
  onGate: (action: "approveTiers" | "plan" | "materialize") => void;
  gateBusy: "approveTiers" | "plan" | "materialize" | null;
  gateError: string | null;
  /** 失败步重试 */
  onRetryStep: (step: 1 | 2 | 3 | 4) => void;
  /** 这一步推进失败的原因（驱动器不再静默重试，把原因摆到界面上）。 */
  stepError: { step: number; message: string } | null;
  /** 「忽略追问，强制继续」：需求信息偏少时给用户的另一条路。 */
  onForceContinue: () => void;
  /** 经理门（审核段）：blocked 任务的通过/驳回。 */
  onDecideTask: (taskId: string, decision: "approve" | "reject", reason: string) => Promise<void>;
  /** 监管策略草稿卡片的合成态（由页面的拓扑态 + 草稿态合成，见 useIssueFlowState） */
  policyCard: PolicyDraftState;
  onConfigurePolicy: () => void;
  onRetryPolicy: () => void;
  /** 输入框配置；null=不渲染输入 */
  input: { placeholder: string; sending: boolean; disabled?: boolean; onSend: (text: string) => void } | null;
  /** 交付期到达且人工参与:合并确认也作为一条带按钮的消息出现在 Manager 房间 */
  mergePending?: boolean;
  /** 点「查看交付序列」:左栏那列交付列车高亮（合并确认在列车上做，聊天卡只做指针）
   *  2026-09-20 移植主线 9f206b0a。 */
  onViewTrain?: () => void;
  /** 收起右栏(2026-09-20 移植主线 0c7a54a1):收起后左栏铺满,窄条上一个展开按钮 */
  onCollapse?: () => void;
  /** 候选分流(2026-09-18 用户裁定):第 2 步聊天室里选——人勾选 / AI 推断 */
  onChooseManual?: () => void;
  onChooseAI?: () => void;
  /** 漏选清单确认(勾中的仓库名) */
  onConfirmSupplements?: (repositories: string[]) => void;
  /** 分流动作进行中(选择卡按钮置灰) */
  selectionBusy?: boolean;
  /** 测试团队的记录（task 单点 / DAG 节点集成 / 跨仓库联调回归）。 */
  testEvidence: TestEvidenceView | null;
  /** 阶段历史（顶栏「流程」点开）要看的东西：任务行与交付列车。 */
  tasks: PlanTaskItem[] | null;
  trainCars: TrainCarSpec[] | null;
  /** 本次 Issue 当前的仓库范围（issue 详情的 repositoryIds） */
  scopeRepoIds: string[];
  /** 本项目已挂的仓库（追加候选只从这里来） */
  repoOptions: Array<{ id: string; name: string }>;
  /** 当前项目 id —— 「worker 工作内容」读面按 (projectId, taskId) 取，
   *  而那条读面在服务端按「项目 owner 或 admin」授权。 */
  projectId?: string | null;
  /** 人确认把一个仓库追加进本次 Issue 的范围 */
  onAppendRepository: (repositoryId: string) => Promise<void>;
  /** 计划换代状态（GET /plans/{id}）：当前版本号与收集窗状态。null = 还没有计划。
   *  ↑ 这两枚是「执行中人工打断」那套（PlanReplanCard，A1(f)）的入参，
   *  调用方 WorkbenchPage 已经在传，但顶层 props 漏了声明、StageHistory 调用点
   *  也漏了透传——于是 42acf537 自己就 tsc 不干净（部署流程跳过 tsc，所以照样上线）。
   *  这里补齐三处：接口、解构、StageHistory 调用点。 */
  planState: { planVersion: string; replanState: string } | null;
  /** ③ 执行中人工打断：提交一个**人点名**的仓库，后端判定它是否影响当前计划。 */
  planRevisions: PlanRevisionView[] | null;
  /** 跨仓交付的一致版本清单（null = 还没有清单）。 */
  deliveryManifest: DeliveryManifestView | null;
  /** 生成一份清单快照（幂等键由页面持有）。 */
  onBuildManifest: () => Promise<void>;
  onInterruptPlan: (repository: string, note: string) => Promise<InterruptOutcomeView>;
}

export function FocusPanel({
  entry,
  discovery,
  requirementText,
  stepStates,
  task,
  messages,
  roomMessages,
  onGate,
  gateBusy,
  gateError,
  onRetryStep,
  stepError,
  onForceContinue,
  onDecideTask,
  policyCard,
  onConfigurePolicy,
  onRetryPolicy,
  input,
  mergePending = false,
  onViewTrain,
  onCollapse,
  testEvidence,
  tasks,
  trainCars,
  scopeRepoIds,
  projectId = null,
  repoOptions,
  onAppendRepository,
  onChooseManual,
  onChooseAI,
  onConfirmSupplements,
  selectionBusy = false,
  planState,
  onInterruptPlan,
  planRevisions,
  deliveryManifest,
  onBuildManifest,
}: FocusPanelProps) {
  const body = (() => {
    if (entry === null) {
      return (
        <div className="flex flex-1 items-center justify-center px-6 text-center text-[11.5px] leading-[1.8] text-[var(--tree-faint)]">
          点击左侧步骤 / 任务行，这里显示详情、人工门操作与消息时序流
        </div>
      );
    }
    if (entry.kind === "step") return <StepDetail step={entry.step} state={stepStates[entry.step - 1]} discovery={discovery} onGate={onGate} gateBusy={gateBusy} gateError={gateError} onRetryStep={onRetryStep} stepError={stepError} onForceContinue={onForceContinue} policyCard={policyCard} onConfigurePolicy={onConfigurePolicy} onRetryPolicy={onRetryPolicy} messages={messages} />;
    // 测试组：task 单点 / DAG 节点集成 / 跨仓库联调回归的**真实记录**。
    // 2026-09-20：主体改成房间消息流（TestStream），跟其他房间一个读法——
    // 每条记录就是测试组说过的一句话；下面的分类清单保留，便于按 kind 核对。
    if (entry.kind === "tests") {
      return (
        <div className="flex min-h-0 flex-1 flex-col overflow-y-auto">
          {/* 测试排期排在最前：先看"要跑什么、跑到哪了"，再看逐条记录。
              2026-09-20 用户反馈"测试组全程不展示任何规划和测试排期"——
              此前这一栏只有事后记录，没有计划那一层。 */}
          <TestSchedule tasks={tasks} view={testEvidence} />
          {testEvidence === null ? (
            <div className="flex flex-1 items-center justify-center text-[11px] text-[var(--tree-faint)]">测试记录加载中…</div>
          ) : (
            <TestStream view={testEvidence} />
          )}
          <TestEvidenceDetail view={testEvidence} />
        </div>
      );
    }
    // 顶栏「流程」点开的阶段历史（规划/执行/审核/交付）。
    if (entry.kind === "stage") {
      return (
        <StageHistory
          stage={entry.stage}
          discovery={discovery}
          stepStates={stepStates}
          tasks={tasks}
          testEvidence={testEvidence}
          trainCars={trainCars}
          scopeRepoIds={scopeRepoIds}
          projectId={projectId}
          repoOptions={repoOptions}
          onAppendRepository={onAppendRepository}
          planState={planState}
          onInterruptPlan={onInterruptPlan}
          planRevisions={planRevisions}
          deliveryManifest={deliveryManifest}
          onBuildManifest={onBuildManifest}
        />
      );
    }
    if (entry.kind === "task") {
      return (
        <div className="flex min-h-0 flex-1 flex-col">
          {/* Worker 恢复卡（Phase 2+3，2026-09-20 并入）：派工门判定 + 恢复动作 */}
          <WorkerHealthGate workerName={task?.workerLabel ?? null} />
          {task === null ? (
            <p className="px-4 py-3 text-[11px] text-[var(--tree-faint)]">任务行数据未取到，不摆假详情。</p>
          ) : messages === null ? (
            <div className="flex flex-1 items-center justify-center text-[11px] text-[var(--tree-faint)]">消息流加载中…</div>
          ) : (
            <MessageTimeline messages={messages} />
          )}
          {/* 经理门（审核段）：agent 跑完把任务置 blocked 等经理批 —— 此前后端有
              approve/reject 端点、前端也有调用封装，但**界面上没有任何入口**，
              于是任务永远停在 blocked，整条链看起来"卡住"。 */}
          {task !== null && task.status === "blocked" && (
            <TaskGate task={task} onDecide={onDecideTask} />
          )}
          {/* worker 的真实工作内容（agent-stdout/stderr 尾部）。
              用户反复提过"看不到 worker 具体的工作内容 / 内部工作记录"——
              此前右栏这一块**没有任何数据源**（public.log_entries 零生产者），
              真正的产出一直躺在工作区的 agent-stdout.log 里没人读。 */}
          {task !== null && <TaskAgentOutput projectId={projectId} taskId={task.id} />}
          <PlanHistory discovery={discovery} />
        </div>
      );
    }
    // MGR：主会话完整时间线（回看入口）+ 待人审计项就地成为消息
    return (
      <div className="flex min-h-0 flex-1 flex-col overflow-y-auto">
        {/* 开场两句话排在最前（2026-09-20）：人上传/发送需求这件事发生在建项那一刻，
            不依赖分析有没有开始，所以它必须在时间线之前、且与链路状态无关。 */}
        <RequirementOpening requirement={requirementText} />
        {roomMessages !== null ? (
          /* AgentTeams 团队房的真实消息：建项「收到新需求」、规划派发/完成/失败都落在这。
             空数组=真没人说话，如实显示空态；读不到就回落，不冒充。 */
          <MessageTimeline messages={roomMessages} />
        ) : messages === null ? (
          <div className="flex flex-1 items-center justify-center py-6 text-[11px] text-[var(--tree-faint)]">主会话加载中…</div>
        ) : (
          <MessageTimeline messages={messages} />
        )}
        <StepStream discovery={discovery} stepStates={stepStates} onGate={onGate} gateBusy={gateBusy} />
        {/* 测试组的产出也进房间流（2026-09-20）：此前它只在自己的条目里有记录，
            Manager 主房间里看不到"验收过了没有"，得自己切过去翻。 */}
        <TestStream view={testEvidence} />
        <PlanHistory discovery={discovery} />
        <GateStack
          stepStates={stepStates}
          mergePending={mergePending}
          onGate={onGate}
          onViewTrain={onViewTrain}
          gateBusy={gateBusy}
          gateError={gateError}
          discovery={discovery}
          onChooseManual={onChooseManual}
          onChooseAI={onChooseAI}
          onConfirmSupplements={onConfirmSupplements}
          onRetryStep={onRetryStep}
          selectionBusy={selectionBusy}
        />
      </div>
    );
  })();

  const header = (() => {
    if (entry === null) {
      return { title: "选择条目查看详情", roomNo: "RM-—", note: "点左侧步骤 / 任务行进入房间", members: [] as RoomMember[] };
    }
    if (entry.kind === "step") {
      const titles = ["需求分析", "候选评分", "分档审批", "生成计划", "物化确认"];
      return {
        title: titles[entry.step - 1], roomNo: `RM-S${entry.step}`,
        note: `规划 · 步骤 ${entry.step}`, members: [{ label: "M", cls: "m" }] as RoomMember[],
      };
    }
    if (entry.kind === "task") {
      const members: RoomMember[] = [];
      if (task?.leaderLabel) members.push({ label: "L", cls: "l", name: task.leaderLabel });
      if (task?.workerLabel) members.push({ label: "W", cls: "w", name: task.workerLabel });
      // 与任务行同一套取值顺序：装配期显示名 → 真实执行者（后端任务树读面带出的
      // assignee）→ 才写「待指派」。此前这里只看 leaderLabel，而物化写入端不填它，
      // 于是右栏顶部永远写「待指派」，哪怕这条任务已经跑完。
      return {
        title: task ? `${task.taskUid ?? ""} ${task.title}`.trim() : "任务详情",
        roomNo: `RM-${(task?.taskUid ?? task?.id ?? "—").split(":").pop()?.slice(0, 6).toUpperCase() ?? "—"}`,
        note: `${task?.leaderLabel || task?.assignee || "待指派"} · 批次${task?.batchNo ?? "—"}`,
        members,
      };
    }
    // 测试组自己的房间（2026-09-20 接上）：此前它没有门牌分支，会掉进下面的
    // Manager 兜底——点开测试组，门牌写着 RM-MGR「Manager · 主会话时间线」，
    // 看上去就是"测试组根本没有自己的页面"。
    if (entry.kind === "tests") {
      const total = testEvidence?.items.length ?? 0;
      const passed = testEvidence?.passed ?? 0;
      const failed = testEvidence?.failed ?? 0;
      return {
        title: "测试组 · db-test",
        roomNo: "RM-TEST",
        note:
          total === 0
            ? "验证团队 · 还没有记录"
            : `验证团队 · ${total} 条记录 · ${passed} 通过${failed > 0 ? ` · ${failed} 未过` : ""}`,
        members: [
          { label: "T", cls: "t", name: "db-test 测试组" },
          { label: "你", cls: "u" },
        ] as RoomMember[],
      };
    }
    return {
      title: "Manager · 主会话时间线", roomNo: "RM-MGR",
      note: "主会话 · 规划与下发全程回看", members: [{ label: "M", cls: "m" }, { label: "你", cls: "u" }] as RoomMember[],
    };
  })();

  return (
    <aside className="flex h-full w-[400px] flex-none flex-col border-l border-line bg-[var(--tree-card)]">
      {/* 门牌（房间样式提案 E，2026-09-18）：房檐条 = 房间图标 + 房间名 + mono 门牌号，
          右侧住户头像堆叠 + 在线点——房间有地址、有住户。 */}
      <div className="flex items-center gap-2.5 border-b border-[var(--tree-hairline)] bg-[var(--tree-zone)] px-4 py-2.5">
        <IconHouse size={15} className="flex-none text-[var(--tree-acc)]" />
        <div className="min-w-0 flex-1">
          <p className="truncate text-[12.5px] font-medium leading-[1.4] text-[var(--tree-ink)]">{header.title}</p>
          <p className="truncate font-mono text-[9.5px] tracking-wide text-[var(--tree-faint)]">
            {header.roomNo} · {header.note}
          </p>
        </div>
        {header.members.length > 0 && (
          <div className="ml-auto flex flex-none pl-1.5">
            {header.members.map((m, i) => (
              <RoomAvatar key={i} member={m} />
            ))}
          </div>
        )}
        {/* 收起右栏(2026-09-20 移植主线 0c7a54a1)：摆在门牌条上而不是正文里，
            「选中了条目时也能收起」——正文里那块空态按钮只在没选条目时可见。 */}
        {onCollapse && (
          <button
            type="button"
            className="grid size-6 flex-none place-items-center rounded-hard text-[var(--tree-faint)] transition-colors hover:bg-[var(--tree-card)] hover:text-[var(--tree-ink)]"
            title="收起详情面板"
            onClick={() => onCollapse()}
          >
            <IconX size={13} strokeWidth={1.75} />
          </button>
        )}
      </div>
      {/* 天花板：房檐下一条渐变阴影，「进到屋里」的纵深 */}
      <div className="h-2.5 flex-none bg-gradient-to-b from-[color-mix(in_oklab,var(--tree-faint)_16%,transparent)] to-transparent" />
      {body}
      {input && <FocusInput {...input} />}
    </aside>
  );
}

/** 门牌住户头像：角色色圆 + 在线点（「你」按离线渲染——人在门外看） */
type RoomMember = { label: string; cls: "m" | "l" | "w" | "u" | "t"; name?: string };
const ROOM_AVA_CLS: Record<string, string> = {
  m: "bg-[var(--tree-acc)] text-[#16130d]",
  l: "bg-[var(--tree-role-l)] text-white",
  w: "bg-olive text-white",
  u: "bg-amber text-[#16130d]",
  // 测试组：与左树那枚烧瓶同色。
  t: "bg-amber text-[#16130d]",
};
const ROOM_ROLE_NAME: Record<string, string> = {
  m: "Manager",
  l: "Leader",
  w: "Worker",
  u: "用户",
  t: "测试组",
};
function RoomAvatar({ member }: { member: RoomMember }) {
  return (
    <span
      className={`relative grid size-[22px] place-items-center rounded-full text-[8.5px] font-bold ring-2 ring-[var(--tree-zone)] ${ROOM_AVA_CLS[member.cls]} -ml-1.5 first:ml-0`}
      title={member.name ? `${member.name}（${ROOM_ROLE_NAME[member.cls] ?? ""}）` : undefined}
    >
      {member.label}
      <span className={`absolute -right-0.5 -bottom-0.5 size-2 rounded-full border-[1.5px] border-[var(--tree-zone)] ${member.cls === "u" ? "bg-[var(--tree-faint)]" : "bg-[#4caf7d]"}`} />
    </span>
  );
}

/** 待人审计项:就地成为 Manager 房间里的带按钮消息(「需要人的地方成为消息」
 *  惯例)。分档审批 / 物化确认 / 合并确认,与树上步骤卡共用同一套写回路。 */

/** 房间里的一条消息（发言人行）：M=Manager / 你=用户。
 *  StepStream 与开场消息共用同一套——同一间房里的消息不该长两种样子。 */
function ChatRow({ who, children }: { who: "user" | "mgr"; children: ReactNode }) {
  return (
    <div className="flex gap-2.5">
      {who === "mgr" ? (
        <span className="mt-0.5 grid h-[22px] w-[22px] flex-none place-items-center rounded-full bg-[var(--tree-acc)] text-[8.5px] font-bold text-white">M</span>
      ) : (
        <span className="mt-0.5 grid h-[22px] w-[22px] flex-none place-items-center rounded-full bg-[var(--tree-zone)] text-[8.5px] font-bold text-[var(--tree-sub)]">你</span>
      )}
      <div className="min-w-0 flex-1">
        <div className="mb-0.5 flex items-center gap-1.5">
          <span className="text-[11px] font-semibold text-[var(--tree-ink)]">{who === "mgr" ? "Manager" : "你"}</span>
          <span className="rounded-[5px] bg-[var(--tree-acc)]/12 px-1.5 py-px text-[9.5px] text-[var(--tree-acc)]">{who === "mgr" ? "Manager" : "用户"}</span>
        </div>
        {children}
      </div>
    </div>
  );
}

function ChatCard({ children }: { children: ReactNode }) {
  return (
    <div className="rounded-lg border border-[var(--tree-hairline)] bg-[var(--tree-card)] px-2.5 py-2 text-[11.5px] leading-[1.65] text-[var(--tree-ink)]">{children}</div>
  );
}

/** 房间的开场两句话：「你上传了需求文档」→「收到，开始分析需求…」。
 *
 *  2026-09-20 修两个叠在一起的问题（用户实测：点进 Manager 房间，流里没有
 *  "我上传需求文档"这一条）：
 *
 *  1. **挂错了数据源**。此前这两条长在 `StepStream` 里、读的是
 *     `discovery.analysis.analyzed_requirement`——那是**分析跑完之后**才有的东西。
 *     而"我上传了需求文档"这件事发生在**建项那一刻**，早于任何分析：刚建完还没跑
 *     分析时 `issue_discoveries` 连行都还没有（discovery 为 null），`StepStream`
 *     第一行的 `if (!discovery ...) return null` 就把整段开场吞掉了；跑完分析之后
 *     读到的又变成"模型改写后的一行摘要"，而不是用户真正提交的东西。
 *     现在它自己成一块、由需求正文驱动，**与链路状态无关**——人说过的话不依赖
 *     机器有没有开始干活。
 *
 *  2. **位置不在最前**。此前它排在 `MessageTimeline` **之后**，所以就算渲染出来也
 *     不是房间里的第一句。现在由 Manager 房间体摆在时间线**之前**。
 *
 *  文档正文本身不往气泡里倒（契约：用户没打字就一个字都不替他展示），只报
 *  「上传了需求文档」+ 文档自己的标题。 */
function RequirementOpening({ requirement }: { requirement: string }) {
  const { typed, document } = splitRequirement(requirement);
  if (typed === "" && document === "") return null;
  const docTitle = document ? documentTitleOf(document) : null;
  return (
    <div className="flex flex-col gap-3 px-4 pt-3 pb-1">
      <ChatRow who="user">
        <ChatCard>
          <span className="whitespace-pre-wrap break-words font-semibold">
            {typed !== "" ? typed : "上传了需求文档"}
          </span>
          {document !== "" && (
            <p className="mt-1 break-words text-[11px] text-[var(--tree-sub)]">
              {typed !== "" ? "并上传了文档：" : "文档："}
              {docTitle ?? "未命名文档"}
            </p>
          )}
        </ChatCard>
      </ChatRow>
      <ChatRow who="mgr">
        <ChatCard>
          <span className="font-semibold">收到，开始分析需求…</span>
        </ChatCard>
      </ChatRow>
    </div>
  );
}

/** 规划步骤流(2026-09-18):①-⑤ 全部消息化进 Manager 主房间。
 *  数据从发现链读面推导——真实链路每进一步,轮询刷新即流入新「消息」;
 *  人工门(③⑤)的消息带按钮,就地成为可操作的对话。 */
function StepStream({
  discovery,
  stepStates,
  onGate,
  gateBusy,
}: {
  discovery: DiscoveryView | null;
  stepStates: StepState[];
  onGate: (action: "approveTiers" | "plan" | "materialize") => void;
  gateBusy: "approveTiers" | "plan" | "materialize" | null;
}) {
  if (!discovery || stepStates[0] === "wait") return null;
  const msgRow = (node: ReactNode, key: string, who: "user" | "mgr" = "mgr") => (
    <ChatRow key={key} who={who}>
      {node}
    </ChatRow>
  );
  const card = (children: ReactNode) => <ChatCard>{children}</ChatCard>;
  const flow: ReactNode[] = [];
  // 开场两句话（「你上传了需求文档」→「收到，开始分析需求…」）已经挪到
  // RequirementOpening：由需求正文驱动、与链路状态无关，而且排在时间线之前。
  // 这里只留"链路开始干活之后"的消息——不在这里再报一遍用户说过的话。
  // ① 需求分析
  const a = discovery.analysis;
  if (a) {
    flow.push(msgRow(card(<>
      <span className="font-semibold">需求分析完成</span>
    </>), "s1"));
  }
  // ② 候选评分
  const c = discovery.candidates;
  if (c) {
    flow.push(msgRow(card(<>
      <span className="font-semibold">候选仓库 {c.items.length} 个</span>
      {c.items.length > 0 && (
        <span className="text-[var(--tree-sub)]"> · {c.items.slice(0, 3).map((it) => it.repository_name).join("、")}{c.selection_mode === "manual" ? "(你勾选)" : "(AI 推断)"}</span>
      )}
      {c.items.length === 0 && <span className="text-[var(--tree-sub)]"> · 目录内没有命中候选</span>}
    </>), "s2"));
  }
  // ③ 分档审批
  const cls = discovery.classification;
  if (cls) {
    const pending = discovery.approval?.state !== "approved" && stepStates[2] === "gate";
    // ponytail: 超过 3 个仓库只展示前 3 + "等 N 个"，不平铺 20 个（2026-09-18 用户反馈太密）
    const tierLine = (t: string, arr: Array<{ repository: string }>) => {
      if (!arr.length) return `${t}：无`;
      const names = arr.map((r) => r.repository.replace(/^.*\//, ""));
      return names.length <= 3 ? `${t}：${names.join("、")}` : `${t}：${names.slice(0, 3).join("、")} 等 ${names.length} 个`;
    };
    flow.push(msgRow(card(<>
      <span className="font-semibold">{discovery.approval?.state === "approved" ? "分档已确认" : "分档待人审"}</span>
      <div className="mt-1 flex flex-col gap-0.5 text-[11px] text-[var(--tree-sub)]">
        <p>{tierLine("必需", cls.required)}</p>
        <p>{tierLine("可能", cls.maybe)}</p>
        <p className="text-[var(--tree-faint)]">{tierLine("排除", cls.excluded)}</p>
        {cls.required.length + cls.maybe.length === 0 && <p>必需+可能均为空——评分器无信号,请回树上「分档审批」把仓库调进必需档。</p>}
      </div>
      {pending && cls.required.length + cls.maybe.length > 0 && (
        <button type="button" disabled={gateBusy === "approveTiers"} onClick={() => onGate("approveTiers")}
          className="mt-1.5 rounded-[7px] border border-amber/40 bg-amber-well px-3 py-1 text-[11.5px] font-semibold text-amber hover:bg-amber-well/80 disabled:opacity-50">
          {gateBusy === "approveTiers" ? "提交中…" : "批准分档"}
        </button>
      )}
    </>), "s3"));
  }
  // ④ 生成计划
  //
  //  2026-09-20（用户反馈："生成计划……没有人工确认项，而且也不展示"）：人工参与
  //  模式下 ④ 也是一道**门**，消息里要出现按钮；自动托管模式则由处理员代行，
  //  这里如实写明"代行"，人照样看得见这一步发生过。
  const integration = discovery.integration;
  const planned = (discovery.plan ?? null) !== null || integration !== null;
  const planGate = stepStates[3] === "gate";
  if (planned || planGate || stepStates[3] === "run" || stepStates[3] === "failed") {
    const counts = integration ? ` · ${integration.task_dag_count} 任务 ${integration.batch_count} 批` : "";
    flow.push(msgRow(card(<>
      <span className="font-semibold">
        {planned
          ? `计划已生成${counts}`
          : stepStates[3] === "run"
            ? "正在生成计划…"
            : stepStates[3] === "failed"
              ? "生成计划失败"
              : "生成计划 · 待人审"}
      </span>
      {planGate && (
        <p className="mt-0.5 text-[11px] leading-[1.6] text-[var(--tree-sub)]">
          分档已批准。确认后由仓库 Leader 产出计划（任务 DAG 与批次划分），产出后回到这里确认物化。
        </p>
      )}
      {planGate && (
        <button type="button" disabled={gateBusy === "plan"} onClick={() => onGate("plan")}
          className="mt-1.5 rounded-[7px] border border-amber/40 bg-amber-well px-3 py-1 text-[11.5px] font-semibold text-amber hover:bg-amber-well/80 disabled:opacity-50">
          {gateBusy === "plan" ? "派发中…" : "生成计划"}
        </button>
      )}
    </>), "s4"));
  }
  // ⑤ 物化确认
  if (stepStates[4] === "gate" || stepStates[4] === "done") {
    const done = stepStates[4] === "done";
    flow.push(msgRow(card(<>
      <span className="font-semibold">{done ? "物化完成 · 已开工" : "物化确认 · 待人审"}</span>
      {!done && <p className="mt-0.5 text-[11px] text-[var(--tree-sub)]">确认后组建编制并下发批次1,左侧树换代为任务视图。</p>}
      {!done && (
        <button type="button" disabled={gateBusy === "materialize"} onClick={() => onGate("materialize")}
          className="mt-1.5 rounded-[7px] bg-[var(--tree-acc)] px-3 py-1 text-[11.5px] font-semibold text-white hover:bg-[var(--tree-acc)]/90 disabled:opacity-50">
          {gateBusy === "materialize" ? "物化中…" : "确认物化并开工"}
        </button>
      )}
    </>), "s5"));
  }
  if (flow.length === 0) return null;
  return <div className="flex flex-col gap-3 px-4 pb-1">{flow}</div>;
}

function GateStack({
  stepStates,
  mergePending,
  onGate,
  onViewTrain,
  gateBusy,
  gateError,
  discovery,
  onChooseManual,
  onChooseAI,
  onConfirmSupplements,
  onRetryStep,
  selectionBusy = false,
}: {
  stepStates: StepState[];
  mergePending: boolean;
  onGate: (action: "approveTiers" | "plan" | "materialize") => void;
  onViewTrain?: () => void;
  gateBusy: "approveTiers" | "plan" | "materialize" | null;
  gateError: string | null;
  discovery: DiscoveryView | null;
  onChooseManual?: () => void;
  onChooseAI?: () => void;
  onConfirmSupplements?: (repositories: string[]) => void;
  onRetryStep?: (step: 1 | 2 | 3 | 4) => void;
  selectionBusy?: boolean;
}) {
  const cards: Array<{ key: string; title: string; desc: string; label: string; action: () => void; busy: boolean }> = [];
  // 失败卡点卡：发现链哪步带错误，原因原文就地展示，重试换新幂等键走原触发端点
  const stepNames = ["需求分析", "候选评分", "分档审批", "生成计划"] as const;
  const errorOf: Array<string | null> = [
    discovery?.analysis?.error ? String((discovery.analysis.error as { message?: string }).message ?? "执行失败") : null,
    discovery?.candidates?.error ? String((discovery.candidates.error as { message?: string }).message ?? "执行失败") : null,
    discovery?.classification?.error ? String((discovery.classification.error as { message?: string }).message ?? "执行失败") : null,
    null,
  ];
  errorOf.forEach((msg, i) => {
    if (msg === null) return;
    cards.push({
      key: `failed-${i + 1}`,
      title: `发现链 · ${stepNames[i]} 失败`,
      desc: msg,
      label: "重试",
      action: () => onRetryStep?.((i + 1) as 1 | 2 | 3 | 4),
      busy: false,
    });
  });
  if (stepStates[2] === "gate") {
    cards.push({
      key: "tiers",
      title: "分档审批 · 待人审",
      desc: "候选仓库已定档,批准后处理员继续生成计划",
      label: "批准分档",
      action: () => onGate("approveTiers"),
      busy: gateBusy === "approveTiers",
    });
  }
  // ④ 生成计划（人工参与模式下的第三道门，2026-09-20 补）。
  if (stepStates[3] === "gate") {
    cards.push({
      key: "plan",
      title: "生成计划 · 待人审",
      desc: "分档已批准,确认后派发规划;计划产出后回到这里确认物化",
      label: "生成计划",
      action: () => onGate("plan"),
      busy: gateBusy === "plan",
    });
  }
  if (stepStates[4] === "gate") {
    cards.push({
      key: "materialize",
      title: "物化确认 · 待人审",
      desc: "计划 v1 已就绪,确认后组建编制并下发批次1",
      label: "确认物化并开工",
      action: () => onGate("materialize"),
      busy: gateBusy === "materialize",
    });
  }
  if (mergePending) {
    // 2026-09-20 移植主线 9f206b0a：这张卡从「确认合并」变成「查看交付序列」指针
    // —— 合并确认在左栏那列车上做（那里能看到每节车厢的门禁与顺序），聊天卡里
    // 只负责把人带到列车前。
    cards.push({
      key: "merge",
      title: "交付序列 · 待确认合并",
      desc: "全部任务完成、门禁就绪;合并顺序见左侧交付列车",
      label: "查看交付序列",
      action: () => onViewTrain?.(),
      busy: false,
    });
  }
  const supplementPending = stepStates[2] === "confirm";
  if (cards.length === 0 && stepStates[1] !== "choose" && !supplementPending) return null;
  return (
    <div className="flex flex-col gap-2.5 border-t border-dashed border-[var(--tree-hairline)] px-4 py-3">
      {stepStates[1] === "choose" && (
        <div className="flex gap-2.5">
          <span className="mt-0.5 grid h-[22px] w-[22px] flex-none place-items-center rounded-full bg-amber-well text-amber">
            <IconClock size={12} />
          </span>
          <div className="min-w-0 flex-1">
            <div className="mb-0.5 flex items-center gap-1.5">
              <span className="text-[11px] font-medium text-[var(--tree-ink)]">候选仓库 · 待人选择</span>
            </div>
            <div className="rounded-hard border border-[var(--tree-hairline)] bg-[var(--tree-card)] px-2.5 py-2 transition-colors hover:bg-[var(--tree-zone)]">
              <p className="text-[11px] leading-[1.6] text-[var(--tree-sub)]">这个需求涉及哪些仓库?你来勾选,或让 AI 从项目目录推断。</p>
              <div className="mt-1.5 flex gap-2">
                <button
                  type="button"
                  disabled={selectionBusy}
                  onClick={() => onChooseManual?.()}
                  className="rounded-hard border border-amber/40 bg-amber-well px-3 py-1 text-[11.5px] font-semibold text-amber hover:bg-amber-well/80 disabled:opacity-50"
                >
                  我自己勾选
                </button>
                <button
                  type="button"
                  disabled={selectionBusy}
                  onClick={() => onChooseAI?.()}
                  className="rounded-hard border border-[var(--tree-acc)] bg-[var(--tree-acc)]/10 px-3 py-1 text-[11.5px] font-semibold text-[var(--tree-acc)] hover:bg-[var(--tree-acc)]/20 disabled:opacity-50"
                >
                  让 AI 推断
                </button>
              </div>
            </div>
          </div>
        </div>
      )}
      {supplementPending && <SupplementConfirmCard discovery={discovery} onConfirm={onConfirmSupplements} busy={selectionBusy} />}
      {cards.map((c) => (
        <div key={c.key} className="flex gap-2.5">
          <span className="mt-0.5 grid h-[22px] w-[22px] flex-none place-items-center rounded-full bg-amber-well text-amber">
            <IconClock size={12} />
          </span>
          <div className="min-w-0 flex-1">
            <div className="mb-0.5 flex items-center gap-1.5">
              <span className="text-[11px] font-medium text-[var(--tree-ink)]">{c.title}</span>
            </div>
            <div className="rounded-lg border border-[var(--tree-hairline)] bg-[var(--tree-card)] px-2.5 py-2 transition-colors hover:bg-[var(--tree-zone)]">
              <p className="text-[11px] leading-[1.6] text-[var(--tree-sub)]">{c.desc}</p>
              <button
                type="button"
                disabled={c.busy}
                onClick={c.action}
                className="mt-1.5 rounded-[7px] border border-amber/40 bg-amber-well px-3 py-1 text-[11.5px] font-semibold text-amber hover:bg-amber-well/80 disabled:opacity-50"
              >
                {c.busy ? "提交中…" : c.label}
              </button>
            </div>
          </div>
        </div>
      ))}
      {gateError && (
        <p className="rounded-[7px] border border-salmon/40 bg-salmon-well px-2.5 py-1.5 text-[11px] text-salmon">{gateError}</p>
      )}
    </div>
  );
}

/** 漏选清单确认卡(人工勾选路径的第 3 步)：依赖图查出的漏选仓库，
 *  逐项勾选(默认全选)后确认并入必需档。 */
function SupplementConfirmCard({
  discovery,
  onConfirm,
  busy,
}: {
  discovery: DiscoveryView | null;
  onConfirm?: (repositories: string[]) => void;
  busy: boolean;
}) {
  const supplements = discovery?.classification?.supplements ?? [];
  const [checked, setChecked] = useState<Record<string, boolean>>({});
  const isChecked = (name: string) => checked[name] !== false; // 默认勾选
  const chosen = supplements.filter((s) => isChecked(s.repository)).map((s) => s.repository);
  if (supplements.length === 0) return null;
  return (
    <div className="flex gap-2.5">
      <span className="mt-0.5 grid h-[22px] w-[22px] flex-none place-items-center rounded-full bg-amber-well text-amber">
        <IconClock size={12} />
      </span>
      <div className="min-w-0 flex-1">
        <div className="mb-0.5 flex items-center gap-1.5">
          <span className="text-[11px] font-medium text-[var(--tree-ink)]">漏选清单 · 待人确认</span>
        </div>
        <div className="rounded-hard border border-[var(--tree-hairline)] bg-[var(--tree-card)] px-2.5 py-2 transition-colors hover:bg-[var(--tree-zone)]">
          <p className="text-[11px] leading-[1.6] text-[var(--tree-sub)]">依赖图显示这些仓库可能被漏选(你勾选的仓库依赖它们):</p>
          <div className="mt-1 flex flex-col gap-0.5">
            {supplements.map((s) => (
              <label key={s.repository} className="flex cursor-pointer items-center gap-2 text-[11.5px] text-[var(--tree-ink)]">
                <input
                  type="checkbox"
                  checked={isChecked(s.repository)}
                  onChange={() => setChecked((prev) => ({ ...prev, [s.repository]: !isChecked(s.repository) }))}
                  className="size-3.5 accent-[var(--tree-acc)]"
                />
                <span className="font-mono text-[11px]">{s.repository}</span>
                {s.via && <span className="text-[10px] text-[var(--tree-faint)]">via {s.via}</span>}
              </label>
            ))}
          </div>
          <button
            type="button"
            disabled={busy || chosen.length === 0}
            onClick={() => onConfirm?.(chosen)}
            className="mt-1.5 rounded-hard border border-amber/40 bg-amber-well px-3 py-1 text-[11.5px] font-semibold text-amber hover:bg-amber-well/80 disabled:opacity-50"
          >
            {busy ? "提交中…" : `确认补充${chosen.length > 0 ? `(${chosen.length})` : ""}`}
          </button>
        </div>
      </div>
    </div>
  );
}

function FocusInput({  placeholder,
  sending,
  disabled,
  onSend,
}: {
  placeholder: string;
  sending: boolean;
  disabled?: boolean;
  onSend: (text: string) => void;
}) {
  const ref = useRef<HTMLTextAreaElement | null>(null);
  const send = () => {
    const el = ref.current;
    if (!el) return;
    const text = el.value.trim();
    if (!text || sending || disabled) return;
    onSend(text);
    el.value = "";
  };
  return (
    <div className="border-t border-[var(--tree-hairline)] p-2.5">
      <div className="flex items-center gap-2 rounded-[7px] border border-[var(--tree-line)] bg-[var(--tree-card)] px-3 py-1.5">
        <textarea
          ref={ref}
          rows={1}
          disabled={disabled || sending}
          placeholder={placeholder}
          className="max-h-24 min-w-0 flex-1 resize-none bg-transparent text-[11.5px] leading-[1.6] text-[var(--tree-ink)] outline-none placeholder:text-[var(--tree-faint)]"
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey) {
              e.preventDefault();
              send();
            }
          }}
        />
        <button
          type="button"
          className="grid flex-none place-items-center rounded text-[var(--tree-acc)] disabled:text-[#c9c9c5]"
          disabled={disabled || sending}
          onClick={send}
          title="发送（Enter）"
        >
          <IconSend size={14} />
        </button>
      </div>
    </div>
  );
}

/* ── 规划步骤详情卡 ── */

/** 经理门卡片：blocked 任务的通过 / 驳回（驳回必须给原因 —— 后端会拒空原因）。 */
function TaskGate({
  task,
  onDecide,
}: {
  task: PlanTaskItem;
  onDecide: (taskId: string, decision: "approve" | "reject", reason: string) => Promise<void>;
}) {
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState<"approve" | "reject" | null>(null);
  const [error, setError] = useState<string | null>(null);
  const decide = (decision: "approve" | "reject") => {
    setBusy(decision);
    setError(null);
    onDecide(task.id, decision, reason.trim())
      .then(() => setReason(""))
      .catch((err: unknown) => setError(err instanceof Error ? err.message : String(err)))
      .finally(() => setBusy(null));
  };
  return (
    <div className="border-t border-dashed border-[var(--tree-hairline)] px-4 py-3">
      <div className="microlabel pb-1.5">经理门 · 待人审</div>
      <p className="text-[11.5px] leading-[1.7] text-[var(--tree-sub)]">
        {task.workerLabel ?? "执行者"} 已跑完并把任务交回。通过则任务置为完成；驳回会带原因退回，可再次派发。
      </p>
      <input
        className="mt-2 w-full rounded-[7px] border border-[var(--tree-line)] bg-[var(--tree-card)] px-2.5 py-1.5 text-[11.5px] text-[var(--tree-ink)] placeholder:text-[var(--tree-faint)] focus:border-[var(--tree-acc)] focus:outline-none"
        placeholder="审批意见 / 驳回原因"
        value={reason}
        onChange={(e) => setReason(e.target.value)}
      />
      <div className="mt-2 flex gap-1.5">
        <button
          className="rounded-[7px] bg-[var(--tree-acc)] px-3 py-1 text-[11.5px] font-semibold text-white disabled:opacity-50"
          disabled={busy !== null}
          onClick={() => decide("approve")}
        >
          {busy === "approve" ? "提交中…" : "通过"}
        </button>
        <button
          className="rounded-[7px] border border-[var(--tree-line)] px-3 py-1 text-[11.5px] text-[var(--tree-ink)] hover:border-salmon hover:text-salmon disabled:opacity-50"
          disabled={busy !== null || reason.trim() === ""}
          onClick={() => decide("reject")}
        >
          {busy === "reject" ? "提交中…" : "驳回"}
        </button>
      </div>
      {error && <p className="mt-1.5 text-[11px] text-salmon">{error}</p>}
    </div>
  );
}

/** 规划记录（只读回看）：物化后左树换成任务视图，规划那 5 步与它们的产物
 *  就从界面上消失了 —— 用户实测的原话是"进入执行页面后看不到规划页面的记录"。
 *  这里把 5 步的结论与**产出者**并排摊开，任何一步都能追到是谁产的。
 *
 *  数据源就是 `discovery`（工作台仍在轮询它），所以这不是另一份真相。 */
function PlanHistory({ discovery }: { discovery: DiscoveryView | null }) {
  const [open, setOpen] = useState(false);
  if (!discovery) return null;
  const a = discovery.analysis;
  const c = discovery.candidates;
  const k = discovery.classification;
  const integration = discovery.integration;
  const mat = discovery.materialization;
  const rows: Array<{ step: string; body: ReactNode }> = [
    {
      step: "需求分析",
      body: a ? (
        <>
          <span>{(a.extracted_keywords ?? []).join(" · ") || "（无关键词）"}</span>
          <ProducerLine producer={a.producer} />
        </>
      ) : (
        <span>未跑</span>
      ),
    },
    {
      step: "候选评分",
      body: c ? (
        <>
          <span>{c.items.length} 个候选 · {(c.items[0]?.repository_name ?? "—")}</span>
          <ProducerLine producer={c.producer} />
        </>
      ) : (
        <span>未跑</span>
      ),
    },
    {
      step: "分档审批",
      body: k ? (
        <span>
          必改 {k.required.length} · 可能 {k.maybe.length} · 排除 {k.excluded.length} ·{" "}
          {discovery.approval?.state === "approved" ? "已批准" : "待批准"}
        </span>
      ) : (
        <span>未跑</span>
      ),
    },
    {
      step: "生成计划",
      body: integration ? (
        <>
          <span>{integration.task_dag_count} 个任务</span>
          <ProducerLine producer={integration.producer} />
        </>
      ) : (
        <span>未跑</span>
      ),
    },
    {
      step: "物化确认",
      body: mat ? <span>{mat.status}{mat.at ? ` · ${String(mat.at).slice(0, 16)}` : ""}</span> : <span>未物化</span>,
    },
  ];
  return (
    <div className="border-t border-dashed border-[var(--tree-hairline)] px-4 py-2.5">
      <button
        className="microlabel flex w-full items-center gap-1.5 text-left hover:text-[var(--tree-acc)]"
        onClick={() => setOpen((v) => !v)}
      >
        <span>{open ? "▾" : "▸"}</span> 规划记录（只读回看）
      </button>
      {open && (
        <div className="mt-1.5 flex flex-col gap-1.5">
          {rows.map((row) => (
            <div key={row.step} className="text-[11px] leading-[1.7] text-[var(--tree-sub)]">
              <span className="text-[var(--tree-ink)]">{row.step}</span>
              <span className="pl-1.5">{row.body}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function CardShell({ title, tone = "plain", children }: { title: string; tone?: "plain" | "done" | "gate"; children: ReactNode }) {
  const toneCls =
    tone === "done" ? "text-olive" : tone === "gate" ? "text-amber" : "text-[var(--tree-acc)]";
  return (
    <div className="rounded-[9px] border border-[var(--tree-hairline)] bg-[var(--tree-card)] p-3 transition-colors hover:bg-[var(--tree-zone)]">
      <p className={`mb-1.5 flex items-center gap-1.5 text-[12px] font-medium text-[var(--tree-ink)]`}>
        {tone === "done" ? <IconCheck size={12} className={toneCls} /> : tone === "gate" ? <IconClock size={12} className={toneCls} /> : <IconRun size={12} className={toneCls} />}
        {title}
      </p>
      {children}
    </div>
  );
}

function StepDetail({
  step,
  state,
  discovery,
  onGate,
  gateBusy,
  gateError,
  onRetryStep,
  stepError,
  onForceContinue,
  policyCard,
  onConfigurePolicy,
  onRetryPolicy,
  messages,
}: {
  step: 1 | 2 | 3 | 4 | 5;
  state: StepState;
  discovery: DiscoveryView | null;
  onGate: (action: "approveTiers" | "plan" | "materialize") => void;
  gateBusy: "approveTiers" | "plan" | "materialize" | null;
  gateError: string | null;
  onRetryStep: (step: 1 | 2 | 3 | 4) => void;
  stepError: { step: number; message: string } | null;
  onForceContinue: () => void;
  policyCard: PolicyDraftState;
  onConfigurePolicy: () => void;
  onRetryPolicy: () => void;
  messages: ConversationMessage[] | null;
}) {
  const wrap = (cards: ReactNode) => (
    <div className="flex flex-col gap-3 overflow-y-auto px-4 py-3.5">
      {state === "failed" && (
        <div className="rounded-[9px] border border-salmon/40 bg-salmon-well p-3">
          <p className="text-[11.5px] text-salmon">这一步执行失败。{discovery?.analysis?.error ? String(discovery.analysis.error) : ""}</p>
          <button
            className="mt-2 rounded-[7px] border border-[var(--tree-line)] bg-[var(--tree-card)] px-3 py-1 text-[11.5px] text-[var(--tree-ink)] hover:border-[var(--tree-acc)]"
            onClick={() => onRetryStep(step as 1 | 2 | 3 | 4)}
          >
            重试这一步
          </button>
        </div>
      )}
      {/* 驱动器推进失败的原因：此前是静默吞掉 + 每 5s 重发（用户什么都看不到）。 */}
      {stepError && stepError.step === step && (
        <div className="rounded-[9px] border border-salmon/40 bg-salmon-well p-3">
          <p className="text-[11.5px] leading-[1.7] text-salmon">这一步没能推进：{stepError.message}</p>
          <p className="mt-1 text-[11px] text-[var(--tree-sub)]">改完上面的东西再点「重试这一步」；如果原因写着「分析未通过」，也可以直接忽略追问继续。</p>
        </div>
      )}
      {cards}
      {messages !== null && messages.length > 0 && step <= 2 && (
        <div className="min-h-0">
          <p className="mb-1 text-[10px] tracking-widest text-[var(--tree-faint)]">分析期对话</p>
          <MessageTimeline messages={messages.slice(0, step === 1 ? 2 : 3)} />
        </div>
      )}
    </div>
  );

  if (step === 1) {
    const a = discovery?.analysis ?? null;
    return wrap(
      a ? (
        <CardShell title="需求已解析" tone="done">
          <div className="mt-1">
            {(a.dimensions ?? []).map((d) => (
              <p key={d.name} className="flex gap-1.5 py-px text-[11px] text-[var(--tree-sub)]">
                <span className={d.covered ? "text-olive" : "text-tx3"}>{d.covered ? "✓" : "○"}</span>
                <span>
                  <span className="text-[var(--tree-ink)]">{d.name}</span>：{d.note || (d.covered ? "已说清" : "词面上没提到")}
                </span>
              </p>
            ))}
            <p className="mt-1 text-[10.5px] leading-[1.7] text-[var(--tree-faint)]">
              ○ 只表示**这段话里没有出现**该维度的常见说法，不代表你没说清——判定按词面，真实需求常被漏判。
            </p>
            <ProducerLine producer={a.producer} />
          </div>

          {/* 需求确实太短（真·信息不足）时才追问，并给「忽略追问继续」这条出路。
              此前界面只有四个 ✕、没有追问列表、也没有继续的按钮 —— 用户走到这里
              就没有下一步了（实测：整条链卡在第一步）。 */}
          {!a.sufficient && (
            <div className="mt-2 rounded-[9px] border border-amber/40 bg-amber-well px-2.5 py-2">
              <p className="text-[11.5px] text-amber">
                需求文本偏短，处理员想先问几句 —— 也可以在右栏输入框回答，或直接忽略追问继续。
              </p>
              {(a.questions ?? []).length > 0 && (
                <ul className="mt-1 list-disc pl-4 text-[11px] leading-[1.7] text-[var(--tree-sub)]">
                  {(a.questions ?? []).map((q) => (
                    <li key={q}>{q}</li>
                  ))}
                </ul>
              )}
              <button
                className="mt-1.5 rounded-[7px] border border-amber/40 bg-amber-well px-2.5 py-[3px] text-[11.5px] font-semibold text-amber hover:bg-amber-well/80"
                onClick={onForceContinue}
              >
                忽略追问，强制继续
              </button>
            </div>
          )}
        </CardShell>
      ) : (
        <CardShell title="正在分析需求">{state === "wait" ? <p className="text-[11px] text-[var(--tree-sub)]">处理员会自动开始分析；也可以稍后手动触发。</p> : <p className="text-[11px] text-[var(--tree-sub)]">分析进行中…</p>}</CardShell>
      ),
    );
  }
  if (step === 2) {
    const c = discovery?.candidates ?? null;
    return wrap(
      c ? (
        <CardShell title={`候选仓库 ${c.items.length} 个`} tone="done">
          <ProducerLine producer={c.producer} />
          {c.items.map((it) => (
            <div key={it.repository_id} className="flex items-center gap-2 py-0.5 text-[11.5px]">
              <span className="font-mono text-[11px] text-[var(--tree-ink)]">{it.repository_name}</span>
              <span className="rounded-[5px] bg-[var(--tree-zone)] px-1.5 py-px text-[10px] text-[var(--tree-sub)]">{it.score.toFixed(2)}</span>
              {it.is_entry_point && <span className="rounded-[5px] border border-[var(--tree-acc)] bg-[var(--tree-acc)]/10 px-1.5 py-px text-[10px] text-[var(--tree-acc)]">入口仓</span>}
              <span className="ml-auto min-w-0 truncate text-[10.5px] text-[var(--tree-faint)]" title={it.rationale ?? ""}>{it.rationale ?? ""}</span>
            </div>
          ))}
        </CardShell>
      ) : (
        <CardShell title="候选评分">{state === "run" ? <p className="text-[11px] text-[var(--tree-sub)]">正在评估项目仓库目录中的候选…</p> : <p className="text-[11px] text-[var(--tree-sub)]">等待需求分析完成。</p>}</CardShell>
      ),
    );
  }
  if (step === 3) {
    const cls = discovery?.classification ?? null;
    const approved = discovery?.approval?.state === "approved";
    return wrap(
      cls ? (
        <CardShell title={approved ? "分档已确认" : "分档结果 · 待人审"} tone={approved ? "done" : "gate"}>
          {(["required", "maybe", "excluded"] as const).map((tier) => (
            <div key={tier} className="flex items-center gap-2 py-0.5 text-[11.5px]">
              <span className="w-8 flex-none text-[10.5px] text-[var(--tree-faint)]">{{ required: "必需", maybe: "可能", excluded: "排除" }[tier]}</span>
              <span className="text-[var(--tree-ink)]">{cls[tier].length > 0 ? cls[tier].map((r) => r.repository).join("、") : "无"}</span>
            </div>
          ))}
          {!approved && (
            <div className="mt-2.5 flex gap-2">
              <button
                className="rounded-[7px] border border-amber/40 bg-amber-well px-3.5 py-1.5 text-[12px] font-semibold text-amber hover:bg-amber-well disabled:opacity-50"
                disabled={gateBusy === "approveTiers"}
                onClick={() => onGate("approveTiers")}
              >
                {gateBusy === "approveTiers" ? "提交中…" : "批准分档"}
              </button>
            </div>
          )}
        </CardShell>
      ) : (
        <CardShell title="分档审批">{state === "run" ? <p className="text-[11px] text-[var(--tree-sub)]">正在生成候选分档…</p> : <p className="text-[11px] text-[var(--tree-sub)]">等待候选评分完成。</p>}</CardShell>
      ),
    );
  }
  if (step === 4) {
    const integration = discovery?.integration ?? null;
    const planned = (discovery?.plan ?? null) !== null || integration !== null;
    return wrap(
      <CardShell
        title={planned ? `计划已生成${integration ? ` · ${integration.task_dag_count} 任务 ${integration.batch_count} 批` : ""}` : "生成计划"}
        tone={planned ? "done" : state === "gate" ? "gate" : "plain"}
      >
        <ProducerLine producer={integration?.producer} />
        {planned ? (
          <p className="text-[11px] leading-[1.7] text-[var(--tree-sub)]">
            批次明细的读面（计划纸面快照）在 Go 后端尚未迁移，批次划分以物化确认卡与任务树为准。
          </p>
        ) : state === "run" ? (
          <p className="text-[11px] text-[var(--tree-sub)]">正在生成计划…</p>
        ) : state === "gate" ? (
          <>
            <p className="text-[11px] leading-[1.7] text-[var(--tree-sub)]">
              分档已批准，等人确认后派发规划：仓库 Leader 产出任务 DAG 与批次划分，产物落库后回到这里确认物化。
            </p>
            <button
              className="mt-2.5 rounded-[7px] border border-amber/40 bg-amber-well px-3.5 py-1.5 text-[12px] font-semibold text-amber hover:bg-amber-well disabled:opacity-50"
              disabled={gateBusy === "plan"}
              onClick={() => onGate("plan")}
            >
              {gateBusy === "plan" ? "派发中…" : "生成计划"}
            </button>
          </>
        ) : (
          <p className="text-[11px] text-[var(--tree-sub)]">等待分档审批通过后自动生成。</p>
        )}
        {gateError && state === "gate" && (
          <p className="mt-2 rounded-[7px] border border-salmon/40 bg-salmon-well px-2.5 py-1.5 text-[11px] text-salmon">{gateError}</p>
        )}
      </CardShell>,
    );
  }
  // step 5
  const materialized = discovery?.materialization?.status === "materialized";
  return wrap(
    <>
      {/* 监管策略卡片（§3.1）：与物化按钮同级。**只在拓扑还不存在时**才有按钮
          —— 拓扑一落地档案就锁死了（§3.4），卡片态由 useIssueFlowState 合成。 */}
      {!materialized && (
        <SupervisionPolicyCard state={policyCard} onConfigure={onConfigurePolicy} onRetry={onRetryPolicy} />
      )}
      <CardShell title={materialized ? "已物化并开工" : "物化并开工"} tone={materialized ? "done" : "gate"}>
      {materialized ? (
        <p className="text-[11px] leading-[1.7] text-[var(--tree-sub)]">编制已组装、批次已下发——左侧树已换代为任务视图，点任务行查看各自房间的消息流。</p>
      ) : (
        <>
          <p className="pb-2 text-[10.5px] leading-[1.7] text-[var(--tree-faint)]">将按计划 v1 组建编制并下发批次1；确认后左侧树换代为任务视图。</p>
          <button
            className="rounded-[7px] bg-[var(--tree-acc)] px-3.5 py-1.5 text-[12px] font-semibold text-white hover:bg-[var(--tree-acc)] disabled:opacity-50"
            disabled={gateBusy === "materialize"}
            onClick={() => onGate("materialize")}
          >
            {gateBusy === "materialize" ? "物化中…" : "确认物化并开工"}
          </button>
        </>
      )}
        {gateError && <p className="mt-2 rounded-[7px] border border-salmon/40 bg-salmon-well px-2.5 py-1.5 text-[11px] text-salmon">{gateError}</p>}
      </CardShell>
    </>,
  );
}
