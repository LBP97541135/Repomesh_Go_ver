import fs from 'node:fs';
import { chromium } from 'file://wsl.localhost/Ubuntu-22.04/tmp/repomesh-b02-browser/node_modules/playwright/index.mjs';

const allowedOrigin = 'https://repomesh.bohanxu.me:8443';
const targetId = 'CBEA86CD7C41D085994D442CEB5B60A4';
const expectedUserId = '16198e14-7249-4cc2-a15e-57cd8b568874';
const deadline = Date.parse('2026-09-13T03:48:40Z');
const intervalMs = 10 * 60 * 1000;
const outputPath = '\\\\wsl.localhost\\Ubuntu-22.04\\tmp\\repomesh-b02-live\\session-keepalive-01.jsonl';
const fd = fs.openSync(outputPath, 'wx', 0o600);

function write(entry) {
  fs.writeSync(fd, `${JSON.stringify({ ts: new Date().toISOString(), ...entry })}\n`);
}

function delay(ms) {
  return new Promise(resolve => setTimeout(resolve, ms));
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

async function readSession(page) {
  return page.evaluate(async expectedOrigin => {
    if (location.origin !== expectedOrigin) {
      return { status: 0, errorCode: 'ORIGIN_SKIPPED' };
    }

    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), 10_000);
    try {
      const response = await fetch('/api/session', {
        method: 'GET',
        credentials: 'same-origin',
        signal: controller.signal,
      });
      if (response.status !== 200) return { status: response.status };

      let value;
      try {
        value = await response.json();
      } catch {
        return { status: 200, errorCode: 'INVALID_RESPONSE' };
      }
      return {
        status: 200,
        user: typeof value?.user?.id === 'string' ? { id: value.user.id } : undefined,
        githubConnection: value?.githubConnection && typeof value.githubConnection === 'object'
          ? {
              status: typeof value.githubConnection.status === 'string'
                ? value.githubConnection.status
                : undefined,
              observedAt: typeof value.githubConnection.observedAt === 'string' || value.githubConnection.observedAt === null
                ? value.githubConnection.observedAt
                : undefined,
            }
          : undefined,
      };
    } catch {
      return { status: 0, errorCode: 'NETWORK_FAILURE' };
    } finally {
      clearTimeout(timer);
    }
  }, allowedOrigin);
}

let exitCode = 0;

try {
  const browser = await chromium.connectOverCDP('http://127.0.0.1:9230');
  const page = await findPage(browser);
  if (!page) {
    write({ status: 0, errorCode: 'TARGET_NOT_FOUND' });
    exitCode = 1;
  } else {
    while (Date.now() < deadline) {
      let result;
      try {
        result = await readSession(page);
      } catch {
        write({ status: 0, errorCode: 'BROWSER_DISCONNECTED' });
        exitCode = 1;
        break;
      }

      write(result);
      if (result.status === 401) {
        exitCode = 1;
        break;
      }
      if (result.status === 200 && result.user?.id !== expectedUserId) {
        write({ status: 0, errorCode: 'USER_MISMATCH' });
        exitCode = 1;
        break;
      }

      const remaining = deadline - Date.now();
      if (remaining <= 0) break;
      await delay(Math.min(intervalMs, remaining));
    }
    if (exitCode === 0 && Date.now() >= deadline) write({ status: 'DEADLINE_REACHED' });
  }
} catch {
  write({ status: 0, errorCode: 'BROWSER_DISCONNECTED' });
  exitCode = 1;
} finally {
  fs.closeSync(fd);
}

process.exit(exitCode);
