import { test, expect } from '../lib/fixtures';
import { addNetworkAndConnect, selectNetwork, joinChannel } from '../lib/actions';
import { IrcPeer } from '../lib/irc-peer';

test('a message from a second IRC user appears in the UI', async ({ page, runtime }) => {
  await page.goto(runtime.bridgeUrl);
  await addNetworkAndConnect(page, runtime);
  await selectNetwork(page);
  await joinChannel(page, '#e2e'); // UI user is now in #e2e with the pane open

  const peer = new IrcPeer('localhost', runtime.ergoPort, 'peerbot');
  await peer.connect();
  peer.join('#e2e');
  const unique = `from-peer-${Date.now()}`;
  await new Promise((r) => setTimeout(r, 1000)); // let the peer's JOIN settle
  peer.say('#e2e', unique);

  try {
    await expect(page.getByTestId('message-list').getByText(unique)).toBeVisible({ timeout: 15_000 });
  } finally {
    peer.close();
  }
});
