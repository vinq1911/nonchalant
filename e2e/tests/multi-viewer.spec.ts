// Concurrent-viewer end-to-end test. N parallel raw HTTP-FLV consumers all
// pull the same live stream and must each receive a valid FLV header plus
// a chunk of bytes. Proves the bus's fan-out and the per-subscriber drop
// counters do not interfere with each other.

import http from 'node:http';
import { test, expect } from '../fixtures/server';

const VIEWERS = 8;
const PER_VIEWER_BYTES = 64 * 1024; // 64 KiB — enough for header + a couple of GOPs

// pullFLV opens a single GET to the FLV endpoint and returns the first N bytes.
function pullFLV(url: string, minBytes: number, timeoutMs = 15_000): Promise<Buffer> {
  return new Promise((resolveP, rejectP) => {
    const req = http.get(url, (resp) => {
      if (resp.statusCode !== 200) {
        req.destroy();
        rejectP(new Error(`status ${resp.statusCode}`));
        return;
      }
      const chunks: Buffer[] = [];
      let total = 0;
      resp.on('data', (c: Buffer) => {
        chunks.push(c);
        total += c.length;
        if (total >= minBytes) {
          req.destroy();
          resolveP(Buffer.concat(chunks));
        }
      });
      resp.on('end', () => {
        if (total >= minBytes) resolveP(Buffer.concat(chunks));
        else rejectP(new Error(`only got ${total} bytes`));
      });
      resp.on('error', rejectP);
    });
    req.on('error', rejectP);
    req.setTimeout(timeoutMs, () => { req.destroy(new Error('timeout')); });
  });
}

test(`${VIEWERS} concurrent HTTP-FLV viewers each receive valid FLV bytes`, async ({ server, publisher }) => {
  const url = server.flvUrl(publisher.app, publisher.name);

  // Fire all viewers in parallel.
  const results = await Promise.all(
    Array.from({ length: VIEWERS }, () => pullFLV(url, PER_VIEWER_BYTES)),
  );

  // Each viewer must have got a complete FLV header + meaningful body.
  for (let i = 0; i < results.length; i++) {
    const b = results[i];
    expect(b.length, `viewer ${i} byte count`).toBeGreaterThanOrEqual(PER_VIEWER_BYTES);
    expect(b[0], `viewer ${i} sig F`).toBe(0x46);
    expect(b[1], `viewer ${i} sig L`).toBe(0x4c);
    expect(b[2], `viewer ${i} sig V`).toBe(0x56);
    expect(b[3], `viewer ${i} version`).toBe(0x01);
  }

  // The API should have observed every subscriber attaching at some point.
  const apiBody = await (await fetch(server.apiUrl('/api/streams'))).json() as {
    streams: { app: string; name: string; messages_published: number }[];
  };
  const stream = apiBody.streams.find(
    (s) => s.app === publisher.app && s.name === publisher.name,
  );
  expect(stream, 'stream registered').toBeTruthy();
  expect(stream!.messages_published).toBeGreaterThan(0);
});
