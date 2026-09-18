/** issue 列表数据源：live | replay，开关沿用 `resolveDataSourceMode()`
 *  （URL `?source=live|replay` > `VITE_DATA_SOURCE` > 默认 replay）。
 *
 *  live 打契约 v0.2 §2 的 `GET /issues`；replay 走本地夹具。两侧返回**同一个契约类型**，
 *  页面无分支。 */
import type { IssueListResponse, ParsedDocumentView } from "./contract";
import { defaultClient } from "./client";
import { createIssueCreation, creationOptions } from "./projectIssues";

/** 标题 = 需求首个非空行,截 200(契约 §3 title 上限)。 */
function firstLine(text: string): string {
  const line = text.split("\n").find((l) => l.trim().length > 0);
  return (line ?? "需求").trim().slice(0, 200);
}
import { createProject } from "./projects";
import { listRepositoryCandidates } from "./repositories";
import { resolveDataSourceMode, type DataSourceMode } from "./source";
import { issuesFixture } from "../data/issues";

/** 单页条数。联调种子仅四条，取 20 足够；真实规模下由 next_cursor 续读。 */
export const ISSUES_PAGE_LIMIT = 20;

/** 47 表模型一仓一项目；控制台还没有项目选择器——默认解析取第一个可读项目
 *  （GET /api/projects → {items:[...]}, as-built）。ponytail: 单项目兜底；
 *  多项目选择器等 UI 需求出现再加。 */
export async function resolveProjectId(): Promise<string | null> {
  const res = await fetch("/api/projects", { credentials: "same-origin" });
  if (!res.ok) return null;
  const body = (await res.json()) as { items?: Array<{ id?: string }> };
  return body.items?.[0]?.id ?? null;
}

export interface IssuesQuery {
  state: "open" | "closed";
  /** 可选覆盖：默认内部解析（resolveProjectId）。 */
  projectId?: string;
  /** Q2：工作区由前端持有并传参，服务端不猜。未选工作区时不传 = 全部。 */
  organizationId?: string;
  cursor?: string;
  /** v0.5：默认视图（与两个计数）排除已归档 issue；开关打开时置 true。 */
  includeArchived?: boolean;
}

function replayPage(q: IssuesQuery): IssueListResponse {
  const all = issuesFixture.issues;
  return {
    issues: all.filter((i) => i.state === q.state),
    open_count: all.filter((i) => i.state === "open").length,
    closed_count: all.filter((i) => i.state === "closed").length,
    // 夹具即全量，没有第二页——不给一个点了没反应的「加载更多」
    next_cursor: null,
  };
}

export function issuesSourceMode(): DataSourceMode {
  return resolveDataSourceMode();
}

/** 创建 issue——走 Go B06 异步建项流程 `POST /api/projects/{projectId}/issue-creations`
 *  （同步出票：回执直接带 issue.id；as-built 见 writeIssueCreation 信封）。
 *  仅 live 模式——replay 是回放夹具，「模拟创建」会篡改夹具世界，调用方在弹窗层挡掉。
 *
 *  旧 v0.3 的「处理者 = Org Leader + organization_id 交叉校验」随创建契约 v1 作废：
 *  Go 后端按会话主体与项目作用域裁权，前端不再传处理者。organizationId 参数保留
 *  仅为调用方签名不变，已无线上语义。
 *
 *  幂等键由**调用方**持有并传入（A2 修正）：「每次逻辑创建一个新键、**重试沿用同键**」
 *  ——键在这里现取的话，失败重试就是新键，超时后会创建两个 issue。
 *
 *  返回只承诺 issue_id：Go 读模型（id/number/title/repositoryIds/...）与旧 v0.2
 *  视图（phase/round_count/...）是两种读模型，派生字段前端禁止另行映射（契约 §2）；
 *  调用方创建后统一走 fetchIssues 刷新列表。 */
