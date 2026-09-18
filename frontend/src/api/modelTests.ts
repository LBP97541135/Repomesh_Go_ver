/** 模型测试与应用域（Go：B05 测试 + B04 扩展 应用）。
 *
 *  请求体是**模型配置快照**（保存体同款形状，字段以 `internal/models`
 *  test_types.go 与 B05/B04 采用记录为准——后端原样校验，前端不复刻规则）。
 *  测试提交为 202 异步：`submitModelTest` 后用 `getModelTest` 轮询。 */
import { apiRequest } from "./http";

/** 模型配置快照（C 板块保存体同款；精确字段见 internal/models）。 */
export type ModelSnapshot = Record<string, unknown>;

const KEY_HEADER = (key: string) => ({ "Idempotency-Key": key });

/** POST /api/model-test-previews — 测试预览（不入库，同步返回）。 */
export function previewModelTest(snapshot: ModelSnapshot): Promise<unknown> {
  return apiRequest<unknown>("POST", "/model-test-previews", snapshot);
}

/** POST /api/model-tests — 提交测试（202 + 测试对象，用 id 轮询）。 */
export function submitModelTest(snapshot: ModelSnapshot, idempotencyKey: string): Promise<unknown> {
  return apiRequest<unknown>("POST", "/model-tests", snapshot, KEY_HEADER(idempotencyKey));
}

/** GET /api/model-tests/{testId} — 测试结果/状态。 */
export function getModelTest(testId: string): Promise<unknown> {
  return apiRequest<unknown>("GET", `/model-tests/${encodeURIComponent(testId)}`);
}

/** POST /api/projects/{projectId}/model-application-previews — 应用预览（不入库）。 */
export function previewModelApplication(projectId: string, snapshot: ModelSnapshot): Promise<unknown> {
  return apiRequest<unknown>(
    "POST",
    `/projects/${encodeURIComponent(projectId)}/model-application-previews`,
    snapshot,
  );
}

/** POST /api/projects/{projectId}/model-applications — 应用到项目（200）。 */
export function applyModel(
  projectId: string,
  snapshot: ModelSnapshot,
  idempotencyKey: string,
): Promise<unknown> {
  return apiRequest<unknown>(
    "POST",
    `/projects/${encodeURIComponent(projectId)}/model-applications`,
    snapshot,
    KEY_HEADER(idempotencyKey),
  );
}

/** GET /api/projects/{projectId}/model-applications/{applicationId} — 应用结果读取。 */
export function getModelApplication(projectId: string, applicationId: string): Promise<unknown> {
  return apiRequest<unknown>(
    "GET",
    `/projects/${encodeURIComponent(projectId)}/model-applications/${encodeURIComponent(applicationId)}`,
  );
}
