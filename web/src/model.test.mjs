import assert from "node:assert/strict";
import test from "node:test";
import { parseProvider, parseSaveResult } from "./modelApi.ts";
import { acceptsRead, beginRead, persistRecoveryIndex, persistSessionRecovery, readRecoveryIndex, readSessionRecovery, reduceSave, shouldOpenSaveRecovery } from "./modelRecovery.ts";
import { parseBusinessNextPage, parseDestination, parseRoute } from "./routes.ts";

const saveId = "a2394178-23a4-43ba-a886-2755c1f9ad16";
const observedAt = "2026-09-11T12:01:00Z";

class MemoryStorage {
  values = new Map();
  fail = false;
  get length() { return this.values.size; }
  getItem(key) { if (this.fail) throw new Error("disabled"); return this.values.get(key) ?? null; }
  setItem(key, value) { if (this.fail) throw new Error("disabled"); this.values.set(key, String(value)); }
  removeItem(key) { if (this.fail) throw new Error("disabled"); this.values.delete(key); }
  key(index) { return Array.from(this.values.keys())[index] ?? null; }
  clear() { this.values.clear(); }
}

const session = new MemoryStorage();
const local = new MemoryStorage();
Object.defineProperty(globalThis, "sessionStorage", { configurable: true, value: session });
Object.defineProperty(globalThis, "localStorage", { configurable: true, value: local });

test.beforeEach(() => {
  session.fail = false;
  local.fail = false;
  session.clear();
  local.clear();
});

test("model routes and provider_save destinations are admitted", () => {
  assert.deepEqual(parseRoute("/settings/models"), { kind: "model-settings" });
  assert.deepEqual(parseRoute(`/settings/model-saves/${saveId}`), { kind: "model-save-result", key: saveId });
  assert.equal(parseBusinessNextPage(`/settings/model-saves/${saveId}`), `/settings/model-saves/${saveId}`);
  const destination = { kind: "operation", operationKind: "provider_save", operationId: saveId };
  assert.deepEqual(parseDestination(destination), destination);
});

test("save recovery stores only secret-free snapshots and ignores late generation", () => {
  const locator = { kind: "provider_save", actor: "actor-a", id: saveId };
  persistRecoveryIndex({ ...locator, createdAt: observedAt });
  persistSessionRecovery({
    locator,
    snapshot: { providerId: null, expectedRevision: null, name: "网关", baseUrl: "https://gateway.example.invalid/v1", apiFormat: "openai_chat_completions", models: [], secretMode: "replace" },
  });
  assert.equal(readSessionRecovery("actor-a", saveId).snapshot.secretMode, "replace");
  assert.equal("value" in (readSessionRecovery("actor-a", saveId).snapshot), false);
  assert.deepEqual(readRecoveryIndex({ kind: "provider_save", actor: "actor-a", id: saveId, createdAt: observedAt }, "actor-a").id, saveId);
  const ticket = beginRead(locator, 2);
  assert.equal(acceptsRead(ticket, beginRead(locator, 1), "actor-a"), false);
  const current = { state: "checking", locator, generation: 2 };
  const late = reduceSave(current, beginRead(locator, 1), { kind: "ok", value: { saveId, outcome: "committed", providerId: "p", providerRevision: "r", secretVersionId: "s", committedAt: observedAt, links: { provider: "/api/model-providers/p", operation: `/api/model-provider-saves/${saveId}` } } });
  assert.equal(late.state, "checking");
});

test("validation failures stay on the form and unknown or slotted results open recovery", () => {
  assert.equal(shouldOpenSaveRecovery({ kind: "ok", value: { saveId, outcome: "committed" } }), true);
  assert.equal(shouldOpenSaveRecovery({ kind: "error", status: 404, code: "MODEL_SAVE_NOT_FOUND" }), true);
  assert.equal(shouldOpenSaveRecovery({ kind: "error", status: 503, code: "RESULT_UNCONFIRMED" }), true);
  assert.equal(shouldOpenSaveRecovery({ kind: "error", status: 0, code: "NETWORK_ERROR" }), true);
  assert.equal(shouldOpenSaveRecovery({ kind: "error", status: 0, code: "ABORTED" }), true);
  assert.equal(shouldOpenSaveRecovery({ kind: "error", status: 409, code: "IDEMPOTENCY_CONFLICT" }), true);
  assert.equal(shouldOpenSaveRecovery({ kind: "error", status: 409, code: "MODEL_SAVE_CLOSED" }), true);
  assert.equal(shouldOpenSaveRecovery({ kind: "error", status: 410, code: "MODEL_SAVE_RESULT_REMOVED" }), true);
  assert.equal(shouldOpenSaveRecovery({ kind: "error", status: 409, code: "PROVIDER_REVISION_CONFLICT" }), true);
  assert.equal(shouldOpenSaveRecovery({ kind: "error", status: 422, code: "VALIDATION_FAILED" }), false);
  assert.equal(shouldOpenSaveRecovery({ kind: "error", status: 400, code: "INVALID_JSON" }), false);
  assert.equal(shouldOpenSaveRecovery({ kind: "error", status: 401, code: "AUTHENTICATION_REQUIRED" }), false);
  assert.equal(shouldOpenSaveRecovery({ kind: "error", status: 403, code: "ORIGIN_REJECTED" }), false);
});

test("save parsers accept committed rejected and closed receipts", () => {
  const committed = parseSaveResult({
    saveId, outcome: "committed", providerId: "11111111-1111-4111-8111-111111111111",
    providerRevision: "22222222-2222-4222-8222-222222222222", secretVersionId: "33333333-3333-4333-8333-333333333333",
    committedAt: observedAt, links: { provider: "/api/model-providers/11111111-1111-4111-8111-111111111111", operation: `/api/model-provider-saves/${saveId}` },
  });
  assert.equal(committed.outcome, "committed");
  const closed = parseSaveResult({ saveId, outcome: "closed_without_save", closedAt: observedAt, links: { operation: `/api/model-provider-saves/${saveId}` } });
  assert.equal(closed.outcome, "closed_without_save");
  parseProvider({
    id: "11111111-1111-4111-8111-111111111111", name: "网关", revision: "22222222-2222-4222-8222-222222222222",
    baseUrl: "https://gateway.example.invalid/v1", apiFormat: "openai_chat_completions",
    secret: { configured: true, versionId: "33333333-3333-4333-8333-333333333333", availability: "available" },
    models: [{ id: "44444444-4444-4444-8444-444444444444", modelProfileId: "mp", modelId: "deepseek-chat", displayName: "对话", contextWindow: 256000, maxOutputTokens: 128000, reasoning: true, vision: false }],
    updatedAt: observedAt,
  });
});
