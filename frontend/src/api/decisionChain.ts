import { readActiveProject } from "./activeProject";
/** 历史决策数据源：live | replay，开关沿用 `resolveDataSourceMode()`
 *  （URL `?source=live|replay` > `VITE_DATA_SOURCE` > 默认 live）。
 *
 *  live 打 decision-chain-v0.1 §6 的四个真实端点（trace / similar /
 *  semantic-search / embeddings-refresh，均要求 Bearer agent_action_token，
 *  client.ts 已带）；replay 走 data/decisionChain.ts 的演示剧本——语义检索用
 *  关键词重合度在夹具语料里算「相似」，两种入口共用同一界面。
 *
 *  **回放模式一律拒绝写**：刷新向量库在真实世界批量调 embedding API 给存量
 *  决策单建索引，夹具里没有可写的 embedding 库。 */
import type {
  DecisionChainView,
  DecisionNodeSource,
  DecisionNodeView,
  DecisionStatus,
  DecisionStep,
  EmbeddingRefreshView,
  SemanticSearchView,
  SimilarDecisionView,
  SimilarDecisionsView,
} from "./contract";
import { defaultClient } from "./client";
import { apiRequest } from "./http";
import { resolveDataSourceMode, type DataSourceMode } from "./source";
import { errText, shortId } from "../display";
import { fetchIssues } from "./issues";
import {
  DECISION_PROJECT_META,
  replaySemanticSearch,
  replaySimilar,
  replayTrace,
  searchReplayProjects,
} from "../data/decisionChain";

export function decisionChainSourceMode(): DataSourceMode {
  return resolveDataSourceMode();
}

/** §6.1 完整决策链追溯（需求定位入口）。replay 下未知项目抛错——
 *  与 live 的 404 同一语义，不拿空链冒充「该项目没有决策」。 */
export async function fetchDecisionChain(
  routingId: string,
  organizationId?: string | null,
): Promise<DecisionChainView> {
  if (resolveDataSourceMode() === "replay") {
    const chain = replayTrace(routingId);
    if (!chain) {
      throw new Error(
        `replay 夹具未覆盖项目 ${shortId(routingId)}。可选（夹具世界）：${DECISION_PROJECT_META.map((p) => shortId(p.id)).join(" / ")}`,
      );
    }
    return chain;
  }
  // live：Go 读模型按**决策单**组织链（没有项目维度的聚合端点）。入参是路由
  // 键（语义命中/决策单目录给的是决策单 id）：先取该单拿到 requirement_key，
  // 再按 key 拉同链全部节点，前端组装成 v3 追溯视图。payload_summary 等 publié
  // 存储没有的字段留空，StepCard 按约定隐藏。
  const routingIdTrimmed = routingId.trim();
  if (!routingIdTrimmed) {
    throw new Error("该决策单未关联项目且缺少决策单 id，无法追溯完整链");
  }
  const node = await fetchGoDecisionNode(routingIdTrimmed);
  if (!node.requirementKey) {
    throw new Error("该决策单没有 requirement_key（早于本读模型落库），无法聚合同链记录");
  }
  const chain = await fetchGoDecisionNodes({ requirementKey: node.requirementKey, limit: 200 });
  const nodes = chain.nodes.map(goNodeToView).sort((a, b) => a.version - b.version);
  return {
    project_id: node.projectId,
    organization_id: organizationId ?? "",
    requirement: {
      text: node.requirementText,
      plan_version: 0,
      snapshot_id: "",
    },
    nodes,
    legacy_gaps: [],
  };
}

/** Go 决策单 → 页面 v3 节点视图。payload 摘要等 public 存储没有的字段留空
 *  （渲染层按「缺失字段直接隐藏」约定处理）。 */
