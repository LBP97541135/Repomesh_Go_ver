import assert from "node:assert/strict";
import { readFile, stat } from "node:fs/promises";
import { createRequire } from "node:module";
import { resolve } from "node:path";

const require = createRequire(import.meta.url);
const { chromium } = require("/tmp/repomesh-b02-browser/node_modules/playwright");
const executablePath = "/tmp/repomesh-b02-browser/browsers/chromium-1243/chrome-linux64/chrome";
const manifestPath = process.argv[2];
if (!manifestPath) throw new Error("usage: node browser-acceptance.mjs <manifest.json>");

const manifestFile = await stat(resolve(manifestPath));
assert.equal(manifestFile.mode & 0o077, 0, "manifest must not be group/world accessible");
const manifest = JSON.parse(await readFile(resolve(manifestPath), "utf8"));
const manifestKeys = Object.keys(manifest).sort();
assert.ok(
  JSON.stringify(manifestKeys) === JSON.stringify(["controlPath", "fixtures", "loginPath", "origin", "statusPath"])
    || JSON.stringify(manifestKeys) === JSON.stringify(["controlPath", "emptyCatalog", "fixtures", "loginPath", "origin", "statusPath"]),
  "manifest fields must match the approved browser fixture contract",
);
if ("emptyCatalog" in manifest) assert.equal(typeof manifest.emptyCatalog, "boolean");
const origin = new URL(manifest.origin);
assert.equal(origin.protocol, "https:");
assert.ok(origin.hostname === "127.0.0.1" || origin.hostname === "localhost" || origin.hostname === "::1");
assert.equal(manifest.loginPath, "/__test/login");
assert.equal(manifest.controlPath, "/__test/control");
assert.equal(manifest.statusPath, "/__test/status");

const events = [];
const network = [];
let currentStep = "launch";
const browser = await chromium.launch({ executablePath, headless: true });

function record(id, name, assertions) {
  events.push({ id, event: name, passed: true, assertions });
}

