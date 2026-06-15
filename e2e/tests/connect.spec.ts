import { test, expect } from '../lib/fixtures';
import { addNetworkAndConnect } from '../lib/actions';

test('add a network and connect to Ergo', async ({ page, runtime }) => {
  await page.goto(runtime.bridgeUrl);
  await expect(page.locator('#root')).toBeVisible();

  // The whole suite shares one backend DB within a run, so this spec must not
  // assume a clean slate: another spec may have already added and connected the
  // shared `e2e` network. addNetworkAndConnect does the full add+connect flow on a
  // clean DB and no-ops (just verifies the green indicator) when it's already
  // connected — making this spec order-independent. (Using the raw, non-idempotent
  // addNetwork here previously made the spec silently depend on running first.)
  await addNetworkAndConnect(page, runtime);

  // The network is present in the server tree and shows a connected (green) indicator.
  await expect(page.getByTestId('server-tree').getByText('e2e')).toBeVisible({ timeout: 10_000 });
  await expect(
    page.locator('[data-testid="network-status-indicator"][data-connected="true"]').first(),
  ).toBeVisible({ timeout: 10_000 });
});
