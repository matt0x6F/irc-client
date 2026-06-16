import { test, expect } from '../lib/fixtures';
import {
  openSettings,
  addNetwork,
  toggleAutoConnect,
  readAutoConnect,
  connectViaContextMenu,
} from '../lib/actions';

// Regression test for the bug where connecting to a network reset its
// auto_connect preference back to false. The defect lived in
// buildNetworkFromConfig, which let the connect-time config (which carries no
// auto_connect value) overwrite the stored preference. A connect must never
// mutate that preference.
//
// Uses a dedicated network name so it's independent of the shared `e2e`
// network other specs add/connect. Re-adding the network via addNetwork
// (SaveNetwork) resets its auto_connect to false, so the spec starts from a
// known state regardless of prior runs against the shared DB.
test('enabling auto-connect survives a manual connect + reload', async ({ page, runtime }) => {
  const name = 'e2e-ac';

  await page.goto(runtime.bridgeUrl);
  await expect(page.locator('#root')).toBeVisible();

  // Add the network (not connected yet) via the standalone Settings window,
  // then close Settings so the main page is the active surface.
  const settings = await openSettings(page, runtime, 'networks');
  await addNetwork(settings, runtime, { name, nick: 'e2eac' });
  await settings.close();

  // Enable auto-connect via the context menu, and confirm it took effect.
  await toggleAutoConnect(page, name);
  expect(await readAutoConnect(page, name)).toBe(true);

  // Manually connect — this is the operation that used to clobber the flag.
  await connectViaContextMenu(page, name);

  // Reload the frontend so the menu re-hydrates from the backend's SQLite row,
  // proving the value persisted rather than lingering in frontend memory.
  await page.reload();
  await expect(page.locator('#root')).toBeVisible();

  // The preference must still be enabled.
  expect(await readAutoConnect(page, name)).toBe(true);
});
