import { test, expect } from '../lib/fixtures';
import {
  openSettings,
  addNetwork,
  toggleAutoConnect,
  readAutoConnect,
  connectViaContextMenu,
  deleteNetwork,
} from '../lib/actions';

// Regression test for the bug where connecting to a network reset its
// auto_connect preference back to false. The defect lived in
// buildNetworkFromConfig, which let the connect-time config (which carries no
// auto_connect value) overwrite the stored preference. A connect must never
// mutate that preference.
//
// This suite shares one backend DB across specs and other specs assume a single
// `e2e` network (their locators aren't network-scoped). So this spec uses a
// dedicated `e2e-ac` network and deletes it afterward — on pass or fail — to
// leave the shared world exactly as it found it. The dedicated nick keeps it
// from colliding with the shared `e2e` connection.
const NAME = 'e2e-ac';

test.afterEach(async ({ page }) => {
  await deleteNetwork(page, NAME).catch(() => {});
});

test('enabling auto-connect survives a manual connect + reload', async ({ page, runtime }) => {
  await page.goto(runtime.bridgeUrl);
  await expect(page.locator('#root')).toBeVisible();

  // Add the network (not connected yet) via the standalone Settings window,
  // then close Settings so the main page is the active surface.
  const settings = await openSettings(page, runtime, 'networks');
  await addNetwork(settings, runtime, { name: NAME, nick: 'e2eac' });
  await settings.close();

  // Enable auto-connect via the context menu, and confirm it took effect.
  await toggleAutoConnect(page, NAME);
  expect(await readAutoConnect(page, NAME)).toBe(true);

  // Manually connect — this is the operation that used to clobber the flag.
  await connectViaContextMenu(page, NAME);

  // Reload the frontend so the menu re-hydrates from the backend's SQLite row,
  // proving the value persisted rather than lingering in frontend memory.
  await page.reload();
  await expect(page.locator('#root')).toBeVisible();

  // The preference must still be enabled.
  expect(await readAutoConnect(page, NAME)).toBe(true);
});
