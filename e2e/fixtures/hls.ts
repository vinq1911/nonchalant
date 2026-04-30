// Shared helpers for HLS-based e2e tests.

import http from 'node:http';

// followOnce performs an HTTP GET; if the response is a redirect, follow once.
// Sufficient for our canonical-URL redirect (single hop).
function followOnce(url: string, timeoutMs = 1500): Promise<{ status: number; body: string }> {
  return new Promise((resolveP, rejectP) => {
    const req = http.get(url, (resp) => {
      const code = resp.statusCode ?? 0;
      if (code === 301 || code === 302 || code === 307 || code === 308) {
        const loc = resp.headers.location;
        resp.resume();
        if (typeof loc === 'string') {
          const next = loc.startsWith('http') ? loc : new URL(loc, url).toString();
          http.get(next, (r2) => {
            let b = '';
            r2.on('data', (c) => (b += c));
            r2.on('end', () => resolveP({ status: r2.statusCode ?? 0, body: b }));
            r2.on('error', rejectP);
          }).on('error', rejectP);
          return;
        }
      }
      let b = '';
      resp.on('data', (c) => (b += c));
      resp.on('end', () => resolveP({ status: code, body: b }));
      resp.on('error', rejectP);
    });
    req.on('error', rejectP);
    req.setTimeout(timeoutMs, () => { req.destroy(); resolveP({ status: 0, body: '' }); });
  });
}

// waitForManifest polls a manifest URL until it returns 200 with #EXTM3U
// content (following redirects). A fresh ffmpeg packager takes ~4 s to write
// the first playlist; hls.js otherwise exhausts its retry budget too soon.
export async function waitForManifest(url: string, deadlineMs = 30_000): Promise<void> {
  const deadline = Date.now() + deadlineMs;
  while (Date.now() < deadline) {
    const r = await followOnce(url).catch(() => ({ status: 0, body: '' }));
    if (r.status === 200 && r.body.includes('#EXTM3U')) return;
    await new Promise((r2) => setTimeout(r2, 500));
  }
  throw new Error(`manifest never ready: ${url}`);
}
