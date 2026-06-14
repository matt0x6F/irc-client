import { test, expect } from '../lib/fixtures';
import { addNetworkAndConnect, selectNetwork, joinChannel } from '../lib/actions';

test('join a channel via /join', async ({ page, runtime }) => {
  await page.goto(runtime.bridgeUrl);
  await addNetworkAndConnect(page, runtime);

  // Reveal the input by selecting the network's status pane, then join.
  await selectNetwork(page);
  await joinChannel(page, '#e2e');

  // The joined channel is present (and selected) in the server tree.
  await expect(page.locator('[data-testid="channel-node"][data-channel="#e2e"]'))
    .toBeVisible({ timeout: 20_000 });
});
