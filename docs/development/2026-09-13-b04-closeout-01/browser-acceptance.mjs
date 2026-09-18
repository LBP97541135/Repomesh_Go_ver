import assert from "node:assert/strict";
import { copyFile, mkdir, readFile, stat } from "node:fs/promises";
import { createRequire } from "node:module";
import { resolve } from "node:path";

const require = createRequire(import.meta.url);
const { chromium } = require("/tmp/repomesh-b04-browser/node_modules/playwright-core");
const executablePath = "/home/ubuntu/.cache/ms-playwright/chromium-1148/chrome-linux/chrome";
const manifestPath = process.argv[2];
if (!manifestPath) throw new Error("usage: node browser-acceptance.mjs <manifest.json>");

const screenshotDir = process.env.SCREENSHOT_DIR ?? "";
const artifactDir = process.env.ARTIFACT_DIR ?? "";
const secret = "sk-b04-fixture-not-a-real-key-9f3a";
const providerName = "夹具供应商甲";
const emptyCloseId = "80000000-0000-4000-8000-0000000000c1";

const manifestFile = await stat(resolve(manifestPath));
assert.equal(manifestFile.mode & 0o077, 0, "manifest must not be group/world accessible");
const manifest = JSON.parse(await readFile(resolve(manifestPath), "utf8"));
const manifestKeys = Object.keys(manifest).sort();
assert.ok(
  JSON.stringify(manifestKeys) === JSON.stringify(["controlPath", "fixtures", "loginPath", "origin", "statusPath"])
    || JSON.stringify(manifestKeys) === JSON.stringify(["controlPath", "emptyCatalog", "fixtures", "loginPath", "origin", "statusPath"]),
  "manifest fields must match the approved browser fixture contract",
);
const origin = new URL(manifest.origin);
assert.equal(origin.protocol, "https:");
assert.ok(origin.hostname === "127.0.0.1" || origin.hostname === "localhost" || origin.hostname === "::1");
assert.equal(manifest.loginPath, "/__test/login");
assert.equal(manifest.controlPath, "/__test/control");
assert.equal(manifest.statusPath, "/__test/status");

const events = [];
const network = [];
const shots = [];
let currentStep = "launch";
const browser = await chromium.launch({ executablePath, headless: true });

function record(id, name, assertions) {
  events.push({ id, event: name, passed: true, assertions });
}

function modelPath(pathname) {
  return pathname === "/api/model-providers"
    || pathname.startsWith("/api/model-providers/")
    || pathname === "/api/model-provider-saves"
    || pathname.startsWith("/api/model-provider-saves/");
}

