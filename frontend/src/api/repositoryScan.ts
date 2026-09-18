/** 添加仓库（乙·第 2 步）：数据源已切到 Go 后端（D 板块 scan-jobs）。
 *
 *  这里没有 replay 分支：扫描是**写外部世界**的动作，回放夹具世界里没有对应事实，
 *  编一个假任务比不做更糟（同 workspaces.ts 的取舍）。replay 下由界面如实拒绝提交。 */
import type { ScanTaskView, UrlIdentification as ContractUrlIdentification } from "./contract";
import { createScanJob, getScanJob, getUrlType } from "./repositories";

export function identifyUrl(url: string): Promise<ContractUrlIdentification> {
  return getUrlType(url).then(
    (res): ContractUrlIdentification => ({
      url: res.url,
      url_type: res.url_type,
      // Go 原始值可能是 unsupported/unknown；契约联合类型待扩展，卡片只做展示
      platform: res.platform as ContractUrlIdentification["platform"],
      repository_name: res.repository_name ?? null,
    }),
  );
}

export function startOrgScan(orgUrl: string): Promise<ScanTaskView> {
  return createScanJob({ kind: "organization", url: orgUrl }).then(toScanTask);
}

export function startRepoScan(repoUrl: string): Promise<ScanTaskView> {
  return createScanJob({ kind: "repository", url: repoUrl }).then(toScanTask);
}

export function fetchScanTask(taskId: string): Promise<ScanTaskView> {
  return getScanJob(taskId).then(toScanTask);
}

/** Go 作业出参是 camelCase（internal/scan/jobs.go），卡片组件读的是契约的
 *  snake_case 形状——在这里映射一次，页面零改动。 */
function toScanTask(job: {
  id: string;
  kind: string;
  url: string;
  status: string;
  total: number;
  scanned: number;
  lastScannedRepository?: string;
  registered: number;
  skipped: number;
  failed?: number;
  error?: string;
  startedAt: string;
  finishedAt?: string;
}): ScanTaskView {
  return {
    task_id: job.id,
    kind: job.kind === "repository" ? "repository" : "organization",
    url: job.url,
    status: job.status === "succeeded" || job.status === "failed" ? job.status : "running",
    total: job.total,
    scanned: job.scanned,
    last_scanned_repository: job.lastScannedRepository ?? null,
    registered: job.registered,
    skipped: job.skipped,
    failed: job.failed ?? 0,
    error: job.error ?? null,
    started_at: job.startedAt,
    finished_at: job.finishedAt ?? null,
  };
}

/** 会话存根键。固定前缀、与工作区无关——存的是「这个浏览器标签页正在等哪次扫描」，
 *  不是业务数据。 */
const SCAN_TASK_KEY = "repomesh.console.scan-task";

/** 存根**只存 task_id**，不镜像任何计数。计数与状态的唯一事实源是
 *  `GET /api/scan-jobs/{id}`：存 id、回来重新问一次，慢一个来回但不会说谎。 */
export function rememberScanTask(taskId: string): void {
  try {
    window.sessionStorage.setItem(SCAN_TASK_KEY, taskId);
  } catch {
    /* 隐私模式等存储不可用：功能降级为「离开页面即丢」，不影响本次轮询 */
  }
}

export function recallScanTask(): string | null {
  try {
    return window.sessionStorage.getItem(SCAN_TASK_KEY);
  } catch {
    return null;
  }
}

export function forgetScanTask(): void {
  try {
    window.sessionStorage.removeItem(SCAN_TASK_KEY);
  } catch {
    /* 同上 */
  }
}
