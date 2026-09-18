import { chromium } from 'file://wsl.localhost/Ubuntu-22.04/tmp/repomesh-b02-browser/node_modules/playwright/index.mjs';

const destinations = new Map([
  ['open-app-settings', 'https://github.com/settings/apps/repomesh-bohan-b026'],
  ['open-app-advanced', 'https://github.com/settings/apps/repomesh-bohan-b026/advanced'],
  ['open-installations', 'https://github.com/settings/installations'],
  ['open-a-installation', 'https://github.com/settings/installations/161172403'],
  ['open-b-app-install', 'https://github.com/apps/repomesh-bohan-b026/installations/new'],
  ['open-new-repo', 'https://github.com/new'],
  ['open-repomesh-login', 'https://repomesh.bohanxu.me:8443/login'],
]);
const allowedLocations = new Set([
  'https://github.com/settings/apps/repomesh-bohan-b026',
  'https://github.com/settings/apps/repomesh-bohan-b026/advanced',
  'https://github.com/settings/installations',
  'https://github.com/settings/installations/161172403',
  'https://github.com/apps/repomesh-bohan-b026/installations/new',
  'https://github.com/new',
  'https://github.com/login',
  'https://repomesh.bohanxu.me:8443/',
  'https://repomesh.bohanxu.me:8443/login',
]);

function fail(code) {
  const error = new Error(code);
  error.code = code;
  throw error;
}

function safeLocation(raw) {
  const url = new URL(raw);
  const key = `${url.origin}${url.pathname}`;
  if (!allowedLocations.has(key)) return undefined;
  return { origin: url.origin, path: url.pathname };
}

function safeTitle(value) {
  if (typeof value !== 'string' || value.length > 200 || /[\r\n]/.test(value)) fail('UNSAFE_TITLE');
  return value;
}

