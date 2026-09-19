import type { Attempt, Purpose, Session } from "./api";
import { isSafeSegment, isUuid, normalizeProjectOperationKey, object } from "./values";

export type Destination =
  | { kind: "home" }
  | { kind: "project"; projectId: string }
  | { kind: "operation"; operationKind: "project_create"; operationId: string }
  | { kind: "operation"; operationKind: "project_update"; projectId: string; operationId: string }
  | { kind: "operation"; operationKind: "provider_save"; operationId: string };

export type AppRoute =
  | { kind: "home" }
  | { kind: "login" }
  | { kind: "result"; id: string }
  | { kind: "projects" }
  | { kind: "project-create" }
  | { kind: "project"; projectId: string }
  | { kind: "project-settings"; projectId: string }
  | { kind: "project-creation-result"; key: string }
  | { kind: "project-update-result"; projectId: string; key: string }
  | { kind: "model-settings" }
  | { kind: "model-save-result"; key: string }
  | { kind: "not-found" };

export type BusinessNextPage = "/" | `/projects/${string}` | `/project-creations/${string}` | `/settings/model-saves/${string}`;
export type ProjectAwareAttempt = Omit<Attempt, "nextPage"> & { nextPage: BusinessNextPage | null };

export type SavedAuthorization = {
  attemptId: string;
  purpose: Purpose;
  createdAt: string;
  destination: Destination;
};

const loginDestinationKey = "repomesh.auth.destination";
function exactSegments(path: string): string[] | null {
  if (!path.startsWith("/") || path.includes("?") || path.includes("#") || path.includes("%") || path.includes("\\")) return null;
  const segments = path.slice(1).split("/");
  return segments.some((segment) => segment.length === 0) ? null : segments;
}

const APP_BASE = "/app";

function stripAppBase(path: string): string {
  return path.startsWith(APP_BASE) && (path.length === APP_BASE.length || path[APP_BASE.length] === "/") ? path.slice(APP_BASE.length) || "/" : path;
}

export function parseRoute(rawPath: string): AppRoute {
  const path = stripAppBase(rawPath);
  if (path === "/") return { kind: "home" };
  if (path === "/login") return { kind: "login" };
  if (path === "/projects") return { kind: "projects" };
  if (path === "/projects/new") return { kind: "project-create" };
  if (path === "/settings/models") return { kind: "model-settings" };
  const segments = exactSegments(path);
  if (segments === null) return { kind: "not-found" };
  if (segments.length === 3 && segments[0] === "auth" && segments[1] === "result" && isUuid(segments[2])) return { kind: "result", id: segments[2] };
  if (segments.length === 2 && segments[0] === "project-creations") {
    const key = normalizeProjectOperationKey(segments[1]);
    if (key !== null) return { kind: "project-creation-result", key };
  }
  if (segments.length === 2 && segments[0] === "settings" && segments[1] === "models") return { kind: "model-settings" };
  if (segments.length === 3 && segments[0] === "settings" && segments[1] === "model-saves") {
    const key = normalizeProjectOperationKey(segments[2]);
    if (key !== null) return { kind: "model-save-result", key };
  }
  if (segments[0] !== "projects" || !isSafeSegment(segments[1] ?? "")) return { kind: "not-found" };
  const projectId = segments[1];
  if (segments.length === 2) return { kind: "project", projectId };
  if (segments.length === 3 && segments[2] === "settings") return { kind: "project-settings", projectId };
  if (segments.length === 4 && segments[2] === "updates") {
    const key = normalizeProjectOperationKey(segments[3]);
    if (key !== null) return { kind: "project-update-result", projectId, key };
  }
  return { kind: "not-found" };
}

export function parseBusinessNextPage(value: unknown): BusinessNextPage | null {
  if (value === null) return null;
  if (typeof value !== "string") throw new Error("Invalid next page");
  const route = parseRoute(value);
  switch (route.kind) {
    case "home": return "/";
    case "project": return `/projects/${route.projectId}`;
    case "project-creation-result": return `/project-creations/${route.key}`;
    case "project-update-result": return `/projects/${route.projectId}/updates/${route.key}`;
    case "model-save-result": return `/settings/model-saves/${route.key}`;
    default:
      throw new Error("Invalid next page");
  }
}

export function destinationForRoute(route: AppRoute): Destination {
  switch (route.kind) {
    case "project":
    case "project-settings": return { kind: "project", projectId: route.projectId };
    case "project-creation-result": return { kind: "operation", operationKind: "project_create", operationId: route.key };
    case "project-update-result": return { kind: "operation", operationKind: "project_update", projectId: route.projectId, operationId: route.key };
    case "model-settings": return { kind: "home" };
    case "model-save-result": return { kind: "operation", operationKind: "provider_save", operationId: route.key };
    default: return { kind: "home" };
  }
}

export function parseDestination(value: unknown): Destination | null {
  let data: Record<string, unknown>;
  try {
    data = object(value);
  } catch {
    return null;
  }
  if (data.kind === "home" && Object.keys(data).length === 1) return { kind: "home" };
  if (data.kind === "project" && Object.keys(data).length === 2 && typeof data.projectId === "string" && isSafeSegment(data.projectId)) return { kind: "project", projectId: data.projectId };
  if (data.kind !== "operation" || typeof data.operationId !== "string") return null;
  const operationId = normalizeProjectOperationKey(data.operationId);
  if (operationId === null) return null;
  if (data.operationKind === "project_create" && Object.keys(data).length === 3) return { kind: "operation", operationKind: "project_create", operationId };
  if (data.operationKind === "provider_save" && Object.keys(data).length === 3) return { kind: "operation", operationKind: "provider_save", operationId };
  if (data.operationKind === "project_update" && Object.keys(data).length === 4 && typeof data.projectId === "string" && isSafeSegment(data.projectId)) return { kind: "operation", operationKind: "project_update", projectId: data.projectId, operationId };
  return null;
}

export function rememberLoginDestination(destination: Destination): boolean {
  try {
    sessionStorage.setItem(loginDestinationKey, JSON.stringify(destination));
    return parseDestination(JSON.parse(sessionStorage.getItem(loginDestinationKey) ?? "null")) !== null;
  } catch {
    return false;
  }
}

export function readLoginDestination(): Destination {
  try {
    return parseDestination(JSON.parse(sessionStorage.getItem(loginDestinationKey) ?? "null")) ?? { kind: "home" };
  } catch {
    return { kind: "home" };
  }
}

export function clearLoginDestination(): void {
  try { sessionStorage.removeItem(loginDestinationKey); } catch { /* Browser storage may be unavailable. */ }
}

export function canContinueToDestination(attempt: ProjectAwareAttempt, session: Session | null): boolean {
  return session !== null && attempt.state === "confirmed" && attempt.nextPage !== null && (attempt.purpose === "login" || attempt.connection !== null);
}
