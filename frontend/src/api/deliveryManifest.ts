/** 跨仓交付的**一致版本清单**（评委建议②）：`internal/web/delivery_manifest_routes.go`。
 *
 *  读面回答"这一次交付到底包含哪些版本"：需求、每个仓库的提交/分支/PR、数据库迁移版本
 *  与数据基线、分支与验证结论、测试证据，以及**失败发生在哪个仓库、哪个阶段**。
 *  建面落的是**快照**（幂等键必填）：同键重放返回原快照，换键重跑落新的一份，旧的
 *  那份原样留着 —— 失败尝试不丢。 */
import { apiRequest } from "./http";

export interface ManifestEvidenceView {
  kind: string;
  passed: boolean;
  summary: string;
}

export interface ManifestEntryView {
  repositoryId: string;
  repositoryName: string;
  commitSha?: string;
  branchRef?: string;
  pullRequestUrl?: string;
  migrations: string[];
  databaseBaseline?: string;
  databaseBranch?: string;
  databaseProvider?: string;
  validationStatus: string;
  failureStage?: string;
  failureDetail?: string;
  testEvidence: ManifestEvidenceView[];
}

export interface DeliveryManifestView {
  id: string;
  projectId: string;
  issueId: string;
  planId?: string;
  planVersion: string;
  requirementText: string;
  /** consistent | inconsistent | failed */
  status: string;
  failureSummary?: string;
  createdAt: string;
  entries: ManifestEntryView[];
}

/** GET /api/projects/{pid}/issues/{iid}/delivery-manifest — 最近一份清单。
 *  404 = 还没有清单（不是错误）。 */
export function getLatestDeliveryManifest(
  projectId: string,
  issueId: string,
): Promise<DeliveryManifestView> {
  return apiRequest<DeliveryManifestView>(
    "GET",
    `/projects/${encodeURIComponent(projectId)}/issues/${encodeURIComponent(issueId)}/delivery-manifest`,
  );
}

/** POST /api/projects/{pid}/issues/{iid}/delivery-manifest — 建一份快照（幂等键必填）。 */
export function buildDeliveryManifest(
  projectId: string,
  issueId: string,
  input: { planId?: string; idempotencyKey: string },
): Promise<DeliveryManifestView> {
  return apiRequest<DeliveryManifestView>(
    "POST",
    `/projects/${encodeURIComponent(projectId)}/issues/${encodeURIComponent(issueId)}/delivery-manifest`,
    { planId: input.planId ?? "", idempotencyKey: input.idempotencyKey },
  );
}
