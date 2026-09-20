import { useEffect, useState } from "react";
import {
  type CheckpointDecisionKind,
  type HumanReviewRequestView,
  type HumanReviewStatus,
  fetchReviewRequests,
  recordCheckpointDecision,
} from "../api/reviewDesk";
import { checkpointLabel, dayLabel, errText, shortId } from "../display";
import { ErrorPanel, LoadingLine } from "../components/StatusBlocks";

/** 人工审核台（迁移 2：main 的 ReviewWorkbench 进控制台）。
 *
 *  **为什么是独立一页而不是塞进 issue 详情**：这是一条**跨 issue 的待办队列**。
 *  按 issue 拆开就没有队列了——「今天有几件事等我」正是它唯一要回答的问题。
 *
 *  **与决策夹（DecisionDeck）不是一回事**：决策夹装的是**治理决策**（交付门禁上的
 *  ready/blocked/rollback），落在某一轮交付上；这里装的是**项目检查点**
 *  （repository_scope / specification / execution / validation / delivery /
 *  exception_escalation），由拓扑的 `required_checkpoints` 定义、卡在流程节点上。
 *
 *  **看得见多少取决于你是谁**：管理员看全部，其他账号只看指派给自己的（后端
 *  `_reviews_for`）。所以队列长度因人而异，这是设计不是取数不稳。
 *
 *  **实时性**：SSE（`/review-requests/events`，2s 比对、变了才推）。流断了退回一次性
 *  取数的结果并显示提示，不静默——一个不再更新却看起来正常的待办队列比空白更危险。
 *
 *  ---- 2026-09-20 按用户裁定重排（三条都是他选的）----
 *
 *  线上实测：这一页的 10 条**全部**是发现链镜像（`origin=discovery`），而且 10 条的
 *  summary 一字不差，都是同一句 47 字的登记说明。也就是说它把"回看用的登记"塞进了
 *  待办队列：页面唯一要回答的"今天有几件事等我"被淹没成"0 条可拍板 + 10 张点了没
 *  反应的卡，每张还各印一遍同一句话"。三处改动：
 *
 *   1. **镜像移出待办**：待办只留能在这儿拍板的检查点；镜像收进下方「发现链登记」
 *      折叠区（默认收起），那段说明从"每张卡印一遍"改成"区块顶部说一次"。
 *   2. **同一个 issue 的检查点合并成一张卡**：一次发现链会登记多个步骤，一条一行会
 *      把同一件事铺成一屏。分组键取 `issue_id`，没有则退到 `project_id`。
 *   3. **卡片收成两行紧凑版**：第一行检查点 + 标题 + 状态；第二行项目/日期/证据。
 *      决策理由输入框改成点「要求修改 / 驳回」时才展开——**通过不再带理由**
 *      （这是选中项里写明的取舍）。 */

const STATUS_LABEL: Record<HumanReviewStatus, string> = {
  pending: "待审",
  approved: "已通过",
  rejected: "已驳回",
  changes_requested: "要求修改",
};

/** 状态一律走全站 .pill 族，与仓库页/项目页同一套观感（原先每种状态各写一份描边色）。 */
const STATUS_PILL: Record<HumanReviewStatus, string> = {
  pending: "pill pill-gate",
  approved: "pill pill-done",
  rejected: "pill pill-fail",
  changes_requested: "pill pill-meta",
};

const chip =
  "flex-none rounded-hard border border-line px-2.5 py-[3px] text-[11.5px] text-tx2 hover:border-amber hover:text-amber-hi disabled:opacity-50";

