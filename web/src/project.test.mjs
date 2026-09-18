import assert from "node:assert/strict";
import test from "node:test";
import { createInput, expireSelection, updateInput } from "./projectDrafts.ts";
import { createProject, parseProject, parseProjectCreation, parseProjectRepositories, parseProfiles, updateProject } from "./projectApi.ts";
import { clearOperationInputs, loadOperation, operationStorageKey, parseOperationIndex, parseStoredOperation, prepareOperation, projectRecoveryPath, readOperationIndex } from "./projectRecovery.ts";
import { parseBusinessNextPage, parseDestination, parseRoute, readLoginDestination, rememberLoginDestination } from "./routes.ts";
import { isSafeSegment, scalarLength, timestamp } from "./values.ts";

const operationId = "0a2a5d23-398b-4e46-865b-fc9a1dd15738";
const updateId = "1a2a5d23-398b-4e46-865b-fc9a1dd15739";
const observedAt = "2026-09-12T12:00:00Z";
const capability = { status: "allowed", reasonCodes: [], observedAt };
const repository = { id: "repo_1", displayName: "example/orders", userParticipation: capability, appCapability: { status: "denied", reasonCodes: ["APP_PERMISSION_MISSING"], observedAt } };
const configuration = {
  modelProfile: { mode: "inherit" }, executionProfile: { mode: "inherit" },
  effective: { configurationRevision: "cfg_1", modelProfileId: null, executionProfileId: null, workerConcurrency: null, budgetPolicyId: null, timeLimitPolicyId: null, verificationGroupEnabled: null },
  checks: { status: "denied", reasonCodes: ["MODEL_CONFIG_MISSING"], observedAt },
};
const project = {
  id: "project-one", name: "订单系统", purpose: "协调维护", projectRevision: "revision-1", createdAt: observedAt,
  configuration, actions: { canEdit: true, canCreateIssue: false }, creationReadiness: { status: "restricted", reasonCodes: ["MODEL_CONFIG_MISSING"], observedAt },
};

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

test("fixed routes reject decoded ambiguity and only admit adopted destinations", () => {
  assert.deepEqual(parseRoute("/projects/new"), { kind: "project-create" });
  assert.deepEqual(parseRoute("/projects/project-one/settings"), { kind: "project-settings", projectId: "project-one" });
  assert.deepEqual(parseRoute(`/projects/project-one/updates/${updateId}`), { kind: "project-update-result", projectId: "project-one", key: updateId });
  for (const path of ["/projects/%2e%2e", "/projects/..", "/projects/a%2fb", "/projects/a\\b", "/projects/a?x=1"]) assert.deepEqual(parseRoute(path), { kind: "not-found" });
  assert.equal(parseBusinessNextPage("/projects/project-one"), "/projects/project-one");
  assert.equal(parseBusinessNextPage(`/projects/project-one/updates/${updateId}`), `/projects/project-one/updates/${updateId}`);
  for (const path of ["/projects", "/projects/new", "/projects/project-one/settings", "//example.invalid", "/projects/project-one#x"]) assert.throws(() => parseBusinessNextPage(path));
});

test("project operation keys normalize to lowercase at route and recovery boundaries", () => {
  const uppercase = operationId.toUpperCase();
  assert.deepEqual(parseRoute(`/project-creations/${uppercase}`), { kind: "project-creation-result", key: operationId });
  assert.equal(projectRecoveryPath({ actor: "actor-a", kind: "project_create", key: uppercase }), `/project-creations/${operationId}`);
});

test("the shared scalar boundary rejects isolated surrogates and counts pairs once", () => {
  assert.equal(scalarLength(""), 0);
  assert.equal(scalarLength("😀"), 1);
  assert.equal(scalarLength("a😀b"), 3);
  assert.equal(scalarLength("\ud800"), null);
  assert.equal(scalarLength("\udc00"), null);
  assert.equal(scalarLength("a\ud800"), null);
  assert.equal(isSafeSegment("\ud800"), false);
  assert.throws(() => timestamp("2026-02-31T12:00:00Z"));
});

test("destination storage validates every field and falls back to home", () => {
  const destination = { kind: "operation", operationKind: "project_update", projectId: "project-one", operationId: updateId };
  assert.equal(rememberLoginDestination(destination), true);
  assert.deepEqual(readLoginDestination(), destination);
  assert.equal(parseDestination({ ...destination, extra: "rejected" }), null);
  session.setItem("repomesh.auth.destination", JSON.stringify({ kind: "project", projectId: "../hidden" }));
  assert.deepEqual(readLoginDestination(), { kind: "home" });
});

