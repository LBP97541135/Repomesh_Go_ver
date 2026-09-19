/** 测试团队的记录读面：`GET /api/issues/{issueId}/tests`（Go as-built）。
 *
 *  三种记录一次读回：
 *   · task_single_point     —— 每条任务的单点验收（脚本 / 命令 / 退出码 / 结论）
 *   · repo_integration      —— 一个 DAG 节点（一个仓库）完成后的本仓库集成验证
 *   · cross_repo_regression —— 跨仓库联调 + 回归（计划跨仓库时才有）
 *
 *  没有记录就是空列表 —— 界面显示「还没有记录」，不编造通过。 */
import { apiRequest } from "./http";

export type TestEvidenceKind =
  | "task_single_point"
  | "repo_integration"
  | "cross_repo_regression";

export interface TestEvidenceItem {
  kind: TestEvidenceKind;
  task_id?: string;
  repository_id?: string;
  script?: string;
  command?: string;
  exit_code?: number;
  passed: boolean;
  summary?: string;
  producer?: string;
  created_at: string;
}

export interface TestEvidenceView {
  issue_id: string;
  items: TestEvidenceItem[];
  passed: number;
  failed: number;
}

export function fetchTestEvidence(issueId: string): Promise<TestEvidenceView> {
  return apiRequest<TestEvidenceView>(
    "GET",
    `/issues/${encodeURIComponent(issueId)}/tests`,
  );
}
