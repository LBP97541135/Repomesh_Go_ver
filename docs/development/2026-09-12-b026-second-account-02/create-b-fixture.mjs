import crypto from 'node:crypto';
import fs from 'node:fs';
import path from 'node:path';
import { chromium } from 'file://wsl.localhost/Ubuntu-22.04/tmp/repomesh-b02-browser/node_modules/playwright/index.mjs';

const githubOrigin = 'https://github.com';
const newRepositoryURL = 'https://github.com/new';
const expectedBLogin = 'bohanxu111';
const expectedALogin = 'catbobyman';
const bCDP = 'http://127.0.0.1:9230';
const aCDP = 'http://127.0.0.1:9231';

function fail(code) {
  const error = new Error(code);
  error.code = code;
  throw error;
}

function sampledAt() {
  return new Date().toISOString();
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

function firstContext(browser, code) {
  const context = browser.contexts()[0];
  if (!context) fail(code);
  return context;
}

async function requireExactNewPage(page) {
  const url = new URL(page.url());
  if (url.origin !== githubOrigin || url.pathname !== '/new' || url.search || url.hash) fail('NEW_REPOSITORY_PAGE_MISMATCH');
  if (await metaLogin(page) !== expectedBLogin) fail('ACCOUNT_B_REQUIRED');
}

async function requireOneVisible(locator, code) {
  const visible = [];
  for (let index = 0; index < Math.min(await locator.count(), 20); index += 1) {
    if (await locator.nth(index).isVisible()) visible.push(locator.nth(index));
  }
  if (visible.length !== 1) fail(code);
  return visible[0];
}

async function assertDefaults(page) {
  const description = await requireOneVisible(page.getByRole('textbox', { name: 'Description', exact: true }), 'DESCRIPTION_CONTROL_MISMATCH');
  if (await description.inputValue() !== '') fail('DESCRIPTION_DEFAULT_CHANGED');
  await requireOneVisible(page.getByRole('button', { name: 'Add README', exact: true }), 'README_DEFAULT_CHANGED');
  await requireOneVisible(page.getByRole('button', { name: 'No .gitignore', exact: true }), 'GITIGNORE_DEFAULT_CHANGED');
  await requireOneVisible(page.getByRole('button', { name: 'No license', exact: true }), 'LICENSE_DEFAULT_CHANGED');
}

async function createPrivateRepository(browser) {
  const context = firstContext(browser, 'B_CONTEXT_NOT_FOUND');
  const page = await context.newPage();
  await page.goto(newRepositoryURL, { waitUntil: 'domcontentloaded', timeout: 30_000 });
  await requireExactNewPage(page);

  await requireOneVisible(page.getByText(expectedBLogin, { exact: true }), 'OWNER_B_NOT_CONFIRMED');
  const nameInput = await requireOneVisible(page.getByRole('textbox', { name: 'Repository name *', exact: true }), 'REPOSITORY_NAME_CONTROL_MISMATCH');
  const visibility = await requireOneVisible(page.getByRole('button', { name: 'Public', exact: true }), 'VISIBILITY_CONTROL_MISMATCH');
  await nameInput.fill(`repomesh-b026-b-${crypto.randomBytes(12).toString('hex')}`);
  await visibility.click();
  const privateChoice = await requireOneVisible(page.getByText('Private', { exact: true }), 'PRIVATE_CONTROL_MISMATCH');
  await privateChoice.click();
  await requireOneVisible(page.getByRole('button', { name: 'Private', exact: true }), 'PRIVATE_SELECTION_NOT_CONFIRMED');
  await assertDefaults(page);

  const repositoryName = await nameInput.inputValue();
  if (!/^repomesh-b026-b-[0-9a-f]{24}$/.test(repositoryName)) fail('GENERATED_NAME_MISMATCH');
  await page.waitForFunction(input => {
    const value = input instanceof HTMLInputElement ? input.value : '';
    if (!value) return false;
    const statusText = Array.from(document.querySelectorAll('span'))
      .filter(element => {
        const box = element.getBoundingClientRect();
        return box.width > 0 && box.height > 0 && element.children.length === 0;
      })
      .map(element => element.textContent ?? '')
      .join(' ');
    return statusText.includes(value) && /is available\.?/i.test(statusText);
  }, await nameInput.elementHandle(), { timeout: 20_000 });
  const expectedPath = `/${expectedBLogin}/${repositoryName}`;
  const create = await requireOneVisible(page.getByRole('button', { name: 'Create repository', exact: true }), 'CREATE_CONTROL_MISMATCH');
  const navigation = page.waitForURL(url => url.origin === githubOrigin && url.pathname === expectedPath, { timeout: 60_000 });
  await create.click();
  await navigation;

  const finalURL = new URL(page.url());
  if (finalURL.origin !== githubOrigin || finalURL.pathname !== expectedPath || finalURL.search || finalURL.hash) fail('CREATED_REPOSITORY_PATH_MISMATCH');
  if (await metaLogin(page) !== expectedBLogin) fail('ACCOUNT_B_CHANGED');
  const repositoryId = positiveId(
    await page.locator('meta[name="octolytics-dimension-repository_id"]').getAttribute('content'),
    'REPOSITORY_ID_INVALID',
  );
  const privateValue = await page.locator('meta[name="octolytics-dimension-repository_is_private"]').getAttribute('content');
  if (privateValue !== 'true') fail('REPOSITORY_NOT_PRIVATE');
  const ownerIdValue = await page.locator('meta[name="octolytics-dimension-user_id"]').getAttribute('content');
  const owner = { login: expectedBLogin };
  if (typeof ownerIdValue === 'string' && /^[1-9][0-9]*$/.test(ownerIdValue)) owner.id = positiveId(ownerIdValue, 'OWNER_ID_INVALID');
  return {
    page,
    targetId: await targetId(context, page),
    repositoryName,
    repositoryId,
    owner,
  };
}

async function verifyAccountACannotAccess(browser, repositoryName) {
  const context = firstContext(browser, 'A_CONTEXT_NOT_FOUND');
  const page = await context.newPage();
  const response = await page.goto(`${githubOrigin}/${expectedBLogin}/${repositoryName}`, { waitUntil: 'domcontentloaded', timeout: 30_000 });
  if (response?.status() !== 404) fail('ACCOUNT_A_ACCESS_NOT_404');
  if (await metaLogin(page) !== expectedALogin) fail('ACCOUNT_A_REQUIRED');
  const notFound = page.getByRole('heading', { name: 'Page not found', exact: true });
  if (await notFound.count() !== 1 || !await notFound.isVisible()) fail('ACCOUNT_A_NOT_FOUND_PAGE_MISSING');
  return { aCanAccess: false, aStatus: 404 };
}

function evidencePath() {
  if (process.argv.length !== 3) fail('INVALID_ARGUMENTS');
  const value = process.argv[2];
  if (typeof value !== 'string' || !path.isAbsolute(value) || value.length > 4096) fail('INVALID_EVIDENCE_PATH');
  if (fs.existsSync(value)) fail('EVIDENCE_ALREADY_EXISTS');
  const parent = fs.lstatSync(path.dirname(value));
  if (!parent.isDirectory() || parent.isSymbolicLink()) fail('EVIDENCE_PARENT_INVALID');
  return value;
}

function writeEvidence(filename, value) {
  const fd = fs.openSync(filename, 'wx', 0o600);
  try {
    fs.writeSync(fd, `${JSON.stringify(value, null, 2)}\n`);
    fs.fsyncSync(fd);
  } finally {
    fs.closeSync(fd);
  }
}

async function main() {
  const output = evidencePath();
  const bBrowser = await chromium.connectOverCDP(bCDP);
  const created = await createPrivateRepository(bBrowser);
  const aBrowser = await chromium.connectOverCDP(aCDP);
  const accountA = await verifyAccountACannotAccess(aBrowser, created.repositoryName);
  writeEvidence(output, {
    sampledAt: sampledAt(),
    owner: created.owner,
    repositoryId: created.repositoryId,
    private: true,
    created: true,
    bPageTargetId: created.targetId,
    ...accountA,
  });
}

try {
  await main();
  process.stdout.write('{"status":"FIXTURE_CREATED"}\n');
  process.exit(0);
} catch (error) {
  const errorCode = typeof error?.code === 'string' ? error.code : 'FIXTURE_CREATION_FAILED';
  process.stdout.write(`${JSON.stringify({ status: 'FAIL', errorCode })}\n`);
  process.exit(1);
}
