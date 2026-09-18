import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium } from 'file://wsl.localhost/Ubuntu-22.04/tmp/repomesh-b02-browser/node_modules/playwright/index.mjs';

const githubOrigin = 'https://github.com';
const expectedBLogin = 'bohanxu111';
const expectedALogin = 'catbobyman';
const namePattern = /^repomesh-b026-b-[0-9a-f]{24}$/;
const evidencePath = path.join(path.dirname(fileURLToPath(import.meta.url)), 'b-fixture-created.json');

function fail(code) {
  const error = new Error(code);
  error.code = code;
  throw error;
}

function positiveId(value, code) {
  if (typeof value !== 'string' || !/^[1-9][0-9]*$/.test(value)) fail(code);
  const parsed = Number(value);
  if (!Number.isSafeInteger(parsed)) fail(code);
  return parsed;
}

async function metaLogin(page) {
  const value = await page.locator('meta[name="user-login"]').getAttribute('content');
  return typeof value === 'string' && /^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$/.test(value) ? value : undefined;
}

function firstContext(browser, code) {
  const context = browser.contexts()[0];
  if (!context) fail(code);
  return context;
}

async function targetId(context, page) {
  const session = await context.newCDPSession(page);
  try {
    const result = await session.send('Target.getTargetInfo');
    if (!/^[A-F0-9]{32}$/.test(result.targetInfo.targetId)) fail('INVALID_TARGET_ID');
    return result.targetInfo.targetId;
  } finally {
    await session.detach();
  }
}

async function visibleExact(locator, code) {
  const matches = [];
  for (let index = 0; index < Math.min(await locator.count(), 20); index += 1) {
    if (await locator.nth(index).isVisible()) matches.push(locator.nth(index));
  }
  if (matches.length !== 1) fail(code);
  return matches[0];
}

async function bNewPages(browser) {
  const pages = [];
  for (const context of browser.contexts()) {
    for (const page of context.pages()) {
      let url;
      try { url = new URL(page.url()); } catch { continue; }
      if (url.origin !== githubOrigin || url.pathname !== '/new' || url.search || url.hash || await metaLogin(page) !== expectedBLogin) continue;
      const input = page.getByRole('textbox', { name: 'Repository name *', exact: true });
      if (await input.count() !== 1 || !await input.isVisible()) fail('NEW_PAGE_CONTROL_MISMATCH');
      const name = await input.inputValue();
      pages.push({ context, page, input, name, empty: name === '' });
    }
  }
  return pages;
}

async function requireAvailable(page, name) {
  const available = await page.evaluate(expected => Array.from(document.querySelectorAll('span')).some(element => {
    const box = element.getBoundingClientRect();
    const text = element.textContent ?? '';
    return element.children.length === 0 && box.width > 0 && box.height > 0 && text.includes(expected) && /is available\.?/i.test(text);
  }), name);
  if (!available) fail('NAME_NOT_CONFIRMED_AVAILABLE');
}

async function requireNoErrors(page) {
  const state = await page.evaluate(() => {
    const visible = element => {
      const box = element.getBoundingClientRect();
      return box.width > 0 && box.height > 0;
    };
    const invalid = Array.from(document.querySelectorAll('[aria-invalid="true"], :invalid')).filter(visible).length;
    const text = Array.from(document.querySelectorAll('[role="alert"], [aria-live], .color-fg-danger, .flash-error'))
      .filter(visible).map(element => element.textContent ?? '').join(' ').toLowerCase();
    const known = /already exists|name is already taken|unavailable|name.*required|required.*name|verify.*email|email.*verif|policy|not allowed|forbidden|restricted|owner.*required|required.*owner|invalid|error|problem|unable/.test(text);
    return { invalid: Math.min(invalid, 10), known };
  });
  if (state.invalid !== 0 || state.known) fail('FORM_NOT_READY');
}

async function requireResumeGuards(attempt) {
  if (!namePattern.test(attempt.name)) fail('FILLED_NAME_MISMATCH');
  await visibleExact(attempt.page.getByText(expectedBLogin, { exact: true }), 'OWNER_B_NOT_CONFIRMED');
  await visibleExact(attempt.page.getByRole('button', { name: 'Private', exact: true }), 'PRIVATE_SELECTION_NOT_CONFIRMED');
  const description = await visibleExact(attempt.page.getByRole('textbox', { name: 'Description', exact: true }), 'DESCRIPTION_CONTROL_MISMATCH');
  if (await description.inputValue() !== '') fail('DESCRIPTION_DEFAULT_CHANGED');
  await visibleExact(attempt.page.getByRole('button', { name: 'Add README', exact: true }), 'README_DEFAULT_CHANGED');
  await visibleExact(attempt.page.getByRole('button', { name: 'No .gitignore', exact: true }), 'GITIGNORE_DEFAULT_CHANGED');
  await visibleExact(attempt.page.getByRole('button', { name: 'No license', exact: true }), 'LICENSE_DEFAULT_CHANGED');
  await requireNoErrors(attempt.page);
  await requireAvailable(attempt.page, attempt.name);
  const create = await visibleExact(attempt.page.getByRole('button', { name: 'Create repository', exact: true }), 'CREATE_CONTROL_MISMATCH');
  if (await create.isDisabled() || await create.getAttribute('aria-disabled') === 'true') fail('CREATE_CONTROL_DISABLED');
  return create;
}

