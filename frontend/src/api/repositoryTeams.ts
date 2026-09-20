import { ApiError, apiRequest } from "./http";

export interface RepositoryTeamMember {
  id: string;
  resource_name: string;
  display_label: string;
  runtime_phase: string;
  active_task_count: number;
}

export interface RepositoryTeamSnapshot {
  repository_id: string;
  repository_name: string;
  roster_revision: number;
  runtime_status: string;
  leader: RepositoryTeamMember;
  workers: RepositoryTeamMember[];
}

export interface RepositoryTeamCapacityChange {
  worker_count: number;
  roster_revision: number;
}

export class RepositoryTeamConflictError extends Error {
  readonly current: RepositoryTeamSnapshot;

  constructor(current: RepositoryTeamSnapshot) {
    super("The Team roster changed. Review the latest roster before trying again.");
    this.name = "RepositoryTeamConflictError";
    this.current = current;
  }
}

function detailObject(detail: unknown): Record<string, unknown> | null {
  if (typeof detail === "string") {
    try {
      const parsed: unknown = JSON.parse(detail);
      return typeof parsed === "object" && parsed !== null ? (parsed as Record<string, unknown>) : null;
    } catch {
      return null;
    }
  }
  return typeof detail === "object" && detail !== null ? (detail as Record<string, unknown>) : null;
}

function isSnapshot(value: unknown): value is RepositoryTeamSnapshot {
  if (typeof value !== "object" || value === null) return false;
  const snapshot = value as Partial<RepositoryTeamSnapshot>;
  return (
    typeof snapshot.repository_id === "string" &&
    typeof snapshot.repository_name === "string" &&
    typeof snapshot.roster_revision === "number" &&
    Array.isArray(snapshot.workers) &&
    typeof snapshot.leader === "object" &&
    snapshot.leader !== null
  );
}

export function repositoryTeamErrorCode(error: unknown): string | null {
  return error instanceof ApiError ? (detailObject(error.detail)?.error as string | undefined) ?? null : null;
}

export function repositoryTeamBusyLabels(error: unknown): string[] {
  if (!(error instanceof ApiError)) return [];
  const labels = detailObject(error.detail)?.workers;
  return Array.isArray(labels) ? labels.filter((label): label is string => typeof label === "string") : [];
}

/** 团队自 0057 起按 **(项目, 仓库)** 认：同一份仓库挂到两个项目时各有一支队，
 *  所以这三个端点必须带上项目 —— 路径里的仓库 id 无法区分"要哪个项目的队"。
 *  路径形态跟随本仓惯例（`/projects/{projectId}/…`）。 */
function teamPath(projectId: string, repositoryId: string): string {
  return `/projects/${encodeURIComponent(projectId)}/repositories/${encodeURIComponent(repositoryId)}/agent-team`;
}

export function getRepositoryTeam(projectId: string, repositoryId: string): Promise<RepositoryTeamSnapshot> {
  return apiRequest<RepositoryTeamSnapshot>("GET", teamPath(projectId, repositoryId));
}

export function createRepositoryTeam(
  projectId: string,
  repositoryId: string,
  payload: Pick<RepositoryTeamCapacityChange, "worker_count">,
): Promise<RepositoryTeamSnapshot> {
  return apiRequest<RepositoryTeamSnapshot>("POST", teamPath(projectId, repositoryId), payload);
}

export async function changeRepositoryTeam(
  projectId: string,
  repositoryId: string,
  payload: RepositoryTeamCapacityChange,
): Promise<RepositoryTeamSnapshot> {
  try {
    return await apiRequest<RepositoryTeamSnapshot>("PATCH", teamPath(projectId, repositoryId), payload);
  } catch (error) {
    if (error instanceof ApiError && error.status === 409) {
      const detail = detailObject(error.detail);
      if (detail?.error === "stale_roster" && isSnapshot(detail.current)) {
        throw new RepositoryTeamConflictError(detail.current);
      }
    }
    throw error;
  }
}
