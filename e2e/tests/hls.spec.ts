// Verifies the native /hls/{app}/{name}.m3u8 endpoint returns a valid playlist
// and its first segment is a real MPEG-TS chunk.

import http from 'node:http';
import { test, expect } from '../fixtures/server';

// followGet does an HTTP GET and follows up to 5 redirects (Node's http.get
// doesn't auto-follow). Returns status + raw body buffer on the final hop.
function followGet(url: string, hops = 5): Promise<{ status: number; bytes: Buffer }> {
  return new Promise((resolveP, rejectP) => {
    http.get(url, (resp) => {
      const code = resp.statusCode ?? 0;
      if (hops > 0 && (code === 301 || code === 302 || code === 307 || code === 308)) {
        const loc = resp.headers.location;
        resp.resume();
        if (typeof loc === 'string') {
          const next = loc.startsWith('http') ? loc : new URL(loc, url).toString();
          followGet(next, hops - 1).then(resolveP, rejectP);
          return;
        }
      }
      const chunks: Buffer[] = [];
      resp.on('data', (c: Buffer) => chunks.push(c));
      resp.on('end', () => resolveP({ status: code, bytes: Buffer.concat(chunks) }));
      resp.on('error', rejectP);
    }).on('error', rejectP);
  });
}

async function fetchText(url: string): Promise<{ status: number; body: string }> {
  const r = await followGet(url);
  return { status: r.status, body: r.bytes.toString('utf8') };
}

async function fetchBytes(url: string): Promise<{ status: number; bytes: Buffer }> {
  return followGet(url);
}

async function pollForManifest(url: string, deadlineMs = 25_000): Promise<string> {
  const deadline = Date.now() + deadlineMs;
  while (Date.now() < deadline) {
    const r = await fetchText(url);
    if (r.status === 200 && r.body.includes('#EXTM3U')) return r.body;
    await new Promise((res) => setTimeout(res, 500));
  }
  throw new Error(`HLS manifest never became available at ${url}`);
}

test('native HLS endpoint serves a valid m3u8 + .ts segment', async ({ server, publisher }) => {
  // Use the legacy 2-part URL so we exercise the canonical redirect path.
  const manifestURL = `http://127.0.0.1:${server.httpPort}/hls/${publisher.app}/${publisher.name}.m3u8`;
  const manifest = await pollForManifest(manifestURL);

  // The playlist contains relative segment URIs; resolve them against the
  // canonical manifest URL (post-redirect) which is /hls/{app}/{name}/index.m3u8.
  const seg = manifest.split('\n').map((l) => l.trim()).find((l) => l.endsWith('.ts'));
  expect(seg, `manifest missing .ts segment:\n${manifest}`).toBeTruthy();

  const segURL = `http://127.0.0.1:${server.httpPort}/hls/${publisher.app}/${publisher.name}/${seg}`;
  const { status, bytes } = await fetchBytes(segURL);
  expect(status).toBe(200);
  expect(bytes.length).toBeGreaterThan(188);
  // MPEG-TS sync byte
  expect(bytes[0]).toBe(0x47);
});

test('Prometheus /metrics exposes per-stream counters', async ({ server, publisher }) => {
  // Hit /api/streams first to make sure the publisher is registered.
  const r1 = await fetchText(server.apiUrl('/api/streams'));
  expect(r1.status).toBe(200);

  const r2 = await fetchText(server.apiUrl('/metrics'));
  expect(r2.status).toBe(200);
  expect(r2.body).toContain('nonchalant_streams ');
  expect(r2.body).toContain(`nonchalant_messages_published_total{app="${publisher.app}",name="${publisher.name}"}`);
});
