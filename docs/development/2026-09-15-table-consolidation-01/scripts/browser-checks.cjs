const { chromium } = require(process.env.PLAYWRIGHT_MODULE || "playwright");
const path = require("path");
const ROOT = path.resolve(__dirname, "../../../..");
const EVIDENCE = path.resolve(__dirname, "..");
const fs = require("fs");

const PAGE = require("url").pathToFileURL(path.join(ROOT, "docs/api-database/index.html")).href;
const SHOTS = process.env.BROWSER_OUTPUT_DIR || "/tmp/repomesh-consolidation-20260915/browser-screenshots";
const results = [];

function check(name, ok, detail) {
  results.push({ name, ok, detail });
  console.log(`${ok ? "PASS" : "FAIL"} ${name}${detail === undefined ? "" : ": " + detail}`);
}

function overflowProbe() {
  const de = document.documentElement;
  const wide = [];
  document.querySelectorAll("*").forEach((el) => {
    const box = el.getBoundingClientRect();
    if (box.width > 0 && box.right > de.clientWidth + 1 && !el.closest(".tw") && !el.closest("pre.code")) {
      wide.push(el.tagName + "." + el.className + "@" + Math.round(box.right));
    }
  });
  return { scroll: de.scrollWidth, client: de.clientWidth, wide: wide.slice(0, 6) };
}