test("project parsers keep nullable configuration facts and reject undisclosed repository identities", () => {
  assert.deepEqual(parseProject(project), project);
  assert.deepEqual(parseProfiles({ items: [], nextCursor: "continue", defaultProfileId: null }), { items: [], nextCursor: "continue", defaultProfileId: null });
  assert.deepEqual(parseProjectRepositories({ items: [repository], nextCursor: null, projectRevision: "revision-1", restrictedRepositoryCount: 2 }).restrictedRepositoryCount, 2);
  assert.throws(() => parseProject({ ...project, createdAt: "2026-09-12T05:00:00-07:00" }));
  assert.throws(() => parseProjectRepositories({ items: [{ ...repository, userParticipation: { status: "unknown", reasonCodes: [], observedAt: null } }], nextCursor: null, projectRevision: "revision-1", restrictedRepositoryCount: 0 }));
  assert.equal(JSON.stringify(parseProject(project)).includes("token"), false);
});

test("project requests use explicit methods and reject cross-key receipts", async (t) => {
  const calls = [];
  let body = {
    projectCreationId: operationId, status: "committed", projectId: "project-one", projectRevision: "revision-1", createdAt: observedAt,
    links: { project: "/api/projects/project-one", operation: `/api/project-creations/${operationId}` },
  };
  t.mock.method(globalThis, "fetch", async (path, options) => { calls.push({ path, options }); return new Response(JSON.stringify(body), { status: calls.length === 1 ? 201 : 200, headers: { "Content-Type": "application/json" } }); });
  const createInput = { name: "订单系统", purpose: "协调维护", repositoryIds: ["repo_1"], configuration: { modelProfile: { mode: "inherit" }, executionProfile: { mode: "inherit" } } };
  assert.equal((await createProject({ key: operationId, input: createInput, csrfToken: "csrf" })).kind, "ok");
  assert.equal(calls[0].options.method, "POST");
  assert.equal(calls[0].options.headers.get("Idempotency-Key"), operationId);
  body = { updateId, status: "committed", projectId: "project-one", projectRevision: "revision-2", updatedAt: observedAt, links: { project: "/api/projects/project-one", operation: `/api/projects/project-one/updates/${updateId}` } };
  assert.equal((await updateProject({ projectId: "project-one", key: updateId, input: { expectedProjectRevision: "revision-1", name: "新名称" }, csrfToken: "csrf" })).kind, "ok");
  assert.equal(calls[1].options.method, "PATCH");
  body = { ...body, updateId: operationId, links: { ...body.links, operation: `/api/projects/project-one/updates/${operationId}` } };
  assert.deepEqual(await updateProject({ projectId: "project-one", key: updateId, input: { expectedProjectRevision: "revision-1", name: "新名称" }, csrfToken: "csrf" }), { kind: "error", status: 200, code: "INVALID_RESPONSE", retryAt: null });
});

test("project requests send and compare normalized operation keys", async (t) => {
  const calls = [];
  t.mock.method(globalThis, "fetch", async (path, options) => {
    calls.push({ path, options });
    return new Response(JSON.stringify({
      projectCreationId: operationId,
      status: "committed",
      projectId: "project-one",
      projectRevision: "revision-1",
      createdAt: observedAt,
      links: { project: "/api/projects/project-one", operation: `/api/project-creations/${operationId}` },
    }), { status: 201, headers: { "Content-Type": "application/json" } });
  });
  const result = await createProject({ key: operationId.toUpperCase(), input: { name: "项目", purpose: "用途", repositoryIds: ["repo_1"] }, csrfToken: "csrf" });
  assert.equal(result.kind, "ok");
  assert.equal(calls[0].options.headers.get("Idempotency-Key"), operationId);
});

test("receipt parsers require committed status, UUID identity, UTC time, and exact API links", () => {
  const receipt = { projectCreationId: operationId, status: "committed", projectId: "project-one", projectRevision: "revision-1", createdAt: observedAt, links: { project: "/api/projects/project-one", operation: `/api/project-creations/${operationId}` } };
  assert.deepEqual(parseProjectCreation(receipt), receipt);
  assert.throws(() => parseProjectCreation({ ...receipt, projectCreationId: "not-a-uuid" }));
  assert.throws(() => parseProjectCreation({ ...receipt, links: { ...receipt.links, operation: "/api/project-creations/other" } }));
});

test("draft validation counts Unicode scalars, preserves exact text, and separates configuration intent", () => {
  const now = Date.parse(observedAt) + 30_000;
  const selection = new Map([[repository.id, { kind: "confirmed", id: repository.id, repository }]]);
  const valid = createInput({ name: "😀".repeat(200), purpose: "  保留空格  ", selection, configuration: { modelProfile: { mode: "inherit" }, executionProfile: { mode: "inherit" } } }, now);
  assert.equal(valid.kind, "valid");
  if (valid.kind === "valid") assert.equal(valid.input.purpose, "  保留空格  ");
  assert.equal(createInput({ name: "\ud800", purpose: "用途", selection, configuration: { modelProfile: { mode: "inherit" }, executionProfile: { mode: "inherit" } } }, now).kind, "invalid");
  const base = parseProject(project);
  assert.deepEqual(updateInput({ base, name: base.name, purpose: base.purpose, additions: new Map(), configuration: { kind: "preserve" } }, now), { kind: "unchanged" });
  const replacement = updateInput({ base, name: base.name, purpose: base.purpose, additions: new Map(), configuration: { kind: "replace", value: { modelProfile: { mode: "inherit" }, executionProfile: { mode: "inherit" } } } }, now);
  assert.equal(replacement.kind, "valid");
  if (replacement.kind === "valid") assert.equal("configuration" in replacement.input, true);
});