function goNodeToView(n: GoDecisionNode): DecisionNodeView {
  return {
    decision_id: n.id,
    event_id: n.eventId,
    project_id: n.projectId,
    organization_id: "",
    step: n.step as DecisionStep,
    version: n.version,
    status: n.status as DecisionStatus,
    actor: { type: n.actorType, agent_id: n.actorId || null },
    upstream_ref: n.parentNodeId || null,
    evidence_refs: {},
    payload_summary: {},
    affected_repository_ids: n.affectedRepositories,
    business_time: n.createdAt,
    recorded_at: n.createdAt,
    source: n.source as DecisionNodeSource,
    event_type: n.action,
  };
}

/** §6.5 相似历史（同仓 + 最近，structural；语义命中 score 非空）。
 *  live 下 semantic 缺 embedding 端点时后端回退 structural 并由 mode 如实报告。 */
export async function fetchSimilarDecisions(
  projectId: string,
  organizationId?: string | null,
  opts?: { mode?: "structural" | "semantic"; queryText?: string; topK?: number },
): Promise<SimilarDecisionsView> {
  if (resolveDataSourceMode() === "replay") {
    return replaySimilar(projectId, opts?.topK ?? 5);
  }
  // live：打 Go as-built 端点（requirement/mode/repositoryIds/topK，camelCase），
  // 命中逐条补取决策单详情解析 project_id——语义命中本身不带项目，页面点击
  // 「展开完整决策链」需要它；topK 个本地请求换真实可点，值得。
  const view = await fetchGoSimilar({
    requirement: opts?.queryText ?? "",
    repositoryIds: [],
    mode: (opts?.mode as "auto" | "semantic" | "structural") ?? "auto",
    topK: opts?.topK ?? 5,
    minSimilarity: 0,
  });
  const hits = view.hits.map(goHitToView);
  return { project_id: projectId, organization_id: organizationId ?? "", mode: view.mode, hits };
}

/** §6.5 扩展：跨组织语义检索（按文本搜历史决策）。 */
export async function searchSemanticDecisions(
  queryText: string,
  opts?: { organizationId?: string | null; topK?: number },
): Promise<SemanticSearchView> {
  if (resolveDataSourceMode() === "replay") {
    return replaySemanticSearch(queryText, opts?.topK ?? 5);
  }
  // live：Go as-built 端点用 queryText/topK（camelCase）；旧 client.ts 的
  // query_text/top_k 会被 400 拒绝。命中逐条补 project_id（同 fetchSimilarDecisions）。
  const view = await searchGoSemantic(queryText, { topK: opts?.topK ?? 5 });
  const hits = view.hits.map(goHitToView);
  return { organization_id: null, query_text: queryText, mode: "semantic", hits };
}

/** Go 语义/相似命中 → 页面通用视图。缺的字段（payload、版本、组织）如实留空，
 *  HitCard 的「缺失字段直接隐藏」约定会跳过它们。project_id 槽位放**决策单 id**
 *  作为路由键：Go 命中不带项目，live 追溯按决策单 id 取链（fetchDecisionChain）。 */
function goHitToView(h: GoSimilarHit): SimilarDecisionView {
  return {
    decision_id: h.decisionId,
    project_id: h.decisionId,
    organization_id: "",
    step: h.step as DecisionStep,
    version: 0,
    status: h.status as DecisionStatus,
    affected_repository_ids: h.matchedRepositories?.length ? h.matchedRepositories : h.affectedRepositories,
    payload_summary: {},
    business_time: h.createdAt,
    score: h.score,
    requirement_text: h.requirementText,
  };
}

/** L3 管理端点：一次批量向量化存量决策单。回放模式拒绝写（同 redispatch）。 */
export async function refreshDecisionEmbeddings(): Promise<EmbeddingRefreshView> {
  if (resolveDataSourceMode() === "replay") {
    throw new Error(
      "回放模式不写后端：刷新向量库在真实世界批量调 embedding API 建索引，夹具里没有可写的 embedding 库。加 ?source=live 后可真实刷新。",
    );
  }
  return defaultClient().refreshDecisionEmbeddings();
}

