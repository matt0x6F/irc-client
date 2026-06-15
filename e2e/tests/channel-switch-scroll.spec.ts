import { test, expect } from '../lib/fixtures';
import { addNetworkAndConnect, selectNetwork, joinChannel, openChannel } from '../lib/actions';
import { IrcPeer } from '../lib/irc-peer';
import type { Page } from '@playwright/test';

// Regression coverage for the channel-switch scroll bug:
//
//   Switching to a channel that accumulated messages while it was unfocused left
//   the pane stuck at the previous scroll position (blank / not at the latest
//   message) until the user manually scrolled.
//
// Root cause (frontend/src/components/message-view.tsx): the channel-change
// auto-scroll effect advanced `prevChannelRef` on its first run — while the DOM
// still showed the *old* channel, because loadMessages() resolves a round-trip
// later. When the new channel's taller content finally rendered, the change-guard
// was already satisfied, so no scroll-to-bottom fired. The only scroll was a
// single fire-and-forget 100ms timeout that ran against the *previous* pane.
//
// Reproducing this deterministically in headless Chromium requires defeating the
// two things that otherwise hide the race:
//   1. message-view's *other* auto-scroll path scrolls to the bottom whenever the
//      message count grows. We switch FROM a channel with MORE messages so the
//      count does not grow on switch and that path stays silent.
//   2. With instant local IPC the 100ms timeout wins the race by luck. We wrap the
//      Wails GetMessages binding to add latency, mirroring the real (WebKit) app
//      where the load + render reliably outlasts the timeout.
// The target channel is made TALLER (long, wrapping messages) than the source so a
// retained source-scroll position is unambiguously short of the latest message.

// Per-execution counter feeds unique channel names so the persistent e2e DB (shared
// across specs and across retries/repeats) can't pollute the message counts this
// test relies on.
let execCount = 0;
const SOURCE_LINES = 100; // short, single-line rows
const TARGET_LINES = 80; // fewer rows, but each much taller (wraps) -> taller pane
const PAD = 'lorem ipsum '.repeat(20).trim(); // ~240 chars of *wrappable* words → tall rows
const IPC_LATENCY_MS = 400; // > the buggy 100ms scroll timeout; < assertion timeouts

/** Wrap window.go.main.App.GetMessages to resolve after `ms`, simulating real load latency. */
async function injectMessageLoadLatency(page: Page, ms: number): Promise<void> {
  await page.evaluate((delay) => {
    const app = (window as any).go?.main?.App;
    if (!app || typeof app.GetMessages !== 'function') {
      throw new Error('Wails App.GetMessages binding not available to instrument');
    }
    if (app.__getMessagesDelayed) return;
    const orig = app.GetMessages.bind(app);
    app.GetMessages = (...args: unknown[]) =>
      new Promise((resolve, reject) => setTimeout(() => orig(...args).then(resolve, reject), delay));
    app.__getMessagesDelayed = true;
  }, ms);
}

function distanceFromBottom(page: Page): Promise<number> {
  return page
    .getByTestId('message-list')
    .evaluate((el) => el.scrollHeight - el.scrollTop - el.clientHeight);
}

test('switching to a backlogged channel lands scrolled at the latest message', async ({
  page,
  runtime,
}) => {
  const run = `${Date.now()}x${execCount++}`;
  const SOURCE = `#src${run}`;
  const TARGET = `#tall${run}`;

  // Short viewport so both panes overflow and scroll position is meaningful.
  await page.setViewportSize({ width: 1000, height: 500 });
  await page.goto(runtime.bridgeUrl);
  await addNetworkAndConnect(page, runtime);
  await selectNetwork(page);

  // The UI user joins both channels so the backend receives & persists their
  // traffic. joinChannel leaves the joined channel focused; we end on SOURCE.
  await joinChannel(page, TARGET);
  await joinChannel(page, SOURCE); // focused pane = SOURCE; TARGET is backgrounded

  // A peer floods both channels (fakelag is disabled on the test server, so this
  // is instant). TARGET is flooded first so that once SOURCE's last line shows in
  // the focused pane, TARGET's backlog is already persisted.
  const peer = new IrcPeer('localhost', runtime.ergoPort, 'flooder');
  await peer.connect();
  for (const ch of [TARGET, SOURCE]) {
    peer.join(ch);
    await peer.waitForJoin(ch);
  }
  for (let i = 0; i < TARGET_LINES; i++) peer.say(TARGET, `t-${i} ${PAD}`);
  for (let i = 0; i < SOURCE_LINES; i++) peer.say(SOURCE, `s-${i}`);

  try {
    const list = page.getByTestId('message-list');

    // Wait until the focused SOURCE pane has fully loaded (its last line present),
    // which also guarantees TARGET's earlier backlog has been persisted.
    await expect(list.getByText(`s-${SOURCE_LINES - 1}`, { exact: true })).toBeAttached({
      timeout: 20_000,
    });

    // joinChannel auto-focuses a just-joined channel ~300ms after the join event
    // (App.tsx's pendingJoinChannelRef). Wait that window out so the deferred
    // selectPane(SOURCE) can't fire *after* our switch and snap the pane back.
    await page.waitForTimeout(600);

    // Now make message loads outlast the buggy 100ms scroll timeout, then switch
    // to the taller backlogged channel.
    await injectMessageLoadLatency(page, IPC_LATENCY_MS);
    await openChannel(page, TARGET);

    const lastLine = list.getByText(`t-${TARGET_LINES - 1}`, { exact: false });

    // Wait for the backlog to finish loading into the pane.
    await expect(lastLine).toBeAttached({ timeout: 20_000 });

    // Sanity: the target pane must overflow AND be taller than one viewport, else
    // the regression assertion would be vacuous.
    await expect
      .poll(() => list.evaluate((el) => el.scrollHeight - el.clientHeight), {
        message: 'target pane did not overflow; backlog too small to exercise scroll',
      })
      .toBeGreaterThan(0);

    // REGRESSION: the newest message must be visible and the pane scrolled to the
    // bottom on switch — not stranded at the previous (shorter) scroll position.
    await expect(lastLine).toBeInViewport({ timeout: 10_000 });
    await expect
      .poll(() => distanceFromBottom(page), {
        message: 'message pane not scrolled to the latest message after channel switch',
      })
      .toBeLessThanOrEqual(8);
  } finally {
    peer.close();
  }
});