/** 一行检查点。合并卡里、已决列表里都用它，形状只有一种。 */
function CheckpointRow({
  review,
  onDecide,
  onOpenIssue,
}: {
  review: HumanReviewRequestView;
  onDecide: (review: HumanReviewRequestView, decision: CheckpointDecisionKind, reason: string) => Promise<void>;
  /** 跳到出处 issue（origin=discovery 的条目用）。 */
  onOpenIssue?: (issueId: string) => void;
}) {
  const [reason, setReason] = useState("");
  const [asking, setAsking] = useState<CheckpointDecisionKind | null>(null);
  const [busy, setBusy] = useState<CheckpointDecisionKind | null>(null);
  const [error, setError] = useState<string | null>(null);
  const fromDiscovery = review.origin === "discovery";
  const pending = review.status === "pending";
  // discovery 来源的待审**不在本页决策**：那是发现链的人工步骤（③ 分档审批 /
  // ⑤ 物化确认）在 issue 页面上的镜像登记，流水线在那边推进。在这里按「通过」
  // 推不动它 —— 给一个按了没反应的按钮，比不给按钮更糟。
  const decidable = pending && !fromDiscovery;

  const decide = async (kind: CheckpointDecisionKind, withReason: string) => {
    setBusy(kind);
    setError(null);
    try {
      await onDecide(review, kind, withReason);
      setReason("");
      setAsking(null);
    } catch (err) {
      // 403 没有决策权 / 409 已决或证据漂移——detail 原文，各有各的下一步
      setError(errText(err));
    } finally {
      setBusy(null);
    }
  };

  return (
    <div className="py-2">
      {/* **写死列宽、单行不换行**。此前这里是一行 `flex-wrap`：标题一长，状态/证据/
          按钮就被挤到第二行，而且每行的落点都不一样 —— 一列看下来右侧参差不齐
          （2026-09-20 用户实测："按钮后面都是参差不齐的"）。
          现在**每一列都写死宽度**（只有标题那列弹性 + truncate），连按钮列也是固定的、
          按钮在其中右对齐 —— 这样"按钮数量不同"的行也不会把前面的列挤走，五个列的 x
          在所有行里完全一致（实测：状态列、证据列、按钮右边缘逐行相同）。
          窄屏藏掉"证据"那一列（md 以下），并给按钮列一个够用的固定宽度。 */}
      <div className="grid grid-cols-[60px_minmax(0,1fr)_72px_140px] items-center gap-x-3 md:grid-cols-[68px_minmax(0,1fr)_76px_116px_176px]">
        <span className="truncate text-[11px] text-tx2" title={checkpointLabel(review.checkpoint)}>
          {checkpointLabel(review.checkpoint)}
        </span>
        <span className="min-w-0 truncate text-[12.5px] text-tx" title={review.title}>
          {review.title}
        </span>
        <span className={`${STATUS_PILL[review.status]} justify-self-start`}>{STATUS_LABEL[review.status]}</span>
        {/* 第四列按状态换事实：待审看证据版本（决策钉在它上面），已决看谁在什么时候决的。 */}
        {review.resolved_by_human_id !== null ? (
          <span
            className="hidden truncate font-mono text-[10.5px] text-tx3 md:block"
            title={`决策人 ${review.resolved_by_human_id} · ${dayLabel(review.updated_at)}`}
          >
            决策人 {shortId(review.resolved_by_human_id)}
          </span>
        ) : (
          <span
            className="hidden truncate font-mono text-[10.5px] text-tx3 md:block"
            title={`证据版本 ${review.evidence_version}${review.repository_id ? ` · 仓库 ${review.repository_id}` : ""}`}
          >
            证据 {shortId(review.evidence_version)}
          </span>
        )}
        <span className="flex items-center justify-end gap-1.5">
          {fromDiscovery && onOpenIssue && review.issue_id !== "" && (
            <button className={chip} onClick={() => onOpenIssue(review.issue_id)}>
              {pending ? "去 issue 处理" : "查看 issue"}
            </button>
          )}
          {decidable && (
            <>
              <button className={chip} disabled={busy !== null} onClick={() => void decide("approved", "")}>
                {busy === "approved" ? "提交中…" : "通过"}
              </button>
              {(["changes_requested", "rejected"] as const).map((kind) => (
                <button
                  key={kind}
                  className={chip}
                  disabled={busy !== null}
                  onClick={() => setAsking(asking === kind ? null : kind)}
                >
                  {STATUS_LABEL[kind]}
                </button>
              ))}
            </>
          )}
        </span>
      </div>

      {/* 理由只在需要时说：通过不必填，要求修改/驳回点开才展开输入框。 */}
      {asking !== null && (
        <div className="mt-2 flex flex-wrap items-center gap-2">
          <input
            className="min-w-0 flex-1 rounded-hard border border-line bg-ink px-2.5 py-1.5 text-[12px] text-tx placeholder:text-tx3 focus:border-amber focus:outline-none"
            placeholder={`${STATUS_LABEL[asking]}的理由（随决策一并记录）`}
            value={reason}
            autoFocus
            onChange={(e) => setReason(e.target.value)}
          />
          <button className={chip} disabled={busy !== null} onClick={() => void decide(asking, reason.trim())}>
            {busy === asking ? "提交中…" : `确认${STATUS_LABEL[asking]}`}
          </button>
          <button className={chip} disabled={busy !== null} onClick={() => { setAsking(null); setReason(""); }}>
            取消
          </button>
        </div>
      )}

      {error && (
        <div className="mt-2 rounded-hard border border-salmon/60 bg-salmon/10 px-2.5 py-1.5 text-[11.5px] text-salmon">
          {error}
        </div>
      )}
    </div>
  );
}

