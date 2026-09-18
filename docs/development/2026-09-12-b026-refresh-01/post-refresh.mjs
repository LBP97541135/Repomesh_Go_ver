import fs from 'node:fs';
import path from 'node:path';
import { chromium } from 'file://wsl.localhost/Ubuntu-22.04/tmp/repomesh-b02-browser/node_modules/playwright/index.mjs';

const allowedOrigin = 'https://repomesh.bohanxu.me:8443';
const targetId = 'CBEA86CD7C41D085994D442CEB5B60A4';
const expectedActor = '16198e14-7249-4cc2-a15e-57cd8b568874';
const expectedFixtureId = 'repo_00000000001367444901';
const fixtureQuery = 'repomesh-b026-acceptance';
const runDir = process.argv[2];

if (typeof runDir !== 'string' || runDir.length === 0) {
  process.stderr.write('INVALID_RUN_DIRECTORY\n');
  process.exit(2);
}

const eventPath = path.join(runDir, 'browser-events.jsonl');
const resultPath = path.join(runDir, 'browser-result.json');
let eventFd;
let exitCode = 1;
let result = {
  status: 'FAIL',
  scope: 'POST_REFRESH_BROWSER_ONLY',
  errorCode: 'NOT_STARTED',
};

function writeEvent(event, fields = {}) {
  fs.writeSync(eventFd, `${JSON.stringify({ ts: new Date().toISOString(), event, ...fields })}\n`);
}

function fail(code) {
  const error = new Error(code);
  error.code = code;
  throw error;
}

function object(value, code) {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) fail(code);
  return value;
}

function string(value, code) {
  if (typeof value !== 'string' || value.length === 0) fail(code);
  return value;
}

function instant(value, code) {
  const parsed = Date.parse(string(value, code));
  if (!Number.isFinite(parsed)) fail(code);
  return parsed;
}

function integer(value, code) {
  const parsed = typeof value === 'number' ? value : Number(value);
  if (!Number.isSafeInteger(parsed)) fail(code);
  return parsed;
}

function readSnapshot(filename) {
  const filenamePath = path.join(runDir, filename);
  const stat = fs.lstatSync(filenamePath);
  if (!stat.isFile() || stat.isSymbolicLink()) fail('INVALID_SNAPSHOT_FILE');
  return object(JSON.parse(fs.readFileSync(filenamePath, 'utf8')), 'INVALID_SNAPSHOT');
}

