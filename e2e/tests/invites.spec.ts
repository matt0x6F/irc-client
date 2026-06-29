/**
 * e2e screenshot of the Invites pane.
 *
 * Seeds one invite by having a throwaway IrcPeer send INVITE to the app user,
 * then navigates to the Invites pane and captures a screenshot.
 *
 * Run to generate / update the baseline:
 *   cd e2e && npx playwright test tests/invites.spec.ts
 */
import { test, expect } from '../lib/fixtures';
import { addNetworkAndConnect, selectNetwork, joinChannel } from '../lib/actions';
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

    // The invite should be visible — "invitebot" sent the invite.
    await expect(
      page.getByText('invitebot', { exact: false }),
    ).toBeVisible({ timeout: 10_000 });

    // Snapshot the invites pane.
    await expect(page).toHaveScreenshot('invites-pane.png');
  } finally {
    peer.close();
  }
});
