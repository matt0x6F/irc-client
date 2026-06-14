import { test, expect } from '../lib/fixtures';
import { addNetworkAndConnect, selectNetwork } from '../lib/actions';

test('join a channel via /join', async ({ page, runtime }) => {
  await page.goto(runtime.bridgeUrl);
  await addNetworkAndConnect(page, runtime);

  // Select the network's status pane to reveal the input.
  await selectNetwork(page);

  const input = page.getByTestId('message-input');
  await input.click();
  await input.fill('/join #e2e');
  await input.press('Enter');

  await expect(page.locator('[data-testid="channel-node"][data-channel="#e2e"]'))
    .toBeVisible({ timeout: 20_000 });
});
