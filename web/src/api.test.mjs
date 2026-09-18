import assert from "node:assert/strict";
import test from "node:test";
import { endSession, errorMessage, readAttempt, readRepositories, readSession, startAuthorization } from "./api.ts";

const id = "0a2a5d23-398b-4e46-865b-fc9a1dd15738";
const observedAt = "2026-09-12T12:00:00Z";
const session = {
  user: { id: "usr_1", displayName: "测试账号" },
  githubConnection: { status: "connected", observedAt },
  csrfToken: "test-session-csrf",
};
const receipt = {
  attemptId: id, purpose: "reconnect", state: "confirmed", reasonCode: null, observedAt,
  connection: { committedRevision: "gc_8", isCurrent: true }, nextPage: "/",
};
const start = {
  attemptId: id, authorizationUrl: "https://github.com/login/oauth/authorize?client_id=test&state=example",
  expiresAt: "2026-09-12T12:10:00Z", resultPage: `/auth/result/${id}`,
};
const capability = { status: "allowed", reasonCodes: [], observedAt };
const repository = { id: "repo_1", displayName: "example/orders", userParticipation: capability, appCapability: capability };
const json = (value, status = 200, headers = {}) => new Response(JSON.stringify(value), { status, headers: { "Content-Type": "application/json", ...headers } });

function expectInvalid(result, status = 200) {
  assert.deepEqual(result, { kind: "error", status, code: "INVALID_RESPONSE", retryAt: null });
}

test("a start sends only the adopted home payload and retains the requested attempt key", async (t) => {
  const calls = [];
  t.mock.method(globalThis, "fetch", async (path, options) => {
    calls.push({ path, options });
    return json(start, calls.length === 1 ? 201 : 200);
  });
  assert.deepEqual(await startAuthorization({ id, purpose: "login" }), { kind: "ok", value: start });
  assert.deepEqual(await startAuthorization({ id, purpose: "login" }), { kind: "ok", value: start });
  assert.equal(calls.length, 2);
  for (const { path, options } of calls) {
    assert.equal(path, "/api/auth/github/login");
    assert.equal(options.method, "POST");
    assert.equal(options.headers.get("Idempotency-Key"), id);
    assert.equal(options.headers.has("Origin"), false);
    assert.equal(options.headers.has("X-CSRF-Token"), false);
    assert.equal(options.body, '{"destination":{"kind":"home"}}');
    assert.equal(options.credentials, "same-origin");
    assert.equal(options.redirect, "error");
  }
});

test("a start sends the approved project destination through the single auth transport", async (t) => {
  let sentBody = "";
  t.mock.method(globalThis, "fetch", async (_path, options) => {
    sentBody = options.body;
    return json(start, 201);
  });
  const result = await startAuthorization({ id, purpose: "login", destination: { kind: "project", projectId: "project-one" } });
  assert.equal(result.kind, "ok");
  assert.equal(sentBody, '{"destination":{"kind":"project","projectId":"project-one"}}');
});

test("reconnect and logout use the current session CSRF value", async (t) => {
  const calls = [];
  t.mock.method(globalThis, "fetch", async (path, options) => {
    calls.push({ path, options });
    return path.endsWith("/logout") ? new Response(null, { status: 204 }) : json(start, 201);
  });
  assert.deepEqual(await startAuthorization({ id, purpose: "reconnect", csrfToken: "current-csrf" }), { kind: "ok", value: start });
  assert.deepEqual(await endSession("current-csrf"), { kind: "ok", value: null });
  assert.equal(calls[0].path, "/api/auth/github/reconnect");
  assert.equal(calls[1].path, "/api/auth/logout");
  assert.equal(calls[1].options.body, "{}");
  for (const call of calls) assert.equal(call.options.headers.get("X-CSRF-Token"), "current-csrf");
});

test("start rejects foreign authorization origins, userinfo, fragments, and unrelated result routes", async (t) => {
  let response = start;
  t.mock.method(globalThis, "fetch", async () => json(response, 201));
  for (const authorizationUrl of ["https://github.com.attacker.invalid/login/oauth/authorize", "https://attacker.invalid/login/oauth/authorize", "https://name@github.com/login/oauth/authorize", "https://github.com/login/oauth/authorize#token", "http://github.com/login/oauth/authorize", "https://github.com/other"]) {
    response = { ...start, authorizationUrl };
    expectInvalid(await startAuthorization({ id, purpose: "login" }), 201);
  }
  response = { ...start, resultPage: "/auth/result/another-attempt" };
  expectInvalid(await startAuthorization({ id, purpose: "login" }), 201);
});

