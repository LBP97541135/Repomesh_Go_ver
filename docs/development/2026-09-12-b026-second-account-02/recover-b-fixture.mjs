import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium } from 'file://wsl.localhost/Ubuntu-22.04/tmp/repomesh-b02-browser/node_modules/playwright/index.mjs';

const githubOrigin = 'https://github.com';
const expectedBLogin = 'bohanxu111';
const expectedALogin = 'catbobyman';
const repositoryPath = /^\/bohanxu111\/(repomesh-b026-b-[0-9a-f]{24})$/;
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

function safeLogin(value) {
  return typeof value === 'string' && /^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$/.test(value)
    ? value
    : undefined;
}

async function metaLogin(page) {
  return safeLogin(await page.locator('meta[name="user-login"]').getAttribute('content'));
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

async function repositoryId(page) {
  const meta = await page.locator('meta[name="octolytics-dimension-repository_id"]').getAttribute('content');
  if (typeof meta === 'string' && /^[1-9][0-9]*$/.test(meta)) return positiveId(meta, 'REPOSITORY_ID_INVALID');
  const values = await page.locator('[data-repository-id]').evaluateAll(elements => [...new Set(elements.map(element => element.getAttribute('data-repository-id')).filter(Boolean))]);
  if (values.length !== 1) fail('REPOSITORY_ID_INVALID');
  return positiveId(values[0], 'REPOSITORY_ID_INVALID');
}

async function privateIndicator(page) {
  const privateMeta = page.locator('meta[name="octolytics-dimension-repository_is_private"]');
  const isPrivate = await privateMeta.count() === 1 ? await privateMeta.getAttribute('content') : null;
  if (isPrivate === 'true') return 'META_IS_PRIVATE_TRUE';
  const visibilityMeta = page.locator('meta[name="octolytics-dimension-repository_visibility"]');
  const visibility = await visibilityMeta.count() === 1 ? await visibilityMeta.getAttribute('content') : null;
  if (visibility === 'private') return 'META_VISIBILITY_PRIVATE';
  const accessible = page.getByText('Private repository', { exact: true });
  for (let index = 0; index < Math.min(await accessible.count(), 10); index += 1) {
    if (await accessible.nth(index).isVisible()) return 'ACCESSIBLE_PRIVATE_REPOSITORY';
  }
  const exactPrivate = page.getByText('Private', { exact: true });
  const exactPublic = page.getByText('Public', { exact: true });
  let visiblePrivate = 0;
  let visiblePublic = 0;
  for (let index = 0; index < Math.min(await exactPrivate.count(), 10); index += 1) if (await exactPrivate.nth(index).isVisible()) visiblePrivate += 1;
  for (let index = 0; index < Math.min(await exactPublic.count(), 10); index += 1) if (await exactPublic.nth(index).isVisible()) visiblePublic += 1;
  if (visiblePrivate > 0 && visiblePublic === 0) return 'ACCESSIBLE_PRIVATE_EXACT';
  fail('PRIVATE_STATUS_NOT_CONFIRMED');
}

async function ownerProjection(page) {
  const owner = { login: expectedBLogin };
  const value = await page.locator('meta[name="octolytics-dimension-user_id"]').getAttribute('content');
  if (typeof value === 'string' && /^[1-9][0-9]*$/.test(value)) owner.id = positiveId(value, 'OWNER_ID_INVALID');
  return owner;
}

async function matchingCreatedPages(browser) {
  const matches = [];
  for (const context of browser.contexts()) {
    for (const page of context.pages()) {
      let url;
      try { url = new URL(page.url()); } catch { continue; }
      if (url.origin !== githubOrigin || url.search || url.hash) continue;
      const match = repositoryPath.exec(url.pathname);
      if (!match || await metaLogin(page) !== expectedBLogin) continue;
      matches.push({ context, page, repositoryName: match[1] });
    }
  }
  return matches;
}

async function visibleExactCount(locator) {
  let count = 0;
  for (let index = 0; index < Math.min(await locator.count(), 20); index += 1) {
    if (await locator.nth(index).isVisible()) count += 1;
  }
  return Math.min(count, 10);
}

async function safeNewPageDiagnostic(browser) {
  const pages = [];
  for (const context of browser.contexts()) {
    for (const page of context.pages()) {
      let url;
      try { url = new URL(page.url()); } catch { continue; }
      if (url.origin === githubOrigin && url.pathname === '/new' && !url.search && !url.hash && await metaLogin(page) === expectedBLogin) pages.push(page);
    }
  }
  if (pages.length !== 1) {
    return {
      status: 'NO_CREATED_PAGE',
      newPagePresent: pages.length > 0,
      controls: { repositoryNameTextbox: 0, publicVisibilityButton: 0, privateVisibilityButton: 0, addReadmeButton: 0, noGitignoreButton: 0, noLicenseButton: 0, createRepositoryButton: 0, ownerExactText: 0 },
    };
  }
  const page = pages[0];
  return {
    status: 'NO_CREATED_PAGE',
    newPagePresent: true,
    controls: {
      repositoryNameTextbox: await visibleExactCount(page.getByRole('textbox', { name: 'Repository name *', exact: true })),
      publicVisibilityButton: await visibleExactCount(page.getByRole('button', { name: 'Public', exact: true })),
      privateVisibilityButton: await visibleExactCount(page.getByRole('button', { name: 'Private', exact: true })),
      addReadmeButton: await visibleExactCount(page.getByRole('button', { name: 'Add README', exact: true })),
      noGitignoreButton: await visibleExactCount(page.getByRole('button', { name: 'No .gitignore', exact: true })),
      noLicenseButton: await visibleExactCount(page.getByRole('button', { name: 'No license', exact: true })),
      createRepositoryButton: await visibleExactCount(page.getByRole('button', { name: 'Create repository', exact: true })),
      ownerExactText: await visibleExactCount(page.getByText(expectedBLogin, { exact: true })),
    },
  };
}

async function closeEmptyNewPages(browser) {
  const pages = [];
  for (const context of browser.contexts()) {
    for (const page of context.pages()) {
      let url;
      try { url = new URL(page.url()); } catch { continue; }
      if (url.origin !== githubOrigin || url.pathname !== '/new' || url.search || url.hash || await metaLogin(page) !== expectedBLogin) continue;
      const name = page.getByRole('textbox', { name: 'Repository name *', exact: true });
      const create = page.getByRole('button', { name: 'Create repository', exact: true });
      if (await name.count() === 1 && await name.isVisible() && await name.inputValue() === '' && await create.count() === 1 && await create.isVisible()) pages.push(page);
    }
  }
  for (const page of pages) await page.close({ runBeforeUnload: false });
  return { status: 'EMPTY_NEW_PAGES_CLOSED', closedCount: Math.min(pages.length, 10) };
}

async function verifyAccountA(browser, repositoryName) {
  const context = firstContext(browser, 'A_CONTEXT_NOT_FOUND');
  const page = await context.newPage();
  const response = await page.goto(`${githubOrigin}/${expectedBLogin}/${repositoryName}`, { waitUntil: 'domcontentloaded', timeout: 30_000 });
  if (response?.status() !== 404) fail('ACCOUNT_A_ACCESS_NOT_404');
  if (await metaLogin(page) !== expectedALogin) fail('ACCOUNT_A_REQUIRED');
  let fixedNotFoundVariant = false;
  const heading = page.getByRole('heading', { name: 'Page not found', exact: true });
  if (await heading.count() === 1 && await heading.isVisible()) fixedNotFoundVariant = true;
  if (await page.title() === 'Page not found · GitHub') fixedNotFoundVariant = true;
  if (!fixedNotFoundVariant) fail('ACCOUNT_A_NOT_FOUND_PAGE_MISSING');
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

async function main() {
  if (process.argv.length > 3 || (process.argv.length === 3 && process.argv[2] !== 'close-empty-new-pages')) fail('INVALID_ARGUMENTS');
  if (fs.existsSync(evidencePath)) fail('EVIDENCE_ALREADY_EXISTS');
  const bBrowser = await chromium.connectOverCDP('http://127.0.0.1:9230');
  if (process.argv[2] === 'close-empty-new-pages') return closeEmptyNewPages(bBrowser);
  const matches = await matchingCreatedPages(bBrowser);
  if (matches.length > 1) fail('AMBIGUOUS_CREATED_PAGES');
  if (matches.length === 0) return safeNewPageDiagnostic(bBrowser);

  const recovered = matches[0];
  const indicator = await privateIndicator(recovered.page);
  const id = await repositoryId(recovered.page);
  const owner = await ownerProjection(recovered.page);
  const bPageTargetId = await targetId(recovered.context, recovered.page);
  const aBrowser = await chromium.connectOverCDP('http://127.0.0.1:9231');
  const accountA = await verifyAccountA(aBrowser, recovered.repositoryName);
  writeEvidence({
    sampledAt: new Date().toISOString(),
    owner,
    repositoryId: id,
    private: true,
    created: true,
    recovered: true,
    privateIndicator: indicator,
    bPageTargetId,
    ...accountA,
  });
  return { status: 'RECOVERED' };
}

try {
  process.stdout.write(`${JSON.stringify(await main())}\n`);
  process.exit(0);
} catch (error) {
  const errorCode = typeof error?.code === 'string' ? error.code : 'RECOVERY_FAILED';
  process.stdout.write(`${JSON.stringify({ status: 'FAIL', errorCode })}\n`);
  process.exit(1);
}
