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

// Wails v3 binding ID for App.GetMessagesBeforeTime (the scroll-up pagination query).
// FNV hash of the fully-qualified method name — stable unless the method is renamed/
// moved; read it from frontend/bindings/.../app.js ($Call.ByID(...)) if that happens.
const GET_MESSAGES_BEFORE_TIME_METHOD_ID = 1921588377;

/** Delay the older-history load by `ms` so a test can observe pre-prepend state. */
async function injectHistoryLoadLatency(page: Page, ms: number): Promise<{ count: () => number }> {
  let intercepted = 0;
  await page.route('**/wails/runtime', async (route) => {
    const body = route.request().postData() ?? '';
    if (body.includes(String(GET_MESSAGES_BEFORE_TIME_METHOD_ID))) {
      intercepted++;
      await new Promise((r) => setTimeout(r, ms));
    }
    await route.continue();
  });
  return { count: () => intercepted };
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
  // TALL, wrapping rows so the virtualizer's 44px estimateSize is badly wrong for
  // the prepended (initially unmeasured) rows — this is what makes the anchor drift
  // as they measure, the way real multi-line chat history does. A uniform ~30px
  // single-line flood accidentally sits near the estimate and hides the bug.
  const PAD = 'lorem ipsum '.repeat(30).trim(); // ~3 wrapped lines at width 1000
  const body = (label: string) => `${label} ${PAD}`;
  // Locate a specific tall row's message element (its text is "<label> lorem ipsum …")
  // by its label. `^label ` disambiguates line-10 from line-100 etc.
  const rowByLabel = (label: string) => list.getByText(new RegExp(`^${label} `));

  await page.setViewportSize({ width: 1000, height: 500 });
  await page.goto(runtime.bridgeUrl);
  await addNetworkAndConnect(page, runtime);
  await selectNetwork(page);

  const list = page.getByTestId('message-list');

  // Join TARGET (so its traffic is persisted) but leave SOURCE focused, so TARGET's
  // flood is backgrounded and lands in the DB rather than streaming into the buffer.
  await joinChannel(page, TARGET);
  await joinChannel(page, SOURCE);

  const peer = new IrcPeer('localhost', runtime.ergoPort, 'flooder');
  await peer.connect();
  peer.join(TARGET);
  await peer.waitForJoin(TARGET);
  for (let i = 0; i < OLD_LINES; i++) peer.say(TARGET, body(`old-${i}`));

  try {
    // Time gap so the newer batch lands with strictly later timestamps than the old
    // batch (also covers the ~300ms deferred auto-focus of a just-joined channel).
    await page.waitForTimeout(1500);
    for (let i = 0; i < NEW_LINES; i++) peer.say(TARGET, body(`line-${i}`));

    // Switch into TARGET → windowed load of the latest 100 from the DB, pinned to
    // the bottom. The older rows (all of old-* plus the earliest line-*) stay on
    // disk for scroll-up pagination.
    await openChannel(page, TARGET);
    await expect(rowByLabel(`line-${NEWEST}`)).toBeAttached({ timeout: 20_000 });
    await expect.poll(() => distanceFromBottom(page)).toBeLessThanOrEqual(8);
    const beforeScrollHeight = await list.evaluate((el) => el.scrollHeight);

    // Delay the older-history load so there's a deterministic window between the
    // scroll-to-top (which fires it) and the prepend landing — long enough to read
    // the anchor's pre-prepend position without racing the round-trip.
    const latency = await injectHistoryLoadLatency(page, 900);

    // Scroll to the top so the pagination trigger fires (scrollTop <= 80).
    await list.evaluate((el) => el.scrollTo({ top: 0 }));
    await expect(page.getByTestId('scroll-to-bottom')).toBeVisible({ timeout: 10_000 });

    // The row now at the top of the viewport is our anchor. The prepend is still in
    // flight (delayed above), so this is genuinely the pre-prepend position.
    const firstText = await list.getByTestId('message-item').first().evaluate((el) => el.textContent ?? '');
    // textContent concatenates spans without spaces ("…flooderline-20 lorem…"), so
    // don't anchor on a leading word boundary. `line-\d+ ` (trailing space, present
    // because every body is "line-N lorem …") pins the exact label, not a prefix.
    const topLabel = firstText.match(/(line-\d+) /)?.[1] ?? null;
    expect(topLabel, `no line-N in top row; got: "${firstText.slice(0, 90)}"`).not.toBeNull();
    const anchorRow = rowByLabel(topLabel!);
    const beforeTop = await anchorRow.evaluate((el) => el.getBoundingClientRect().top);

    // Wait for the prepend AND for it to fully settle — the substantial jump happens
    // as the prepended tall rows measure and the scroll height keeps growing, not on
    // the first frame. Poll scrollHeight until it stops changing.
    await expect
      .poll(() => list.evaluate((el) => el.scrollHeight), {
        message: 'older history did not prepend; nothing to preserve the viewport against',
      })
      .toBeGreaterThan(beforeScrollHeight + 88);
    let prev = -1;
    await expect
      .poll(async () => {
        const h = await list.evaluate((el) => el.scrollHeight);
        const stable = h === prev;
        prev = h;
        return stable;
      })
      .toBe(true);

    expect(latency.count(), 'GetMessagesBeforeTime was never intercepted — stale method ID?').toBeGreaterThan(0);

    // REGRESSION (scroll-up jump): the row the user was reading must stay put — the
    // viewport must not lurch past the load point.
    const afterTop = await anchorRow.evaluate((el) => el.getBoundingClientRect().top);
    expect(
      Math.abs(afterTop - beforeTop),
      `anchor row moved ${Math.round(afterTop - beforeTop)}px when older history loaded (viewport jumped)`,
    ).toBeLessThan(48);
  } finally {
    peer.close();
  }
});

