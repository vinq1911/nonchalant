// Verifies the play-key auth gate end-to-end. The server is configured with
// a single play_key; FLV / WS-FLV / HLS / DASH endpoints must reject requests
// without the key and accept requests with it. /api and /healthz must remain
// open.

import http from 'node:http';
import { test, expect } from '../fixtures/server';

test.use({
  serverExtraConfig: `auth:
  play_keys:
    - secret-watch
`,
});

// statusOf opens an HTTP request and returns the response code without
// consuming the body. We need this for live endpoints that never close.
function statusOf(url: string, timeoutMs = 5_000): Promise<number> {
  return new Promise((resolveP, rejectP) => {
    const req = http.get(url, (resp) => {
      req.destroy();
      resolveP(resp.statusCode ?? 0);
    });
    req.on('error', rejectP);
    req.setTimeout(timeoutMs, () => { req.destroy(new Error('timeout')); });
  });
}

test('play_key gate rejects anonymous and accepts authenticated requests', async ({ server, publisher }) => {
  const flvBase = server.flvUrl(publisher.app, publisher.name);
  const hlsBase = `http://127.0.0.1:${server.httpPort}/hls/${publisher.app}/${publisher.name}.m3u8`;

  // Anonymous → 401.
  expect(await statusOf(flvBase)).toBe(401);
  expect(await statusOf(hlsBase)).toBe(401);

  // Wrong key → 401.
  expect(await statusOf(`${flvBase}?key=nope`)).toBe(401);
  expect(await statusOf(`${hlsBase}?key=nope`)).toBe(401);

  // Right key → 200 (or 503 if ffmpeg/manifest race; either way NOT 401).
  const flvOK = await statusOf(`${flvBase}?key=secret-watch`);
  expect(flvOK).not.toBe(401);
  expect(flvOK).toBe(200);

  // Unauthenticated control surfaces remain open: /healthz, /api/streams,
  // /metrics. Auth must not have leaked into them.
  expect(await statusOf(server.apiUrl('/healthz'))).toBe(200);
  expect(await statusOf(server.apiUrl('/api/streams'))).toBe(200);
  expect(await statusOf(server.apiUrl('/metrics'))).toBe(200);
});