function validateRefresh(baseline, confirmed) {
  if (baseline.fixtureId !== expectedFixtureId) fail('FIXTURE_ID_MISMATCH');
  if (confirmed.phase !== 'natural_refresh_confirmed') fail('CONFIRMATION_PHASE_MISMATCH');
  const before = object(baseline.connection, 'INVALID_BASELINE_CONNECTION');
  const after = object(confirmed.connection, 'INVALID_CONFIRMED_CONNECTION');
  const baselineSampledAt = instant(baseline.sampledAt, 'INVALID_BASELINE_SAMPLED_AT');
  const confirmedSampledAt = instant(confirmed.sampledAt, 'INVALID_CONFIRMED_SAMPLED_AT');
  const refreshDueAt = instant(before.refresh_due_at, 'INVALID_REFRESH_DUE_AT');
  const committedAt = instant(after.credential_committed_at, 'INVALID_CREDENTIAL_COMMITTED_AT');
  if (before.actor !== expectedActor || after.actor !== expectedActor) fail('ACTOR_MISMATCH');
  if (confirmedSampledAt <= baselineSampledAt) fail('CONFIRMED_SAMPLE_NOT_LATER');
  if (committedAt < refreshDueAt) fail('REFRESH_COMMITTED_BEFORE_DUE');
  if (after.status !== 'connected' || after.refresh_state !== 'idle') fail('REFRESH_NOT_SETTLED');
  if (before.has_refresh_token !== true || after.has_refresh_token !== true) fail('REFRESH_TOKEN_NOT_CONFIRMED');
  if (integer(after.access_epoch, 'INVALID_ACCESS_EPOCH') !== integer(before.access_epoch, 'INVALID_ACCESS_EPOCH') + 1) fail('ACCESS_EPOCH_NOT_INCREMENTED');
  if (string(after.revision, 'INVALID_REVISION') === string(before.revision, 'INVALID_REVISION')) fail('REVISION_NOT_CHANGED');
  for (const field of ['credential_committed_at', 'access_expires_at', 'refresh_expires_at']) {
    if (instant(after[field], `INVALID_${field.toUpperCase()}`) <= instant(before[field], `INVALID_${field.toUpperCase()}`)) {
      fail(`${field.toUpperCase()}_NOT_ADVANCED`);
    }
  }
  if (!Array.isArray(baseline.attempts) || !Array.isArray(confirmed.attempts)) fail('INVALID_ATTEMPTS');
  const baselineAttemptIds = new Set(baseline.attempts.map(attempt => string(object(attempt, 'INVALID_ATTEMPT').id, 'INVALID_ATTEMPT_ID')));
  for (const attempt of confirmed.attempts) {
    if (!baselineAttemptIds.has(string(object(attempt, 'INVALID_ATTEMPT').id, 'INVALID_ATTEMPT_ID'))) fail('NEW_ATTEMPT_AFTER_BASELINE');
  }
  if (!Array.isArray(baseline.sessions) || !Array.isArray(confirmed.sessions)) fail('INVALID_SESSIONS');
  const activeGenerations = (sessions, sampledAt) => new Set(sessions.flatMap(value => {
    const session = object(value, 'INVALID_SESSION');
    if (session.actor !== expectedActor || session.revoked !== false || instant(session.expires_at, 'INVALID_SESSION_EXPIRY') <= sampledAt) return [];
    return [integer(session.generation, 'INVALID_SESSION_GENERATION')];
  }));
  const baselineGenerations = activeGenerations(baseline.sessions, baselineSampledAt);
  const confirmedGenerations = activeGenerations(confirmed.sessions, confirmedSampledAt);
  const sharedSessionGenerations = [...baselineGenerations].filter(generation => confirmedGenerations.has(generation));
  if (sharedSessionGenerations.length !== 1) fail('SAME_ACTIVE_SESSION_NOT_CONFIRMED');
  return {
    actor: after.actor,
    accessEpochBefore: integer(before.access_epoch, 'INVALID_ACCESS_EPOCH'),
    accessEpochAfter: integer(after.access_epoch, 'INVALID_ACCESS_EPOCH'),
    revisionChanged: true,
    credentialCommittedAdvanced: true,
    accessExpiryAdvanced: true,
    refreshExpiryAdvanced: true,
    noNewAttemptIds: true,
    refreshCommittedAfterDue: true,
    sessionGeneration: sharedSessionGenerations[0],
    settled: true,
  };
}

function safeCapability(value) {
  const source = object(value, 'INVALID_CAPABILITY');
  if (!['allowed', 'denied', 'unknown'].includes(source.status)) fail('INVALID_CAPABILITY');
  if (!Array.isArray(source.reasonCodes) || source.reasonCodes.some(code => typeof code !== 'string')) fail('INVALID_CAPABILITY');
  if (!(source.observedAt === null || typeof source.observedAt === 'string')) fail('INVALID_CAPABILITY');
  if (typeof source.observedAt === 'string') instant(source.observedAt, 'INVALID_CAPABILITY_OBSERVED_AT');
  return { status: source.status, reasonCodes: [...source.reasonCodes], observedAt: source.observedAt };
}

function safeCoverage(value) {
  const source = object(value, 'INVALID_COVERAGE');
  if (!['complete', 'partial', 'unknown'].includes(source.status)) fail('INVALID_COVERAGE');
  if (!Array.isArray(source.reasonCodes) || source.reasonCodes.some(code => typeof code !== 'string')) fail('INVALID_COVERAGE');
  if (!(source.observedAt === null || typeof source.observedAt === 'string')) fail('INVALID_COVERAGE');
  if (typeof source.observedAt === 'string') instant(source.observedAt, 'INVALID_COVERAGE_OBSERVED_AT');
  return { status: source.status, reasonCodes: [...source.reasonCodes], observedAt: source.observedAt };
}

