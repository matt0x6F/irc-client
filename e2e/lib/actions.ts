import { Page } from '@playwright/test';
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

  // "+ Add Network" reveals the add form (the networks pane is the default).
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

/** Open an already-joined channel's pane, expanding the network first if its node stays hidden. */
export async function openChannel(page: Page, channel: string, networkName = 'e2e'): Promise<void> {
  const node = page.locator(`[data-testid="channel-node"][data-channel="${channel}"]`);
  // Fast path: the node may already be visible (e.g. session restore re-expanded the tree).
  // Only click the network to expand if it stays hidden for a short window. Using waitFor
  // (rather than a point-in-time isVisible snapshot) lets Playwright's retry loop handle the race.
  const alreadyVisible = await node
    .waitFor({ state: 'visible', timeout: 2_000 })
    .then(() => true, () => false);
  if (!alreadyVisible) {
    await page.getByTestId('server-tree').getByText(networkName, { exact: true }).click();
    await node.waitFor({ state: 'visible', timeout: 15_000 });
  }
  await node.click();
}
