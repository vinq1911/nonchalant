// Verifies HTTP-FLV endpoint streams a valid FLV byte sequence.
// We can't use Playwright's request.get because the live response never ends —
// instead we open a raw HTTP request, grab the first chunks, then abort.

import http from 'node:http';
import { test, expect } from '../fixtures/server';

function readFlvHead(url: string, timeoutMs = 15_000): Promise<{ status: number; headers: http.IncomingHttpHeaders; head: Buffer }> {
  return new Promise((resolveP, rejectP) => {
    const req = http.get(url, (resp) => {
      const chunks: Buffer[] = [];
      let total = 0;
      const finish = () => {
        req.destroy();
        resolveP({ status: resp.statusCode ?? 0, headers: resp.headers, head: Buffer.concat(chunks) });
      };
      const timer = setTimeout(finish, timeoutMs);
      resp.on('data', (c: Buffer) => {
        chunks.push(c);
        total += c.length;
        if (total >= 64) {
          clearTimeout(timer);
          finish();
        }
      });
      resp.on('error', rejectP);
    });
    req.on('error', rejectP);
    req.setTimeout(timeoutMs, () => { req.destroy(new Error('timeout')); });
  });
}

test('HTTP-FLV response starts with FLV signature', async ({ server, publisher }) => {
  const { status, headers, head } = await readFlvHead(server.flvUrl(publisher.app, publisher.name));
  expect(status).toBe(200);
  expect((headers['content-type'] ?? '').toString()).toMatch(/flv/i);
  expect(head.length).toBeGreaterThanOrEqual(9);
  expect(head[0]).toBe(0x46); // F
  expect(head[1]).toBe(0x4c); // L
  expect(head[2]).toBe(0x56); // V
  expect(head[3]).toBe(0x01); // version 1
});