function safeRepositoryPage(value) {
  const source = object(value, 'INVALID_REPOSITORY_RESPONSE');
  if (!Array.isArray(source.items)) fail('INVALID_REPOSITORY_RESPONSE');
  const items = source.items.map(item => {
    const repository = object(item, 'INVALID_REPOSITORY');
    return {
      id: string(repository.id, 'INVALID_REPOSITORY_ID'),
      userParticipation: safeCapability(repository.userParticipation),
      appCapability: safeCapability(repository.appCapability),
    };
  });
  const coverage = safeCoverage(source.coverage);
  return { items, coverage };
}

function safeLocation(raw) {
  const url = new URL(raw, allowedOrigin);
  return { origin: url.origin, path: url.pathname };
}

async function findMainPage(browser) {
  for (const context of browser.contexts()) {
    for (const candidate of context.pages()) {
      const session = await context.newCDPSession(candidate);
      const info = await session.send('Target.getTargetInfo');
      await session.detach();
      if (info.targetInfo.targetId === targetId) return candidate;
    }
  }
  fail('TARGET_NOT_FOUND');
}

async function readSession(page) {
  return page.evaluate(async origin => {
    if (location.origin !== origin) return { status: 0, errorCode: 'ORIGIN_MISMATCH' };
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), 15_000);
    try {
      const response = await fetch('/api/session', { credentials: 'same-origin', cache: 'no-store', signal: controller.signal });
      if (response.status !== 200) return { status: response.status };
      const value = await response.json().catch(() => undefined);
      return {
        status: 200,
        userId: typeof value?.user?.id === 'string' ? value.user.id : undefined,
        connectionStatus: typeof value?.githubConnection?.status === 'string' ? value.githubConnection.status : undefined,
      };
    } catch {
      return { status: 0, errorCode: 'SESSION_REQUEST_FAILED' };
    } finally {
      clearTimeout(timer);
    }
  }, allowedOrigin);
}

async function clickAndReadRepositories(page, click, attempt) {
  const responsePromise = page.waitForResponse(response => {
    const url = new URL(response.url());
    return response.request().method() === 'GET' && url.origin === allowedOrigin && url.pathname === '/api/repositories';
  }, { timeout: 15_000 });
  await click();
  const response = await responsePromise;
  let body;
  try { body = await response.json(); } catch { body = undefined; }
  const errorCode = typeof body?.error?.code === 'string' ? body.error.code : undefined;
  writeEvent('repository-response', { origin: allowedOrigin, path: '/api/repositories', status: response.status(), attempt, ...(errorCode ? { errorCode } : {}) });
  return { status: response.status(), errorCode, body };
}

async function searchFixture(page, credentialCommittedAt) {
  await page.locator('#repository-search').fill(fixtureQuery);
  let response = await clickAndReadRepositories(page, () => page.getByRole('button', { name: '搜索', exact: true }).click(), 1);
  for (let attempt = 2; response.status === 503 && response.errorCode === 'RESULT_UNCONFIRMED' && attempt <= 10; attempt += 1) {
    await page.waitForTimeout(3_000);
    const retry = page.getByRole('button', { name: '重试读取仓库', exact: true });
    await retry.waitFor({ state: 'visible', timeout: 15_000 });
    response = await clickAndReadRepositories(page, () => retry.click(), attempt);
  }
  if (response.status !== 200) fail('REPOSITORY_QUERY_FAILED');
  const safe = safeRepositoryPage(response.body);
  const fixtures = safe.items.filter(item => item.id === expectedFixtureId);
  if (fixtures.length !== 1) fail('FIXTURE_NOT_UNIQUE');
  const fixture = fixtures[0];
  if (fixture.userParticipation.status !== 'allowed' || fixture.appCapability.status !== 'allowed') fail('FIXTURE_CAPABILITY_NOT_ALLOWED');
  if (instant(fixture.userParticipation.observedAt, 'MISSING_USER_OBSERVATION') <= credentialCommittedAt) fail('USER_OBSERVATION_NOT_AFTER_REFRESH');
  if (instant(fixture.appCapability.observedAt, 'MISSING_APP_OBSERVATION') <= credentialCommittedAt) fail('APP_OBSERVATION_NOT_AFTER_REFRESH');
  const rawFixture = response.body.items.find(item => item?.id === expectedFixtureId);
  const displayName = string(rawFixture?.displayName, 'FIXTURE_DISPLAY_NAME_MISSING');
  return { safe, fixture, displayName };
}

