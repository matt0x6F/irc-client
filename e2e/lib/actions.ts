import { Page, Locator } from '@playwright/test';
import { Runtime } from './runtime';

/** URL of the standalone Settings window for a given pane. */
function settingsUrl(runtime: Runtime, section = 'networks'): string {
  return `${runtime.bridgeUrl}/?view=settings&section=${section}`;
}

/**
 * Open the Settings window on the given pane and return its Page.
 *
 * Settings is its own native window now (not an in-app modal). In e2e server
 * mode the app is served over HTTP and driven as a web page, so the backend's
 * native-window OpenSettings() has nothing to show — instead we open Settings as
 * a second page in the SAME browser context. That shares the single backend
 * (same SQLite DB, same IRC clients), and crucially leaves the main `page` alive
 * so it can observe connection state via its live event subscription / 5s poll.
 */
export async function openSettings(
  page: Page,
  runtime: Runtime,
  section = 'networks',
): Promise<Page> {
  const settings = await page.context().newPage();
  await settings.goto(settingsUrl(runtime, section));
  return settings;
}

/**
 * Fill and submit the add-network form in an already-open Settings page, pointing
 * the server at the per-run Ergo instance. Waits for the saved network's Connect
 * button to confirm the save landed.
 */
export async function addNetwork(
  settings: Page,
  runtime: Runtime,
  opts: { name?: string; nick?: string } = {},
): Promise<void> {
  const name = opts.name ?? 'e2e';
  const nick = opts.nick ?? 'e2euser';

  // "+ Add network" opens the editor view (the networks pane is the default).
  await settings.getByTestId('add-network-button').waitFor({ state: 'visible', timeout: 10_000 });
  await settings.getByTestId('add-network-button').click();

  await settings.getByTestId('network-name-input').fill(name);
  await settings.getByTestId('server-address-input').fill('localhost');
  await settings.getByTestId('server-port-input').fill(String(runtime.ergoPort));
  await settings.getByTestId('network-nickname-input').fill(nick);
  await settings.getByTestId('network-username-input').fill(nick);
  await settings.getByTestId('network-realname-input').fill(nick);

  await settings.getByTestId('save-network-button').click();

  // Network appears in the list (Connect button) after the save.
  await settings.getByTestId('network-connect-button').waitFor({ state: 'visible', timeout: 10_000 });
}

/**
 * Click Connect in the Settings page, then wait for the main app's server tree to
 * show the connected (green) indicator before closing Settings.
 *
 * The server tree on the main page is authoritative: it reflects the Go
 * `connection-status` event and the periodic network poll, not the Settings
 * list's local (non-reactive) state. We observe the green indicator BEFORE
 * closing the Settings page so the asynchronous ConnectNetwork() RPC issued from
 * that page isn't torn down mid-request.
 */
export async function connect(page: Page, settings: Page): Promise<void> {
  await settings.getByTestId('network-connect-button').click();

  await page
    .locator('[data-testid="network-status-indicator"][data-connected="true"]')
    .first()
    .waitFor({ state: 'visible', timeout: 30_000 });

  await settings.close();
}

/** Convenience: add + connect via the Settings window. Used by most specs.
 *
 * When tests run serially in one Playwright run the backend SQLite DB persists
 * across specs. If a previous spec already added and connected the network, the
 * main page's server tree already shows the green indicator — skip the add/connect
 * dance entirely.
 *
 * Note: the fast-path checks only connection state, not channel membership; specs
 * must (re)join channels they need. Specs goto(bridgeUrl) before calling, so the
 * fast-path observes the main app.
 */
export async function addNetworkAndConnect(
  page: Page,
  runtime: Runtime,
  opts: { name?: string; nick?: string } = {},
): Promise<void> {
  // Fast-path: network already connected (DB persisted from an earlier spec).
  const alreadyConnected = await page
    .locator('[data-testid="network-status-indicator"][data-connected="true"]')
    .first()
    .waitFor({ state: 'visible', timeout: 1_500 })
    .then(() => true, () => false);

  if (alreadyConnected) return;

  const settings = await openSettings(page, runtime, 'networks');
  await addNetwork(settings, runtime, opts);
  await connect(page, settings);
}

/**
 * Locator for a network's rail tile. Tiles show a monogram/icon only — the
 * network name lives solely in `aria-label` (title-cased tooltip too), so
 * this is the one place callers should target a network by name.
 */
export function networkTile(page: Page, name: string): Locator {
  return page.locator(
    `[data-testid="network-tile"][aria-label="${name.replace(/"/g, '\\"')}"]`,
  );
}

/** Select the network's status pane so the message input becomes available. */
export async function selectNetwork(page: Page, name = 'e2e'): Promise<void> {
  await networkTile(page, name).click();
  await page.getByTestId('message-input').waitFor({ state: 'visible', timeout: 10_000 });
}