test('virtualized pane: FAST scrolling up through many history pages stays monotonic', async ({
  page,
  runtime,
}) => {
  // Exercises the multi-page fast-scroll path: page in several screens of history as
  // fast as possible and assert the top-of-view message index moves monotonically
  // BACKWARD (older) — no gross forward lurch. This guards against a scroll-restore
  // that over-compensates on prepend (e.g. the scrollHeight-delta approach ran away
  // into a forward over-scroll) and against broken pagination.
  //
  // NOTE: this does NOT reliably reproduce the concurrent-restore *race* that the
  // serialized pagination lock fixes — that race depends on loads resolving faster
  // than the ~600ms restore settles, which holds for the real app's instant local-DB
  // reads but NOT for e2e's slower HTTP-bridged reads (measured: fixed and unfixed
  // builds both show only ~6–12 messages of rapid-scroll render noise here). That fix
  // is verified manually against a copy of the real DB; see PR #145.
  const run = `${Date.now()}x${execCount++}`;
  const TARGET = `#fast${run}`;
  const SOURCE = `#src${run}`;
  const BATCHES = 5;
  const PER_BATCH = 100; // > SCROLLBACK_PAGE window so scroll-up pages in batch by batch
  const PAD = 'lorem ipsum '.repeat(30).trim(); // tall rows: 44px estimate is badly wrong
  // Global index m-0 (oldest) .. m-(N-1) (newest). Higher index == newer == further down.
  const topIndex = () =>
    page.getByTestId('message-list').evaluate((el) => {
      const lt = el.getBoundingClientRect().top;
      const items = [...el.querySelectorAll('[data-testid="message-item"]')].filter(
        (it) => it.getBoundingClientRect().bottom > lt + 2,
      );
      const t = items[0]?.textContent ?? '';
      // textContent concatenates spans without spaces ("…flooderm-499 lorem…"), so no
      // leading word boundary. `m-\d+ ` (trailing space) pins the label unambiguously.
      const m = t.match(/m-(\d+) /);
      return m ? Number(m[1]) : null;
    });

  await page.setViewportSize({ width: 1000, height: 500 });
  await page.goto(runtime.bridgeUrl);
  await addNetworkAndConnect(page, runtime);
  await selectNetwork(page);
  const list = page.getByTestId('message-list');

  await joinChannel(page, TARGET);
  await joinChannel(page, SOURCE);

  const peer = new IrcPeer('localhost', runtime.ergoPort, 'flooder');
  await peer.connect();
  peer.join(TARGET);
  await peer.waitForJoin(TARGET);

  try {
    // Time-separated batches so GetMessagesBeforeTime (WHERE timestamp < cursor) can
    // page across the boundaries instead of colliding on one instant.
    let idx = 0;
    for (let b = 0; b < BATCHES; b++) {
      for (let i = 0; i < PER_BATCH; i++) peer.say(TARGET, `m-${idx++} ${PAD}`);
      await page.waitForTimeout(1300);
    }
    const NEWEST = idx - 1;

    await page.waitForTimeout(600); // let deferred auto-focus settle
    await openChannel(page, TARGET);
    await expect(list.getByText(new RegExp(`^m-${NEWEST} `))).toBeAttached({ timeout: 20_000 });
    await expect.poll(() => distanceFromBottom(page)).toBeLessThanOrEqual(8);

    // Install a per-frame monitor of the top-of-view index. The concurrent-restore
    // thrash lurches the view forward for only a frame or two before settling, so
    // sampling between scroll steps misses it — we must watch every animation frame.
    await page.evaluate(() => {
      const w = window as unknown as { __wf: number; __li: number | null; __raf: number };
      w.__wf = 0;
      w.__li = null;
      const listEl = document.querySelector('[data-testid="message-list"]')!;
      const tick = () => {
        const lt = listEl.getBoundingClientRect().top;
        const it = [...listEl.querySelectorAll('[data-testid="message-item"]')].find(
          (e) => e.getBoundingClientRect().bottom > lt + 2,
        );
        const m = (it?.textContent ?? '').match(/m-(\d+) /);
        const idx = m ? Number(m[1]) : null;
        if (idx != null && w.__li != null) w.__wf = Math.max(w.__wf, idx - w.__li);
        if (idx != null) w.__li = idx;
        w.__raf = requestAnimationFrame(tick);
      };
      w.__raf = requestAnimationFrame(tick);
    });

    // Scroll up as fast as possible, paging in history back-to-back.
    for (let step = 0; step < 40; step++) {
      await list.evaluate((el) => el.scrollBy({ top: -3000 }));
      await page.waitForTimeout(45);
      if ((await list.evaluate((el) => el.scrollTop)) === 0) await page.waitForTimeout(120);
    }

    const worstForward = await page.evaluate(() => {
      const w = window as unknown as { __wf: number; __raf: number };
      cancelAnimationFrame(w.__raf);
      return w.__wf;
    });

    // Sanity: we must have actually paged through history (else the test is vacuous).
    const finalTop = await topIndex();
    expect(finalTop, 'never paged past the initial window — flood/pagination too small').toBeLessThan(NEWEST - PER_BATCH);
    // No GROSS forward lurch. The threshold is well above the ~6–12 of rapid-scroll
    // render noise (see the NOTE above) but far below the hundreds a runaway restore or
    // buffer-window slide produces.
    expect(worstForward, `top-of-view lurched forward by ${worstForward} messages during fast scroll-up`).toBeLessThan(60);
  } finally {
    peer.close();
  }
});

