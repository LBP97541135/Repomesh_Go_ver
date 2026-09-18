import type { ConfigurationSelection, ProjectCreateInput, ProjectFieldError, ProjectUpdateInput, ProjectView, RepositoryItem } from "./projectApi";
import { scalarLength } from "./values";

export type SelectedRepository =
  | { kind: "confirmed"; id: string; repository: RepositoryItem }
  | { kind: "unconfirmed"; id: string };
export type RepositorySelection = ReadonlyMap<string, SelectedRepository>;
export type ConfigurationEdit =
  | { kind: "preserve" }
  | { kind: "replace"; value: ConfigurationSelection };
export type CreateDraft = { name: string; purpose: string; selection: RepositorySelection; configuration: ConfigurationSelection };
export type UpdateDraft = { base: ProjectView; name: string; purpose: string; additions: RepositorySelection; configuration: ConfigurationEdit };
export type DraftValidation<T> =
  | { kind: "valid"; input: T }
  | { kind: "invalid"; fields: readonly ProjectFieldError[] }
  | { kind: "authorization-unconfirmed" }
  | { kind: "unchanged" };

const observationLifetime = 60_000;

function textError(field: string, value: string, maximum: number): ProjectFieldError | null {
  const length = scalarLength(value);
  if (length === null) return { field, code: "INVALID_UNICODE" };
  if (value.trim().length === 0) return { field, code: "REQUIRED" };
  return length > maximum ? { field, code: "TOO_LONG" } : null;
}

function ids(selection: RepositorySelection): string[] | null {
  const values: string[] = [];
  for (const [id, selected] of selection) {
    if (selected.id !== id || selected.kind === "unconfirmed" || selected.repository.id !== id) return null;
    values.push(id);
  }
  return values;
}

export function expireSelection(selection: RepositorySelection, now: number): RepositorySelection {
  const next = new Map<string, SelectedRepository>();
  for (const [id, selected] of selection) {
    if (selected.kind === "unconfirmed") {
      next.set(id, selected);
      continue;
    }
    const observed = Date.parse(selected.repository.userParticipation.observedAt ?? "");
    if (!Number.isFinite(observed) || observed > now || now - observed > observationLifetime || selected.repository.userParticipation.status !== "allowed") next.set(id, { kind: "unconfirmed", id });
    else next.set(id, selected);
  }
  return next;
}

export function clearSelectionDisclosure(selection: RepositorySelection): RepositorySelection {
  return new Map(Array.from(selection.keys(), (id) => [id, { kind: "unconfirmed", id }]));
}

export function createInput(draft: CreateDraft, now: number): DraftValidation<ProjectCreateInput> {
  const fields = [textError("name", draft.name, 200), textError("purpose", draft.purpose, 20000)].filter((item): item is ProjectFieldError => item !== null);
  const current = expireSelection(draft.selection, now);
  const repositoryIds = ids(current);
  if (repositoryIds === null) return { kind: "authorization-unconfirmed" };
  if (repositoryIds.length === 0) fields.push({ field: "repositoryIds", code: "REQUIRED" });
  if (repositoryIds.length > 100) fields.push({ field: "repositoryIds", code: "TOO_MANY" });
  if (fields.length > 0) return { kind: "invalid", fields };
  return { kind: "valid", input: { name: draft.name, purpose: draft.purpose, repositoryIds, configuration: draft.configuration } };
}

export function updateInput(draft: UpdateDraft, now: number): DraftValidation<ProjectUpdateInput> {
  const fields = [textError("name", draft.name, 200), textError("purpose", draft.purpose, 20000)].filter((item): item is ProjectFieldError => item !== null);
  if (fields.length > 0) return { kind: "invalid", fields };
  const current = expireSelection(draft.additions, now);
  const repositoryIdsToAdd = ids(current);
  if (repositoryIdsToAdd === null) return { kind: "authorization-unconfirmed" };
  if (repositoryIdsToAdd.length > 100) return { kind: "invalid", fields: [{ field: "repositoryIdsToAdd", code: "TOO_MANY" }] };
  const common = { expectedProjectRevision: draft.base.projectRevision };
  const name = draft.name !== draft.base.name ? draft.name : undefined;
  const purpose = draft.purpose !== draft.base.purpose ? draft.purpose : undefined;
  const additions = repositoryIdsToAdd.length > 0 ? repositoryIdsToAdd : undefined;
  const configuration = draft.configuration.kind === "replace" ? draft.configuration.value : undefined;
  if (name !== undefined) return { kind: "valid", input: { ...common, name, ...(purpose === undefined ? {} : { purpose }), ...(additions === undefined ? {} : { repositoryIdsToAdd: additions }), ...(configuration === undefined ? {} : { configuration }) } };
  if (purpose !== undefined) return { kind: "valid", input: { ...common, purpose, ...(additions === undefined ? {} : { repositoryIdsToAdd: additions }), ...(configuration === undefined ? {} : { configuration }) } };
  if (additions !== undefined) return { kind: "valid", input: { ...common, repositoryIdsToAdd: additions, ...(configuration === undefined ? {} : { configuration }) } };
  if (configuration !== undefined) return { kind: "valid", input: { ...common, configuration } };
  return { kind: "unchanged" };
}
