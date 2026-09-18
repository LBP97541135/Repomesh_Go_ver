import type { ConfigurationSelection, ProjectCreateInput, ProjectUpdateInput } from "./projectApi";
import type { ProjectCreationReceipt, ProjectUpdateReceipt } from "./projectApi";
import { parseConfigurationSelection } from "./projectApi";
import { isSafeSegment, normalizeProjectOperationKey, object, scalarLength, timestamp } from "./values";

export type CreateIdentity = { readonly actor: string; readonly kind: "project_create"; readonly key: string; readonly projectId?: never };
export type UpdateIdentity = { readonly actor: string; readonly kind: "project_update"; readonly key: string; readonly projectId: string };
export type OperationIdentity = CreateIdentity | UpdateIdentity;
export type CreateOperation = CreateIdentity & { readonly input: ProjectCreateInput };
export type UpdateOperation = UpdateIdentity & { readonly input: ProjectUpdateInput };
export type ProjectOperation = CreateOperation | UpdateOperation;
export type OperationIndex = OperationIdentity & { readonly recordedAt: string };
export type StoredCreateOperation = CreateOperation & { readonly schemaVersion: 1; readonly recordedAt: string };
export type StoredUpdateOperation = UpdateOperation & { readonly schemaVersion: 1; readonly recordedAt: string };
export type StoredOperation = StoredCreateOperation | StoredUpdateOperation;
export type RecoverableOperation =
  | { kind: "project_create"; identity: CreateIdentity; original: StoredCreateOperation | null }
  | { kind: "project_update"; identity: UpdateIdentity; original: StoredUpdateOperation | null };
export type CommittedOperation =
  | { kind: "project_create"; identity: CreateIdentity; receipt: ProjectCreationReceipt }
  | { kind: "project_update"; identity: UpdateIdentity; receipt: ProjectUpdateReceipt };
export type PreparedOperation =
  | { kind: "project_create"; operation: StoredCreateOperation }
  | { kind: "project_update"; operation: StoredUpdateOperation };
export type StoragePreparation =
  | { kind: "stored"; prepared: PreparedOperation; recoveryPath: string }
  | { kind: "link-required"; prepared: PreparedOperation; recoveryPath: string };
export type OperationState =
  | { kind: "idle" }
  | { kind: "ready"; preparation: StoragePreparation }
  | { kind: "sending"; operation: PreparedOperation }
  | { kind: "unknown"; operation: RecoverableOperation; error: import("./api").ApiError | null }
  | { kind: "committed"; result: CommittedOperation }
  | { kind: "removed"; identity: OperationIdentity }
  | { kind: "rejected"; operation: PreparedOperation; error: import("./api").ApiError }
  | { kind: "inaccessible" };

const inputPrefix = "repomesh.project.operation.v1:";
const indexKey = "repomesh.project.operation-index.v1";
function validText(value: unknown, maximum: number, allowWhitespace = true): value is string {
  if (typeof value !== "string") return false;
  const length = scalarLength(value);
  return length !== null && length <= maximum && (allowWhitespace || value.trim().length > 0);
}

function validIdentityText(value: unknown): value is string {
  return typeof value === "string" && isSafeSegment(value);
}

function repositoryIds(value: unknown, emptyAllowed: boolean): string[] | null {
  if (!Array.isArray(value) || value.length > 100 || (!emptyAllowed && value.length === 0)) return null;
  const ids: string[] = [];
  const seen = new Set<string>();
  for (const item of value) {
    if (!validIdentityText(item) || seen.has(item)) return null;
    seen.add(item);
    ids.push(item);
  }
  return ids;
}

function parseCreateInput(value: unknown): ProjectCreateInput | null {
  let data: Record<string, unknown>;
  try { data = object(value); } catch { return null; }
  if (!validText(data.name, 200, false) || !validText(data.purpose, 20000, false)) return null;
  const ids = repositoryIds(data.repositoryIds, false);
  if (ids === null) return null;
  const keys = Object.keys(data);
  if (keys.some((key) => !["name", "purpose", "repositoryIds", "configuration"].includes(key))) return null;
  if (data.configuration === undefined) return { name: data.name, purpose: data.purpose, repositoryIds: ids };
  try {
    return { name: data.name, purpose: data.purpose, repositoryIds: ids, configuration: parseConfigurationSelection(data.configuration) };
  } catch {
    return null;
  }
}

