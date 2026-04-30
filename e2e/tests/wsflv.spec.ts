// End-to-end browser playback: open a flv.js page, point it at the live WS-FLV
// endpoint, and verify the <video> element actually decodes frames.

import { test, expect } from '../fixtures/server';

test('flv.js plays live WebSocket-FLV stream in the browser', async ({ server, publisher, page }) => {
  const consoleErrors: string[] = [];
  page.on('console', (msg) => {
    if (msg.type() === 'error') consoleErrors.push(msg.text());
  });
  page.on('pageerror', (e) => consoleErrors.push(`pageerror: ${e.message}`));

  await page.goto(server.pageUrl('wsflv-player.html'));
  await expect(page.locator('#player')).toBeVisible();

  const wsUrl = server.wsUrl(publisher.app, publisher.name);
  const result = await page.evaluate(
    (url) => (window as unknown as { startPlayback: (u: string) => Promise<unknown> }).startPlayback(url),
    wsUrl,
  ) as { currentTime: number; videoWidth: number; videoHeight: number };

  expect(result.currentTime).toBeGreaterThan(0);
  expect(result.videoWidth).toBeGreaterThan(0);
  expect(result.videoHeight).toBeGreaterThan(0);

  // Give it another second and confirm playback advances.
  const before = result.currentTime;
  await page.waitForTimeout(1500);
  const after = await page.evaluate(
    () => (document.getElementById('player') as HTMLVideoElement).currentTime,
  );
  expect(after, 'currentTime should advance during live playback').toBeGreaterThan(before);

  expect(consoleErrors, `unexpected console errors: ${consoleErrors.join(' | ')}`).toEqual([]);
});
