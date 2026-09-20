// Opt-in browser check for the real React components and the Go fixture API.
// Playwright may be supplied through NODE_PATH; no application dependency is added.
const { chromium } = require("playwright");
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const net = require("node:net");
const { spawn } = require("node:child_process");

async function main() {
  const root = process.env.REPOMESH_TYPESAFE_WORKTREE;
  const frontend = path.join(root, "frontend");
  const pageName = `typesafe-verification-${process.pid}.html`;
  const fixture = JSON.parse(process.env.REPOMESH_TYPESAFE_UI_FIXTURE);
  const headers = { Cookie: `__Host-repomesh-session=${fixture.cookie}`, Origin: "https://repomesh.test", "X-CSRF-Token": fixture.csrf };
  const server = net.createServer();
  await new Promise(resolve => server.listen(0, "127.0.0.1", resolve));
  const port = server.address().port;
  await new Promise(resolve => server.close(resolve));
  const html = `<!doctype html><html><head><meta charset="UTF-8"></head><body><div id="root"></div><script type="module">
import React from "react";
import {createRoot} from "react-dom/client";
import {TypeSafeSettings} from "/src/components/TypeSafeSettings.tsx";
import {TypeSafeEvaluations} from "/src/components/TypeSafeEvaluations.tsx";
import {setCsrfToken} from "/src/api/http.ts";
import "/src/index.css";
const f=window.typeSafeFixture; setCsrfToken(f.csrf);
function Harness(){ const [project,setProject]=React.useState(f.projectId); return React.createElement("main",{className:"mx-auto max-w-3xl p-8"},
React.createElement("h1",null,"RepoMesh · TypeSafe 验证"),
React.createElement(TypeSafeSettings,{projectId:project,projectName:"合成验收项目"}),
React.createElement(TypeSafeEvaluations,{projectId:project,issueId:f.issueId}),
React.createElement("button",{onClick:()=>setProject(f.foreignProjectId)},"切到其他账号的项目"),
React.createElement("button",{onClick:()=>setProject(f.projectId)},"返回当前项目"));}
createRoot(document.getElementById("root")).render(React.createElement(Harness));
</script></body></html>`;
  await fs.writeFile(path.join(frontend, pageName), html);
  const vite = spawn(process.execPath, [path.join(frontend, "node_modules/vite/bin/vite.js"), "--host", "127.0.0.1", "--port", String(port), "--strictPort"], { cwd: frontend, stdio: ["ignore", "pipe", "pipe"] });
  let browser;
  try {
    const url = `http://127.0.0.1:${port}/${pageName}`;
    let available = false;
    for (let n = 0; n < 80; n++) {
      try { if ((await fetch(url)).ok) { available = true; break; } } catch { /* startup */ }
      await new Promise(resolve => setTimeout(resolve, 100));
    }
    assert(available, "fixture UI did not start");
    browser = await chromium.launch({ headless: true, executablePath: process.env.REPOMESH_TYPESAFE_BROWSER_EXECUTABLE || undefined });
    const page = await browser.newPage({ viewport: { width: 1200, height: 900 } });
    const pageErrors = [];
    page.on("pageerror", error => pageErrors.push(error.message));
    await page.addInitScript(value => { window.typeSafeFixture = value; }, {
      projectId: fixture.projectId, foreignProjectId: fixture.foreignProjectId, issueId: fixture.issueId, csrf: fixture.csrf,
    });
    await page.route("**/api/**", async route => {
      const request = route.request();
      const u = new URL(request.url());
	  // Vite imports /src/api/*.ts too; only product API requests go to Go.
	  if (!u.pathname.startsWith("/api/")) { await route.continue(); return; }
      const response = await fetch(fixture.baseURL + u.pathname + u.search, {
        method: request.method(), headers: { ...headers, "Content-Type": "application/json" },
        body: request.postDataBuffer() || undefined,
      });
      await route.fulfill({ status: response.status, contentType: "application/json", body: await response.text() });
    });
    const read = async () => {
      const res = await fetch(`${fixture.baseURL}/api/projects/${fixture.projectId}/typesafe`, { headers });
      assert.equal(res.status, 200); return res.json();
    };
    await page.goto(url);
    const box = page.getByRole("checkbox", { name: "启用 Jev 辅助测试与代码审查" });
    await page.getByText(/已验证鉴权和推理可用/).waitFor();
    assert(await box.isChecked());
    await page.getByText("仓库负责人 · 代码审查 · 已完成判断").waitFor();
    await page.getByText("测试团队 · 证据核对 · 已完成判断").waitFor();
    if (process.env.REPOMESH_TYPESAFE_UI_SCREENSHOT) await page.screenshot({ path: process.env.REPOMESH_TYPESAFE_UI_SCREENSHOT, fullPage: true });
    await box.uncheck(); await page.getByRole("button", { name: "保存配置", exact: true }).click();
    await page.getByRole("status").waitFor(); assert.equal((await read()).enabled, false);
    await box.check(); await page.getByRole("button", { name: "保存配置", exact: true }).click();
    await page.getByRole("status").waitFor(); assert.equal((await read()).enabled, true);
    const input = page.getByLabel("TypeSafe API Key", { exact: true });
    await input.fill("fixture-browser-replacement-key");
    await page.getByRole("button", { name: "保存配置", exact: true }).click();
    await page.getByRole("status").waitFor(); assert.equal(await input.inputValue(), "");
    assert(!JSON.stringify(await read()).includes("fixture-browser-replacement-key"));
    await page.reload(); await page.getByText(/Key 已加密保存/).waitFor(); assert.equal(await input.inputValue(), "");
    await page.getByRole("button", { name: "清除 Key 并关闭" }).click();
    await page.getByRole("status").waitFor();
    const cleared = await read(); assert.equal(cleared.enabled, false); assert.equal(cleared.configured, false);
    await page.getByRole("button", { name: "切到其他账号的项目" }).click();
    await page.getByRole("alert").first().waitFor(); assert(await input.isDisabled()); assert.equal(await input.inputValue(), "");
    assert.equal(pageErrors.length, 0, "React page errors");
    console.log(JSON.stringify({ passed: true, checks: ["saved-key-hidden", "real-test-and-review-results", "shared-toggle-off-on", "replace-key", "reload", "clear-and-disable", "cross-project-denied"], pageErrors }));
  } finally {
    if (browser) await browser.close();
    vite.kill("SIGTERM");
    await fs.unlink(path.join(frontend, pageName)).catch(() => undefined);
  }
}
main().catch(error => { console.error(error.message); process.exitCode = 1; });