async function contextFor(viewport) {
  const context = await browser.newContext({ ignoreHTTPSErrors: true, viewport });
  context.on("response", async (response) => {
    const url = new URL(response.url());
    if (url.origin !== manifest.origin || !(/^\/api\/projects(?:\/|$)/u.test(url.pathname) || /^\/api\/project-creations\//u.test(url.pathname) || url.pathname === "/api/configuration-profiles" || url.pathname === "/api/repositories")) return;
    let code = null;
    if (response.status() >= 400) {
      try {
        const body = await response.json();
        code = typeof body?.error?.code === "string" ? body.error.code : null;
      } catch {
        code = null;
      }
    }
    network.push({
      method: response.request().method(),
      path: url.pathname,
      status: response.status(),
      code,
      key: response.request().headers()["idempotency-key"] ?? null,
    });
  });
  return context;
}

async function login(page, actor) {
  await page.goto(`${manifest.origin}${manifest.loginPath}?actor=${actor}`, { waitUntil: "domcontentloaded" });
  await page.getByRole("heading", { name: "仓库与账号连接" }).waitFor();
}

async function control(context, command) {
  const response = await context.request.post(`${manifest.origin}${manifest.controlPath}`, { data: command, headers: { Origin: manifest.origin } });
  assert.equal(response.status(), 200, `control ${command.action} must succeed`);
  const body = await response.json();
  assert.equal(body.accepted, true);
  return body;
}

async function status(context) {
  const response = await context.request.get(`${manifest.origin}${manifest.statusPath}`);
  assert.equal(response.status(), 200);
  return response.json();
}

async function waitStatus(context, field, expected = true) {
  const deadline = Date.now() + 10_000;
  while (Date.now() < deadline) {
    const current = await status(context);
    if (current[field] === expected) return current;
    await new Promise((resolveDelay) => setTimeout(resolveDelay, 50));
  }
  throw new Error(`status ${field} did not become ${expected}`);
}

async function waitForRepository(page, displayName) {
  const repository = page.locator(".selectable-list label").filter({ hasText: displayName });
  const deadline = Date.now() + 15_000;
  while (Date.now() < deadline) {
    if (await repository.count()) return repository;
    const retry = page.getByRole("button", { name: "重试读取" });
    if (await retry.count()) await retry.click();
    await page.waitForTimeout(100);
  }
  throw new Error(`repository ${displayName} did not become available`);
}

async function installTrackedFetch(context, rule) {
  assert.equal(typeof rule.method, "string");
  assert.equal(typeof rule.path, "string");
  assert.equal(typeof rule.label, "string");
  await context.addInitScript(({ method, path, label }) => {
    const state = window.__repomeshTrackedFetch ?? {};
    window.__repomeshTrackedFetch = state;
    const nativeFetch = window.fetch.bind(window);
    window.fetch = async (input, init) => {
      const request = input instanceof Request ? input : null;
      const url = new URL(request?.url ?? String(input), window.location.href);
      const actualMethod = (init?.method ?? request?.method ?? "GET").toUpperCase();
      if (actualMethod !== method || `${url.pathname}${url.search}` !== path) return nativeFetch(input, init);
      state[label] = { started: true, finished: false, status: null };
      const { signal: ignoredSignal, ...uncancellableInit } = init ?? {};
      void ignoredSignal;
      try {
        const response = await nativeFetch(input, uncancellableInit);
        state[label].status = response.status;
        void response.clone().arrayBuffer().catch(() => undefined).finally(() => {
          state[label].finished = true;
        });
        return response;
      } catch (error) {
        state[label].finished = true;
        throw error;
      }
    };
  }, rule);
}

async function waitForTrackedFetch(page, label) {
  await page.waitForFunction((trackedLabel) => window.__repomeshTrackedFetch?.[trackedLabel]?.finished === true, label);
  await page.evaluate(() => new Promise((resolveFrame) => requestAnimationFrame(() => requestAnimationFrame(resolveFrame))));
}

async function assertMinimalOperationIndexes(page) {
  const indexes = await page.evaluate(() => {
    const raw = localStorage.getItem("repomesh.project.operation-index.v1");
    return raw === null ? [] : JSON.parse(raw);
  });
  assert.equal(Array.isArray(indexes), true);
  for (const index of indexes) {
    assert.equal(index !== null && typeof index === "object" && !Array.isArray(index), true);
    const allowed = index.kind === "project_create"
      ? ["actor", "key", "kind", "recordedAt"]
      : ["actor", "key", "kind", "projectId", "recordedAt"];
    assert.deepEqual(Object.keys(index).sort(), allowed.sort());
    assert.equal(typeof index.actor, "string");
    assert.equal(typeof index.key, "string");
    assert.equal(typeof index.recordedAt, "string");
    assert.ok(index.kind === "project_create" || index.kind === "project_update");
    if (index.kind === "project_update") assert.equal(typeof index.projectId, "string");
  }
}

async function prepareCreate(page, { name, purpose = `用于 ${name} 的多仓协作。`, repository = "fixture-a/one", configure = false }) {
  await page.goto(`${manifest.origin}/projects`);
  await page.getByRole("button", { name: "新建项目" }).click();
  await (await waitForRepository(page, repository)).getByRole("checkbox").check();
  await page.getByRole("button", { name: "完成选择" }).click();
  await page.getByLabel("项目名称").fill(name);
  await page.getByLabel("项目用途").fill(purpose);
  if (configure) {
    await page.getByRole("radio", { name: /Fixture model/ }).check();
    await page.getByRole("radio", { name: /Fixture execution/ }).check();
  }
  await page.getByRole("button", { name: "查看保存摘要" }).click();
  await page.getByRole("heading", { name: "保存摘要" }).waitFor();
  const visibleRecoveryPath = page.locator(".recovery-path");
  const recoveryUrl = await visibleRecoveryPath.count()
    ? await visibleRecoveryPath.inputValue()
    : `${manifest.origin}${await page.evaluate(() => {
      const indexes = JSON.parse(localStorage.getItem("repomesh.project.operation-index.v1") ?? "[]");
      const item = indexes.filter((candidate) => candidate.kind === "project_create").at(-1);
      return `/project-creations/${item.key}`;
    })}`;
  const recoveryPath = new URL(recoveryUrl).pathname;
  return { recoveryPath, key: recoveryPath.split("/").at(-1) };
}

async function finishCreate(page) {
  const normal = page.getByRole("button", { name: "确认保存项目" });
  const storageFailure = page.getByRole("button", { name: "已保存链接，继续发送" });
  if (await normal.count()) await normal.click();
  else await storageFailure.click();
  await page.getByRole("heading", { name: "本次已保存" }).waitFor();
  const receiptRevision = await page.locator(".receipt code").first().textContent();
  const receiptTime = await page.locator(".receipt dd").nth(2).textContent();
  await page.getByRole("button", { name: "查看当前项目" }).click();
  return { projectPath: new URL(page.url()).pathname, receiptRevision, receiptTime };
}

async function projectSnapshot(context, projectPath) {
  const response = await context.request.get(`${manifest.origin}/api${projectPath}`);
  assert.equal(response.status(), 200);
  return response.json();
}

async function reviewAndSaveSettings(page) {
  await page.getByRole("button", { name: "查看本次改动" }).click();
  await page.getByRole("heading", { name: "本次改动摘要" }).waitFor();
  const normal = page.getByRole("button", { name: "确认保存修改" });
  const storageFailure = page.getByRole("button", { name: "已保存链接，继续发送" });
  if (await normal.count()) await normal.click();
  else await storageFailure.click();
  await page.getByRole("heading", { name: "本次已保存" }).waitFor();
}

async function assertNoProjectInputs(page) {
  const storage = await page.evaluate(() => ({ hasInput: Array.from({ length: sessionStorage.length }, (_, index) => sessionStorage.key(index)).some((key) => key?.startsWith("repomesh.project.operation.v1:")) }));
  assert.equal(storage.hasInput, false);
  await assertMinimalOperationIndexes(page);
}

async function verifyEmptyCatalog(context) {
  assert.equal(manifest.emptyCatalog, true);
  assert.equal(manifest.fixtures.modelProfile, "");
  assert.equal(manifest.fixtures.executionProfile, "");
  const page = await context.newPage();
  await login(page, "a");
  for (const kind of ["model", "execution"]) {
    const response = await context.request.get(`${manifest.origin}/api/configuration-profiles?${new URLSearchParams({ kind, limit: "50" })}`);
    assert.equal(response.status(), 200);
    const catalog = await response.json();
    assert.deepEqual(Object.keys(catalog).sort(), ["defaultProfileId", "items", "nextCursor"]);
    assert.deepEqual(catalog.items, []);
    assert.equal(catalog.nextCursor, null);
    assert.equal(catalog.defaultProfileId, null);
  }

  const createOperation = await prepareCreate(page, { name: "空目录待配置项目" });
  await page.getByText("当前没有可引用的模型配置。继承项仍可保存为待配置。", { exact: true }).waitFor();
  await page.getByText("当前没有可引用的执行配置。继承项仍可保存为待配置。", { exact: true }).waitFor();
  const createRequestPromise = page.waitForRequest((request) => request.method() === "POST" && new URL(request.url()).pathname === "/api/projects");
  const createResponsePromise = page.waitForResponse((response) => response.request().method() === "POST" && new URL(response.url()).pathname === "/api/projects" && response.request().headers()["idempotency-key"] === createOperation.key);
  const created = await finishCreate(page);
  const createRequest = await createRequestPromise;
  const createBody = createRequest.postDataJSON();
  assert.deepEqual(createBody.configuration, { modelProfile: { mode: "inherit" }, executionProfile: { mode: "inherit" } });
  assert.equal((await createResponsePromise).status(), 201);
  const emptyProject = await projectSnapshot(context, created.projectPath);
  assert.deepEqual(emptyProject.configuration.modelProfile, { mode: "inherit" });
  assert.deepEqual(emptyProject.configuration.executionProfile, { mode: "inherit" });
  assert.equal(typeof emptyProject.configuration.effective.configurationRevision, "string");
  assert.deepEqual({ ...emptyProject.configuration.effective, configurationRevision: null }, {
    configurationRevision: null,
    modelProfileId: null,
    executionProfileId: null,
    workerConcurrency: null,
    budgetPolicyId: null,
    timeLimitPolicyId: null,
    verificationGroupEnabled: null,
  });
  assert.equal(emptyProject.configuration.checks.status, "denied");
  assert.equal(emptyProject.actions.canCreateIssue, false);
  assert.notEqual(emptyProject.creationReadiness.status, "ready");
  const configurationPanel = page.locator("section.panel").filter({ has: page.getByRole("heading", { name: "固定配置引用" }) });
  assert.equal(await configurationPanel.getByText("未解析", { exact: true }).count(), 2);
  assert.equal(await configurationPanel.getByText("关闭", { exact: true }).count(), 0);

  await page.getByRole("button", { name: "项目设置" }).click();
  assert.equal(await page.getByLabel("本次更新配置引用").isChecked(), false);
  await page.getByLabel("项目用途").fill("空目录下只更新资料。");
  let updateRequestPromise = page.waitForRequest((request) => request.method() === "PATCH" && new URL(request.url()).pathname === `/api${created.projectPath}`);
  await reviewAndSaveSettings(page);
  let updateRequest = await updateRequestPromise;
  let updateBody = updateRequest.postDataJSON();
  assert.equal("configuration" in updateBody, false);
  await page.getByRole("button", { name: "查看当前项目" }).click();
  const metadataOnly = await projectSnapshot(context, created.projectPath);
  assert.equal(metadataOnly.configuration.effective.configurationRevision, emptyProject.configuration.effective.configurationRevision);

  await page.getByRole("button", { name: "项目设置" }).click();
  await page.getByLabel("本次更新配置引用").check();
  await page.getByText("当前没有可引用的模型配置。继承项仍可保存为待配置。", { exact: true }).waitFor();
  await page.getByText("当前没有可引用的执行配置。继承项仍可保存为待配置。", { exact: true }).waitFor();
  updateRequestPromise = page.waitForRequest((request) => request.method() === "PATCH" && new URL(request.url()).pathname === `/api${created.projectPath}`);
  await reviewAndSaveSettings(page);
  updateRequest = await updateRequestPromise;
  updateBody = updateRequest.postDataJSON();
  const updateKey = updateRequest.headers()["idempotency-key"];
  assert.equal(typeof updateKey, "string");
  assert.deepEqual(updateBody.configuration, { modelProfile: { mode: "inherit" }, executionProfile: { mode: "inherit" } });
  const updateRevision = await page.locator(".receipt code").first().textContent();
  const updateTime = await page.locator(".receipt dd").nth(2).textContent();
  assert.equal(updateRevision, metadataOnly.projectRevision);
  assert.equal(typeof updateTime, "string");
  await page.goto(`${manifest.origin}${createOperation.recoveryPath}`);
  await page.getByRole("heading", { name: "本次已保存" }).waitFor();
  assert.equal(await page.locator(".receipt code").first().textContent(), created.receiptRevision);
  await page.goto(`${manifest.origin}${created.projectPath}/updates/${updateKey}`);
  await page.getByRole("heading", { name: "本次已保存" }).waitFor();
  assert.equal(await page.locator(".receipt code").first().textContent(), updateRevision);
  assert.equal(await page.locator(".receipt dd").nth(2).textContent(), updateTime);
  record("EMPTY_CATALOG", "an empty configuration catalog saves and recovers an unresolved inherit project", {
    profileResponses: 2,
    defaultProfileIdsNull: true,
    createStatus: 201,
    inheritStored: true,
    unresolvedEffectiveFieldsNull: true,
    canCreateIssue: false,
    metadataUpdateOmittedConfiguration: true,
    explicitInheritAccepted: true,
    creationReceiptStable: true,
    updateReceiptStable: true,
  });
  await page.close();
}

try {
  if (manifest.emptyCatalog === true) {
    currentStep = "EMPTY_CATALOG empty configuration fixture";
    const emptyContext = await contextFor({ width: 1280, height: 900 });
    await verifyEmptyCatalog(emptyContext);
    await emptyContext.close();
    currentStep = "shutdown";
    const shutdownContext = await contextFor({ width: 800, height: 600 });
    await control(shutdownContext, { action: "shutdown" });
    await shutdownContext.close();
    assert.deepEqual(events.map((event) => event.id), ["EMPTY_CATALOG"]);
    console.log(JSON.stringify({ suite: "B03 frontend empty configuration catalog acceptance", passed: true, environment: "fresh Playwright context with the shared TLS harness", events, network }, null, 2));
  } else {
  currentStep = "UI01 initial states and create";
  const desktop = await contextFor({ width: 1440, height: 960 });
  const page = await desktop.newPage();
  await login(page, "a");
  await control(desktop, { action: "hold_response", method: "GET", path: "/api/projects?q=&limit=50" });
  await page.getByRole("button", { name: "项目", exact: true }).click();
  await page.getByText("正在读取项目…").waitFor();
  await waitStatus(desktop, "responseHeld", true);
  await control(desktop, { action: "release_response" });
  await page.getByText("还没有项目", { exact: true }).waitFor();
  const firstOperation = await prepareCreate(page, { name: "前端验收项目一", configure: true });
  const first = await finishCreate(page);
  await page.getByRole("heading", { name: "前端验收项目一" }).waitFor();
  await page.getByRole("link", { name: "项目", exact: true }).click();
  await page.getByRole("link", { name: "前端验收项目一" }).waitFor();
  await page.getByRole("link", { name: "前端验收项目一" }).click();
  const configurationStatus = page.locator(".status-copy");
  await configurationStatus.waitFor();
  assert.match(await configurationStatus.textContent() ?? "", /当前配置检查通过，但不表示模型调用或运行已接通。|项目已保存，必要配置尚未满足。|项目已保存，配置可用性待确认。/u);
  assert.equal(await page.getByRole("button", { name: /Issue|运行/u }).count(), 0);
  assert.equal(await page.getByRole("link", { name: /Issue|运行/u }).count(), 0);
  const firstStatus201 = network.some((item) => item.method === "POST" && item.path === "/api/projects" && item.status === 201 && item.key === firstOperation.key);
  assert.equal(firstStatus201, true);
  record("UI01", "two-step create, receipt, list, and detail", { firstStatus201, noIssueOrRunSuccess: true });

  currentStep = "UI02 explicit repository and profile updates";
  const fixedBefore = await projectSnapshot(desktop, first.projectPath);
  await page.getByRole("button", { name: "项目设置" }).click();
  await page.getByRole("heading", { name: "1. 项目资料" }).waitFor();
  assert.deepEqual(await page.locator("section.panel h2").allTextContents(), ["1. 项目资料", "2. 已保存仓库", "3. 配置引用"]);
  await page.getByRole("button", { name: "添加仓库" }).click();
  await (await waitForRepository(page, "fixture-b/two")).getByRole("checkbox").check();
  await page.route("**/api/repositories?*", async (route) => {
    const url = new URL(route.request().url());
    if (url.searchParams.get("q") !== "分页检查") return route.continue();
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({
      items: [],
      nextCursor: url.searchParams.has("cursor") ? null : "fixture-next-page",
      coverage: { status: "partial", reasonCodes: [], observedAt: new Date().toISOString() },
    }) });
  }, { times: 2 });
  await page.getByLabel("搜索仓库候选").fill("分页检查");
  await page.getByRole("button", { name: "搜索" }).click();
  await page.getByText("本页没有可披露仓库，仍可继续加载").waitFor();
  assert.equal(await page.getByText("fixture-b/two").count(), 1);
  await page.getByRole("button", { name: "加载更多" }).click();
  await page.getByText("当前发现结果没有匹配仓库").waitFor();
  assert.equal(await page.getByText("fixture-b/two").count(), 1);
  await page.getByRole("button", { name: "完成选择" }).click();
  await page.getByLabel("本次更新配置引用").check();
  await page.getByRole("radio", { name: /Fixture model/ }).check();
  await page.getByRole("radio", { name: /Fixture execution/ }).check();
  let patchRequest = page.waitForRequest((request) => request.method() === "PATCH" && new URL(request.url()).pathname === `/api${first.projectPath}`);
  await reviewAndSaveSettings(page);
  let completedPatchRequest = await patchRequest;
  let updateBody = completedPatchRequest.postDataJSON();
  assert.equal(updateBody.expectedProjectRevision, fixedBefore.projectRevision);
  assert.equal(Array.isArray(updateBody.repositoryIdsToAdd) && updateBody.repositoryIdsToAdd.length === 1, true);
  assert.equal("repositoryIds" in updateBody, false);
  assert.equal(typeof updateBody.configuration, "object");
  await page.getByRole("button", { name: "查看当前项目" }).click();
  const configured = await projectSnapshot(desktop, first.projectPath);
  await control(desktop, { action: "advance_profile", actor: "a", kind: "model" });
  await page.getByRole("button", { name: "项目设置" }).click();
  await page.getByLabel("项目用途").fill("只更新资料，不重新绑定配置。");
  patchRequest = page.waitForRequest((request) => request.method() === "PATCH" && new URL(request.url()).pathname === `/api${first.projectPath}`);
  await reviewAndSaveSettings(page);
  updateBody = (await patchRequest).postDataJSON();
  assert.equal(updateBody.expectedProjectRevision, configured.projectRevision);
  assert.equal("configuration" in updateBody, false);
  await page.getByRole("button", { name: "查看当前项目" }).click();
  const metadataOnly = await projectSnapshot(desktop, first.projectPath);
  assert.equal(metadataOnly.configuration.effective.configurationRevision, configured.configuration.effective.configurationRevision);
  await page.getByRole("button", { name: "项目设置" }).click();
  await page.getByLabel("本次更新配置引用").check();
  await reviewAndSaveSettings(page);
  await page.getByRole("button", { name: "查看当前项目" }).click();
  const rebound = await projectSnapshot(desktop, first.projectPath);
  assert.notEqual(rebound.configuration.effective.configurationRevision, configured.configuration.effective.configurationRevision);
  await page.getByRole("button", { name: "项目设置" }).click();
  await page.getByLabel("本次更新配置引用").check();
  const noChangeRevision = (await projectSnapshot(desktop, first.projectPath)).projectRevision;
  patchRequest = page.waitForRequest((request) => request.method() === "PATCH" && new URL(request.url()).pathname === `/api${first.projectPath}`);
  await reviewAndSaveSettings(page);
  completedPatchRequest = await patchRequest;
  const noChangeUpdateKey = completedPatchRequest.headers()["idempotency-key"];
  assert.equal(typeof noChangeUpdateKey, "string");
  const noChangeReceiptRevision = await page.locator(".receipt code").first().textContent();
  const noChangeReceiptTime = await page.locator(".receipt dd").nth(2).textContent();
  const noChangeUpdatePath = `${first.projectPath}/updates/${noChangeUpdateKey}`;
  assert.equal(noChangeReceiptRevision, noChangeRevision);
  assert.equal(typeof noChangeReceiptTime, "string");
  const originalConfigurationRevisionPresent = fixedBefore.configuration.effective.configurationRevision !== "";
  assert.equal(originalConfigurationRevisionPresent, true);
  record("UI02", "profile pinning, explicit add, pagination selection, metadata-only update, and no-change save", { originalConfigurationRevisionPresent, selectionSurvivedSearchAndEmptyCursorPage: true, metadataPreservedPinnedConfiguration: true, explicitConfigurationRebound: true, noChangeRevisionStable: true });

  currentStep = "UI02 inherit pinning and explicit rebind";
  const inheritOperation = await prepareCreate(page, { name: "继承默认固定项目" });
  const inheritProject = await finishCreate(page);
  assert.ok(inheritOperation.key);
  const inheritBefore = await projectSnapshot(desktop, inheritProject.projectPath);
  assert.deepEqual(inheritBefore.configuration.modelProfile, { mode: "inherit" });
  assert.deepEqual(inheritBefore.configuration.executionProfile, { mode: "inherit" });
  await control(desktop, { action: "advance_profile", actor: "a", kind: "model" });
  await page.getByRole("button", { name: "项目设置" }).click();
  await page.getByLabel("项目用途").fill("默认变化后只更新资料。");
  patchRequest = page.waitForRequest((request) => request.method() === "PATCH" && new URL(request.url()).pathname === `/api${inheritProject.projectPath}`);
  await reviewAndSaveSettings(page);
  updateBody = (await patchRequest).postDataJSON();
  assert.equal("configuration" in updateBody, false);
  await page.getByRole("button", { name: "查看当前项目" }).click();
  const inheritMetadataOnly = await projectSnapshot(desktop, inheritProject.projectPath);
  assert.equal(inheritMetadataOnly.configuration.effective.configurationRevision, inheritBefore.configuration.effective.configurationRevision);
  await page.getByRole("button", { name: "项目设置" }).click();
  await page.getByLabel("本次更新配置引用").check();
  patchRequest = page.waitForRequest((request) => request.method() === "PATCH" && new URL(request.url()).pathname === `/api${inheritProject.projectPath}`);
  await reviewAndSaveSettings(page);
  updateBody = (await patchRequest).postDataJSON();
  assert.deepEqual(updateBody.configuration, { modelProfile: { mode: "inherit" }, executionProfile: { mode: "inherit" } });
  await page.getByRole("button", { name: "查看当前项目" }).click();
  const inheritRebound = await projectSnapshot(desktop, inheritProject.projectPath);
  assert.notEqual(inheritRebound.configuration.effective.configurationRevision, inheritBefore.configuration.effective.configurationRevision);
  record("UI02", "inherit remains pinned until an explicit inherit update", { inheritSelectionStored: true, metadataKeptConfigurationRevision: true, explicitInheritRebound: true });

  currentStep = "UI02 restricted original scope";
  await page.goto(`${manifest.origin}${first.projectPath}`);
  await control(desktop, { action: "repository", repositoryId: manifest.fixtures.repositoryA, mode: "denied" });
  await page.getByRole("button", { name: "项目设置" }).click();
  await page.getByText(/本次未披露/).waitFor();
  const restrictedDisclosure = await page.evaluate(() => {
    const main = document.querySelector("main");
    if (main === null) return "";
    const attributes = Array.from(main.querySelectorAll("[aria-label], [title]")).flatMap((element) => [element.getAttribute("aria-label") ?? "", element.getAttribute("title") ?? ""]);
    return [main.innerText, ...attributes].join("\n");
  });
  assert.equal(restrictedDisclosure.includes("fixture-a/one"), false);
  const restrictedPageResponse = await desktop.request.get(`${manifest.origin}/api${first.projectPath}/repositories?limit=50`);
  assert.equal(restrictedPageResponse.status(), 200);
  const restrictedPage = await restrictedPageResponse.json();
  assert.equal(restrictedPage.restrictedRepositoryCount, 1);
  assert.deepEqual(restrictedPage.items.map((item) => item.id), [manifest.fixtures.repositoryB]);
  await page.getByLabel("项目用途").fill("原范围受限时仍只编辑资料。");
  patchRequest = page.waitForRequest((request) => request.method() === "PATCH" && new URL(request.url()).pathname === `/api${first.projectPath}`);
  await reviewAndSaveSettings(page);
  updateBody = (await patchRequest).postDataJSON();
  assert.deepEqual(Object.keys(updateBody).sort(), ["expectedProjectRevision", "purpose"]);
  await control(desktop, { action: "repository", repositoryId: manifest.fixtures.repositoryA, mode: "allowed" });
  const restoredScopeResponse = await desktop.request.get(`${manifest.origin}/api${first.projectPath}/repositories?limit=50`);
  assert.equal(restoredScopeResponse.status(), 200);
  const restoredScope = await restoredScopeResponse.json();
  assert.equal(restoredScope.restrictedRepositoryCount, 0);
  assert.deepEqual(restoredScope.items.map((item) => item.id).sort(), [manifest.fixtures.repositoryA, manifest.fixtures.repositoryB].sort());
  await page.goto(`${manifest.origin}${noChangeUpdatePath}`);
  await page.getByText(/记录后来有更新/).waitFor();
  assert.equal(await page.locator(".receipt code").first().textContent(), noChangeReceiptRevision);
  assert.equal(await page.locator(".receipt dd").nth(2).textContent(), noChangeReceiptTime);
  await page.reload();
  await page.getByText(/记录后来有更新/).waitFor();
  assert.equal(await page.locator(".receipt code").first().textContent(), noChangeReceiptRevision);
  assert.equal(await page.locator(".receipt dd").nth(2).textContent(), noChangeReceiptTime);
  const oldUpdateReads = network.filter((item) => item.method === "GET" && item.path === `/api${noChangeUpdatePath}` && item.status === 200);
  assert.ok(oldUpdateReads.length >= 2);
  record("UI02", "restricted saved scope survives metadata edit and old update receipts stay stable", { fullRepositoryReplacementAbsent: true, configurationAbsent: true, restrictedIdentityHidden: true, completeScopeRestored: true, oldUpdateReceiptStable: true, oldUpdateReadCount: oldUpdateReads.length });

  currentStep = "UI03 query before commit and dropped response";
  await installTrackedFetch(desktop, { method: "POST", path: "/api/projects", label: "ui03-create-drop" });
  const pendingPage = await desktop.newPage();
  await pendingPage.goto(`${manifest.origin}/projects`);
  const pending = await prepareCreate(pendingPage, { name: "提交前查询项目" });
  let pendingCreateRequests = 0;
  const countPendingCreate = (request) => {
    if (request.method() === "POST" && new URL(request.url()).pathname === "/api/projects" && request.headers()["idempotency-key"] === pending.key) pendingCreateRequests += 1;
  };
  desktop.on("request", countPendingCreate);
  await control(desktop, { action: "hold_request", method: "POST", path: "/api/projects" });
  await control(desktop, { action: "drop_response", method: "POST", path: "/api/projects" });
  await pendingPage.getByRole("button", { name: "确认保存项目" }).click();
  await waitStatus(desktop, "requestHeld", true);
  assert.equal(await pendingPage.getByLabel("项目名称").isDisabled(), true);
  assert.equal(await pendingPage.getByLabel("项目用途").isDisabled(), true);
  assert.equal(await pendingPage.getByRole("button", { name: "返回修改仓库" }).isDisabled(), true);
  assert.equal(await pendingPage.locator(".configuration-fields input").first().isDisabled(), true);
  const heldCreateIdentity = await pendingPage.evaluate(() => JSON.parse(localStorage.getItem("repomesh.project.operation-index.v1") ?? "[]").filter((candidate) => candidate.kind === "project_create").at(-1));
  assert.equal(heldCreateIdentity.key, pending.key);
  assert.equal(pendingCreateRequests, 1);
  const precommitResponse = await desktop.request.get(`${manifest.origin}/api/project-creations/${pending.key}`);
  assert.equal(precommitResponse.status(), 404);
  const precommitBody = await precommitResponse.json();
  assert.equal(precommitBody.error?.code, "PROJECT_CREATION_NOT_FOUND");
  const recoveryPage = await desktop.newPage();
  await recoveryPage.goto(`${manifest.origin}${pending.recoveryPath}`);
  await recoveryPage.getByText(/尚未查到原操作结果/).waitFor();
  assert.equal(await recoveryPage.getByRole("button", { name: "使用原键和原输入明确重试" }).count(), 0);
  assert.equal(new URL(recoveryPage.url()).pathname, pending.recoveryPath);
  await pendingPage.evaluate((recoveryPath) => {
    window.history.pushState(null, "", recoveryPath);
    window.dispatchEvent(new PopStateEvent("popstate"));
  }, pending.recoveryPath);
  await pendingPage.getByText(/尚未查到原操作结果/).waitFor();
  assert.equal(await pendingPage.getByRole("button", { name: "使用原键和原输入明确重试" }).count(), 1);
  assert.equal(new URL(pendingPage.url()).pathname, pending.recoveryPath);
  const beforeRelease = await status(desktop);
  assert.equal(beforeRelease.requestHeld, true);
  await control(desktop, { action: "release_request" });
  await waitStatus(desktop, "requestHeld", false);
  await waitForTrackedFetch(pendingPage, "ui03-create-drop");
  const droppedFetch = await pendingPage.evaluate(() => window.__repomeshTrackedFetch?.["ui03-create-drop"]);
  assert.equal(droppedFetch.started, true);
  assert.equal(droppedFetch.finished, true);
  assert.ok(droppedFetch.status === null || droppedFetch.status === 200);
  if (droppedFetch.status === 200) assert.equal(network.some((item) => item.method === "POST" && item.path === "/api/projects" && item.status === 200 && item.key === pending.key), true);
  await pendingPage.getByRole("button", { name: "查询原结果" }).click();
  await pendingPage.getByRole("heading", { name: "本次已保存" }).waitFor();
  await recoveryPage.getByRole("button", { name: "查询原结果" }).click();
  await recoveryPage.getByRole("heading", { name: "本次已保存" }).waitFor();
  assert.equal(pendingCreateRequests, 1);
  assert.equal(network.some((item) => item.method === "GET" && item.path === `/api/project-creations/${pending.key}` && item.status === 200), true);
  desktop.off("request", countPendingCreate);
  record("UI03", "404 before release and same-key recovery after a dropped create response", { precommitStatus: 404, precommitCode: "PROJECT_CREATION_NOT_FOUND", crossTabInputAbsent: true, originalTabInputPresent: true, requestHeldBefore404: true, dropResponseInjected: true, browserFinalWriteStatus: droppedFetch.status, originalKeyKept: true, createWriteCount: pendingCreateRequests, recoveredWithInput: true, recoveredWithoutInput: true });
  await recoveryPage.close();
  await pendingPage.close();

  currentStep = "UI03 stable old receipt";
  await page.goto(`${manifest.origin}${firstOperation.recoveryPath}`);
  await page.getByText(/记录后来有更新/).waitFor();
  assert.equal(await page.locator(".receipt code").first().textContent(), first.receiptRevision);
  assert.equal(await page.locator(".receipt dd").nth(2).textContent(), first.receiptTime);
  record("UI03", "old receipt remains immutable after later updates", { originalRevisionStable: true, originalTimeStable: true });

  currentStep = "UI03 dropped update response and same-key recovery";
  await installTrackedFetch(desktop, { method: "PATCH", path: `/api${first.projectPath}`, label: "ui03-update-drop" });
  await page.goto(`${manifest.origin}${first.projectPath}/settings`);
  await page.getByRole("heading", { name: "1. 项目资料" }).waitFor();
  await page.getByLabel("项目用途").fill("PATCH 丢响应后按原键恢复。");
  await page.getByRole("button", { name: "添加仓库" }).click();
  await page.locator(".picker").waitFor();
  await page.getByLabel("本次更新配置引用").check();
  await page.locator(".configuration-fields").waitFor();
  await page.getByRole("button", { name: "查看本次改动" }).click();
  const pendingUpdateIdentity = await page.evaluate(() => {
    const indexes = JSON.parse(localStorage.getItem("repomesh.project.operation-index.v1") ?? "[]");
    return indexes.filter((candidate) => candidate.kind === "project_update").at(-1);
  });
  assert.equal(pendingUpdateIdentity?.projectId, first.projectPath.split("/").at(-1));
  assert.equal(typeof pendingUpdateIdentity?.key, "string");
  const pendingUpdatePath = `${first.projectPath}/updates/${pendingUpdateIdentity.key}`;
  let pendingUpdateRequests = 0;
  const countPendingUpdate = (request) => {
    if (request.method() === "PATCH" && new URL(request.url()).pathname === `/api${first.projectPath}` && request.headers()["idempotency-key"] === pendingUpdateIdentity.key) pendingUpdateRequests += 1;
  };
  desktop.on("request", countPendingUpdate);
  await control(desktop, { action: "hold_request", method: "PATCH", path: `/api${first.projectPath}` });
  await control(desktop, { action: "drop_response", method: "PATCH", path: `/api${first.projectPath}` });
  await page.getByRole("button", { name: "确认保存修改" }).click();
  await waitStatus(desktop, "requestHeld", true);
  assert.equal(await page.locator(".picker").count(), 0);
  assert.equal(await page.getByLabel("项目名称").isDisabled(), true);
  assert.equal(await page.getByLabel("项目用途").isDisabled(), true);
  assert.equal(await page.getByRole("button", { name: "添加仓库" }).isDisabled(), true);
  assert.equal(await page.getByLabel("本次更新配置引用").isDisabled(), true);
  assert.equal(await page.locator(".configuration-fields input").first().isDisabled(), true);
  const heldUpdateIdentity = await page.evaluate(() => JSON.parse(localStorage.getItem("repomesh.project.operation-index.v1") ?? "[]").filter((candidate) => candidate.kind === "project_update").at(-1));
  assert.equal(heldUpdateIdentity.key, pendingUpdateIdentity.key);
  assert.equal(pendingUpdateRequests, 1);
  const precommitUpdateResponse = await desktop.request.get(`${manifest.origin}/api${pendingUpdatePath}`);
  assert.equal(precommitUpdateResponse.status(), 404);
  const precommitUpdateBody = await precommitUpdateResponse.json();
  assert.equal(precommitUpdateBody.error?.code, "PROJECT_UPDATE_NOT_FOUND");
  const noInputUpdatePage = await desktop.newPage();
  await noInputUpdatePage.goto(`${manifest.origin}${pendingUpdatePath}`);
  await noInputUpdatePage.getByText(/尚未查到原操作结果/).waitFor();
  assert.equal(await noInputUpdatePage.getByRole("button", { name: "使用原键和原输入明确重试" }).count(), 0);
  await page.evaluate((recoveryPath) => {
    window.history.pushState(null, "", recoveryPath);
    window.dispatchEvent(new PopStateEvent("popstate"));
  }, pendingUpdatePath);
  await page.getByText(/尚未查到原操作结果/).waitFor();
  assert.equal(await page.getByRole("button", { name: "使用原键和原输入明确重试" }).count(), 1);
  await control(desktop, { action: "release_request" });
  await waitStatus(desktop, "requestHeld", false);
  await waitForTrackedFetch(page, "ui03-update-drop");
  const droppedUpdateFetch = await page.evaluate(() => window.__repomeshTrackedFetch?.["ui03-update-drop"]);
  assert.equal(droppedUpdateFetch.started, true);
  assert.equal(droppedUpdateFetch.finished, true);
  assert.ok(droppedUpdateFetch.status === null || droppedUpdateFetch.status === 200);
  await page.getByRole("button", { name: "查询原结果" }).click();
  await page.getByRole("heading", { name: "本次已保存" }).waitFor();
  await noInputUpdatePage.getByRole("button", { name: "查询原结果" }).click();
  await noInputUpdatePage.getByRole("heading", { name: "本次已保存" }).waitFor();
  assert.equal(pendingUpdateRequests, 1);
  desktop.off("request", countPendingUpdate);
  record("UI03", "a real dropped PATCH response recovers the original update key", { precommitStatus: 404, precommitCode: "PROJECT_UPDATE_NOT_FOUND", originalTabInputPresent: true, crossTabInputAbsent: true, dropResponseInjected: true, browserFinalWriteStatus: droppedUpdateFetch.status, updateWriteCount: pendingUpdateRequests, originalKeyKept: true, recoveredWithInput: true, recoveredWithoutInput: true });
  await noInputUpdatePage.close();

  currentStep = "UI03 committed create with a client-dropped response";
  let clientDroppedCreateStatus = null;
  let finishClientDroppedCreate;
  const clientDroppedCreateFinished = new Promise((resolveFinished) => { finishClientDroppedCreate = resolveFinished; });
  const clientDroppedCreate = await prepareCreate(page, { name: "客户端丢创建响应" });
  await page.route("**/api/projects", async (route) => {
    if (route.request().method() !== "POST") return route.continue();
    const response = await route.fetch();
    clientDroppedCreateStatus = response.status();
    await response.body();
    await route.abort("failed");
    finishClientDroppedCreate();
  }, { times: 1 });
  let clientDroppedCreateWrites = 0;
  page.on("request", (request) => {
    if (request.method() === "POST" && new URL(request.url()).pathname === "/api/projects" && request.headers()["idempotency-key"] === clientDroppedCreate.key) clientDroppedCreateWrites += 1;
  });
  await page.getByRole("button", { name: "确认保存项目" }).click();
  await clientDroppedCreateFinished;
  assert.equal(clientDroppedCreateStatus, 201);
  const clientCreateRecoveryLink = page.getByRole("link", { name: "查询原创建结果" });
  await clientCreateRecoveryLink.waitFor();
  assert.equal(await clientCreateRecoveryLink.getAttribute("href"), clientDroppedCreate.recoveryPath);
  assert.equal(await page.getByLabel("项目名称").isDisabled(), true);
  assert.equal(await page.getByLabel("项目用途").isDisabled(), true);
  assert.equal(await page.getByRole("button", { name: "返回修改仓库" }).isDisabled(), true);
  assert.equal(await page.locator(".configuration-fields input").first().isDisabled(), true);
  assert.equal(await clientCreateRecoveryLink.getAttribute("href"), clientDroppedCreate.recoveryPath);
  assert.equal(clientDroppedCreateWrites, 1);
  await clientCreateRecoveryLink.click();
  await page.getByRole("heading", { name: "本次已保存" }).waitFor();
  record("UI03", "a client-dropped committed create response enters unknown and recovers by the original key", { realHandlerStatus: 201, clientFailureSource: "Playwright route.abort after route.fetch", unknownLinkRendered: true, originalKeyKept: true, createWriteCount: clientDroppedCreateWrites, recovered: true });

  currentStep = "UI03 committed update with a client-dropped response";
  const clientDroppedCreateReceiptResponse = await desktop.request.get(`${manifest.origin}/api/project-creations/${clientDroppedCreate.key}`);
  assert.equal(clientDroppedCreateReceiptResponse.status(), 200);
  const clientDroppedCreateReceipt = await clientDroppedCreateReceiptResponse.json();
  const clientDroppedProjectId = clientDroppedCreateReceipt.projectId;
  assert.equal(typeof clientDroppedProjectId, "string");
  await page.getByRole("button", { name: "查看当前项目" }).click();
  await page.getByRole("button", { name: "项目设置" }).click();
  await page.getByLabel("项目用途").fill("客户端丢 PATCH 响应后按原键恢复。");
  await page.getByRole("button", { name: "添加仓库" }).click();
  await page.locator(".picker").waitFor();
  await page.getByLabel("本次更新配置引用").check();
  await page.locator(".configuration-fields").waitFor();
  await page.getByRole("button", { name: "查看本次改动" }).click();
  let clientDroppedUpdateStatus = null;
  let finishClientDroppedUpdate;
  const clientDroppedUpdateFinished = new Promise((resolveFinished) => { finishClientDroppedUpdate = resolveFinished; });
  await page.route(`**/api/projects/${clientDroppedProjectId}`, async (route) => {
    if (route.request().method() !== "PATCH") return route.continue();
    const response = await route.fetch();
    clientDroppedUpdateStatus = response.status();
    await response.body();
    await route.abort("failed");
    finishClientDroppedUpdate();
  }, { times: 1 });
  const clientDroppedUpdateRequest = page.waitForRequest((request) => request.method() === "PATCH" && new URL(request.url()).pathname === `/api/projects/${clientDroppedProjectId}`);
  let clientUpdatePageRequests = 0;
  page.on("request", (request) => {
    if (request.method() === "PATCH" && new URL(request.url()).pathname === `/api/projects/${clientDroppedProjectId}`) clientUpdatePageRequests += 1;
  });
  await page.getByRole("button", { name: "确认保存修改" }).click();
  const clientUpdateRequest = await clientDroppedUpdateRequest;
  const clientDroppedUpdateKey = clientUpdateRequest.headers()["idempotency-key"];
  assert.equal(typeof clientDroppedUpdateKey, "string");
  await clientDroppedUpdateFinished;
  assert.equal(clientDroppedUpdateStatus, 200);
  const clientUpdateRecoveryLink = page.getByRole("link", { name: "查询原更新结果" });
  await clientUpdateRecoveryLink.waitFor();
  assert.equal(await clientUpdateRecoveryLink.getAttribute("href"), `/projects/${clientDroppedProjectId}/updates/${clientDroppedUpdateKey}`);
  assert.equal(await page.locator(".picker").count(), 0);
  assert.equal(await page.getByLabel("项目名称").isDisabled(), true);
  assert.equal(await page.getByLabel("项目用途").isDisabled(), true);
  assert.equal(await page.getByRole("button", { name: "添加仓库" }).isDisabled(), true);
  assert.equal(await page.getByLabel("本次更新配置引用").isDisabled(), true);
  assert.equal(await page.locator(".configuration-fields input").first().isDisabled(), true);
  assert.equal(await clientUpdateRecoveryLink.getAttribute("href"), `/projects/${clientDroppedProjectId}/updates/${clientDroppedUpdateKey}`);
  await clientUpdateRecoveryLink.click();
  await page.getByRole("heading", { name: "本次已保存" }).waitFor();
  assert.equal(clientUpdatePageRequests, 1);
  record("UI03", "a client-dropped committed PATCH response enters unknown and recovers by the original key", { realHandlerStatus: 200, clientFailureSource: "Playwright route.abort after route.fetch", unknownLinkRendered: true, originalKeyKept: true, updateWriteCount: clientUpdatePageRequests, recovered: true });

  currentStep = "UI03 repository subresource loss clears project overview";
  const missingOverview = { projectPath: `/projects/${clientDroppedProjectId}` };
  await page.goto(`${manifest.origin}${missingOverview.projectPath}`);
  await page.getByRole("heading", { name: "客户端丢创建响应" }).waitFor();
  await page.getByText("fixture-a/one", { exact: true }).waitFor();
  await control(desktop, { action: "remove_project", path: `/api${missingOverview.projectPath}` });
  const missingOverviewResponse = page.waitForResponse((response) => response.request().method() === "GET" && new URL(response.url()).pathname === `/api${missingOverview.projectPath}/repositories` && response.status() === 404);
  await page.getByRole("button", { name: "刷新" }).click();
  assert.equal((await missingOverviewResponse).status(), 404);
  await page.getByText("当前无法访问该项目。").waitFor();
  assert.equal(await page.getByRole("heading", { name: "客户端丢创建响应" }).count(), 0);
  assert.equal(await page.getByText("客户端丢 PATCH 响应后按原键恢复。", { exact: true }).count(), 0);
  assert.equal(await page.getByText("fixture-a/one", { exact: true }).count(), 0);
  record("UI03", "a real repository subresource 404 immediately clears the readable project shell", { firstPageFromHandler: true, repositoryStatus: 404, projectNameCleared: true, purposeCleared: true, repositoryCleared: true, inaccessibleShown: true });

  currentStep = "UI03 repository next-page loss clears project settings";
  const missingSettings = inheritProject;
  await control(desktop, { action: "expand_project", path: `/api${missingSettings.projectPath}` });
  await page.goto(`${manifest.origin}${missingSettings.projectPath}/settings`);
  await page.getByRole("heading", { name: "继承默认固定项目" }).waitFor();
  await page.getByText("fixture-a/one", { exact: true }).waitFor();
  const loadMissingSettings = page.getByRole("button", { name: "加载更多已保存仓库" });
  await loadMissingSettings.waitFor();
  await control(desktop, { action: "remove_project", path: `/api${missingSettings.projectPath}` });
  const missingSettingsResponse = page.waitForResponse((response) => response.request().method() === "GET" && new URL(response.url()).pathname === `/api${missingSettings.projectPath}/repositories` && response.status() === 404);
  await loadMissingSettings.click();
  assert.equal((await missingSettingsResponse).status(), 404);
  await page.getByText("当前身份无法继续访问该项目，项目输入已清理。").waitFor();
  assert.equal(await page.getByRole("heading", { name: "继承默认固定项目" }).count(), 0);
  assert.equal(await page.getByLabel("项目用途").count(), 0);
  assert.equal(await page.getByText("fixture-a/one", { exact: true }).count(), 0);
  record("UI03", "a real repository next-page 404 immediately clears settings and its draft", { firstPageFromHandler: true, repositoryStatus: 404, projectNameCleared: true, draftCleared: true, repositoryCleared: true, inaccessibleShown: true });

  currentStep = "UI04 session storage failure";
  const sessionFailure = await contextFor({ width: 1280, height: 900 });
  await sessionFailure.addInitScript(() => {
    window.__repomeshStorageRefusal = { sessionAttempted: false, sessionRejected: false, clipboardRejected: false };
    const original = Storage.prototype.setItem;
    Storage.prototype.setItem = function setItem(key, value) {
      if (this === sessionStorage && key.startsWith("repomesh.project.operation.v1:")) {
        window.__repomeshStorageRefusal.sessionAttempted = true;
        window.__repomeshStorageRefusal.sessionRejected = true;
        throw new DOMException("disabled", "SecurityError");
      }
      return original.call(this, key, value);
    };
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText: async () => {
      window.__repomeshStorageRefusal.clipboardRejected = true;
      throw new DOMException("disabled", "NotAllowedError");
    } } });
  });
  const sessionFailurePage = await sessionFailure.newPage();
  await login(sessionFailurePage, "a");
  let writesBeforeContinue = 0;
  sessionFailurePage.on("request", (request) => { if (request.method() === "POST" && new URL(request.url()).pathname === "/api/projects") writesBeforeContinue += 1; });
  const sessionFailureOperation = await prepareCreate(sessionFailurePage, { name: "sessionStorage 失败" });
  await sessionFailurePage.getByText(/浏览器无法可靠保存原输入/).waitFor();
  assert.equal(writesBeforeContinue, 0);
  const refusalBeforeSend = await sessionFailurePage.evaluate(() => window.__repomeshStorageRefusal);
  assert.equal(refusalBeforeSend.sessionAttempted, true);
  assert.equal(refusalBeforeSend.sessionRejected, true);
  const visibleSessionRecovery = sessionFailurePage.locator(".recovery-path");
  assert.equal(new URL(await visibleSessionRecovery.inputValue()).pathname, sessionFailureOperation.recoveryPath);
  assert.equal(sessionFailureOperation.recoveryPath.startsWith("/api/"), false);
  await sessionFailurePage.getByRole("button", { name: "复制恢复链接" }).click();
  await sessionFailurePage.waitForFunction(() => window.__repomeshStorageRefusal?.clipboardRejected === true);
  await visibleSessionRecovery.focus();
  const selectableRecovery = await visibleSessionRecovery.evaluate((input) => input.selectionStart === 0 && input.selectionEnd === input.value.length);
  assert.equal(selectableRecovery, true);
  await sessionFailurePage.getByRole("button", { name: "已保存链接，继续发送" }).click();
  await sessionFailurePage.getByRole("heading", { name: "本次已保存" }).waitFor();
  assert.equal(writesBeforeContinue, 1);
  record("UI04", "session storage and clipboard refusal show a selectable browser link before an explicit send", { storageWriteRejected: true, writeCountBeforeContinue: 0, browserRecoveryPath: true, clipboardRejected: true, selectableFallback: true, explicitContinueRequired: true });
  await sessionFailure.close();

  currentStep = "UI04 local index failure and pending request logout";
  const localFailure = await contextFor({ width: 1280, height: 900 });
  await installTrackedFetch(localFailure, { method: "POST", path: "/api/projects", label: "ui04-pending-401" });
  await localFailure.addInitScript(() => {
    window.__repomeshStorageRefusal = { localAttempted: false, localRejected: false };
    const original = Storage.prototype.setItem;
    Storage.prototype.setItem = function setItem(key, value) {
      if (this === localStorage && key === "repomesh.project.operation-index.v1") {
        window.__repomeshStorageRefusal.localAttempted = true;
        window.__repomeshStorageRefusal.localRejected = true;
        throw new DOMException("disabled", "SecurityError");
      }
      return original.call(this, key, value);
    };
  });
  const localFailurePage = await localFailure.newPage();
  await login(localFailurePage, "a");
  await prepareCreate(localFailurePage, { name: "localStorage 失败" });
  assert.equal(await localFailurePage.locator(".recovery-path").count(), 1);
  const localRefusal = await localFailurePage.evaluate(() => window.__repomeshStorageRefusal);
  assert.equal(localRefusal.localAttempted, true);
  assert.equal(localRefusal.localRejected, true);
  await control(localFailure, { action: "hold_request", method: "POST", path: "/api/projects" });
  await localFailurePage.getByRole("button", { name: "已保存链接，继续发送" }).click();
  await waitStatus(localFailure, "requestHeld", true);
  const logout204 = localFailurePage.waitForResponse((response) => response.request().method() === "POST" && new URL(response.url()).pathname === "/api/auth/logout");
  await localFailurePage.getByRole("button", { name: "退出登录" }).click();
  assert.equal((await logout204).status(), 204);
  await assertNoProjectInputs(localFailurePage);
  const pending401 = localFailurePage.waitForResponse((response) => response.request().method() === "POST" && new URL(response.url()).pathname === "/api/projects");
  await control(localFailure, { action: "release_request" });
  await waitStatus(localFailure, "requestHeld", false);
  const pending401Response = await pending401;
  await pending401Response.finished();
  assert.equal(pending401Response.status(), 401);
  await waitForTrackedFetch(localFailurePage, "ui04-pending-401");
  const pending401Track = await localFailurePage.evaluate(() => window.__repomeshTrackedFetch?.["ui04-pending-401"]);
  assert.deepEqual(pending401Track, { started: true, finished: true, status: 401 });
  await assertNoProjectInputs(localFailurePage);
  assert.equal(await localFailurePage.getByText("localStorage 失败", { exact: true }).count(), 0);
  record("UI04", "local index refusal still permits sensitive input cleanup", { localIndexWriteRejected: true, sessionInputClearedWithoutIndex: true });
  record("UI05", "a pending request released after logout returns a real 401 without restoring state", { logoutStatus: 204, releasedRequestStatus: 401, cancellationDisabledForRequest: true, stateStayedCleared: true, storageStayedCleared: true });
  await localFailure.close();

  currentStep = "UI05 late successful response after logout";
  const lateSuccess = await contextFor({ width: 1280, height: 900 });
  await installTrackedFetch(lateSuccess, { method: "POST", path: "/api/projects", label: "ui05-late-post-201" });
  const lateSuccessPage = await lateSuccess.newPage();
  await login(lateSuccessPage, "a");
  const lateSuccessOperation = await prepareCreate(lateSuccessPage, { name: "迟到成功回执" });
  await control(lateSuccess, { action: "hold_response", method: "POST", path: "/api/projects" });
  await lateSuccessPage.getByRole("button", { name: "确认保存项目" }).click();
  await waitStatus(lateSuccess, "responseHeld", true);
  const lateLogout204 = lateSuccessPage.waitForResponse((response) => response.request().method() === "POST" && new URL(response.url()).pathname === "/api/auth/logout");
  await lateSuccessPage.getByRole("button", { name: "退出登录" }).click();
  assert.equal((await lateLogout204).status(), 204);
  await assertNoProjectInputs(lateSuccessPage);
  const latePost201 = lateSuccessPage.waitForResponse((response) => response.request().method() === "POST" && new URL(response.url()).pathname === "/api/projects" && response.request().headers()["idempotency-key"] === lateSuccessOperation.key);
  await control(lateSuccess, { action: "release_response" });
  await waitStatus(lateSuccess, "responseHeld", false);
  const latePostResponse = await latePost201;
  await latePostResponse.finished();
  assert.equal(latePostResponse.status(), 201);
  await waitForTrackedFetch(lateSuccessPage, "ui05-late-post-201");
  const latePostTrack = await lateSuccessPage.evaluate(() => window.__repomeshTrackedFetch?.["ui05-late-post-201"]);
  assert.deepEqual(latePostTrack, { started: true, finished: true, status: 201 });
  await assertNoProjectInputs(lateSuccessPage);
  assert.equal(await lateSuccessPage.getByText("迟到成功回执", { exact: true }).count(), 0);
  assert.equal(await lateSuccessPage.getByRole("heading", { name: "本次已保存" }).count(), 0);
  assert.equal(network.some((item) => item.method === "POST" && item.path === "/api/projects" && item.status === 201 && item.key === lateSuccessOperation.key), true);
  record("UI05", "a real successful response released after logout cannot restore state or storage", {
    requestCommittedBeforeLogout: true,
    responseStatus: 201,
    requestKey: lateSuccessOperation.key,
    logoutStatus: 204,
    fetchCancellationDisabledForThisRequest: true,
    stateStayedCleared: true,
    storageStayedCleared: true,
  });
  await lateSuccess.close();

  currentStep = "UI05 late successful PATCH response after logout";
  const lateUpdate = await contextFor({ width: 1280, height: 900 });
  const lateUpdatePage = await lateUpdate.newPage();
  await login(lateUpdatePage, "a");
  const lateUpdateCreate = await prepareCreate(lateUpdatePage, { name: "迟到 PATCH 基础项目" });
  const lateUpdateProject = await finishCreate(lateUpdatePage);
  assert.ok(lateUpdateCreate.key);
  await installTrackedFetch(lateUpdate, { method: "PATCH", path: `/api${lateUpdateProject.projectPath}`, label: "ui05-late-patch-200" });
  await lateUpdatePage.goto(`${manifest.origin}${lateUpdateProject.projectPath}/settings`);
  await lateUpdatePage.getByRole("heading", { name: "1. 项目资料" }).waitFor();
  await lateUpdatePage.getByLabel("项目名称").fill("迟到 PATCH 不得复活");
  await lateUpdatePage.getByRole("button", { name: "查看本次改动" }).click();
  const latePatchRequestPromise = lateUpdatePage.waitForRequest((request) => request.method() === "PATCH" && new URL(request.url()).pathname === `/api${lateUpdateProject.projectPath}`);
  await control(lateUpdate, { action: "hold_response", method: "PATCH", path: `/api${lateUpdateProject.projectPath}` });
  await lateUpdatePage.getByRole("button", { name: "确认保存修改" }).click();
  const latePatchRequest = await latePatchRequestPromise;
  const latePatchKey = latePatchRequest.headers()["idempotency-key"];
  assert.equal(typeof latePatchKey, "string");
  await waitStatus(lateUpdate, "responseHeld", true);
  const latePatchOperationPath = `/api${lateUpdateProject.projectPath}/updates/${latePatchKey}`;
  const latePatchReadOne = await lateUpdate.request.get(`${manifest.origin}${latePatchOperationPath}`);
  const latePatchReadTwo = await lateUpdate.request.get(`${manifest.origin}${latePatchOperationPath}`);
  assert.equal(latePatchReadOne.status(), 200);
  assert.equal(latePatchReadTwo.status(), 200);
  const latePatchReceiptOne = await latePatchReadOne.json();
  const latePatchReceiptTwo = await latePatchReadTwo.json();
  assert.equal(latePatchReceiptOne.updateId, latePatchKey);
  assert.equal(latePatchReceiptOne.projectId, lateUpdateProject.projectPath.split("/").at(-1));
  assert.equal(latePatchReceiptTwo.projectRevision, latePatchReceiptOne.projectRevision);
  assert.equal(latePatchReceiptTwo.updatedAt, latePatchReceiptOne.updatedAt);
  const latePatchLogout204 = lateUpdatePage.waitForResponse((response) => response.request().method() === "POST" && new URL(response.url()).pathname === "/api/auth/logout");
  await lateUpdatePage.getByRole("button", { name: "退出登录" }).click();
  assert.equal((await latePatchLogout204).status(), 204);
  await assertNoProjectInputs(lateUpdatePage);
  const latePatch200 = lateUpdatePage.waitForResponse((response) => response.request().method() === "PATCH" && new URL(response.url()).pathname === `/api${lateUpdateProject.projectPath}` && response.request().headers()["idempotency-key"] === latePatchKey);
  await control(lateUpdate, { action: "release_response" });
  await waitStatus(lateUpdate, "responseHeld", false);
  const latePatchResponse = await latePatch200;
  await latePatchResponse.finished();
  assert.equal(latePatchResponse.status(), 200);
  await waitForTrackedFetch(lateUpdatePage, "ui05-late-patch-200");
  const latePatchTrack = await lateUpdatePage.evaluate(() => window.__repomeshTrackedFetch?.["ui05-late-patch-200"]);
  assert.deepEqual(latePatchTrack, { started: true, finished: true, status: 200 });
  await assertNoProjectInputs(lateUpdatePage);
  assert.equal(await lateUpdatePage.getByText("迟到 PATCH 不得复活", { exact: true }).count(), 0);
  assert.equal(await lateUpdatePage.getByRole("heading", { name: "本次已保存" }).count(), 0);
  const latePatchWrites = network.filter((item) => item.method === "PATCH" && item.path === `/api${lateUpdateProject.projectPath}` && item.key === latePatchKey && item.status === 200);
  assert.equal(latePatchWrites.length, 1);
  record("UI05", "a committed PATCH 200 released after logout cannot restore state or storage", { logoutStatus: 204, responseStatus: 200, requestKey: latePatchKey, fetchCancellationDisabledForThisRequest: true, stateStayedCleared: true, storageStayedCleared: true, originalKeyReadTwice: true, updateWriteCount: latePatchWrites.length });
  await lateUpdate.close();

  currentStep = "UI05 actor isolation and 404 code split";
  const actorContext = await contextFor({ width: 1280, height: 900 });
  const actorPage = await actorContext.newPage();
  await login(actorPage, "a");
  const actorAOperation = await prepareCreate(actorPage, { name: "Actor A 未知操作" });
  await control(actorContext, { action: "drop_response", method: "POST", path: "/api/projects" });
  await actorPage.getByRole("button", { name: "确认保存项目" }).click();
  let actorAReceiptResponse = null;
  for (let attempt = 0; attempt < 100; attempt += 1) {
    const candidate = await actorContext.request.get(`${manifest.origin}/api/project-creations/${actorAOperation.key}`);
    if (candidate.status() === 200) {
      actorAReceiptResponse = candidate;
      break;
    }
    assert.equal(candidate.status(), 404);
    await new Promise((resolveDelay) => setTimeout(resolveDelay, 50));
  }
  assert.notEqual(actorAReceiptResponse, null);
  assert.equal(actorAReceiptResponse.status(), 200);
  const actorAReceipt = await actorAReceiptResponse.json();
  assert.equal(actorAReceipt.links.project, `/api/projects/${actorAReceipt.projectId}`);
  await login(actorPage, "b");
  await actorPage.goto(`${manifest.origin}${actorAOperation.recoveryPath}`);
  await actorPage.getByText(/尚未查到原操作结果/).waitFor();
  assert.equal(await actorPage.getByRole("button", { name: "使用原键和原输入明确重试" }).count(), 0);
  await assertNoProjectInputs(actorPage);
  assert.equal((await actorPage.locator("main").innerText()).includes("Actor A 未知操作"), false);
  assert.equal(network.some((item) => item.method === "GET" && item.path === `/api/project-creations/${actorAOperation.key}` && item.status === 404 && item.code === "PROJECT_CREATION_NOT_FOUND"), true);
  record("UI05", "actor change hides another actor's input while an unknown operation key stays query-only", {
    actorAInputLoadedByActorB: false,
    realHttpCode: "PROJECT_CREATION_NOT_FOUND",
    retryWriteAvailable: false,
  });
  const actorProject404 = actorPage.waitForResponse((response) => response.request().method() === "GET" && new URL(response.url()).pathname === actorAReceipt.links.project);
  await actorPage.goto(`${manifest.origin}/projects/${actorAReceipt.projectId}`);
  const actorProject404Response = await actorProject404;
  assert.equal(actorProject404Response.status(), 404);
  await actorPage.getByText("当前无法访问该项目。").waitFor();
  assert.equal(await actorPage.getByRole("heading", { name: "Actor A 未知操作" }).count(), 0);
  record("UI06", "real HTTP operation and resource 404 codes have distinct UI outcomes", {
    projectCreationNotFoundStayedUnknown: true,
    resourceNotFoundClearedTarget: true,
  });
  await actorContext.close();

  currentStep = "UI05 current 401 cleanup";
  const unauthorizedContext = await contextFor({ width: 1280, height: 900 });
  const unauthorizedPage = await unauthorizedContext.newPage();
  await login(unauthorizedPage, "b");
  await prepareCreate(unauthorizedPage, { name: "401 清理输入" });
  await control(unauthorizedContext, { action: "revoke_session", actor: "b" });
  const currentSession401 = unauthorizedPage.waitForResponse((response) => response.request().method() === "GET" && new URL(response.url()).pathname === "/api/session" && response.status() === 401);
  await unauthorizedPage.goto(`${manifest.origin}/projects`);
  const currentSession401Response = await currentSession401;
  await currentSession401Response.finished();
  assert.equal(currentSession401Response.status(), 401);
  await unauthorizedPage.getByRole("heading", { name: "开始使用 RepoMesh" }).waitFor();
  assert.equal(await unauthorizedPage.getByText("正在检查登录状态…", { exact: true }).count(), 0);
  await assertNoProjectInputs(unauthorizedPage);
  record("UI05", "a completed current session 401 clears sensitive operation input", { sessionStatus: 401, anonymousPageReached: true, sensitiveInputCleared: true });
  await unauthorizedContext.close();

  currentStep = "UI05 same-query and cross-project late responses";
  const secondOperation = await prepareCreate(page, { name: "前端验收项目二" });
  const second = await finishCreate(page);
  assert.ok(secondOperation.key);
  const oldRepositoryQueryPath = `/api/repositories?${new URLSearchParams({ q: "旧仓库", limit: "50" })}`;
  await installTrackedFetch(desktop, { method: "GET", path: oldRepositoryQueryPath, label: "ui05-old-repository-200" });
  await installTrackedFetch(desktop, { method: "GET", path: `/api${first.projectPath}`, label: "ui05-old-project-200" });
  await page.goto(`${manifest.origin}${second.projectPath}/settings`);
  await page.getByRole("button", { name: "添加仓库" }).click();
  await waitForRepository(page, "fixture-a/one");
  let finishOldRepositoryHandler;
  const oldRepositoryHandlerReady = new Promise((resolveReady) => { finishOldRepositoryHandler = resolveReady; });
  let releaseOldRepositoryResponse;
  const oldRepositoryResponseRelease = new Promise((resolveRelease) => { releaseOldRepositoryResponse = resolveRelease; });
  let finishOldRepositoryRoute;
  const oldRepositoryRouteFinished = new Promise((resolveFinished) => { finishOldRepositoryRoute = resolveFinished; });
  let oldRepositoryHandlerStatus = null;
  await page.route(`${manifest.origin}${oldRepositoryQueryPath}`, async (route) => {
    let response = null;
    for (let attempt = 0; attempt < 100; attempt += 1) {
      response = await route.fetch();
      if (response.status() === 200) break;
      assert.equal(response.status(), 503);
      await new Promise((resolveDelay) => setTimeout(resolveDelay, 50));
    }
    assert.notEqual(response, null);
    oldRepositoryHandlerStatus = response.status();
    assert.equal(oldRepositoryHandlerStatus, 200);
    await response.body();
    finishOldRepositoryHandler();
    await oldRepositoryResponseRelease;
    await route.fulfill({ response });
    finishOldRepositoryRoute();
  }, { times: 1 });
  await page.getByLabel("搜索仓库候选").fill("旧仓库");
  await page.getByRole("button", { name: "搜索" }).click();
  await oldRepositoryHandlerReady;
  await page.getByLabel("搜索仓库候选").fill("fixture-b");
  await page.getByRole("button", { name: "搜索" }).click();
  await waitForRepository(page, "fixture-b/two");
  releaseOldRepositoryResponse();
  await oldRepositoryRouteFinished;
  await waitForTrackedFetch(page, "ui05-old-repository-200");
  const oldRepositoryTrack = await page.evaluate(() => window.__repomeshTrackedFetch?.["ui05-old-repository-200"]);
  assert.deepEqual(oldRepositoryTrack, { started: true, finished: true, status: 200 });
  assert.equal(await page.getByLabel("搜索仓库候选").inputValue(), "fixture-b");
  assert.equal(new URL(page.url()).pathname, `${second.projectPath}/settings`);
  assert.equal(await page.getByText("fixture-b/two", { exact: true }).count(), 1);
  assert.equal(await page.getByText("当前发现结果没有匹配仓库", { exact: true }).count(), 0);
  await control(desktop, { action: "hold_response", method: "GET", path: `/api${first.projectPath}` });
  await page.getByRole("link", { name: "项目", exact: true }).click();
  await page.getByRole("link", { name: "前端验收项目一" }).click();
  await page.getByText("正在读取当前项目…").waitFor();
  await waitStatus(desktop, "responseHeld", true);
  await page.getByRole("link", { name: "项目", exact: true }).click();
  await page.getByRole("link", { name: "前端验收项目二" }).click();
  await page.getByRole("heading", { name: "前端验收项目二" }).waitFor();
  await control(desktop, { action: "release_response" });
  await waitForTrackedFetch(page, "ui05-old-project-200");
  const oldProjectTrack = await page.evaluate(() => window.__repomeshTrackedFetch?.["ui05-old-project-200"]);
  assert.deepEqual(oldProjectTrack, { started: true, finished: true, status: 200 });
  assert.equal(new URL(page.url()).pathname, second.projectPath);
  assert.equal(await page.getByRole("heading", { name: "前端验收项目一" }).count(), 0);
  record("UI05", "same-list and cross-project late 200 responses reach one live SPA document and are discarded", { oldQueryHandlerStatus: oldRepositoryHandlerStatus, oldQueryDelaySource: "Playwright delayed real route.fetch response", oldQueryCancellationDisabled: true, currentQueryResultsKept: true, oldProjectStatus: 200, oldProjectCancellationDisabled: true, currentProjectKept: true, sameSpaDocument: true });

  currentStep = "UI05 late 401 and browser history";
  const late401Path = `/api/projects?${new URLSearchParams({ q: "迟到401", limit: "50" })}`;
  await installTrackedFetch(desktop, { method: "GET", path: late401Path, label: "ui05-late-read-401" });
  await page.goto(`${manifest.origin}/projects`);
  await page.getByLabel("搜索项目名称").waitFor();
  await control(desktop, { action: "revoke_session", actor: "a" });
  await control(desktop, { action: "hold_response", method: "GET", path: late401Path });
  await page.getByLabel("搜索项目名称").fill("迟到401");
  await page.getByRole("button", { name: "搜索" }).click();
  await waitStatus(desktop, "responseHeld", true);
  const actorBLogin = await desktop.newPage();
  await login(actorBLogin, "b");
  await actorBLogin.close();
  const currentActorSession = page.waitForResponse((response) => response.request().method() === "GET" && new URL(response.url()).pathname === "/api/session" && response.status() === 200);
  await page.evaluate(() => window.dispatchEvent(new Event("focus")));
  assert.equal((await currentActorSession).status(), 200);
  await page.getByTitle("当前账号").filter({ hasText: "Fixture B" }).waitFor();
  await page.getByRole("button", { name: "新建项目" }).click();
  await (await waitForRepository(page, "fixture-a/one")).getByRole("checkbox").check();
  await page.getByRole("button", { name: "完成选择" }).click();
  await page.getByLabel("项目名称").fill("Actor B 保留输入");
  await page.getByLabel("项目用途").fill("迟到的旧 401 不得清理当前主体输入。");
  await page.getByRole("button", { name: "查看保存摘要" }).click();
  const actorBInputBeforeLate401 = await page.evaluate(() => Array.from({ length: sessionStorage.length }, (_, index) => sessionStorage.key(index)).filter((key) => key?.startsWith("repomesh.project.operation.v1:")).length);
  assert.ok(actorBInputBeforeLate401 > 0);
  await assertMinimalOperationIndexes(page);
  const lateRead401 = page.waitForResponse((response) => response.request().method() === "GET" && `${new URL(response.url()).pathname}${new URL(response.url()).search}` === late401Path);
  await control(desktop, { action: "release_response" });
  const lateRead401Response = await lateRead401;
  await lateRead401Response.finished();
  assert.equal(lateRead401Response.status(), 401);
  await waitForTrackedFetch(page, "ui05-late-read-401");
  const late401Track = await page.evaluate(() => window.__repomeshTrackedFetch?.["ui05-late-read-401"]);
  assert.deepEqual(late401Track, { started: true, finished: true, status: 401 });
  assert.equal(await page.getByTitle("当前账号").textContent(), "Fixture B");
  const actorBInputAfterLate401 = await page.evaluate(() => Array.from({ length: sessionStorage.length }, (_, index) => sessionStorage.key(index)).filter((key) => key?.startsWith("repomesh.project.operation.v1:")).length);
  assert.equal(actorBInputAfterLate401, actorBInputBeforeLate401);
  await assertMinimalOperationIndexes(page);
  await page.goBack();
  await page.getByRole("heading", { name: "项目", exact: true }).waitFor();
  await page.goForward();
  await page.getByRole("heading", { name: "保存新项目" }).waitFor();
  record("UI05", "a real late 401 reaches the live document without clearing the current actor", { lateReadStatus: 401, cancellationDisabledForOldRead: true, currentActorStayedB: true, currentActorInputStayedPresent: true, minimalIndexesStayedValid: true, backForwardReachedCurrentRoutes: true });

  currentStep = "UI06 loading, reliable empty, and network error";
  const stateContext = await contextFor({ width: 1280, height: 900 });
  const statePage = await stateContext.newPage();
  await login(statePage, "b");
  await control(stateContext, { action: "hold_response", method: "GET", path: "/api/projects?q=&limit=50" });
  await statePage.goto(`${manifest.origin}/projects`);
  await statePage.getByText("正在读取项目…").waitFor();
  await waitStatus(stateContext, "responseHeld", true);
  await control(stateContext, { action: "release_response" });
  await statePage.getByText("还没有项目", { exact: true }).waitFor();
  await stateContext.setOffline(true);
  await statePage.getByRole("button", { name: "刷新" }).click();
  await statePage.getByText(/暂时无法连接服务器/).waitFor();
  assert.equal(await statePage.getByText("还没有项目", { exact: true }).count(), 0);
  await stateContext.setOffline(false);
  await statePage.getByRole("button", { name: "重新读取项目" }).click();
  await statePage.getByText("还没有项目", { exact: true }).waitFor();
  record("UI06", "loading, a reliable empty list, and a network error remain distinct", { loadingObserved: true, emptySuccessObserved: true, errorWasNotEmpty: true, emptyRecoveredAfterNetwork: true });
  await stateContext.close();

  currentStep = "UI06 merged focus refresh";
  const focusContext = await contextFor({ width: 1280, height: 900 });
  const focusPage = await focusContext.newPage();
  await login(focusPage, "a");
  await focusPage.goto(`${manifest.origin}${first.projectPath}`);
  await focusPage.getByRole("heading", { name: "前端验收项目一" }).waitFor();
  let sessionReads = 0;
  let projectReads = 0;
  let activeProjectReads = 0;
  let maximumActiveProjectReads = 0;
  focusPage.on("request", (request) => {
    const path = new URL(request.url()).pathname;
    if (request.method() === "GET" && path === "/api/session") sessionReads += 1;
    if (request.method() === "GET" && path === `/api${first.projectPath}`) {
      projectReads += 1;
      activeProjectReads += 1;
      maximumActiveProjectReads = Math.max(maximumActiveProjectReads, activeProjectReads);
    }
  });
  const finishProjectRead = (request) => {
    if (request.method() === "GET" && new URL(request.url()).pathname === `/api${first.projectPath}`) activeProjectReads -= 1;
  };
  focusPage.on("requestfinished", finishProjectRead);
  focusPage.on("requestfailed", finishProjectRead);
  await control(focusContext, { action: "hold_response", method: "GET", path: "/api/session" });
  await focusPage.evaluate(() => window.dispatchEvent(new Event("focus")));
  await waitStatus(focusContext, "responseHeld", true);
  await focusPage.evaluate(() => { window.dispatchEvent(new Event("focus")); window.dispatchEvent(new Event("focus")); window.dispatchEvent(new Event("focus")); });
  assert.equal(sessionReads, 1);
  const focusedSession200 = focusPage.waitForResponse((response) => response.request().method() === "GET" && new URL(response.url()).pathname === "/api/session");
  const focusedProject200 = focusPage.waitForResponse((response) => response.request().method() === "GET" && new URL(response.url()).pathname === `/api${first.projectPath}`);
  await control(focusContext, { action: "release_response" });
  assert.equal((await focusedSession200).status(), 200);
  assert.equal((await focusedProject200).status(), 200);
  await focusPage.getByRole("heading", { name: "前端验收项目一" }).waitFor();
  assert.equal(projectReads, 1);
  assert.equal(maximumActiveProjectReads, 1);
  record("UI06", "concurrent focus events merge into one session refresh and one project reread", { sessionReadCount: sessionReads, projectReadCount: projectReads, maximumConcurrentProjectReads: maximumActiveProjectReads });
  await focusContext.close();

  currentStep = "UI06 readable shell after update denial";
  const denialContext = await contextFor({ width: 1280, height: 900 });
  const denialPage = await denialContext.newPage();
  await login(denialPage, "b");
  const deniedCreate = await prepareCreate(denialPage, { name: "只读外壳检查" });
  const deniedProject = await finishCreate(denialPage);
  assert.ok(deniedCreate.key);
  await denialPage.getByRole("button", { name: "项目设置" }).click();
  await denialPage.getByLabel("项目用途").fill("触发只读外壳检查。");
  await denialPage.getByRole("button", { name: "查看本次改动" }).click();
  await denialPage.route(`**/api${deniedProject.projectPath}`, async (route) => {
    if (route.request().method() !== "PATCH") return route.continue();
    await route.fulfill({ status: 403, contentType: "application/json", body: JSON.stringify({ error: { code: "PROJECT_UPDATE_NOT_ALLOWED", message: "not exposed" } }) });
  }, { times: 1 });
  await denialPage.getByRole("button", { name: "确认保存修改" }).click();
  await denialPage.getByText(/当前项目只读/).waitFor();
  assert.equal(await denialPage.getByRole("heading", { name: "只读外壳检查" }).count(), 1);
  assert.equal(await denialPage.getByText("只读", { exact: true }).count(), 1);
  record("UI06", "mocked client error rendering preserves the readable shell for PROJECT_UPDATE_NOT_ALLOWED", {
    responseSource: "Playwright route.fulfill; not a server authorization result",
    projectShellPreserved: true,
    editingDisabled: true,
  });
  await denialContext.close();

  currentStep = "UI07 desktop and mobile layout";
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), true);
  const mobile = await contextFor({ width: 390, height: 844 });
  await mobile.addInitScript(() => {
    window.__repomeshMobileStorageRejected = false;
    const original = Storage.prototype.setItem;
    Storage.prototype.setItem = function setItem(key, value) {
      if (this === sessionStorage && key.startsWith("repomesh.project.operation.v1:")) {
        window.__repomeshMobileStorageRejected = true;
        throw new DOMException("disabled", "SecurityError");
      }
      return original.call(this, key, value);
    };
  });
  const mobilePage = await mobile.newPage();
  await login(mobilePage, "a");
  await mobilePage.goto(`${manifest.origin}${first.projectPath}/settings`);
  await mobilePage.getByRole("button", { name: "添加仓库" }).click();
  await mobilePage.getByText(/本次已选/).waitFor();
  assert.equal(await mobilePage.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), true);
  await mobilePage.goto(`${manifest.origin}${firstOperation.recoveryPath}`);
  await mobilePage.getByRole("heading", { name: "本次已保存" }).waitFor();
  assert.equal(await mobilePage.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), true);
  await mobilePage.goto(`${manifest.origin}/projects/new`);
  await (await waitForRepository(mobilePage, "fixture-a/one")).getByRole("checkbox").check();
  await mobilePage.getByText(/本次已选 1\/100/).waitFor();
  assert.equal(await mobilePage.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), true);
  await mobilePage.getByRole("button", { name: "完成选择" }).click();
  await mobilePage.getByLabel("项目名称").fill("移动存储失败路径");
  await mobilePage.getByLabel("项目用途").fill("验证恢复链接和明确继续按钮可操作。");
  await mobilePage.getByRole("button", { name: "查看保存摘要" }).click();
  await mobilePage.getByText(/浏览器无法可靠保存原输入/).waitFor();
  assert.equal(await mobilePage.evaluate(() => window.__repomeshMobileStorageRejected), true);
  assert.equal(await mobilePage.locator(".recovery-path").count(), 1);
  assert.equal(await mobilePage.getByRole("button", { name: "已保存链接，继续发送" }).count(), 1);
  assert.equal(await mobilePage.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), true);
  record("UI07", "desktop and 390x844 project controls remain reachable without horizontal overflow", { desktopNoOverflow: true, mobileSettings: true, mobileSelectionSummary: true, mobileRecovery: true, mobileStorageFailurePath: true });
  await mobile.close();

  await page.close();
  await desktop.close();
  currentStep = "shutdown";
  const shutdownContext = await contextFor({ width: 800, height: 600 });
  await control(shutdownContext, { action: "shutdown" });
  await shutdownContext.close();
  const covered = new Set(events.map((event) => event.id));
  assert.deepEqual([...covered].sort(), ["UI01", "UI02", "UI03", "UI04", "UI05", "UI06", "UI07"]);
  console.log(JSON.stringify({ suite: "B03 frontend browser acceptance", passed: true, environment: "fresh Playwright contexts with the shared TLS harness", events, network }, null, 2));
  }
} catch (error) {
  console.log(JSON.stringify({ suite: "B03 frontend browser acceptance", passed: false, failedStep: currentStep, error: error instanceof Error ? error.message : "unknown failure", events, network }, null, 2));
  process.exitCode = 1;
} finally {
  await browser.close();
}
