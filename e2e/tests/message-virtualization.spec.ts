import { test, expect } from '../lib/fixtures';
import { addNetworkAndConnect, selectNetwork, joinChannel, openChannel } from '../lib/actions';
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

test('virtualized pane: loading older history preserves the viewport (no jump past the load point)', async ({
  page,
  runtime,
}) => {
  const run = `${Date.now()}x${execCount++}`;
  const TARGET = `#older${run}`;
  const SOURCE = `#src${run}`;
  // NEW_LINES > 100 so switching *into* TARGET loads only the latest-100 window from
  // the DB, leaving older rows on disk to page in on scroll-up. (Flooding the focused
  // pane instead would stream them all live into the buffer, so messages[0] would
  // already be the oldest and there'd be nothing older to load.) The OLDER batch is
  // sent first with a real time gap so those rows have strictly smaller timestamps
  // than the window's oldest — GetMessagesBeforeTime pages by `timestamp < cursor`,
  // and a single instantaneous burst collides on one timestamp with nothing before it.
  const OLD_LINES = 60;
  const NEW_LINES = 120;
  const NEWEST = NEW_LINES - 1;

  await page.setViewportSize({ width: 1000, height: 500 });
  await page.goto(runtime.bridgeUrl);
  await addNetworkAndConnect(page, runtime);
  await selectNetwork(page);

  // Join TARGET (so its traffic is persisted) but leave SOURCE focused, so TARGET's
  // flood is backgrounded and lands in the DB rather than streaming into the buffer.
  await joinChannel(page, TARGET);
  await joinChannel(page, SOURCE);

  const peer = new IrcPeer('localhost', runtime.ergoPort, 'flooder');
  await peer.connect();
  peer.join(TARGET);
  await peer.waitForJoin(TARGET);
  for (let i = 0; i < OLD_LINES; i++) peer.say(TARGET, `old-${i}`);

  try {
    const list = page.getByTestId('message-list');

    // Time gap so the newer batch lands with strictly later timestamps than the old
    // batch (also covers the ~300ms deferred auto-focus of a just-joined channel).
    await page.waitForTimeout(1500);
    for (let i = 0; i < NEW_LINES; i++) peer.say(TARGET, `line-${i}`);

    // Switch into TARGET → windowed load of the latest 100 from the DB, pinned to
    // the bottom. The older rows (all of old-* plus the earliest line-*) stay on
    // disk for scroll-up pagination.
    await openChannel(page, TARGET);
    await expect(list.getByText(`line-${NEWEST}`, { exact: true })).toBeAttached({ timeout: 20_000 });
    await expect.poll(() => distanceFromBottom(page)).toBeLessThanOrEqual(8);
    const beforeScrollHeight = await list.evaluate((el) => el.scrollHeight);

    // Scroll to the very top: releases the pin, shows the badge, and triggers the
    // older-history load.
    await list.evaluate((el) => el.scrollTo({ top: 0 }));
    await expect(page.getByTestId('scroll-to-bottom')).toBeVisible({ timeout: 10_000 });

    // Anchor on whatever line is currently at the top of the viewport. Capture its
    // on-screen position *before* the older page prepends (the load is an async
    // round-trip, so this read wins the race), then wait for the prepend to land.
    const anchorName = (await list.getByText(/^line-\d+$/).first().textContent())!.trim();
    const anchorRow = list.getByText(anchorName, { exact: true });
    const beforeTop = await anchorRow.evaluate((el) => el.getBoundingClientRect().top);

    // The prepend grows the scroll height as older rows are added and measured.
    await expect
      .poll(() => list.evaluate((el) => el.scrollHeight), {
        message: 'older history did not prepend; nothing to preserve the viewport against',
      })
      .toBeGreaterThan(beforeScrollHeight + 88);

    // REGRESSION (scroll-up jump): the row the user was reading must stay put — the
    // viewport must not lurch past the load point. anchorTo:'end' re-anchors it, so
    // its on-screen position holds within roughly one row.
    await expect(anchorRow).toBeInViewport();
    const afterTop = await anchorRow.evaluate((el) => el.getBoundingClientRect().top);
    expect(
      Math.abs(afterTop - beforeTop),
      'viewport jumped when older history loaded',
    ).toBeLessThan(48);
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