function safeLogin(value) {
  return typeof value === 'string' && /^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$/.test(value)
    ? value
    : undefined;
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

async function safePage(context, page) {
  const id = await targetId(context, page);
  const location = safeLocation(page.url());
  if (!location) return { targetId: id, allowed: false };
  const documentLogin = await page.evaluate(() => ({
    dataLogin: document.documentElement.getAttribute('data-login'),
    metaUserLogin: document.querySelector('meta[name="user-login"]')?.getAttribute('content') ?? null,
  }));
  const dataLogin = safeLogin(documentLogin.dataLogin);
  const metaUserLogin = safeLogin(documentLogin.metaUserLogin);
  return {
    targetId: id,
    allowed: true,
    ...location,
    title: safeTitle(await page.title()),
    ...(dataLogin ? { dataLogin } : {}),
    ...(metaUserLogin ? { metaUserLogin } : {}),
  };
}

async function metaUserLogin(page) {
  return safeLogin(await page.locator('meta[name="user-login"]').getAttribute('content'));
}

async function exactPage(browser, expectedPath) {
  const matches = [];
  for (const context of browser.contexts()) {
    for (const page of context.pages()) {
      const location = safeLocation(page.url());
      if (location?.origin === 'https://github.com' && location.path === expectedPath) matches.push({ context, page });
    }
  }
  if (matches.length !== 1) fail('EXACT_PAGE_NOT_FOUND');
  const login = await metaUserLogin(matches[0].page);
  if (login !== 'catbobyman') fail('ACCOUNT_A_REQUIRED');
  return { ...matches[0], login };
}

async function visibleExactTextCount(page, label) {
  const matches = page.getByText(label, { exact: true });
  const count = await matches.count();
  let visible = 0;
  for (let index = 0; index < Math.min(count, 20); index += 1) {
    if (await matches.nth(index).isVisible()) visible += 1;
  }
  return Math.min(visible, 10);
}

async function inspectAdvanced(browser) {
  const expectedPath = '/settings/apps/repomesh-bohan-b026/advanced';
  const { context, page, login } = await exactPage(browser, expectedPath);
  const labels = ['Make public', 'Make private', 'Make GitHub App public', 'Make GitHub App private', 'Transfer ownership', 'Delete GitHub App'];
  const buttonCounts = {};
  for (const label of labels) {
    const buttons = page.getByRole('button', { name: label, exact: true });
    const count = await buttons.count();
    let visible = 0;
    for (let index = 0; index < Math.min(count, 20); index += 1) {
      if (await buttons.nth(index).isVisible()) visible += 1;
    }
    buttonCounts[label] = Math.min(visible, 10);
  }
  const publicTextCount = (await visibleExactTextCount(page, 'Make public')) + (await visibleExactTextCount(page, 'Make GitHub App public'));
  const privateTextCount = (await visibleExactTextCount(page, 'Make private')) + (await visibleExactTextCount(page, 'Make GitHub App private'));
  return {
    status: 'OK',
    command: 'inspect-app-advanced',
    targetId: await targetId(context, page),
    origin: 'https://github.com',
    path: expectedPath,
    metaUserLogin: login,
    makePublicVisible: publicTextCount > 0,
    makePrivateVisible: privateTextCount > 0,
    buttonCounts,
  };
}

async function accessibleNamePresent(page, name) {
  for (const role of ['radio', 'row', 'listitem', 'group']) {
    const matches = page.getByRole(role, { name, exact: true });
    const count = await matches.count();
    for (let index = 0; index < Math.min(count, 20); index += 1) {
      if (await matches.nth(index).isVisible()) return true;
    }
  }
  return false;
}

async function repositoryMode(page) {
  const checked = [];
  for (const [mode, labels] of [
    ['ALL', ['All repositories']],
    ['SELECTED', ['Only select repositories', 'Selected repositories']],
  ]) {
    for (const label of labels) {
      const radios = page.getByRole('radio', { name: label, exact: true });
      const count = await radios.count();
      for (let index = 0; index < Math.min(count, 20); index += 1) {
        if (await radios.nth(index).isVisible() && await radios.nth(index).isChecked()) checked.push(mode);
      }
    }
  }
  return new Set(checked).size === 1 ? checked[0] : 'UNKNOWN';
}

async function permissionLevel(page, permission) {
  const levels = ['Read-only', 'Read and write', 'No access'];
  const found = [];
  for (const level of levels) {
    for (const separator of [' ', ': ']) {
      if (await accessibleNamePresent(page, `${permission}${separator}${level}`)) {
        found.push(level);
        break;
      }
    }
  }
  return found.length === 1 ? found[0].toUpperCase().replaceAll(' ', '_').replace('-', '_') : 'UNKNOWN';
}

async function inspectAInstallation(browser) {
  const expectedPath = '/settings/installations/161172403';
  const { context, page, login } = await exactPage(browser, expectedPath);
  return {
    status: 'OK',
    command: 'inspect-a-installation',
    targetId: await targetId(context, page),
    origin: 'https://github.com',
    path: expectedPath,
    metaUserLogin: login,
    repositoryMode: await repositoryMode(page),
    permissions: {
      Metadata: await permissionLevel(page, 'Metadata'),
      Contents: await permissionLevel(page, 'Contents'),
      'Pull requests': await permissionLevel(page, 'Pull requests'),
    },
  };
}

const mutationButtonLabels = [
  'Make public',
  'Make private',
  'Cancel',
  'I understand, make this GitHub App public',
  'I understand, make this GitHub App private',
];

async function visibleButtons(page, label) {
  const locator = page.getByRole('button', { name: label, exact: true });
  const matches = [];
  for (let index = 0; index < Math.min(await locator.count(), 20); index += 1) {
    if (await locator.nth(index).isVisible()) matches.push(locator.nth(index));
  }
  return matches;
}

async function allowedMutationButtonCounts(page) {
  const counts = {};
  for (const label of mutationButtonLabels) counts[label] = Math.min((await visibleButtons(page, label)).length, 10);
  return counts;
}

async function needsHumanResult(context, page, command) {
  return {
    status: 'NEEDS_HUMAN_CONFIRMATION',
    command,
    targetId: await targetId(context, page),
    origin: 'https://github.com',
    path: '/settings/apps/repomesh-bohan-b026/advanced',
    allowedButtonCounts: await allowedMutationButtonCounts(page),
  };
}

async function confirmationContainers(page) {
  const containers = [];
  for (const locator of [page.getByRole('dialog'), page.locator('details[open]')]) {
    for (let index = 0; index < Math.min(await locator.count(), 10); index += 1) {
      if (await locator.nth(index).isVisible()) containers.push(locator.nth(index));
    }
  }
  return containers;
}

async function confirmationRequiresInput(containers) {
  for (const container of containers) {
    const inputs = container.locator('input, textarea');
    for (let index = 0; index < Math.min(await inputs.count(), 20); index += 1) {
      if (await inputs.nth(index).isVisible()) return true;
    }
  }
  return false;
}

async function setAppVisibility(browser, command, makePublic) {
  const expectedPath = '/settings/apps/repomesh-bohan-b026/advanced';
  const { context, page } = await exactPage(browser, expectedPath);
  const initialLabel = makePublic ? 'Make public' : 'Make private';
  const oppositeLabel = makePublic ? 'Make private' : 'Make public';
  const initial = await visibleButtons(page, initialLabel);
  const opposite = await visibleButtons(page, oppositeLabel);
  if (initial.length !== 1 || opposite.length !== 0) fail('VISIBILITY_INITIAL_GUARD_FAILED');

  const initialHandle = await initial[0].elementHandle();
  if (!initialHandle) fail('VISIBILITY_INITIAL_GUARD_FAILED');
  let confirmationClicked = false;
  try {
    await initial[0].click({ timeout: 15_000 });
    const deadline = Date.now() + 15_000;
    while (Date.now() < deadline) {
      const currentLocation = new URL(page.url());
      if (currentLocation.origin !== 'https://github.com' || currentLocation.pathname !== expectedPath) {
        return needsHumanResult(context, page, command);
      }
      const current = await exactPage(browser, expectedPath);
      const desiredPostcondition = await visibleButtons(current.page, oppositeLabel);
      const staleControl = await visibleButtons(current.page, initialLabel);
      if (desiredPostcondition.length === 1 && staleControl.length === 0) {
        return {
          status: makePublic ? 'PUBLIC_CONFIRMED' : 'PRIVATE_CONFIRMED',
          command,
          targetId: await targetId(current.context, current.page),
          origin: 'https://github.com',
          path: expectedPath,
          allowedButtonCounts: await allowedMutationButtonCounts(current.page),
        };
      }

      const containers = await confirmationContainers(current.page);
      if (containers.length > 0) {
        if (await confirmationRequiresInput(containers)) return needsHumanResult(current.context, current.page, command);
        const finalControls = [];
        for (const container of containers) {
          const candidates = container.getByRole('button', { name: initialLabel, exact: true });
          for (let index = 0; index < Math.min(await candidates.count(), 10); index += 1) {
            const candidate = candidates.nth(index);
            if (!await candidate.isVisible()) continue;
            const distinct = await candidate.evaluate((node, original) => node !== original, initialHandle);
            if (distinct) finalControls.push(candidate);
          }
        }
        if (!confirmationClicked && finalControls.length === 1) {
          if (!await finalControls[0].isEnabled()) return needsHumanResult(current.context, current.page, command);
          await finalControls[0].click({ timeout: 15_000 });
          confirmationClicked = true;
        } else if (finalControls.length !== 1) {
          return needsHumanResult(current.context, current.page, command);
        }
      }
      await page.waitForTimeout(250);
    }
    return needsHumanResult(context, page, command);
  } finally {
    await initialHandle.dispose();
  }
}

function positiveInstallationId(value) {
  if (typeof value !== 'string' || !/^[1-9][0-9]*$/.test(value)) return undefined;
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) ? parsed : undefined;
}