/** Right-click a network tile to open its context menu. */
export async function openNetworkContextMenu(page: Page, name = 'e2e'): Promise<void> {
  await networkTile(page, name).click({ button: 'right' });
  await page.getByTestId('network-context-menu').waitFor({ state: 'visible', timeout: 5_000 });
}

/** Dismiss any open context menu (Escape is wired to close it). */
async function dismissContextMenu(page: Page): Promise<void> {
  await page.keyboard.press('Escape');
  await page
    .getByTestId('network-context-menu')
    .waitFor({ state: 'hidden', timeout: 5_000 })
    .catch(() => {});
}

/**
 * Read the network's persisted auto-connect preference by opening its context
 * menu and reading the toggle button's reflected state, then closing the menu.
 * After a page reload this reflects the value the backend persisted to SQLite.
 */
export async function readAutoConnect(page: Page, name = 'e2e'): Promise<boolean> {
  await openNetworkContextMenu(page, name);
  const value = await page
    .getByTestId('toggle-auto-connect-button')
    .getAttribute('data-auto-connect');
  await dismissContextMenu(page);
  return value === 'true';
}

/** Toggle the auto-connect preference for a network via its context menu. */
export async function toggleAutoConnect(page: Page, name = 'e2e'): Promise<void> {
  await openNetworkContextMenu(page, name);
  await page.getByTestId('toggle-auto-connect-button').click();
  // The menu closes itself after the toggle + network refresh completes.
  await page.getByTestId('network-context-menu').waitFor({ state: 'hidden', timeout: 5_000 });
}

/**
 * Connect to a network via its context-menu "Connect" entry (only present when
 * the network is disconnected) and wait for its green indicator. This is the
 * manual-connect path that historically clobbered the stored auto_connect flag.
 * The indicator is scoped to the named network's row so it's unambiguous when
 * other networks are also present in the shared-DB run.
 */
export async function connectViaContextMenu(page: Page, name = 'e2e'): Promise<void> {
  await openNetworkContextMenu(page, name);
  await page.getByTestId('network-context-menu').getByText('Connect', { exact: true }).click();
  await networkIndicator(page, name, true).waitFor({ state: 'visible', timeout: 30_000 });
}

/**
 * Disconnect a network via its context-menu "Disconnect" entry (only present
 * when the network is connected) and wait for its indicator to go grey. Scoped
 * to the named network's row so it's unambiguous in the shared-DB run.
 */
export async function disconnectViaContextMenu(page: Page, name = 'e2e'): Promise<void> {
  await openNetworkContextMenu(page, name);
  await page.getByTestId('network-context-menu').getByText('Disconnect', { exact: true }).click();
  await networkIndicator(page, name, false).waitFor({ state: 'visible', timeout: 30_000 });
}

/**
 * Locator for a network tile's status indicator in the named connection state.
 * The indicator is now a descendant of the tile (matched by its aria-label),
 * not a preceding-sibling of a name text node — tiles show a monogram/icon
 * only, the name lives solely in aria-label.
 */
export function networkIndicator(page: Page, name: string, connected: boolean): Locator {
  return networkTile(page, name).locator(
    `[data-testid="network-status-indicator"][data-connected="${connected ? 'true' : 'false'}"]`,
  );
}

/**
 * Delete a network via its context-menu "Delete" entry and confirm the dialog,
 * then wait for the node to disappear. Used to keep a dedicated network fully
 * hermetic: the shared-DB suite assumes a single `e2e` network, so a spec that
 * adds its own network must remove it (connection state and DB row) afterward.
 * Best-effort no-op if the network isn't present.
 */
export async function deleteNetwork(page: Page, name: string): Promise<void> {
  const node = networkTile(page, name);
  if (!(await node.isVisible().catch(() => false))) return;
  await openNetworkContextMenu(page, name);
  await page.getByTestId('network-context-menu').getByText('Delete', { exact: true }).click();
  await page.getByTestId('confirm-delete-network-button').click();
  await node.waitFor({ state: 'hidden', timeout: 15_000 });
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

/** Open an already-joined channel's pane, selecting its network first if the channel-panel isn't showing it. */
export async function openChannel(page: Page, channel: string, networkName = 'e2e'): Promise<void> {
  const node = page.locator(`[data-testid="channel-node"][data-channel="${channel}"]`);
  // Fast path: the node may already be visible (e.g. session restore left this
  // network selected). Only click the network tile if it stays hidden for a
  // short window. Using waitFor (rather than a point-in-time isVisible snapshot)
  // lets Playwright's retry loop handle the race.
  const alreadyVisible = await node
    .waitFor({ state: 'visible', timeout: 2_000 })
    .then(() => true, () => false);
  if (!alreadyVisible) {
    await networkTile(page, networkName).click();
    await node.waitFor({ state: 'visible', timeout: 15_000 });
  }
  await node.click();
}
