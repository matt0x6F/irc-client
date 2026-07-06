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

    // Open the global Activity destination (top of the tree). The inbox
    // subscribes to the activity-changed event, so it updates live when the
    // invite lands as an activity_items row.
    await page.getByTestId('server-tree').getByText('Activity', { exact: true }).click();

    // The invite should render in the inbox — "invitebot" sent it to #invite-target.
    await expect(
      page.getByText('invitebot', { exact: false }),
    ).toBeVisible({ timeout: 15_000 });
    await expect(
      page.getByText('#invite-target', { exact: false }),
    ).toBeVisible({ timeout: 10_000 });
  } finally {
    peer.close();
  }
});
