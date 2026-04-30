// Plays the live HLS stream end-to-end through hls.js in Chromium and asserts
// the <video> element actually decodes frames. Complements hls.spec.ts which
// only validates the playlist + segment bytes.

import { test, expect } from '../fixtures/server';
import { waitForManifest } from '../fixtures/hls';

test('hls.js plays the live HLS stream in the browser', async ({ server, publisher, page }) => {
  const consoleErrors: string[] = [];
  page.on('console', (msg) => {
    if (msg.type() === 'error') consoleErrors.push(msg.text());
  });
  page.on('pageerror', (e) => consoleErrors.push(`pageerror: ${e.message}`));

  // Use the canonical /hls/{app}/{name}/index.m3u8 form so hls.js resolves
  // relative segment URIs against the same path the server serves them at.
  const playlist = `http://127.0.0.1:${server.httpPort}/hls/${publisher.app}/${publisher.name}/index.m3u8`;
  await waitForManifest(playlist);

  await page.goto(server.pageUrl('hls-player.html'));
  await expect(page.locator('#player')).toBeVisible();
  const result = await page.evaluate(
    (url) => (window as unknown as {
      startPlayback: (u: string) => Promise<unknown>;
    }).startPlayback(url),
    playlist,
  ) as { currentTime: number; videoWidth: number; videoHeight: number; levelCount: number };

  expect(result.currentTime).toBeGreaterThan(0);
  expect(result.videoWidth).toBeGreaterThan(0);
  expect(result.videoHeight).toBeGreaterThan(0);
  expect(result.levelCount).toBeGreaterThanOrEqual(1);

  // Confirm playback advances after another second — proves it's live, not stalled.
  const before = result.currentTime;
  await page.waitForTimeout(1500);
  const after = await page.evaluate(
    () => (document.getElementById('player') as HTMLVideoElement).currentTime,
  );
  expect(after, `currentTime should advance from ${before}`).toBeGreaterThan(before);

  // We tolerate non-fatal hls.js errors (buffer stalls etc.) — only fail on
  // unrelated console errors.
  const unrelated = consoleErrors.filter((m) => !m.includes('[hls]') && !m.includes('hls.js'));
  expect(unrelated, `unexpected errors: ${unrelated.join(' | ')}`).toEqual([]);
});
