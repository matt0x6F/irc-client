import { Page } from '@playwright/test';
import { Runtime } from './runtime';

/**
 * Fill and submit the add-network form, pointing the server at the per-run
 * Ergo instance. Opens Settings first. Leaves Settings open on the network list.
 */
export async function addNetwork(
  page: Page,
  runtime: Runtime,
  opts: { name?: string; nick?: string } = {},
): Promise<void> {
  const name = opts.name ?? 'e2e';
  const nick = opts.nick ?? 'e2euser';

  // Ensure the page body has focus before sending keyboard shortcuts.
  await page.locator('body').click();
  await page.keyboard.press('Control+Comma'); // open Settings (defaults to Networks)

  // Wait for the Settings modal to appear (Close button is the reliable sentinel).
  await page.getByTestId('settings-close-button').waitFor({ state: 'visible', timeout: 10_000 });

  // Click "+ Add Network" to show the add form.
  await page.getByTestId('add-network-button').click();

  // Fill the form fields.
  await page.getByTestId('network-name-input').fill(name);
  await page.getByTestId('server-address-input').fill('localhost');
  await page.getByTestId('server-port-input').fill(String(runtime.ergoPort));
  await page.getByTestId('network-nickname-input').fill(nick);
  await page.getByTestId('network-username-input').fill(nick);
  await page.getByTestId('network-realname-input').fill(nick);

  // Submit — the save button has testid "save-network-button".
  await page.getByTestId('save-network-button').click();

  // Wait for the network to appear in the list (save-network-button disappears after submit).
  await page.getByTestId('network-connect-button').waitFor({ state: 'visible', timeout: 10_000 });
}

/**
 * Click Connect and wait for the server tree to show the connected (green) indicator.
 * The settings modal's local state won't update reactively after ConnectNetwork() returns
 * (the Go side connects asynchronously), but the server tree subscribes to the Wails
 * connection-status event via the Zustand store, so its indicator is authoritative.
 */
export async function connect(page: Page): Promise<void> {
  await page.getByTestId('network-connect-button').click();

  // Close settings so the server tree is visible — the tree uses the Zustand store
  // which is updated by the Go `connection-status` event, making it the reliable
  // indicator of actual connection state.
  await page.getByTestId('settings-close-button').click();
  await page.getByTestId('settings-close-button').waitFor({ state: 'hidden', timeout: 5_000 });

  // The server tree shows a connected indicator anchored by data-testid + data-connected attribute.
  await page.locator('[data-testid="network-status-indicator"][data-connected="true"]').first().waitFor({ state: 'visible', timeout: 30_000 });
}

/** Close the Settings modal to reach the main chat UI. */
export async function closeSettings(page: Page): Promise<void> {
  // If the modal is still open, close it; otherwise this is a no-op.
  const closeBtn = page.getByTestId('settings-close-button');
  const isVisible = await closeBtn.isVisible();
  if (isVisible) {
    await closeBtn.click();
    await closeBtn.waitFor({ state: 'hidden', timeout: 5_000 });
  }
}

/** Convenience: add + connect + close settings. Used by most specs. */
export async function addNetworkAndConnect(
  page: Page,
  runtime: Runtime,
  opts: { name?: string; nick?: string } = {},
): Promise<void> {
  await addNetwork(page, runtime, opts);
  await connect(page);
  // connect() already closes settings; closeSettings() is idempotent.
}

/** Select the network's status pane so the message input becomes available. */
export async function selectNetwork(page: Page, name = 'e2e'): Promise<void> {
  await page.getByTestId('server-tree').getByText(name, { exact: true }).click();
  await page.getByTestId('message-input').waitFor({ state: 'visible', timeout: 10_000 });
}

/** Join a channel via the /join command and open its pane. */
export async function joinChannel(page: Page, channel: string): Promise<void> {
  const input = page.getByTestId('message-input');
  await input.click();
  await input.fill(`/join ${channel}`);
  await input.press('Enter');
  const node = page.locator(`[data-testid="channel-node"][data-channel="${channel}"]`);
  await node.waitFor({ state: 'visible', timeout: 20_000 });
  await node.click();
}