(async () => {
  fs.mkdirSync(SHOTS, { recursive: true });
  const browser = await chromium.launch();
  const errors = [];
  const requests = [];
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
  page.on("pageerror", (error) => errors.push(String(error)));
  page.on("console", (message) => {
    if (message.type() === "error") errors.push(message.text());
  });
  page.on("request", (request) => requests.push(request.url()));
  await page.goto(PAGE);
  await page.waitForLoadState("load");
  await page.addStyleTag({ content: "html { scroll-behavior: auto !important; }" });

  const structure = await page.evaluate(() => {
    const ids = new Set([...document.querySelectorAll("[id]")].map((el) => el.id));
    const bad = [...document.querySelectorAll('a[href^="#"]')]
      .map((a) => a.getAttribute("href").slice(1))
      .filter((anchor) => anchor && !ids.has(decodeURIComponent(anchor)));
    const nav = document.querySelectorAll(".docnav a").length;
    const labels = [...document.querySelectorAll(".docnav a")].map((a) => a.textContent.trim());
    const batches = labels.filter((label) => /^B\d\d$/.test(label));
    const stats = [...document.querySelectorAll(".stat")].map((el) => el.textContent.trim());
    const tableStat = [...document.querySelectorAll(".stat")]
      .map((el) => el.textContent.trim())
      .find((text) => text.endsWith("说明表格"));
    return {
      bad: [...new Set(bad)],
      nav,
      labels,
      batches,
      stats,
      tableStat,
      databaseStats: Object.fromEntries([...document.querySelectorAll(".stat")].map(el => [el.querySelector(".t").textContent.trim(), Number(el.querySelector(".n").textContent.trim())])),
      physicalDeclarations: [...document.querySelectorAll("p")].map(el => el.textContent.trim()).filter(text => /^物理表：[a-z_]+\.[a-z_]+$/.test(text)),
      tables: document.querySelectorAll("table.grid").length,
      h2: document.querySelector("h1").textContent,
      sub: document.querySelector(".sub").textContent,
    };
  });
  const expectedBatches = Array.from({ length: 12 }, (_, i) => "B" + String(i).padStart(2, "0"));
  check("nav 批次标签为大写 B00-B11", structure.batches.join(",") === expectedBatches.join(","), structure.labels.join(","));
  check("nav 保留 foundations 与目录入口", structure.labels[0] === "foundations" && structure.labels[structure.labels.length - 1] === "目录", structure.labels.join(","));
  check("标题覆盖 B00-B11", structure.h2.includes("B00-B11"), structure.h2);
  check("首行说明采用状态与设计理由", structure.sub.includes("采用状态与设计理由") && !structure.sub.includes("时间戳"), structure.sub);
  check("内部锚点全部存在", structure.bad.length === 0, structure.bad.join(","));
  check("表格元素数与计数一致", String(structure.tables) === (structure.tableStat || "").replace("说明表格", ""), `${structure.tables} 个说明表格 / 计数 ${structure.tableStat}`);
  check("13章均有完整内容", structure.batches.length === 12 && structure.tables > 0, String(structure.tables));

  const expectedDatabaseStats = {"已有数据库表（手册基线）":36,"扫描表（手册外）":1,"B05-B11 设计表":34,"手册范围合计":70,"全仓含扫描合计":71,"Markdown 章节":13};
  check("数据库统计按已实现与设计分列", Object.entries(expectedDatabaseStats).every(([key, value]) => structure.databaseStats[key] === value), JSON.stringify(structure.databaseStats));
  check("34张物理表声明完整且唯一", structure.physicalDeclarations.length === 34 && new Set(structure.physicalDeclarations).size === 34, structure.physicalDeclarations.length);

  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: width === 390 ? 844 : 900 });
    const probe = await page.evaluate(overflowProbe);
    check(`无页面横向溢出 @${width}`, probe.scroll <= probe.client + 1 && probe.wide.length === 0, JSON.stringify(probe));
    await page.click('.docnav a[href="#doc-b10"]');
    await page.waitForTimeout(100);
    const anchor = await page.evaluate(() => ({
      titleTop: document.getElementById('doc-b10').getBoundingClientRect().top,
      navBottom: document.querySelector('.docnav').getBoundingClientRect().bottom
    }));
    check(`锚点标题完整落在导航下方 @${width}`, anchor.titleTop >= anchor.navBottom + 4 && anchor.titleTop <= anchor.navBottom + 80, JSON.stringify(anchor));
    await page.screenshot({ path: `${SHOTS}/final-${width}.png`, fullPage: false });
  }

  await page.setViewportSize({ width: 1440, height: 900 });
  const headingText = await page.locator('#doc-b04').textContent();
  check('章节标题不重复批次编号', !/B04\s*[·•]\s*B04/.test(headingText), headingText.trim().slice(0, 100));
  const localBefore = await page.evaluate(() => [...document.querySelectorAll(".toc.local li")].filter((li) => !li.hidden).length);
  await page.fill("#tocFilter", "B04");
  const filtered = await page.evaluate(() => {
    const rows = [...document.querySelectorAll("#toc ul > li")];
    const start = rows.findIndex((li) => li.classList.contains("tgroup") && li.textContent.trim().startsWith("B04"));
    let end = rows.length;
    for (let i = start + 1; i < rows.length; i++) {
      if (rows[i].classList.contains("tgroup")) {
        end = i;
        break;
      }
    }
    const visible = rows.map((li, i) => (li.hidden ? -1 : i)).filter((i) => i >= 0);
    const expected = Array.from({ length: end - start }, (_, k) => start + k);
    const local = [...document.querySelectorAll(".toc.local li")].filter((li) => !li.hidden).length;
    return { visible, expected, total: rows.length, local };
  });
  check("筛选 B04 只留该章与其分组", JSON.stringify(filtered.visible) === JSON.stringify(filtered.expected), `${filtered.visible.length} 可见 / 期望 ${filtered.expected.length}，共 ${filtered.total} 行`);
  check("筛选不影响各章本地目录", filtered.local === localBefore, `local ${filtered.local}/${localBefore}`);
  await page.screenshot({ path: `${SHOTS}/final-filter-b04.png`, clip: { x: 0, y: 0, width: 1440, height: 620 } });

  const groupTitle = await page.evaluate(() => {
    const nodes = [...document.querySelectorAll("#toc ul > li.tnode")];
    const target = nodes.find((li) => li.textContent.includes("接口") || li.textContent.length > 6);
    return target ? target.textContent.trim().slice(0, 8) : "";
  });
  await page.fill("#tocFilter", groupTitle);
  const groupFiltered = await page.evaluate(() => {
    const rows = [...document.querySelectorAll("#toc ul > li")];
    return rows.filter((li) => !li.hidden).map((li) => li.className + ":" + li.textContent.trim().slice(0, 10));
  });
  check("筛选分组时保留所属章节行", groupFiltered.some((row) => row.startsWith("tgroup")), groupFiltered.join(" | "));

  await page.fill("#tocFilter", "");
  const cleared = await page.evaluate(() => [...document.querySelectorAll("#toc ul > li")].filter((li) => li.hidden).length);
  check("清空筛选后全部恢复", cleared === 0, String(cleared));

  await page.addStyleTag({ content: "html { scroll-behavior: auto !important; }" });
  await page.click('.docnav a[href="#doc-b09"]');
  await page.waitForTimeout(150);
  const jump = await page.evaluate(() => {
    const box = document.getElementById("doc-b09").getBoundingClientRect();
    return { top: Math.round(box.top), scrollY: Math.round(window.scrollY) };
  });
  check("锚点跳转落在目标章节", jump.scrollY > 0 && jump.top > -20 && jump.top < 120, JSON.stringify(jump));
  await page.screenshot({ path: `${SHOTS}/final-jump-b09.png`, fullPage: false });

  await page.emulateMedia({ media: "print" });
  const printChrome = await page.evaluate(() => ({
    nav: getComputedStyle(document.querySelector(".docnav")).display,
    search: getComputedStyle(document.querySelector("#tocFilter")).display,
  }));
  const printState = { ...printChrome, ...(await page.evaluate(overflowProbe)) };
  check("打印隐藏浮动导航与搜索框", printState.nav === "none" && printState.search === "none", JSON.stringify(printState));
  check("打印无横向溢出", printState.scroll <= printState.client + 1, `${printState.scroll}/${printState.client}`);
  await page.pdf({ path: `${SHOTS}/final-print.pdf`, format: "A4", printBackground: false });
  const pdfBytes = fs.statSync(`${SHOTS}/final-print.pdf`).size;
  check("打印输出 PDF", pdfBytes > 100000, `${Math.round(pdfBytes / 1024)} KB`);
  await page.emulateMedia({ media: "screen" });

  const external = requests.filter((url) => !url.startsWith("file://") && !url.startsWith("data:"));
  check("无外部网络请求", external.length === 0, external.join(","));
  check("无 JS 异常或 console 错误", errors.length === 0, errors.join(" | "));

  await page.setViewportSize({ width: 390, height: 844 });
  const clipped = await page.evaluate(() => {
    const problems = [];
    document.querySelectorAll('code:not(pre code)').forEach(el => {
      if (el.closest('.tw')) return;
      const card = el.closest('.card');
      if (!card) return;
      const edge = card.getBoundingClientRect();
      const range = document.createRange();
      range.selectNodeContents(el);
      for (const rect of range.getClientRects()) {
        if (rect.right > edge.right + 1 || rect.left < edge.left - 1) {
          problems.push(el.textContent.slice(0, 100));
          break;
        }
      }
    });
    return problems;
  });
  check('窄屏行内代码未被卡片截断', clipped.length === 0, clipped.slice(0, 5));
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: width === 390 ? 844 : 1000 });
    await page.waitForFunction(() => Math.abs(parseFloat(getComputedStyle(document.documentElement).getPropertyValue('--docnav-height')) - document.querySelector('.docnav').getBoundingClientRect().height) < 1);
    for (const batch of ['b04', 'b10']) {
      await page.locator('#doc-' + batch).evaluate(el => el.scrollIntoView({block: 'start', behavior: 'instant'}));
      await page.screenshot({ path: `${SHOTS}/final-${batch}-${width}.png`, fullPage: false });
    }
  }
  fs.mkdirSync(path.join(EVIDENCE, 'screenshots'), { recursive: true });
  for (const name of ['final-b04-1440.png', 'final-b04-390.png', 'final-b10-1440.png', 'final-b10-390.png']) {
    fs.copyFileSync(path.join(SHOTS, name), path.join(EVIDENCE, 'screenshots', name));
  }
  const crypto = require('crypto');
  const artifact = path.join(ROOT, "docs/api-database/index.html");
  fs.writeFileSync(path.join(EVIDENCE, "browser-checks.json"), JSON.stringify({
    artifact: 'docs/api-database/index.html',
    sha256: crypto.createHash('sha256').update(fs.readFileSync(artifact)).digest('hex'),
    structure,
    results,
    screenshots: ['screenshots/final-b04-1440.png', 'screenshots/final-b04-390.png', 'screenshots/final-b10-1440.png', 'screenshots/final-b10-390.png'],
    scope: 'Chromium local file; documentation only; no product service or database integration verification'
  }, null, 2) + '\n');
  await browser.close();
  const failed = results.filter((r) => !r.ok).length;
  console.log(`\n${results.length - failed}/${results.length} 浏览器检查通过`);
  process.exit(failed ? 1 : 0);
})();