async function layoutEvidence(page) {
  return page.evaluate(() => ({
    viewport: { width: innerWidth, height: innerHeight },
    documentScrollWidth: document.documentElement.scrollWidth,
    bodyScrollWidth: document.body.scrollWidth,
    horizontalOverflow: Math.max(document.documentElement.scrollWidth, document.body.scrollWidth) > innerWidth,
    buttons: Array.from(document.querySelectorAll('button')).map(button => ({
      text: button.textContent?.trim() ?? '',
      visible: Boolean(button.offsetWidth || button.offsetHeight || button.getClientRects().length),
      withinViewport: (() => {
        const box = button.getBoundingClientRect();
        return box.left >= 0 && box.right <= innerWidth;
      })(),
    })).filter(button => ['搜索', '重新发现', '退出登录'].includes(button.text)),
  }));
}

async function verifyAnonymous(page, fixtureDisplayName, stage) {
  const session = await readSession(page);
  if (session.status !== 401) fail(`${stage}_SESSION_NOT_401`);
  if ((await page.locator('body').innerText()).includes(fixtureDisplayName)) fail(`${stage}_FIXTURE_STILL_VISIBLE`);
  const route = safeLocation(page.url());
  if (route.origin !== allowedOrigin) fail(`${stage}_ORIGIN_MISMATCH`);
  writeEvent('anonymous-route', { stage, ...route, sessionStatus: 401, fixtureAbsent: true });
  return route;
}

