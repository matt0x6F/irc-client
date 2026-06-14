import { test, expect } from '../lib/fixtures';
import { addNetwork, connect, closeSettings } from '../lib/actions';

test('add a network and connect to Ergo', async ({ page, runtime }) => {
  await page.goto(runtime.bridgeUrl);
  await expect(page.locator('#root')).toBeVisible();

  await addNetwork(page, runtime);
  // connect() clicks Connect, closes settings, and waits for the green
  // connected indicator in the server tree (event-driven via Zustand store).
  await connect(page);

  // The server tree shows the network name once connected.
  await expect(page.getByText('e2e').first()).toBeVisible({ timeout: 10_000 });
});
