/** 模型供应商（中转站）管理域 —— 全局配置，多个中转站并存。
 *
 *  端点唯一来源 internal/web/models.go（B04 模型供应商保存）：
 *   GET  /api/model-providers                     列表 {items:[{id,name,revision,modelCount}],nextCursor}
 *   GET  /api/model-providers/{id}                详情 {id,name,revision,baseUrl,apiFormat,secret,models[]}
 *   GET  /api/model-providers/{id}/versions/{rev} 历史版本
 *   POST /api/model-provider-saves                保存（异步信封，需 Idempotency-Key）
 *   GET  /api/model-provider-saves/{saveId}       轮询
 *   POST /api/model-provider-saves/{saveId}/close 收口
 *   POST /api/model-tests + GET /api/model-tests/{id}   连通性测试
 *
 *  凭据不在 providers 表里（表只有 id/owner/head_revision/enabled/access_epoch）——
 *  它走加密根（repomesh_secrets），所以"多中转站"不需要新建表，多存几行即可。
 *  与 `api/modelProviders.ts`（旧 Python 面的零件）刻意分开：那个文件是给
 *  /app/ 与装机向导用的，本文件是控制台管理页的通道。 */
import { apiRequest } from "./http";

export interface ProviderSummary {
  id: string;
  name: string;
  revision: string;
  modelCount: number;
}

export interface ProviderModelView {
  modelId: string;
  displayName?: string | null;
  contextWindow?: number;
  maxOutputTokens?: number;
  reasoning?: boolean;
  vision?: boolean;
  [key: string]: unknown;
}

export interface ProviderView {
  id: string;
  name: string;
  revision: string;
  baseUrl: string;
  apiFormat: string;
  secret?: Record<string, unknown>;
  models?: ProviderModelView[];
  updatedAt?: string;
  [key: string]: unknown;
}

/** GET /api/model-providers —— 中转站列表（全局）。 */
export async function listProviders(): Promise<ProviderSummary[]> {
  const raw = await apiRequest<{ items?: ProviderSummary[] } | ProviderSummary[]>(
    "GET",
    "/model-providers?limit=100",
  );
  return Array.isArray(raw) ? raw : (raw.items ?? []);
}

/** GET /api/model-providers/{id} —— 单个中转站详情（含模型清单）。 */
export function getProvider(id: string): Promise<ProviderView> {
  return apiRequest<ProviderView>("GET", `/model-providers/${encodeURIComponent(id)}`);
}

/** GET /api/model-providers/{id}/versions/{revision} —— 历史版本。 */
export function getProviderVersion(id: string, revision: string): Promise<ProviderView> {
  return apiRequest<ProviderView>(
    "GET",
    `/model-providers/${encodeURIComponent(id)}/versions/${encodeURIComponent(revision)}`,
  );
}

export interface ProviderModelInput {
  id?: string;
  modelId: string;
  displayName?: string;
  contextWindow?: number;
  maxOutputTokens?: number;
  reasoning?: boolean;
  vision?: boolean;
}

/** 保存回执：outcome=committed 带 providerId/revision，rejected 带 error。 */
export interface SaveReceipt {
  saveId?: string;
  outcome?: string;
  providerId?: string;
  providerRevision?: string;
  secretVersionId?: string;
  error?: unknown;
  [key: string]: unknown;
}

/** POST /api/model-provider-saves —— 保存中转站（新建或改）。
 *
 *  后端约束（internal/models/input.go）：
 *   · `Idempotency-Key` 必须是合法 UUID（`validUUID` 校验，否则 400）；
 *   · 体字段**白名单**（rejectUnknown）：providerId / expectedRevision / name /
 *     baseUrl / apiFormat / secret / models —— 多一个键就 422；
 *   · `secret` 是判别式对象：`{mode:"keep"}` 保留原凭据、
 *     `{mode:"replace", value:"<key>"}` 替换；新建时 providerId 缺席则**必须** replace。
 *   · 凭据不落在 providers 表里，走后端加密根（repomesh_secrets）。 */
export function saveProvider(input: {
  providerId?: string;
  expectedRevision?: string;
  name: string;
  baseUrl: string;
  apiFormat: string;
  secret: { mode: "keep" } | { mode: "replace"; value: string };
  models: ProviderModelInput[];
}): Promise<SaveReceipt> {
  return apiRequest<SaveReceipt>("POST", "/model-provider-saves", input, {
    "Idempotency-Key": crypto.randomUUID(),
  });
}

/** POST /api/model-provider-saves/{saveId}/close —— 收口一次保存。
 *  后端要求**路径键与幂等头完全相等**（NewCloseCommand：`pathKey != headerKey` → 400），
 *  所以这里把 saveId 同时当幂等键；体必须是 `{}`（其它内容 400 INVALID_JSON）。 */
export function closeSave(saveId: string): Promise<unknown> {
  return apiRequest(
    "POST",
    `/model-provider-saves/${encodeURIComponent(saveId)}/close`,
    {},
    { "Idempotency-Key": saveId },
  );
}

/** POST /api/model-tests —— 对某个**模型行**做连通性测试。
 *
 *  体形状（internal/models/destination.go 的 ReadSnapshotTarget，三个都必填）：
 *   { providerId: uuid, providerRevision: string, modelRowId: uuid }
 *  —— modelRowId 是 provider 详情里 models[].id（模型行 id），不是 modelId 字符串。
 *  测试策略（预算/时限/出网白名单）在后端锁定，前端不参与解析。 */
export function testModel(input: {
  providerId: string;
  providerRevision: string;
  modelRowId: string;
}): Promise<Record<string, unknown>> {
  return apiRequest<Record<string, unknown>>("POST", "/model-tests", input, {
    "Idempotency-Key": crypto.randomUUID(),
  });
}

/** GET /api/model-tests/{testId} —— 轮询测试结果。 */
export function getModelTest(testId: string): Promise<Record<string, unknown>> {
  return apiRequest<Record<string, unknown>>("GET", `/model-tests/${encodeURIComponent(testId)}`);
}