function parseUpdateInput(value: unknown): ProjectUpdateInput | null {
  let data: Record<string, unknown>;
  try { data = object(value); } catch { return null; }
  if (!validText(data.expectedProjectRevision, 128, false)) return null;
  if (Object.keys(data).some((key) => !["expectedProjectRevision", "name", "purpose", "repositoryIdsToAdd", "configuration"].includes(key))) return null;
  const fields: { name?: string; purpose?: string; repositoryIdsToAdd?: string[]; configuration?: ConfigurationSelection } = {};
  if (data.name !== undefined) {
    if (!validText(data.name, 200, false)) return null;
    fields.name = data.name;
  }
  if (data.purpose !== undefined) {
    if (!validText(data.purpose, 20000, false)) return null;
    fields.purpose = data.purpose;
  }
  if (data.repositoryIdsToAdd !== undefined) {
    const ids = repositoryIds(data.repositoryIdsToAdd, true);
    if (ids === null) return null;
    fields.repositoryIdsToAdd = ids;
  }
  if (data.configuration !== undefined) {
    try { fields.configuration = parseConfigurationSelection(data.configuration); } catch { return null; }
  }
  if (fields.name !== undefined) return { expectedProjectRevision: data.expectedProjectRevision, ...fields, name: fields.name };
  if (fields.purpose !== undefined) return { expectedProjectRevision: data.expectedProjectRevision, ...fields, purpose: fields.purpose };
  if (fields.repositoryIdsToAdd !== undefined) return { expectedProjectRevision: data.expectedProjectRevision, ...fields, repositoryIdsToAdd: fields.repositoryIdsToAdd };
  if (fields.configuration !== undefined) return { expectedProjectRevision: data.expectedProjectRevision, ...fields, configuration: fields.configuration };
  return null;
}

function identity(value: Record<string, unknown>): OperationIdentity | null {
  if (!validIdentityText(value.actor) || typeof value.key !== "string") return null;
  const key = normalizeProjectOperationKey(value.key);
  if (key === null) return null;
  if (value.kind === "project_create" && value.projectId === undefined) return { actor: value.actor, kind: "project_create", key };
  if (value.kind === "project_update" && validIdentityText(value.projectId)) return { actor: value.actor, kind: "project_update", key, projectId: value.projectId };
  return null;
}

export function parseStoredOperation(value: unknown): StoredOperation | null {
  let data: Record<string, unknown>;
  try { data = object(value); } catch { return null; }
  if (data.schemaVersion !== 1) return null;
  let recordedAt: string;
  try { recordedAt = timestamp(data.recordedAt); } catch { return null; }
  const expectedKeys = data.kind === "project_create"
    ? ["actor", "input", "key", "kind", "recordedAt", "schemaVersion"]
    : ["actor", "input", "key", "kind", "projectId", "recordedAt", "schemaVersion"];
  if (Object.keys(data).sort().join("\u0000") !== expectedKeys.join("\u0000")) return null;
  const parsedIdentity = identity(data);
  if (parsedIdentity === null) return null;
  if (parsedIdentity.kind === "project_create") {
    const input = parseCreateInput(data.input);
    if (input === null) return null;
    return { ...parsedIdentity, input, schemaVersion: 1, recordedAt };
  }
  const input = parseUpdateInput(data.input);
  return input === null ? null : { ...parsedIdentity, input, schemaVersion: 1, recordedAt };
}

export function parseOperationIndex(value: unknown): OperationIndex | null {
  let data: Record<string, unknown>;
  try { data = object(value); } catch { return null; }
  let recordedAt: string;
  try { recordedAt = timestamp(data.recordedAt); } catch { return null; }
  const expectedKeys = data.kind === "project_create" ? ["actor", "key", "kind", "recordedAt"] : ["actor", "key", "kind", "projectId", "recordedAt"];
  if (Object.keys(data).sort().join("\u0000") !== expectedKeys.join("\u0000")) return null;
  const parsedIdentity = identity(data);
  return parsedIdentity === null ? null : { ...parsedIdentity, recordedAt };
}

export function operationStorageKey(value: OperationIdentity): string {
  const key = normalizeProjectOperationKey(value.key);
  if (key === null) throw new Error("Invalid project operation key");
  return `${inputPrefix}${encodeURIComponent(value.actor)}:${value.kind}:${value.kind === "project_update" ? `${encodeURIComponent(value.projectId)}:` : ""}${key}`;
}

export function projectRecoveryPath(value: OperationIdentity): string {
  const key = normalizeProjectOperationKey(value.key);
  if (key === null) throw new Error("Invalid project operation key");
  return value.kind === "project_create" ? `/project-creations/${key}` : `/projects/${encodeURIComponent(value.projectId)}/updates/${key}`;
}

function prepared(operation: StoredOperation): PreparedOperation {
  return operation.kind === "project_create" ? { kind: "project_create", operation } : { kind: "project_update", operation };
}