test('virtualized pane: scrolling up on a BUSY live channel does not jump as new messages arrive', async ({
  page,
  runtime,
}) => {
  const run = `${Date.now()}x${execCount++}`;
  const TARGET = `#busy${run}`;
  const SOURCE = `#src${run}`;
  const SEED = 130; // > 100 so the loaded window caps at 100 and shifts as new msgs arrive
  const BURST = 15; // new messages that arrive while the user is reading history
  const PAD = 'lorem ipsum '.repeat(30).trim();
  const body = (label: string) => `${label} ${PAD}`;

  await page.setViewportSize({ width: 1000, height: 500 });
  await page.goto(runtime.bridgeUrl);
  await addNetworkAndConnect(page, runtime);
  await selectNetwork(page);

  const list = page.getByTestId('message-list');
  const rowByLabel = (label: string) => list.getByText(new RegExp(`^${label} `));

  await joinChannel(page, TARGET);
  await joinChannel(page, SOURCE);

  const peer = new IrcPeer('localhost', runtime.ergoPort, 'flooder');
  await peer.connect();
  peer.join(TARGET);
  await peer.waitForJoin(TARGET);
  for (let i = 0; i < SEED; i++) peer.say(TARGET, body(`line-${i}`));

  try {
    await page.waitForTimeout(600);
    await openChannel(page, TARGET);
    await expect(rowByLabel(`line-${SEED - 1}`)).toBeAttached({ timeout: 20_000 });
    await expect.poll(() => distanceFromBottom(page)).toBeLessThanOrEqual(8);

    // Scroll up into the MIDDLE of the window — released from the bottom pin, but not
    // at the very top, so the pane stays in 'live' mode (no anchored history load).
    // This is the state where App.tsx reloads the whole buffer on every new message.
    await list.evaluate((el) => el.scrollTo({ top: Math.round(el.scrollHeight * 0.4) }));
    await expect(page.getByTestId('scroll-to-bottom')).toBeVisible({ timeout: 10_000 });
    await page.waitForTimeout(300); // let measurement settle at the new position

    const topLabel = (await list.getByTestId('message-item').first().evaluate((el) => el.textContent ?? ''))
      .match(/(line-\d+) /)?.[1];
    expect(topLabel, 'no line-N at the top after scrolling up').toBeTruthy();
    const anchorRow = rowByLabel(topLabel!);
    const beforeTop = await anchorRow.evaluate((el) => el.getBoundingClientRect().top);

    // A burst of new traffic arrives while the user reads history. Each message makes
    // App.tsx call loadMessages(), replacing the buffer with the latest 100 (the
    // window shifts). The row the user is reading must not move.
    for (let i = 0; i < BURST; i++) peer.say(TARGET, body(`line-${SEED + i}`));
    await page.waitForTimeout(1500); // let the reloads land + measure

    const afterTop = await anchorRow.evaluate((el) => el.getBoundingClientRect().top);
    expect(
      Math.abs(afterTop - beforeTop),
      `anchor row moved ${Math.round(afterTop - beforeTop)}px as live messages arrived (viewport jumped forward)`,
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
