/**
 * e2e integration test for the Invites pane.
 *
 * Seeds one invite by having a throwaway IrcPeer send INVITE to the app user,
 * then asserts the full path works: the Invites tree node badges, and the
 * Invites pane renders the inviter. Deterministic and cross-platform — no
 * screenshot (Playwright snapshots are per-OS; the visual is covered by the
 * invites-view render unit test).
 */
import { test, expect } from '../lib/fixtures';
import { addNetworkAndConnect, selectNetwork } from '../lib/actions';
import { IrcPeer } from '../lib/irc-peer';

test('invites pane renders a received invite', async ({ page, runtime }) => {
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

    // Wait for the invite badge to appear on the Invites tree node.
    // The badge is a rounded pill next to the "Invites" label.
    const inviteBadge = page
      .getByTestId('server-tree')
      .locator('[title="Pending invites"]');
    await inviteBadge.waitFor({ state: 'visible', timeout: 15_000 });

    // Click the Invites pane.
    await page.getByTestId('server-tree').getByText('Invites', { exact: true }).click();

    // The invite should be visible in the pane — "invitebot" sent it.
    await expect(
      page.getByText('invitebot', { exact: false }),
    ).toBeVisible({ timeout: 10_000 });
  } finally {
    peer.close();
  }
});
