import crypto from 'node:crypto';
import fs from 'node:fs';
import path from 'node:path';
import { chromium } from 'file://wsl.localhost/Ubuntu-22.04/tmp/repomesh-b02-browser/node_modules/playwright/index.mjs';

const configPath = '\\\\wsl.localhost\\Ubuntu-22.04\\home\\xubohan\\.config\\repomesh\\auth.json';
const apiRoot = 'https://api.github.com';
const apiVersion = '2026-03-10';
const expectedAppSlug = 'repomesh-bohan-b026';
const expectedInstallationId = 161172403;
const expectedRepositoryId = 1367444901;
const expectedALogin = 'catbobyman';
const expectedBLogin = 'bohanxu111';
const expectedBId = 328458192;

function fail(code) { const error = new Error(code); error.code = code; throw error; }
function b64(value) { return Buffer.from(JSON.stringify(value)).toString('base64url'); }
function positiveId(value, code) {
  const parsed = Number(value);
  if (!Number.isSafeInteger(parsed) || parsed <= 0 || String(parsed) !== String(value)) fail(code);
  return parsed;
}
function windowsPath(linuxPath) {
  if (typeof linuxPath !== 'string' || !linuxPath.startsWith('/home/xubohan/')) fail('PRIVATE_KEY_PATH_INVALID');
  return `\\\\wsl.localhost\\Ubuntu-22.04${linuxPath.replaceAll('/', '\\')}`;
}

function appJWT(appId, privateKey) {
  const now = Math.floor(Date.now() / 1000);
  const input = `${b64({ alg: 'RS256', typ: 'JWT' })}.${b64({ iat: now - 60, exp: now + 540, iss: String(appId) })}`;
  try { return `${input}.${crypto.sign('RSA-SHA256', Buffer.from(input), privateKey).toString('base64url')}`; }
  catch { fail('JWT_SIGNING_FAILED'); }
}

function headers(token) {
  return { Accept: 'application/vnd.github+json', Authorization: `Bearer ${token}`, 'X-GitHub-Api-Version': apiVersion, 'User-Agent': 'RepoMesh-B02.6-invite' };
}

async function api(method, endpoint, token, body) {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), 30_000);
  try {
    const response = await fetch(`${apiRoot}${endpoint}`, {
      method, headers: { ...headers(token), ...(body ? { 'Content-Type': 'application/json' } : {}) },
      body: body ? JSON.stringify(body) : undefined, signal: controller.signal,
    });
    if (!response.ok && response.status !== 204) fail([401, 403, 404, 422, 429].includes(response.status) ? `GITHUB_HTTP_${response.status}` : 'GITHUB_HTTP_ERROR');
    return response.status === 204 ? null : await response.json();
  } catch (error) {
    if (typeof error?.code === 'string') throw error;
    fail('GITHUB_REQUEST_FAILED');
  } finally { clearTimeout(timer); }
}

async function resolveRepository() {
  let config;
  let privateKey;
  try {
    config = JSON.parse(fs.readFileSync(configPath, 'utf8'));
    privateKey = fs.readFileSync(windowsPath(config.privateKeyFile));
  } catch { fail('AUTH_MATERIAL_INVALID'); }
  const appId = positiveId(config.appId, 'APP_ID_INVALID');
  const jwt = appJWT(appId, privateKey);
  const app = await api('GET', '/app', jwt);
  if (positiveId(app?.id, 'APP_RESPONSE_INVALID') !== appId || app?.slug !== expectedAppSlug) fail('APP_IDENTITY_MISMATCH');
  const installation = await api('GET', `/app/installations/${expectedInstallationId}`, jwt);
  if (
    positiveId(installation?.id, 'INSTALLATION_RESPONSE_INVALID') !== expectedInstallationId ||
    positiveId(installation?.app_id, 'INSTALLATION_RESPONSE_INVALID') !== appId ||
    installation?.account?.login !== expectedALogin ||
    installation.repository_selection !== 'selected' ||
    installation.permissions?.metadata !== 'read' || installation.permissions?.contents !== 'write' || installation.permissions?.pull_requests !== 'write'
  ) fail('INSTALLATION_GUARD_FAILED');
  const inviterId = positiveId(installation.account.id, 'INVITER_ID_INVALID');
  const tokenResponse = await api('POST', `/app/installations/${expectedInstallationId}/access_tokens`, jwt, {});
  const token = tokenResponse?.token;
  if (typeof token !== 'string' || !token) fail('INSTALLATION_TOKEN_INVALID');
  let repositories;
  let revokeFailed = false;
  try {
    repositories = await api('GET', '/installation/repositories?per_page=100', token);
  } finally {
    try { await api('DELETE', '/installation/token', token); } catch { revokeFailed = true; }
  }
  if (revokeFailed) fail('INSTALLATION_TOKEN_REVOKE_FAILED');
  if (repositories?.total_count !== 1 || !Array.isArray(repositories.repositories)) fail('REPOSITORY_SET_INVALID');
  const matches = repositories.repositories.filter(repository => repository?.id === expectedRepositoryId && repository?.owner?.login === expectedALogin);
  if (matches.length !== 1 || typeof matches[0].name !== 'string' || !/^[A-Za-z0-9._-]{1,100}$/.test(matches[0].name)) fail('REPOSITORY_IDENTITY_MISMATCH');
  return { name: matches[0].name, inviterId };
}