/** 同一个 issue（没有 issue_id 时退到同一个项目）的检查点合成一张卡。 */
type Group = { key: string; issueId: string; projectId: string; latest: string; rows: HumanReviewRequestView[] };

function groupByIssue(rows: HumanReviewRequestView[]): Group[] {
  const map = new Map<string, Group>();
  for (const row of rows) {
    const key = row.issue_id !== "" ? `issue:${row.issue_id}` : `project:${row.project_id}`;
    const found = map.get(key);
    if (found) {
      found.rows.push(row);
      if (row.created_at > found.latest) found.latest = row.created_at;
      continue;
    }
    map.set(key, {
      key,
      issueId: row.issue_id,
      projectId: row.project_id,
      latest: row.created_at,
      rows: [row],
    });
  }
  return [...map.values()];
}

/** 一个 issue（没有 issue_id 时是一个项目）的检查点合集，一张卡。
 *
 *  **待办与发现链登记共用它**：先前镜像区是一张张平铺的裸行，而线上 10 条全是镜像 ——
 *  于是"按 issue 分类"在真页面上一次都没显示出来（2026-09-20 用户实测）。现在两边
 *  都按同一个分组键归拢，只是卡头那句话不同（待审 / 登记）。 */
function GroupCard({
  group,
  badge,
  onDecide,
  onOpenIssue,
}: {
  group: Group;
  badge: string;
  onDecide: (review: HumanReviewRequestView, decision: CheckpointDecisionKind, reason: string) => Promise<void>;
  onOpenIssue?: (issueId: string) => void;
}) {
  return (
    <section className="rounded-hard border border-line bg-panel">
      {/* 卡头一行：谁 + 有多少 + 什么时候。下面直接接清单，用发丝线分行 —— 先前是
          "卡里再套一层带边框的小卡"，一层套一层显脏，也把那点信息挤成了两张皮。 */}
      <div className="flex flex-wrap items-baseline gap-2.5 border-b border-line px-4 py-2.5">
        <span className="text-[12.5px] text-cream">
          {group.issueId !== "" ? `Issue ${shortId(group.issueId)}` : `项目 ${shortId(group.projectId)}`}
        </span>
        <span className="text-[11px] text-tx3">{badge}</span>
        <span className="ml-auto font-mono text-[10.5px] text-tx3" title={`项目 ${group.projectId}`}>
          {dayLabel(group.latest)}
        </span>
      </div>
      <div className="divide-y divide-line px-4">
        {group.rows.map((review) => (
          <CheckpointRow key={review.id} review={review} onDecide={onDecide} onOpenIssue={onOpenIssue} />
        ))}
      </div>
    </section>
  );
}