async function inspectBInstallCompletion(browser) {
  const candidates = [];
  for (const context of browser.contexts()) {
    for (const page of context.pages()) {
      let url;
      try { url = new URL(page.url()); } catch { continue; }
      if (url.origin !== 'https://github.com') continue;
      let installationId;
      let pathClass;
      const settings = /^\/settings\/installations\/([1-9][0-9]*)(\/.*)?$/.exec(url.pathname);
      if (settings) {
        installationId = positiveInstallationId(settings[1]);
        pathClass = settings[2] ? 'SETTINGS_INSTALLATION_SUBPATH' : 'SETTINGS_INSTALLATION';
      } else if (url.pathname === '/apps/repomesh-bohan-b026/installations/new') {
        installationId = positiveInstallationId(url.searchParams.get('installation_id'));
        if (installationId !== undefined) pathClass = 'APP_INSTALL_CALLBACK';
      }
      if (installationId === undefined || pathClass === undefined) continue;
      if (await metaUserLogin(page) !== 'bohanxu111') fail('ACCOUNT_B_REQUIRED');
      candidates.push({ installationId, pathClass });
    }
  }
  if (candidates.length === 0) fail('INSTALL_COMPLETION_NOT_FOUND');
  const ids = new Set(candidates.map(candidate => candidate.installationId));
  const classes = new Set(candidates.map(candidate => candidate.pathClass));
  if (ids.size !== 1 || classes.size !== 1) fail('INSTALL_COMPLETION_AMBIGUOUS');
  return {
    status: 'OK',
    command: 'inspect-b-install-completion',
    metaUserLogin: 'bohanxu111',
    installationId: candidates[0].installationId,
    pathClass: candidates[0].pathClass,
  };
}