async function targetId(context, page) {
  const session = await context.newCDPSession(page);
  try { const info = await session.send('Target.getTargetInfo'); if (!/^[A-F0-9]{32}$/.test(info.targetInfo.targetId)) fail('TARGET_ID_INVALID'); return info.targetInfo.targetId; }
  finally { await session.detach(); }
}
async function meta(page, name) { const item = page.locator(`meta[name="${name}"]`); return await item.count() === 1 ? await item.getAttribute('content') : null; }
async function visible(locator) {
  const result = [];
  for (let index = 0; index < Math.min(await locator.count(), 20); index += 1) if (await locator.nth(index).isVisible()) result.push(locator.nth(index));
  return result;
}
async function exactlyOne(locator, code) { const result = await visible(locator); if (result.length !== 1) fail(code); return result[0]; }
async function waitForOne(locators, timeout = 15_000) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    const matches = [];
    for (const locator of locators) matches.push(...await visible(locator));
    if (matches.length === 1) return matches[0];
    if (matches.length > 1) return undefined;
    await new Promise(resolve => setTimeout(resolve, 250));
  }
  return undefined;
}

async function needsHuman(context, page, controlState) {
  return { status: 'NEEDS_HUMAN', targetId: await targetId(context, page), pathClass: 'REPOSITORY_ACCESS', controlState };
}

async function pendingInviteConfirmed(page) {
  const logins = await visible(page.getByText(expectedBLogin, { exact: true }));
  for (const login of logins) {
    const confirmed = await login.evaluate(element => {
      const container = element.closest('li, tr, [role="row"], [data-testid], .Box-row');
      const text = container?.textContent ?? '';
      const controls = [...(container?.querySelectorAll('button, [role="button"]') ?? [])]
        .map(control => `${control.getAttribute('aria-label') ?? ''} ${control.textContent ?? ''}`);
      return /pending|invited|awaiting/i.test(text) && controls.some(label => /remove|cancel invitation|revoke/i.test(label));
    });
    if (confirmed) return true;
  }
  return false;
}

async function resumableAccessPage(context, repositoryName) {
  const expectedPath = `/${expectedALogin}/${repositoryName}/settings/access`;
  const safe = [];
  const pending = [];
  for (const page of context.pages()) {
    let url;
    try { url = new URL(page.url()); } catch { continue; }
    if (url.origin !== 'https://github.com' || url.pathname !== expectedPath || url.search || url.hash) continue;
    if (await meta(page, 'user-login') !== expectedALogin || await meta(page, 'octolytics-dimension-repository_id') !== String(expectedRepositoryId)) continue;
    safe.push(page);
    if (await pendingInviteConfirmed(page)) pending.push(page);
  }
  if (pending.length > 1) fail('PENDING_PAGE_AMBIGUOUS');
  if (pending.length === 1) return pending[0];
  if (safe.length > 1) fail('ACCESS_PAGE_AMBIGUOUS');
  return safe[0];
}

async function writePendingEvidence(context, page, repository, evidencePath) {
  const evidence = {
    sampledAt: new Date().toISOString(), repositoryId: expectedRepositoryId,
    inviter: { id: repository.inviterId, login: expectedALogin },
    invitee: { id: expectedBId, login: expectedBLogin }, status: 'PENDING',
    aTargetId: await targetId(context, page),
  };
  const fd = fs.openSync(evidencePath, 'wx', 0o600);
  try { fs.writeSync(fd, `${JSON.stringify(evidence, null, 2)}\n`); fs.fsyncSync(fd); }
  finally { fs.closeSync(fd); }
  return { status: 'INVITE_PENDING' };
}