async function contextFor(viewport) {
  const context = await browser.newContext({ ignoreHTTPSErrors: true, viewport });
  context.on("response", async (response) => {
    const url = new URL(response.url());
    if (url.origin !== manifest.origin || !modelPath(url.pathname)) return;
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

async function shot(page, name) {
  if (!screenshotDir) return;
  await mkdir(screenshotDir, { recursive: true });
  const file = `${screenshotDir}/${name}.png`;
  await page.screenshot({ path: file, fullPage: true });
  shots.push(file);
  if (artifactDir) {
    await mkdir(artifactDir, { recursive: true });
    await copyFile(file, `${artifactDir}/${name}.png`);
  }
}

function storageHasSecret(raw) {
  return JSON.stringify(raw).includes(secret);
}

async function assertSecretAbsent(page) {
  const storage = await page.evaluate(() => {
    const collect = (store) => {
      const items = {};
      for (let index = 0; index < store.length; index += 1) {
        const key = store.key(index);
        if (key !== null) items[key] = store.getItem(key);
      }
      return items;
    };
    return { session: collect(sessionStorage), local: collect(localStorage), html: document.documentElement.innerHTML };
  });
  assert.equal(storageHasSecret(storage), false);
  return storage;
}

async function installTrackedFetch(context, rule) {
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
}

async function fillProviderForm(page) {
  await page.getByLabel("名称").fill(providerName);
  await page.getByLabel("Base URL").fill("https://gateway.example.invalid/v1");
  await page.getByLabel("API Key").fill(secret);
  await page.getByLabel("modelId").fill("fixture-chat");
  await page.getByLabel("显示名").fill("夹具对话");
}

try {
  currentStep = "U04.4 loading then empty catalog";
  const desktop = await contextFor({ width: 1440, height: 960 });
  await installTrackedFetch(desktop, { method: "POST", path: "/api/model-provider-saves", label: "b04-save-drop" });
  const page = await desktop.newPage();
  page.on("dialog", async (dialog) => {
    events.push({ id: "DIALOG", event: dialog.message(), passed: true, assertions: { type: dialog.type() } });
    await dialog.accept();
  });
  await login(page, "a");
  await control(desktop, { action: "hold_response", method: "GET", path: "/api/model-providers" });
  await page.getByRole("button", { name: "模型设置" }).click();
  await page.getByText("正在读取供应商…").waitFor();
  await waitStatus(desktop, "responseHeld", true);
  await shot(page, "b04_closeout_settings_loading");
  await control(desktop, { action: "release_response" });
  await page.getByText("还没有已保存的供应商。", { exact: true }).waitFor();
  await page.getByRole("heading", { name: "模型连接" }).waitFor();
  await shot(page, "b04_closeout_settings_empty");
  record("U04.4", "fixture login opens model settings with a held then empty provider list", {
    loadingObserved: true,
    emptyObserved: true,
  });

  currentStep = "U04.2 invalid URL stays on the form";
  await page.getByLabel("名称").fill(providerName);
  await page.getByLabel("Base URL").fill("http://gateway.example.invalid/v1");
  await page.getByLabel("API Key").fill(secret);
  await page.getByLabel("modelId").fill("fixture-chat");
  await page.getByLabel("显示名").fill("夹具对话");
  const invalidSave = page.waitForResponse((response) => response.request().method() === "POST" && new URL(response.url()).pathname === "/api/model-provider-saves");
  await page.getByRole("button", { name: "保存供应商" }).click();
  const invalidResponse = await invalidSave;
  assert.equal(invalidResponse.status(), 422);
  assert.equal((await invalidResponse.json()).error?.code, "VALIDATION_FAILED");
  assert.equal(new URL(page.url()).pathname, "/settings/models");
  await page.getByRole("alert").waitFor();
  assert.match(await page.getByRole("alert").innerText(), /这次保存没有受理/);
  assert.equal(await page.getByLabel("名称").inputValue(), providerName);
  assert.equal(await page.getByLabel("modelId").inputValue(), "fixture-chat");
  assert.equal(await page.getByLabel("API Key").inputValue(), "");
  assert.equal(await page.getByRole("heading", { name: "查询这次模型保存" }).count(), 0);
  await assertSecretAbsent(page);
  await shot(page, "b04_validation_stay");
  record("U04.2", "validation failure stays on the form, keeps typed fields, and clears the key", {
    stayedOnForm: true,
    alertShown: true,
    secretCleared: true,
  });

  currentStep = "U04.4 dropped save response recovers the original key";
  await fillProviderForm(page);
  await shot(page, "b04_closeout_settings_filled");
  let saveWrites = 0;
  let saveId = "";
  const countSave = (request) => {
    if (request.method() === "POST" && new URL(request.url()).pathname === "/api/model-provider-saves") {
      saveWrites += 1;
      saveId = request.headers()["idempotency-key"] ?? "";
    }
  };
  desktop.on("request", countSave);
  await control(desktop, { action: "drop_response", method: "POST", path: "/api/model-provider-saves" });
  await page.getByRole("button", { name: "保存供应商" }).click();
  await waitForTrackedFetch(page, "b04-save-drop");
  const dropped = await page.evaluate(() => window.__repomeshTrackedFetch?.["b04-save-drop"]);
  assert.equal(dropped.started, true);
  assert.equal(dropped.finished, true);
  assert.ok(dropped.status === null || dropped.status === 200 || dropped.status === 201);
  assert.equal(typeof saveId, "string");
  assert.match(saveId, /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/u);
  assert.equal(new URL(page.url()).pathname, `/settings/model-saves/${saveId}`);
  await page.getByText(new RegExp(`已保存供应商 [0-9a-f-]+，版本 [0-9a-f-]+。`, "u")).waitFor();
  const storage = await assertSecretAbsent(page);
  assert.equal(storage.html.includes("type=\"password\""), false);
  const index = JSON.parse(storage.local["repomesh.model.save-index.v1"] ?? "[]");
  assert.equal(index.at(-1)?.id, saveId);
  assert.equal(index.at(-1)?.kind, "provider_save");
  const snapshotKey = Object.keys(storage.session).find((key) => key.startsWith("repomesh.model.save.v1:"));
  assert.equal(typeof snapshotKey, "string");
  const snapshot = JSON.parse(storage.session[snapshotKey]);
  assert.equal(snapshot.snapshot.name, providerName);
  assert.equal(snapshot.snapshot.secretMode, "replace");
  assert.equal("value" in snapshot.snapshot, false);
  assert.equal(saveWrites, 1);
  desktop.off("request", countSave);
  await shot(page, "b04_closeout_save_recovered");
  record("U04.4", "a dropped 2xx save still recovers the original saveId without storing the key", {
    saveWrites,
    saveId,
    droppedStatus: dropped.status,
    keyAbsentFromStorage: true,
    secretFreeSnapshot: true,
  });

  currentStep = "U04.4 close after commit keeps the original result";
  const actorSession = await desktop.request.get(`${manifest.origin}/api/session`);
  assert.equal(actorSession.status(), 200);
  const actorSessionBody = await actorSession.json();
  const closeAfterCommit = await desktop.request.post(`${manifest.origin}/api/model-provider-saves/${saveId}/close`, {
    headers: {
      Origin: manifest.origin,
      "Content-Type": "application/json",
      "Idempotency-Key": saveId,
      "X-CSRF-Token": actorSessionBody.csrfToken,
    },
    data: {},
  });
  assert.equal(closeAfterCommit.status(), 200);
  const closeAfterCommitBody = await closeAfterCommit.json();
  assert.equal(closeAfterCommitBody.outcome, "committed");
  assert.equal(closeAfterCommitBody.saveId, saveId);
  await page.getByRole("button", { name: "返回模型设置" }).click();
  await page.getByText(providerName, { exact: true }).waitFor();
  await page.getByText("1 个模型", { exact: true }).waitFor();
  await assertSecretAbsent(page);
  await shot(page, "b04_closeout_settings_after_save");
  record("U04.4", "close after a committed save returns the original receipt and the list shows the provider", {
    closeKeptCommitted: true,
    providerListed: true,
  });

  currentStep = "U04.4 empty slot close blocks a later save";
  const emptyGetPromise = page.waitForResponse((response) => response.request().method() === "GET" && new URL(response.url()).pathname === `/api/model-provider-saves/${emptyCloseId}`);
  await page.goto(`${manifest.origin}/settings/model-saves/${emptyCloseId}`);
  const emptyGetResponse = await emptyGetPromise;
  assert.equal(emptyGetResponse.status(), 404);
  const emptyGetBody = await emptyGetResponse.json();
  assert.equal(emptyGetBody.error?.code, "MODEL_SAVE_NOT_FOUND");
  await page.getByRole("button", { name: "终结原保存" }).waitFor();
  await page.getByRole("button", { name: "终结原保存" }).click();
  await page.getByText("原操作已终结，未保存。", { exact: true }).waitFor();
  const sessionResponse = await desktop.request.get(`${manifest.origin}/api/session`);
  assert.equal(sessionResponse.status(), 200);
  const session = await sessionResponse.json();
  const lateSave = await desktop.request.post(`${manifest.origin}/api/model-provider-saves`, {
    headers: {
      Origin: manifest.origin,
      "Content-Type": "application/json",
      "Idempotency-Key": emptyCloseId,
      "X-CSRF-Token": session.csrfToken,
    },
    data: {
      providerId: null,
      expectedRevision: null,
      name: "迟到保存",
      baseUrl: "https://gateway.example.invalid/v1",
      apiFormat: "openai_chat_completions",
      models: [{ id: null, modelId: "late", displayName: "late", contextWindow: 128000, maxOutputTokens: 4096, reasoning: false, vision: false }],
      secret: { mode: "replace", value: "sk-late-must-not-write" },
    },
  });
  assert.equal(lateSave.status(), 409);
  const lateBody = await lateSave.json();
  assert.equal(lateBody.error?.code, "MODEL_SAVE_CLOSED");
  record("U04.4", "closing an empty save slot returns closed_without_save and blocks a late write", {
    emptyLookupCode: "MODEL_SAVE_NOT_FOUND",
    lateCode: "MODEL_SAVE_CLOSED",
  });

  currentStep = "U04.4 other actor cannot read the original receipt";
  await login(page, "b");
  const foreignGetPromise = page.waitForResponse((response) => response.request().method() === "GET" && new URL(response.url()).pathname === `/api/model-provider-saves/${saveId}`);
  await page.goto(`${manifest.origin}/settings/model-saves/${saveId}`);
  const foreignGet = await foreignGetPromise;
  assert.equal(foreignGet.status(), 404);
  assert.equal((await foreignGet.json()).error?.code, "MODEL_SAVE_NOT_FOUND");
  await page.getByRole("button", { name: "终结原保存" }).waitFor();
  assert.equal(await page.getByText(providerName, { exact: true }).count(), 0);
  assert.equal(await page.locator("main").innerText().then((text) => text.includes("已保存供应商")), false);
  record("U04.4", "another actor sees MODEL_SAVE_NOT_FOUND and no provider identity", {
    crossActorHidden: true,
  });

  await page.close();
  await desktop.close();
  currentStep = "shutdown";
  const shutdownContext = await contextFor({ width: 800, height: 600 });
  await control(shutdownContext, { action: "shutdown" });
  await shutdownContext.close();
  assert.deepEqual(events.filter((event) => event.id.startsWith("U")).map((event) => event.id), ["U04.4", "U04.2", "U04.4", "U04.4", "U04.4", "U04.4"]);
  console.log(JSON.stringify({
    suite: "B04 fixture browser Key and save recovery",
    passed: true,
    environment: "fresh Playwright context with the shared TLS harness",
    events,
    network,
    shots,
  }, null, 2));
} catch (error) {
  console.log(JSON.stringify({
    suite: "B04 fixture browser Key and save recovery",
    passed: false,
    failedStep: currentStep,
    error: error instanceof Error ? error.message : "unknown failure",
    events,
    network,
    shots,
  }, null, 2));
  process.exitCode = 1;
} finally {
  await browser.close();
}
