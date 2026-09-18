import { isUuid, object, text } from "./values";
import type { SaveFields, SaveResult } from "./modelApi";

export type SaveLocator = { kind: "provider_save"; actor: string; id: string };
export type RecoveryIndex = SaveLocator & { createdAt: string };
export type SecretFreeSaveSnapshot = SaveFields & { secretMode: "keep" | "replace" };
export type SessionRecovery = { locator: SaveLocator; snapshot: SecretFreeSaveSnapshot };
export type ReadTicket = { locator: SaveLocator; generation: number; requestSerial: number };
export type OperationState<T> =
  | { state: "in_flight" | "unconfirmed" | "checking"; locator: SaveLocator; generation: number }
  | { state: "confirmed"; locator: SaveLocator; generation: number; result: T }
  | { state: "removed"; locator: SaveLocator; generation: number }
  | { state: "reauthenticate" | "restricted"; locator: SaveLocator; generation: number };

const indexKey = "repomesh.model.save-index.v1";
const sessionPrefix = "repomesh.model.save.v1:";

function sameLocator(left: SaveLocator, right: SaveLocator): boolean {
  return left.kind === right.kind && left.actor === right.actor && left.id === right.id;
}

export function persistRecoveryIndex(index: RecoveryIndex): { saved: boolean; recoveryPath: string } {
  const recoveryPath = `/settings/model-saves/${index.id}`;
  try {
    const raw = localStorage.getItem(indexKey);
    const items: RecoveryIndex[] = raw ? JSON.parse(raw) as RecoveryIndex[] : [];
    const next = items.filter((item) => !sameLocator(item, index));
    next.push(index);
    localStorage.setItem(indexKey, JSON.stringify(next));
    return { saved: true, recoveryPath };
  } catch {
    return { saved: false, recoveryPath };
  }
}

export function persistSessionRecovery(snapshot: SessionRecovery): boolean {
  try {
    sessionStorage.setItem(sessionPrefix + snapshot.locator.actor + ":" + snapshot.locator.id, JSON.stringify(snapshot));
    return true;
  } catch {
    return false;
  }
}

export function readSessionRecovery(actor: string, id: string): SessionRecovery | null {
  try {
    const raw = sessionStorage.getItem(sessionPrefix + actor + ":" + id);
    if (raw === null) return null;
    const data = object(JSON.parse(raw));
    const locator = object(data.locator);
    if (locator.kind !== "provider_save" || locator.actor !== actor || text(locator.id, 36) !== id) return null;
    const snapshot = object(data.snapshot);
    return {
      locator: { kind: "provider_save", actor, id },
      snapshot: {
        providerId: snapshot.providerId === null ? null : text(snapshot.providerId, 128),
        expectedRevision: snapshot.expectedRevision === null ? null : text(snapshot.expectedRevision, 128),
        name: text(snapshot.name, 100),
        baseUrl: text(snapshot.baseUrl, 2048),
        apiFormat: "openai_chat_completions",
        models: Array.isArray(snapshot.models) ? snapshot.models as SaveFields["models"] : [],
        secretMode: snapshot.secretMode === "replace" ? "replace" : "keep",
      },
    };
  } catch {
    return null;
  }
}

export function readRecoveryIndex(raw: unknown, actor: string): RecoveryIndex | null {
  try {
    const data = object(raw);
    if (data.kind !== "provider_save" || data.actor !== actor || !isUuid(data.id)) return null;
    return { kind: "provider_save", actor, id: String(data.id).toLowerCase(), createdAt: text(data.createdAt, 40) };
  } catch {
    return null;
  }
}

export function beginRead(locator: SaveLocator, generation: number): ReadTicket {
  return { locator, generation, requestSerial: Date.now() };
}

export function acceptsRead(current: ReadTicket, incoming: ReadTicket, authenticatedActor: string): boolean {
  return sameLocator(current.locator, incoming.locator) && current.generation === incoming.generation && incoming.locator.actor === authenticatedActor;
}

export function reduceSave(current: OperationState<SaveResult>, ticket: ReadTicket, result: { kind: "ok"; value: SaveResult } | { kind: "error"; status: number; code: string }): OperationState<SaveResult> {
  if (ticket.locator.actor !== current.locator.actor || ticket.generation !== current.generation || !sameLocator(ticket.locator, current.locator)) return current;
  if (result.kind === "error") {
    if (result.status === 401) return { state: "reauthenticate", locator: current.locator, generation: current.generation };
    if (result.status === 403) return { state: "restricted", locator: current.locator, generation: current.generation };
    if (result.status === 410) return { state: "removed", locator: current.locator, generation: current.generation };
    if (result.status === 404 || result.status === 503 || result.code === "ABORTED" || result.code === "NETWORK_ERROR") {
      return { state: "unconfirmed", locator: current.locator, generation: current.generation };
    }
    return current;
  }
  return { state: "confirmed", locator: current.locator, generation: current.generation, result: result.value };
}

export function clearModelRecovery(): void {
  try {
    const keys: string[] = [];
    for (let index = 0; index < sessionStorage.length; index += 1) {
      const key = sessionStorage.key(index);
      if (key !== null && key.startsWith(sessionPrefix)) keys.push(key);
    }
    for (const key of keys) sessionStorage.removeItem(key);
  } catch { /* Storage may be disabled. */ }
}

export function recoveryPath(id: string): string {
  return `/settings/model-saves/${id.toLowerCase()}`;
}

export function shouldOpenSaveRecovery(response: { kind: "ok" } | { kind: "error"; status: number; code: string }): boolean {
  if (response.kind === "ok") return true;
  if (response.status === 401 || response.status === 403) return false;
  if (response.status === 404 || response.status === 503 || response.code === "ABORTED" || response.code === "NETWORK_ERROR") return true;
  return response.code === "IDEMPOTENCY_CONFLICT" || response.code === "MODEL_SAVE_CLOSED" || response.code === "MODEL_SAVE_RESULT_REMOVED" || response.code === "PROVIDER_REVISION_CONFLICT";
}
