/** 流程卡点读面（发现链域自报，观测告警面聚合）。
 *
 *  kind: failed = 某步块带错误（原因原文在 message）；
 *        stalled = 自动步该跑没跑（分档已批、计划迟迟未生成）。
 *  消费方：观测告警页的「流程卡点」区 + Manager 房间的失败卡点卡。
 *  恢复动作一律走各域既有端点（重试=发现链触发），本读面只发现不执行。 */
import { apiRequest } from "./http";

export interface DiscoveryStall {
  issueId: string;
  issueNumber: number;
  title: string;
  step: number;
  kind: "failed" | "stalled";
  message: string;
  updatedAt: string;
}

export function fetchDiscoveryStalls(projectId: string): Promise<DiscoveryStall[]> {
  return apiRequest<{ items: DiscoveryStall[] }>(
    "GET",
    `/projects/${encodeURIComponent(projectId)}/discovery-stalls`,
  ).then((page) => page.items);
}
