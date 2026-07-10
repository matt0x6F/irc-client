/**
 * e2e integration test for received invites in the Activity inbox.
 *
 * Seeds one invite by having a throwaway IrcPeer send INVITE to the app user,
 * then asserts the full path works: the invite is absorbed into activity_items
 * and rendered in the global Activity inbox (which subsumed the old standalone
 * invites pane). Deterministic and cross-platform — no screenshot (Playwright
 * snapshots are per-OS; the visual is covered by the activity-inbox render unit test).
 */
import { test, expect } from '../lib/fixtures';
import { addNetworkAndConnect, selectNetwork } from '../lib/actions';
import { IrcPeer } from '../lib/irc-peer';

test('activity inbox renders a received invite', async ({ page, runtime }) => {
  await page.goto(runtime.bridgeUrl);
  await addNetworkAndConnect(page, runtime);
  await selectNetwork(page);

  const peer = new IrcPeer('localhost', runtime.ergoPort, 'invitebot');
  await peer.connect();
  // The peer joins a fresh channel, becoming its operator, then invites the
  // app user. IRC requires the inviter to be in the channel they invite to.
  peer.join('#invite-target');
  await peer.waitForJoin('#invite-target');

  try {
    // INVITE the app user to the channel the peer is in.
    peer.sendRaw('INVITE e2euser #invite-target');

    // Wait for the unseen-activity badge on the rail's Activity tile BEFORE
    // navigating. The badge (a numeric count) only appears once the invite has
    // been absorbed as an activity_items row and the rail has re-rendered, so
    // this removes the race between "click the tile" and "the invite arrives"
    // — we don't open the inbox until the item exists.
    await expect(page.getByTestId('rail-activity-badge')).toHaveText(/\d/, { timeout: 15_000 });

    // Open the global Activity destination on the rail.
    await page.getByTestId('rail-activity').click();

    // The invite should render as a row in the inbox — "invitebot" sent it to
    // #invite-target. Target the row by its accessible name (the row is a button
    // labelled "<channel> — open") to avoid matching both the row label and the
    // preview text.
    await expect(
      page.getByRole('button', { name: /#invite-target/ }),
    ).toBeVisible({ timeout: 10_000 });
    await expect(page.getByText('invitebot', { exact: false }).first()).toBeVisible();
  } finally {
    peer.close();
  }
});