try {
  eventFd = fs.openSync(eventPath, 'wx', 0o600);
  if (fs.existsSync(resultPath)) fail('RESULT_OUTPUT_ALREADY_EXISTS');
  const baseline = readSnapshot('baseline.json');
  const confirmed = readSnapshot('confirmed-refresh.json');
  const refresh = validateRefresh(baseline, confirmed);
  writeEvent('refresh-precondition-validated', refresh);

  const browser = await chromium.connectOverCDP('http://127.0.0.1:9230');
  const page = await findMainPage(browser);
  const initial = safeLocation(page.url());
  if (initial.origin !== allowedOrigin || initial.path !== '/') fail('MAIN_PAGE_ORIGIN_OR_PATH_MISMATCH');

  page.on('response', response => {
    try {
      const url = new URL(response.url());
      const tracked = url.origin === allowedOrigin && (
        url.pathname === '/api/session' ||
        url.pathname === '/api/auth/logout' ||
        url.pathname === '/api/auth/github/callback'
      );
      if (tracked) writeEvent('response', { origin: url.origin, path: url.pathname, status: response.status() });
    } catch { /* Ignore unparseable third-party response URLs. */ }
  });

  const initialSession = await readSession(page);
  if (initialSession.status !== 200 || initialSession.userId !== expectedActor || initialSession.connectionStatus !== 'connected') fail('INITIAL_SESSION_INVALID');
  writeEvent('initial-session', { origin: allowedOrigin, path: '/api/session', status: 200, userId: expectedActor, connectionStatus: 'connected' });

  const credentialCommittedAt = instant(confirmed.connection.credential_committed_at, 'INVALID_CREDENTIAL_COMMITTED_AT');
  const desktop = await searchFixture(page, credentialCommittedAt);
  writeEvent('desktop-fixture', { fixture: desktop.fixture, coverage: desktop.safe.coverage });

  const originalViewport = page.viewportSize() ?? await page.evaluate(() => ({ width: innerWidth, height: innerHeight }));
  await page.setViewportSize({ width: 390, height: 844 });
  await page.reload({ waitUntil: 'domcontentloaded', timeout: 15_000 });
  if (safeLocation(page.url()).origin !== allowedOrigin) fail('MOBILE_RELOAD_ORIGIN_MISMATCH');
  const mobile = await searchFixture(page, credentialCommittedAt);
  if (mobile.displayName !== desktop.displayName) fail('FIXTURE_DISPLAY_NAME_CHANGED');
  const mobileLayout = await layoutEvidence(page);
  if (mobileLayout.horizontalOverflow || mobileLayout.buttons.length !== 3 || mobileLayout.buttons.some(button => !button.visible || !button.withinViewport)) fail('MOBILE_LAYOUT_INVALID');
  writeEvent('mobile-fixture-and-layout', { fixture: mobile.fixture, coverage: mobile.safe.coverage, layout: mobileLayout });
  await page.setViewportSize(originalViewport);

  const logoutResponse = page.waitForResponse(response => {
    const url = new URL(response.url());
    return response.request().method() === 'POST' && url.origin === allowedOrigin && url.pathname === '/api/auth/logout';
  }, { timeout: 15_000 });
  await page.getByRole('button', { name: '退出登录', exact: true }).click();
  if ((await logoutResponse).status() !== 204) fail('LOGOUT_FAILED');
  await page.waitForFunction(name => !document.body.innerText.includes(name), desktop.displayName, { timeout: 15_000 });
  const routes = [await verifyAnonymous(page, desktop.displayName, 'after-logout')];

  for (const [stage, action] of [['history-back', () => page.goBack({ waitUntil: 'domcontentloaded', timeout: 15_000 })], ['history-forward', () => page.goForward({ waitUntil: 'domcontentloaded', timeout: 15_000 })]]) {
    const beforeURL = page.url();
    const navigation = await action();
    await page.waitForTimeout(250);
    if (navigation === null && page.url() === beforeURL) {
      writeEvent('history-unavailable', { stage });
      fail(`${stage.toUpperCase().replace('-', '_')}_UNAVAILABLE`);
    } else {
      routes.push(await verifyAnonymous(page, desktop.displayName, stage));
    }
  }

  result = {
    status: 'PASS',
    scope: 'POST_REFRESH_BROWSER_ONLY',
    fullBatchStatus: 'NOT_ASSERTED',
    passCriteria: {
      refreshSnapshotInvariants: true,
      refreshCommittedAfterDue: true,
      sameActiveSessionGeneration: true,
      existingSessionAndConnectedActor: true,
      desktopFixtureAllowedWithPostRefreshObservations: true,
      mobileFixtureAllowedWithPostRefreshObservationsAndNoHorizontalOverflow: true,
      logoutReturned204: true,
      sessionAfterLogout401: true,
      fixtureAbsentAfterLogoutAndHistory: true,
      historyBackAndForwardVerified: true,
    },
    refresh,
    fixture: desktop.fixture,
    coverage: desktop.safe.coverage,
    mobileLayout,
    routes,
  };
  exitCode = 0;
} catch (error) {
  const errorCode = typeof error?.code === 'string' ? error.code : 'SCRIPT_FAILED';
  if (eventFd !== undefined) writeEvent('failure', { errorCode });
  result = { status: 'FAIL', scope: 'POST_REFRESH_BROWSER_ONLY', errorCode };
} finally {
  if (eventFd !== undefined) fs.closeSync(eventFd);
  try {
    const resultFd = fs.openSync(resultPath, 'wx', 0o600);
    try { fs.writeSync(resultFd, `${JSON.stringify(result, null, 2)}\n`); } finally { fs.closeSync(resultFd); }
  } catch {
    exitCode = 1;
  }
}

process.exit(exitCode);
