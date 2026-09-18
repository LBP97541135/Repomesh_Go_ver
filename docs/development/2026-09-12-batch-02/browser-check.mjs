import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { once } from "node:events";
import { mkdir, writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const { chromium } = await import(pathToFileURL(resolve(process.env.REPOMESH_PLAYWRIGHT_DIR, "index.mjs")));
const evidence = resolve(dirname(fileURLToPath(import.meta.url)), process.env.REPOMESH_BROWSER_EVIDENCE ?? "browser-01");
await mkdir(evidence);
const records = [];
const server = spawn(process.env.REPOMESH_WEB_BINARY, ["--addr", "127.0.0.1:0", "--assets", resolve(process.env.REPOMESH_WEB_ASSETS ?? "web/dist")], {
  env: { ...process.env, REPOMESH_AUTH_CONFIG: "", REPOMESH_DATABASE_URL: "" }, stdio: ["ignore", "ignore", "pipe"],
});
let browser;
try {
  const origin = await new Promise((resolveOrigin, reject) => {
    const timer = setTimeout(() => reject(new Error("Web startup timed out")), 10000);
    server.on("exit", () => { clearTimeout(timer); reject(new Error("Web exited")); });
    server.stderr.on("data", chunk => {
      const match = /address=127\.0\.0\.1:(\d+)/.exec(chunk.toString());
      if (match) { clearTimeout(timer); resolveOrigin(`http://127.0.0.1:${match[1]}`); }
    });
  });
  browser = await chromium.launch({ headless: true });
  const errors = [];
  const context = await browser.newContext({ viewport: { width: 1280, height: 900 } });
  const page = await context.newPage();
  page.on("pageerror", error => errors.push(error.message));
  const visible = async text => { await page.getByText(text, { exact: true }).first().waitFor({ state: "visible", timeout: 10000 }); };
  await page.goto(`${origin}/login`);
  await visible("暂时无法确认登录状态");
  assert.equal(await page.getByRole("button", { name: "使用 GitHub 登录", exact: true }).count(), 0);
  await page.screenshot({ path: resolve(evidence, "unconfigured.png"), fullPage: true });
  records.push({ name: "actual-unconfigured-backend", passed: true, provider: "actual Go API; no App configuration" });

  const timestamp = new Date().toISOString();
  const user = { user: { id: "user-a", displayName: "验收账号 A" }, githubConnection: { status: "connected", observedAt: timestamp }, csrfToken: "fixture-csrf-a" };
  let authenticated = false;
  const attempts = new Map();
  const starts = [];
  const reads = [];
  let repositories = { items: [], nextCursor: "fixture-cursor", coverage: { status: "partial", reasonCodes: ["APP_INSTALLATION_SCOPE"], observedAt: timestamp } };
  const repo = { id: "repo_fixture", displayName: "fixture/private-repository", userParticipation: { status: "allowed", reasonCodes: [], observedAt: timestamp }, appCapability: { status: "denied", reasonCodes: ["APP_INSTALLATION_MISSING"], observedAt: timestamp } };
  let continueLosesSession = false;
  await page.route(`${origin}/api/**`, async route => {
    const request = route.request();
    const url = new URL(request.url());
    const reply = (status, body) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body), headers: { "Cache-Control": "no-store" } });
    reads.push(url.pathname);
    if (url.pathname === "/api/session") {
      if (continueLosesSession) authenticated = false;
      return reply(authenticated ? 200 : 401, authenticated ? user : { error: { code: "AUTHENTICATION_REQUIRED" } });
    }
    if (url.pathname === "/api/auth/github/login" || url.pathname === "/api/auth/github/reconnect") {
      starts.push({ id: request.headers()["idempotency-key"], body: request.postDataJSON(), origin: request.headers().origin });
      return reply(503, { error: { code: "RESULT_UNCONFIRMED" } });
    }
    if (url.pathname.startsWith("/api/auth/attempts/")) {
      const attempt = attempts.get(url.pathname.split("/").at(-1));
      return reply(attempt ? 200 : 404, attempt ?? { error: { code: "RESOURCE_NOT_FOUND" } });
    }
    if (url.pathname === "/api/auth/logout") {
      assert.equal(request.headers()["x-csrf-token"], "fixture-csrf-a");
      authenticated = false;
      return route.fulfill({ status: 204, body: "" });
    }
    if (url.pathname === "/api/repositories") return reply(200, url.searchParams.has("cursor") ? { ...repositories, items: [repo], nextCursor: null } : repositories);
    return reply(404, { error: { code: "RESOURCE_NOT_FOUND" } });
  });

  await page.goto(`${origin}/login`);
  await visible("开始使用 RepoMesh");
  await page.screenshot({ path: resolve(evidence, "login-desktop.png"), fullPage: true });
  await page.getByRole("button", { name: "使用 GitHub 登录", exact: true }).click();
  await visible("授权发起结果待确认");
  assert.equal(starts.length, 1);
  const firstId = starts[0].id;
  assert.match(firstId, /^[a-f0-9-]{36}$/);
  assert.deepEqual(starts[0].body, { destination: { kind: "home" } });
  assert.equal(starts[0].origin, origin);
  const recovery = await page.evaluate(() => JSON.parse(sessionStorage.getItem("repomesh.auth.attempt")));
  assert.deepEqual(Object.keys(recovery).sort(), ["attemptId", "createdAt", "purpose"]);
  await page.reload();
  await visible("暂时无法读取授权结果");
  assert.equal(starts.length, 1);
  await page.getByRole("button", { name: "返回登录入口", exact: true }).click();
  await page.getByRole("button", { name: "使用原尝试重新发起授权", exact: true }).click();
  await visible("授权发起结果待确认");
  assert.equal(starts[1].id, firstId);
  records.push({ name: "unknown-start-refresh-and-explicit-same-key-retry", passed: true, provider: "browser API fixtures" });

  const id = "11111111-1111-4111-8111-111111111111";
  const result = { attemptId: id, purpose: "login", state: "confirmed", reasonCode: null, observedAt: timestamp, connection: null, nextPage: null };
  attempts.set(id, result);
  await page.goto(`${origin}/auth/result/${id}`);
  await visible("当前登录尚未确认");
  assert.equal(await page.getByRole("button", { name: "确认当前账号，返回工作区", exact: true }).count(), 0);
  records.push({ name: "confirmed-attempt-without-session-cannot-continue", passed: true, provider: "browser API fixtures" });

  authenticated = true;
  attempts.set(id, { ...result, purpose: "reconnect", state: "unknown", reasonCode: "EXCHANGE_UNCONFIRMED" });
  await page.goto(`${origin}/auth/result/${id}`);
  await visible("连接结果待确认");
  assert.equal(await page.getByRole("button", { name: "确认当前账号，返回工作区", exact: true }).count(), 0);
  await page.screenshot({ path: resolve(evidence, "reconnect-unknown.png"), fullPage: true });
  records.push({ name: "connected-session-does-not-confirm-reconnect", passed: true, provider: "browser API fixtures" });

  attempts.set(id, { ...result, purpose: "reconnect", connection: { committedRevision: "revision-a", isCurrent: true }, nextPage: "/" });
  await page.getByRole("button", { name: "查询本次连接结果", exact: true }).click();
  const proceed = page.getByRole("button", { name: "确认当前账号，返回工作区", exact: true });
  await proceed.waitFor({ state: "visible" });
  const readsBefore = reads.filter(path => path === "/api/session").length;
  continueLosesSession = true;
  await proceed.click();
  await visible("当前登录尚未确认");
  assert.ok(reads.filter(path => path === "/api/session").length > readsBefore);
  assert.ok(page.url().includes("/auth/result/"));
  continueLosesSession = false;
  records.push({ name: "explicit-continue-rechecks-session-and-receipt", passed: true, provider: "browser API fixtures" });

  authenticated = true;
  await page.goto(`${origin}/`);
  await visible("这一页暂时没有可披露的仓库");
  await page.getByRole("button", { name: "下一页", exact: true }).click();
  await visible("fixture/private-repository");
  await visible("已到当前发现结果末页，账号仍可能有未发现的仓库。");
  await page.screenshot({ path: resolve(evidence, "workspace-desktop.png"), fullPage: true });
  records.push({ name: "empty-page-cursor-and-partial-final-page", passed: true, provider: "browser API fixtures" });
  await page.setViewportSize({ width: 390, height: 844 });
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), true);
  await page.screenshot({ path: resolve(evidence, "workspace-mobile.png"), fullPage: true });
  await page.getByRole("button", { name: "退出登录", exact: true }).click();
  await visible("开始使用 RepoMesh");
  assert.equal(await page.getByText("fixture/private-repository", { exact: true }).count(), 0);
  assert.equal(await page.evaluate(() => sessionStorage.getItem("repomesh.auth.attempt")), null);
  await page.screenshot({ path: resolve(evidence, "login-mobile.png"), fullPage: true });
  await page.keyboard.press("Tab");
  assert.equal(await page.evaluate(() => document.activeElement?.tagName), "BUTTON");
  records.push({ name: "logout-clears-sensitive-view-mobile-and-keyboard", passed: true, provider: "browser API fixtures" });

  const polling = await context.newPage();
  await polling.clock.install();
  let queries = 0;
  let posts = 0;
  await polling.route(`${origin}/api/**`, async route => {
    const request = route.request();
    if (request.method() === "POST") posts++;
    if (new URL(request.url()).pathname === "/api/session") return route.fulfill({ json: user });
    queries++;
    if (queries === 1) return route.fulfill({ status: 429, json: { error: { code: "RATE_LIMITED" } }, headers: { "Retry-After": "2" } });
    return route.fulfill({ json: { ...result, purpose: "reconnect", state: "unknown", reasonCode: "EXCHANGE_UNCONFIRMED" } });
  });
  await polling.goto(`${origin}/auth/result/${id}`);
  try { await polling.getByText("暂时无法读取授权结果", { exact: true }).waitFor({timeout:5000}); }
  catch(error){ await writeFile(resolve(evidence,"polling-debug.json"),JSON.stringify({queries,posts,text:await polling.locator("body").innerText()},null,2));throw error; }
  assert.equal(queries, 1);
  await polling.clock.runFor(1000);
  assert.equal(queries, 1);
  await polling.clock.runFor(1100);
  await polling.getByText("连接结果待确认", { exact: true }).waitFor();
  await polling.evaluate(() => { Object.defineProperty(document, "visibilityState", { configurable: true, value: "hidden" }); document.dispatchEvent(new Event("visibilitychange")); });
  const beforeHidden = queries;
  await polling.clock.runFor(60000);
  assert.equal(queries, beforeHidden);
  await polling.evaluate(() => { Object.defineProperty(document, "visibilityState", { configurable: true, value: "visible" }); document.dispatchEvent(new Event("visibilitychange")); });
  await polling.clock.runFor(100);
  await polling.waitForFunction(() => document.querySelector("h1")?.textContent === "连接结果待确认");
  assert.ok(queries > beforeHidden);
  assert.equal(posts, 0);
  await polling.close();
  records.push({ name: "retry-after-cooldown-hidden-pause-and-no-post-retry", passed: true, provider: "browser API fixtures and controlled browser clock" });

  const stale = await context.newPage();
  let loggedIn = true;
  let releaseRepository;
  const heldRepository = new Promise(resolveHeld => { releaseRepository = resolveHeld; });
  let repositoryRequested;
  const enteredRepository = new Promise(resolveEntered => { repositoryRequested = resolveEntered; });
  await stale.route(`${origin}/api/**`, async route => {
    const path = new URL(route.request().url()).pathname;
    if (path === "/api/session") return route.fulfill({ status: loggedIn ? 200 : 401, json: loggedIn ? user : { error: { code: "AUTHENTICATION_REQUIRED" } } });
    if (path === "/api/auth/logout") { loggedIn = false; return route.fulfill({ status: 204, body: "" }); }
    if (path === "/api/repositories") {
      repositoryRequested();
      await heldRepository;
      return route.fulfill({ json: { ...repositories, items: [repo], nextCursor: null } });
    }
    return route.fulfill({ status: 404, json: { error: { code: "RESOURCE_NOT_FOUND" } } });
  });
  await stale.goto(origin);
  await enteredRepository;
  await stale.getByRole("button", { name: "退出登录", exact: true }).click();
  await stale.getByText("开始使用 RepoMesh", { exact: true }).waitFor();
  releaseRepository();
  await stale.waitForTimeout(100);
  assert.equal(await stale.getByText("fixture/private-repository", { exact: true }).count(), 0);
  await stale.close();
  records.push({ name: "late-repository-response-after-logout-is-discarded", passed: true, provider: "browser API fixtures" });
  assert.deepEqual(errors, []);
  await context.close();
  await writeFile(resolve(evidence, "checks.json"), JSON.stringify({ result: "PASS", recordedAt: new Date().toISOString(), browser: await browser.version(), checks: records, externalGitHub: "NOT_RUN" }, null, 2));
  process.stdout.write(`PASS ${records.length} browser scenarios; ${evidence}\n`);
} catch (error) {
  await writeFile(resolve(evidence, "checks.json"), JSON.stringify({ result: "FAIL", checks: records, error: String(error) }, null, 2));
  throw error;
} finally {
  if (browser) await browser.close();
  const stopped = once(server, "exit");
  server.kill("SIGTERM");
  await stopped;
}
