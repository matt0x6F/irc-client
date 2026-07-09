/**
 * e2e coverage for the two-zone network side rail (network-rail + channel-panel),
 * which replaced the single server-tree. DOM assertions only — no screenshots,
 * per repo convention (Playwright snapshots are per-OS; see other specs).
 *
 * Setup mirrors connect.spec.ts: goto the bridge URL, then addNetworkAndConnect
 * (idempotent — no-ops if the shared `e2e` network from an earlier spec in this
 * serial run is already connected).
 */
import { test, expect } from '../lib/fixtures';
import { addNetworkAndConnect, selectNetwork, joinChannel, networkTile } from '../lib/actions';

test.describe('network side rail', () => {
  test.beforeEach(async ({ page, runtime }) => {
    await page.goto(runtime.bridgeUrl);
    await addNetworkAndConnect(page, runtime);
  });

  test('renders one rail tile per network', async ({ page }) => {
    await expect(page.getByTestId('network-rail')).toBeVisible();

    const tiles = page.getByTestId('network-tile');
    const count = await tiles.count();
    expect(count).toBeGreaterThanOrEqual(1);

    // No two tiles claim the same network — one tile per network, no duplicates.
    const labels = await tiles.evaluateAll((els) => els.map((el) => el.getAttribute('aria-label')));
    expect(new Set(labels).size).toBe(labels.length);

    // The connected `e2e` network specifically has exactly one tile.
    await expect(networkTile(page, 'e2e')).toHaveCount(1);
  });

  test('clicking a network tile shows the channel panel with its channels', async ({ page }) => {
    // selectNetwork clicks the `e2e` tile and waits for the message input,
    // which only appears once the channel panel's status pane is selected.
    await selectNetwork(page);
    await expect(page.getByTestId('channel-panel')).toBeVisible();

    const channel = `#rail-${Date.now().toString(36)}`;
    await joinChannel(page, channel);

    await expect(
      page.locator(`[data-testid="channel-node"][data-channel="${channel}"]`),
    ).toBeVisible();
  });

  test('clicking rail-activity takes over the panel and hides the channel panel', async ({ page }) => {
    await selectNetwork(page);
    await expect(page.getByTestId('channel-panel')).toBeVisible();
    await expect(page.getByTestId('rail-activity')).toHaveAttribute('data-active', 'false');

    await page.getByTestId('rail-activity').click();

    await expect(page.getByTestId('rail-activity')).toHaveAttribute('data-active', 'true');
    await expect(page.getByTestId('channel-panel')).toBeHidden();
  });

  test('rail-add-network is present and clickable', async ({ page }) => {
    const addButton = page.getByTestId('rail-add-network');
    await expect(addButton).toBeVisible();
    await expect(addButton).toBeEnabled();

    // OpenSettings() opens a native OS window, which has nothing to show in
    // the server-mode e2e harness (see lib/actions.ts openSettings doc comment).
    // The only thing observable here is that the click doesn't throw and the
    // rail keeps rendering.
    await addButton.click();
    await expect(page.getByTestId('network-rail')).toBeVisible();
  });
});
