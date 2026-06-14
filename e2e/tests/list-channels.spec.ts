import { test, expect } from '../lib/fixtures';
import { addNetworkAndConnect, selectNetwork } from '../lib/actions';
import { IrcPeer } from '../lib/irc-peer';

/**
 * Browse Channels must list the server's channels end-to-end (real Ergo + a peer
 * that makes a channel exist).
 *
 * Scope note: this guards the happy path (request → 322/323 parse → event shape →
 * modal render). It does NOT reproduce the original "always empty" bug, which
 * needed a server that returns an empty list for a rapid *duplicate* LIST (the
 * StrictMode double-request). The test Ergo tolerates duplicate LISTs, so this
 * spec passes with or without the single-request guard. The guard is still
 * correct (don't issue overlapping LISTs); this just can't prove it by failing.
 */
test('Browse Channels lists channels that exist on the server', async ({ page, runtime }) => {
  await page.goto(runtime.bridgeUrl);
  await addNetworkAndConnect(page, runtime);
  await selectNetwork(page);

  // Make a channel exist on the server by having a throwaway peer join it.
  const channel = `#browse-${Date.now().toString(36)}`;
  const peer = new IrcPeer('localhost', runtime.ergoPort, 'listerbot');
  await peer.connect();
  peer.join(channel);
  await peer.waitForJoin(channel);

  try {
    // Open the Browse Channels overlay from the header.
    await page.getByRole('button', { name: 'Browse channels' }).click();

    // Modal opened (header is the sentinel).
    await expect(page.getByText('Browse Channels', { exact: true })).toBeVisible({ timeout: 10_000 });

    // The peer's channel must appear — and the empty state must NOT.
    await expect(page.getByText(channel, { exact: false })).toBeVisible({ timeout: 25_000 });
    await expect(page.getByText('No channels found.')).toHaveCount(0);
  } finally {
    peer.close();
  }
});
