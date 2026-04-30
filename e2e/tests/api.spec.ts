// Smoke tests against the HTTP API. No browser needed but worth running first.

import { test, expect } from '../fixtures/server';

test.describe('HTTP API', () => {
  test('GET /healthz returns 200', async ({ server, request }) => {
    const r = await request.get(server.apiUrl('/healthz'));
    expect(r.status()).toBe(200);
  });

  test('GET /api/server returns server info', async ({ server, request }) => {
    const r = await request.get(server.apiUrl('/api/server'));
    expect(r.status()).toBe(200);
    const body = await r.json();
    expect(body).toBeDefined();
  });

  test('GET /api/streams returns array', async ({ server, request }) => {
    const r = await request.get(server.apiUrl('/api/streams'));
    expect(r.status()).toBe(200);
    const body = await r.json();
    expect(Array.isArray(body.streams)).toBe(true);
  });

  test('GET /api/streams shows live publisher', async ({ server, publisher, request }) => {
    const r = await request.get(server.apiUrl('/api/streams'));
    expect(r.status()).toBe(200);
    const body = await r.json();
    const found = body.streams.find(
      (s: { app: string; name: string; has_publisher: boolean }) =>
        s.app === publisher.app && s.name === publisher.name,
    );
    expect(found, 'published stream should appear in /api/streams').toBeTruthy();
    expect(found.has_publisher).toBe(true);
  });
});
