// ABR (multi-bitrate) end-to-end test. The server is configured with two
// ladder rungs; we verify hls.js sees both as selectable levels and that
// playback advances through one of them.

import { test, expect } from '../fixtures/server';
import { waitForManifest } from '../fixtures/hls';

// Override the server fixture with a small two-rung ladder. Tiny resolutions
// keep the test cheap on CI runners.
test.use({
  serverExtraConfig: `hls:
  ladder:
    - {name: med, width: 320, height: 240, video_bitrate: 400}
    - {name: low, width: 160, height: 120, video_bitrate: 150}
`,
});

test('hls.js sees the ABR master playlist with multiple levels', async ({ server, publisher, page }) => {
  // Use the canonical /hls/{app}/{name}/index.m3u8 form (the master playlist).
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
  // The master must have advertised both rungs.
  expect(result.levelCount).toBeGreaterThanOrEqual(2);

  // Inspect the master playlist directly to confirm it lists each rung.
  const text = await (await page.request.get(playlist)).text();
  expect(text).toContain('#EXT-X-STREAM-INF');
  expect(text).toContain('med/index.m3u8');
  expect(text).toContain('low/index.m3u8');
});
