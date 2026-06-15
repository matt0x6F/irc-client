import { test, expect } from '../lib/fixtures';
import { addNetworkAndConnect, selectNetwork, joinChannel } from '../lib/actions';

// NOTE: the suite runs serially (workers: 1) against a backend whose SQLite DB persists
// across specs. Specs must therefore be idempotent against shared state — use
// addNetworkAndConnect (which no-ops if the network is already connected) rather than
// assuming a clean DB or a particular run order.

// The UI user creates the channel via /join, so Ergo grants it channel-operator
// status — it may therefore set channel modes. These tests exercise the full loop:
// editor -> SendCommand -> server echo -> MODE parse -> canonical string -> live header.

test('setting a flag and a key through the mode editor updates the header live', async ({ page, runtime }) => {
  await page.goto(runtime.bridgeUrl);
  await addNetworkAndConnect(page, runtime);
  await selectNetwork(page);
  await joinChannel(page, '#e2e-modes');

  // Open the editor from the channel header.
  const modesButton = page.getByTestId('channel-modes-button');
  await modesButton.waitFor({ state: 'visible', timeout: 20_000 });
  await modesButton.click();

  const editor = page.getByTestId('channel-mode-editor');
  await editor.waitFor({ state: 'visible', timeout: 10_000 });

  // Toggle "moderated" (+m) and set a channel key (+k).
  const key = `k${Date.now()}`;
  await page.getByTestId('mode-flag-m').check();
  await page.getByTestId('mode-param-k').fill(key);
  await page.getByTestId('mode-save-button').click();

  // The editor closes on the server's MODE echo; the header then reflects the new
  // canonical mode string (letters sorted, key as a parameter).
  await editor.waitFor({ state: 'hidden', timeout: 15_000 });
  await expect(modesButton).toContainText('m', { timeout: 15_000 });
  await expect(modesButton).toContainText(key, { timeout: 15_000 });

  // The change is also surfaced as a system line in the channel itself, not the server log.
  await expect(
    page.getByTestId('message-list').getByText(/sets mode:/i).first(),
  ).toBeVisible({ timeout: 15_000 });
});

test('adding a ban persists and reappears when the editor is reopened', async ({ page, runtime }) => {
  await page.goto(runtime.bridgeUrl);
  await addNetworkAndConnect(page, runtime);
  await selectNetwork(page);
  await joinChannel(page, '#e2e-bans');

  const modesButton = page.getByTestId('channel-modes-button');
  await modesButton.waitFor({ state: 'visible', timeout: 20_000 });
  await modesButton.click();
  await page.getByTestId('channel-mode-editor').waitFor({ state: 'visible', timeout: 10_000 });

  const mask = `e2eban${Date.now()}!*@*`;
  await page.getByTestId('mode-ban-input').fill(mask);
  await page.getByTestId('mode-ban-add').click();
  await page.getByTestId('mode-save-button').click();
  await page.getByTestId('channel-mode-editor').waitFor({ state: 'hidden', timeout: 15_000 });

  // Reopen — the editor fetches the live ban list (RPL_BANLIST 367/368) and should
  // show the mask we just set. Scope to the editor: the mask also appears in the
  // channel buffer as the "sets mode: +b <mask>" system line, so an unscoped
  // getByText(mask) is a strict-mode violation (two matches).
  await modesButton.click();
  const editor = page.getByTestId('channel-mode-editor');
  await editor.waitFor({ state: 'visible', timeout: 10_000 });
  await expect(editor.getByText(mask, { exact: false })).toBeVisible({ timeout: 15_000 });
});
