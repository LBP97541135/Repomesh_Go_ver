import fs from 'node:fs';
import { chromium } from 'file://wsl.localhost/Ubuntu-22.04/tmp/repomesh-b02-browser/node_modules/playwright/index.mjs';

const allowedOrigin = 'https://repomesh.bohanxu.me:8443';
const targetId = process.argv[2];
if (!/^[A-F0-9]{32}$/.test(targetId ?? '')) {
  process.stderr.write('INVALID_TARGET_ID\n');
  process.exit(2);
}

const outputPath = '\\\\wsl.localhost\\Ubuntu-22.04\\tmp\\repomesh-b02-live\\windows-monitor-03.jsonl';
const fd = fs.openSync(outputPath, 'wx', 0o600);

function write(entry) {
  fs.writeSync(fd, `${JSON.stringify(entry)}\n`);
}

function base(event, url, status) {
  return {
    ts: new Date().toISOString(),
    event,
    origin: url.origin,
    pathname: url.pathname,
    status,
  };
}

function safeLocation(raw, baseURL) {
  if (!raw) return undefined;
  try {
    const value = new URL(raw, baseURL);
    return { origin: value.origin, pathname: value.pathname };
  } catch {
    return undefined;
  }
}

function cookieMetadata(raw) {
  const pieces = raw.split(';').map(value => value.trim());
  const separator = pieces[0]?.indexOf('=') ?? -1;
  if (separator < 1) return undefined;
  const attributes = pieces.slice(1);
  const sameSite = attributes.find(value => /^samesite=/i.test(value))?.split('=', 2)[1];
  const path = attributes.find(value => /^path=/i.test(value))?.slice(5);
  return {
    name: pieces[0].slice(0, separator),
    secure: attributes.some(value => /^secure$/i.test(value)),
    httpOnly: attributes.some(value => /^httponly$/i.test(value)),
    ...(sameSite ? { sameSite } : {}),
    ...(path ? { path } : {}),
  };
}

function sessionFields(value) {
  if (!value || typeof value !== 'object') return undefined;
  const result = {};
  if (value.user && typeof value.user === 'object' && typeof value.user.id === 'string') {
    result.user = { id: value.user.id };
  }
  const source = value.githubConnection ?? value.connection;
  if (source && typeof source === 'object') {
    const connection = {};
    if (typeof source.status === 'string') connection.status = source.status;
    if (typeof source.revision === 'string') connection.revision = source.revision;
    if (typeof source.committedRevision === 'string') connection.committedRevision = source.committedRevision;
    if (Object.keys(connection).length) result.connection = connection;
  }
  return Object.keys(result).length ? result : undefined;
}

function attemptFields(value) {
  if (!value || typeof value !== 'object') return undefined;
  const result = {};
  for (const key of ['attemptId', 'state', 'reasonCode', 'purpose', 'expiresAt', 'observedAt']) {
    if (typeof value[key] === 'string' || (['reasonCode', 'observedAt'].includes(key) && value[key] === null)) {
      result[key] = value[key];
    }
  }
  return Object.keys(result).length ? result : undefined;
}

async function record(response) {
  let url;
  try {
    url = new URL(response.url());
  } catch {
    return;
  }
  if (url.origin !== allowedOrigin) return;

  const entry = base('response', url, response.status());
  if (url.pathname === '/api/auth/github/callback') {
    const headers = await response.headersArray().catch(() => []);
    const location = headers.find(header => header.name.toLowerCase() === 'location')?.value;
    const cookies = headers
      .filter(header => header.name.toLowerCase() === 'set-cookie')
      .map(header => cookieMetadata(header.value))
      .filter(Boolean);
    const safe = safeLocation(location, allowedOrigin);
    if (safe) entry.location = safe;
    if (cookies.length) entry.setCookie = cookies;
  } else if (url.pathname === '/api/session' && response.request().method() === 'GET') {
    const value = await response.json().catch(() => undefined);
    const session = sessionFields(value);
    if (session) entry.session = session;
  } else if (url.pathname.startsWith('/api/auth/')) {
    const value = await response.json().catch(() => undefined);
    const attempt = attemptFields(value);
    if (attempt) entry.attempt = attempt;
  }
  write(entry);
}

try {
  const browser = await chromium.connectOverCDP('http://127.0.0.1:9230');
  let page;
  for (const context of browser.contexts()) {
    for (const candidate of context.pages()) {
      const session = await context.newCDPSession(candidate);
      const info = await session.send('Target.getTargetInfo');
      await session.detach();
      if (info.targetInfo.targetId === targetId) page = candidate;
    }
  }
  if (!page) throw new Error('TARGET_NOT_FOUND');

  page.on('response', response => {
    void record(response).catch(() => write(base('monitor_error', new URL(allowedOrigin), 0)));
  });
  page.on('framenavigated', frame => {
    if (frame !== page.mainFrame()) return;
    const location = safeLocation(frame.url(), allowedOrigin);
    if (location) write({ ts: new Date().toISOString(), event: 'framenavigated', ...location });
  });
  write(base('monitor_ready', new URL(allowedOrigin), 0));
  await new Promise(() => {});
} catch {
  write(base('monitor_start_error', new URL(allowedOrigin), 0));
  fs.closeSync(fd);
  process.exit(1);
}