test("a confirmed reconnect receipt and a missing current session remain separate results", async (t) => {
  t.mock.method(globalThis, "fetch", async (path) => path === "/api/session"
    ? json({ error: { code: "AUTHENTICATION_REQUIRED", message: "no session" } }, 401)
    : json(receipt));
  assert.deepEqual(await readAttempt({ id }), { kind: "ok", value: receipt });
  assert.deepEqual(await readSession(), { kind: "error", status: 401, code: "AUTHENTICATION_REQUIRED", retryAt: null });
});

test("a connected current session does not change an unknown reconnect attempt", async (t) => {
  const unknown = { ...receipt, state: "unknown", reasonCode: "EXCHANGE_UNCONFIRMED", connection: null, nextPage: null };
  t.mock.method(globalThis, "fetch", async (path) => json(path === "/api/session" ? session : unknown));
  assert.deepEqual(await readSession(), { kind: "ok", value: session });
  assert.deepEqual(await readAttempt({ id }), { kind: "ok", value: unknown });
});

test("result routes reject external, escaped, and unimplemented destinations", async (t) => {
  let nextPage = "/";
  t.mock.method(globalThis, "fetch", async () => json({ ...receipt, nextPage }));
  assert.deepEqual(await readAttempt({ id }), { kind: "ok", value: receipt });
  for (nextPage of ["//attacker.invalid", "https://attacker.invalid", "/%2e%2e/", "/\\attacker.invalid", "/?returnUrl=https://attacker.invalid", "/projects/prj_1/issues", "/#private"]) {
    expectInvalid(await readAttempt({ id }));
  }
});

test("malformed session, attempt enums, and incompatible receipts fail closed", async (t) => {
  let response = null;
  t.mock.method(globalThis, "fetch", async () => json(response));
  for (response of [{ ...session, user: null }, { ...session, csrfToken: 123 }, { ...session, githubConnection: { status: "ready", observedAt } }]) expectInvalid(await readSession());
  for (response of [{ ...receipt, state: "success" }, { ...receipt, purpose: "login" }, { ...receipt, connection: { committedRevision: "gc_8", isCurrent: "true" } }, { ...receipt, reasonCode: "RAW_PROVIDER_ERROR" }]) expectInvalid(await readAttempt({ id }));
});

test("repository pages preserve partial coverage and empty pages with a continuation cursor", async (t) => {
  const page = { items: [], nextCursor: "next-page", coverage: { status: "partial", reasonCodes: ["APP_INSTALLATION_SCOPE"], observedAt } };
  const calls = [];
  t.mock.method(globalThis, "fetch", async (path) => { calls.push(path); return json(page); });
  assert.deepEqual(await readRepositories({ query: "订单 & api", cursor: "cursor/+?=" }), { kind: "ok", value: page });
  const params = new URL(calls[0], "https://example.invalid").searchParams;
  assert.equal(params.get("q"), "订单 & api");
  assert.equal(params.get("cursor"), "cursor/+?=");
  assert.equal(params.get("limit"), "50");
  assert.equal(params.get("refresh"), null);
  const refreshed = [];
  t.mock.method(globalThis, "fetch", async (path) => { refreshed.push(path); return json(page); });
  await readRepositories({ query: "", cursor: null, refresh: true });
  assert.equal(new URL(refreshed[0], "https://example.invalid").searchParams.get("refresh"), "1");
});

test("repository names with unconfirmed user participation never enter a successful page", async (t) => {
  let status = "allowed";
  t.mock.method(globalThis, "fetch", async () => json({ items: [{ ...repository, userParticipation: { ...capability, status } }], nextCursor: null, coverage: { status: "partial", reasonCodes: [], observedAt } }));
  const allowed = await readRepositories({ query: "", cursor: null });
  assert.equal(allowed.kind, "ok");
  assert.equal(allowed.value.items[0].displayName, "example/orders");
  for (status of ["unknown", "denied"]) {
    const result = await readRepositories({ query: "", cursor: null });
    expectInvalid(result);
    assert.equal(JSON.stringify(result).includes("example/orders"), false);
  }
});

