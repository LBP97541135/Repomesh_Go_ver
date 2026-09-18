import { request } from "./api";
import type { Result } from "./api";
import { identifier, isUuid, object, text, timestamp } from "./values";

export type APIReply<T> = Result<T>;
export type WriteContext = { actor: string; csrfToken: string };

export type ProviderID = string;
export type ProviderRevision = string;
export type ModelRowID = string;
export type SecretVersionID = string;
export type SaveID = string;
export type ProfileID = string;

export type ModelInput = {
  id: ModelRowID | null;
  modelId: string;
  displayName: string | null;
  contextWindow: number;
  maxOutputTokens: number;
  reasoning: boolean;
  vision: boolean;
};

export type ModelView = Omit<ModelInput, "id" | "displayName"> & {
  id: ModelRowID;
  displayName: string;
  modelProfileId: ProfileID;
};

export type ProviderView = {
  id: ProviderID;
  name: string;
  revision: ProviderRevision;
  baseUrl: string;
  apiFormat: "openai_chat_completions";
  secret: { configured: boolean; versionId: SecretVersionID | null; availability: "available" | "unavailable" | "unknown" | "missing" };
  models: readonly ModelView[];
  updatedAt: string;
};

export type ProviderPage = {
  items: readonly { id: ProviderID; name: string; revision: ProviderRevision; modelCount: number }[];
  nextCursor: string | null;
};

export type SaveFields = {
  providerId: ProviderID | null;
  expectedRevision: ProviderRevision | null;
  name: string;
  baseUrl: string;
  apiFormat: "openai_chat_completions";
  models: readonly ModelInput[];
};

export type SaveBody = SaveFields & {
  secret: { mode: "keep" } | { mode: "replace"; value: string };
};

export type SaveResult =
  | { saveId: SaveID; outcome: "committed"; providerId: ProviderID; providerRevision: ProviderRevision; secretVersionId: SecretVersionID; committedAt: string; links: { provider: string; operation: string } }
  | { saveId: SaveID; outcome: "rejected"; error: { code: string; message: string; fieldErrors: readonly { field: string; code: string }[]; requestId: string }; decidedAt: string }
  | { saveId: SaveID; outcome: "closed_without_save"; closedAt: string; links: { operation: string } };

function member<const T extends string>(value: unknown, values: readonly T[]): T {
  for (const item of values) if (value === item) return item;
  throw new Error("Invalid choice");
}

function integer(value: unknown): number {
  if (typeof value !== "number" || !Number.isInteger(value) || value < 1 || value > 2147483647) throw new Error("Invalid integer");
  return value;
}

function path(value: unknown, prefix: string): string {
  const parsed = text(value, 256);
  if (!parsed.startsWith(prefix)) throw new Error("Invalid API path");
  return parsed;
}

export function parseProvider(raw: unknown): ProviderView {
  const data = object(raw);
  const secret = object(data.secret);
  if (!Array.isArray(data.models) || data.models.length < 1 || data.models.length > 50) throw new Error("Invalid models");
  return {
    id: identifier(data.id),
    name: text(data.name, 100),
    revision: identifier(data.revision),
    baseUrl: text(data.baseUrl, 2048),
    apiFormat: member(data.apiFormat, ["openai_chat_completions"]),
    secret: {
      configured: secret.configured === true,
      versionId: secret.versionId === null ? null : identifier(secret.versionId),
      availability: member(secret.availability, ["available", "unavailable", "unknown", "missing"]),
    },
    models: data.models.map((item: unknown) => {
      const model = object(item);
      return {
        id: identifier(model.id),
        modelProfileId: identifier(model.modelProfileId),
        modelId: text(model.modelId, 200),
        displayName: text(model.displayName, 200),
        contextWindow: integer(model.contextWindow),
        maxOutputTokens: integer(model.maxOutputTokens),
        reasoning: model.reasoning === true,
        vision: model.vision === true,
      };
    }),
    updatedAt: timestamp(data.updatedAt),
  };
}