function parseArguments() {
  const portText = process.argv[2];
  const command = process.argv[3];
  if (!/^[0-9]{1,5}$/.test(portText ?? '')) fail('INVALID_CDP_PORT');
  const port = Number(portText);
  if (port < 1 || port > 65535) fail('INVALID_CDP_PORT');
  if (!['inspect', 'inspect-app-advanced', 'inspect-a-installation', 'inspect-b-install-completion', 'make-app-public', 'make-app-private'].includes(command) && !destinations.has(command)) fail('INVALID_COMMAND');
  if (process.argv.length !== 4) fail('INVALID_ARGUMENTS');
  return { port, command };
}

async function main() {
  const { port, command } = parseArguments();
  if (['open-b-app-install', 'inspect-b-install-completion'].includes(command) && port !== 9230) fail('ACCOUNT_B_CDP_PORT_REQUIRED');
  const browser = await chromium.connectOverCDP(`http://127.0.0.1:${port}`);
  if (command === 'inspect-b-install-completion') return inspectBInstallCompletion(browser);
  if (command === 'inspect-app-advanced') return inspectAdvanced(browser);
  if (command === 'inspect-a-installation') return inspectAInstallation(browser);
  if (command === 'make-app-public') return setAppVisibility(browser, command, true);
  if (command === 'make-app-private') return setAppVisibility(browser, command, false);
  if (command === 'inspect') {
    const pages = [];
    for (const context of browser.contexts()) {
      for (const page of context.pages()) pages.push(await safePage(context, page));
    }
    return { status: 'OK', command, pages };
  }

  const context = browser.contexts()[0];
  if (!context) fail('BROWSER_CONTEXT_NOT_FOUND');
  const page = await context.newPage();
  await page.goto(destinations.get(command), { waitUntil: 'domcontentloaded', timeout: 30_000 });
  if (command === 'open-b-app-install') {
    const url = new URL(page.url());
    if (url.origin !== 'https://github.com' || url.pathname !== '/apps/repomesh-bohan-b026/installations/new') fail('FINAL_LOCATION_NOT_ALLOWED');
    const login = await metaUserLogin(page);
    if (login !== 'bohanxu111') fail('ACCOUNT_B_REQUIRED');
    return { status: 'OK', command, pathClass: 'B_APP_INSTALL_START', metaUserLogin: login };
  }
  const pageResult = await safePage(context, page);
  if (!pageResult.allowed) fail('FINAL_LOCATION_NOT_ALLOWED');
  return { status: 'OK', command, page: pageResult };
}

try {
  const result = await main();
  process.stdout.write(`${JSON.stringify(result)}\n`);
  process.exit(0);
} catch (error) {
  const errorCode = typeof error?.code === 'string' ? error.code : 'CONTROL_FAILED';
  process.stdout.write(`${JSON.stringify({ status: 'FAIL', errorCode })}\n`);
  process.exit(1);
}
