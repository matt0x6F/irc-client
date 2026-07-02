import { test, expect } from '../lib/fixtures';
import { addNetworkAndConnect, selectNetwork, joinChannel } from '../lib/actions';
import { IrcPeer } from '../lib/irc-peer';
import type { Page } from '@playwright/test';

// Coverage for the virtualized message list (frontend/src/components/message-view.tsx):
// with @tanstack/react-virtual only the visible window is in the DOM, so the
// scroll-dependent behaviors (stay-at-bottom, scroll-up releases the pin, the
// return-to-bottom badge, and scroll-to-top pagination with viewport preservation)
// all have to keep working against a virtual scroll height. jsdom has no layout, so
// these can only be verified in a real browser here.

let execCount = 0;

function distanceFromBottom(page: Page): Promise<number> {
  return page
    .getByTestId('message-list')
    .evaluate((el) => el.scrollHeight - el.scrollTop - el.clientHeight);
}

test('virtualized pane: pins to bottom, releases on scroll-up, and the badge returns to bottom', async ({
  page,
  runtime,
}) => {
  const run = `${Date.now()}x${execCount++}`;
  const CH = `#virt${run}`;
  const LINES = 120; // > one viewport of rows so the pane overflows

  await page.setViewportSize({ width: 1000, height: 500 });
  await page.goto(runtime.bridgeUrl);
  await addNetworkAndConnect(page, runtime);
  await selectNetwork(page);
  await joinChannel(page, CH);

  const peer = new IrcPeer('localhost', runtime.ergoPort, 'flooder');
  await peer.connect();
  peer.join(CH);
  await peer.waitForJoin(CH);
  for (let i = 0; i < LINES; i++) peer.say(CH, `line-${i}`);

  try {
    const list = page.getByTestId('message-list');

    // Latest line present and pane pinned to the bottom.
    await expect(list.getByText(`line-${LINES - 1}`, { exact: true })).toBeAttached({ timeout: 20_000 });
    await expect.poll(() => distanceFromBottom(page)).toBeLessThanOrEqual(8);

    // The pane must overflow, else the scroll assertions are vacuous.
    await expect
      .poll(() => list.evaluate((el) => el.scrollHeight - el.clientHeight))
      .toBeGreaterThan(0);

    // Scroll to the top: releases the bottom-pin and shows the return-to-bottom badge.
    await list.evaluate((el) => el.scrollTo({ top: 0 }));
    await expect(page.getByTestId('scroll-to-bottom')).toBeVisible({ timeout: 10_000 });
    await expect.poll(() => distanceFromBottom(page)).toBeGreaterThan(100);

    // Click the badge → returns to the bottom, latest line visible again.
    await page.getByTestId('scroll-to-bottom').click();
    await expect(list.getByText(`line-${LINES - 1}`, { exact: true })).toBeInViewport({ timeout: 10_000 });
    await expect.poll(() => distanceFromBottom(page)).toBeLessThanOrEqual(8);
  } finally {
    peer.close();
  }
});

test('virtualized pane: only a windowed subset of rows is mounted in the DOM', async ({
  page,
  runtime,
}) => {
  const run = `${Date.now()}x${execCount++}`;
  const CH = `#dom${run}`;
  const LINES = 120; // many more than fit in one viewport

  await page.setViewportSize({ width: 1000, height: 500 });
  await page.goto(runtime.bridgeUrl);
  await addNetworkAndConnect(page, runtime);
  await selectNetwork(page);
  await joinChannel(page, CH);

  const peer = new IrcPeer('localhost', runtime.ergoPort, 'flooder');
  await peer.connect();
  peer.join(CH);
  await peer.waitForJoin(CH);
  for (let i = 0; i < LINES; i++) peer.say(CH, `line-${i}`);

  try {
    const list = page.getByTestId('message-list');

    // Latest line present and pinned to the bottom (so the window is fully loaded).
    await expect(list.getByText(`line-${LINES - 1}`, { exact: true })).toBeInViewport({ timeout: 20_000 });

    // The core virtualization win: despite the buffer holding ~100 messages, only a
    // small windowed subset of rows is actually mounted in the DOM. Without
    // virtualization every loaded row would be present.
    const mounted = await page.getByTestId('message-item').count();
    expect(mounted, 'no rows rendered').toBeGreaterThan(0);
    expect(mounted, 'virtualizer should mount only a window, not the whole buffer').toBeLessThan(60);
  } finally {
    peer.close();
  }
});
