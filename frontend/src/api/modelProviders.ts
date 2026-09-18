/** 模型供应商档案域（Go：B03/B04 model-providers + provider-saves，C 板块保存体）。
 *
 *  端点与形状唯一来源：`docs/current/api-design.md` as-built 与 `internal/models`
 *  types.go（json tag 原样）。与 modelTests.ts（测试/应用）同属模型域，按域拆分
 *  各占一文件。保存体后端原样校验，前端不复刻规则；保存回执带自定义序列化，
 *  形状待页面接入时以 handler 实测回填（ponytail: 先窄类型不臆测）。 */
import { apiRequest } from "./http";

const KEY_HEADER = (key: string) => ({ "Idempotency-Key": key });

/** 供应商下挂模型行（ModelView）。 */
export interface ModelView {
  id: string;
  modelProfileId: string;
  modelId: string;
  displayName: string;
  contextWindow: number;
  maxOutputTokens: number;
  reasoning: boolean;
  vision: boolean;
}

/** 供应商凭据读面（SecretView）：出参永不回显密钥本体。 */
export interface ProviderSecretView {
  configured: boolean;
  versionId: string | null;
  availability: string;
}

/** 供应商档案（ProviderView）。 */
export interface ModelProviderView {
  id: string;
  name: string;
  revision: string;
  baseUrl: string;
  apiFormat: string;
  secret: ProviderSecretView;
  models: ModelView[];
  updatedAt: string;
}

export interface ModelProviderPage {
  items: ModelProviderView[];
  nextCursor: string | null;
}

/** POST /api/model-provider-saves 请求体（SaveBody，input.go as-built）。 */
export interface ModelProviderSaveInput {
  schemaVersion: number;
  /** 新建省略；更新已有档案时带，配 expectedRevision 做乐观锁。 */
  providerId?: string;
  expectedRevision?: string;
  name: string;
  baseUrl: string;
  apiFormat: string;
  /** true = 整体替换 models 列表。 */
  replace: boolean;
  models: Array<Record<string, unknown>>;
}

export interface PageQuery {
  q?: string;
  cursor?: string;
  /** 1..100，后端缺省 50。 */
  limit?: number;
}

function pageQuery(query?: PageQuery): string {
  if (!query) return "";
  const params = new URLSearchParams();
  if (query.q) params.set("q", query.q);
  if (query.cursor) params.set("cursor", query.cursor);
  if (query.limit !== undefined) params.set("limit", String(query.limit));
  const qs = params.toString();
  return qs ? `?${qs}` : "";
}

/** GET /api/model-providers — 供应商档案目录。 */
export function listModelProviders(query?: PageQuery): Promise<ModelProviderPage> {
  return apiRequest<ModelProviderPage>("GET", `/model-providers${pageQuery(query)}`);
}

/** GET /api/model-providers/{id} — 供应商档案当前版。 */
export function getModelProvider(id: string): Promise<ModelProviderView> {
  return apiRequest<ModelProviderView>("GET", `/model-providers/${encodeURIComponent(id)}`);
}

/** GET /api/model-providers/{id}/versions/{revision} — 指定版本（审计/回看）。 */
export function getModelProviderVersion(id: string, revision: string): Promise<ModelProviderView> {
  return apiRequest<ModelProviderView>(
    "GET",
    `/model-providers/${encodeURIComponent(id)}/versions/${encodeURIComponent(revision)}`,
  );
}

/** POST /api/model-provider-saves — 两阶段保存的发起（幂等，回执含 saveId）。
 *  回执形状经自定义序列化（SaveReceipt.MarshalJSON），待页面接入时回填精确字段。 */
export function createModelProviderSave(
  input: ModelProviderSaveInput,
  idempotencyKey: string,
): Promise<{ saveId: string } & Record<string, unknown>> {
  return apiRequest("POST", "/model-provider-saves", input, KEY_HEADER(idempotencyKey));
}

/** GET /api/model-provider-saves/{saveId} — 保存操作回执查询。 */
export function getModelProviderSave(
  saveId: string,
): Promise<{ saveId: string } & Record<string, unknown>> {
  return apiRequest("GET", `/model-provider-saves/${encodeURIComponent(saveId)}`);
}

/** POST /api/model-provider-saves/{saveId}/close — 关闭保存操作（收尾/放弃）。 */
export function closeModelProviderSave(
  saveId: string,
  body: Record<string, unknown>,
  idempotencyKey: string,
): Promise<Record<string, unknown>> {
  return apiRequest(
    "POST",
    `/model-provider-saves/${encodeURIComponent(saveId)}/close`,
    body,
    KEY_HEADER(idempotencyKey),
  );
}