/** 统一取错文案（历史决策页 catch 分支共用）。 */
export function decisionChainErrText(err: unknown): string {
  return errText(err);
}

/** 需求定位候选（live 与 replay 统一形状；live 侧来自 issue 列表读模型）。 */
export interface DecisionProjectCandidate {
  project_id: string;
  title: string;
  /** 最新决策时间（replay 有；live 从 issue 列表拿不到决策时间 → null） */
  latest_at: string | null;
  /** live 侧 issue 的阶段文案（如「发布门禁」）；replay 无 → null */
  note: string | null;
}

/** 需求定位输入解析（live 用）：剥掉 # 后若像 UUID（带/不带连字符）→ id 直查；
 *  否则当标题关键词搜索。replay 另有 resolveReplayProjectId 兜底短前缀。 */
export function parseProjectInput(
  input: string,
): { kind: "id"; id: string } | { kind: "keyword"; keyword: string } {
  const raw = input.trim().replace(/^#/, "");
  const dashed = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
  if (dashed.test(raw)) return { kind: "id", id: raw.toLowerCase() };
  if (/^[0-9a-f]{32}$/i.test(raw)) {
    const h = raw.toLowerCase();
    return {
      kind: "id",
      id: `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`,
    };
  }
  return { kind: "keyword", keyword: raw };
}

/** 需求定位：输入标题关键词（或 replay 的短前缀），返回候选项目。
 *  - replay：决策链夹具按 id 前缀 / 标题关键词匹配；
 *  - live：issue 列表读模型（open+closed 各自第一页）客户端过滤标题/需求文本，
 *    诚实标注「基于当前加载列表」，不冒充全量。 */
export async function locateProjectCandidates(
  keyword: string,
  organizationId?: string | null,
): Promise<DecisionProjectCandidate[]> {
  const kw = keyword.trim();
  if (!kw) return [];
  if (resolveDataSourceMode() === "replay") {
    return searchReplayProjects(kw).map((c) => ({
      project_id: c.project_id,
      title: c.title,
      latest_at: c.latest_at,
      note: null,
    }));
  }
  const projectId = readActiveProject();
  if (!projectId) return [];
  const [open, closed] = await Promise.all([
    fetchIssues({ projectId, state: "open", organizationId: organizationId ?? undefined }),
    fetchIssues({ projectId, state: "closed", organizationId: organizationId ?? undefined }),
  ]);
  const needle = kw.toLowerCase();
  return [...open.issues, ...closed.issues]
    .filter(
      (i) =>
        i.title.toLowerCase().includes(needle) ||
        (i.requirement_text ?? "").toLowerCase().includes(needle),
    )
    .map((i) => ({
      project_id: i.issue_id, // issue_id 即决策链 project_id（E1 根同源）
      title: i.title,
      latest_at: null,
      note: i.phase_note,
    }));
}

/** ==================== Go 版 I 板块（api-design.md §3.I） ====================
 *  与上方 Python 契约函数的差异（前端对接必读）：
 *  - 链根是**需求文本**（requirement），{id} 是**决策节点 id**（非项目 id）；
 *  - 参数 camelCase：queryText / topK / minSimilarity / repositoryIds；
 *  - 认证是会话 cookie（apiRequest 自动带），similar 的 repositoryIds 传仓库名；
 *  - semantic-search 无降级：未配 embedding = 503、提供方失败 = 502；
 *  - similar 有结构化回退：语义失败/空结果自动降为 structural，响应 mode 如实标注。 */

export interface GoDecisionNode {
  id: string;
  eventId: string;
  requirementText: string;
  requirementKey: string;
  projectId: string;
  parentNodeId: string;
  step: string;
  version: number;
  status: string;
  actorType: string;
  actorId: string;
  action: string;
  rationale: string;
  contextRef: Record<string, unknown>;
  affectedRepositories: string[];
  source: string;
  createdAt: string;
}

export interface GoSimilarHit {
  decisionId: string;
  requirementText: string;
  step: string;
  status: string;
  actorId: string;
  affectedRepositories: string[];
  score: number;
  matchedRepositories?: string[];
  createdAt: string;
}

export interface GoNodeListFilter {
  requirementKey?: string;
  keyword?: string;
  repository?: string;
  step?: string;
  projectId?: string;
  limit?: number;
  offset?: number;
}

export function fetchGoDecisionNodes(filter: GoNodeListFilter = {}): Promise<{ nodes: GoDecisionNode[]; mode: string }> {
  const params = new URLSearchParams();
  if (filter.requirementKey) params.set("requirementKey", filter.requirementKey);
  if (filter.keyword) params.set("keyword", filter.keyword);
  if (filter.repository) params.set("repository", filter.repository);
  if (filter.step) params.set("step", filter.step);
  if (filter.projectId) params.set("projectId", filter.projectId);
  if (filter.limit !== undefined) params.set("limit", String(filter.limit));
  if (filter.offset !== undefined) params.set("offset", String(filter.offset));
  const q = params.toString();
  return apiRequest<{ nodes: GoDecisionNode[]; mode: string }>("GET", `/decision-chains${q ? `?${q}` : ""}`);
}

export function fetchGoDecisionNode(id: string): Promise<GoDecisionNode> {
  return apiRequest<GoDecisionNode>("GET", `/decision-chains/${encodeURIComponent(id)}`);
}

export interface GoSimilarQuery {
  requirement: string;
  /** 仓库名数组（不是内部 id）——结构化召回的匹配键。 */
  repositoryIds?: string[];
  topK?: number;
  minSimilarity?: number;
  mode?: "auto" | "semantic" | "structural";
}

export function fetchGoSimilar(q: GoSimilarQuery): Promise<{ mode: string; hits: GoSimilarHit[] }> {
  const params = new URLSearchParams({ requirement: q.requirement });
  if (q.repositoryIds?.length) params.set("repositoryIds", q.repositoryIds.join(","));
  if (q.topK !== undefined) params.set("topK", String(q.topK));
  if (q.minSimilarity !== undefined) params.set("minSimilarity", String(q.minSimilarity));
  if (q.mode && q.mode !== "auto") params.set("mode", q.mode);
  return apiRequest<{ mode: string; hits: GoSimilarHit[] }>("GET", `/decision-chains/similar?${params.toString()}`);
}

/** 无降级探针：503 = embedding 未配置、502 = 提供方失败（ApiError.status 可辨）。 */
export function searchGoSemantic(
  queryText: string,
  opts?: { topK?: number; minSimilarity?: number },
): Promise<{ mode: string; hits: GoSimilarHit[] }> {
  const params = new URLSearchParams({ queryText });
  if (opts?.topK !== undefined) params.set("topK", String(opts.topK));
  if (opts?.minSimilarity !== undefined) params.set("minSimilarity", String(opts.minSimilarity));
  return apiRequest<{ mode: string; hits: GoSimilarHit[] }>(
    "GET",
    `/decision-chains/semantic-search?${params.toString()}`,
  );
}

export interface GoRefreshResult {
  refreshed: number;
  failed: number;
  reason?: string;
}

export function refreshGoEmbeddings(): Promise<GoRefreshResult> {
  return apiRequest<GoRefreshResult>("POST", "/decision-chains/embeddings/refresh");
}

export interface GoFeatureToggle {
  feature: string;
  enabled: boolean;
}

export function fetchDecisionToggle(): Promise<GoFeatureToggle> {
  return apiRequest<GoFeatureToggle>("GET", "/settings/decision-chain");
}

export function putDecisionToggle(enabled: boolean): Promise<GoFeatureToggle> {
  return apiRequest<GoFeatureToggle>("PUT", "/settings/decision-chain", { enabled });
}
