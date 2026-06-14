import { test, expect } from '../lib/fixtures';
import { addNetworkAndConnect, selectNetwork, joinChannel, openChannel } from '../lib/actions';

test('send a message; it renders and survives a reload', async ({ page, runtime }) => {
  await page.goto(runtime.bridgeUrl);
  await addNetworkAndConnect(page, runtime);
  await selectNetwork(page);
  await joinChannel(page, '#e2e');

  const input = page.getByTestId('message-input');
  const unique = `hello-${Date.now()}`;
  await input.click();
  await input.fill(unique);
  await input.press('Enter');

  // Renders in the buffer (echo-message).
  await expect(page.getByTestId('message-list').getByText(unique)).toBeVisible({ timeout: 15_000 });

  // Survives a frontend reload (persisted to the per-run SQLite DB).
  await page.reload();
  await expect(page.locator('#root')).toBeVisible();
  await openChannel(page, '#e2e');
  await expect(page.getByTestId('message-list').getByText(unique)).toBeVisible({ timeout: 15_000 });
});