export function parseProviderPage(raw: unknown): ProviderPage {
  const data = object(raw);
  if (!Array.isArray(data.items) || data.items.length > 100) throw new Error("Invalid providers");
  return {
    items: data.items.map((item: unknown) => {
      const row = object(item);
      return { id: identifier(row.id), name: text(row.name, 100), revision: identifier(row.revision), modelCount: integer(row.modelCount) };
    }),
    nextCursor: data.nextCursor === null ? null : text(data.nextCursor, 8192),
  };
}

export function parseSaveResult(raw: unknown): SaveResult {
  const data = object(raw);
  const saveId = identifier(data.saveId);
  if (!isUuid(saveId)) throw new Error("Invalid save id");
  const outcome = member(data.outcome, ["committed", "rejected", "closed_without_save"]);
  if (outcome === "committed") {
    return {
      saveId, outcome,
      providerId: identifier(data.providerId),
      providerRevision: identifier(data.providerRevision),
      secretVersionId: identifier(data.secretVersionId),
      committedAt: timestamp(data.committedAt),
      links: { provider: path(object(data.links).provider, "/api/model-providers/"), operation: path(object(data.links).operation, "/api/model-provider-saves/") },
    };
  }
  if (outcome === "rejected") {
    const error = object(data.error);
    if (!Array.isArray(error.fieldErrors) || error.fieldErrors.length > 100) throw new Error("Invalid field errors");
    return {
      saveId, outcome, decidedAt: timestamp(data.decidedAt),
      error: {
        code: text(error.code, 128),
        message: text(error.message, 512),
        requestId: text(error.requestId, 128),
        fieldErrors: error.fieldErrors.map((item: unknown) => {
          const field = object(item);
          return { field: text(field.field, 128), code: text(field.code, 128) };
        }),
      },
    };
  }
  return {
    saveId, outcome, closedAt: timestamp(data.closedAt),
    links: { operation: path(object(data.links).operation, "/api/model-provider-saves/") },
  };
}

export function listProviders(query: { q?: string; cursor?: string; limit?: number }, signal: AbortSignal): Promise<APIReply<ProviderPage>> {
  const params = new URLSearchParams();
  if (query.q) params.set("q", query.q);
  if (query.cursor) params.set("cursor", query.cursor);
  if (query.limit) params.set("limit", String(query.limit));
  const suffix = params.size > 0 ? `?${params}` : "";
  return request({ path: `/api/model-providers${suffix}`, parse: parseProviderPage, signal });
}

export function getProvider(id: ProviderID, signal: AbortSignal): Promise<APIReply<ProviderView>> {
  return request({ path: `/api/model-providers/${encodeURIComponent(id)}`, parse: parseProvider, signal });
}

export function getProviderVersion(id: ProviderID, revision: ProviderRevision, signal: AbortSignal): Promise<APIReply<ProviderView>> {
  return request({ path: `/api/model-providers/${encodeURIComponent(id)}/versions/${encodeURIComponent(revision)}`, parse: parseProvider, signal });
}

export function saveProvider(context: WriteContext, id: SaveID, body: SaveBody, signal: AbortSignal): Promise<APIReply<SaveResult>> {
  return request({ path: "/api/model-provider-saves", method: "POST", body, csrfToken: context.csrfToken, key: id, parse: parseSaveResult, signal, successStatuses: [200, 201] });
}

export function getSave(id: SaveID, signal: AbortSignal): Promise<APIReply<SaveResult>> {
  return request({ path: `/api/model-provider-saves/${encodeURIComponent(id)}`, parse: parseSaveResult, signal });
}

export function closeSave(context: WriteContext, id: SaveID, signal: AbortSignal): Promise<APIReply<SaveResult>> {
  return request({ path: `/api/model-provider-saves/${encodeURIComponent(id)}/close`, method: "POST", body: {}, csrfToken: context.csrfToken, key: id, parse: parseSaveResult, signal });
}