async function privateIndicator(page) {
  const direct = await page.locator('meta[name="octolytics-dimension-repository_is_private"]').getAttribute('content');
  if (direct === 'true') return 'META_IS_PRIVATE_TRUE';
  const visibility = await page.locator('meta[name="octolytics-dimension-repository_visibility"]').getAttribute('content');
  if (visibility === 'private') return 'META_VISIBILITY_PRIVATE';
  const text = page.getByText('Private repository', { exact: true });
  for (let index = 0; index < Math.min(await text.count(), 10); index += 1) if (await text.nth(index).isVisible()) return 'ACCESSIBLE_PRIVATE_REPOSITORY';
  fail('PRIVATE_STATUS_NOT_CONFIRMED');
}

async function repositoryId(page) {
  const meta = await page.locator('meta[name="octolytics-dimension-repository_id"]').getAttribute('content');
  if (typeof meta === 'string' && /^[1-9][0-9]*$/.test(meta)) return positiveId(meta, 'REPOSITORY_ID_INVALID');
  const values = await page.locator('[data-repository-id]').evaluateAll(elements => [...new Set(elements.map(element => element.getAttribute('data-repository-id')).filter(Boolean))]);
  if (values.length !== 1) fail('REPOSITORY_ID_INVALID');
  return positiveId(values[0], 'REPOSITORY_ID_INVALID');
}

async function owner(page) {
  const result = { login: expectedBLogin };
  const value = await page.locator('meta[name="octolytics-dimension-user_id"]').getAttribute('content');
  if (typeof value === 'string' && /^[1-9][0-9]*$/.test(value)) result.id = positiveId(value, 'OWNER_ID_INVALID');
  return result;
}

async function verifyAccountA(browser, name) {
  const context = firstContext(browser, 'A_CONTEXT_NOT_FOUND');
  const page = await context.newPage();
  const response = await page.goto(`${githubOrigin}/${expectedBLogin}/${name}`, { waitUntil: 'domcontentloaded', timeout: 30_000 });
  if (response?.status() !== 404 || await metaLogin(page) !== expectedALogin) fail('ACCOUNT_A_404_NOT_CONFIRMED');
  const heading = page.getByRole('heading', { name: 'Page not found', exact: true });
  const fixedVariant = (await heading.count() === 1 && await heading.isVisible()) || await page.title() === 'Page not found · GitHub';
  if (!fixedVariant) fail('ACCOUNT_A_NOT_FOUND_PAGE_MISSING');
  return { aCanAccess: false, aStatus: 404 };
}

function writeEvidence(value) {
  const fd = fs.openSync(evidencePath, 'wx', 0o600);
  try {
    fs.writeSync(fd, `${JSON.stringify(value, null, 2)}\n`);
    fs.fsyncSync(fd);
  } finally {
    fs.closeSync(fd);
  }
}

async function closeEmptyDuplicates(pages) {
  let closed = 0;
  for (const candidate of pages) {
    if (!candidate.empty) continue;
    const create = candidate.page.getByRole('button', { name: 'Create repository', exact: true });
    if (await create.count() !== 1 || !await create.isVisible() || await candidate.input.inputValue() !== '') continue;
    await candidate.page.close({ runBeforeUnload: false });
    closed += 1;
  }
  return Math.min(closed, 10);
}

async function main() {
  if (process.argv.length !== 2) fail('INVALID_ARGUMENTS');
  if (fs.existsSync(evidencePath)) fail('EVIDENCE_ALREADY_EXISTS');
  const bBrowser = await chromium.connectOverCDP('http://127.0.0.1:9230');
  const pages = await bNewPages(bBrowser);
  const filled = pages.filter(candidate => !candidate.empty);
  if (filled.length !== 1) fail(filled.length > 1 ? 'AMBIGUOUS_FILLED_PAGES' : 'FILLED_PAGE_NOT_FOUND');
  if (pages.some(candidate => candidate !== filled[0] && !candidate.empty)) fail('DUPLICATE_PAGE_NOT_EMPTY');
  const attempt = filled[0];
  const create = await requireResumeGuards(attempt);
  const expectedPath = `/${expectedBLogin}/${attempt.name}`;
  const navigation = attempt.page.waitForURL(url => url.origin === githubOrigin && url.pathname === expectedPath && !url.search && !url.hash, { timeout: 60_000 });
  await create.click();
  await navigation;
  if (await metaLogin(attempt.page) !== expectedBLogin) fail('ACCOUNT_B_CHANGED');
  const indicator = await privateIndicator(attempt.page);
  const id = await repositoryId(attempt.page);
  const ownerProjection = await owner(attempt.page);
  const bPageTargetId = await targetId(attempt.context, attempt.page);
  const aBrowser = await chromium.connectOverCDP('http://127.0.0.1:9231');
  const accountA = await verifyAccountA(aBrowser, attempt.name);
  writeEvidence({
    sampledAt: new Date().toISOString(), owner: ownerProjection, repositoryId: id,
    private: true, created: true, resumed: true, privateIndicator: indicator, bPageTargetId, ...accountA,
  });
  return { status: 'RESUMED', emptyDuplicatesClosed: await closeEmptyDuplicates(pages) };
}

try {
  process.stdout.write(`${JSON.stringify(await main())}\n`);
  process.exit(0);
} catch (error) {
  const errorCode = typeof error?.code === 'string' ? error.code : 'RESUME_FAILED';
  process.stdout.write(`${JSON.stringify({ status: 'FAIL', errorCode })}\n`);
  process.exit(1);
}