export async function createIssue(
  requirementText: string,
  idempotencyKey: string,
  _documentFilename: string | null,
): Promise<CreatedIssueRef> {
  // 建issue即建项（Go 语义：issue 挂项目、项目挂仓库）。无项目时自动走
  // 「发现仓库 → 建项」：仓库取候选第一位（发现仓库段的读模型），发现/物化
  // 轮再补齐依赖仓库。无候选时如实报错引导授权/登记。
  let projectId = await resolveProjectId();
  if (!projectId) {
    const candidates = await listRepositoryCandidates();
    const repoId = candidates.items[0]?.id;
    if (!repoId) {
      throw new Error(
        `候选仓库为空——发现批次的上游抓取暂未成功（GitHub 访问超时/限流），稍候重试即可；已返回 ${candidates.items.length} 条。`,
      );
    }
    const created = await createProject(
      {
        name: requirementText.trim().slice(0, 60) || "新需求",
        purpose: requirementText,
        repositoryIds: [repoId],
      },
      idempotencyKey,
    );
    projectId = created.projectId;
  }
  // 契约 §3:expectedCreationContextRevision 必填,来自创建条件查询
  // (GET issue-creation-options,Go 视图默认帕斯卡字段 CreationContextRevision);
  // 不匹配 = 409 CREATION_CONTEXT_CHANGED。
  const options = await creationOptions(projectId) as {
    creationContextRevision?: string;
    canSubmit?: boolean;
    blockingReasons?: string[];
    repositories?: Array<{ repositoryId?: string; selectable?: boolean; reasons?: string[] }>;
    /** 兼容旧无 tag 帕斯卡线格式(后端 8e1f863 之前) */
    CreationContextRevision?: string;
    CanSubmit?: boolean;
    BlockingReasons?: string[];
    Repositories?: Array<{ repositoryId?: string; RepositoryID?: string; selectable?: boolean }>;
  };
  const revision = options.creationContextRevision ?? options.CreationContextRevision;
  const repos = options.repositories ?? options.Repositories ?? [];
  const canSubmit = options.canSubmit ?? options.CanSubmit;
  if (canSubmit === false) {
    throw new Error(
      `当前不满足创建条件:${(options.blockingReasons ?? options.BlockingReasons ?? []).join("、") || "无可选仓库"}`,
    );
  }
  const repositoryIds = repos
    .map((r) => r.repositoryId ?? ((r as { RepositoryID?: string }).RepositoryID))
    .filter((id, i): id is string => !!id && repos[i].selectable !== false);
  if (repositoryIds.length === 0) {
    const detail = repos
      .map((r) => {
        const reasons = (r as { reasons?: string[] }).reasons;
        return `${r.repositoryId ?? "?"}(selectable=${r.selectable},理由=${(reasons ?? []).join("/") || "无"})`;
      })
      .join(";");
    throw new Error(`无可选仓库——创建条件返回 ${repos.length} 个仓库均不可选:${detail || "空清单"}`);
  }
  const receipt = await createIssueCreation(
    projectId,
    {
      expectedCreationContextRevision: revision,
      conversation: { mode: "new" },
      repositoryIds,
      title: firstLine(requirementText),
      description: requirementText,
    },
    idempotencyKey,
  );
  return { issue_id: receipt.issue.id };
}

/** createIssue 的返回契约：只承诺 id（其余读模型字段由列表刷新提供）。 */
export interface CreatedIssueRef {
  issue_id: string;
}

export async function fetchIssues(q: IssuesQuery): Promise<IssueListResponse> {
  if (issuesSourceMode() === "replay") return replayPage(q);

  const projectId = q.projectId ?? (await resolveProjectId());
  if (!projectId) {
    // 没有任何可读项目：诚实空态（组织/项目批次未接时也走这里）
    return { issues: [], open_count: 0, closed_count: 0, next_cursor: null };
  }
  return defaultClient().listIssues({
    projectId,
    state: q.state,
    organizationId: q.organizationId,
    cursor: q.cursor,
    limit: ISSUES_PAGE_LIMIT,
    includeArchived: q.includeArchived,
  });
}

/** v0.5 §1：归档 issue（墓碑语义，不删除；幂等，重复归档返回同一 archived_at）。
 *  replay 夹具不可篡改（createIssue 同一条红线），调用方在页面层挡掉。 */
export async function archiveIssue(issueId: string): Promise<void> {
  await defaultClient().archiveIssue(issueId);
}

export interface IssuePurgeReceipt {
  snapshots: number;
  decision_chain_nodes: number;
  audit_events: number;
}

/** 彻底清除（2026-09-08 用户裁决）：**不可逆**——硬删除已归档 issue 的快照、
 *  决策链与审计事件（保留一条 IssuePurged 审计）。仅对已归档 issue 可用
 *  （后端 409 兜底）；replay 模式由调用方在页面层挡掉。 */
export async function purgeIssue(issueId: string): Promise<IssuePurgeReceipt> {
  return defaultClient().purgeIssue(issueId);
}

/** 需求文档真实上传（与 createIssue 同源鉴权）：把 .txt/.md/.docx/.pdf/.odt/.rtf
 *  解析成纯文本，由弹窗填入需求区继续编辑。回放模式同样可用——解析只读后端，
 *  不写任何数据，不篡改夹具世界。 */
export async function parseRequirementDocument(file: File): Promise<ParsedDocumentView> {
  return defaultClient().parseIssueDocument(file);
}
