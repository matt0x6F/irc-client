/**
 * Functional e2e assertions for IRCv3 +draft/reply and +draft/channel-context.
 *
 * These run unconditionally in CI — no CASCADE_SCREENSHOTS gate. They exercise
 * the full tag→parse→store→render round-trip against a live Ergo server.
 *
 * +draft/reply msgid round-trip strategy (test B):
 *   The peer negotiates message-tags + echo-message so Ergo echoes its outbound
 *   PRIVMSG back to it with the server-assigned @msgid. We capture that echo via
 *   IrcPeer.captureLine() and parse the msgid, then immediately send the reply
 *   referencing it. If the echo is not available within the timeout, the test
 *   falls back to asserting the unresolved-reply render path (still exercises the
 *   +draft/reply parse→store→render code path end-to-end).
 *
 * Ergo requires both the sender AND receiver to have negotiated message-tags for
 * client-only tags (prefixed with +) to be relayed. Both peers in this spec
 * negotiate message-tags accordingly.
 */

import { test, expect } from '../lib/fixtures';
import { addNetworkAndConnect, selectNetwork, joinChannel } from '../lib/actions';
import { IrcPeer } from '../lib/irc-peer';

// ---------------------------------------------------------------------------
// A) +draft/channel-context pill in a PM view
// ---------------------------------------------------------------------------

test('+draft/channel-context tag renders a #channel pill in the PM view', async ({ page, runtime }) => {
  await page.goto(runtime.bridgeUrl);
  await addNetworkAndConnect(page, runtime);
  await selectNetwork(page);

  const peer = new IrcPeer('localhost', runtime.ergoPort, 'ctxbot');
  // Negotiate message-tags so Ergo relays client-only tags (prefixed with +)
  // from this peer to the app, which also has message-tags negotiated.
  await peer.connect(30_000, ['message-tags']);

  try {
    // Send a PM to the app user carrying the +draft/channel-context tag.
    // Ergo relays client-only tags (prefixed with +) between clients that have
    // negotiated message-tags — the app negotiates it, so the tag passes through.
    const unique = `ctx-pill-${Date.now()}`;
    peer.sendRaw(`@+draft/channel-context=#test PRIVMSG e2euser :${unique}`);

    // The PM conversation appears in the server tree once the message lands.
    // Click the peer's nick to open the PM pane.
    await page
      .getByTestId('server-tree')
      .getByText('ctxbot', { exact: true })
      .waitFor({ state: 'visible', timeout: 15_000 });
    await page.getByTestId('server-tree').getByText('ctxbot', { exact: true }).click();

    // The message text must be visible in the PM pane.
    const msgList = page.getByTestId('message-list');
    await expect(msgList.getByText(unique)).toBeVisible({ timeout: 15_000 });

    // The channel-context pill must appear alongside the message, showing #test.
    // It renders as a button with class "channel-context-pill" containing the channel name.
    await expect(msgList.locator('.channel-context-pill')).toBeVisible({ timeout: 10_000 });
    await expect(msgList.locator('.channel-context-pill')).toContainText('#test');
  } finally {
    peer.close();
  }
});

// ---------------------------------------------------------------------------
// B) +draft/reply round-trip
//
// Primary path: the peer negotiates echo-message so Ergo echoes outbound
// PRIVMSGs back to the peer with the server-assigned @msgid. captureLine()
// pulls that msgid from the echo; the peer then sends a reply referencing it.
// The app, which has message-tags negotiated, receives the @+draft/reply-tagged
// PRIVMSG and renders a .reply-quote showing the parent.
//
// Fallback path: if the echo-based msgid capture times out, we fall back to
// reading the msgid from the DOM's data-msgid attribute. If that too is absent,
// we assert the unresolved-reply render path ("replying to an earlier message"),
// which still proves the +draft/reply parse→store→render pipeline end-to-end.
// ---------------------------------------------------------------------------

test('+draft/reply tag renders a reply-quote preview in the channel view', async ({ page, runtime }) => {
  await page.goto(runtime.bridgeUrl);
  await addNetworkAndConnect(page, runtime);
  await selectNetwork(page);
  await joinChannel(page, '#reply-test');

  const peer = new IrcPeer('localhost', runtime.ergoPort, 'replybot');
  // echo-message: Ergo echoes our outbound PRIVMSGs back with @msgid stamped.
  // message-tags: required for Ergo to relay client-only (+) tags to the app.
  await peer.connect(30_000, ['message-tags', 'echo-message']);
  peer.join('#reply-test');
  await peer.waitForJoin('#reply-test');

  try {
    const parentText = `parent-msg-${Date.now()}`;

    // Start listening for the echo BEFORE sending (avoids a race).
    const echoPromise = peer.captureLine(
      new RegExp(`PRIVMSG #reply-test :${parentText.replace(/[-[\]{}()*+?.,\\^$|#\s]/g, '\\$&')}`),
      10_000,
    ).catch(() => null); // null on timeout → fall back to DOM path

    peer.say('#reply-test', parentText);

    const msgList = page.getByTestId('message-list');

    // Wait for the parent to appear in the app.
    await expect(msgList.getByText(parentText)).toBeVisible({ timeout: 15_000 });

    // Try to get the msgid from the echo first, then from the DOM.
    let msgid: string | null = null;
    const echoLine = await echoPromise;
    if (echoLine) {
      // The echo line looks like: "@msgid=abc;…tag… :replybot!u@h PRIVMSG #chan :text"
      const m = echoLine.match(/(?:^|[;@])msgid=([^;\s]+)/);
      if (m) msgid = m[1];
    }

    if (!msgid) {
      // Secondary: read data-msgid from the DOM row.
      const parentRow = msgList.locator('[data-testid="message-item"]').filter({ hasText: parentText });
      msgid = await parentRow.getAttribute('data-msgid').catch(() => null);
    }

    if (msgid) {
      // Full round-trip: send a real reply referencing the server-assigned msgid.
      const replyText = `reply-to-${msgid.slice(0, 8)}-${Date.now()}`;
      peer.sendRaw(`@+draft/reply=${msgid} PRIVMSG #reply-test :${replyText}`);

      await expect(msgList.getByText(replyText)).toBeVisible({ timeout: 15_000 });

      // The reply row must render a .reply-quote button. Because the parent is in
      // the loaded message window, the quote shows nick + snippet.
      const replyRow = msgList.locator('[data-testid="message-item"]').filter({ hasText: replyText });
      await expect(replyRow.locator('.reply-quote')).toBeVisible({ timeout: 10_000 });
      await expect(replyRow.locator('.reply-quote')).toContainText('replybot');
    } else {
      // Fallback: send a reply with a nonexistent msgid to exercise the
      // unresolved-reply render path ("replying to an earlier message").
      const fallbackReplyText = `unresolved-reply-${Date.now()}`;
      peer.sendRaw(`@+draft/reply=nonexistent-id-${Date.now()} PRIVMSG #reply-test :${fallbackReplyText}`);

      await expect(msgList.getByText(fallbackReplyText)).toBeVisible({ timeout: 15_000 });

      const replyRow = msgList.locator('[data-testid="message-item"]').filter({ hasText: fallbackReplyText });
      await expect(replyRow.locator('.reply-quote')).toBeVisible({ timeout: 10_000 });
      await expect(replyRow.locator('.reply-quote')).toContainText('replying to an earlier message');
    }
  } finally {
    peer.close();
  }
});
