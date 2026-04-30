// Test fixtures: spawn nonchalant + ffmpeg publisher per worker, expose URLs to tests.

import { test as base, expect } from '@playwright/test';
import { ChildProcess, spawn } from 'node:child_process';
import { mkdtempSync, writeFileSync, existsSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import net from 'node:net';
import http from 'node:http';

const REPO_ROOT = resolve(__dirname, '..', '..');
const BIN_PATH = join(REPO_ROOT, 'bin', 'nonchalant');
const TEST_VIDEO = join(REPO_ROOT, 'assets', 'nonchalant-test.mp4');
const PAGES_DIR = resolve(__dirname, '..', 'pages');

async function freePort(): Promise<number> {
  return new Promise((resolveP, rejectP) => {
    const srv = net.createServer();
    srv.unref();
    srv.on('error', rejectP);
    srv.listen(0, () => {
      const addr = srv.address();
      if (typeof addr === 'object' && addr) {
        const port = addr.port;
        srv.close(() => resolveP(port));
      } else {
        rejectP(new Error('failed to allocate port'));
      }
    });
  });
}

async function waitForHealth(port: number, timeoutMs = 10_000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  const url = `http://127.0.0.1:${port}/healthz`;
  while (Date.now() < deadline) {
    const ok = await new Promise<boolean>((res) => {
      const req = http.get(url, (resp) => {
        resp.resume();
        res(resp.statusCode === 200);
      });
      req.on('error', () => res(false));
      req.setTimeout(500, () => { req.destroy(); res(false); });
    });
    if (ok) return;
    await new Promise((r) => setTimeout(r, 200));
  }
  throw new Error(`health endpoint not ready on :${port}`);
}

async function waitForStream(port: number, app: string, name: string, timeoutMs = 30_000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const found = await new Promise<boolean>((res) => {
      const req = http.get(`http://127.0.0.1:${port}/api/streams`, (resp) => {
        let body = '';
        resp.on('data', (c) => (body += c));
        resp.on('end', () => {
          try {
            const parsed = JSON.parse(body);
            const streams = parsed.streams ?? [];
            const hit = streams.some(
              (s: { app: string; name: string; has_publisher: boolean }) =>
                s.app === app && s.name === name && s.has_publisher,
            );
            res(hit);
          } catch {
            res(false);
          }
        });
      });
      req.on('error', () => res(false));
      req.setTimeout(500, () => { req.destroy(); res(false); });
    });
    if (found) return;
    await new Promise((r) => setTimeout(r, 250));
  }
  throw new Error(`stream ${app}/${name} did not become live within ${timeoutMs}ms`);
}

function startStaticServer(port: number): http.Server {
  const allowed = new Set(['wsflv-player.html', 'hls-player.html']);
  const srv = http.createServer((req, res) => {
    const name = (req.url ?? '/').replace(/^\/+/, '').split('?')[0];
    if (!allowed.has(name)) {
      res.statusCode = 404;
      res.end('not found');
      return;
    }
    try {
      const body = readFileSync(join(PAGES_DIR, name));
      res.setHeader('Content-Type', 'text/html; charset=utf-8');
      res.end(body);
    } catch (e) {
      res.statusCode = 500;
      res.end(String(e));
    }
  });
  srv.listen(port, '127.0.0.1');
  return srv;
}

export type ServerHandle = {
  httpPort: number;
  rtmpPort: number;
  wsUrl: (app: string, name: string) => string;
  flvUrl: (app: string, name: string) => string;
  apiUrl: (path: string) => string;
  pageUrl: (file: string) => string;
};

export type PublisherHandle = {
  app: string;
  name: string;
  proc: ChildProcess;
};

type Fixtures = {
  // Extra YAML appended below the server: section when starting nonchalant.
  // Override per-test with test.use({ serverExtraConfig: '...' }).
  serverExtraConfig: string;
  server: ServerHandle;
  publisher: PublisherHandle;
};

export const test = base.extend<Fixtures>({
  serverExtraConfig: ['', { option: true }],
  server: async ({ serverExtraConfig }, use) => {
    if (!existsSync(BIN_PATH)) {
      throw new Error(
        `nonchalant binary not found at ${BIN_PATH}. Run "make build" before "npm test".`,
      );
    }

    const httpPort = await freePort();
    const rtmpPort = await freePort();
    const healthPort = await freePort();
    const pagePort = await freePort();

    const dir = mkdtempSync(join(tmpdir(), 'nonchalant-e2e-'));
    const cfgPath = join(dir, 'config.yaml');
    const baseCfg = `server:\n  health_port: ${healthPort}\n  http_port: ${httpPort}\n  rtmp_port: ${rtmpPort}\n`;
    writeFileSync(cfgPath, baseCfg + (serverExtraConfig ?? ''));

    const proc = spawn(BIN_PATH, ['--config', cfgPath], {
      stdio: ['ignore', 'pipe', 'pipe'],
    });
    proc.stdout?.on('data', (c) => process.stdout.write(`[server] ${c}`));
    proc.stderr?.on('data', (c) => process.stderr.write(`[server] ${c}`));

    try {
      await waitForHealth(httpPort);
    } catch (e) {
      proc.kill('SIGKILL');
      throw e;
    }

    const pageSrv = startStaticServer(pagePort);

    const handle: ServerHandle = {
      httpPort,
      rtmpPort,
      wsUrl: (app, name) => `ws://127.0.0.1:${httpPort}/ws/${app}/${name}`,
      flvUrl: (app, name) => `http://127.0.0.1:${httpPort}/${app}/${name}.flv`,
      apiUrl: (path) => `http://127.0.0.1:${httpPort}${path}`,
      pageUrl: (file) => `http://127.0.0.1:${pagePort}/${file}`,
    };

    await use(handle);

    pageSrv.close();
    proc.kill('SIGINT');
    await new Promise<void>((res) => {
      const t = setTimeout(() => { proc.kill('SIGKILL'); res(); }, 3_000);
      proc.once('exit', () => { clearTimeout(t); res(); });
    });
  },

  publisher: async ({ server }, use) => {
    if (!existsSync(TEST_VIDEO)) {
      throw new Error(`test video missing at ${TEST_VIDEO}`);
    }
    const app = 'live';
    const name = `e2e_${Date.now().toString(36)}`;
    const rtmpURL = `rtmp://127.0.0.1:${server.rtmpPort}/${app}/${name}`;

    // -re streams at native rate, -stream_loop -1 keeps it alive for the whole test.
    const proc = spawn('ffmpeg', [
      '-hide_banner', '-loglevel', 'warning',
      '-re', '-stream_loop', '-1', '-i', TEST_VIDEO,
      '-c', 'copy', '-f', 'flv', rtmpURL,
    ], { stdio: ['ignore', 'pipe', 'pipe'] });
    proc.stderr?.on('data', (c) => process.stderr.write(`[publisher] ${c}`));

    try {
      await waitForStream(server.httpPort, app, name);
    } catch (e) {
      proc.kill('SIGKILL');
      throw e;
    }

    await use({ app, name, proc });

    proc.kill('SIGTERM');
    await new Promise<void>((res) => {
      const t = setTimeout(() => { proc.kill('SIGKILL'); res(); }, 2_000);
      proc.once('exit', () => { clearTimeout(t); res(); });
    });
  },
});

export { expect };
