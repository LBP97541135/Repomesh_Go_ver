import fs from 'node:fs';
import { chromium } from 'file://wsl.localhost/Ubuntu-22.04/tmp/repomesh-b02-browser/node_modules/playwright/index.mjs';

const allowedOrigin = 'https://repomesh.bohanxu.me:8443';
const targetId = 'CBEA86CD7C41D085994D442CEB5B60A4';
const fixtureId = 'repo_00000000001367444901';
const outputPath = '\\\\wsl.localhost\\Ubuntu-22.04\\tmp\\repomesh-b02-live\\pagination-01.jsonl';
const fd = fs.openSync(outputPath, 'wx', 0o600);

function write(entry) {
  fs.writeSync(fd, `${JSON.stringify({ ts: new Date().toISOString(), ...entry })}\n`);
}

function capability(value) {
  if (!value || typeof value !== 'object') return undefined;
  const result = {};
  if (typeof value.status === 'string') result.status = value.status;
  if (Array.isArray(value.reasonCodes)) {
    result.reasonCodes = value.reasonCodes.filter(reason => typeof reason === 'string');
  }
  if (typeof value.observedAt === 'string' || value.observedAt === null) {
    result.observedAt = value.observedAt;
  }
  return result;
}

function safePage(value, pageIndex) {
  if (
    !value ||
    typeof value !== 'object' ||
    !Array.isArray(value.items) ||
    !(value.nextCursor === null || typeof value.nextCursor === 'string')
  ) return undefined;
  return {
    status: 200,
    pageIndex,
    items: value.items.map(item => ({
      id: typeof item?.id === 'string' ? item.id : undefined,
      userParticipation: capability(item?.userParticipation),
      appCapability: capability(item?.appCapability),
    })),
    coverage: capability(value.coverage),
    hasNext: typeof value.nextCursor === 'string',
  };
}

async function findPage(browser) {
  for (const context of browser.contexts()) {
    for (const candidate of context.pages()) {
      const session = await context.newCDPSession(candidate);
      const info = await session.send('Target.getTargetInfo');
      await session.detach();
      if (info.targetInfo.targetId === targetId) return candidate;
    }
  }
  return undefined;
}

async function fetchPage(page, cursor) {
  return page.evaluate(async ({ cursorValue, allowedOriginValue }) => {
    if (location.origin !== allowedOriginValue) {
      return { status: 0, errorCode: 'ORIGIN_MISMATCH' };
    }
    const params = new URLSearchParams({ limit: '5' });
    if (cursorValue !== null) params.set('cursor', cursorValue);
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), 15_000);
    try {
      const response = await fetch(`/api/repositories?${params}`, {
        method: 'GET',
        credentials: 'same-origin',
        signal: controller.signal,
      });
      let body;
      try {
        body = await response.json();
      } catch {
        body = undefined;
      }
      return {
        status: response.status,
        errorCode: typeof body?.error?.code === 'string'
          ? body.error.code
          : typeof body?.code === 'string' ? body.code : undefined,
        body: response.status === 200 ? body : undefined,
      };
    } catch {
      return { status: 0, errorCode: 'FETCH_FAILED' };
    } finally {
      clearTimeout(timer);
    }
  }, { cursorValue: cursor, allowedOriginValue: allowedOrigin });
}

const uniqueIds = new Set();
const duplicateIds = new Set();
let pages = 0;
let terminalPartial = false;

try {
  const browser = await chromium.connectOverCDP('http://127.0.0.1:9230');
  const page = await findPage(browser);
  if (!page) {
    write({ status: 0, errorCode: 'TARGET_NOT_FOUND', pageIndex: 0 });
    process.exitCode = 1;
  } else if (new URL(page.url()).origin !== allowedOrigin) {
    write({ status: 0, errorCode: 'ORIGIN_MISMATCH', pageIndex: 0 });
    process.exitCode = 1;
  } else {
    let cursor = null;
    for (let pageIndex = 1; pageIndex <= 10; pageIndex += 1) {
      let result;
      for (let retry = 0; retry <= 10; retry += 1) {
        result = await fetchPage(page, cursor);
        if (result.status === 200) break;
        write({ status: result.status, errorCode: result.errorCode, pageIndex });
        if (result.status !== 503 || result.errorCode !== 'RESULT_UNCONFIRMED' || retry === 10) break;
        await new Promise(resolve => setTimeout(resolve, 3_000));
      }

      if (result?.status !== 200) {
        process.exitCode = 1;
        break;
      }

      const safe = safePage(result.body, pageIndex);
      if (!safe) {
        write({ status: 200, errorCode: 'INVALID_RESPONSE', pageIndex });
        process.exitCode = 1;
        break;
      }
      write(safe);
      pages += 1;

      for (const item of safe.items) {
        if (typeof item.id !== 'string') continue;
        if (uniqueIds.has(item.id)) duplicateIds.add(item.id);
        uniqueIds.add(item.id);
      }

      cursor = result.body.nextCursor;
      if (cursor === null) {
        terminalPartial = safe.coverage?.status === 'partial';
        break;
      }
      if (pageIndex === 10) process.exitCode = 1;
    }
  }
} catch {
  write({ status: 0, errorCode: 'SCRIPT_FAILED', pageIndex: pages + 1 });
  process.exitCode = 1;
} finally {
  write({
    status: 'summary',
    pages,
    uniqueCount: uniqueIds.size,
    duplicateIdsCount: duplicateIds.size,
    fixtureSeen: uniqueIds.has(fixtureId),
    terminalPartial,
  });
  fs.closeSync(fd);
}

process.exit(process.exitCode ?? 0);