function indexFor(operation: StoredOperation): OperationIndex {
  return operation.kind === "project_create"
    ? { actor: operation.actor, kind: "project_create", key: operation.key, recordedAt: operation.recordedAt }
    : { actor: operation.actor, kind: "project_update", key: operation.key, projectId: operation.projectId, recordedAt: operation.recordedAt };
}

function readAllIndexes(): OperationIndex[] {
  try {
    const values: unknown = JSON.parse(localStorage.getItem(indexKey) ?? "[]");
    if (!Array.isArray(values) || values.length > 1000) return [];
    return values.map(parseOperationIndex).filter((item): item is OperationIndex => item !== null);
  } catch {
    return [];
  }
}

function writeIndexes(indexes: readonly OperationIndex[]): boolean {
  try {
    localStorage.setItem(indexKey, JSON.stringify(indexes));
    return JSON.stringify(JSON.parse(localStorage.getItem(indexKey) ?? "null")) === JSON.stringify(indexes);
  } catch {
    return false;
  }
}

export function readOperationIndex(actor: string): OperationIndex[] {
  return readAllIndexes().filter((item) => item.actor === actor);
}

export function prepareOperation(operation: ProjectOperation): StoragePreparation {
  const recordedAt = new Date().toISOString();
  const raw = { ...operation, schemaVersion: 1, recordedAt };
  const stored = parseStoredOperation(raw);
  if (stored === null) throw new Error("Invalid project operation");
  if (stored.kind === "project_create") Object.freeze(stored.input.repositoryIds);
  else if (stored.input.repositoryIdsToAdd !== undefined) Object.freeze(stored.input.repositoryIdsToAdd);
  if (stored.input.configuration !== undefined) {
    Object.freeze(stored.input.configuration.modelProfile);
    Object.freeze(stored.input.configuration.executionProfile);
    Object.freeze(stored.input.configuration);
  }
  Object.freeze(stored.input);
  Object.freeze(stored);
  const value = prepared(stored);
  const recoveryPath = projectRecoveryPath(stored);
  let inputStored = false;
  try {
    sessionStorage.setItem(operationStorageKey(stored), JSON.stringify(stored));
    inputStored = loadOperation(stored) !== null;
  } catch { /* The caller must show the recovery link before sending. */ }
  const nextIndexes = [...readAllIndexes().filter((item) => operationStorageKey(item) !== operationStorageKey(stored)), indexFor(stored)];
  const indexed = writeIndexes(nextIndexes);
  return inputStored && indexed ? { kind: "stored", prepared: value, recoveryPath } : { kind: "link-required", prepared: value, recoveryPath };
}

export function loadOperation(identity: CreateIdentity): StoredCreateOperation | null;
export function loadOperation(identity: UpdateIdentity): StoredUpdateOperation | null;
export function loadOperation(identity: OperationIdentity): StoredOperation | null;
export function loadOperation(identity: OperationIdentity): StoredOperation | null {
  try {
    const parsed = parseStoredOperation(JSON.parse(sessionStorage.getItem(operationStorageKey(identity)) ?? "null"));
    const key = normalizeProjectOperationKey(identity.key);
    if (parsed === null || key === null || parsed.actor !== identity.actor || parsed.kind !== identity.kind || parsed.key !== key) return null;
    if (identity.kind === "project_update" && (parsed.kind !== "project_update" || parsed.projectId !== identity.projectId)) return null;
    return parsed;
  } catch {
    return null;
  }
}

function removeInput(identity: OperationIdentity): void {
  try { sessionStorage.removeItem(operationStorageKey(identity)); } catch { /* Browser storage may be unavailable. */ }
}

export function clearOperationInputs(args: { actor: string; projectId?: string }): void {
  try {
    for (let index = sessionStorage.length - 1; index >= 0; index -= 1) {
      const key = sessionStorage.key(index);
      if (key === null || !key.startsWith(inputPrefix)) continue;
      const operation = parseStoredOperation(JSON.parse(sessionStorage.getItem(key) ?? "null"));
      if (operation === null || operation.actor !== args.actor) continue;
      if (args.projectId === undefined || operation.kind === "project_update" && operation.projectId === args.projectId) sessionStorage.removeItem(key);
    }
  } catch { /* Browser storage may be unavailable. */ }
}

export function clearAllOperationInputs(): void {
  for (const item of readAllIndexes()) removeInput(item);
  try {
    for (let index = sessionStorage.length - 1; index >= 0; index -= 1) {
      const key = sessionStorage.key(index);
      if (key?.startsWith(inputPrefix)) sessionStorage.removeItem(key);
    }
  } catch { /* Browser storage may be unavailable. */ }
}