export function ReviewDeskPage({
  rows,
  error,
  streaming,
  onRefresh,
  onToast,
  onOpenIssue,
}: {
  /** null = 尚未取到（加载中）。空数组 = 队列真的空了，两态不合并。 */
  rows: HumanReviewRequestView[] | null;
  error: string | null;
  /** SSE 是否还活着；false 时列表仍可读，只是不再自动更新 */
  streaming: boolean;
  onRefresh: () => void;
  onToast: (text: string) => void;
  /** 跳到出处 issue：discovery 来源的待审项只在那边能推进（见 CheckpointRow 注释）。 */
  onOpenIssue?: (issueId: string) => void;
}) {
  const [showResolved, setShowResolved] = useState(false);
  const [resolved, setResolved] = useState<HumanReviewRequestView[] | null>(null);
  const [resolvedError, setResolvedError] = useState<string | null>(null);
  const [mirrorsOpen, setMirrorsOpen] = useState(false);

  // 已决条目按需取：SSE 只推 pending，已决的要另外问一次全量再筛。
  useEffect(() => {
    if (!showResolved) return;
    let cancelled = false;
    setResolvedError(null);
    fetchReviewRequests()
      .then((all) => !cancelled && setResolved(all.filter((r) => r.status !== "pending")))
      .catch((err: unknown) => !cancelled && setResolvedError(errText(err)));
    return () => {
      cancelled = true;
    };
  }, [showResolved]);

  const decide = async (
    review: HumanReviewRequestView,
    decision: CheckpointDecisionKind,
    reason: string,
  ) => {
    await recordCheckpointDecision(review.project_id, {
      review_request_id: review.id,
      decision,
      reason,
    });
    onToast(`已记录决策：${review.title} → ${STATUS_LABEL[decision]}`);
    // SSE 会把这条从待办里撤走；主动刷一次是为了流断时也能收敛。
    onRefresh();
  };

  // 待办与登记分开：镜像在本页按不动，混在一起队列长度就不再是"有几件事等我"。
  const live = rows ?? [];
  const decidable = live.filter((row) => row.origin !== "discovery");
  const mirrors = live.filter((row) => row.origin === "discovery");
  const groups = groupByIssue(decidable);
  const mirrorGroups = groupByIssue(mirrors);

  return (
    <div className="max-w-[860px]">
      <div className="flex items-baseline gap-3 border-b border-line pb-3">
        <h1 className="text-[16px] font-semibold text-cream">人工审核</h1>
        {rows !== null && (
          <span className="text-[11.5px] text-tx2">
            {decidable.length} 项待审
            {mirrors.length > 0 && ` · ${mirrors.length} 条发现链登记`}
          </span>
        )}
        <button className={`ml-auto ${chip}`} onClick={() => setShowResolved((v) => !v)}>
          {showResolved ? "只看待审" : "查看已决"}
        </button>
      </div>

      {!streaming && rows !== null && (
        <p className="mt-3 rounded-hard border border-line bg-panel px-3 py-2 text-[11.5px] text-tx2">
          实时流已断开，下面是最后一次取到的结果，不会自动更新。
          <button className="ml-2 text-amber hover:text-amber-hi" onClick={onRefresh}>
            重新取一次
          </button>
        </p>
      )}

      {error ? (
        <ErrorPanel title="审核队列加载失败" message={error} onRetry={onRefresh} />
      ) : rows === null ? (
        <LoadingLine />
      ) : (
        <>
          {groups.length === 0 ? (
            <p className="mt-3 rounded-hard border border-line bg-panel px-3 py-2 text-[11.5px] text-tx3">
              没有需要你拍板的事。检查点由项目拓扑的 required_checkpoints 定义，没有受控项目时这里长期为空是正常的。
            </p>
          ) : (
            <div className="mt-3 grid gap-2">
              {groups.map((group) => (
                <GroupCard
                  key={group.key}
                  group={group}
                  badge={`${group.rows.length} 个检查点待审`}
                  onDecide={decide}
                  onOpenIssue={onOpenIssue}
                />
              ))}
            </div>
          )}

          {/* 发现链登记：本页推不动（流水线在 issue 那边），所以它不属于待办。
              那段说明从"每张卡各印一遍"改成这里说一次；展开后**同样按 issue 分组**
              —— 线上此刻的数据全在这一区，不分组等于这一页没有分类。 */}
          {mirrors.length > 0 && (
            <section className="mt-4">
              <button
                className="flex w-full flex-wrap items-center gap-2.5 rounded-hard border border-line bg-panel px-4 py-2.5 text-left hover:border-line-strong"
                onClick={() => setMirrorsOpen((v) => !v)}
                aria-expanded={mirrorsOpen}
                title={mirrorsOpen ? "收起登记区" : "展开看这些登记与出处 issue"}
              >
                <span className="text-[10px] text-tx3">{mirrorsOpen ? "▼" : "▶"}</span>
                <span className="text-[12.5px] text-tx2">发现链登记</span>
                <span className="text-[11px] text-tx3">
                  {mirrors.length} 条 · {mirrorGroups.length} 个 issue
                </span>
                <span className="min-w-0 flex-1 truncate text-[11px] text-tx3">
                  分档审批 / 物化确认在 issue 页面完成，这里只做登记与回看
                </span>
                <span className="ml-auto flex-none text-[11.5px] text-amber">{mirrorsOpen ? "收起" : "展开"}</span>
              </button>
              {mirrorsOpen && (
                <div className="mt-2 grid gap-2">
                  {mirrorGroups.map((group) => (
                    <GroupCard
                      key={group.key}
                      group={group}
                      badge={`${group.rows.length} 条登记`}
                      onDecide={decide}
                      onOpenIssue={onOpenIssue}
                    />
                  ))}
                </div>
              )}
            </section>
          )}
        </>
      )}

      {showResolved && (
        <div className="mt-6">
          <div className="eyebrow mb-1.5">已决</div>
          {resolvedError ? (
            <p className="text-[11.5px] text-salmon">{resolvedError}</p>
          ) : resolved === null ? (
            <LoadingLine />
          ) : resolved.length === 0 ? (
            <p className="text-[11.5px] text-tx3">还没有已决事项。</p>
          ) : (
            /* 已决是一条条回看用的，不再分组；但和上面一样放进一个带发丝线的面板里，
               否则裸行会散在页面上（CheckpointRow 现在自己不画边框）。 */
            <div className="divide-y divide-line rounded-hard border border-line bg-panel px-4">
              {resolved.map((review) => (
                <CheckpointRow key={review.id} review={review} onDecide={decide} onOpenIssue={onOpenIssue} />
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