test("429 honors Retry-After and supplies a cooldown when the header is absent", async (t) => {
  t.mock.method(Date, "now", () => 1000);
  let headers = { "Retry-After": "7" };
  t.mock.method(globalThis, "fetch", async () => json({ error: { code: "RATE_LIMITED" } }, 429, headers));
  assert.deepEqual(await readAttempt({ id }), { kind: "error", status: 429, code: "RATE_LIMITED", retryAt: 8000 });
  headers = {};
  assert.deepEqual(await readAttempt({ id }), { kind: "error", status: 429, code: "RATE_LIMITED", retryAt: 31000 });
});

test("unconfigured service errors are readable without echoing provider data", async (t) => {
  t.mock.method(globalThis, "fetch", async () => json({ error: { code: "AUTH_NOT_CONFIGURED", message: "provider-private-detail" } }, 503));
  const result = await startAuthorization({ id, purpose: "login" });
  assert.deepEqual(result, { kind: "error", status: 503, code: "AUTH_NOT_CONFIGURED", retryAt: null });
  assert.equal(errorMessage(result), "认证或 GitHub 服务暂不可用，请稍后重试；如持续出现，请联系部署管理员检查配置。");
  assert.equal(JSON.stringify(result).includes("provider-private-detail"), false);
});

test("invalid success JSON and unexpected success statuses are not mistaken for usable sessions", async (t) => {
  let response = new Response("{bad", { status: 200 });
  t.mock.method(globalThis, "fetch", async () => response);
  expectInvalid(await readSession());
  response = json(session, 202);
  expectInvalid(await readSession(), 202);
});

test("an aborted success body is not reported as an invalid payload", async (t) => {
  t.mock.method(globalThis, "fetch", async () => ({
    ok: true,
    status: 200,
    json: async () => { throw new DOMException("The user aborted a request.", "AbortError"); },
  }));
  assert.deepEqual(await readSession(), { kind: "error", status: 0, code: "ABORTED", retryAt: null });
});

test("a lost start response stays unknown and does not retry itself", async (t) => {
  let count = 0;
  t.mock.method(globalThis, "fetch", async () => { count += 1; throw new TypeError("Network failed"); });
  assert.deepEqual(await startAuthorization({ id, purpose: "login" }), { kind: "error", status: 0, code: "NETWORK_ERROR", retryAt: null });
  assert.equal(count, 1);
});

test("overlapping repository reads share one in-flight request", async (t) => {
  const page = { items: [], nextCursor: null, coverage: { status: "partial", reasonCodes: [], observedAt } };
  let release;
  let calls = 0;
  t.mock.method(globalThis, "fetch", () => {
    calls += 1;
    return new Promise((resolve) => { release = () => resolve(json(page)); });
  });
  const first = readRepositories({ query: "single-flight-share", cursor: null });
  const second = readRepositories({ query: "single-flight-share", cursor: null });
  assert.equal(calls, 1);
  release();
  const [a, b] = await Promise.all([first, second]);
  assert.equal(a.kind, "ok");
  assert.equal(b.kind, "ok");
  assert.equal(calls, 1);
});

test("aborting one repository reader leaves the shared read running", async (t) => {
  const page = { items: [], nextCursor: null, coverage: { status: "partial", reasonCodes: [], observedAt } };
  let release;
  let calls = 0;
  t.mock.method(globalThis, "fetch", () => {
    calls += 1;
    return new Promise((resolve) => { release = () => resolve(json(page)); });
  });
  const controller = new AbortController();
  const first = readRepositories({ query: "single-flight-abort", cursor: null, signal: controller.signal });
  const second = readRepositories({ query: "single-flight-abort", cursor: null });
  controller.abort();
  assert.deepEqual(await first, { kind: "error", status: 0, code: "ABORTED", retryAt: null });
  release();
  const remaining = await second;
  assert.equal(remaining.kind, "ok");
  assert.equal(calls, 1);
});