test("selection expiry removes disclosed names but preserves stable selection count", () => {
  const selection = new Map([[repository.id, { kind: "confirmed", id: repository.id, repository }]]);
  const expired = expireSelection(selection, Date.parse(observedAt) + 60_001);
  assert.deepEqual(expired.get(repository.id), { kind: "unconfirmed", id: repository.id });
  assert.equal(JSON.stringify(expired.get(repository.id)).includes(repository.displayName), false);
});

test("operation storage isolates actor, kind, key, and project and rejects extra fields", () => {
  const input = { name: "项目", purpose: "用途", repositoryIds: ["repo_1"], configuration: { modelProfile: { mode: "inherit" }, executionProfile: { mode: "inherit" } } };
  const prepared = prepareOperation({ actor: "actor-a", kind: "project_create", key: operationId, input });
  assert.equal(prepared.kind, "stored");
  assert.equal(Object.isFrozen(prepared.prepared.operation.input), true);
  assert.equal(Object.isFrozen(prepared.prepared.operation.input.repositoryIds), true);
  assert.equal(Object.isFrozen(prepared.prepared.operation.input.configuration), true);
  assert.equal(Object.isFrozen(prepared.prepared.operation.input.configuration.modelProfile), true);
  input.repositoryIds.push("repo_2");
  input.configuration.modelProfile.mode = "reference";
  input.configuration.modelProfile.id = "changed";
  assert.deepEqual(prepared.prepared.operation.input.repositoryIds, ["repo_1"]);
  assert.deepEqual(prepared.prepared.operation.input.configuration.modelProfile, { mode: "inherit" });
  assert.notEqual(loadOperation({ actor: "actor-a", kind: "project_create", key: operationId }), null);
  assert.equal(loadOperation({ actor: "actor-b", kind: "project_create", key: operationId }), null);
  assert.equal(loadOperation({ actor: "actor-a", kind: "project_update", projectId: "project-one", key: operationId }), null);
  const raw = JSON.parse(session.getItem(operationStorageKey({ actor: "actor-a", kind: "project_create", key: operationId })));
  assert.equal(parseStoredOperation({ ...raw, leaked: "value" }), null);
  assert.equal(parseOperationIndex({ actor: "actor-a", kind: "project_create", key: operationId, recordedAt: observedAt, name: "must not persist" }), null);
  assert.equal(readOperationIndex("actor-a").length, 1);
  assert.equal(JSON.stringify(local.values).includes("项目"), false);
});

test("storage failure requires a recovery link before send", () => {
  session.fail = true;
  const input = { name: "项目", purpose: "用途", repositoryIds: ["repo_1"] };
  const prepared = prepareOperation({ actor: "actor-a", kind: "project_create", key: operationId, input });
  assert.equal(prepared.kind, "link-required");
  assert.equal(prepared.recoveryPath, `/project-creations/${operationId}`);
});

test("actor cleanup scans stored inputs when the local index write failed", () => {
  local.fail = true;
  const identity = { actor: "actor-a", kind: "project_create", key: operationId };
  const prepared = prepareOperation({ ...identity, input: { name: "项目", purpose: "用途", repositoryIds: ["repo_1"] } });
  assert.equal(prepared.kind, "link-required");
  local.fail = false;
  assert.notEqual(loadOperation(identity), null);
  clearOperationInputs({ actor: "actor-a" });
  assert.equal(loadOperation(identity), null);
});

test("a prepared update keeps its original expected revision and nested input", () => {
  const input = {
    expectedProjectRevision: "revision-original",
    purpose: "原用途",
    repositoryIdsToAdd: ["repo_1"],
    configuration: { modelProfile: { mode: "reference", id: "model_1" }, executionProfile: { mode: "inherit" } },
  };
  const prepared = prepareOperation({ actor: "actor-a", kind: "project_update", projectId: "project-one", key: updateId, input });
  input.expectedProjectRevision = "revision-later";
  input.repositoryIdsToAdd.push("repo_2");
  input.configuration.modelProfile.id = "model_later";
  assert.equal(prepared.prepared.operation.input.expectedProjectRevision, "revision-original");
  assert.deepEqual(prepared.prepared.operation.input.repositoryIdsToAdd, ["repo_1"]);
  assert.deepEqual(prepared.prepared.operation.input.configuration.modelProfile, { mode: "reference", id: "model_1" });
  assert.equal(Object.isFrozen(prepared.prepared.operation.input.repositoryIdsToAdd), true);
  assert.equal(Object.isFrozen(prepared.prepared.operation.input.configuration.modelProfile), true);
});