async function invite(repository, evidencePath) {
  const browser = await chromium.connectOverCDP('http://127.0.0.1:9231');
  const context = browser.contexts()[0];
  if (!context) fail('A_CONTEXT_NOT_FOUND');
  let page = await resumableAccessPage(context, repository.name);
  if (!page) {
    page = await context.newPage();
    await page.goto(`https://github.com/${expectedALogin}/${repository.name}/settings/access`, { waitUntil: 'domcontentloaded', timeout: 30_000 });
  }
  if (await meta(page, 'user-login') !== expectedALogin || positiveId(await meta(page, 'octolytics-dimension-repository_id'), 'UI_REPOSITORY_ID_INVALID') !== expectedRepositoryId) fail('UI_IDENTITY_MISMATCH');
  if (await pendingInviteConfirmed(page)) return writePendingEvidence(context, page, repository, evidencePath);
  let searchInput = await waitForOne([page.getByRole('combobox', { name: 'Find people', exact: true })], 500);
  if (!searchInput) {
    if ((await visible(page.getByText(expectedBLogin, { exact: true }))).length !== 0) fail('INVITEE_ALREADY_PRESENT');
    const addPeople = await exactlyOne(page.getByRole('button', { name: 'Add people', exact: true }), 'ADD_PEOPLE_CONTROL_MISMATCH');
    await addPeople.click();
    const current = new URL(page.url());
    if (current.origin !== 'https://github.com' || current.pathname.includes('/sessions/')) return needsHuman(context, page, 'AUTHENTICATION_REQUIRED');
    searchInput = await waitForOne([page.getByRole('combobox', { name: 'Find people', exact: true })]);
  }
  if (!searchInput) return needsHuman(context, page, 'SEARCH_CONTROL_AMBIGUOUS');
  await searchInput.fill(expectedBLogin);
  const candidate = await waitForOne([page.getByText(expectedBLogin, { exact: true })]);
  if (!candidate) return needsHuman(context, page, 'INVITEE_CANDIDATE_AMBIGUOUS');
  const exposedId = await candidate.evaluate(element => {
    const source = element.closest('[data-user-id], [data-hovercard-url]');
    const direct = source?.getAttribute('data-user-id');
    const hover = source?.getAttribute('data-hovercard-url') ?? '';
    return direct ?? /user_id=([0-9]+)/.exec(hover)?.[1] ?? null;
  });
  if (exposedId !== null && positiveId(exposedId, 'INVITEE_ID_INVALID') !== expectedBId) fail('INVITEE_ID_MISMATCH');
  await candidate.click();
  const finalLabels = [`Add ${expectedBLogin} to this repository`, `Invite ${expectedBLogin}`, `Add ${expectedBLogin}`];
  const finalControl = await waitForOne(finalLabels.map(label => page.getByRole('button', { name: label, exact: true })));
  if (!finalControl) return needsHuman(context, page, 'FINAL_INVITE_CONTROL_AMBIGUOUS');
  const typedInputs = await visible(page.locator('input[type="password"], input[autocomplete="one-time-code"]'));
  if (typedInputs.length > 0) return needsHuman(context, page, 'AUTHENTICATION_INPUT_REQUIRED');
  await finalControl.click();
  const pendingDeadline = Date.now() + 15_000;
  while (Date.now() < pendingDeadline && !await pendingInviteConfirmed(page)) await page.waitForTimeout(250);
  if (!await pendingInviteConfirmed(page)) return needsHuman(context, page, 'PENDING_STATE_NOT_CONFIRMED');
  return writePendingEvidence(context, page, repository, evidencePath);
}

async function main() {
  if (process.argv.length !== 3 || !path.win32.isAbsolute(process.argv[2]) || fs.existsSync(process.argv[2])) fail('EVIDENCE_PATH_INVALID');
  const repository = await resolveRepository();
  return invite(repository, process.argv[2]);
}

try { process.stdout.write(`${JSON.stringify(await main())}\n`); process.exit(0); }
catch (error) { const errorCode = typeof error?.code === 'string' ? error.code : 'INVITE_FAILED'; process.stdout.write(`${JSON.stringify({ status: 'FAIL', errorCode })}\n`); process.exit(1); }
